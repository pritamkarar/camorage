# spike/env.sh — source before every M0 command:  . spike/env.sh
# PHONE_SERIAL, SD_UUID and CAM_IP come from .env.local (not in git): see scripts/deploy.sh
[ -f .env.local ] && . ./.env.local
PHONE_IP=$(adb -s "${PHONE_SERIAL:?set PHONE_SERIAL in .env.local}" shell ip -4 addr show wlan0 | tr -d '\r' | awk '/inet /{split($2,a,"/"); print a[1]}')
KEY=~/.ssh/camorage_phone_ed25519
P="ssh -i $KEY -p 8022 -o StrictHostKeyChecking=accept-new -o ConnectTimeout=10 $PHONE_IP"
PCP="scp -i $KEY -P 8022 -o StrictHostKeyChecking=accept-new"
SD=/storage/${SD_UUID:-XXXX-XXXX}/Android/data/com.termux/files
CAM_MAIN=rtsp://${CAM_IP:-192.168.1.30}/live/ch00_0
CAM_SUB=rtsp://${CAM_IP:-192.168.1.30}/live/ch00_1
SCR=$PWD/spike/dl
mkdir -p "$SCR"
