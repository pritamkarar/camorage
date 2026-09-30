package supervisor

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func waitRunning(t *testing.T, s *Supervisor, name string) int {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		for _, st := range s.Status() {
			if st.Name == name && st.Running && st.PID > 0 {
				return st.PID
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("%s never running", name)
	return 0
}

// countProcs counts live processes whose command line contains arg.
func countProcs(arg string) int {
	n := 0
	entries, _ := os.ReadDir("/proc")
	for _, e := range entries {
		if _, err := strconv.Atoi(e.Name()); err != nil {
			continue
		}
		b, err := os.ReadFile("/proc/" + e.Name() + "/cmdline")
		if err == nil && strings.Contains(string(b), arg) {
			n++
		}
	}
	return n
}

func TestRestartsCrashedChild(t *testing.T) {
	dir := t.TempDir()
	count := filepath.Join(dir, "count")
	s := NewWithBackoff(dir, 10*time.Millisecond, 40*time.Millisecond)
	defer s.StopAll()
	s.Start(Spec{Name: "crasher", Path: "sh", Args: []string{"-c", "echo x >> " + count + "; exit 1"}})
	time.Sleep(400 * time.Millisecond)
	b, _ := os.ReadFile(count)
	if n := strings.Count(string(b), "x"); n < 3 {
		t.Fatalf("ran %d times, want >= 3", n)
	}
	if st := s.Status(); len(st) != 1 || st[0].Restarts < 2 || st[0].LastExit == "" {
		t.Fatalf("status %+v", st)
	}
}

func TestStopTerminatesChild(t *testing.T) {
	s := New(t.TempDir())
	s.Start(Spec{Name: "sleeper", Path: "sleep", Args: []string{"30"}})
	pid := waitRunning(t, s, "sleeper")
	s.Stop("sleeper")
	if syscall.Kill(pid, 0) == nil {
		t.Fatal("child still alive after Stop")
	}
	if len(s.Status()) != 0 {
		t.Fatal("stopped child still listed")
	}
}

func TestKillsOrphanFromPreviousRun(t *testing.T) {
	dir := t.TempDir()
	orphan := exec.Command("sleep", "30")
	if err := orphan.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan struct{})
	go func() { orphan.Wait(); close(exited) }()
	os.WriteFile(filepath.Join(dir, "sleeper.pid"), []byte(strconv.Itoa(orphan.Process.Pid)), 0o600)
	s := New(dir)
	defer s.StopAll()
	s.Start(Spec{Name: "sleeper", Path: "sleep", Args: []string{"30"}})
	select {
	case <-exited:
	case <-time.After(3 * time.Second):
		orphan.Process.Kill()
		t.Fatal("orphan from the previous run was not killed")
	}
}

func TestIgnoresPidFileOfUnrelatedProcess(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "sleeper.pid"), []byte(strconv.Itoa(os.Getpid())), 0o600)
	s := New(dir)
	defer s.StopAll()
	s.Start(Spec{Name: "sleeper", Path: "sleep", Args: []string{"30"}})
	waitRunning(t, s, "sleeper") // reaching here means the test process itself was not signalled
}

func TestConcurrentStartKeepsOneChild(t *testing.T) {
	s := New(t.TempDir())
	defer s.StopAll()
	spec := Spec{Name: "one", Path: "sleep", Args: []string{"30.4242"}}
	var wg sync.WaitGroup
	began := time.Now()
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); s.Start(spec) }()
	}
	wg.Wait()
	// a Stop racing a child's startup must still deliver SIGTERM, not fall through to the 5 s SIGKILL
	if d := time.Since(began); d > 3*time.Second {
		t.Fatalf("5 back-to-back Starts took %v", d)
	}
	waitRunning(t, s, "one")
	if n := countProcs("30.4242"); n != 1 {
		t.Fatalf("%d children running, want 1", n)
	}
}

func TestStopRightAfterStartIsPrompt(t *testing.T) {
	s := New(t.TempDir())
	defer s.StopAll()
	began := time.Now()
	for i := 0; i < 30; i++ {
		s.Start(Spec{Name: "blink", Path: "sleep", Args: []string{"30.5151"}})
		time.Sleep(time.Duration(i) * 100 * time.Microsecond) // sweep 0–3 ms across the child's startup
		s.Stop("blink")
	}
	if d := time.Since(began); d > 3*time.Second {
		t.Fatalf("30 Start/Stop cycles took %v: a Stop during startup missed its SIGTERM", d)
	}
	if n := countProcs("30.5151"); n != 0 {
		t.Fatalf("%d children left running", n)
	}
}

func TestCapturesOutputToLog(t *testing.T) {
	dir := t.TempDir()
	s := NewWithBackoff(dir, time.Hour, time.Hour) // runs once
	defer s.StopAll()
	s.Start(Spec{Name: "echo", Path: "sh", Args: []string{"-c", "echo hello-from-child"}})
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		b, _ := os.ReadFile(filepath.Join(dir, "echo.log"))
		if strings.Contains(string(b), "hello-from-child") {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("child output not captured in echo.log")
}

// prootLike behaves like proot: it ignores SIGTERM while its child runs. The child's PID goes to file.
func prootLike(file string) Spec {
	return Spec{Name: "tracer", Path: "sh", Args: []string{"-c", "sleep 30 & echo $! > " + file + "; trap '' TERM; wait"}}
}

func readPID(t *testing.T, file string) int {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(file); err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil {
				return pid
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("grandchild never started")
	return 0
}

func TestStopReachesGrandchildren(t *testing.T) {
	dir := t.TempDir()
	gc := filepath.Join(dir, "gc.pid")
	s := New(dir)
	s.Start(prootLike(gc))
	pid := readPID(t, gc)
	began := time.Now()
	s.Stop("tracer")
	if d := time.Since(began); d > 3*time.Second {
		t.Fatalf("Stop took %v: SIGTERM missed the child's process group", d)
	}
	if syscall.Kill(pid, 0) == nil {
		syscall.Kill(pid, syscall.SIGKILL)
		t.Fatal("grandchild survived Stop")
	}
}

func TestKillsOrphanGroupFromPreviousRun(t *testing.T) {
	dir := t.TempDir()
	gc := filepath.Join(dir, "gc.pid")
	spec := prootLike(gc)
	orphan := exec.Command(spec.Path, spec.Args...)
	orphan.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} // as the supervisor starts children
	if err := orphan.Start(); err != nil {
		t.Fatal(err)
	}
	go orphan.Wait()
	pid := readPID(t, gc)
	os.WriteFile(filepath.Join(dir, "tracer.pid"), []byte(strconv.Itoa(orphan.Process.Pid)), 0o600)
	os.Remove(gc)
	s := New(dir)
	defer s.StopAll()
	s.Start(spec)
	readPID(t, gc) // the new child is up, so the orphan was dealt with before it started
	if syscall.Kill(pid, 0) == nil {
		syscall.Kill(-orphan.Process.Pid, syscall.SIGKILL)
		t.Fatal("the orphan's child survived: a second tunnel would be running")
	}
}

func TestStdoutGoesToTheConsumer(t *testing.T) {
	dir := t.TempDir()
	got := make(chan string, 1)
	s := NewWithBackoff(dir, time.Hour, time.Hour) // runs once
	defer s.StopAll()
	s.Start(Spec{Name: "talker", Path: "sh", Args: []string{"-c", "printf frame-bytes; echo oops >&2"},
		Stdout: func(r io.Reader) {
			b, _ := io.ReadAll(r)
			got <- string(b)
		}})
	select {
	case out := <-got:
		if out != "frame-bytes" {
			t.Fatalf("consumer got %q", out)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the consumer never saw the end of the child's output")
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if b, _ := os.ReadFile(filepath.Join(dir, "talker.log")); strings.Contains(string(b), "oops") {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("stderr no longer reaches the log")
}
