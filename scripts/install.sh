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

# stop_running stops the running portal (TERM; its shutdown stops MediaMTX and the rest) and waits
# for it, or gives up without having changed anything. kill, not pkill: Termux's procps 4.0.7 of
# September 2026 shipped a pkill that dies of SIGSYS.
stop_running() {
	local pids p
	pids=$(pgrep -x camorage) || return 0
	for p in $pids; do kill "$p" 2>/dev/null || true; done
	for _ in $(seq 1 $((${CAMORAGE_STOP_WAIT:-60} * 2))); do
		pgrep -x camorage >/dev/null || return 0
		sleep 0.5
	done
	die "the running camorage did not stop within ${CAMORAGE_STOP_WAIT:-60} s; nothing was changed (stop it, then run this again)"
}

# healthy waits up to 20 s for the portal to answer.
healthy() {
	for _ in $(seq 1 40); do
		curl -fsS -m 2 http://127.0.0.1:8080/api/health >/dev/null 2>&1 && return 0
		sleep 0.5
	done
	return 1
}

# main runs only once bash has read the whole script: a download cut short does nothing, and
# nothing below can read the rest of the script from stdin (curl | bash).
main() {
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
		# stdin is this script under curl | bash: a package prompt must not read it; keep existing configs
		pkg install -y -o Dpkg::Options::=--force-confdef -o Dpkg::Options::=--force-confold "${need[@]}" </dev/null
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
	mkdir -p "$DIR"
	stop_running
	rm -rf "${DIR:?}/bin.old"
	[ ! -d "$DIR/bin" ] || mv "$DIR/bin" "$DIR/bin.old" # put back if the new camorage does not start
	mkdir -p "$DIR/bin"
	mv -f "$tmp"/bin/* "$DIR/bin/"
	cat >"$DIR/start.sh" <<-EOF
	#!$PREFIX/bin/bash
	# Start camorage in the background unless it is running. Termux:Boot runs this at boot (Android 7+);
	# on Android 5/6 open Termux and run it by hand after a reboot. argv[0] is set to "camorage":
	# current Termux's pgrep -x matches argv[0], the 2019 one the program's name.
	cd "$DIR" || exit 1
	pgrep -x camorage >/dev/null && exit 0
	[ ! -f camorage.log ] || mv -f camorage.log camorage.log.1
	( trap '' HUP; exec -a camorage bin/camorage > camorage.log 2>&1 </dev/null ) &
	EOF
	chmod 755 "$DIR/start.sh"
	# not named "camorage": Termux:Boot runs the script as a program of that name, and start.sh's
	# pgrep -x camorage would take it for a running portal
	mkdir -p "$HOME/.termux/boot"
	rm -f "$HOME/.termux/boot/camorage" # its name in the first 0.4.0 installs
	printf '#!%s/bin/sh\ntermux-wake-lock\n%s/start.sh\n' "$PREFIX" "$DIR" >"$HOME/.termux/boot/start-camorage"
	chmod 755 "$HOME/.termux/boot/start-camorage"
	"$DIR/start.sh"
	if ! healthy; then
		[ -d "$DIR/bin.old" ] || die "camorage did not start: see $DIR/camorage.log"
		stop_running
		rm -rf "${DIR:?}/bin"
		mv "$DIR/bin.old" "$DIR/bin"
		"$DIR/start.sh"
		healthy || die "camorage $VERSION did not start, and neither did the previous version: see $DIR/camorage.log and $DIR/camorage.log.1"
		die "camorage $VERSION did not start (its log: $DIR/camorage.log.1); the previous version is running again"
	fi
	rm -rf "${DIR:?}/bin.old"
	ip=$(ip -4 -o addr show wlan0 2>/dev/null | awk '{ split($4, a, "/"); print a[1]; exit }') || ip=
	[ -n "$ip" ] || ip="<this phone's Wi-Fi address>"
	say "camorage $VERSION is running."
	echo "Open http://$ip:8080 in a browser on the same Wi-Fi to set it up."
	sdk=$(getprop ro.build.version.sdk 2>/dev/null) || sdk=
	if [ "${sdk:-0}" -ge 24 ] 2>/dev/null; then
		echo "To start it automatically after a reboot, install the Termux:Boot app and open it once."
	else
		echo "After a reboot, open Termux and run: ~/camorage/start.sh"
	fi
}

main "$@"
