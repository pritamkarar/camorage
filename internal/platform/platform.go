// Package platform isolates Android/Termux specifics: timezone, wake lock and health readings.
package platform

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	_ "time/tzdata" // Android keeps zoneinfo where Go does not look; embed it
)

// Getprop reads an Android system property; "" when not on Android.
func Getprop(name string) string {
	out, err := exec.Command("/system/bin/getprop", name).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// ZoneFrom loads an IANA zone name, falling back to time.Local.
func ZoneFrom(name string) *time.Location {
	if name != "" {
		if loc, err := time.LoadLocation(name); err == nil {
			return loc
		}
	}
	return time.Local
}

// LocalZone is the phone's configured timezone. Go binaries on Android otherwise run in UTC (M0).
func LocalZone() *time.Location { return ZoneFrom(Getprop("persist.sys.timezone")) }

// WakeLock keeps Termux's CPU running with the screen off; a no-op outside Termux.
func WakeLock() {
	if p, err := exec.LookPath("termux-wake-lock"); err == nil {
		_ = exec.Command(p).Run()
	}
}

type Health struct {
	BatteryPct   int     `json:"batteryPct"`   // -1 if unknown
	BatteryTempC float64 `json:"batteryTempC"` // 0 if unknown
	Charging     string  `json:"charging"`     // "Charging", "Full", "Discharging", …
	MemFreeMB    int     `json:"memFreeMB"`    // MemFree + Cached (kernel 3.10 has no MemAvailable); -1 if unknown
	DiskFreeMB   uint64  `json:"diskFreeMB"`
	DiskTotalMB  uint64  `json:"diskTotalMB"`
	DiskErr      string  `json:"diskErr,omitempty"`
}

// ReadHealth reads the battery from sysfsBattery (/sys/class/power_supply/battery), memory from
// meminfo (/proc/meminfo) and free space of diskPath (the recordings dir). It never fails.
func ReadHealth(sysfsBattery, meminfo, diskPath string) Health {
	h := Health{BatteryPct: -1, MemFreeMB: -1}
	if v, err := readInt(filepath.Join(sysfsBattery, "capacity")); err == nil {
		h.BatteryPct = v
	}
	if v, err := readInt(filepath.Join(sysfsBattery, "temp")); err == nil {
		h.BatteryTempC = float64(v) / 10
	}
	if b, err := os.ReadFile(filepath.Join(sysfsBattery, "status")); err == nil {
		h.Charging = strings.TrimSpace(string(b))
	}
	if v, ok := memFreeKB(meminfo); ok {
		h.MemFreeMB = v / 1024
	}
	if diskPath != "" {
		free, total, err := Disk(diskPath)
		if err != nil {
			h.DiskErr = err.Error()
		} else {
			h.DiskFreeMB, h.DiskTotalMB = free>>20, total>>20
		}
	}
	return h
}

// Disk returns free (usable without root) and total bytes of the filesystem holding path.
func Disk(path string) (free, total uint64, err error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, 0, err
	}
	return uint64(st.Bavail) * uint64(st.Bsize), uint64(st.Blocks) * uint64(st.Bsize), nil
}

func readInt(path string) (int, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(b)))
}

func memFreeKB(meminfo string) (int, bool) {
	f, err := os.Open(meminfo)
	if err != nil {
		return 0, false
	}
	defer f.Close()
	kb, found := 0, false
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) >= 2 && (fields[0] == "MemFree:" || fields[0] == "Cached:") {
			if v, err := strconv.Atoi(fields[1]); err == nil {
				kb += v
				found = true
			}
		}
	}
	return kb, found
}
