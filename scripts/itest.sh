#!/usr/bin/env bash
# M1a end-to-end test on the laptop: real MediaMTX, a fake camera, the camorage API (~5 min).
set -euo pipefail
cd "$(dirname "$0")/.."
V=v1.21.1
mkdir -p .cache
if [ ! -x .cache/mediamtx-amd64 ]; then
  curl -fsSL -o .cache/mtx.tgz "https://github.com/bluenviron/mediamtx/releases/download/$V/mediamtx_${V}_linux_amd64.tar.gz"
  tar -xzf .cache/mtx.tgz -C .cache mediamtx && mv .cache/mediamtx .cache/mediamtx-amd64
fi
if [ ! -x .cache/rclone-amd64 ]; then # the phone's version (Termux): storage behaviour is checked against it
  curl -fsSL -o .cache/rclone.zip https://downloads.rclone.org/v1.50.1/rclone-v1.50.1-linux-amd64.zip
  unzip -q -o -j .cache/rclone.zip 'rclone-v1.50.1-linux-amd64/rclone' -d .cache && mv .cache/rclone .cache/rclone-amd64
fi
T=$(mktemp -d)
cleanup() { kill $(jobs -p) 2>/dev/null || true; wait 2>/dev/null || true; rm -rf "$T"; }
trap cleanup EXIT
fail() {
  echo "ITEST FAIL: $*"
  echo "--- camorage.log"; tail -20 "$T/camorage.log" || true
  echo "--- mediamtx.log"; tail -20 "$T/data/logs/mediamtx.log" 2>/dev/null || true
  exit 1
}

# fake camera: a second MediaMTX on :18554 fed by ffmpeg test patterns (keyframe every 2 s, like the real one)
printf '%s' '{"api":false,"playback":false,"hls":false,"webrtc":false,"rtmp":false,"srt":false,"moq":false,"rtspAddress":"127.0.0.1:18554","rtspTransports":["tcp"],"paths":{"cam":{"source":"publisher"},"camsub":{"source":"publisher"}}}' > "$T/cam.yml"
.cache/mediamtx-amd64 "$T/cam.yml" > "$T/cam.log" 2>&1 &
sleep 2
PUBS=()
for p in "cam 1280x720" "camsub 640x360"; do
  set -- $p
  ffmpeg -nostdin -loglevel error -re -f lavfi -i "testsrc2=size=$2:rate=20" -c:v libx264 -preset ultrafast -tune zerolatency -g 40 \
    -f rtsp -rtsp_transport tcp "rtsp://127.0.0.1:18554/$1" &
  PUBS+=($!)
done

go build -o "$T/camorage" ./cmd/camorage
cp .cache/mediamtx-amd64 "$T/mediamtx" # camorage looks for mediamtx next to its own binary
cp .cache/rclone-amd64 "$T/rclone" # camorage uses an rclone next to itself
"$T/camorage" -data "$T/data" -listen 127.0.0.1:18080 > "$T/camorage.log" 2>&1 &
CAMO=$!
B=http://127.0.0.1:18080
api() { local m=$1 p=$2; shift 2; curl -sS -b "$T/jar" -c "$T/jar" -H "Origin: $B" -H 'Content-Type: application/json' -X "$m" "$@" "$B$p"; }
status_of() { api GET /api/status | python3 -c "import json,sys; c=json.load(sys.stdin)['cameras'][0]; print(c['available'], c['recording'])"; }
wait_for() {
  for _ in $(seq 1 "$2"); do [ "$(status_of 2>/dev/null)" = "$1" ] && return 0; sleep 1; done
  fail "status never became '$1' (last: $(status_of 2>/dev/null))"
}
CAM='"name":"Test Cam","enabled":true,"mainUrl":"rtsp://127.0.0.1:18554/cam","subUrl":"rtsp://127.0.0.1:18554/camsub"'

for _ in $(seq 1 40); do curl -fs "$B/api/health" >/dev/null && break; sleep 0.25; done
[ "$(curl -s -o /dev/null -w '%{http_code}' "$B/api/cameras")" = 401 ] || fail "unauthenticated request not refused"
R=$(api POST /api/setup -d "{\"password\":\"itest-password\",\"recDir\":\"$T/rec\"}"); [[ $R == *'"ok":true'* ]] || fail "setup: $R"
R=$(api POST /api/cameras -d "{$CAM}"); [[ $R == *'"id":"test-cam"'* ]] || fail "add camera: $R"
wait_for "True True" 30
echo "ok: camera available and recording"

# M1b: the UI is served and live HLS works through the portal's proxy (incl. MediaMTX's redirect)
UI=$(curl -fsS "$B/")
[[ $UI == *'<title>camorage</title>'* ]] || fail "UI not served"
PL=""
for _ in $(seq 1 20); do PL=$(api GET /live/hls/test-cam_sub/index.m3u8 -L); [[ $PL == *'#EXTM3U'* ]] && break; sleep 1; done
[[ $PL == *'#EXTM3U'* ]] || fail "HLS through the portal: $PL"
echo "ok: UI served; HLS playlist through the portal proxy"

# M1c: remote-access API. No cloudflared on this machine: the token is stored, never returned, and a start is attempted.
TOK=$(printf '{"a":"acct","t":"00000000-0000-4000-8000-000000000000","s":"c2VjcmV0LXZhbHVl"}' | base64 -w0)
R=$(api PUT /api/tunnels/cloudflare -d "{\"token\":\"cloudflared service install $TOK\",\"hostname\":\"https://Cams.Example.com/\"}"); [[ $R == *'"ok":true'* ]] || fail "save Cloudflare token: $R"
R=$(api GET /api/tunnels); [[ $R == *'"tokenSet":true'* && $R == *'"hostname":"cams.example.com"'* && $R != *"$TOK"* ]] || fail "tunnels: $R"
for _ in $(seq 1 20); do [[ $(api GET /api/status) == *'"name":"cloudflared"'* ]] && break; sleep 0.5; done
[[ $(api GET /api/status) == *'"name":"cloudflared"'* ]] || fail "cloudflared was not started"
R=$(api DELETE /api/tunnels/cloudflare); [[ $R == *'"ok":true'* ]] || fail "remove Cloudflare: $R"
for _ in $(seq 1 20); do [[ $(api GET /api/status) != *'"name":"cloudflared"'* ]] && break; sleep 0.5; done
[[ $(api GET /api/status) != *'"name":"cloudflared"'* ]] || fail "cloudflared was not stopped"
[ "$(curl -s -o /dev/null -w '%{http_code}' -b "$T/jar" "$B/live/hls/test-cam_sub%2F..%2Ftest-cam/index.m3u8")" = 404 ] || fail "an encoded ../ reached MediaMTX"
echo "ok: tunnels API keeps the token secret, starts and stops cloudflared; encoded ../ refused"

# schedule: a window ending ~2 minutes from now; the 30 s reconcile loop must stop recording by itself
START=$(date -d '-10 min' +%H:%M); END=$(date -d '+2 min' +%H:%M)
R=$(api PUT /api/cameras/test-cam -d "{$CAM,\"schedule\":[{\"days\":[1,2,3,4,5,6,7],\"start\":\"$START\",\"end\":\"$END\"}]}"); [[ $R == *'"ok":true'* ]] || fail "put schedule: $R"
wait_for "True True" 30
echo "ok: inside window $START-$END, recording"
wait_for "True False" 185
echo "ok: window ended, reconcile turned recording off"

N=$(ls "$T/rec/test-cam" | wc -l)
[ "$N" -ge 2 ] || fail "expected >= 2 segments, got $N"
echo "ok: $N segments on disk"
SPANS=$(api GET "/api/playback/spans?cam=test-cam&date=$(date +%F)")
LONGEST=$(echo "$SPANS" | python3 -c 'import json,sys,urllib.parse; s=max(json.load(sys.stdin), key=lambda x: x["durationSec"]); assert s["durationSec"] > 40, s; print(urllib.parse.quote(s["start"]))') || fail "spans: $SPANS"
api GET "/api/playback/video?cam=test-cam&start=$LONGEST&duration=30&format=mp4" -o "$T/clip.mp4"
D=$(ffprobe -v error -show_entries format=duration -of csv=p=0 "$T/clip.mp4") || fail "30 s clip is not a video: $(head -c 300 "$T/clip.mp4")"
python3 -c "import sys; sys.exit(0 if 28 <= float('$D') <= 32 else 1)" || fail "30 s clip lasted $D s"
echo "ok: 30 s playback clip is ${D}s"

OLD="$T/rec/test-cam/2020-01-01_00-00-00-000000.mp4"
touch "$OLD"
for _ in $(seq 1 75); do [ -e "$OLD" ] || break; sleep 1; done
[ ! -e "$OLD" ] || fail "janitor did not delete an expired segment"
echo "ok: janitor deleted an expired segment"
# M2: motion mode. The fake camera's test pattern changes between keyframes, so it always shows motion.
PRE="$T/rec/test-cam/$(date -u -d '-2 hours' +%Y-%m-%d_%H-%M-%S)-000000.mp4"
touch "$PRE" # recorded before the switch to motion mode: must survive it
R=$(api PUT /api/cameras/test-cam -d "{$CAM,\"mode\":\"motion\",\"motion\":{\"sensitivity\":\"medium\"}}"); [[ $R == *'"ok":true'* ]] || fail "switch to motion mode: $R"
motion_on() { api GET /api/status | python3 -c "import json,sys; print(json.load(sys.stdin)['cameras'][0]['motion'])"; }
for _ in $(seq 1 90); do [ "$(motion_on 2>/dev/null)" = True ] && break; sleep 1; done
[ "$(motion_on)" = True ] || fail "no motion on the moving test pattern: $(tail -3 "$T/data/logs/motion-test-cam.log" 2>/dev/null)"
EV=$(api GET "/api/playback/events?cam=test-cam&date=$(date +%F)")
[[ $EV == *'"open":true'* ]] || fail "events: $EV"
echo "ok: motion detected; the event in progress is listed"
FIRST=$(echo "$EV" | python3 -c 'import json,sys; print(json.load(sys.stdin)[0]["start"])')
kill "$CAMO"; wait "$CAMO" 2>/dev/null || true
"$T/camorage" -data "$T/data" -listen 127.0.0.1:18080 >> "$T/camorage.log" 2>&1 &
CAMO=$!
for _ in $(seq 1 40); do curl -fs "$B/api/health" >/dev/null && break; sleep 0.25; done
EV=$(api GET "/api/playback/events?cam=test-cam&date=$(date +%F)")
[ "$(echo "$EV" | python3 -c 'import json,sys; print(json.load(sys.stdin)[0]["start"])')" = "$FIRST" ] || fail "the event was lost in a restart: $EV"
echo "ok: the event survived a portal restart"
sleep 10 # the janitor runs at startup
[ -e "$PRE" ] || fail "footage recorded before motion mode was deleted"
echo "ok: footage from before motion mode kept"
# M3: cloud copy to a folder target through rclone 1.50.1. Stopping the fake camera's substream
# ends motion detection's frames, so the motion event closes and its clip becomes due.
mkdir -p "$T/cloud"
R=$(api POST /api/storage/local -d "{\"name\":\"itest cloud\",\"dir\":\"$T/cloud\"}"); [[ $R == *'"id":"itest-cloud"'* ]] || fail "add folder target: $R"
R=$(api PUT /api/cameras/test-cam -d "{$CAM,\"mode\":\"motion\",\"motion\":{\"sensitivity\":\"medium\"},\"cloud\":{\"targetId\":\"itest-cloud\",\"days\":7}}"); [[ $R == *'"ok":true'* ]] || fail "cloud copy on: $R"
kill "${PUBS[1]}"
DAY="$T/cloud/camorage/test-cam/$(date +%F)"
for _ in $(seq 1 300); do ls "$DAY"/*_motion_*s.mp4 >/dev/null 2>&1 && break; sleep 1; done
CLIP=$(ls "$DAY"/*_motion_*s.mp4 2>/dev/null | head -1 || true)
[ -n "$CLIP" ] || fail "no motion clip in the cloud folder: $(api GET /api/storage)"
ffprobe -v error -show_entries format=duration -of csv=p=0 "$CLIP" >/dev/null || fail "the uploaded clip is not a video"
echo "ok: the motion event's clip reached the cloud target"
NAME=$(basename "$CLIP")
R=$(api GET "/api/playback/cloud-spans?cam=test-cam&date=$(date +%F)"); [[ $R == *"\"file\":\"$NAME\""* ]] || fail "cloud spans: $R"
CODE=$(curl -sS -b "$T/jar" -H 'Range: bytes=0-99' -o "$T/range.bin" -w '%{http_code}' "$B/api/playback/cloud?cam=test-cam&date=$(date +%F)&file=$NAME")
[ "$CODE" = 206 ] && cmp -s "$T/range.bin" <(head -c 100 "$CLIP") || fail "cloud range read: HTTP $CODE"
echo "ok: cloud clips are listed and streamed by byte range"
# M1c: a Tailscale sign-in link from a real tailscaled, and tunnels coming back after a portal
# restart. Skipped on machines without Tailscale. Contacts Tailscale's login server; the new node
# is never signed in.
if command -v tailscaled >/dev/null && command -v tailscale >/dev/null; then
  ln -s "$(command -v tailscaled)" "$T/tailscaled"; ln -s "$(command -v tailscale)" "$T/tailscale"
  R=$(api PUT /api/tunnels/tailscale -d '{"enabled":true}'); [[ $R == *'"ok":true'* ]] || fail "turn on Tailscale: $R"
  for _ in $(seq 1 60); do R=$(api GET /api/tunnels); [[ $R == *'"authUrl":"https://login.tailscale.com/'* ]] && break; sleep 1; done
  [[ $R == *'"authUrl":"https://login.tailscale.com/'* ]] || fail "no Tailscale sign-in link: $R"
  echo "ok: Tailscale sign-in link shown"
  kill "$CAMO"; wait "$CAMO" 2>/dev/null || true
  [ "$(pgrep -fc "[t]ailscaled --tun=userspace-networking --statedir=$T")" = 0 ] || fail "tailscaled outlived the portal"
  "$T/camorage" -data "$T/data" -listen 127.0.0.1:18080 >> "$T/camorage.log" 2>&1 &
  CAMO=$!
  for _ in $(seq 1 40); do curl -fs "$B/api/health" >/dev/null && break; sleep 0.25; done
  for _ in $(seq 1 60); do R=$(api GET /api/tunnels 2>/dev/null) || true; [[ $R == *'"authUrl":"https://login.tailscale.com/'* ]] && break; sleep 1; done
  [[ $R == *'"authUrl":"https://login.tailscale.com/'* ]] || fail "Tailscale did not come back after a restart: $R"
  echo "ok: a portal restart stops tailscaled and starts it again"
else
  echo "skip: no tailscaled on this machine"
fi
echo "ITEST PASS"
