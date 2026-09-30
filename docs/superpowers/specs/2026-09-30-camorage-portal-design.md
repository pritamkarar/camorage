# camorage portal — design spec

Date: 2026-09-30 · Status: approved in conversation, pending written-spec review

## 1. Intent

A self-hosted camera recorder + web portal that runs on an old Android phone and is usable from anywhere.

- **For:** the owner's own cameras now; a portable product later (no device-specific paths, simple installer, other phones).
- **Success ("done means"):** from mobile data outside home, open `https://cams.<domain>` (or the Tailscale URL), log in, watch cameras live, scrub yesterday on a 24 h timeline with motion markers, change retention, and see clips arriving in Google Drive.

### In scope
- Add cameras (ONVIF discovery or manual RTSP URL); up to **4 cameras per phone**.
- Live view: grid (substreams) + single camera (main stream); WebRTC when directly reachable, HLS otherwise.
- Recording modes per camera: **Continuous** or **Motion**, each optionally limited by a **weekly schedule**.
- Motion detection: phone-side (default) or camera ONVIF events; markers on the timeline.
- Storage: **local SD (last N days) + optional cloud copy** (Google Drive, S3-compatible, B2) with separate retention.
- Playback: **24 h timeline** per camera/day, click-to-play, download.
- Remote access: **Tailscale and Cloudflare Tunnel**.
- Single admin login.

### Out of scope (for now)
AI object detection, motion alerts/notifications (natural next feature), audio, PTZ, mobile app, multi-site management, multiple users/roles, encrypting secrets inside the private data dir.

## 2. Platform constraints (measured 2026-09-30)

| Item | Value |
|---|---|
| Phone | Samsung Galaxy On7 SM-G600FY, Android 6.0.1 (SDK 23), kernel 3.10.49, 32-bit armv7 userspace, 2 GB RAM, no root |
| Storage | internal ~0.9 GB free; microSD 7.4 GB (vfat), Termux-writable at `/storage/<uuid>/Android/data/com.termux/files` |
| Network | 2.4 GHz Wi-Fi, 72 Mbps link |
| Termux | 0.119 "android-5" build → frozen repo `termux-main-21` (late 2019): ffmpeg 4.2.1, rclone 1.50.1, proot, python 3.8, go 1.13. No `$PREFIX/etc/resolv.conf` on this install. |
| Termux (current) | F-Droid/GitHub build, Android 7+ (probed in `termux/termux-docker:x86_64`, 2026-09-27; installtest 2026-09-30): ffmpeg 8.1.2–8.1.3, rclone 1.75.1, proot; the M3 storage tests and the motion reader pass against them (M4) |
| Kernel quirks | 3.10 has no `MemAvailable` in `/proc/meminfo`; `getprop net.dns1` may be an IPv6 address |
| Camera (first) | macro-video-soft 720p, ONVIF :8899; main `rtsp://<ip>/live/ch00_0` 1280×720 ~0.5 Mbps; sub `/live/ch00_1` 640×360 ~0.14 Mbps; keyframe every 2 s; RTSP without auth; advertises ONVIF `MotionAlarm` but did not emit it in a live test; occasional duplicate DTS |
| Laptop (build host) | Go 1.27, Node 24, ffmpeg 6.1, Python 3.12 |

Known quirks (already solved in `camorage.sh`/`setup.sh`, must be carried into adapters):
- ffmpeg 4.x: RTSP socket timeout is `-stimeout` (4.x `-timeout` means *listen*); 5.x+: `-timeout`.
- rclone < 1.51 with `drive.file` scope: set `root_folder_id = root` or root lookup 404s.
- rclone's shared Google client ID is rate-limited/retired → always use the user's own OAuth client.
- Android has no `/etc/resolv.conf` and a 2015 CA store → Go `GOOS=linux` binaries need a DNS dialer and a current CA bundle. Third-party Go binaries (cloudflared, tailscaled) cannot take a dialer → run them under `proot -b <data>/resolv.conf:/etc/resolv.conf` (M0: proot overhead ≈ 0 % CPU).
- ffmpeg 4.2 rawvideo output needs `-vsync 0` (otherwise frames are duplicated to a constant rate).
- MediaMTX (Go, `GOOS=linux`) runs in **UTC** on Android: segment names, logs and playback timestamps are UTC.
- The camera's irregular frame timing makes MediaMTX's low-latency HLS warn that its parts will break iOS clients (warning seen on the substream; no iOS device tested) → use the standard `fmp4` HLS variant (not run in M0; verify on iOS in M1).

## 3. Architecture

```
 browser ──(Tailscale serve / Cloudflare Tunnel / LAN)──▶ camorage portal (Go, :8080)
                                                          │ UI + JSON API + auth
                                                          │ supervises children ↓
 cameras ──RTSP──▶ MediaMTX (loopback only) ◀─────────────┤ config file + HTTP API
                     ├─ live: HLS / WebRTC ───────────────┤ reverse-proxied by portal
                     ├─ record: fMP4 1-min segments ──▶ SD card
                     └─ playback server (time range → one MP4)
 <cam>_sub keyframes ─▶ ffmpeg (Termux) ─▶ 64×36 gray frames ─▶ portal motion detector
 ONVIF PullPoint ─────────────────────────────────────────▶ portal motion detector (camera source)
 portal ─▶ rclone (Termux): upload, list, delete, cat (cloud playback)
 portal ─▶ cloudflared, tailscaled (userspace) when enabled
```

### Components

| Unit | Responsibility | Depends on |
|---|---|---|
| `cmd/camorage` | entrypoint: load config, platform setup, start supervisor + HTTP server | all below |
| `internal/platform` | Android shims: DNS dialer for the portal itself (a `net.DefaultResolver` whose Dial takes the nameservers of the generated `<data>/resolv.conf` in turn, fallback 1.1.1.1), generated `<data>/resolv.conf` (`nameserver <net.dns1>` + `nameserver <net.dns2>` + `nameserver 1.1.1.1`, re-checked every minute and rewritten when they change; Go re-reads resolv.conf on change, so children need no restart) for proot-wrapped children, embedded CA bundle written to `<data>/certs.pem` and exported as `SSL_CERT_FILE` for self + children, wake lock (`termux-wake-lock`), health (battery, temp, disk, memory = `MemFree + Cached`) | — |
| `internal/config` | `config.json` load/save (atomic temp+rename), schema version + migrations, validation | — |
| `internal/auth` | admin password (argon2id), signed session cookie, login lockout, origin check, trusted client IP | config |
| `internal/supervisor` | start/stop/restart child processes with backoff (1 s → 60 s), capture logs (rotating), expose status; each child gets its own process group and stops signal the group (proot ignores SIGTERM and orphans its program when killed — M1c) | — |
| `internal/mediamtx` | generate `mediamtx.yml` (pinned MediaMTX v1.21.x; MoQ, RTMP, SRT disabled; `hlsVariant: fmp4`; `webrtcAdditionalHosts` = Tailscale IP when enabled, with interface IPs left enabled so LAN WebRTC keeps working and ICE picks the reachable pair), API client (paths, record on/off, bitrate stats; liveness = path `available`, not `online`), playback client (`/list`, `/get`; UTC ↔ local conversion), reverse proxy for HLS/WebRTC | supervisor |
| `internal/onvif` | WS-Discovery, GetProfiles/GetStreamUri (WS-UsernameToken), PullPoint events | — |
| `internal/motion` | ffmpeg keyframe reader, block-diff detector, ONVIF event source, event store (`events/<cam>/<date>.jsonl`) | onvif, supervisor |
| `internal/recorder` | schedule evaluation → record on/off; janitor: keep/prune rule, local retention, space floor | mediamtx, motion, config |
| `internal/storage` | rclone adapter (version-aware), cloud targets, upload queue + ledger, cloud retention, cloud listing + ranged streaming, Drive sign-in exchange | platform, mediamtx |
| `internal/ffmpeg` | ffmpeg adapter (version-aware flags): lives in `internal/motion` (`ParseVersion`, `ReaderArgs`: `-stimeout` / `-vsync` for 4.x, `-timeout` from 5, `-fps_mode` from 5.1); snapshot grab not built | — |
| `internal/tunnel` | cloudflared + tailscaled process specs, Tailscale login URL + `serve` setup | supervisor, platform |
| `internal/web` | HTTP routes, JSON API, embedded static UI (vanilla ES modules + vendored hls.js, no build step) | all services |

Build: `CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags "-s -w -X main.version=<v>"` on the laptop for `GOARCH=arm GOARM=7`, `arm64` and `amd64` → single static binaries (`scripts/release.sh`). External Go deps limited to `golang.org/x/crypto` (argon2).

Install (M4): `curl -fsSL https://github.com/pritamkarar/camorage/releases/latest/download/install.sh | bash` in Termux. install.sh maps `uname -m` to a build (`armv7*`/`armv8l` → arm, `aarch64` → arm64, `x86_64` → amd64), installs `ffmpeg rclone proot` with `pkg`, downloads camorage from the release and MediaMTX v1.21.1, cloudflared 2026.9.3 and Tailscale 1.102.4 from their official releases, checks them all against the release's `SHA256SUMS`, and only then stops the old portal (giving up, with nothing changed, if it does not stop within 60 s) and swaps the binaries into `~/camorage/bin`, keeping the previous ones until the new portal answers (otherwise it puts them back and restarts the previous version); it writes `~/camorage/start.sh` and `~/.termux/boot/start-camorage` and never touches `~/.camorage` or recordings. Re-running it upgrades.

### Ports

| Listener | Bind | Notes |
|---|---|---|
| portal HTTP | `0.0.0.0:8080` | LAN + tunnels (tunnels connect via loopback) |
| MediaMTX RTSP 8554, HLS 8888, WebRTC HTTP 8889, API 9997, playback 9996 | `127.0.0.1` | only the portal talks to them |
| MediaMTX WebRTC ICE 8189 (UDP + TCP) | `0.0.0.0` | media only; signalling goes through the portal |

### MediaMTX paths
- `<camId>`: main stream; `record` toggled by the recorder; recording settings: `recordFormat: fmp4`, `recordPartDuration: 1s`, `recordSegmentDuration: 1m`, `recordPath: <recDir>/%path/%Y-%m-%d_%H-%M-%S-%f`, `recordDeleteAfter: 0s` (portal owns deletion).
- `<camId>_sub`: substream; never recorded; always-on (needed by motion + grid).
- Camera sees at most 2 RTSP connections regardless of viewer count.
- All MediaMTX timestamps (segment file names, `/list`, `/get?start=`) are UTC; the portal converts to phone local time for display, schedules and cloud file names.

## 4. Data

Data dir `CAMORAGE_DATA` (default `~/.camorage`, Termux-private, files `0600`):

| Path | Content |
|---|---|
| `config.json` | `version`, `admin {hash}`, `sessionKey`, `recDir`, `cameras[]`, `storageTargets[]`, `tunnels {cloudflareToken, cloudflareHostname, tailscaleEnabled}`, `dns` |
| `events/<cam>/<YYYY-MM-DD>.jsonl` | `{"start":…, "end":…, "peak":…, "source":"phone|onvif"}` |
| `uploads/<cam>/<YYYY-MM-DD>.txt` | ledger of cloud file names already uploaded |
| `rclone.conf` | portal-managed remotes (never the user's personal config) |
| `mediamtx.yml`, `certs.pem`, `logs/` | generated / embedded / rotated |

Camera record:
```json
{ "id": "cam1", "name": "Gate", "enabled": true,
  "mainUrl": "rtsp://…/live/ch00_0", "subUrl": "rtsp://…/live/ch00_1",
  "onvif": { "xaddr": "http://…:8899/onvif/device_service", "user": "", "pass": "" },
  "mode": "continuous|motion",
  "schedule": [ { "days": [1,2,3,4,5], "start": "20:00", "end": "08:00" } ],
  "motion": { "source": "phone|onvif", "sensitivity": "low|medium|high",
              "ignore": [/* 16×9 block mask, row-major booleans */],
              "preRollSec": 10, "postRollSec": 30 },
  "motionSince": "2026-10-01T09:00:00+05:30",  // set by the portal when mode switches to motion; the keep rule starts there
  "localDays": 1,
  "cloud": { "targetId": "gdrive", "days": 7 } }
```
Empty `schedule` = always. `days` are ISO weekdays (1 = Monday … 7 = Sunday) in phone local time. A window whose `end` ≤ `start` crosses midnight and belongs to the day it starts.

Storage target: `{ "id", "name", "type": "drive|s3|local", "remote": "<rclone root>" }`. `remote` is `<id>:` for Drive, `<id>:<bucket>` for S3-compatible storage (AWS, Backblaze B2 through its S3 endpoint, Cloudflare R2, Wasabi, MinIO), `<id>:<dir>` for a local folder (tests). Credentials live only in the `<id>` section of `rclone.conf`. A camera's `cloud` is `{ "targetId", "days", "since" }`; the portal sets `since` when cloud copy is switched on or retargeted, and footage from before it is not uploaded. Local SD is implicit.

Day boundaries: dates in `events/` and `uploads/` file names and in cloud `<YYYY-MM-DD>` folders are phone-local calendar days, converted from MediaMTX's UTC.

Recordings dir is chosen in first-run setup from detected writable volumes (no hardcoded UUID paths).

## 5. Recording pipeline

### 5.1 Schedule
Every 30 s the recorder evaluates each camera's schedule (phone local time) and sets MediaMTX `record` on/off for `<camId>`. Outside the schedule nothing is recorded and motion events are not stored.

### 5.2 Motion detection
- **Phone source:** `ffmpeg -skip_frame nokey -rtsp_transport tcp -i rtsp://127.0.0.1:8554/<cam>_sub -vsync 0 -vf scale=64:36,format=gray -f rawvideo pipe:1` (plus a version-correct timeout flag: `-stimeout` on 4.x, `-timeout` on ≥ 5; `-vsync 0` becomes `-fps_mode passthrough` on ffmpeg ≥ 5.1; `-flush_packets 1` so each frame arrives at once). One reader per enabled camera in motion mode, supervised as `motion-<cam>`; it reads `<cam>_sub`, or `<cam>` when there is no substream. Frames are timestamped on arrival; outside the schedule they count as no motion. M0: ~3 % of one core and 9 MB per camera, full rate with the screen off. One 2304-byte frame per keyframe (~every 2 s).
- **Detector:** 16×9 grid of 4×4-px blocks. Block changed if mean |Δ| > T. Motion if changed (non-ignored) blocks ≥ K. If > 50 % of non-ignored blocks change at once → lighting change, not motion. Defaults (T, K): low (25, 6), medium (15, 3), high (8, 2) — tunable after spike measurements.
- **ONVIF source:** PullPoint subscription on the camera's event service; `MotionAlarm`/`CellMotionDetector` `State|IsMotion=true/false`. Subscription renewed; on failure the camera falls back to phone source and the UI shows a warning. Deferred (M2): the only camera never emits motion events (M0), so `motion.source` accepts only `phone` until a camera that does is available.
- **Events:** `start` = time of the first motion frame, `end` = time of the last motion frame. An event closes when no motion has been seen for `postRollSec` after `end`; it is appended to `events/…jsonl` when it opens and again when it closes; readers keep the latest line per start, so a restart or crash loses at most an event's end (the event in progress is also kept in memory and reported by the API).

### 5.3 Keep rule (motion mode)
Segment `S = [s0, s1]` is **kept** iff some event `E` (open or closed) satisfies `E.start − preRoll < s1` and (`E` open or `E.end + postRoll > s0`).
A segment's fate is decided only when `now ≥ s1 + preRoll`; before that it is pending. Not-kept segments are deleted once older than **1 hour** (recent-footage buffer). Continuous mode keeps every segment. A segment's end s1 is the next segment's start, at most s0 + 2 min. The rule applies only to footage recorded since the camera switched to motion mode (`motionSince`); earlier footage keeps continuous-mode retention, so switching modes never deletes what was already recorded.

### 5.4 Janitor (every 60 s)
1. Apply keep rule (motion cameras).
2. Delete local segments older than `localDays`.
3. Space floor: while free < max(500 MB, 5 % of volume) delete oldest not-kept segments, then oldest kept. Recording never stops for space.
4. Enqueue cloud uploads (5.5).
5. Once per day: cloud retention (5.6), event/ledger files older than max(localDays, cloud.days).

### 5.5 Cloud copy (upload)
- **Continuous:** one file per clock hour, enqueued once `now ≥ hourEnd + 2 min`.
- **Motion:** one file per event covering `[start − preRoll, end + postRoll]`, enqueued once the event is closed and `now ≥ end + postRoll + 1 min`.
- Worker: `GET playback /get?path=<cam>&start=…&duration=…&format=mp4` → temp file in `<recDir>/.upload/` → `rclone copyto` → append name to ledger → delete temp. Failures retry next round with backoff; queue rebuilt from segments + ledger on startup.
- Remote path: `camorage/<cam>/<YYYY-MM-DD>/<HH-MM-SS>_<hour|motion>_<secs>s.mp4` (phone-local date and start of the clip). MediaMTX's `/get` stops at the first gap in a recording, so an hour or an event window is uploaded as one clip per recorded piece (pieces under 1 s are skipped). A motion window starts no earlier than the previous event's window ends. The ledger `uploads/<cam>/<date>.txt` lists uploaded clip paths; a piece counts as uploaded when the listed clips together cover its time, so footage re-planned after its start was deleted is not uploaded again. Each run plans back at most 2 days (or from `cloud.since`). Blind periods (M2) stay on the phone.
- Local segments are not removed by uploading (copy, not move).

### 5.6 Cloud retention
Daily per camera: `rclone delete <remote>:camorage/<cam> --min-age <days>d` (Drive: `--drive-use-trash=false`), then `rclone rmdirs --leave-root`.

## 6. UI

Pages: **Login**, **Live**, **Playback**, **Cameras**, **Storage**, **Settings**, **First-run setup**.

- **Live:** 2×2 grid of substream HLS; badges offline/recording/motion via polling `GET /api/status` every 3 s. Tap → full screen main stream: try WebRTC (WHEP via portal), fall back to HLS after 4 s. HLS uses the standard fMP4 variant (expected ≈ 4–8 s delay and iOS compatibility — both to be verified in M1); WebRTC is the low-latency path on LAN and Tailscale.
- **Playback:** camera + local date → 24 h bar (zoom to 1 h; the portal queries `/list` with the UTC range covering that local day): local spans from MediaMTX `/list`, cloud-only spans from `rclone lsjson` of the day folder (parsed from file names), motion markers from events. Click → local: `/api/playback/local?cam&start&duration=600` (proxied `/get`, fmp4) and on `ended` request the next 10 min; cloud: `/api/playback/cloud/<file>` with HTTP Range → `rclone cat --offset --count`. Download: local range as `format=mp4`, cloud file as-is.
- **Cameras:** list; "Scan network" (WS-Discovery) → pick → credentials → auto-fill main/sub URLs from profiles (largest = main, smallest = sub); manual URL entry; per-camera settings incl. weekly schedule editor and ignore-area painting over the live substream (the picture motion detection sees), by mouse or touch drag.
- **Storage:** recordings dir, usage, days-that-fit estimate (from MediaMTX bytes-received rate); cloud targets: S3-compatible form (Amazon S3, or "Other" with an endpoint: Backblaze B2, Cloudflare R2, Wasabi, MinIO) + Test + Remove (refused while a camera copies to it); a target is stored only after its check passes; Drive: client ID/secret → "Sign in with Google" (Desktop client, loopback redirect `http://127.0.0.1:53682/`) → user pastes the resulting URL → portal exchanges the code and writes the rclone remote (`scope=drive.file`, `root_folder_id=root` for rclone < 1.51).
- **Settings:** admin password; Tailscale (enable, login link, URL); Cloudflare (token, hostname); phone health; child process status.

## 7. Remote access & security

- **Tailscale:** `proot -b <data>/resolv.conf:/etc/resolv.conf tailscaled --tun=userspace-networking --statedir=<data>/tailscale --socket=<data>/tailscale/sock` (tailscaled itself works without proot via bootstrap DNS, but `serve`'s HTTPS certificate fetch from Let's Encrypt uses system DNS — M0); requires MagicDNS + HTTPS certificates enabled in the tailnet admin console (already on for the owner's tailnet); first HTTPS request after start takes ~20 s while the cert is issued. WebRTC over the tailnet works because userspace mode forwards inbound UDP to `127.0.0.1:8189` (M0 proved this with interface candidates disabled; M1 acceptance must repeat it from a tailnet device off the LAN, e.g. a phone on mobile data, with interface candidates enabled); login URL surfaced in Settings; `tailscale serve` → `https://<phone>.<tailnet>.ts.net` → `http://127.0.0.1:8080`. The portal reads tailscaled's LocalAPI status (`/localapi/v0/status` on its socket) every 5 s. When a sign-in link is needed it runs `tailscale up --hostname=camorage --reset --timeout=10s` (the link stays in the status after the CLI stops waiting). Once Running, it runs `tailscale serve --bg --yes http://127.0.0.1:8080` and restarts MediaMTX with the Tailscale IPv4 in `webrtcAdditionalHosts`.
- **Cloudflare:** `proot -b <data>/resolv.conf:/etc/resolv.conf cloudflared tunnel --no-autoupdate --metrics 127.0.0.1:20241 run` with the token in `TUNNEL_TOKEN` (never argv, which every app can read; the dashboard's whole "cloudflared service install <token>" command is accepted too; `GET 127.0.0.1:20241/ready` = 200 while connected) (M0: fails DNS without proot; under proot QUIC works, ~8.5 % of one core and 22 MB while streaming — tested as a quick tunnel `--url`; the named-tunnel form with `TUNNEL_TOKEN` was verified in M1c at cams.example.com (QUIC, edge del03)); user maps `cams.<domain>` → `http://localhost:8080` in the dashboard; Cloudflare Access (email OTP) recommended in front. Free-plan terms discourage heavy video → Tailscale for heavy viewing.
- **Auth:** all routes except `/login` and static assets require a session. Password argon2id. Session = HMAC-signed cookie (30-day expiry; key rotated on password change) with `HttpOnly`, `SameSite=Lax`, and `Secure` when the request came through a tunnel: a loopback peer **with** `X-Forwarded-For` (both tunnels terminate HTTPS, connect via loopback and set that header; a tailnet device opening `http://<tailscale-ip>:8080` is also handed over from loopback by userspace tailscaled, but over plain HTTP and without the header).
- **Lockout:** 5 failed logins → 15-min lockout per client IP. Client IP = the **last** `X-Forwarded-For` entry when the request came through a tunnel (Cloudflare appends the address it saw; `tailscale serve` replaces the header with the tailnet peer), otherwise the peer address. `CF-Connecting-IP` is not trusted: anyone on the tailnet could send it (M1c). An IPv6 client counts by its /64 (one client usually holds a whole /64). Failed sign-ins through Cloudflare from all addresses together are capped at 20 per 15 min; Tailscale and the home network are not counted, so the owner can still sign in there during an attack.
- **CSRF:** mutating requests require `Origin` matching the request host.
- **Secrets:** tokens/keys stored `0600`, never returned by the API (masked, replace-only).
- **First run:** tunnels cannot be enabled until the admin password exists; setup therefore happens on the LAN.

## 8. Reliability

- Supervisor restarts MediaMTX, per-camera motion ffmpeg, cloudflared, tailscaled with backoff; status visible in Settings.
- Camera offline → MediaMTX retries; UI badge; timeline gap.
- Internet down → upload queue waits; space floor protects the SD card.
- Crash safety: 1 s fMP4 parts; atomic config writes; append-only events.
- Startup reconciles: rescan segments, rebuild upload queue from ledger.
- After a reboot: Android 7+ starts camorage through Termux:Boot (`~/.termux/boot/start-camorage`, written by the installer); Android 5/6 have no Termux:Boot → the user opens Termux and runs `~/camorage/start.sh` (battery covers power cuts). Android 12+ may kill Termux's child processes (phantom process killer): the README gives the setting / adb command.
- Resource budget (4 cameras): < 150 MB RAM, < 25 % average CPU. M0 measured MediaMTX + 4 motion readers on one camera at 22.8 % of one core (~6 % of the phone) and 60 MB, screen off; cloudflared adds ~8.5 % and 22 MB while streaming (MediaMTX peaked at 14.9 % / 33 MB during that run). M1c measured the full stack with 1 camera over 60 s, streaming the grid over Tailscale and HLS over Cloudflare at once: camorage 2.6 % / 10 MB, MediaMTX 13.8 % / 35 MB, tailscaled 11.8 % / 54 MB, cloudflared 9.1 % / 25 MB, proot ≈ 0 %; TOTAL 37.4 % of one core, 124 MB (Tailscale only: 19.6 %, 125 MB). 3 more camera ingests are not measured yet. M2 measured one phone-side motion reader (substream keyframes) at 2.5 % of one core and 4 MB; full stack TOTAL 16.6 % / 51 MB with nobody watching. M3 measured an upload of a 17-minute hour clip (48 MB) to Google Drive: rclone 1.50.1 finished in under 30 s, portal 0.5 % / 12 MB and MediaMTX 3.7 % / 22 MB meanwhile; a cloud clip seek (range request) takes ~12 s on the phone (folder listing ~3.5 s + `rclone cat` start).

## 9. Testing

- **Unit (laptop, `go test`):** motion detector (static, moving block, global lighting, ignore mask); keep rule incl. pending-before-preRoll; schedule windows (overnight, midnight, empty = always); janitor floor ordering; config migration; auth (hash verify, lockout, origin check, trusted IP); ffmpeg/rclone adapters (flag selection per version).
- **Integration (laptop):** real MediaMTX (amd64) + ffmpeg `testsrc` moving-box publisher as a fake camera + rclone local-dir remote: record → motion event → keep/prune → upload → cloud listing → playback API. Repeat storage tests with rclone 1.50.1.
- **Installer (laptop):** `scripts/installtest.sh`: download plan per CPU and refusals; the release for arm, arm64 and amd64 with checksums; fresh install, first-run, upgrade in place, damaged-download refusal and the motion-reader / rclone tests inside current Termux (`termux/termux-docker:x86_64`), served from a local copy of the release.
- **On-phone acceptance:** the "done means" checklist on the real phone and camera.

## 10. Milestones

Each milestone gets its own implementation plan.

| # | Scope | Exit |
|---|---|---|
| **M0 Spike** (throwaway) — **done, GO** ([findings](../spikes/2026-09-30-m0-findings.md)) | MediaMTX armv7 on Android 6 (RTSP in, HLS, WebRTC, record, playback `/get`); Go DNS dialer + CA bundle; cloudflared + tailscaled userspace + `tailscale serve`; WebRTC over Tailscale; CPU/RAM of 4× keyframe motion decode; camera accepts 3 concurrent RTSP clients | go/no-go per item + measured numbers; spec amended |
| **M1 Core** | portal skeleton, first-run, auth, cameras + ONVIF discovery, live (HLS + WebRTC), continuous + scheduled recording, local retention + floor, 24 h local timeline, Tailscale + Cloudflare, health page | "done means" minus motion and cloud |
| **M2 Motion** — **done** (phone source; ONVIF source deferred) | phone + ONVIF detection, motion mode keep rule, timeline markers, ignore areas | motion-only retention verified on phone |
| **M3 Cloud** — **done** (Google Drive verified on the phone; S3-compatible by form, B2 via its S3 endpoint) | Drive sign-in flow, S3/B2, upload queue + ledger, cloud retention, cloud playback; retire `camorage.sh` | full "done means" |
| **M4 Portability** | installer, arm64 builds, newer Termux (version-aware adapters), docs | fresh install on a second phone |

Until M3, `camorage.sh` keeps running for Drive backup (camera then serves 3 RTSP clients — checked in M0).

## 11. Decisions log

| Decision | Choice | Why |
|---|---|---|
| Hardware | old Android phone | ESP32 too weak for video; Pi-class boards unavailable locally |
| Stack | Go portal + MediaMTX (approach A) | least custom code; WebRTC + HLS; built-in playback server; portable single binary |
| Storage model | local N days + cloud copy | instant local playback, offline-safe, long cloud retention |
| Playback | 24 h timeline | NVR-standard UX; MediaMTX `/list` + `/get` make it cheap |
| Scale | ≤ 4 cameras/phone | 2.4 GHz Wi-Fi + SD capacity |
| Motion | phone keyframe diff default, ONVIF events optional | camera did not emit ONVIF motion; keyframe-only decode ~0.5 fps/camera |
| Remote | Tailscale + Cloudflare | private + fast for owner, public domain for others |
| Dev access | SSH into Termux, key-only (approved 2026-09-30) | spike/testing needs many on-phone commands |
| Third-party Go binaries on Android | run cloudflared + tailscaled under proot with a generated resolv.conf | M0: no resolv.conf on Android or in this Termux; proot costs ≈ 0 % CPU |
| MediaMTX time base | treat all MediaMTX timestamps as UTC | M0: segment names/logs/`/list` are UTC on Android |
| HLS variant | standard `fmp4`, not low-latency | M0: MediaMTX warns LL-HLS part-duration jitter from this camera will break iOS clients; WebRTC covers low latency |
| Stream liveness | MediaMTX path `available` | M0: `online` is true even for an unreachable source |
| MediaMTX version | pin v1.21.x, disable MoQ/RTMP/SRT | M0 validated v1.21.1 on armv7; MoQ listener is on by default |
