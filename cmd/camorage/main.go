// Command camorage is the camera recorder portal for an old Android phone (runs in Termux).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"camorage/internal/auth"
	"camorage/internal/config"
	"camorage/internal/mediamtx"
	"camorage/internal/motion"
	"camorage/internal/onvif"
	"camorage/internal/platform"
	"camorage/internal/recorder"
	"camorage/internal/storage"
	"camorage/internal/supervisor"
	"camorage/internal/tunnel"
	"camorage/internal/web"
)

// version is set by release builds (scripts/release.sh: -ldflags "-X main.version=<v>").
var version = "dev"

func main() {
	home, _ := os.UserHomeDir()
	exe, _ := os.Executable()
	binDir := filepath.Dir(exe) // mediamtx, cloudflared, tailscale and tailscaled live next to camorage
	dataDir := flag.String("data", filepath.Join(home, ".camorage"), "data directory")
	listen := flag.String("listen", ":8080", "HTTP listen address")
	mtxBin := flag.String("mediamtx", filepath.Join(binDir, "mediamtx"), "MediaMTX binary")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println(version)
		return
	}
	_, port, err := net.SplitHostPort(*listen)
	if err != nil {
		log.Fatalf("-listen %q: %v", *listen, err)
	}
	// Catch SIGTERM/SIGINT before any child starts: a signal during startup must still end in
	// the graceful shutdown that stops MediaMTX, the tunnels and the motion readers.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	time.Local = platform.LocalZone() // Go on Android otherwise runs in UTC
	platform.WakeLock()
	tsState := filepath.Join(*dataDir, "tailscale")
	if err := os.MkdirAll(tsState, 0o700); err != nil { // also creates the data dir
		log.Fatal(err)
	}
	// Android 6 trusts no Let's Encrypt root and has no /etc/resolv.conf. cloudflared and tailscaled
	// inherit SSL_CERT_FILE, and proot shows them the generated resolv.conf.
	certs := filepath.Join(*dataDir, "certs.pem")
	if err := config.WriteFileIfChanged(certs, platform.CACerts); err != nil {
		log.Fatal(err)
	}
	os.Setenv("SSL_CERT_FILE", certs)
	resolvConf := filepath.Join(*dataDir, "resolv.conf")
	refreshDNS := func() {
		b := platform.ResolvConf(platform.Getprop("net.dns1"), platform.Getprop("net.dns2"))
		if err := config.WriteFileIfChanged(resolvConf, b); err != nil {
			log.Printf("resolv.conf: %v", err)
		}
	}
	refreshDNS()
	if _, err := os.Stat("/etc/resolv.conf"); err != nil {
		net.DefaultResolver = platform.Resolver(resolvConf) // Android: the portal itself talks to Google (Drive sign-in)
	}

	store, err := config.Open(filepath.Join(*dataDir, "config.json"))
	if err != nil {
		log.Fatal(err)
	}
	sup := supervisor.New(filepath.Join(*dataDir, "logs"))
	mtx := mediamtx.NewClient()
	mtxConf := filepath.Join(*dataDir, "mediamtx.yml")

	// ponytail: any camera change rewrites the config and restarts MediaMTX (a ~2 s gap in every
	// stream); move to MediaMTX's per-path API if cameras start changing often.
	var mtxMu sync.Mutex
	tsIP := "" // guarded by mtxMu: the phone's Tailscale IPv4, offered to WebRTC clients on the tailnet
	restartMediaMTX := func() {
		mtxMu.Lock()
		defer mtxMu.Unlock()
		cfg := store.Get()
		if cfg.RecDir == "" {
			return // not set up yet
		}
		b, err := mediamtx.Config(cfg.RecDir, cfg.Cameras, recorder.Desired(cfg.Cameras, time.Now()), tsIP)
		if err == nil {
			err = config.WriteFileAtomic(mtxConf, b)
		}
		if err != nil {
			log.Printf("mediamtx config: %v", err)
			return
		}
		sup.Start(mediamtx.ProcessSpec(*mtxBin, mtxConf))
	}
	restartMediaMTX()
	events := &motion.Store{Dir: filepath.Join(*dataDir, "events"), Zone: time.Local}
	motions := &motion.Manager{
		Sup: sup, FFmpeg: "ffmpeg", FFmpegVersion: ffmpegVersion(), RTSP: mediamtx.RTSPAddr,
		Store: events, Now: time.Now, Logf: log.Printf,
	}
	motions.Apply(store.Get().Cameras)
	camerasChanged := func() {
		restartMediaMTX()
		motions.Apply(store.Get().Cameras)
	}
	rclone := storage.Rclone{Bin: nextTo(binDir, "rclone"), Config: filepath.Join(*dataDir, "rclone.conf")}
	uploader := &storage.Uploader{
		Store: store, Rclone: rclone, Ledger: &storage.Ledger{Dir: filepath.Join(*dataDir, "uploads")},
		Spans: mtx.Spans, Events: motions.Events,
		Fetch: mtx.Download, Now: time.Now, Zone: time.Local,
	}
	cloud := storage.Service{Rclone: rclone, Google: storage.NewGoogle(), Uploader: uploader}

	tunnels := &tunnel.Manager{
		Bins: tunnel.Bins{
			Cloudflared: filepath.Join(binDir, "cloudflared"),
			Tailscaled:  filepath.Join(binDir, "tailscaled"),
			Tailscale:   filepath.Join(binDir, "tailscale"),
			Proot:       prootPath(),
			ResolvConf:  resolvConf,
			StateDir:    tsState,
		},
		Sup:         sup,
		Hostname:    "camorage",
		ServeTarget: "http://127.0.0.1:" + port,
		ReadyURL:    "http://" + tunnel.CloudflaredMetrics + "/ready",
		// ponytail: a new Tailscale IP restarts MediaMTX (~2 s gap), in practice once per start;
		// patch webrtcAdditionalHosts through MediaMTX's API if that gap ever matters.
		OnIP: func(ip string) {
			mtxMu.Lock()
			tsIP = ip
			mtxMu.Unlock()
			restartMediaMTX()
		},
	}
	tunnels.Apply(store.Get().Tunnels)

	go every(ctx, 5*time.Second, func() { tunnels.Poll(ctx) })
	go every(ctx, time.Minute, refreshDNS)                            // the phone's DNS servers change with the network
	go every(ctx, 5*time.Second, func() { motions.Tick(time.Now()) }) // closes events when frames stop
	// uploads one clip at a time; a long upload delays the next run
	go every(ctx, time.Minute, func() {
		if err := uploader.Run(ctx); err != nil {
			log.Printf("upload: %v", err)
		}
	})
	go every(ctx, 24*time.Hour, func() { // cloud retention (spec §5.6), old ledger days
		if err := uploader.Retention(ctx); err != nil {
			log.Printf("cloud retention: %v", err)
		}
	})
	go every(ctx, 30*time.Second, func() {
		if cfg := store.Get(); cfg.RecDir != "" {
			recorder.Reconcile(ctx, mtx, cfg.Cameras, time.Now(), log.Printf)
		}
	})
	go every(ctx, time.Minute, func() {
		cfg := store.Get()
		if cfg.RecDir == "" {
			return
		}
		deleted, err := recorder.Janitor(cfg.RecDir, cfg.Cameras, time.Now(), platform.Disk, motions.Events)
		if len(deleted) > 0 {
			log.Printf("janitor: deleted %d segments", len(deleted))
		}
		if err != nil {
			log.Printf("janitor: %v", err)
		}
		days := map[string]int{}
		for _, c := range cfg.Cameras {
			days[c.ID] = c.LocalDays
		}
		keep := func(cam string) int {
			if d := days[cam]; d > 0 {
				return d
			}
			return config.DefaultLocalDays
		}
		if err := events.Prune(time.Now(), keep); err != nil {
			log.Printf("events: %v", err)
		}
	})

	srv := &http.Server{
		Addr:              *listen,
		ReadHeaderTimeout: 10 * time.Second,
		Handler: web.New(web.Deps{
			Store: store, MTX: mtx, Processes: sup.Status,
			Health: func(recDir string) platform.Health {
				return platform.ReadHealth("/sys/class/power_supply/battery", "/proc/meminfo", recDir)
			},
			Zone: time.Local, Limiter: auth.NewLimiter(), Now: time.Now, HTTP: &http.Client{},
			OnSetup: restartMediaMTX, OnCamerasChanged: camerasChanged,
			HLSBase: "http://" + mediamtx.HLSAddr, WebRTCBase: "http://" + mediamtx.WebRTCAddr,
			Volumes: platform.Volumes, ONVIF: onvif.LAN{},
			Tunnels: tunnels.Status, OnTunnelsChanged: func() { tunnels.Apply(store.Get().Tunnels) },
			Motion: motions,
			Cloud:  cloud, Usage: func(recDir string) (recorder.Usage, error) { return recorder.MeasureUsage(recDir, time.Now()) },
			Version: version,
		}),
	}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()
	log.Printf("camorage listening on %s (data %s)", *listen, *dataDir)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Print(err)
	}
	motions.Flush() // events in progress keep their latest end
	sup.StopAll()
}

// every runs fn now and then every interval until ctx ends.
func every(ctx context.Context, interval time.Duration, fn func()) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		fn()
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// prootPath is proot when this system has no /etc/resolv.conf (Android): cloudflared and
// tailscaled then run under proot with the portal's resolv.conf in its place (M0). Elsewhere they
// run directly.
func prootPath() string {
	if _, err := os.Stat("/etc/resolv.conf"); err == nil {
		return ""
	}
	p, err := exec.LookPath("proot")
	if err != nil {
		log.Print("no /etc/resolv.conf and no proot: cloudflared and tailscaled cannot resolve names (pkg install proot)")
		return ""
	}
	return p
}

// ffmpegVersion is the installed ffmpeg's major.minor (its flags differ between versions);
// unknown builds get the newest flags.
func ffmpegVersion() [2]int {
	out, _ := exec.Command("ffmpeg", "-version").Output()
	major, minor := motion.ParseVersion(string(out))
	return [2]int{major, minor}
}

// nextTo is the program called name next to camorage's own binary if there is one, else name
// (looked up in PATH: Termux's package on the phone).
func nextTo(binDir, name string) string {
	if p := filepath.Join(binDir, name); fileExists(p) {
		return p
	}
	return name
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }
