package schedule

import (
	"testing"
	"time"

	"camorage/internal/config"
)

var ist = time.FixedZone("IST", 5*3600+1800)

// 2026-09-28 is a Monday.
func at(day, hh, mm int) time.Time { return time.Date(2026, 9, 28+day, hh, mm, 0, 0, ist) }

func TestActive(t *testing.T) {
	weekdays9to5 := []config.Window{{Days: []int{1, 2, 3, 4, 5}, Start: "09:00", End: "17:00"}}
	mondayNight := []config.Window{{Days: []int{1}, Start: "20:00", End: "08:00"}}
	sundayNight := []config.Window{{Days: []int{7}, Start: "22:00", End: "02:00"}}
	allWednesday := []config.Window{{Days: []int{3}, Start: "00:00", End: "00:00"}}
	cases := []struct {
		name string
		ws   []config.Window
		t    time.Time
		want bool
	}{
		{"no windows = always", nil, at(0, 3, 0), true},
		{"weekday inside", weekdays9to5, at(0, 10, 0), true},
		{"weekday end is exclusive", weekdays9to5, at(0, 17, 0), false},
		{"saturday", weekdays9to5, at(5, 10, 0), false},
		{"overnight evening part", mondayNight, at(0, 21, 0), true},
		{"overnight morning part next day", mondayNight, at(1, 7, 59), true},
		{"overnight ends at end time", mondayNight, at(1, 8, 0), false},
		{"monday early morning belongs to sunday's window", mondayNight, at(0, 7, 0), false},
		{"sunday window wraps into monday", sundayNight, at(7, 1, 0), true},
		{"start == end is the whole start day", allWednesday, at(2, 13, 0), true},
		{"start == end does not spill into next day", allWednesday, at(3, 0, 0), false},
	}
	for _, c := range cases {
		if got := Active(c.ws, c.t); got != c.want {
			t.Errorf("%s: Active = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestActiveUsesTimesOwnLocation(t *testing.T) {
	ws := []config.Window{{Days: []int{1}, Start: "09:00", End: "10:00"}}
	moment := at(0, 9, 30) // Monday 09:30 IST == 04:00 UTC
	if !Active(ws, moment) {
		t.Fatal("09:30 IST should be inside 09:00-10:00")
	}
	if Active(ws, moment.In(time.UTC)) {
		t.Fatal("04:00 UTC must not be treated as 09:30: callers pass local time")
	}
}
