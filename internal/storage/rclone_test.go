package storage

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// rcloneBin is rclone 1.50.1, the phone's version: scripts/itest.sh downloads it to .cache.
func rcloneBin(t *testing.T) string {
	t.Helper()
	p, _ := filepath.Abs("../../.cache/rclone-amd64")
	if v := os.Getenv("RCLONE"); v != "" {
		p = v
	}
	if _, err := os.Stat(p); err != nil {
		t.Skip("no rclone 1.50.1 at .cache/rclone-amd64: run scripts/itest.sh once to fetch it")
	}
	return p
}

func TestSections(t *testing.T) {
	p := filepath.Join(t.TempDir(), "rclone.conf")
	if err := SetSection(p, "a", map[string]string{"type": "local"}); err != nil {
		t.Fatal(err)
	}
	SetSection(p, "b", map[string]string{"type": "drive", "token": `{"access_token":"x","expiry":"2026-10-01T00:00:00Z"}`})
	SetSection(p, "a", map[string]string{"type": "s3", "provider": "Other"}) // replaced, b kept
	b, _ := os.ReadFile(p)
	want := "[b]\ntoken = {\"access_token\":\"x\",\"expiry\":\"2026-10-01T00:00:00Z\"}\ntype = drive\n\n[a]\nprovider = Other\ntype = s3\n"
	if string(b) != want {
		t.Fatalf("got\n%s\nwant\n%s", b, want)
	}
	if st, _ := os.Stat(p); st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", st.Mode())
	}
	RemoveSection(p, "b")
	if b, _ := os.ReadFile(p); string(b) != "[a]\nprovider = Other\ntype = s3\n" {
		t.Fatalf("after remove:\n%s", b)
	}
}

func TestJoin(t *testing.T) {
	for in, want := range map[[2]string]string{
		{"gdrive:", "camorage/gate"}:       "gdrive:camorage/gate",
		{"b2:bucket", "camorage/gate"}:     "b2:bucket/camorage/gate",
		{"t:/tmp/cloud/", "camorage/gate"}: "t:/tmp/cloud/camorage/gate",
	} {
		if got := Join(in[0], in[1]); got != want {
			t.Errorf("Join(%q, %q) = %q", in[0], in[1], got)
		}
	}
}

func TestRcloneAgainstAFolder(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	r := Rclone{Bin: rcloneBin(t), Config: filepath.Join(dir, "rclone.conf")}
	SetSection(r.Config, "t", map[string]string{"type": "local"})
	cloud := filepath.Join(dir, "cloud")
	os.MkdirAll(cloud, 0o700)
	root := "t:" + cloud

	if err := r.Check(ctx, root); err != nil {
		t.Fatalf("check: %v", err)
	}
	if err := r.Check(ctx, "nosuch:"); err == nil || !strings.Contains(err.Error(), "nosuch") {
		t.Fatalf("check of a missing remote: %v", err)
	}
	src := filepath.Join(dir, "clip.mp4")
	os.WriteFile(src, []byte("0123456789abcdef"), 0o600)
	day := Join(root, "camorage/gate/2026-10-01")
	if err := r.CopyTo(ctx, src, day+"/10-00-00_hour_60s.mp4"); err != nil {
		t.Fatal(err)
	}
	files, err := r.List(ctx, day)
	if err != nil || len(files) != 1 || files[0].Name != "10-00-00_hour_60s.mp4" || files[0].Size != 16 {
		t.Fatalf("list: %+v %v", files, err)
	}
	if files, err := r.List(ctx, Join(root, "camorage/gate/2026-10-02")); err != nil || files != nil {
		t.Fatalf("a missing day: %+v %v", files, err)
	}
	rc, err := r.Cat(ctx, day+"/10-00-00_hour_60s.mp4", 4, 6)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(rc)
	rc.Close()
	if string(got) != "456789" {
		t.Fatalf("cat: %q", got)
	}

	old := filepath.Join(cloud, "camorage/gate/2026-09-20/10-00-00_hour_60s.mp4")
	os.MkdirAll(filepath.Dir(old), 0o700)
	os.WriteFile(old, []byte("old"), 0o600)
	tenDays := time.Now().Add(-10 * 24 * time.Hour)
	os.Chtimes(old, tenDays, tenDays)
	if err := r.Prune(ctx, Join(root, "camorage/gate"), 7, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Dir(old)); !os.IsNotExist(err) {
		t.Fatal("the old clip or its emptied folder is still there")
	}
	if files, _ := r.List(ctx, day); len(files) != 1 {
		t.Fatal("pruned a clip that is within its days")
	}
	if err := r.Prune(ctx, Join(root, "camorage/never-uploaded"), 7, false); err != nil {
		t.Fatalf("pruning a camera with nothing in the cloud: %v", err)
	}
}

func TestRcloneErrorMessages(t *testing.T) {
	exit := errors.New("exit status 1")
	// rclone 1.50.1 ends a Google token failure with the token endpoint's JSON body
	expired := "2026/10/01 09:00:00 Failed to lsd: couldn't list directory: Get https://www.googleapis.com/drive/v3/files?alt=json&fields=files(id,name,size,md5Checksum,trashed,modifiedTime,createdTime,mimeType,parents,webViewLink,shortcutDetails),nextPageToken,incompleteSearch&pageSize=1000&prettyPrint=false&q=trashed%3Dfalse+and+%27root%27+in+parents&supportsAllDrives=true: oauth2: cannot fetch token: 400 Bad Request\nResponse: {\n  \"error\": \"invalid_grant\",\n  \"error_description\": \"Token has been expired or revoked.\"\n}\n"
	if got := failure(expired, exit); !strings.Contains(got, "sign-in has expired or was revoked") || !strings.Contains(got, "cannot fetch token") {
		t.Errorf("expired token: %q", got)
	}
	plain := "2026/10/01 09:00:00 NOTICE: something\n2026/10/01 09:00:00 Failed to create file system for \"nosuch:\": didn't find section in config file\n"
	if got := failure(plain, exit); got != `Failed to create file system for "nosuch:": didn't find section in config file` {
		t.Errorf("plain: %q", got)
	}
	if got := failure("", exit); got != "exit status 1" {
		t.Errorf("no stderr: %q", got)
	}
	if got := failure("Failed to copy: "+strings.Repeat("x", 1000), exit); len(got) > 310 {
		t.Errorf("not capped: %d bytes", len(got))
	}
}

func TestRcloneGivesUpQuickly(t *testing.T) {
	// rclone's defaults retry an unreachable cloud for many minutes (low-level retries 10, retries 3,
	// 1 min connect timeout); the uploader has its own back-off, so fail fast and report it
	dir := t.TempDir()
	bin := filepath.Join(dir, "rclone")
	sh, err := exec.LookPath("sh") // Android has no /bin/sh; Termux's is $PREFIX/bin/sh
	if err != nil {
		t.Skip("no sh on PATH")
	}
	os.WriteFile(bin, []byte("#!"+sh+"\necho \"$@\" > \"$0.args\"\n"), 0o700)
	r := Rclone{Bin: bin, Config: filepath.Join(dir, "rclone.conf")}
	r.Check(context.Background(), "x:")
	b, _ := os.ReadFile(bin + ".args")
	if !strings.Contains(string(b), "--contimeout 15s --timeout 1m --low-level-retries 3 --retries 1 ") {
		t.Fatalf("rclone ran with: %s", b)
	}
}
