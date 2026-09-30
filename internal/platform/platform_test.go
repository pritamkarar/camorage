package platform

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReadHealthFromFixtures(t *testing.T) {
	dir := t.TempDir()
	bat := filepath.Join(dir, "battery")
	os.MkdirAll(bat, 0o755)
	os.WriteFile(filepath.Join(bat, "capacity"), []byte("87\n"), 0o644)
	os.WriteFile(filepath.Join(bat, "temp"), []byte("333\n"), 0o644)
	os.WriteFile(filepath.Join(bat, "status"), []byte("Charging\n"), 0o644)
	mem := filepath.Join(dir, "meminfo")
	os.WriteFile(mem, []byte("MemTotal:  1942424 kB\nMemFree:   100000 kB\nBuffers:  78876 kB\nCached:   200000 kB\n"), 0o644)

	h := ReadHealth(bat, mem, dir)
	if h.BatteryPct != 87 || h.BatteryTempC != 33.3 || h.Charging != "Charging" {
		t.Fatalf("battery: %+v", h)
	}
	if h.MemFreeMB != 292 { // (100000+200000)/1024
		t.Fatalf("MemFreeMB = %d, want 292", h.MemFreeMB)
	}
	if h.DiskTotalMB == 0 || h.DiskErr != "" {
		t.Fatalf("disk: %+v", h)
	}
}

func TestReadHealthMissingPaths(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "gone")
	h := ReadHealth(missing, missing, missing)
	if h.BatteryPct != -1 || h.MemFreeMB != -1 || h.DiskErr == "" {
		t.Fatalf("want unknown battery/memory and a disk error, got %+v", h)
	}
}

func TestZoneFrom(t *testing.T) {
	loc := ZoneFrom("Asia/Kolkata")
	if _, off := time.Date(2026, 9, 30, 0, 0, 0, 0, loc).Zone(); off != 19800 {
		t.Fatalf("offset %d, want 19800", off)
	}
	if ZoneFrom("") != time.Local || ZoneFrom("Not/AZone") != time.Local {
		t.Fatal("unknown zone should fall back to time.Local")
	}
}
