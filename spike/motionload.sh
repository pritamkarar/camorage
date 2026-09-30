# M0: 4 keyframe-only motion readers on MediaMTX's substream; each writes its output byte count when done.
# usage: bash motionload.sh <seconds>      frames = bytes / 2304 (64x36 gray)
cd ~/spike
for i in 1 2 3 4; do
  timeout "$1" ffmpeg -nostdin -loglevel error -stimeout 10000000 -rtsp_transport tcp -skip_frame nokey \
    -i rtsp://127.0.0.1:8554/cam1_sub -vsync 0 -vf scale=64:36,format=gray -f rawvideo pipe:1 | wc -c > motion$i.bytes &
done
wait
