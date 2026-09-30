package storage

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Kinds of clips (spec §5.5): a clock hour of continuous recording, or one motion event.
const (
	KindHour   = "hour"
	KindMotion = "motion"
)

var clipRe = regexp.MustCompile(`^(\d\d)-(\d\d)-(\d\d)_(hour|motion)_(\d+)s\.mp4$`)

// ClipPath is where a clip goes under a target: camorage/<cam>/<YYYY-MM-DD>/<HH-MM-SS>_<kind>_<secs>s.mp4,
// by the clip's start in phone-local time (start must be in the phone's zone).
func ClipPath(cam string, start time.Time, kind string, secs int) string {
	return fmt.Sprintf("camorage/%s/%s/%s_%s_%ds.mp4", cam, start.Format("2006-01-02"), start.Format("15-04-05"), kind, secs)
}

// ValidClipName reports whether name is a clip file name (and nothing a path could be built from).
func ValidClipName(name string) bool { return clipRe.MatchString(name) }

// ParseClip reads a clip's file name, in the folder of day (a phone-local midnight), back into its
// start, length and kind.
func ParseClip(day time.Time, name string) (time.Time, time.Duration, string, bool) {
	m := clipRe.FindStringSubmatch(name)
	if m == nil {
		return time.Time{}, 0, "", false
	}
	h, _ := strconv.Atoi(m[1])
	mi, _ := strconv.Atoi(m[2])
	s, _ := strconv.Atoi(m[3])
	secs, _ := strconv.Atoi(m[5])
	y, mo, d := day.Date()
	return time.Date(y, mo, d, h, mi, s, 0, day.Location()), time.Duration(secs) * time.Second, m[4], true
}

// Ledger remembers which clips are in the cloud, so footage is uploaded once:
// <Dir>/<cam>/<YYYY-MM-DD>.txt, one clip path per line, by the clip's day folder.
type Ledger struct {
	Dir string
	mu  sync.Mutex
}

const slack = 2 * time.Second // clip names keep whole seconds

// Covered reports whether the clips of cam in the ledger together cover [from, to), to a couple of
// seconds. A window planned again after the start of its footage was deleted (by the janitor, or
// by the planning window sliding past it) gets a new clip name, yet is already in the cloud.
func (l *Ledger) Covered(cam string, from, to time.Time, zone *time.Location) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	f := from.In(zone)
	day := time.Date(f.Year(), f.Month(), f.Day(), 0, 0, 0, 0, zone)
	var have [][2]time.Time
	for _, d := range []time.Time{day.AddDate(0, 0, -1), day} { // a clip may have started the day before
		b, _ := os.ReadFile(filepath.Join(l.Dir, cam, d.Format("2006-01-02")+".txt"))
		for _, line := range strings.Split(string(b), "\n") {
			if start, length, _, ok := ParseClip(d, path.Base(line)); ok {
				have = append(have, [2]time.Time{start, start.Add(length)})
			}
		}
	}
	sort.Slice(have, func(i, j int) bool { return have[i][0].Before(have[j][0]) })
	reached := from
	for _, c := range have {
		if c[0].After(reached.Add(slack)) {
			break
		}
		if end := c[1].Add(slack); end.After(reached) {
			reached = end
		}
	}
	return !reached.Before(to)
}

// Add records that clip is in the cloud.
func (l *Ledger) Add(cam, clip string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	day := "unknown"
	if parts := strings.Split(clip, "/"); len(parts) == 4 {
		day = parts[2]
	}
	p := filepath.Join(l.Dir, cam, day+".txt")
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	_, err = f.WriteString(clip + "\n")
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// Prune deletes ledger files of days before the last days days (phone-local): the uploader no
// longer plans that far back, so they cannot matter any more.
func (l *Ledger) Prune(now time.Time, days int) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	cams, err := os.ReadDir(l.Dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	y, m, d := now.Date()
	cutoff := time.Date(y, m, d, 0, 0, 0, 0, now.Location()).AddDate(0, 0, -days).Format("2006-01-02")
	for _, c := range cams {
		if !c.IsDir() {
			continue
		}
		files, err := os.ReadDir(filepath.Join(l.Dir, c.Name()))
		if err != nil {
			return err
		}
		for _, f := range files {
			if day, ok := strings.CutSuffix(f.Name(), ".txt"); ok && day < cutoff {
				if err := os.Remove(filepath.Join(l.Dir, c.Name(), f.Name())); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
