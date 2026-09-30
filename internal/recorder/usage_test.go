package recorder

import (
	"testing"
	"time"
)

func TestUsageAndDaysFit(t *testing.T) {
	dir := t.TempDir()
	const mb = 1 << 20
	seg(t, dir, "gate", now.Add(-30*time.Hour), 100*mb) // older than a day: counts as used, not as the rate
	for i := 0; i < 6; i++ {                            // the last 6 hours: 60 MB per hour
		seg(t, dir, "gate", now.Add(-time.Duration(6-i)*time.Hour), 60*mb)
	}
	u, err := MeasureUsage(dir, now)
	if err != nil || u.Bytes != 460*mb || u.PerDay != 1440*mb {
		t.Fatalf("usage %+v %v", u, err)
	}
	// 20 GB volume: floor 1 GB; 460 MB used + 2 GB free − 1 GB floor = 1.46 GB at 1.44 GB/day
	if d := DaysFit(u, 2048*mb, 20480*mb); d < 1.03 || d > 1.05 {
		t.Fatalf("days fit %.3f", d)
	}
	if d := DaysFit(Usage{}, 2048*mb, 20480*mb); d != 0 {
		t.Fatalf("no recordings: %.3f", d)
	}
	if d := DaysFit(Usage{Bytes: mb, PerDay: mb}, 100*mb, 20480*mb); d != 0 {
		t.Fatalf("under the floor: %.3f", d)
	}
}
