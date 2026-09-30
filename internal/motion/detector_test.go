package motion

import (
	"bytes"
	"testing"
)

func frame(fill byte) []byte { return bytes.Repeat([]byte{fill}, FrameSize) }

// paint returns a copy of f with the 4×4-pixel blocks idx set to v.
func paint(f []byte, v byte, idx ...int) []byte {
	g := append([]byte(nil), f...)
	for _, b := range idx {
		bx, by := (b%Cols)*4, (b/Cols)*4
		for y := by; y < by+4; y++ {
			for x := bx; x < bx+4; x++ {
				g[y*W+x] = v
			}
		}
	}
	return g
}

func ignoring(idx ...int) []bool {
	m := make([]bool, Blocks)
	for _, i := range idx {
		m[i] = true
	}
	return m
}

func TestDetect(t *testing.T) {
	bg := frame(100)
	person := paint(bg, 200, 40, 41, 56, 57) // a 2×2-block shape
	faint := paint(bg, 110, 40, 41)          // mean |Δ| 10
	cases := []struct {
		name      string
		prev, cur []byte
		sens      string
		ignore    []bool
		motion    bool
		changed   int
	}{
		{"still", bg, bg, "medium", nil, false, 0},
		{"someone moves", bg, person, "medium", nil, true, 4},
		{"too small for low", bg, person, "low", nil, false, 4},
		{"lights on", bg, frame(160), "medium", nil, false, 144},
		{"ignored area", bg, person, "medium", ignoring(40, 41, 56, 57), false, 0},
		{"faint change seen by high", bg, faint, "high", nil, true, 2},
		{"faint change missed by medium", bg, faint, "medium", nil, false, 0},
		{"unknown sensitivity counts as medium", bg, person, "", nil, true, 4},
	}
	for _, c := range cases {
		r := Detect(c.prev, c.cur, c.sens, c.ignore)
		if r.Motion != c.motion || r.Changed != c.changed {
			t.Errorf("%s: got %+v, want motion=%v changed=%d", c.name, r, c.motion, c.changed)
		}
	}
}
