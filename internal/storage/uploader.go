package storage

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"camorage/internal/config"
	"camorage/internal/mediamtx"
	"camorage/internal/motion"
)

// UploadStatus is what the Storage page shows per camera.
type UploadStatus struct {
	Queued     int    `json:"queued"`               // clips due but not in the cloud yet
	LastUpload string `json:"lastUpload,omitempty"` // RFC 3339
	LastError  string `json:"lastError,omitempty"`
}

// Uploader copies due clips to each camera's cloud target, one at a time (spec §5.5), and applies
// cloud retention (§5.6).
type Uploader struct {
	Store  *config.Store
	Rclone Rclone
	Ledger *Ledger
	Spans  func(ctx context.Context, cam string, from, to time.Time) ([]mediamtx.Span, error)
	Events func(cam string, from, to time.Time) ([]motion.Event, error)
	Fetch  func(ctx context.Context, cam string, from time.Time, dur time.Duration, dst string) error
	Now    func() time.Time
	Zone   *time.Location

	mu      sync.Mutex
	status  map[string]UploadStatus
	retry   time.Time     // after a failure, runs before this do nothing
	backoff time.Duration // 1 min, doubling up to 30 min
}

const (
	firstBackoff = time.Minute
	maxBackoff   = 30 * time.Minute
	// ponytail: an outage longer than lookback leaves the older clips on the phone only; plan from
	// the oldest local footage instead if that ever matters.
	lookback   = 48 * time.Hour
	ledgerDays = 4 // lookback, plus the day before a clip's start (Ledger.Covered)
)

// Run uploads every due clip the ledger does not cover yet, oldest first, and stops at the first
// failure (the caller runs it again every minute; after a failure it backs off).
// ponytail: one back-off for all cameras, so a broken target delays the others' uploads too;
// make it per target if cameras ever copy to different ones.
func (u *Uploader) Run(ctx context.Context) error {
	now := u.Now()
	u.mu.Lock()
	waiting := now.Before(u.retry)
	u.mu.Unlock()
	if waiting {
		return nil
	}
	cfg := u.Store.Get()
	if cfg.RecDir == "" {
		return nil
	}
	targets := map[string]config.StorageTarget{}
	for _, t := range cfg.StorageTargets {
		targets[t.ID] = t
	}
	active := map[string]bool{}
	for _, cam := range cfg.Cameras {
		t, ok := targets[targetOf(cam)]
		if !ok {
			continue
		}
		active[cam.ID] = true
		todo, err := u.due(ctx, cam, now)
		if err != nil {
			return u.failed(cam.ID, len(todo), err)
		}
		u.set(cam.ID, func(s *UploadStatus) {
			if s.Queued = len(todo); len(todo) == 0 {
				s.LastError = "" // what failed is in the cloud now, or no longer due
			}
		})
		for i, c := range todo {
			if err := u.upload(ctx, cfg.RecDir, t, c); err != nil {
				return u.failed(cam.ID, len(todo)-i, err)
			}
			u.set(cam.ID, func(s *UploadStatus) {
				s.Queued, s.LastUpload, s.LastError = len(todo)-i-1, u.Now().Format(time.RFC3339), ""
			})
		}
	}
	u.mu.Lock()
	for cam := range u.status {
		if !active[cam] {
			delete(u.status, cam)
		}
	}
	u.backoff = 0
	u.mu.Unlock()
	return nil
}

func targetOf(cam config.Camera) string {
	if cam.Cloud == nil {
		return ""
	}
	return cam.Cloud.TargetID
}

// due is cam's clips that are due and not covered by the ledger, from the later of cloud.since
// and now − lookback.
func (u *Uploader) due(ctx context.Context, cam config.Camera, now time.Time) ([]Clip, error) {
	since := now.Add(-lookback)
	if cam.Cloud.Since != nil && cam.Cloud.Since.After(since) {
		since = *cam.Cloud.Since
	}
	spans, err := u.Spans(ctx, cam.ID, since, now)
	if err != nil {
		return nil, err
	}
	var evs []motion.Event
	if cam.Mode == "motion" {
		if evs, err = u.Events(cam.ID, since, now); err != nil {
			return nil, err
		}
	}
	var todo []Clip
	for _, c := range Due(cam, spans, evs, since, now, u.Zone) {
		if !u.Ledger.Covered(cam.ID, c.From, c.To, u.Zone) {
			todo = append(todo, c)
		}
	}
	return todo, nil
}

// upload cuts c from the recording into a temp file, copies it to the target and records it.
func (u *Uploader) upload(ctx context.Context, recDir string, t config.StorageTarget, c Clip) error {
	dir := filepath.Join(recDir, ".upload") // the janitor skips dot folders
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp := filepath.Join(dir, c.Cam+".mp4")
	defer os.Remove(tmp)
	if err := u.Fetch(ctx, c.Cam, c.From, c.To.Sub(c.From), tmp); err != nil {
		return fmt.Errorf("cutting %s: %w", c.Path, err)
	}
	if err := u.Rclone.CopyTo(ctx, tmp, Join(t.Remote, c.Path)); err != nil {
		return err
	}
	return u.Ledger.Add(c.Cam, c.Path)
}

func (u *Uploader) failed(cam string, queued int, err error) error {
	u.mu.Lock()
	u.backoff = min(max(u.backoff*2, firstBackoff), maxBackoff)
	u.retry = u.Now().Add(u.backoff)
	u.mu.Unlock()
	u.set(cam, func(s *UploadStatus) { s.Queued, s.LastError = queued, err.Error() })
	return fmt.Errorf("%s: %w", cam, err)
}

func (u *Uploader) set(cam string, f func(*UploadStatus)) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.status == nil {
		u.status = map[string]UploadStatus{}
	}
	s := u.status[cam]
	f(&s)
	u.status[cam] = s
}

// Status returns each cloud-copying camera's upload status.
func (u *Uploader) Status() map[string]UploadStatus {
	u.mu.Lock()
	defer u.mu.Unlock()
	out := make(map[string]UploadStatus, len(u.status))
	for k, v := range u.status {
		out[k] = v
	}
	return out
}

// Retention deletes each cloud-copying camera's clips older than its cloud days (spec §5.6), and
// ledger days the uploader no longer plans.
func (u *Uploader) Retention(ctx context.Context) error {
	cfg := u.Store.Get()
	targets := map[string]config.StorageTarget{}
	for _, t := range cfg.StorageTargets {
		targets[t.ID] = t
	}
	var errs []error
	for _, cam := range cfg.Cameras {
		t, ok := targets[targetOf(cam)]
		if !ok {
			continue
		}
		if err := u.Rclone.Prune(ctx, Join(t.Remote, "camorage/"+cam.ID), cam.Cloud.Days, t.Type == "drive"); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", cam.ID, err))
		}
	}
	return errors.Join(append(errs, u.Ledger.Prune(u.Now(), ledgerDays))...)
}
