## Task 0
Warning: Permanently added '[192.168.1.29]:8022' (ED25519) to the list of known hosts.
armv7l
ffmpeg version 4.2.1 Copyright (c) 2000-2019 the FFmpeg developers
rclone v1.50.1
ls: cannot access '/system/etc/resolv.conf': No such file or directory
2409:40e0:11ae:47e6:ba3b:abff:fee2:9109
4
MemTotal:        1942424 kB
32318
32318 ffmpeg cpu=1.3% rss=6MB
TOTAL cpu=1.3% of one core (4 cores) rss=6MB
MemFree+Cached: 974 MB
## Task 1
-- default
FAIL https://oauth2.googleapis.com/: Get "https://oauth2.googleapis.com/": dial tcp: lookup oauth2.googleapis.com on [::1]:53: read udp [::1]:39780->[::1]:53: read: connection refused
FAIL https://letsencrypt.org/: Get "https://letsencrypt.org/": dial tcp: lookup letsencrypt.org on [::1]:53: read udp [::1]:34418->[::1]:53: read: connection refused
FAIL https://controlplane.tailscale.com/: Get "https://controlplane.tailscale.com/": dial tcp: lookup controlplane.tailscale.com on [::1]:53: read udp [::1]:36975->[::1]:53: read: connection refused
FAIL https://api.cloudflare.com/: Get "https://api.cloudflare.com/": dial tcp: lookup api.cloudflare.com on [::1]:53: read udp [::1]:41610->[::1]:53: read: connection refused
-- fixed
dns server: [2409:40e0:11ae:47e6:ba3b:abff:fee2:9109]:53
OK   https://oauth2.googleapis.com/: HTTP 404
OK   https://letsencrypt.org/: HTTP 200
OK   https://controlplane.tailscale.com/: HTTP 200
OK   https://api.cloudflare.com/: HTTP 200
## Task 2 (mediamtx v1.21.1)
2026/09/30 07:22:33 INF [path deadcam] [RTSP source] started
2026/09/30 07:22:33 INF [RTSP] started with listeners on :8554 (TCP/RTSP), :8000 (UDP/RTP), :8001 (UDP/RTCP)
2026/09/30 07:22:33 INF [HLS] started with listener on :8888 (TCP/HTTP)
2026/09/30 07:22:33 INF [path cam1] [RTSP source] started
2026/09/30 07:22:33 INF [path cam1_sub] [RTSP source] started
2026/09/30 07:22:33 INF [WebRTC] started with listeners on :8889 (TCP/HTTP), :8189 (UDP/ICE), :8189 (TCP/ICE)
2026/09/30 07:22:33 WAR [MoQ] certificate auto.key not found, generating it from scratch
2026/09/30 07:22:33 INF [MoQ] started with listeners on :8892 (TCP/HTTP2), :8892 (UDP/HTTP3), :8893 (UDP/QUIC)
2026/09/30 07:22:33 INF [API] started with listener on :9997 (TCP/HTTP)
2026/09/30 07:22:33 INF [path cam1_sub] stream is available and online, 1 track (H264)
2026/09/30 07:22:33 INF [path cam1] [recorder] recording 1 track (H264)
2026/09/30 07:22:33 INF [path cam1] stream is available and online, 1 track (H264)
2026/09/30 07:22:33 INF [path cam1_sub] RTP packets are too big (1444 > 1440), remuxing them into smaller ones
2026/09/30 07:22:34 INF [path cam1] RTP packets are too big (1444 > 1440), remuxing them into smaller ones
2026/09/30 07:22:36 ERR [path deadcam] [RTSP source] dial tcp 192.168.1.250:554: connect: no route to host
cam1 online= True inboundBytes= 9013774
cam1_sub online= True inboundBytes= 1804269
deadcam online= True inboundBytes= 0
recorder t1: -rwxr-x--- 1 u0_a155 everybody  1259918 Sep 30 12:55 2026-09-30_12-55-00.mp4
cam1 {'ready': True, 'available': True, 'online': True, 'inboundFramesInError': 0} source: rtspSource
cam1_sub {'ready': True, 'available': True, 'online': True, 'inboundFramesInError': 0} source: rtspSource
deadcam {'ready': False, 'available': False, 'online': True, 'inboundFramesInError': 0} source: rtspSource
h264,1280,720

h264,1280,720
h264,640,360

h264,640,360
total 12480
-rwxr-x--- 1 u0_a155 everybody 3674657 Sep 30 12:53 2026-09-30_07-22-34-843790.mp4
-rwxr-x--- 1 u0_a155 everybody 3631388 Sep 30 12:54 2026-09-30_07-23-35-669499.mp4
-rwxr-x--- 1 u0_a155 everybody 4098171 Sep 30 12:55 2026-09-30_07-24-36-909499.mp4
-rwxr-x--- 1 u0_a155 everybody 1296286 Sep 30 12:55 2026-09-30_07-25-38-329499.mp4
Wed Sep 30 12:55:54 IST 2026
segment 2026-09-30_07-23-35-669499.mp4:
codec_name=h264
width=1280
height=720
duration=61.240000
phone local now: 12:56
newest segment: 2026-09-30_07-25-38-329499.mp4
=> MediaMTX segment names are UTC (local = UTC+5:30)
[{"start":"2026-09-30T07:22:34.84379Z","duration":217.520709,"url":"http://192.168.1.29:9996/get?duration=217.520709\u0026path=cam1\u0026start=2026-09-30T07%3A22%3A34.84379Z"}]http 200, 5470891 bytes, 1.612367s
get duration: 90.040000
err/warn lines: 40
     29 ERR [path deadcam] [RTSP source] dial tcp 192.168.1.250:554: connect: no route to host
      1 WAR [MoQ] certificate auto.key not found, generating it from scratch
      1 WAR [HLS] [muxer cam1_sub] part duration changed from 385ms to 365ms - this will cause an error in iOS clients
      1 WAR [HLS] [muxer cam1_sub] part duration changed from 375ms to 385ms - this will cause an error in iOS clients
      1 WAR [HLS] [muxer cam1_sub] part duration changed from 365ms to 395ms - this will cause an error in iOS clients
      1 WAR [HLS] [muxer cam1_sub] part duration changed from 350ms to 375ms - this will cause an error in iOS clients
dts/timestamp lines: 0
deadcam retry lines: 31
mediamtx alive
2953 mediamtx cpu=6.9% rss=28MB
TOTAL cpu=6.9% of one core (4 cores) rss=28MB
5
## Task 3
MemFree+Cached before: 977 MB
readers: 4
  mWakefulness=Asleep
3897 ffmpeg cpu=3.0% rss=9MB
3900 ffmpeg cpu=2.9% rss=9MB
3902 ffmpeg cpu=3.0% rss=9MB
3906 ffmpeg cpu=3.0% rss=9MB
2953 mediamtx cpu=10.9% rss=24MB
TOTAL cpu=22.8% of one core (4 cores) rss=60MB
## Task 4
WebRTC LAN: first {t:11.51, 1280x720, paused:false} second {t:14.51} (+3.0s/3s) screenshot spike/dl/webrtc-lan.png
2026/09/30 07:29:01 INF [WebRTC] [session f65ac7a8] peer connection established, local candidate: host/udp/2409:40e0:11ae:47e6:26f:64ff:fe19:60d4/8189, remote candidate: prflx/udp/2409:40e0:11ae:47e6:b4c6:e89e:13c8:1520/50277
WebRTC LAN candidate: host/udp IPv6 (phone 2409:…:60d4/8189) ↔ prflx/udp IPv6 laptop
HLS LAN page: first {t:23.63, 1280x720, bufferedEnd 24.30} second {t:26.63, bufferedEnd 27.28} (+3.0s/3s, ~0.65s ahead of playhead)
## Task 5
cloudflared version 2026.9.3 (built 2026-09-24-16:08 UTC)
2026-09-30T07:30:22Z INF Thank you for trying Cloudflare Tunnel. Doing so, without a Cloudflare account, is a quick way to experiment and try it out. However, be aware that these account-less Tunnels have no uptime guarantee, are subject to the Cloudflare Online Services Terms of Use (https://www.cloudflare.com/website-terms/), and Cloudflare reserves the right to investigate your use of Tunnels for violations of such terms. If you intend to use Tunnels in production you should use a pre-created named tunnel by following: https://developers.cloudflare.com/cloudflare-one/connections/connect-apps
2026-09-30T07:30:22Z INF Requesting new quick Tunnel on trycloudflare.com...
failed to request quick Tunnel: Post "https://api.trycloudflare.com/tunnel": dial tcp: lookup api.trycloudflare.com on [::1]:53: read udp [::1]:47453->[::1]:53: read: connection refused
 _____ _____              ___
|  __ \  __ \_____  _____|   |_
started
cat: /data/data/com.termux/files/usr/etc/resolv.conf: No such file or directory
nameserver 2409:40e0:11ae:47e6:ba3b:abff:fee2:9109
nameserver 1.1.1.1
restarted
tunnel: https://guns-attribute-contributor-toe.trycloudflare.com
2026-09-30T07:31:16Z INF Settings: map[ha-connections:1 no-autoupdate:true protocol:quic url:http://127.0.0.1:8888]
2026-09-30T07:31:16Z INF Initial protocol quic
2026-09-30T07:31:17Z INF |  SUMMARY: Environment is healthy. cloudflared will use 'quic' as primary protocol.  |
2026-09-30T07:31:17Z INF precheck complete hard_fail=false run_id=ed759d77-b1b6-4149-8c6d-a0b3d8fe94f7 suggested_protocol=quic
2026-09-30T07:31:18Z INF Registered tunnel connection connIndex=0 connection=bb527b3a-8a27-467b-9fca-84972898be5c event=0 ip=2606:4700:a8::1 location=del04 protocol=quic
4829 proot cpu=0.0% rss=0MB
4834 cloudflared cpu=8.5% rss=22MB
2953 mediamtx cpu=14.9% rss=33MB
TOTAL cpu=23.4% of one core (4 cores) rss=55MB
through tunnel: h264,1280,720
playlist via tunnel: http 302 in 0.294072s
## Task 6 (tailscale_1.102.4_arm.tgz)
2026/09/30 07:33:05 health(warnable=wantrunning-false): error: Tailscale is stopped.
2026/09/30 07:33:06 logtail: dial "log.tailscale.com:443" failed: dial tcp: lookup log.tailscale.com on [::1]:53: read udp [::1]:38490->[::1]:53: read: connection refused (in 6ms), trying bootstrap...
2026/09/30 07:33:06 trying bootstrapDNS("derp4d.tailscale.com", "134.122.94.167") for "log.tailscale.com" ...
2026/09/30 07:33:08 bootstrapDNS("derp4d.tailscale.com", "134.122.94.167") for "log.tailscale.com" = [2606:b740:1:20::102 199.165.136.100]
2026/09/30 07:33:08 monitor: RTM_NEWROUTE: src=, dst=2606:b740:1:20::102/128, gw=fe80::ba3b:abff:fee2:9109, outif=25, table=1025
2026/09/30 07:33:08 logtail: bootstrap dial succeeded
tailscaled userspace: runs WITHOUT proot — system DNS fails, bootstrapDNS via DERP succeeds
tailscale ip: 100.64.0.7
HLS over tailnet: h264,1280,720

https://camorage-phone.tail0a1b2c.ts.net/
|-- proxy http://127.0.0.1:8888

Serve started and running in the background.
To disable the proxy, run: tailscale serve --https=443 off
serve url: https://camorage-phone.tail0a1b2c.ts.net
serve HTTPS: http 000, tls verify 1, 0.036242s (first request may include cert issuance)
HLS via serve HTTPS: [tls @ 0x5dfbfeea3100] A TLS fatal alert has been received.
2026/09/30 07:36:36 health(warnable=tls-cert-pending): error: Fetching TLS certificate via ACME for: camorage-phone.tail0a1b2c.ts.net
2026/09/30 07:36:36 cert("camorage-phone.tail0a1b2c.ts.net"): getCertPEM: acme.GetReg: Get "https://acme-v02.api.letsencrypt.org/directory": dial tcp: lookup acme-v02.api.letsencrypt.org on [::1]:53: read udp [::1]:33213->[::1]:53: read: connection refused
2026/09/30 07:36:36 http: TLS handshake error from 100.70.24.39:44598: acme.GetReg: Get "https://acme-v02.api.letsencrypt.org/directory": dial tcp: lookup acme-v02.api.letsencrypt.org on [::1]:53: read udp [::1]:33213->[::1]:53: read: connection refused
2026/09/30 07:36:36 health(warnable=tls-cert-pending): ok
2026/09/30 07:36:36 health(warnable=tls-cert-pending): error: Fetching TLS certificate via ACME for: camorage-phone.tail0a1b2c.ts.net
2026/09/30 07:36:36 cert("camorage-phone.tail0a1b2c.ts.net"): getCertPEM: acme.GetReg: Get "https://acme-v02.api.letsencrypt.org/directory": dial tcp: lookup acme-v02.api.letsencrypt.org on [::1]:53: read udp [::1]:42403->[::1]:53: read: connection refused
2026/09/30 07:36:36 health(warnable=tls-cert-pending): ok
2026/09/30 07:36:36 http: TLS handshake error from 100.70.24.39:44606: acme.GetReg: Get "https://acme-v02.api.letsencrypt.org/directory": dial tcp: lookup acme-v02.api.letsencrypt.org on [::1]:53: read udp [::1]:42403->[::1]:53: read: connection refused
root cause: tailscale serve HTTPS cert (ACME → acme-v02.api.letsencrypt.org) uses system DNS, not bootstrapDNS → fails without resolv.conf
attempt 1: 200 verify=0 21.139386s
HLS via serve HTTPS (proot): h264,1280,720
## Task 3 (soak result)
motion1.bytes 283 frames
motion2.bytes 283 frames
motion3.bytes 283 frames
motion4.bytes 283 frames
motionload.log: []
mediamtx alive
recorder alive
MemFree+Cached after: 967 MB
screen during soak end: mWakefulness=Asleep
WebRTC via http://100.64.0.7:8889/cam1: first {t:14.92, 1280x720} second {t:17.92} (+3.0s/3s)
2026/09/30 07:38:30 INF [WebRTC] [session fe8841e6] peer connection established, local candidate: host/udp/2409:40e0:11ae:47e6:26f:64ff:fe19:60d4/8189, remote candidate: prflx/udp/2409:40e0:11ae:47e6:b4c6:e89e:13c8:1520/45514
WebRTC tailnet-only (webrtcIPsFromInterfaces: no): first {t:17.91, 1280x720} second {t:20.91} (+3.0s/3s)
2026/09/30 07:39:27 INF [WebRTC] [session 55890af2] peer connection established, local candidate: host/udp/127.0.0.1/8189, remote candidate: prflx/udp/127.0.0.1/42108
