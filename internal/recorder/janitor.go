package recorder

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"camorage/internal/config"
	"camorage/internal/motion"
)

// Segment is one MediaMTX recording file.
type Segment struct {
	Cam   string
	Path  string
	Start time.Time // UTC, from the file name
	Size  int64
}

const segLayout = "2006-01-02_15-04-05"

// ParseSegmentName parses MediaMTX's "%Y-%m-%d_%H-%M-%S-%f.mp4" (written in UTC on Android).
func ParseSegmentName(name string) (time.Time, bool) {
	if !strings.HasSuffix(name, ".mp4") || len(name) < len(segLayout) {
		return time.Time{}, false
	}
	t, err := time.ParseInLocation(segLayout, name[:len(segLayout)], time.UTC)
	return t, err == nil
}

// ListSegments returns every segment under recDir/<cam>/, oldest first.
func ListSegments(recDir string) ([]Segment, error) {
	dirs, err := os.ReadDir(recDir)
	if err != nil {
		return nil, err
	}
	var out []Segment
	for _, d := range dirs {
		if !d.IsDir() || strings.HasPrefix(d.Name(), ".") {
			continue
		}
		files, err := os.ReadDir(filepath.Join(recDir, d.Name()))
		if err != nil {
			return nil, err
		}
		for _, f := range files {
			start, ok := ParseSegmentName(f.Name())
			if !ok || f.IsDir() {
				continue
			}
			info, err := f.Info()
			if err != nil {
				continue // deleted meanwhile
			}
			out = append(out, Segment{Cam: d.Name(), Path: filepath.Join(recDir, d.Name(), f.Name()), Start: start, Size: info.Size()})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Start.Before(out[j].Start) })
	return out, nil
}

// DiskFunc reports free and total bytes of the filesystem holding path.
type DiskFunc func(path string) (free, total uint64, err error)

const floorMin = 500 << 20

// Floor is the free space the janitor keeps: max(500 MB, 5 % of the volume).
func Floor(total uint64) uint64 {
	if f := total / 20; f > floorMin {
		return f
	}
	return floorMin
}

// Janitor deletes segments older than each camera's localDays (DefaultLocalDays for cameras that
// no longer exist). For cameras in motion mode it also deletes footage no event keeps once it is
// over an hour old, counting only what was recorded since the switch to motion mode. When free space
// is under Floor it deletes the oldest not-kept motion footage first, then the oldest of the rest.
// The newest segment of each camera is never deleted: MediaMTX may still be writing it. Recording
// never stops for space. events may be nil (no motion anywhere).
// ponytail: re-reads a motion camera's events for all its footage every run; cache them if a year
// of footage ever makes that slow.
func Janitor(recDir string, cams []config.Camera, now time.Time, disk DiskFunc, events EventsFunc) ([]string, error) {
	segs, err := ListSegments(recDir)
	if err != nil {
		return nil, err
	}
	byID := map[string]config.Camera{}
	for _, c := range cams {
		byID[c.ID] = c
	}
	newest := map[string]string{}
	next := map[string]time.Time{} // segment path → the start of that camera's next segment
	last := map[string]Segment{}
	for _, s := range segs { // oldest first
		if p, ok := last[s.Cam]; ok {
			next[p.Path] = s.Start
		}
		last[s.Cam] = s
		newest[s.Cam] = s.Path
	}
	var problems []error
	evs := map[string][]motion.Event{}
	unknown := map[string]bool{} // cameras whose events could not be read: their footage is kept
	if events != nil && len(segs) > 0 {
		for _, c := range cams {
			if c.Mode != "motion" {
				continue
			}
			e, err := events(c.ID, segs[0].Start.Add(-24*time.Hour), now)
			if err != nil {
				unknown[c.ID] = true
				problems = append(problems, fmt.Errorf("%s: keeping its footage: %w", c.ID, err))
			}
			evs[c.ID] = e
		}
	}
	var deleted []string
	var spare, keep []Segment // spare: motion footage no event keeps, the first to go when space runs low
	for _, s := range segs {
		c, known := byID[s.Cam]
		d := c.LocalDays
		if !known || d <= 0 {
			d = config.DefaultLocalDays
		}
		if newest[s.Cam] == s.Path {
			keep = append(keep, s)
			continue
		}
		end := s.Start.Add(segCap)
		if n, ok := next[s.Path]; ok && n.Before(end) {
			end = n
		}
		unkept := known && c.Mode == "motion" && !unknown[s.Cam] &&
			(c.MotionSince == nil || !s.Start.Before(*c.MotionSince)) &&
			!Kept(s.Start, end, evs[s.Cam], time.Duration(c.Motion.PreRollSec)*time.Second, time.Duration(c.Motion.PostRollSec)*time.Second)
		switch {
		case s.Start.Before(now.Add(-time.Duration(d) * 24 * time.Hour)), unkept && end.Before(now.Add(-recentBuffer)):
			if os.Remove(s.Path) == nil {
				deleted = append(deleted, s.Path)
			}
		case unkept:
			spare = append(spare, s)
		default:
			keep = append(keep, s)
		}
	}
	free, total, err := disk(recDir)
	if err != nil {
		return deleted, errors.Join(append(problems, err)...)
	}
	floor := Floor(total)
	for _, s := range append(spare, keep...) {
		if free >= floor {
			break
		}
		if newest[s.Cam] == s.Path {
			continue
		}
		if os.Remove(s.Path) == nil {
			deleted = append(deleted, s.Path)
			free += uint64(s.Size)
		}
	}
	if free < floor {
		problems = append(problems, fmt.Errorf("free space %d MB is still under the %d MB floor", free>>20, floor>>20))
	}
	return deleted, errors.Join(problems...)
}
