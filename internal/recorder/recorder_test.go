package recorder

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"camorage/internal/config"
)

type fakeMTX struct {
	record  map[string]bool
	errOn   string
	patches []string
}

func (f *fakeMTX) Record(_ context.Context, n string) (bool, error) {
	if n == f.errOn {
		return false, errors.New("connection refused")
	}
	return f.record[n], nil
}

func (f *fakeMTX) SetRecord(_ context.Context, n string, on bool) error {
	f.patches = append(f.patches, fmt.Sprintf("%s=%v", n, on))
	f.record[n] = on
	return nil
}

var monday10 = time.Date(2026, 9, 28, 10, 0, 0, 0, time.FixedZone("IST", 19800))

func TestDesired(t *testing.T) {
	cams := []config.Camera{
		{ID: "always", Enabled: true},
		{ID: "off", Enabled: false},
		{ID: "nights", Enabled: true, Schedule: []config.Window{{Days: []int{1, 2, 3, 4, 5, 6, 7}, Start: "20:00", End: "06:00"}}},
	}
	got := Desired(cams, monday10)
	if len(got) != 2 || !got["always"] || got["nights"] {
		t.Fatalf("got %v", got)
	}
}

func TestReconcileOnlyPatchesChanges(t *testing.T) {
	f := &fakeMTX{record: map[string]bool{"same": true, "flip": true}, errOn: "down"}
	cams := []config.Camera{
		{ID: "same", Enabled: true},
		{ID: "flip", Enabled: true, Schedule: []config.Window{{Days: []int{7}, Start: "01:00", End: "02:00"}}},
		{ID: "down", Enabled: true},
	}
	var logs []string
	Reconcile(context.Background(), f, cams, monday10, func(format string, a ...any) { logs = append(logs, fmt.Sprintf(format, a...)) })
	if len(f.patches) != 1 || f.patches[0] != "flip=false" {
		t.Fatalf("patches = %v", f.patches)
	}
	if !strings.Contains(strings.Join(logs, "\n"), "down") {
		t.Fatalf("error for camera 'down' not logged: %v", logs)
	}
}
