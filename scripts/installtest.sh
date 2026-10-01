#!/usr/bin/env bash
# shellcheck disable=SC2015 # "check && check || fail": fail exits, so it is not an if-then-else
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
shellcheck scripts/install.sh scripts/installtest.sh scripts/release.sh || fail "shellcheck"
echo "ok: download plans per CPU, refusals, shellcheck"
[ "${1:-}" = --quick ] && exit 0

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

# release.sh --publish only publishes the pushed HEAD (a fake gh records what it would do)
G=$(mktemp -d)
printf '#!/bin/sh\necho "$@" >>"%s/gh.log"\n' "$G" >"$G/gh" && chmod 755 "$G/gh"
[ "$(PATH="$G:$PATH" command -v gh)" = "$G/gh" ] || fail "the fake gh is not first on PATH"
OUT=$(PATH="$G:$PATH" scripts/release.sh "$V" --publish 2>&1) && fail "published a HEAD that is not on origin/main"
[[ $OUT == *"push"* ]] && [ ! -e "$G/gh.log" ] || fail "publish refusal: $OUT"
rm -rf "$G"
echo "ok: release.sh publishes only the pushed HEAD"

# the installer on the laptop in a fake Termux prefix (no docker, ~1 min): curl | bash while a
# package prompt reads stdin, the Termux:Boot script, an upgrade whose pkill fails, a portal that
# will not stop, and a release whose camorage does not start (it must roll back)
F=$(mktemp -d)
FP=$F/data/data/com.termux/files/usr
mkdir -p "$FP/tmp" "$FP/bin" "$F/home" "$F/nopkill"
cat >"$FP/bin/curl" <<'EOF'
#!/usr/bin/env bash
# fake curl: releases and third-party files from the laptop's .cache; health checks to the real curl
out= url=
while [ $# -gt 0 ]; do
	case $1 in -o) out=$2 && shift ;; http*) url=$1 ;; esac
	shift
done
case $url in
http://127.0.0.1*) exec "$REAL_CURL" -fsS -m 2 "$url" ;;
http://fake.test/*) cp "$FAKE_RELEASES/${url#http://fake.test/}" "$out" ;;
*) cp "$FAKE_THIRDPARTY/${url##*/}" "$out" ;;
esac
EOF
cat >"$FP/bin/pkg" <<'EOF'
#!/usr/bin/env bash
# fake pkg: reads stdin the way dpkg's conffile prompt does, notes its arguments, "installs" stubs
cat >/dev/null
echo "$*" >>"$PREFIX/pkg.log"
for a in "$@"; do
	case $a in -* | *::* | install) ;; *) printf '#!/bin/sh\n' >"$PREFIX/bin/$a" && chmod 755 "$PREFIX/bin/$a" ;; esac
done
EOF
printf '#!/bin/sh\n' >"$FP/bin/termux-wake-lock"
# a phone with an SD card Termux has no folder on yet; the fake termux-setup-storage reads stdin
# like a prompt, then "Android" creates the folder (as when the user taps Allow)
mkdir -p "$F/storage/1234-ABCD" "$F/storage/emulated/0" "$F/storage/self"
# shellcheck disable=SC2016 # $CAMORAGE_STORAGE belongs to the generated script
printf '#!/bin/sh\ncat >/dev/null\nmkdir -p "$CAMORAGE_STORAGE/1234-ABCD/Android/data/com.termux/files"\n' >"$FP/bin/termux-setup-storage"
printf '#!/bin/sh\nexit 159\n' >"$F/nopkill/pkill" # pkill killed by SIGSYS (Termux procps 4.0.7, Sep 2026)
chmod 755 "$FP"/bin/* "$F/nopkill/pkill"
ln -s "$(command -v bash)" "$FP/bin/bash"
ln -s "$(command -v dash || command -v sh)" "$FP/bin/sh"
finstall() { # finstall <release dir> [VAR=value …]: install.sh from that release, fed through a pipe
	local rel=$1
	shift
	# shellcheck disable=SC2002 # the pipe is the point: bash reads the script from stdin, as with curl | bash
	cat ".cache/release/$rel/install.sh" | env HOME="$F/home" PREFIX="$FP" PATH="$FP/bin:/usr/bin:/bin" \
		REAL_CURL="$(command -v curl)" FAKE_RELEASES="$PWD/.cache/release" FAKE_THIRDPARTY="$PWD/.cache/thirdparty" \
		CAMORAGE_BASE="http://fake.test/$rel" "$@" bash
}
portals() { # the fake install's running portals
	for p in $(pgrep -x camorage); do grep -qz "$F" "/proc/$p/environ" 2>/dev/null && echo "$p"; done
	return 0
}
fstop() {
	for p in $(portals); do kill "$p"; done
	for _ in $(seq 1 40); do [ -z "$(portals)" ] && return; sleep 0.5; done
}
STUB=
trap 'fstop; [ -n "$STUB" ] && kill -9 "$STUB" 2>/dev/null; true' EXIT
OUT=$(finstall "v$V" CAMORAGE_STORAGE="$F/storage" 2>&1) || fail "curl | bash install: $OUT"
[[ $OUT == *"camorage $V is running"* ]] || fail "curl | bash stopped early (a package prompt ate the script?): $OUT"
[ "$(portals | wc -l)" = 1 ] || fail "portals after install: $(portals | wc -l)"
grep -q -- "--force-confold" "$FP/pkg.log" || fail "pkg install without dpkg's keep-my-config options: $(cat "$FP/pkg.log")"
[[ $OUT == *"SD card found (1234-ABCD)"* && $OUT == *"SD card ready for recordings."* ]] && [ -d "$F/storage/1234-ABCD/Android/data/com.termux/files" ] ||
	fail "a first install did not ready the SD card: $OUT"
echo "ok: curl | bash survives a package prompt and termux-setup-storage reading stdin; the SD card is readied"
fstop
BOOT=$(ls "$F/home/.termux/boot")
env HOME="$F/home" PREFIX="$FP" PATH="$FP/bin:/usr/bin:/bin" "$F/home/.termux/boot/$BOOT" >/dev/null 2>&1
for _ in $(seq 1 20); do [ -n "$(portals)" ] && break; sleep 0.5; done
[ "$(portals | wc -l)" = 1 ] || fail "the Termux:Boot script ($BOOT) started $(portals | wc -l) portals"
echo "ok: the Termux:Boot script starts the portal"
printf '#!/bin/sh\n' >"$F/home/.termux/boot/camorage" # the boot script of release 0.4.0 test installs
P1=$(portals)
OUT=$(finstall "v$V" PATH="$F/nopkill:$FP/bin:/usr/bin:/bin" 2>&1) || fail "upgrade with a failing pkill: $OUT"
P2=$(portals)
[ -n "$P2" ] && [ "$P2" != "$P1" ] && [ "$(portals | wc -l)" = 1 ] || fail "upgrade with a failing pkill kept the old portal ($P1 → $P2): $OUT"
[ "$(ls "$F/home/.termux/boot")" = "$BOOT" ] || fail "boot scripts after the upgrade: $(ls "$F/home/.termux/boot")"
echo "ok: an upgrade replaces the portal even when pkill fails; the old boot script is removed"
fstop
cp "$(command -v sleep)" "$F/camorage" # a "portal" that ignores SIGTERM
(
	trap '' TERM
	exec "$F/camorage" 600
) &
STUB=$!
OUT=$(finstall "v$V" CAMORAGE_STOP_WAIT=2 2>&1) && fail "installed over a portal that did not stop"
[[ $OUT == *"did not stop"* ]] && kill -0 "$STUB" && [ "$("$F/home/camorage/bin/camorage" -version)" = "$V" ] || fail "portal that will not stop: $OUT"
kill -9 "$STUB"
STUB=
echo "ok: a portal that will not stop is left alone, nothing changed"
"$F/home/camorage/start.sh"
for _ in $(seq 1 20); do [ -n "$(portals)" ] && break; sleep 0.5; done
rm -rf ".cache/release/v$V-broken"
cp -r "$R" ".cache/release/v$V-broken"
# shellcheck disable=SC2016 # $1 belongs to the generated script
printf '#!/bin/sh\n[ "$1" = -version ] && { echo 9.9.9-broken; exit 0; }\necho "broken build" >&2\nexit 2\n' >".cache/release/v$V-broken/camorage-linux-amd64"
(cd ".cache/release/v$V-broken" && sed -i '/ camorage-linux-amd64$/d' SHA256SUMS && sha256sum camorage-linux-amd64 >>SHA256SUMS)
OUT=$(finstall "v$V-broken" 2>&1) && fail "a camorage that does not start was reported installed"
[[ $OUT == *"previous version is running again"* ]] || fail "no rollback message: $OUT"
[ "$(portals | wc -l)" = 1 ] && [ "$("$F/home/camorage/bin/camorage" -version)" = "$V" ] || fail "rollback: $(portals | wc -l) portals, installed $("$F/home/camorage/bin/camorage" -version)"
grep -q "broken build" "$F/home/camorage/camorage.log.1" || fail "the failed start's log was not kept"
[ ! -e "$F/home/camorage/bin.old" ] || fail "bin.old left behind"
echo "ok: a release whose camorage does not start rolls back to the previous version"
rm -rf "$F/storage/1234-ABCD/Android" # an SD card Termux cannot use, on a phone that is set up already
mkdir -p "$F/rec"
sed -i "s|\"recDir\": *\"\"|\"recDir\": \"$F/rec\"|" "$F/home/.camorage/config.json"
grep -q "\"recDir\": \"$F/rec\"" "$F/home/.camorage/config.json" || fail "could not set recDir in $(cat "$F/home/.camorage/config.json")"
OUT=$(finstall "v$V" CAMORAGE_STORAGE="$F/storage" 2>&1) || fail "upgrade of a set-up portal: $OUT"
[[ $OUT != *"SD card"* ]] && [ ! -d "$F/storage/1234-ABCD/Android" ] || fail "an upgrade asked for SD card access: $OUT"
echo "ok: an upgrade of a set-up portal does not ask for SD card access"
fstop
trap - EXIT
rm -rf "$F" ".cache/release/v$V-broken"

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
# served on the laptop's loopback only; containers reach it as host.docker.internal (Docker Desktop
# runs them in a VM, so the bridge gateway and --network host are not this machine)
python3 -m http.server 18765 --bind 127.0.0.1 --directory .cache/release >/dev/null 2>&1 &
SRV=$!
CN=camorage-installtest-$$
trap 'kill $SRV 2>/dev/null || true; docker rm -f "$CN" >/dev/null 2>&1 || true' EXIT
# shellcheck disable=SC2016 # the script below runs inside the container: its $ expand there
# (the image's entrypoint switches user and drops docker run -e variables: the values go in the script)
timeout 1500 docker run --rm --name "$CN" --add-host=host.docker.internal:host-gateway -v "$PWD/$T:/tests:ro" termux/termux-docker:x86_64 \
	bash -c "BASE=http://host.docker.internal:18765/v$V BAD=http://host.docker.internal:18765/v$V-bad V=$V"'
set -euo pipefail
fail() { echo "INSTALLTEST FAIL: $*"; tail -20 ~/camorage/camorage.log 2>/dev/null || true; exit 1; }
# Termux main repo on Cloudflare; pkg would otherwise pick a random mirror, and some stall for good
echo "deb https://packages-cf.termux.dev/apt/termux-main stable main" >"$PREFIX/etc/apt/sources.list"
export TERMUX_PKG_NO_MIRROR_SELECT=1
leftovers() { find "$PREFIX/tmp" -maxdepth 1 -name "camorage-install.*" | wc -l; }
curl -fsSL "$BASE/install.sh" | CAMORAGE_BASE=$BASE bash >~/install1.log 2>&1 || { cat ~/install1.log; fail "fresh install"; }
[ "$(~/camorage/bin/camorage -version)" = "$V" ] || fail "installed version"
curl -fsS localhost:8080/api/health | grep "\"setupDone\":false" >/dev/null || fail "a fresh install is not waiting for setup"
curl -fsS -c ~/jar -H "Origin: http://localhost:8080" -H "Content-Type: application/json" \
	-d "{\"password\":\"correct-horse-battery\",\"recDir\":\"$HOME/rec\"}" localhost:8080/api/setup | grep "\"ok\":true" >/dev/null || fail "first-run setup"
for _ in $(seq 1 20); do pgrep -f camorage/bin/mediamtx >/dev/null && break; sleep 1; done
pgrep -f camorage/bin/mediamtx >/dev/null || fail "MediaMTX did not start after setup"
~/camorage/bin/mediamtx --version | grep v1.21.1 >/dev/null || fail "mediamtx version"
~/camorage/bin/cloudflared --version | grep 2026.9.3 >/dev/null || fail "cloudflared version"
~/camorage/bin/tailscale version | sed -n 1p | grep 1.102.4 >/dev/null || fail "tailscale version"
[ -x ~/.termux/boot/start-camorage ] && [ -x ~/camorage/start.sh ] || fail "no start.sh or Termux:Boot script"
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

echo "INSTALLTEST PASS"
