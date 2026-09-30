package recorder

import "time"

// Usage is what recordings take on the phone.
type Usage struct {
	Bytes  uint64 // all segments
	PerDay uint64 // recorded per day lately (the last 24 h, scaled up if there is less history)
}

// MeasureUsage adds up the segments in recDir.
func MeasureUsage(recDir string, now time.Time) (Usage, error) {
	segs, err := ListSegments(recDir)
	if err != nil {
		return Usage{}, err
	}
	var u Usage
	var recent uint64
	oldest := now
	for _, s := range segs {
		u.Bytes += uint64(s.Size)
		if s.Start.After(now.Add(-24 * time.Hour)) {
			recent += uint64(s.Size)
			if s.Start.Before(oldest) {
				oldest = s.Start
			}
		}
	}
	covered := min(max(now.Sub(oldest), time.Hour), 24*time.Hour)
	u.PerDay = uint64(float64(recent) * float64(24*time.Hour) / float64(covered))
	return u, nil
}

// DaysFit is how many days of recordings the volume holds at the recent rate: what recordings use
// now plus the free space above the floor the janitor keeps.
func DaysFit(u Usage, free, total uint64) float64 {
	usable, floor := u.Bytes+free, Floor(total)
	if u.PerDay == 0 || usable <= floor {
		return 0
	}
	return float64(usable-floor) / float64(u.PerDay)
}
