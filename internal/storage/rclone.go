package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// Rclone runs rclone against the portal's own rclone.conf, never the user's personal one.
type Rclone struct {
	Bin    string // rclone executable (Termux's package on the phone: v1.50.1)
	Config string // <data>/rclone.conf
}

// File is one file in a cloud folder.
type File struct {
	Name string `json:"Name"`
	Size int64  `json:"Size"`
}

// Join appends path to an rclone root ("gdrive:", "b2:bucket", "t:/dir").
func Join(remote, path string) string {
	if strings.HasSuffix(remote, ":") || strings.HasSuffix(remote, "/") {
		return remote + path
	}
	return remote + "/" + path
}

const exitDirNotFound = 3 // rclone's exit code for a missing directory

// failFast makes rclone give up on an unreachable cloud within about a minute instead of retrying
// for many; the uploader backs off and tries again itself, and the Storage page shows the error.
var failFast = []string{"--contimeout", "15s", "--timeout", "1m", "--low-level-retries", "3", "--retries", "1"}

func (r Rclone) cmd(ctx context.Context, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, r.Bin, append(append([]string{"--config", r.Config}, failFast...), args...)...)
}

// run runs rclone and returns its stdout, or an error carrying rclone's last message.
func (r Rclone) run(ctx context.Context, args ...string) ([]byte, error) {
	var out, errb bytes.Buffer
	c := r.cmd(ctx, args...)
	c.Stdout, c.Stderr = &out, &errb
	if err := c.Run(); err != nil {
		return out.Bytes(), &runError{args[0], failure(errb.String(), err), exitCode(err)}
	}
	return out.Bytes(), nil
}

type runError struct {
	op, msg string
	code    int
}

func (e *runError) Error() string { return "rclone " + e.op + ": " + e.msg }

func exitCode(err error) int {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return -1
}

func notFound(err error) bool {
	var re *runError
	return errors.As(err, &re) && re.code == exitDirNotFound
}

var urlQuery = regexp.MustCompile(`(https?://[^?\s]+)\?[^\s]*?(:|\s|$)`)

// failure is what the user sees of a failed rclone run: its final "Failed to …" message (which
// for a Google token error runs on over several lines of JSON), else its last line, else err.
func failure(stderr string, err error) string {
	s := strings.TrimSpace(stderr)
	if i := strings.LastIndex(s, "Failed to"); i >= 0 {
		s = s[i:]
	} else if i := strings.LastIndex(s, "\n"); i >= 0 {
		s = s[i+1:]
	}
	s = urlQuery.ReplaceAllString(s, "$1$2") // long and noisy (Drive's field lists)
	if s = strings.Join(strings.Fields(s), " "); s == "" {
		return err.Error()
	}
	expired := strings.Contains(s, "invalid_grant")
	if len(s) > 300 {
		s = s[:300] + "…"
	}
	if expired {
		s = "the Google sign-in has expired or was revoked: remove this storage and add Google Drive again (" + s + ")"
	}
	return s
}

// CopyTo uploads the local file src to the remote path dst.
func (r Rclone) CopyTo(ctx context.Context, src, dst string) error {
	_, err := r.run(ctx, "copyto", src, dst)
	return err
}

// List lists the files in a cloud folder; a folder that does not exist is empty.
func (r Rclone) List(ctx context.Context, dir string) ([]File, error) {
	out, err := r.run(ctx, "lsjson", "--files-only", dir)
	if notFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var files []File
	if err := json.Unmarshal(out, &files); err != nil {
		return nil, fmt.Errorf("rclone lsjson: %w", err)
	}
	return files, nil
}

// Cat streams count bytes of a cloud file from offset. Close ends rclone.
func (r Rclone) Cat(ctx context.Context, path string, offset, count int64) (io.ReadCloser, error) {
	c := r.cmd(ctx, "cat", "--offset", strconv.FormatInt(offset, 10), "--count", strconv.FormatInt(count, 10), path)
	out, err := c.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := c.Start(); err != nil {
		return nil, err
	}
	return catReader{out, c}, nil
}

type catReader struct {
	io.ReadCloser
	c *exec.Cmd
}

func (r catReader) Close() error {
	r.ReadCloser.Close()
	return r.c.Wait()
}

// Prune deletes clips older than days under dir, then the folders that became empty (spec §5.6).
// Drive deletes skip the trash, which would otherwise keep filling the user's quota.
func (r Rclone) Prune(ctx context.Context, dir string, days int, drive bool) error {
	args := []string{"delete", dir, "--min-age", fmt.Sprintf("%dd", days)}
	if drive {
		args = append(args, "--drive-use-trash=false")
	}
	if _, err := r.run(ctx, args...); err != nil && !notFound(err) {
		return err
	}
	if _, err := r.run(ctx, "rmdirs", dir, "--leave-root"); err != nil && !notFound(err) {
		return err
	}
	return nil
}

// Check proves a remote works (credentials, bucket, network) by listing its top folder.
func (r Rclone) Check(ctx context.Context, remote string) error {
	_, err := r.run(ctx, "lsd", remote)
	return err
}
