#!/usr/bin/env bash
# Makes the H.265 fallback's test fixtures. Synthetic test patterns only: the repository is
# public, so never footage from a real camera. Needs ffmpeg with libx265 and libx264 and the
# MediaMTX that scripts/itest.sh downloads into .cache. Rewrites the files in this folder:
#   hevc-ffmpeg.mp4   ffmpeg's fragmented MP4, H.265, 2 s at 15 fps, a key frame every 15 frames
#                     (tfhd defaults, trun with sample sizes only, an mfra box at the end)
#   avc-ffmpeg.mp4    the same in H.264, 1 s (the fallback must refuse it)
#   mtx-hls-init.mp4, mtx-hls-seg.mp4   a MediaMTX HLS init segment and one media segment (H.265)
#   mtx-get.mp4       2 s of a MediaMTX playback stream (/get?format=fmp4, H.265)
set -euo pipefail
cd "$(dirname "$0")"
MTX=$(cd ../../../.. && pwd)/.cache/mediamtx-amd64
[ -x "$MTX" ] || { echo "make-fixtures: run scripts/itest.sh once first (it downloads MediaMTX into .cache)" >&2; exit 1; }
SRC=(-f lavfi -i testsrc=size=320x240:rate=15)
X265=(-c:v libx265 -preset ultrafast -x265-params keyint=15:min-keyint=15:scenecut=0:bframes=0:log-level=error)
FRAG=(-movflags frag_keyframe+empty_moov+default_base_moof)
ffmpeg -nostdin -loglevel error "${SRC[@]}" -t 2 "${X265[@]}" -tag:v hvc1 "${FRAG[@]}" -y hevc-ffmpeg.mp4
ffmpeg -nostdin -loglevel error "${SRC[@]}" -t 1 -c:v libx264 -preset ultrafast -g 15 "${FRAG[@]}" -y avc-ffmpeg.mp4

T=$(mktemp -d)
# shellcheck disable=SC2046 # one pid per word
trap 'kill $(jobs -p) 2>/dev/null || true; wait 2>/dev/null || true; rm -rf "$T"' EXIT
printf '%s' "{\"logLevel\":\"error\",\"api\":false,\"metrics\":false,\"pprof\":false,\"rtmp\":false,\"srt\":false,\"webrtc\":false,\"moq\":false,\"rtspAddress\":\"127.0.0.1:18654\",\"rtspTransports\":[\"tcp\"],\"hls\":true,\"hlsAddress\":\"127.0.0.1:18688\",\"hlsVariant\":\"fmp4\",\"playback\":true,\"playbackAddress\":\"127.0.0.1:18696\",\"paths\":{\"fx\":{\"source\":\"publisher\",\"record\":true,\"recordPath\":\"$T/%path/%Y-%m-%d_%H-%M-%S-%f\"}}}" >"$T/mediamtx.yml"
"$MTX" "$T/mediamtx.yml" >"$T/mediamtx.log" 2>&1 &
sleep 1.5
ffmpeg -nostdin -loglevel error -re "${SRC[@]}" -t 30 "${X265[@]}" -f rtsp -rtsp_transport tcp rtsp://127.0.0.1:18654/fx 2>/dev/null & # killed on exit
sleep 4
B=http://127.0.0.1:18688/fx
J=$T/jar
# MediaMTX starts the HLS muxer on the first request and answers 404 until it has segments
curl -s -L -c "$J" -b "$J" -o /dev/null "$B/index.m3u8" || true
sleep 5
V=$(curl -fsS -L -c "$J" -b "$J" "$B/index.m3u8" | grep -v '^#' | grep . | head -1)
P=$(curl -fsS -L -c "$J" -b "$J" "$B/$V")
curl -fsS -c "$J" -b "$J" -o mtx-hls-init.mp4 "$B/$(echo "$P" | grep -o 'URI="[^"]*"' | cut -d'"' -f2)"
curl -fsS -c "$J" -b "$J" -o mtx-hls-seg.mp4 "$B/$(echo "$P" | grep -v '^#' | grep . | tail -1)"
L=$(curl -fsS "http://127.0.0.1:18696/list?path=fx" | grep -o '"start":"[^"]*"' | head -1 | cut -d'"' -f4)
curl -fsS -G -o mtx-get.mp4 http://127.0.0.1:18696/get --data-urlencode path=fx --data-urlencode "start=$L" --data-urlencode duration=2 --data-urlencode format=fmp4
ls -l ./*.mp4
