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
[[ $V =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$ ]] || { echo "version must look like 1.2.3 or 1.2.3-rc1, not $V" >&2; exit 1; }
if [ "${2:-}" = --publish ]; then # the release must be built from, and tag, exactly what GitHub has
	[ "$(git rev-parse HEAD)" = "$(git rev-parse origin/main)" ] || { echo "push main first: HEAD is not origin/main, so the release would not match its tag" >&2; exit 1; }
	[ -z "$(git status --porcelain)" ] || { echo "commit your changes first: the release is built from the working tree" >&2; exit 1; }
fi
OUT=.cache/release/v$V
TP=.cache/thirdparty
rm -rf "$OUT"
mkdir -p "$OUT" "$TP"
sed "s/@VERSION@/$V/" scripts/install.sh >"$OUT/install.sh"
: >"$OUT/SHA256SUMS"
for m in armv7l aarch64 x86_64; do
	plan=$(CAMORAGE_ARCH=$m bash "$OUT/install.sh" --dry-run)
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
	done <<<"$plan"
done
(cd "$OUT" && sha256sum install.sh) >>"$OUT/SHA256SUMS"
[ "$(wc -l <"$OUT/SHA256SUMS")" = 13 ] || { echo "SHA256SUMS has $(wc -l <"$OUT/SHA256SUMS") lines, want 13" >&2; exit 1; }
echo "built $OUT"
if [ "${2:-}" = --publish ]; then
	gh release create "v$V" "$OUT"/camorage-linux-* "$OUT/install.sh" "$OUT/SHA256SUMS" --target "$(git rev-parse HEAD)" \
		--title "camorage $V" \
		--notes "Install or upgrade in Termux on an Android phone:

\`\`\`
curl -fsSL https://github.com/pritamkarar/camorage/releases/latest/download/install.sh | bash
\`\`\`"
fi
