// Package supervisor runs child processes (MediaMTX now; ffmpeg, cloudflared, tailscaled later)
// and restarts them with exponential backoff until they are stopped.
package supervisor

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

type Spec struct {
	Name   string // unique; also names <logDir>/<Name>.log and <Name>.pid
	Path   string // executable (looked up in PATH if it has no slash)
	Args   []string
	Env    []string        // added to the parent's environment
	Stdout func(io.Reader) // if set, gets the child's stdout on every run (EOF when it exits); stderr still goes to the log
}

type Status struct {
	Name     string    `json:"name"`
	Running  bool      `json:"running"`
	PID      int       `json:"pid"`
	Restarts int       `json:"restarts"`
	LastExit string    `json:"lastExit,omitempty"`
	Since    time.Time `json:"since"`
}

const maxLogBytes = 5 << 20

var errStopped = errors.New("stopped before start")

type Supervisor struct {
	logDir           string
	minWait, maxWait time.Duration
	opMu             sync.Mutex // serialises Start/Stop so racing Starts cannot leave two children
	mu               sync.Mutex // guards procs
	procs            map[string]*proc
}

type proc struct {
	spec Spec
	stop chan struct{}
	done chan struct{}
	mu   sync.Mutex
	cmd  *exec.Cmd
	st   Status
}

// New restarts crashed children after 1 s, doubling up to 60 s (back to 1 s after a minute of uptime).
func New(logDir string) *Supervisor { return NewWithBackoff(logDir, time.Second, time.Minute) }

func NewWithBackoff(logDir string, minWait, maxWait time.Duration) *Supervisor {
	return &Supervisor{logDir: logDir, minWait: minWait, maxWait: maxWait, procs: map[string]*proc{}}
}

// Start runs spec until stopped, replacing any running child with the same name.
func (s *Supervisor) Start(spec Spec) {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	s.stop(spec.Name)
	killOrphan(s.pidFile(spec.Name), spec.Path)
	p := &proc{spec: spec, stop: make(chan struct{}), done: make(chan struct{}), st: Status{Name: spec.Name}}
	s.mu.Lock()
	s.procs[spec.Name] = p
	s.mu.Unlock()
	go s.run(p)
}

// Stop sends SIGTERM, waits up to 5 s, then SIGKILLs. Unknown names are ignored.
func (s *Supervisor) Stop(name string) {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	s.stop(name)
}

func (s *Supervisor) StopAll() {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	s.mu.Lock()
	names := make([]string, 0, len(s.procs))
	for n := range s.procs {
		names = append(names, n)
	}
	s.mu.Unlock()
	for _, n := range names {
		s.stop(n)
	}
}

func (s *Supervisor) Status() []Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Status, 0, len(s.procs))
	for _, p := range s.procs {
		p.mu.Lock()
		out = append(out, p.st)
		p.mu.Unlock()
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (s *Supervisor) stop(name string) {
	s.mu.Lock()
	p := s.procs[name]
	delete(s.procs, name)
	s.mu.Unlock()
	if p == nil {
		return
	}
	close(p.stop)
	p.signal(syscall.SIGTERM)
	select {
	case <-p.done:
	case <-time.After(5 * time.Second):
		p.signal(syscall.SIGKILL)
		<-p.done
	}
	os.Remove(s.pidFile(name))
}

func (s *Supervisor) pidFile(name string) string { return filepath.Join(s.logDir, name+".pid") }

func (s *Supervisor) run(p *proc) {
	defer close(p.done)
	wait := s.minWait
	for {
		select {
		case <-p.stop:
			return
		default:
		}
		started := time.Now()
		err := s.startOnce(p)
		if err == nil {
			err = p.cmd.Wait()
		}
		p.mu.Lock()
		p.st.Running, p.st.PID = false, 0
		if err != nil {
			p.st.LastExit = err.Error()
		} else {
			p.st.LastExit = "exit status 0"
		}
		p.mu.Unlock()
		if time.Since(started) >= s.maxWait { // it ran fine for a while: restart fast
			wait = s.minWait
		}
		select {
		case <-p.stop:
			return
		case <-time.After(wait):
		}
		p.mu.Lock()
		p.st.Restarts++
		p.mu.Unlock()
		if wait *= 2; wait > s.maxWait {
			wait = s.maxWait
		}
	}
}

func (s *Supervisor) startOnce(p *proc) error {
	if err := os.MkdirAll(s.logDir, 0o700); err != nil {
		return err
	}
	logPath := filepath.Join(s.logDir, p.spec.Name+".log")
	if st, err := os.Stat(logPath); err == nil && st.Size() > maxLogBytes {
		_ = os.Rename(logPath, logPath+".1")
	}
	logf, err := os.OpenFile(logPath, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer logf.Close() // the child keeps its own descriptor
	cmd := exec.Command(p.spec.Path, p.spec.Args...)
	cmd.Env = append(os.Environ(), p.spec.Env...)
	cmd.Stdout, cmd.Stderr = logf, logf
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} // its own process group: see signal()

	var out *os.File // our end of the stdout pipe, for spec.Stdout
	if p.spec.Stdout != nil {
		r, w, err := os.Pipe()
		if err != nil {
			return err
		}
		defer w.Close() // the child keeps its own copy, so the consumer sees EOF when the child exits
		cmd.Stdout, out = w, r
	}
	// Check for Stop and start the child under p.mu, which signal() also takes: otherwise a Stop
	// landing mid-startup sends its SIGTERM to nothing and then waits 5 s for the SIGKILL fallback.
	p.mu.Lock()
	defer p.mu.Unlock()
	select {
	case <-p.stop:
		if out != nil {
			out.Close()
		}
		return errStopped
	default:
	}
	if err := cmd.Start(); err != nil {
		if out != nil {
			out.Close()
		}
		fmt.Fprintf(logf, "supervisor: start %s: %v\n", p.spec.Path, err)
		return err
	}
	if out != nil {
		go func() {
			p.spec.Stdout(out)
			out.Close()
		}()
	}
	_ = os.WriteFile(s.pidFile(p.spec.Name), []byte(strconv.Itoa(cmd.Process.Pid)), 0o600)
	p.cmd = cmd
	p.st.Running, p.st.PID, p.st.Since = true, cmd.Process.Pid, time.Now()
	return nil
}

// signal signals the child's whole process group. proot (the Android wrapper of cloudflared and
// tailscaled) ignores SIGTERM, and when killed it leaves the program it runs behind (M1c).
func (p *proc) signal(sig syscall.Signal) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cmd != nil && p.cmd.Process != nil && p.st.Running {
		_ = syscall.Kill(-p.cmd.Process.Pid, sig)
	}
}

// killOrphan kills a child left by a previous camorage run (e.g. after kill -9) that would still hold
// its ports. It only acts when the PID's argv[0] has the same base name as exe, so a reused PID is safe.
func killOrphan(pidFile, exe string) {
	b, err := os.ReadFile(pidFile)
	if err != nil {
		return
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || pid <= 1 {
		return
	}
	cmdline, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	if err != nil {
		return
	}
	argv0, _, _ := strings.Cut(string(cmdline), "\x00")
	if filepath.Base(argv0) != filepath.Base(exe) {
		return
	}
	killGroup(pid, syscall.SIGTERM)
	for i := 0; i < 50; i++ {
		if syscall.Kill(pid, 0) != nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	killGroup(pid, syscall.SIGKILL)
}

// killGroup signals pid's process group, or pid alone when it leads none (a child left by a
// camorage from before children got their own groups).
func killGroup(pid int, sig syscall.Signal) {
	if syscall.Kill(-pid, sig) != nil {
		_ = syscall.Kill(pid, sig)
	}
}
