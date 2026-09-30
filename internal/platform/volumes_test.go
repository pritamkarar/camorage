package platform

import (
	"os"
	"path/filepath"
	"testing"
)

func TestVolumesUnder(t *testing.T) {
	root := t.TempDir()
	storage := filepath.Join(root, "storage")
	sd := filepath.Join(storage, "1234-ABCD", "Android", "data", "com.termux", "files")
	os.MkdirAll(sd, 0o755)
	os.MkdirAll(filepath.Join(storage, "emulated", "0", "Android", "data", "com.termux", "files"), 0o755)
	os.MkdirAll(filepath.Join(storage, "ABCD-0000"), 0o755) // an SD card Termux has no folder on
	home := filepath.Join(root, "home")
	os.MkdirAll(home, 0o755)

	got := VolumesUnder(storage, home)
	if len(got) != 2 {
		t.Fatalf("got %+v", got)
	}
	if got[0].Path != filepath.Join(sd, "camorage-rec") || got[0].Label != "SD card 1234-ABCD" || got[0].TotalMB == 0 {
		t.Fatalf("SD volume = %+v", got[0])
	}
	if got[1].Path != filepath.Join(home, "camorage-rec") || got[1].Label != "Phone storage" {
		t.Fatalf("home volume = %+v", got[1])
	}
}

func TestVolumesUnderMissingStorage(t *testing.T) {
	if got := VolumesUnder(filepath.Join(t.TempDir(), "nope"), ""); len(got) != 0 {
		t.Fatalf("got %+v", got)
	}
}
