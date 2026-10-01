package platform

import (
	"os"
	"path/filepath"
)

// Volume is a candidate recordings folder offered at first-run setup.
type Volume struct {
	Path    string `json:"path"`  // folder camorage creates and records into
	Label   string `json:"label"` // "SD card 1234-ABCD" or "Phone storage"
	FreeMB  uint64 `json:"freeMB"`
	TotalMB uint64 `json:"totalMB"`
	Ready   bool   `json:"ready"` // false: an SD card Termux has no folder on yet (termux-setup-storage)
}

// Volumes lists places for recordings: each SD card's Termux app folder (the only SD location
// Termux can write without root), then Termux's home directory. Android creates an SD card's
// Termux folder only once Termux asks for storage access (termux-setup-storage); until then the
// card is listed as not ready, so setup can say what to do.
func Volumes() []Volume {
	home, _ := os.UserHomeDir()
	return VolumesUnder("/storage", home)
}

// VolumesUnder is Volumes with the storage root and home made explicit (for tests).
func VolumesUnder(storage, home string) []Volume {
	var out []Volume
	entries, _ := os.ReadDir(storage)
	for _, e := range entries {
		if e.Name() == "emulated" || e.Name() == "self" {
			continue
		}
		card := filepath.Join(storage, e.Name())
		if st, err := os.Stat(card); err != nil || !st.IsDir() {
			continue
		}
		dir := filepath.Join(card, "Android", "data", "com.termux", "files")
		if st, err := os.Stat(dir); err == nil && st.IsDir() {
			out = append(out, volume(filepath.Join(dir, "camorage-rec"), dir, "SD card "+e.Name(), true))
		} else {
			out = append(out, volume(filepath.Join(dir, "camorage-rec"), card, "SD card "+e.Name(), false))
		}
	}
	if home != "" {
		out = append(out, volume(filepath.Join(home, "camorage-rec"), home, "Phone storage", true))
	}
	return out
}

func volume(path, statDir, label string, ready bool) Volume {
	v := Volume{Path: path, Label: label, Ready: ready}
	if free, total, err := Disk(statDir); err == nil {
		v.FreeMB, v.TotalMB = free>>20, total>>20
	}
	return v
}
