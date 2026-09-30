// Package motion detects motion on the phone: an ffmpeg keyframe reader per camera, a block-diff
// detector, and events kept as JSON lines (spec §5.2).
package motion

// Frames are the reader's output: 64×36 gray pixels, one per keyframe.
const (
	W         = 64
	H         = 36
	FrameSize = W * H
	Cols      = 16 // the detection grid: 4×4-pixel blocks
	Rows      = 9
	Blocks    = Cols * Rows
)

// thresholds per sensitivity: a block changed when its mean |Δ| exceeds T; motion needs K changed blocks.
var thresholds = map[string]struct{ T, K int }{
	"low":    {25, 6},
	"medium": {15, 3},
	"high":   {8, 2},
}

// Result compares two consecutive frames.
type Result struct {
	Motion  bool
	Changed int // changed blocks outside the ignore mask
}

// Detect compares frame with prev (both FrameSize bytes). More than half of the watched blocks
// changing at once is a lighting change (lights, the camera's night mode), not motion.
func Detect(prev, frame []byte, sensitivity string, ignore []bool) Result {
	th, ok := thresholds[sensitivity]
	if !ok {
		th = thresholds["medium"]
	}
	changed, watched := 0, 0
	for b := 0; b < Blocks; b++ {
		if len(ignore) == Blocks && ignore[b] {
			continue
		}
		watched++
		bx, by := (b%Cols)*4, (b/Cols)*4
		sum := 0
		for y := by; y < by+4; y++ {
			for x := bx; x < bx+4; x++ {
				d := int(frame[y*W+x]) - int(prev[y*W+x])
				if d < 0 {
					d = -d
				}
				sum += d
			}
		}
		if sum > th.T*16 { // mean over the block's 16 pixels > T
			changed++
		}
	}
	lighting := changed*2 > watched
	return Result{Motion: changed >= th.K && !lighting, Changed: changed}
}
