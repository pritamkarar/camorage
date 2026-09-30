// Package recorder turns camera settings into MediaMTX record state and enforces local retention.
package recorder

import (
	"context"
	"time"

	"camorage/internal/config"
	"camorage/internal/schedule"
)

// MTX is the part of the MediaMTX client the recorder needs.
type MTX interface {
	Record(ctx context.Context, name string) (bool, error)
	SetRecord(ctx context.Context, name string, on bool) error
}

// Desired is each enabled camera's record state at now (phone local time).
// Motion mode records like continuous mode; the janitor then deletes what no motion event keeps.
func Desired(cams []config.Camera, now time.Time) map[string]bool {
	out := map[string]bool{}
	for _, c := range cams {
		if c.Enabled {
			out[c.ID] = schedule.Active(c.Schedule, now)
		}
	}
	return out
}

// Reconcile makes MediaMTX's record flags match Desired, PATCHing only on change so paths are
// not reloaded every tick. MediaMTX being down (e.g. restarting) is logged and retried next tick.
func Reconcile(ctx context.Context, mtx MTX, cams []config.Camera, now time.Time, logf func(string, ...any)) {
	for id, want := range Desired(cams, now) {
		have, err := mtx.Record(ctx, id)
		if err != nil {
			logf("recorder: %s: read record flag: %v", id, err)
			continue
		}
		if have == want {
			continue
		}
		if err := mtx.SetRecord(ctx, id, want); err != nil {
			logf("recorder: %s: set record=%v: %v", id, want, err)
			continue
		}
		logf("recorder: %s: record=%v", id, want)
	}
}
