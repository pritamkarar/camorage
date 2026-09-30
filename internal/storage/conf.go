// Package storage copies recordings to cloud storage with rclone (spec §5.5, §5.6) and signs in
// to Google Drive.
package storage

import (
	"errors"
	"io/fs"
	"os"
	"sort"
	"strings"
	"sync"

	"camorage/internal/config"
)

var confMu sync.Mutex // one writer of rclone.conf at a time

// SetSection writes section name of the rclone.conf at path with the keys in kv, replacing an
// earlier section of that name and keeping the others. rclone itself rewrites the file when it
// refreshes a Drive token, so every write reads the current file first.
// ponytail: a token refresh landing between the read and the write is lost (rclone then refreshes
// again); lock the file against rclone if that ever bites.
func SetSection(path, name string, kv map[string]string) error {
	confMu.Lock()
	defer confMu.Unlock()
	sections, err := readSections(path)
	if err != nil {
		return err
	}
	keys := make([]string, 0, len(kv))
	for k := range kv {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString("[" + name + "]\n")
	for _, k := range keys {
		b.WriteString(k + " = " + kv[k] + "\n")
	}
	return writeSections(path, append(without(sections, name), section{name, b.String()}))
}

// RemoveSection drops section name from the rclone.conf at path.
func RemoveSection(path, name string) error {
	confMu.Lock()
	defer confMu.Unlock()
	sections, err := readSections(path)
	if err != nil {
		return err
	}
	return writeSections(path, without(sections, name))
}

type section struct{ name, text string }

func readSections(path string) ([]section, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []section
	for _, line := range strings.SplitAfter(string(b), "\n") {
		if t := strings.TrimSpace(line); strings.HasPrefix(t, "[") && strings.HasSuffix(t, "]") {
			out = append(out, section{name: t[1 : len(t)-1]})
		}
		if len(out) > 0 && strings.TrimSpace(line) != "" {
			out[len(out)-1].text += strings.TrimRight(line, "\n") + "\n"
		}
	}
	return out, nil
}

func without(sections []section, name string) []section {
	var out []section
	for _, s := range sections {
		if s.name != name {
			out = append(out, s)
		}
	}
	return out
}

func writeSections(path string, sections []section) error {
	parts := make([]string, len(sections))
	for i, s := range sections {
		parts[i] = s.text
	}
	return config.WriteFileAtomic(path, []byte(strings.Join(parts, "\n")))
}
