#!/usr/bin/env bash
# A portal on this laptop for UI work: MediaMTX, fake test-pattern cameras (one disabled, with a
# long name and URL) and a folder standing in for cloud storage.
#   scripts/demo.sh          →  http://127.0.0.1:18090, password demo-password. Ctrl-C stops it all.
#   scripts/demo.sh --fresh  →  the same, not set up yet: it shows the first-run screen.
#   DEMO_H265=1 scripts/demo.sh  →  Front door sends H.265 (for the in-browser H.265 decoder)
# MediaMTX uses fixed ports (8554, 8888, 8889, 9997): don't run it together with scripts/itest.sh.
set -euo pipefail
MODE=${1:-} # read now: the camera loop below reuses $1
cd "$(dirname "$0")/.."
if [ ! -x .cache/mediamtx-amd64 ] || [ ! -x .cache/rclone-amd64 ]; then
  echo "demo: run scripts/itest.sh once first (it downloads MediaMTX and rclone into .cache)" >&2
  exit 1
fi
T=$(mktemp -d)
# shellcheck disable=SC2046 # one pid per word
cleanup() { kill $(jobs -p) 2>/dev/null || true; wait 2>/dev/null || true; rm -rf "$T"; }
trap cleanup EXIT

# the fake cameras: a second MediaMTX on :18564 fed by ffmpeg test patterns
printf '%s' '{"api":false,"playback":false,"hls":false,"webrtc":false,"rtmp":false,"srt":false,"moq":false,"rtspAddress":"127.0.0.1:18564","rtspTransports":["tcp"],"paths":{"cam":{"source":"publisher"},"camsub":{"source":"publisher"},"cam2":{"source":"publisher"}}}' > "$T/cam.yml"
.cache/mediamtx-amd64 "$T/cam.yml" > "$T/cam.log" 2>&1 &
sleep 2
for p in "cam 1280x720 testsrc2" "camsub 640x360 testsrc2" "cam2 1280x720 smptehdbars"; do
  # shellcheck disable=SC2086 # split "path size source" into $1 $2 $3
  set -- $p
  enc=(-c:v libx264 -preset ultrafast -tune zerolatency -g 40)
  if [ "${DEMO_H265:-}" = 1 ] && [ "$1" != cam2 ]; then # Front door (cam, camsub): H.265, no B-frames
    enc=(-c:v libx265 -preset ultrafast -x265-params keyint=40:min-keyint=40:scenecut=0:bframes=0:log-level=error)
  fi
  ffmpeg -nostdin -loglevel error -re -f lavfi -i "$3=size=$2:rate=20" "${enc[@]}" \
    -f rtsp -rtsp_transport tcp "rtsp://127.0.0.1:18564/$1" &
done

go build -o "$T/camorage" ./cmd/camorage
cp .cache/mediamtx-amd64 "$T/mediamtx" # camorage looks for mediamtx next to its own binary
cp .cache/rclone-amd64 "$T/rclone"
"$T/camorage" -data "$T/data" -listen 127.0.0.1:18090 > "$T/camorage.log" 2>&1 &
B=http://127.0.0.1:18090
api() { local m=$1 p=$2; shift 2; curl -fsS -b "$T/jar" -c "$T/jar" -H "Origin: $B" -H 'Content-Type: application/json' -X "$m" "$@" "$B$p" >/dev/null; }
for _ in $(seq 1 40); do curl -fs "$B/api/health" >/dev/null && break; sleep 0.25; done
if [ "$MODE" = --fresh ]; then
  echo "camorage demo (not set up yet): $B. Ctrl-C stops it."
  wait
  exit 0
fi
mkdir -p "$T/cloud"
api POST /api/setup -d "{\"password\":\"demo-password\",\"recDir\":\"$T/rec\"}"
api POST /api/storage/local -d "{\"name\":\"Folder cloud\",\"dir\":\"$T/cloud\"}"
api POST /api/cameras -d '{"name":"Front door","enabled":true,"mainUrl":"rtsp://127.0.0.1:18564/cam","subUrl":"rtsp://127.0.0.1:18564/camsub","localDays":1,"cloud":{"targetId":"folder-cloud","days":7}}'
api POST /api/cameras -d '{"name":"Garage","enabled":true,"mainUrl":"rtsp://127.0.0.1:18564/cam2","localDays":3,"mode":"motion","motion":{"sensitivity":"medium"},"schedule":[{"days":[1,2,3,4,5,6,7],"start":"22:00","end":"06:00"}]}'
api POST /api/cameras -d '{"name":"Back garden camera by the old shed with a very long name","enabled":false,"mainUrl":"rtsp://192.168.1.99:554/a-very-long-path/that/keeps/going/and/going/stream1","localDays":2}'
echo "camorage demo: $B (password demo-password). Ctrl-C stops it."
wait
