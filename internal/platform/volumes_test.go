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
	if len(got) != 3 {
		t.Fatalf("got %+v", got)
	}
	if got[0].Path != filepath.Join(sd, "camorage-rec") || got[0].Label != "SD card 1234-ABCD" || got[0].TotalMB == 0 || !got[0].Ready {
		t.Fatalf("SD volume = %+v", got[0])
	}
	// offered so setup can say how to make it usable (termux-setup-storage creates the folder)
	notYet := filepath.Join(storage, "ABCD-0000", "Android", "data", "com.termux", "files", "camorage-rec")
	if got[1].Path != notYet || got[1].Label != "SD card ABCD-0000" || got[1].TotalMB == 0 || got[1].Ready {
		t.Fatalf("SD card without a Termux folder = %+v", got[1])
	}
	if got[2].Path != filepath.Join(home, "camorage-rec") || got[2].Label != "Phone storage" || !got[2].Ready {
		t.Fatalf("home volume = %+v", got[2])
	}
}

func TestVolumesUnderMissingStorage(t *testing.T) {
	if got := VolumesUnder(filepath.Join(t.TempDir(), "nope"), ""); len(got) != 0 {
		t.Fatalf("got %+v", got)
	}
}
