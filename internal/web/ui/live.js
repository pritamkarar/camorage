import { h, api, playHLS } from './dom.js';
import { streamPaths } from './lib.js';
import { playWHEP } from './whep.js';

// webrtcUnreachable remembers, for this page load, that WebRTC could not reach the phone (usually
// through Cloudflare from outside), so later full-screen views go straight to HLS.
let webrtcUnreachable = false;

// renderLive shows every enabled camera as a tile (substream over HLS) with a status badge.
// Clicking a tile opens the main stream full screen: WebRTC when the phone is directly reachable
// (home Wi-Fi, Tailscale), otherwise HLS.
export async function renderLive(root) {
  const cams = (await api('/api/cameras')).filter((c) => c.enabled);
  if (cams.length === 0) {
    root.append(h('p', { class: 'empty' }, 'No cameras yet. ', h('a', { href: '#/cameras' }, 'Add one')));
    return () => {};
  }

  const players = [];
  const badges = new Map();
  const grid = h('div', { class: 'grid' });
  for (const cam of cams) {
    const video = h('video', { muted: true, autoplay: true, playsinline: true });
    video.muted = true; // the attribute alone does not satisfy autoplay policies
    const badge = h('span', { class: 'badge' }, '…');
    badges.set(cam.id, badge);
    grid.append(h('figure', {
      class: 'tile',
      tabindex: 0,
      onclick: () => openFull(cam),
      onkeydown: (e) => { if (e.key === 'Enter') openFull(cam); },
    }, video, h('figcaption', {}, h('span', {}, cam.name), badge)));
    players.push(playHLS(video, `/live/hls/${streamPaths(cam).tile}/index.m3u8`));
  }
  root.append(grid);

  async function refresh() {
    try {
      const st = await api('/api/status');
      for (const c of st.cameras) {
        const badge = badges.get(c.id);
        if (!badge) continue;
        const [text, cls] = !c.available ? ['offline', 'bad'] : c.motion ? ['● motion', 'motion'] : c.recording ? ['● REC', 'rec'] : ['live', 'ok'];
        badge.textContent = text;
        badge.className = `badge ${cls}`;
      }
    } catch {
      // a sign-out is handled by the app; a failed poll just waits for the next one
    }
  }
  refresh();
  const timer = setInterval(refresh, 3000);

  let full = null;
  async function openFull(cam) {
    closeFull();
    const video = h('video', { muted: true, autoplay: true, playsinline: true, controls: true });
    video.muted = true;
    const mode = h('span', { class: 'badge' }, 'connecting…');
    const overlay = h('div', { class: 'overlay', onclick: (e) => { if (e.target === overlay) closeFull(); } },
      h('div', { class: 'full' },
        h('div', { class: 'full-bar' }, h('strong', {}, cam.name), mode, h('button', { onclick: closeFull }, 'Close')),
        video));
    document.body.append(overlay);
    const me = { overlay, player: null };
    full = me;
    const path = streamPaths(cam).full;
    const hlsURL = `/live/hls/${path}/index.m3u8`;
    let player = null;
    if (!webrtcUnreachable) {
      // Start MediaMTX's HLS muxer now, so a fallback does not also wait for it to warm up.
      fetch(hlsURL, { credentials: 'same-origin' }).catch(() => {});
      try {
        player = await playWHEP(video, `/live/whep/${path}`, 4000, () => {
          if (full === me) openFull(cam); // the WebRTC connection died: reconnect (WebRTC, else HLS)
        });
        mode.textContent = 'WebRTC';
      } catch {
        webrtcUnreachable = true; // e.g. through Cloudflare from outside: don't wait for it again
      }
    }
    if (!player) {
      player = playHLS(video, hlsURL);
      mode.textContent = 'HLS';
    }
    if (full !== me) {
      player.close(); // closed while connecting
      return;
    }
    me.player = player;
  }

  function closeFull() {
    if (!full) return;
    if (full.player) full.player.close();
    full.overlay.remove();
    full = null;
  }

  const onKey = (e) => { if (e.key === 'Escape') closeFull(); };
  document.addEventListener('keydown', onKey);

  return () => {
    clearInterval(timer);
    document.removeEventListener('keydown', onKey);
    closeFull();
    players.forEach((p) => p.close());
  };
}
