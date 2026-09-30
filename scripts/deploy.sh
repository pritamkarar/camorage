#!/usr/bin/env bash
# Build camorage for the phone and (re)start it in Termux over SSH. Development only: phones install with scripts/install.sh (README).
set -euo pipefail
cd "$(dirname "$0")/.."
# The phone: PHONE_IP, or PHONE_SERIAL (adb) to look it up; either can live in .env.local (not in git).
[ -f .env.local ] && . ./.env.local
PHONE_IP=${PHONE_IP:-$(adb -s "${PHONE_SERIAL:?set PHONE_IP or PHONE_SERIAL, e.g. in .env.local}" shell ip -4 addr show wlan0 | tr -d '\r' | awk '/inet /{split($2,a,"/"); print a[1]}')}
KEY=~/.ssh/camorage_phone_ed25519
SSH=(ssh -i "$KEY" -p 8022 -o ConnectTimeout=10 "$PHONE_IP")
mkdir -p .cache
CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build -trimpath -ldflags='-s -w' -o .cache/camorage-armv7 ./cmd/camorage
"${SSH[@]}" 'mkdir -p ~/camorage/bin && cd ~/camorage/bin && for b in mediamtx cloudflared tailscale tailscaled; do [ -x $b ] || cp ~/spike/$b $b; done'
scp -q -i "$KEY" -P 8022 .cache/camorage-armv7 "$PHONE_IP:camorage/bin/camorage.new"
"${SSH[@]}" 'cat > ~/camorage/start.sh <<"EOF"
#!/data/data/com.termux/files/usr/bin/bash
# Start camorage in the background unless it is running (the same start.sh install.sh writes).
cd ~/camorage || exit 1
pgrep -x camorage >/dev/null && exit 0
[ ! -f camorage.log ] || mv -f camorage.log camorage.log.1
( trap '' HUP; exec -a camorage bin/camorage > camorage.log 2>&1 </dev/null ) &
EOF
chmod +x ~/camorage/start.sh'
"${SSH[@]}" 'pkill -x camorage; for i in $(seq 1 20); do pgrep -x camorage >/dev/null || break; sleep 0.5; done; cd ~/camorage && mv bin/camorage.new bin/camorage && chmod +x bin/camorage && ./start.sh && sleep 3 && tail -3 camorage.log'
for _ in $(seq 1 20); do curl -fsS "http://$PHONE_IP:8080/api/health" && echo && exit 0; sleep 1; done
echo "portal did not answer on http://$PHONE_IP:8080" && exit 1
