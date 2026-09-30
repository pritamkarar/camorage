<p align="center"><img src="internal/web/ui/logo.svg" width="96" alt=""></p>

# camorage

A camera recorder and web portal that runs on an old Android phone. Watch your IP cameras live, record them to the phone's SD card, scrub a 24-hour timeline with motion markers, keep a copy in Google Drive or S3-compatible storage, and reach it all from anywhere through Tailscale or a Cloudflare Tunnel.

- Live view: a grid of all cameras, and full screen per camera (WebRTC at home and over Tailscale, HLS everywhere else)
- Recording: continuous or motion-only, each with an optional weekly schedule; the phone keeps the last N days and never fills its card
- Motion detection on the phone, with areas to ignore
- Playback: a 24-hour timeline per camera, click to play, download clips
- Cloud copy: Google Drive (your own OAuth client) or S3-compatible storage (Amazon S3, Backblaze B2, Cloudflare R2, Wasabi, MinIO), with its own retention
- Remote access: Tailscale and Cloudflare Tunnel, one admin login
- Up to 4 cameras per phone

## What you need

- An Android phone (5 or newer) with **Termux** from [F-Droid](https://f-droid.org/packages/com.termux/) or [GitHub](https://github.com/termux/termux-app/releases) (not the Play Store build). Android 5 and 6 use Termux's older "android-5" build.
- An ARM phone (32- or 64-bit); x86_64 works too.
- IP cameras with RTSP (ONVIF optional, for finding them on the network), on the same Wi-Fi.
- A microSD card is best for recordings; the phone's own storage works too.

## Install

Open Termux and run:

```
curl -fsSL https://github.com/pritamkarar/camorage/releases/latest/download/install.sh | bash
```

It installs the Termux packages camorage uses (`ffmpeg`, `rclone`, `proot`), downloads camorage, MediaMTX, cloudflared and Tailscale for your phone's CPU, checks each download's SHA-256 checksum, and starts camorage. It prints the address to open, such as `http://192.168.1.20:8080`. Open it in a browser on the same Wi-Fi to choose an admin password and where recordings go (pick the SD card if it is listed; run `termux-setup-storage` once if it is not).

**Upgrade:** run the same command again. Your settings (`~/.camorage`) and recordings are kept.

## Keep it running

- **After a reboot:** on Android 7 and newer, install the [Termux:Boot](https://f-droid.org/packages/com.termux.boot/) app and open it once; camorage then starts by itself. On Android 5 and 6, open Termux and run `~/camorage/start.sh`.
- **Battery:** in Android's settings, turn battery optimization off for Termux. camorage holds a Termux wake lock so recording goes on with the screen off.
- **Android 12 and newer** may stop Termux's background programs ("phantom process killer"). On Android 14 and newer, turn on *Developer options → Disable child process restrictions*. On Android 12 and 13, from a computer with adb: `adb shell "/system/bin/device_config set_sync_disabled_for_tests persistent; /system/bin/device_config put activity_manager max_phantom_processes 2147483647"`.
- Leave the phone on its charger; the battery covers power cuts.

## Remote access

Settings → Remote access:

- **Tailscale** (private, fast, good for watching): turn it on, open the sign-in link it shows, and approve the phone in your tailnet. Your tailnet needs MagicDNS and HTTPS certificates turned on (admin console → DNS). The portal is then at `https://<name>.<tailnet>.ts.net`; the first visit takes about 20 seconds while the certificate is issued.
- **Cloudflare Tunnel** (a public address on your own domain): in the Cloudflare dashboard create a tunnel (any operating system: only the token matters), add a public hostname pointing to `http://localhost:8080`, and paste the token (or the whole install command) into Settings. Put Cloudflare Access (email one-time codes) in front of it. Cloudflare's free plan is not meant for heavy video: use Tailscale for long viewing.

## Install as an app

Open the portal through its Tailscale or Cloudflare address (HTTPS) and use **Settings → About → Install app**, or your browser's menu (*Install app*; on an iPhone *Share → Add to Home Screen*). camorage then opens in its own window with its own icon, and shows a "Can't reach your camorage phone" page when the phone is off or offline. Browsers install apps only from HTTPS addresses: on the Wi-Fi address (`http://192.168.x.x:8080`) you can still add a home-screen shortcut, but it opens in the browser.

## Cloud copy

Storage → add a target, then pick it in a camera's settings (Cloud copy: where, and how many days to keep).

- **Google Drive:** in Google Cloud Console create an OAuth client of type *Desktop app* with the Google Drive API enabled, and publish its consent screen ("In production": while it says "Testing", Google ends the sign-in after 7 days). In camorage, Add Google Drive → paste the client ID and secret → Sign in with Google → paste back the address of the page that does not load. Clips go to a `camorage` folder; camorage can only see files it created.
- **S3-compatible:** Amazon S3, or "Other" with an endpoint (Backblaze B2, Cloudflare R2, Wasabi, MinIO); the bucket must exist.

Continuous cameras upload each clock hour a couple of minutes after it ends; motion cameras upload each event. Copying starts when you turn it on (earlier footage stays on the phone).

## Troubleshooting

- Logs: `~/camorage/camorage.log` (the portal) and `~/.camorage/logs/` (MediaMTX, motion readers, tunnels). Settings shows each background program's state and restarts.
- Stop: `kill $(pgrep -x camorage)`. Start: `~/camorage/start.sh`.
- Uninstall: `kill $(pgrep -x camorage); rm -rf ~/camorage ~/.camorage ~/.termux/boot/start-camorage`, then delete the recordings folder shown on the Storage page.

## Development

Built on a laptop with Go 1.27; the phone only runs the result.

| Command | What it does |
|---|---|
| `go test -race ./...` | unit tests |
| `node --test internal/web/ui/lib.test.mjs` | UI helper tests |
| `scripts/demo.sh` | a portal on this laptop with fake cameras, for UI work (http://127.0.0.1:18090, password `demo-password`) |
| `scripts/itest.sh` | end-to-end on the laptop: real MediaMTX, a fake camera, motion, cloud copy through rclone 1.50.1 (~10 min) |
| `scripts/installtest.sh [--quick]` | the installer: download plans, the release build, and install/upgrade/damaged download inside current Termux (docker) |
| `scripts/release.sh <version> [--publish]` | builds the release (arm, arm64, amd64) and with `--publish` creates the GitHub release |
| `scripts/deploy.sh` | builds for the phone and restarts it over SSH (development only) |

Design: `docs/superpowers/specs/2026-09-30-camorage-portal-design.md`.

## License

camorage is MIT-licensed (see `LICENSE`). The third-party parts below keep their own licenses.

## Third-party software

- [hls.js](https://github.com/video-dev/hls.js) 1.7.3 (Apache-2.0) is included in the web UI (`internal/web/ui/vendor/hls.min.js`).
- The installer downloads [MediaMTX](https://github.com/bluenviron/mediamtx) (MIT), [cloudflared](https://github.com/cloudflare/cloudflared) (Apache-2.0) and [Tailscale](https://github.com/tailscale/tailscale) (BSD-3-Clause) from their official releases.
- camorage uses [golang.org/x/crypto](https://pkg.go.dev/golang.org/x/crypto) (BSD-3-Clause).
