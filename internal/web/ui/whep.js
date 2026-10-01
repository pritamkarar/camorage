// Minimal WHEP (WebRTC-HTTP egress) client for MediaMTX without trickle ICE: gather local
// candidates, POST the offer through the portal, apply the answer, wait for the first frame.
// onLost is called once if the connection later fails (MediaMTX restart, camera reboot, network
// change), so the caller can reconnect instead of showing a frozen last frame as if it were live.
export async function playWHEP(video, url, timeoutMs = 4000, onLost = () => {}) {
  if (typeof RTCPeerConnection === 'undefined') throw new Error('WebRTC is not available');
  const pc = new RTCPeerConnection();
  const stream = new MediaStream();
  let session = null;
  let closing = false;
  let established = false; // failures during setup are the caller's catch path, not onLost
  let lostTimer = null;
  const lost = () => {
    if (closing || !established) return;
    closing = true;
    onLost();
  };
  pc.addEventListener('connectionstatechange', () => {
    clearTimeout(lostTimer);
    if (pc.connectionState === 'failed' || pc.connectionState === 'closed') lost();
    else if (pc.connectionState === 'disconnected') lostTimer = setTimeout(lost, 5000);
  });
  const close = () => {
    closing = true;
    clearTimeout(lostTimer);
    pc.close();
    if (session) fetch(session, { method: 'DELETE', credentials: 'same-origin' }).catch(() => {});
  };
  try {
    pc.addTransceiver('video', { direction: 'recvonly' });
    pc.addTransceiver('audio', { direction: 'recvonly' });
    pc.ontrack = (e) => {
      stream.addTrack(e.track);
      video.srcObject = stream;
    };
    await pc.setLocalDescription(await pc.createOffer());
    await gathered(pc, 2000);
    const res = await fetch(url, {
      method: 'POST',
      credentials: 'same-origin',
      headers: { 'Content-Type': 'application/sdp' },
      body: pc.localDescription.sdp,
    });
    if (res.status !== 201) {
      const err = new Error(`WHEP HTTP ${res.status}`);
      // MediaMTX's 400 when this browser cannot decode the stream: WebRTC itself got through
      err.codec = (await res.text()).includes('codecs not supported by client');
      throw err;
    }
    session = res.headers.get('Location');
    await pc.setRemoteDescription({ type: 'answer', sdp: await res.text() });
    await playing(video, timeoutMs);
    established = true;
    return { close };
  } catch (err) {
    close();
    video.srcObject = null;
    throw err;
  }
}

// gathered waits for ICE gathering to finish, at most maxMs (host candidates come much sooner).
function gathered(pc, maxMs) {
  return new Promise((resolve) => {
    if (pc.iceGatheringState === 'complete') {
      resolve();
      return;
    }
    const timer = setTimeout(resolve, maxMs);
    pc.addEventListener('icegatheringstatechange', () => {
      if (pc.iceGatheringState === 'complete') {
        clearTimeout(timer);
        resolve();
      }
    });
  });
}

function playing(video, ms) {
  return new Promise((resolve, reject) => {
    const timer = setTimeout(() => reject(new Error('no video over WebRTC')), ms);
    video.addEventListener('playing', () => {
      clearTimeout(timer);
      resolve();
    }, { once: true });
    video.play().catch(() => {});
  });
}
