# M4 Portability Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Anyone with an Android phone and Termux can install or upgrade camorage with one command, on 32-bit or 64-bit ARM, on old or current Termux. The install is proven by a from-scratch reinstall on the owner's phone and by an automated install on current Termux.

**Architecture:**
- **`scripts/install.sh` runs inside Termux.** It:
  - maps the CPU to a build: `arm`, `arm64` or `amd64`;
  - installs the Termux packages it needs: `ffmpeg rclone proot`;
  - downloads camorage from the GitHub release, and MediaMTX, cloudflared and Tailscale from their own official releases;
  - checks every download against the release's `SHA256SUMS`;
  - only then stops the old portal, swaps in the new binaries, writes `start.sh` and a Termux:Boot script, and starts camorage.
- **`scripts/release.sh` runs on the laptop.** It:
  - builds camorage for the three CPUs, with the version compiled in;
  - fills the version into `install.sh`;
  - writes `SHA256SUMS`, downloading the pinned third-party files for that;
  - with `--publish`, creates the GitHub release.
  - `install.sh --dry-run` prints the download plan, and `release.sh` reads that plan, so the file names and URLs live in one place.
- **`scripts/installtest.sh` proves it on the laptop.**
  - Quick checks: the CPU mapping and refusals.
  - Release checks.
  - A full install, upgrade, damaged-download refusal and adapter tests inside Termux's official docker image (`termux/termux-docker:x86_64`, current Termux: ffmpeg 8.1.2, rclone 1.75.1), served from a local copy of the release.

**Tech Stack:**
- Go 1.27 (cross-builds `GOARCH=arm GOARM=7`, `arm64`, `amd64`) and Bash.
- Termux `pkg`, `curl` and `sha256sum`.
- Docker with `termux/termux-docker:x86_64` (laptop tests only).
- The `gh` CLI (publishing only; already signed in as pritamkarar).

**Spec:** `docs/superpowers/specs/2026-09-30-camorage-portal-design.md`.
- The M4 row of §10: "installer, arm64 builds, newer Termux (version-aware adapters), docs", exit "fresh install on a second phone".
- §2 platform, §3 build, §8 reliability (reboot), §9 testing.

Builds on `main` (M0–M3 merged).

**Owner's decisions (2026-09-30):**
- **No second phone.** The exit test is a from-scratch reinstall on this phone (Android 6, armv7, frozen 2019 Termux). arm64 and current Termux are tested on the laptop only (Task 5).
- **Public repo and one-line install.** The repo becomes public and releases are published on GitHub. Phones install with `curl -fsSL https://github.com/pritamkarar/camorage/releases/latest/download/install.sh | bash`.

**Order:** Tasks 1–7 run on branch `m4-portability`. Task 7 installs from a release served by the laptop, not GitHub. After Task 7 come the final whole-branch review and the merge. **Task 8 (publishing) runs after the merge**, and stops for the owner before every outward-facing step.

## Global Constraints

**Branch and code**
- Branch `m4-portability`, created from `main`. Commit after each task with the trailer `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.
- Module `camorage`, Go 1.27. No new Go dependencies.

**Builds and pins**
- Release builds: `CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=<v>"` for `GOOS=linux` with `GOARCH=arm GOARM=7`, `GOARCH=arm64` and `GOARCH=amd64`.
- Dev builds (`scripts/deploy.sh`, `go build`) report version `dev`.
- Pinned third-party versions, the ones the phone runs today:
  - MediaMTX `v1.21.1` (spec: "pin v1.21.x").
  - cloudflared `2026.9.3`.
  - Tailscale `1.102.4` (static builds from `pkgs.tailscale.com/stable`).
- Release asset names:
  - `camorage-linux-{arm,arm64,amd64}`, `install.sh`, `SHA256SUMS`.
  - Third-party names as upstream publishes them: `mediamtx_v1.21.1_linux_{armv7,arm64,amd64}.tar.gz`, `cloudflared-linux-{arm,arm64,amd64}`, `tailscale_1.102.4_{arm,arm64,amd64}.tgz`.

**Installer behaviour**
- CPU mapping from `uname -m`: `armv7*` and `armv8l` → `arm`; `aarch64` and `arm64` → `arm64`; `x86_64` → `amd64`.
  - `armv8l` is 32-bit Termux on a 64-bit phone; its `proot` is 32-bit too, so it gets the 32-bit build.
  - Anything else is refused with a message.
- **Install layout:** `~/camorage/bin/{camorage,mediamtx,cloudflared,tailscale,tailscaled}`, `~/camorage/start.sh`, `~/camorage/camorage.log`, `~/.termux/boot/camorage`.
- **Data:** the data dir `~/.camorage` and the recordings are never touched by the installer.
- **Stop rule:** the installer changes nothing until every download has passed its checksum. Its temp dir `$PREFIX/tmp/camorage-install.*` is always removed.
- **Secrets:** none in the installer, the release or the logs. The pre-publish audit (Task 8) found only fake test fixtures in the history.

**Phone dev access (unchanged)**
- `. spike/env.sh; $P '<cmd>'` over SSH (port 8022), and `scripts/deploy.sh`.
- The admin password is in `.cache/admin-password`.
- Destructive steps on the phone (moving the install aside, deleting old copies) wait for the owner's yes.

## Facts established before this plan (2026-09-30 probes)

**Current Termux** (`termux/termux-docker:x86_64`, image updated 2026-09-27):
- ffmpeg **8.1.2**, rclone **v1.75.1-termux**, `proot`, `curl`, `termux-wake-lock`. Runs as uid 1000.
- `HOME=/data/data/com.termux/files/home`.
- No `/system/bin/getprop`: Termux's `getprop` wrapper fails. `/etc/resolv.conf` exists inside docker.

**M3 storage code with rclone v1.75.1** (amd64, `RCLONE=` override): all 19 storage tests pass. rclone needs no version-specific code.

**Motion reader across ffmpeg versions:**
- `motion.ParseVersion` handles `4.2.1`, `6.1.1-3ubuntu5`, `n7.0.2` and unknown git builds (treated as newest).
- `ReaderArgs` uses `-timeout` and `-fps_mode passthrough` from 5.1 on.

**Third-party downloads:**
- All 9 URLs exist, for arm, arm64 and amd64.
- mediamtx's arm64 asset is `linux_arm64`, not `arm64v8`.
- cloudflared also has `-armhf`; we use `-arm`, which runs on every ARMv7.

**The phone** (Termux 0.119, curl 7.67.0, OpenSSL 1.1.1d):
- HTTPS to github.com and pkgs.tailscale.com works.
- GitHub release downloads follow the redirect to release-assets.githubusercontent.com at about 1 MB/s (cloudflared 36.5 MB in 36.5 s).
- `sha256sum`, `tar`, `bash`, `pgrep` and `nohup` are present.
- About 0.9 GB free internally.

**GitHub:**
- `pritamkarar/camorage` is **PRIVATE**. `gh` is signed in as pritamkarar. Local `main` is 64 commits ahead of `origin/main`.

**History audit:**
- **No real secrets.** The matches are the fixtures `GOCSPX-secret`, `cid.apps.googleusercontent.com`, `ya29.a`, `1//r`, and the UI hint `eyJhIjoi…`.
- **Personal details:**
  - `example.com` (15 times), tailnet `tail0a1b2c` and `100.64.0.7` (19 each), `192.168.1.x` (78), adb serial `<adb-serial>` (8).
  - `~` paths in docs and plans.
- **Vendored third-party code:** `internal/web/ui/vendor/hls.min.js` is hls.js **1.7.3** (Apache-2.0).

## Review Focus

1. **Re-running the installer where camorage already runs (upgrade).**
   - Settings and recordings are kept, the old portal is replaced, and exactly one portal runs afterwards.
   - → Task 5, the upgrade step in `installtest.sh`.
2. **A damaged or tampered download, or a connection that drops mid-install.**
   - The installer stops with a clear message before touching the running install, and leaves no temp files.
   - → Task 5, the damaged-release step (checksum refusal, same portal still running, temp dir gone).
3. **32-bit Termux on a 64-bit phone (`armv8l`), and CPUs camorage has no build for.**
   - The first gets the 32-bit build; the others get a clear refusal, with nothing changed.
   - → Task 3, quick checks; Task 5, the `i686` refusal inside Termux.
4. **Current Termux's ffmpeg 8.x rejecting the motion reader's options, or current rclone behaving differently.**
   - Motion detection and cloud copy must work on today's Termux.
   - → Task 2, `TestReaderArgsAgainstInstalledFFmpeg`, run in Task 5 against ffmpeg 8.1.2 and in Task 7 against the phone's 4.2.1. The storage tests run against rclone 1.75.1 (Task 5) and 1.50.1 (Task 7).
5. **The installer run outside Termux** (on a laptop, or with `curl | bash` in the wrong shell).
   - A clear message and nothing written.
   - → Task 3, quick checks, with a temporary `HOME` asserted untouched.

---

### Task 1: The version, compiled in and shown

**Files:**
- Modify: `cmd/camorage/main.go`, `internal/web/web.go`, `internal/web/ui/settings.js`
- Test: `internal/web/web_test.go`

**Interfaces:**
- Produces (Tasks 3, 4, 5 and 7 use these):
  - `var version = "dev"` in package main, set with `-ldflags "-X main.version=<v>"`.
  - `camorage -version` prints the version and exits 0.
  - `web.Deps.Version string`.
  - `GET /api/status` gains `"version"`. It is signed-in only; `/api/health` stays as it is.

- [ ] **Step 1: Create the branch**

Run: `cd ~/src/camorage && git switch -c m4-portability && git branch --show-current`
Expected: `m4-portability`

- [ ] **Step 2: Write the failing test** (append to `internal/web/web_test.go`)

```go
func TestStatusReportsVersion(t *testing.T) {
	e := newEnv(t, func(d *Deps) { d.Version = "0.4.0" })
	e.setUp()
	if st := decode[map[string]any](t, e.do("GET", "/api/status", "")); st["version"] != "0.4.0" {
		t.Fatalf("version %v", st["version"])
	}
}
```

- [ ] **Step 3: Run it to verify it fails**

Run: `go test ./internal/web/ -run TestStatusReportsVersion 2>&1 | tail -3`
Expected: build failure, `d.Version undefined`.

- [ ] **Step 4: Implement**

In `internal/web/web.go`:
1. Add to `Deps`, after the `Usage` field:
   ```go
   	Version string // camorage's version (release builds set it with -ldflags -X main.version)
   ```
2. In `status`, change `out := map[string]any{"time": now.Format(time.RFC3339), "cameras": cams, "health": s.d.Health(cfg.RecDir), "processes": s.d.Processes()}` to:
   ```go
   	out := map[string]any{"time": now.Format(time.RFC3339), "cameras": cams, "health": s.d.Health(cfg.RecDir), "processes": s.d.Processes(), "version": s.d.Version}
   ```
3. Run `gofmt -w internal/web/web.go`.

In `cmd/camorage/main.go`:
1. Add `"fmt"` to the imports.
2. Add above `func main()`:
   ```go
   // version is set by release builds (scripts/release.sh: -ldflags "-X main.version=<v>").
   var version = "dev"
   ```
3. Add `showVersion := flag.Bool("version", false, "print the version and exit")` after the `mtxBin` flag.
4. Right after `flag.Parse()`, add:
   ```go
   	if *showVersion {
   		fmt.Println(version)
   		return
   	}
   ```
5. In `web.Deps`, add after the `Cloud: cloud, Usage: …` line:
   ```go
   			Version: version,
   ```

In `internal/web/ui/settings.js`, in `refresh()`, make the first row of `healthBox.replaceChildren(dl([` this:

```js
        ['camorage version', st.version || 'dev'],
```

- [ ] **Step 5: Run the tests, the build and the flag**

Run: `gofmt -l cmd internal ; go vet ./... && go test -race ./internal/web/ && go build -o /tmp/claude-1000/-home-user-src-camorage/62a92098-e579-4c6b-b46c-ed0f9a3bbd32/scratchpad/camo -ldflags "-X main.version=9.9.9" ./cmd/camorage && /tmp/claude-1000/-home-user-src-camorage/62a92098-e579-4c6b-b46c-ed0f9a3bbd32/scratchpad/camo -version; node --check internal/web/ui/settings.js && echo syntax-ok`
Expected: `ok  camorage/internal/web`, then `9.9.9`, then `syntax-ok`.

- [ ] **Step 6: Commit**

```bash
git add cmd/camorage/main.go internal/web/ docs/superpowers/plans/2026-09-30-m4-portability.md
git commit -m "feat: camorage -version, and the version on the Settings page

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 2: The motion reader's options, checked against the installed ffmpeg

**Files:**
- Test: `internal/motion/reader_test.go`
- Test: `internal/storage/rclone_test.go` (the fake rclone of `TestRcloneGivesUpQuickly`)

**Interfaces:**
- Consumes: `ParseVersion(out string) (major, minor int)`, `ReaderArgs(major, minor int, url string) []string` (M2).
- Produces: `TestReaderArgsAgainstInstalledFFmpeg`. Task 5 runs it against Termux's ffmpeg 8.1.2, and Task 7 against the phone's 4.2.1.

- [ ] **Step 1: Write the test** (in `internal/motion/reader_test.go`, add `"context"`, `"fmt"`, `"net"`, `"os"`, `"os/exec"` and `"time"` to the imports, then append)

```go
// The keyframe reader's options must be ones the installed ffmpeg accepts: Termux ships whatever
// its repo has (4.2 on Android 5/6, 8.x today). Pointed at a closed port, an ffmpeg that took every
// option fails to connect; one that did not complains about the option before trying.
// FFMPEG_ASSUME=4.2 builds the options for another version (it shows the test catches a mismatch).
func TestReaderArgsAgainstInstalledFFmpeg(t *testing.T) {
	out, err := exec.Command("ffmpeg", "-version").Output()
	if err != nil {
		t.Skip("no ffmpeg on PATH")
	}
	major, minor := ParseVersion(string(out))
	if v := os.Getenv("FFMPEG_ASSUME"); v != "" {
		fmt.Sscanf(v, "%d.%d", &major, &minor)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close() // now nothing listens there
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "ffmpeg", ReaderArgs(major, minor, "rtsp://"+addr+"/cam")...)
	cmd.Stderr = &stderr
	cmd.Run()
	msg := stderr.String()
	if strings.Contains(msg, "Unrecognized option") || strings.Contains(msg, "Option not found") || !strings.Contains(msg, "Connection refused") {
		t.Fatalf("ffmpeg (options for %d.%d) did not accept the reader's options:\n%s", major, minor, msg)
	}
}
```

- [ ] **Step 2: Show the test catches a mismatch**

The laptop's ffmpeg is 6.1, and the 4.x options include `-stimeout`, which ffmpeg 5 removed.
Run: `FFMPEG_ASSUME=4.2 go test ./internal/motion/ -run TestReaderArgsAgainstInstalledFFmpeg 2>&1 | grep -E "stimeout|FAIL" | head -3`
Expected: a failure that mentions `stimeout` (`Unrecognized option 'stimeout'`).

- [ ] **Step 3: Run it for the installed ffmpeg, then the package**

Run: `gofmt -l internal ; go test -race ./internal/motion/ -run TestReaderArgsAgainstInstalledFFmpeg -v 2>&1 | grep -E "^(---|ok)"; go test -race ./internal/motion/`
Expected: `--- PASS: TestReaderArgsAgainstInstalledFFmpeg`, then two `ok` lines.

- [ ] **Step 4: Make the fake rclone of `TestRcloneGivesUpQuickly` run on Android**

Tasks 5 and 7 run the storage tests inside Termux. Android has no `/bin/sh`, and a static Go binary starts the script directly, without Termux's shebang rewriting. So the fake must use the `sh` found on PATH (Termux: `$PREFIX/bin/sh`).

In `internal/storage/rclone_test.go`:
1. Add `"os/exec"` to the imports.
2. In `TestRcloneGivesUpQuickly`, replace the line `os.WriteFile(bin, []byte("#!/bin/sh\necho \"$@\" > \"$0.args\"\n"), 0o700)` with:

```go
	sh, err := exec.LookPath("sh") // Android has no /bin/sh; Termux's is $PREFIX/bin/sh
	if err != nil {
		t.Skip("no sh on PATH")
	}
	os.WriteFile(bin, []byte("#!"+sh+"\necho \"$@\" > \"$0.args\"\n"), 0o700)
```

Run: `gofmt -l internal ; go test -race ./internal/storage/ -run TestRcloneGivesUpQuickly -v 2>&1 | grep -E "^(---|ok)"`
Expected: `--- PASS: TestRcloneGivesUpQuickly`, then `ok`. The case this fixes, no `/bin/sh`, only exists inside Termux: Task 5 runs this test there, and Task 7 runs it on the phone.

- [ ] **Step 5: Commit**

```bash
git add internal/motion/reader_test.go internal/storage/rclone_test.go
git commit -m "test: the keyframe reader's options are checked against the installed ffmpeg; the fake rclone runs on Android

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 3: The installer

**Files:**
- Create: `scripts/install.sh`, `scripts/installtest.sh`

**Interfaces:**
- Consumes: `camorage -version` (Task 1).
- Produces (Tasks 4, 5, 7 and 8 use these):
  - `bash scripts/install.sh --dry-run` prints `<file> <url>`, one line per download, in the order: camorage, mediamtx, cloudflared, tailscale.
  - Environment overrides, for tests: `CAMORAGE_ARCH=<uname -m value>`, `CAMORAGE_BASE=<release URL>`, `CAMORAGE_VERSION=<v>`.
  - `@VERSION@` is filled in by `release.sh`.
  - `scripts/installtest.sh --quick` runs the checks that need neither a build nor docker.

- [ ] **Step 1: Write the failing checks** (`scripts/installtest.sh`)

```bash
#!/usr/bin/env bash
# Installer tests. --quick: the download plan per CPU and the refusals (seconds, no network).
# Without --quick also the release (scripts/release.sh) and a real install, upgrade and damaged
# download inside current Termux (termux/termux-docker:x86_64; needs docker and internet, ~6 min).
set -euo pipefail
cd "$(dirname "$0")/.."
fail() { echo "INSTALLTEST FAIL: $*"; exit 1; }

plan() { CAMORAGE_ARCH=$1 CAMORAGE_VERSION=9.9.9 CAMORAGE_BASE=https://example.test/r bash scripts/install.sh --dry-run; }
names() { plan "$1" | awk '{print $1}' | tr '\n' ' '; }
ARM="camorage-linux-arm mediamtx_v1.21.1_linux_armv7.tar.gz cloudflared-linux-arm tailscale_1.102.4_arm.tgz "
[ "$(names armv7l)" = "$ARM" ] || fail "armv7l plan: $(names armv7l)"
[ "$(names armv8l)" = "$ARM" ] || fail "armv8l (32-bit Termux on a 64-bit phone) plan: $(names armv8l)"
[ "$(names aarch64)" = "camorage-linux-arm64 mediamtx_v1.21.1_linux_arm64.tar.gz cloudflared-linux-arm64 tailscale_1.102.4_arm64.tgz " ] || fail "aarch64 plan: $(names aarch64)"
[ "$(names x86_64)" = "camorage-linux-amd64 mediamtx_v1.21.1_linux_amd64.tar.gz cloudflared-linux-amd64 tailscale_1.102.4_amd64.tgz " ] || fail "x86_64 plan: $(names x86_64)"
[[ $(plan aarch64 | sed -n 1p) == "camorage-linux-arm64 https://example.test/r/camorage-linux-arm64" ]] || fail "camorage URL: $(plan aarch64 | sed -n 1p)"
[[ $(plan aarch64 | sed -n 2p) == *"github.com/bluenviron/mediamtx/releases/download/v1.21.1/mediamtx_v1.21.1_linux_arm64.tar.gz" ]] || fail "mediamtx URL"
[[ $(plan aarch64 | sed -n 3p) == *"github.com/cloudflare/cloudflared/releases/download/2026.9.3/cloudflared-linux-arm64" ]] || fail "cloudflared URL"
[[ $(plan aarch64 | sed -n 4p) == *"pkgs.tailscale.com/stable/tailscale_1.102.4_arm64.tgz" ]] || fail "tailscale URL"
OUT=$(CAMORAGE_ARCH=i686 bash scripts/install.sh --dry-run 2>&1) && fail "i686 accepted"
[[ $OUT == *"not supported"* ]] || fail "i686 message: $OUT"
H=$(mktemp -d)
OUT=$(env -u PREFIX HOME="$H" bash scripts/install.sh 2>&1) && fail "installed outside Termux"
[[ $OUT == *"run it in Termux"* ]] || fail "outside-Termux message: $OUT"
[ -z "$(ls -A "$H")" ] || fail "wrote into HOME outside Termux: $(ls -A "$H")"
rmdir "$H"
shellcheck scripts/install.sh scripts/installtest.sh || fail "shellcheck"
echo "ok: download plans per CPU, refusals, shellcheck"
[ "${1:-}" = --quick ] && exit 0

echo "INSTALLTEST PASS"
```

- [ ] **Step 2: Run them to verify they fail**

Run: `chmod +x scripts/installtest.sh && scripts/installtest.sh --quick; echo "exit=$?"`
Expected: `INSTALLTEST FAIL: armv7l plan: ` (there is no install.sh yet), then `exit=1`.

- [ ] **Step 3: Implement** (`scripts/install.sh`)

```bash
#!/usr/bin/env bash
# Install or upgrade camorage in Termux on an Android phone:
#   curl -fsSL https://github.com/pritamkarar/camorage/releases/latest/download/install.sh | bash
# Settings (~/.camorage) and recordings are kept. Nothing changes until every download has passed
# its checksum. Tests: CAMORAGE_ARCH (a uname -m value), CAMORAGE_BASE, CAMORAGE_VERSION, --dry-run.
set -euo pipefail

VERSION=${CAMORAGE_VERSION:-@VERSION@}
BASE=${CAMORAGE_BASE:-https://github.com/pritamkarar/camorage/releases/download/v$VERSION}
MEDIAMTX=v1.21.1
CLOUDFLARED=2026.9.3
TAILSCALE=1.102.4
DIR=$HOME/camorage

say() { printf '\033[1m%s\033[0m\n' "$*"; }
die() {
	printf 'camorage install: %s\n' "$*" >&2
	exit 1
}

# arch maps the CPU to a camorage build. armv8l is 32-bit Termux on a 64-bit phone: its proot is
# 32-bit too, so it gets the 32-bit build.
arch() {
	case ${CAMORAGE_ARCH:-$(uname -m)} in
	armv7* | armv8l) echo arm ;;
	aarch64 | arm64) echo arm64 ;;
	x86_64) echo amd64 ;;
	*) return 1 ;;
	esac
}

# plan prints "<file> <url>" for each download for build $1.
plan() {
	local mtx=$1
	[ "$1" = arm ] && mtx=armv7
	echo "camorage-linux-$1 $BASE/camorage-linux-$1"
	echo "mediamtx_${MEDIAMTX}_linux_$mtx.tar.gz https://github.com/bluenviron/mediamtx/releases/download/$MEDIAMTX/mediamtx_${MEDIAMTX}_linux_$mtx.tar.gz"
	echo "cloudflared-linux-$1 https://github.com/cloudflare/cloudflared/releases/download/$CLOUDFLARED/cloudflared-linux-$1"
	echo "tailscale_${TAILSCALE}_$1.tgz https://pkgs.tailscale.com/stable/tailscale_${TAILSCALE}_$1.tgz"
}

A=$(arch) || die "this phone's CPU (${CAMORAGE_ARCH:-$(uname -m)}) is not supported: camorage runs on ARM (32- or 64-bit) and x86_64"
if [ "${1:-}" = --dry-run ]; then
	plan "$A"
	exit 0
fi
[[ ${PREFIX:-} == */com.termux/files/usr ]] || die "this installs camorage on an Android phone: run it in Termux"

need=()
for p in ffmpeg rclone proot curl; do
	command -v "$p" >/dev/null || need+=("$p")
done
if [ ${#need[@]} -gt 0 ]; then
	say "Installing Termux packages: ${need[*]}"
	pkg install -y "${need[@]}"
fi

tmp=$(mktemp -d "$PREFIX/tmp/camorage-install.XXXXXX")
trap 'rm -rf "$tmp"' EXIT
say "Downloading camorage $VERSION ($A)"
curl -fsSL --retry 3 --connect-timeout 20 -o "$tmp/SHA256SUMS" "$BASE/SHA256SUMS" || die "could not download $BASE/SHA256SUMS"
while read -r f url; do
	printf '  %s\n' "$f"
	curl -fsSL --retry 3 --connect-timeout 20 -o "$tmp/$f" "$url" || die "could not download $url"
done < <(plan "$A")
awk 'NR == FNR { want[$1]; next } ($2 in want)' <(plan "$A") "$tmp/SHA256SUMS" >"$tmp/want.sha256"
[ "$(wc -l <"$tmp/want.sha256")" -eq "$(plan "$A" | wc -l)" ] || die "SHA256SUMS of $VERSION does not list every download"
(cd "$tmp" && sha256sum -c --quiet want.sha256) >/dev/null 2>&1 ||
	die "a download is damaged or was changed (checksum mismatch); nothing was installed"

mkdir -p "$tmp/bin"
cp "$tmp/camorage-linux-$A" "$tmp/bin/camorage"
cp "$tmp/cloudflared-linux-$A" "$tmp/bin/cloudflared"
tar -xzf "$tmp"/mediamtx_*.tar.gz -C "$tmp/bin" mediamtx
tar -xzf "$tmp/tailscale_${TAILSCALE}_$A.tgz" -C "$tmp"
cp "$tmp/tailscale_${TAILSCALE}_$A/tailscale" "$tmp/tailscale_${TAILSCALE}_$A/tailscaled" "$tmp/bin/"
chmod 755 "$tmp"/bin/*
"$tmp/bin/camorage" -version >/dev/null 2>&1 || die "the downloaded camorage does not run on this phone"

say "Installing into $DIR"
mkdir -p "$DIR/bin"
if pgrep -x camorage >/dev/null; then
	pkill -x camorage || true
	for _ in $(seq 1 40); do
		pgrep -x camorage >/dev/null || break
		sleep 0.5
	done
fi
mv -f "$tmp"/bin/* "$DIR/bin/"
cat >"$DIR/start.sh" <<EOF
#!$PREFIX/bin/bash
# Start camorage in the background unless it is running. Termux:Boot runs this at boot (Android 7+);
# on Android 5/6 open Termux and run it by hand after a reboot.
cd "$DIR" && { pgrep -x camorage >/dev/null || ( nohup bin/camorage > camorage.log 2>&1 </dev/null & ); }
EOF
chmod 755 "$DIR/start.sh"
mkdir -p "$HOME/.termux/boot"
printf '#!%s/bin/sh\ntermux-wake-lock\n%s/start.sh\n' "$PREFIX" "$DIR" >"$HOME/.termux/boot/camorage"
chmod 755 "$HOME/.termux/boot/camorage"
"$DIR/start.sh"

for _ in $(seq 1 40); do
	curl -fsS -m 2 http://127.0.0.1:8080/api/health >/dev/null 2>&1 && break
	sleep 0.5
done
curl -fsS -m 2 http://127.0.0.1:8080/api/health >/dev/null 2>&1 || die "camorage did not start: see $DIR/camorage.log"
ip=$(ip -4 -o addr show wlan0 2>/dev/null | awk '{ split($4, a, "/"); print a[1]; exit }') || ip=
say "camorage $VERSION is running."
echo "Open http://${ip:-<this phone's Wi-Fi address>}:8080 in a browser on the same Wi-Fi to set it up."
sdk=$(getprop ro.build.version.sdk 2>/dev/null) || sdk=
if [ "${sdk:-0}" -ge 24 ] 2>/dev/null; then
	echo "To start it automatically after a reboot, install the Termux:Boot app and open it once."
else
	echo "After a reboot, open Termux and run: ~/camorage/start.sh"
fi
```

- [ ] **Step 4: Run the quick checks**

Run: `scripts/installtest.sh --quick; echo "exit=$?"`
Expected: `ok: download plans per CPU, refusals, shellcheck`, then `exit=0`.

- [ ] **Step 5: Commit**

```bash
git add scripts/install.sh scripts/installtest.sh
git commit -m "feat: install.sh installs or upgrades camorage in Termux, checking every download

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 4: The release build

**Files:**
- Create: `scripts/release.sh`
- Modify: `scripts/installtest.sh`

**Interfaces:**
- Consumes: `install.sh --dry-run` (Task 3), `-X main.version` (Task 1).
- Produces (Tasks 5, 7 and 8 use these):
  - `scripts/release.sh <version> [--publish]` builds into `.cache/release/v<version>/`: `camorage-linux-{arm,arm64,amd64}`, `install.sh` with the version filled in, and `SHA256SUMS`.
  - `SHA256SUMS` covers those four files and the 9 pinned third-party downloads, which are cached in `.cache/thirdparty/`.
  - `--publish` runs `gh release create v<version>` with the camorage binaries, `install.sh` and `SHA256SUMS`.

- [ ] **Step 1: Write the failing checks** (in `scripts/installtest.sh`, insert before the final `echo "INSTALLTEST PASS"`)

```bash
# the release: binaries for each CPU with the version compiled in, install.sh with it filled in,
# and checksums for everything install.sh downloads
V=0.0.0-test
R=.cache/release/v$V
scripts/release.sh "$V" >/dev/null || fail "release.sh"
for a in arm arm64 amd64; do [ -s "$R/camorage-linux-$a" ] || fail "no camorage-linux-$a"; done
[ "$("$R/camorage-linux-amd64" -version)" = "$V" ] || fail "version not compiled in"
file "$R/camorage-linux-arm" | grep "ARM, EABI5" >/dev/null || fail "arm build: $(file "$R/camorage-linux-arm")"
file "$R/camorage-linux-arm64" | grep "ARM aarch64" >/dev/null || fail "arm64 build: $(file "$R/camorage-linux-arm64")"
grep -q "^VERSION=\${CAMORAGE_VERSION:-$V}$" "$R/install.sh" || fail "version not filled into install.sh"
[ "$(wc -l <"$R/SHA256SUMS")" = 13 ] || fail "SHA256SUMS has $(wc -l <"$R/SHA256SUMS") lines, want 13"
(cd "$R" && grep -E ' (camorage-linux-|install.sh)' SHA256SUMS | sha256sum -c --quiet) || fail "camorage checksums"
(cd .cache/thirdparty && grep -vE ' (camorage-linux-|install.sh)' "../../$R/SHA256SUMS" | sha256sum -c --quiet) || fail "third-party checksums"
echo "ok: release built for arm, arm64 and amd64, with checksums"
```

- [ ] **Step 2: Run them to verify they fail**

Run: `scripts/installtest.sh; echo "exit=$?"`
Expected: `ok: download plans per CPU, refusals, shellcheck`, then `INSTALLTEST FAIL: release.sh` (there is no release.sh yet), then `exit=1`.

- [ ] **Step 3: Implement** (`scripts/release.sh`)

```bash
#!/usr/bin/env bash
# Build a camorage release into .cache/release/v<version>: camorage for 32- and 64-bit ARM and
# x86_64 (version compiled in), install.sh with the version filled in, and SHA256SUMS covering
# them and every third-party file install.sh downloads (from install.sh --dry-run, one place).
#   scripts/release.sh 0.4.0             build only
#   scripts/release.sh 0.4.0 --publish   also create GitHub release v0.4.0 (public once published)
set -euo pipefail
cd "$(dirname "$0")/.."
V=${1:?usage: scripts/release.sh <version> [--publish]}
V=${V#v}
OUT=.cache/release/v$V
TP=.cache/thirdparty
rm -rf "$OUT"
mkdir -p "$OUT" "$TP"
sed "s/@VERSION@/$V/" scripts/install.sh >"$OUT/install.sh"
: >"$OUT/SHA256SUMS"
for m in armv7l aarch64 x86_64; do
	while read -r f url; do
		case $f in
		camorage-linux-*)
			a=${f#camorage-linux-}
			CGO_ENABLED=0 GOOS=linux GOARCH=$a GOARM=7 go build -trimpath -ldflags "-s -w -X main.version=$V" -o "$OUT/$f" ./cmd/camorage
			(cd "$OUT" && sha256sum "$f") >>"$OUT/SHA256SUMS"
			;;
		*)
			if [ ! -s "$TP/$f" ]; then # downloaded once, kept only when complete
				curl -fsSL --retry 3 -o "$TP/$f.part" "$url"
				mv "$TP/$f.part" "$TP/$f"
			fi
			(cd "$TP" && sha256sum "$f") >>"$OUT/SHA256SUMS"
			;;
		esac
	done < <(CAMORAGE_ARCH=$m bash "$OUT/install.sh" --dry-run)
done
(cd "$OUT" && sha256sum install.sh) >>"$OUT/SHA256SUMS"
echo "built $OUT"
if [ "${2:-}" = --publish ]; then
	gh release create "v$V" "$OUT"/camorage-linux-* "$OUT/install.sh" "$OUT/SHA256SUMS" \
		--title "camorage $V" \
		--notes "Install or upgrade in Termux on an Android phone:

\`\`\`
curl -fsSL https://github.com/pritamkarar/camorage/releases/latest/download/install.sh | bash
\`\`\`"
fi
```

Add `scripts/release.sh` to the `shellcheck` line in `installtest.sh`: `shellcheck scripts/install.sh scripts/installtest.sh scripts/release.sh || fail "shellcheck"`.

- [ ] **Step 4: Run the checks**

Run: `chmod +x scripts/release.sh && scripts/installtest.sh; echo "exit=$?"`
Expected: `ok: download plans per CPU, refusals, shellcheck`, `ok: release built for arm, arm64 and amd64, with checksums`, `INSTALLTEST PASS`, `exit=0`. The first run downloads about 270 MB of third-party files into `.cache/thirdparty`.

- [ ] **Step 5: Commit**

```bash
git add scripts/release.sh scripts/installtest.sh
git commit -m "feat: release.sh builds camorage for arm, arm64 and amd64 with checksums for every download

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 5: Install, upgrade and a damaged download on current Termux

**Files:**
- Modify: `scripts/installtest.sh`

**Interfaces:**
- Consumes:
  - The release (Task 4) and `install.sh` (Task 3).
  - `TestReaderArgsAgainstInstalledFFmpeg` (Task 2), and the storage tests with `RCLONE=` (M3).
  - `POST /api/setup {password, recDir}` (M1).
- Produces: the full `scripts/installtest.sh`. Task 7 runs its release step before the phone install.

- [ ] **Step 1: Write the failing end-to-end part** (in `scripts/installtest.sh`, insert before the final `echo "INSTALLTEST PASS"`)

```bash
# current Termux (termux-docker x86_64: ffmpeg 8.x, rclone 1.7x) installs from a locally served
# release, is set up, upgraded in place, refuses a damaged release and an unknown CPU, and runs
# the motion reader and rclone tests against its own ffmpeg and rclone
T=.cache/installtest
rm -rf "$T" .cache/release/v$V-bad
mkdir -p "$T"
CGO_ENABLED=0 go test -c -o "$T/motion.test" ./internal/motion
CGO_ENABLED=0 go test -c -o "$T/storage.test" ./internal/storage
cp -r "$R" ".cache/release/v$V-bad"
printf 'x' >>".cache/release/v$V-bad/camorage-linux-amd64" # damaged in transit
GW=$(docker network inspect bridge -f '{{(index .IPAM.Config 0).Gateway}}')
python3 -m http.server 18765 --bind "$GW" --directory .cache/release >/dev/null 2>&1 &
SRV=$!
trap 'kill $SRV 2>/dev/null || true' EXIT
# shellcheck disable=SC2016 # the script below runs inside the container: its $ expand there
docker run --rm -v "$PWD/$T:/tests:ro" -e BASE="http://$GW:18765/v$V" -e BAD="http://$GW:18765/v$V-bad" -e V="$V" \
	termux/termux-docker:x86_64 bash -c '
set -euo pipefail
fail() { echo "INSTALLTEST FAIL: $*"; tail -20 ~/camorage/camorage.log 2>/dev/null || true; exit 1; }
leftovers() { find "$PREFIX/tmp" -maxdepth 1 -name "camorage-install.*" | wc -l; }
curl -fsSL "$BASE/install.sh" | CAMORAGE_BASE=$BASE bash >~/install1.log 2>&1 || { cat ~/install1.log; fail "fresh install"; }
[ "$(~/camorage/bin/camorage -version)" = "$V" ] || fail "installed version"
curl -fsS localhost:8080/api/health | grep "\"setupDone\":false" >/dev/null || fail "a fresh install is not waiting for setup"
curl -fsS -c ~/jar -H "Origin: http://localhost:8080" -H "Content-Type: application/json" \
	-d "{\"password\":\"correct-horse-battery\",\"recDir\":\"$HOME/rec\"}" localhost:8080/api/setup | grep "\"ok\":true" >/dev/null || fail "first-run setup"
for _ in $(seq 1 20); do pgrep -x mediamtx >/dev/null && break; sleep 1; done
pgrep -x mediamtx >/dev/null || fail "MediaMTX did not start after setup"
~/camorage/bin/mediamtx --version | grep v1.21.1 >/dev/null || fail "mediamtx version"
~/camorage/bin/cloudflared --version | grep 2026.9.3 >/dev/null || fail "cloudflared version"
~/camorage/bin/tailscale version | sed -n 1p | grep 1.102.4 >/dev/null || fail "tailscale version"
[ -x ~/.termux/boot/camorage ] && [ -x ~/camorage/start.sh ] || fail "no start.sh or Termux:Boot script"
[ "$(leftovers)" = 0 ] || fail "temp files left after a successful install"
echo "ok: fresh install on current Termux, set up, MediaMTX running"
P1=$(pgrep -x camorage)
curl -fsSL "$BASE/install.sh" | CAMORAGE_BASE=$BASE bash >~/install2.log 2>&1 || { cat ~/install2.log; fail "upgrade"; }
P2=$(pgrep -x camorage)
[ "$P2" != "$P1" ] && [ "$(pgrep -xc camorage)" = 1 ] || fail "upgrade did not replace the portal (before $P1, after $P2)"
curl -fsS localhost:8080/api/health | grep "\"setupDone\":true" >/dev/null || fail "upgrade lost the settings"
echo "ok: upgrade in place keeps the settings, one portal running"
if curl -fsSL "$BAD/install.sh" | CAMORAGE_BASE=$BAD bash >~/install3.log 2>&1; then fail "a damaged release was installed"; fi
grep -q "checksum mismatch" ~/install3.log || { cat ~/install3.log; fail "no checksum message"; }
[ "$(pgrep -x camorage)" = "$P2" ] || fail "a failed install stopped the running portal"
[ "$(leftovers)" = 0 ] || fail "temp files left after a failed install"
if curl -fsSL "$BASE/install.sh" | CAMORAGE_ARCH=i686 CAMORAGE_BASE=$BASE bash >~/install4.log 2>&1; then fail "i686 accepted"; fi
grep -q "not supported" ~/install4.log && [ "$(pgrep -x camorage)" = "$P2" ] || fail "unknown CPU: $(cat ~/install4.log)"
echo "ok: a damaged release and an unknown CPU change nothing"
/tests/motion.test -test.run TestReaderArgsAgainstInstalledFFmpeg -test.v 2>&1 | grep "^--- PASS" >/dev/null || fail "motion reader against $(ffmpeg -version | sed -n 1p)"
RCLONE=$(command -v rclone) /tests/storage.test -test.run "TestRclone|TestUploader|TestRetention" -test.v >~/storage.log 2>&1 || { cat ~/storage.log; fail "storage tests against $(rclone version | sed -n 1p)"; }
grep -q "^--- SKIP" ~/storage.log && fail "storage tests skipped"
echo "ok: motion reader and rclone adapter work with $(ffmpeg -version | sed -n 1p | cut -d" " -f1-3) and $(rclone version | sed -n 1p)"
' || fail "inside current Termux"
```

- [ ] **Step 2: Run it to verify it catches a broken installer**

Temporarily break the installer's upgrade, then check the test notices. In `scripts/install.sh`, change `pkill -x camorage || true` to `true # pkill -x camorage || true`.
Run: `scripts/installtest.sh > /tmp/claude-1000/-home-user-src-camorage/62a92098-e579-4c6b-b46c-ed0f9a3bbd32/scratchpad/installtest.log 2>&1; echo "exit=$?"; grep -E "^(ok|INSTALLTEST)" /tmp/claude-1000/-home-user-src-camorage/62a92098-e579-4c6b-b46c-ed0f9a3bbd32/scratchpad/installtest.log`
Expected: `exit=1` and `INSTALLTEST FAIL: upgrade did not replace the portal …`, after the `ok: fresh install …` line.
Then restore `pkill -x camorage || true`.

- [ ] **Step 3: Run the whole test**

Run the same command again (never pipe it to `tail`: that hides the exit code).
Expected:
- `exit=0`.
- `ok: download plans per CPU, refusals, shellcheck`
- `ok: release built for arm, arm64 and amd64, with checksums`
- `ok: fresh install on current Termux, set up, MediaMTX running`
- `ok: upgrade in place keeps the settings, one portal running`
- `ok: a damaged release and an unknown CPU change nothing`
- `ok: motion reader and rclone adapter work with ffmpeg version 8.1.2 and rclone v1.75.1-termux`
- `INSTALLTEST PASS`

- [ ] **Step 4: Commit**

```bash
git add scripts/installtest.sh scripts/install.sh
git commit -m "test: install, upgrade, damaged download and adapters on current Termux (termux-docker)

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 6: Docs, and retiring the old scripts

**Files:**
- Create: `README.md`
- Delete: `camorage.sh`, `setup.sh` (the old Drive uploader, retired in M3)
- Modify: `scripts/deploy.sh` (header comment), the spec (§2, §3, §8, §9)

**Interfaces:**
- Consumes: the install command, the layout and the test scripts (Tasks 1–5).
- Produces: the README that Task 8 publishes.

- [ ] **Step 1: Check the old scripts are unused**

Run: `grep -rn "camorage\.sh\|setup\.sh" --include=*.go --include=*.sh --include=*.js scripts cmd internal; echo "refs=$?"`
Expected: no lines, and `refs=1`. Only the spec and plans mention them, as history.

- [ ] **Step 2: Write `README.md`**

````markdown
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

## Cloud copy

Storage → add a target, then pick it in a camera's settings (Cloud copy: where, and how many days to keep).

- **Google Drive:** in Google Cloud Console create an OAuth client of type *Desktop app* with the Google Drive API enabled, and publish its consent screen ("In production": while it says "Testing", Google ends the sign-in after 7 days). In camorage, Add Google Drive → paste the client ID and secret → Sign in with Google → paste back the address of the page that does not load. Clips go to a `camorage` folder; camorage can only see files it created.
- **S3-compatible:** Amazon S3, or "Other" with an endpoint (Backblaze B2, Cloudflare R2, Wasabi, MinIO); the bucket must exist.

Continuous cameras upload each clock hour a couple of minutes after it ends; motion cameras upload each event. Copying starts when you turn it on (earlier footage stays on the phone).

## Troubleshooting

- Logs: `~/camorage/camorage.log` (the portal) and `~/.camorage/logs/` (MediaMTX, motion readers, tunnels). Settings shows each background program's state and restarts.
- Stop: `pkill -x camorage`. Start: `~/camorage/start.sh`.
- Uninstall: `pkill -x camorage; rm -rf ~/camorage ~/.camorage ~/.termux/boot/camorage`, then delete the recordings folder shown on the Storage page.

## Development

Built on a laptop with Go 1.27; the phone only runs the result.

| Command | What it does |
|---|---|
| `go test -race ./...` | unit tests |
| `node --test internal/web/ui/lib.test.mjs` | UI helper tests |
| `scripts/itest.sh` | end-to-end on the laptop: real MediaMTX, a fake camera, motion, cloud copy through rclone 1.50.1 (~10 min) |
| `scripts/installtest.sh [--quick]` | the installer: download plans, the release build, and install/upgrade/damaged download inside current Termux (docker) |
| `scripts/release.sh <version> [--publish]` | builds the release (arm, arm64, amd64) and with `--publish` creates the GitHub release |
| `scripts/deploy.sh` | builds for the phone and restarts it over SSH (development only) |

Design: `docs/superpowers/specs/2026-09-30-camorage-portal-design.md`.

## Third-party software

- [hls.js](https://github.com/video-dev/hls.js) 1.7.3 (Apache-2.0) is included in the web UI (`internal/web/ui/vendor/hls.min.js`).
- The installer downloads [MediaMTX](https://github.com/bluenviron/mediamtx) (MIT), [cloudflared](https://github.com/cloudflare/cloudflared) (Apache-2.0) and [Tailscale](https://github.com/tailscale/tailscale) (BSD-3-Clause) from their official releases.
- camorage uses [golang.org/x/crypto](https://pkg.go.dev/golang.org/x/crypto) (BSD-3-Clause).
````

- [ ] **Step 3: Retire the old scripts and point deploy.sh at the installer**

Run: `git rm -q camorage.sh setup.sh && sed -i 's|^# Build camorage for the phone and (re)start it in Termux over SSH. Dev tool; the product installer is M4.$|# Build camorage for the phone and (re)start it in Termux over SSH. Development only: phones install with scripts/install.sh (README).|' scripts/deploy.sh && sed -n 2p scripts/deploy.sh`
Expected: `# Build camorage for the phone and (re)start it in Termux over SSH. Development only: phones install with scripts/install.sh (README).`

- [ ] **Step 4: Amend the spec**

Run this script (it asserts that each anchor exists exactly once):

```bash
python3 - <<'EOF'
p = 'docs/superpowers/specs/2026-09-30-camorage-portal-design.md'
s = open(p).read()
def rep(old, new):
    global s
    assert s.count(old) == 1, old
    s = s.replace(old, new)
i = s.index('| Termux | 0.119')
j = s.index('\n', i) + 1
s = s[:j] + '| Termux (current) | F-Droid/GitHub build, Android 7+ (probed in `termux/termux-docker:x86_64`, 2026-09-27): ffmpeg 8.1.2, rclone 1.75.1, proot; the M3 storage tests and the motion reader pass against them (M4) |\n' + s[j:]
rep('| `internal/ffmpeg` | ffmpeg adapter (version-aware flags), snapshot grab | — |',
    '| `internal/ffmpeg` | ffmpeg adapter (version-aware flags): lives in `internal/motion` (`ParseVersion`, `ReaderArgs`: `-stimeout` / `-vsync` for 4.x, `-timeout` from 5, `-fps_mode` from 5.1); snapshot grab not built | — |')
i = s.index('Build: `CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build`')
j = s.index('\n', i)
s = s[:i] + ('Build: `CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags "-s -w -X main.version=<v>"` on the laptop for `GOARCH=arm GOARM=7`, `arm64` and `amd64` → single static binaries (`scripts/release.sh`). External Go deps limited to `golang.org/x/crypto` (argon2).\n\n'
    'Install (M4): `curl -fsSL https://github.com/pritamkarar/camorage/releases/latest/download/install.sh | bash` in Termux. install.sh maps `uname -m` to a build (`armv7*`/`armv8l` → arm, `aarch64` → arm64, `x86_64` → amd64), installs `ffmpeg rclone proot` with `pkg`, downloads camorage from the release and MediaMTX v1.21.1, cloudflared 2026.9.3 and Tailscale 1.102.4 from their official releases, checks them all against the release\'s `SHA256SUMS`, and only then stops the old portal and swaps the binaries into `~/camorage/bin`; it writes `~/camorage/start.sh` and `~/.termux/boot/camorage` and never touches `~/.camorage` or recordings. Re-running it upgrades.') + s[j:]
rep('- Android 6 has no Termux:Boot → after a phone reboot the user opens Termux and runs `camorage` (battery covers power cuts).',
    '- After a reboot: Android 7+ starts camorage through Termux:Boot (`~/.termux/boot/camorage`, written by the installer); Android 5/6 have no Termux:Boot → the user opens Termux and runs `~/camorage/start.sh` (battery covers power cuts). Android 12+ may kill Termux\'s child processes (phantom process killer): the README gives the setting / adb command.')
i = s.index('- **Integration (laptop):**')
j = s.index('\n', i) + 1
s = s[:j] + '- **Installer (laptop):** `scripts/installtest.sh`: download plan per CPU and refusals; the release for arm, arm64 and amd64 with checksums; fresh install, first-run, upgrade in place, damaged-download refusal and the motion-reader / rclone tests inside current Termux (`termux/termux-docker:x86_64`), served from a local copy of the release.\n' + s[j:]
open(p, 'w').write(s)
EOF
```

Run: `git diff --stat docs/superpowers/specs/`
Expected: the spec changed, 1 file.

- [ ] **Step 5: Check and commit**

Run: `go test ./... >/dev/null && scripts/installtest.sh --quick && echo docs-ok`
Expected: `ok: download plans per CPU, refusals, shellcheck`, then `docs-ok`.

```bash
git add README.md scripts/deploy.sh docs/superpowers/specs/2026-09-30-camorage-portal-design.md
git commit -m "docs: README (install, keep running, remote access, cloud copy, development); retire camorage.sh and setup.sh

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 7: From-scratch install on this phone (exit test, before publishing)

**Files:**
- Modify: `~/.claude/projects/-home-user-src-camorage/memory/camorage-phone-deployment.md`

**Interfaces:**
- Consumes:
  - `scripts/release.sh` and `install.sh` (Tasks 3–4), the test binaries built as in Task 5.
  - The phone over SSH (`. spike/env.sh; $P`), Chrome through the chrome-devtools MCP, and the camera at 192.168.1.129.

Steps 2 and 10 wait for the owner. While the fresh install runs (about 20–30 minutes), live view, recording, the tunnels and cloud copy of the current setup are paused.

- [ ] **Step 1: Build the release and serve it on the LAN**

Run:
```bash
LAN=$(ip -4 route get 192.168.1.29 | awk '{for (i = 1; i < NF; i++) if ($i == "src") print $(i + 1)}'); echo "$LAN"
scripts/release.sh 0.4.0 && python3 -m http.server 18765 --bind "$LAN" --directory .cache/release
```
Start the second line with `run_in_background`; it keeps serving until it is stopped in Step 9.
Expected: the laptop's `192.168.1.x` address, and `built .cache/release/v0.4.0`.

- [ ] **Step 2: Ask the owner**

Ask: *"For the from-scratch install I'll stop camorage and move `~/camorage` and `~/.camorage` aside to `*.old`. Your settings, Drive sign-in and Tailscale identity are all inside `~/.camorage`. Then I'll install from the laptop, set it up fresh in the browser with Camera 1, and afterwards put your settings back. Live view, recording, remote access and cloud copy pause for about 20–30 minutes. Recordings stay where they are. Go ahead?"*

Continue only on a yes.

- [ ] **Step 3: Move the current install aside**

Run: `. spike/env.sh; $P 'df -h $HOME | tail -1; pkill -x camorage; for i in $(seq 1 40); do pgrep -x camorage >/dev/null || break; sleep 0.5; done; pgrep -x 'camorage|mediamtx|cloudflared|tailscaled' || echo stopped; mv ~/camorage ~/camorage.old && mv ~/.camorage ~/.camorage.old && ls -d ~/camorage* ~/.camorage*'`
Expected:
- The free space on `/data`, which must be at least 400 MB.
- `stopped`.
- `/data/data/com.termux/files/home/.camorage.old` and `/data/data/com.termux/files/home/camorage.old`.

- [ ] **Step 4: Install from scratch**

Run: `. spike/env.sh; time $P "curl -fsSL http://$LAN:18765/v0.4.0/install.sh | CAMORAGE_BASE=http://$LAN:18765/v0.4.0 bash"`
Expected:
- `Downloading camorage 0.4.0 (arm)`, then the four file names.
- `camorage 0.4.0 is running.`
- `Open http://192.168.1.29:8080 …`
- `After a reboot, open Termux and run: ~/camorage/start.sh` (this is Android 6).
- Under 5 minutes in all. The packages are already there, so `pkg` is skipped.

- [ ] **Step 5: First-run setup and a camera, in the browser**

In Chrome:
1. Open `http://192.168.1.29:8080`. The first-run page appears.
2. Choose a new admin password: generate one with `openssl rand -base64 15` and save it to `.cache/admin-password-m4test`.
3. Pick the SD card recordings folder, then set up.
4. On Cameras, click Add camera and enter name "Camera 1", main `rtsp://192.168.1.129/live/ch00_0` and sub `rtsp://192.168.1.129/live/ch00_1`. Save.

Expected:
- Live shows the tile.
- Full screen plays over WebRTC, 1280×720.
- Settings shows `camorage version 0.4.0`.
- After 3 minutes, Playback for today shows a new span that plays.

- [ ] **Step 6: The adapters against the phone's ffmpeg 4.2.1 and rclone 1.50.1**

Run:
```bash
CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go test -c -o .cache/motion-arm.test ./internal/motion
CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go test -c -o .cache/storage-arm.test ./internal/storage
. spike/env.sh; scp -q -i ~/.ssh/camorage_phone_ed25519 -P 8022 .cache/motion-arm.test .cache/storage-arm.test "$PHONE_IP:"
$P './motion-arm.test -test.run TestReaderArgsAgainstInstalledFFmpeg -test.v | grep -E "^(---|ok|FAIL)"; RCLONE=$(command -v rclone) ./storage-arm.test -test.run "TestRclone|TestUploader|TestRetention" -test.v | grep -E "^(---|ok|FAIL)"; rm -f motion-arm.test storage-arm.test'
```
(`spike/env.sh` sets `PHONE_IP`.)
Expected: `--- PASS: TestReaderArgsAgainstInstalledFFmpeg`, then PASS for every storage test and no SKIP.

- [ ] **Step 7: Resources on the fresh install**

Run: `. spike/env.sh; $P 'bash ~/spike/measure.sh 30 $(pgrep -x camorage) $(pgrep -x mediamtx) $(pgrep -f "ffmpeg.*_sub" | head -1)'`
Expected: a TOTAL line. Record it (compare M2: 16.6 % / 51 MB).

- [ ] **Step 8: Put the owner's settings back**

Run: `. spike/env.sh; $P 'pkill -x camorage; for i in $(seq 1 40); do pgrep -x camorage >/dev/null || break; sleep 0.5; done; mv ~/.camorage ~/.camorage.m4test && mv ~/.camorage.old ~/.camorage && ~/camorage/start.sh; sleep 5; curl -s localhost:8080/api/health'`
Expected: `{"ok":true,"setupDone":true}`. The installed `~/camorage` (release 0.4.0) now runs with the owner's data.

Then check in Chrome, signing in with `.cache/admin-password`:
- Settings shows `camorage version 0.4.0`, and Tailscale and Cloudflare connected.
- Storage lists Google Drive.
- Camera 1 has cloud copy on.
- Live plays.
- `https://cams.example.com/api/health` answers.

- [ ] **Step 9: Stop serving the release**

Stop the background `http.server` from Step 1 (TaskStop).

- [ ] **Step 10: Ask about the leftovers**

Ask: *"The from-scratch install worked, and your setup is back on release 0.4.0. Two leftovers take space on the phone: `~/camorage.old` (the old binaries, about 150 MB) and `~/.camorage.m4test` (the test install's settings). Shall I delete them?"*

On a yes, run: `. spike/env.sh; $P 'rm -rf ~/camorage.old ~/.camorage.m4test; df -h $HOME | tail -1'`

- [ ] **Step 11: Record**

Update the memory file:
- M4 install layout (`~/camorage` from install.sh 0.4.0, Termux:Boot script present but unused on Android 6).
- The fresh-install result and time.
- The resources measured in Step 7.
- That the phone now runs the release build.

---

### Task 8: Publish (after the final review and the merge; the owner confirms each step)

**Files:**
- Modify: the spec (§10 M4 row), the memory file. Possibly `LICENSE`, depending on the owner's choice.

**Interfaces:**
- Consumes: `scripts/release.sh 0.4.0 --publish` (Task 4), the merged `main`, `gh`.

Everything here is outward-facing: code, history and binaries become public. **Stop and ask before steps 3, 4 and 5.**

- [ ] **Step 1: Re-run the audit on the merged history**

Run:
```bash
git log --all -p > /tmp/claude-1000/-home-user-src-camorage/62a92098-e579-4c6b-b46c-ed0f9a3bbd32/scratchpad/hist.txt
H=/tmp/claude-1000/-home-user-src-camorage/62a92098-e579-4c6b-b46c-ed0f9a3bbd32/scratchpad/hist.txt
grep -oE "GOCSPX[^\"' ]{0,24}|[a-z0-9.-]{0,20}apps\.googleusercontent\.com|eyJhIjoi[A-Za-z0-9+/=]{0,30}|ya29[^\"]{0,12}|tskey-[a-z]{0,8}|BEGIN [A-Z ]*PRIVATE KEY" "$H" | sort | uniq -c
grep -cF "$(cat .cache/admin-password)" "$H"
for p in example.com tail0a1b2c 100.64.0.7 192.168.1 <adb-serial> ~; do printf '%s %s\n' "$p" "$(grep -c "$p" "$H")"; done
```
Expected:
- Only the fixtures `GOCSPX-secret`, `cid.apps…`, `id-1.apps…`, `ya29.a`, `ya29.b` and the `eyJhIjoi` hint.
- Admin password count `0`.
- The personal-detail counts, reported to the owner.

- [ ] **Step 2: Ask the owner three questions**

*"Before the repo goes public:*
1. *The history holds no secrets, but it does hold your Cloudflare domain, tailnet name and Tailscale IP, LAN addresses, the phone's adb serial and `~` paths (in docs and plans). Publish as is, or first replace them in the current files? The history keeps them either way; rewriting it is a separate decision.*
2. *Which license, if any? Without one, the code is visible but not open source. MIT is the common choice.*
3. *OK to push `main` (64+ commits), make `pritamkarar/camorage` public, and publish release v0.4.0?"*

- [ ] **Step 3: Apply the owner's answers to 1 and 2** (only what they chose)

**Replace in current files.** Run this, then review the diff and commit:
```bash
grep -rlE "example\.com|tail0a1b2c|100\.64\.0\.7|<adb-serial>" docs scripts spike README.md | xargs sed -i -e 's/cams\.example\.com/cams.example.com/g' -e 's/example\.com/example.com/g' -e 's/camorage-phone\.tail0a1b2c\.ts\.net/camorage.<tailnet>.ts.net/g' -e 's/tail0a1b2c/<tailnet>/g' -e 's/100\.64\.0\.7/100.x.y.z/g'
```
Leave `spike/env.sh` and `scripts/deploy.sh` (the adb serial and phone IP) alone: they are the owner's dev tools and need the real values.

**MIT license.** Write `LICENSE` with the standard MIT text, `Copyright (c) 2026 Pritam Karar`, then:
```bash
git add LICENSE && git commit -m "Add MIT license

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

- [ ] **Step 4: Push and go public** (after the owner's yes to 3)

Run: `git push origin main && gh repo edit pritamkarar/camorage --visibility public --accept-visibility-change-consequences && gh repo view pritamkarar/camorage --json visibility`
Expected: `{"visibility":"PUBLIC"}`.

- [ ] **Step 5: Publish the release**

Run: `scripts/release.sh 0.4.0 --publish && curl -fsSIL -o /dev/null -w '%{http_code}\n' https://github.com/pritamkarar/camorage/releases/latest/download/install.sh`
Expected: the release URL, then `200` (downloaded without signing in).

- [ ] **Step 6: The real one-liner on the phone (an upgrade)**

Run: `. spike/env.sh; time $P 'curl -fsSL https://github.com/pritamkarar/camorage/releases/latest/download/install.sh | bash'`
Expected: `camorage 0.4.0 is running.` Health shows `setupDone:true`, and Settings, Storage and cameras are unchanged.

- [ ] **Step 7: Record and commit**

1. In the spec §10, change the M4 row's first cell to `**M4 Portability** — **done** (one-line install from public GitHub releases; arm/arm64/amd64; current Termux verified in termux-docker; exit test = from-scratch reinstall on the owner's phone, no second phone)`.
2. Update the memory file: public repo, release v0.4.0, how to release (`scripts/release.sh <v> --publish`).
3. Commit on `main` and push:
   ```bash
   git add docs/superpowers/specs/2026-09-30-camorage-portal-design.md
   git commit -m "docs: M4 done (public release v0.4.0, one-line install verified on the phone)

   Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
   git push origin main
   ```
