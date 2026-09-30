import { h, api, playHLS, icon, chip, setChip, pageHead, emptyState } from './dom.js';
import { camStatus, plural, streamPaths } from './lib.js';
import { playWHEP } from './whep.js';

// webrtcUnreachable remembers, for this page load, that WebRTC could not reach the phone (usually
// through Cloudflare from outside), so later full-screen views go straight to HLS.
let webrtcUnreachable = false;

// renderLive shows every enabled camera as a tile (substream over HLS) with a status chip.
// Clicking a tile opens the main stream full screen: WebRTC when the phone is directly reachable
// (home Wi-Fi, Tailscale), otherwise HLS.
export async function renderLive(root) {
  const all = await api('/api/cameras');
  const cams = all.filter((c) => c.enabled);
  const head = pageHead('Live', plural(cams.length, 'camera'));
  root.append(head);
  if (cams.length === 0) {
    root.append(emptyState('i-live', all.length ? 'All cameras are turned off.' : 'No cameras yet.',
      h('a', { class: 'button primary', href: '#/cameras' }, all.length ? 'Cameras' : [icon('i-plus'), 'Add camera'])));
    return () => {};
  }
  const sub = head.querySelector('.sub');

  const players = [];
  const badges = new Map();
  const grid = h('div', { class: 'grid' });
  for (const cam of cams) {
    const video = h('video', { muted: true, autoplay: true, playsinline: true });
    video.muted = true; // the attribute alone does not satisfy autoplay policies
    const badge = chip('…');
    badges.set(cam.id, badge);
    grid.append(h('figure', {
      class: 'tile',
      tabindex: 0,
      role: 'button',
      'aria-label': `${cam.name}: open full screen`,
      onclick: () => openFull(cam),
      onkeydown: (e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); openFull(cam); } },
    }, video, h('figcaption', {}, h('span', {}, cam.name), badge)));
    players.push(playHLS(video, `/live/hls/${streamPaths(cam).tile}/index.m3u8`));
  }
  root.append(grid);

  async function refresh() {
    try {
      const st = await api('/api/status');
      let recording = 0;
      for (const c of st.cameras) {
        if (c.enabled && c.recording) recording++;
        const badge = badges.get(c.id);
        if (badge) setChip(badge, camStatus(c));
      }
      sub.textContent = `${plural(cams.length, 'camera')} · ${recording} recording`;
    } catch {
      // a sign-out is handled by the app; a failed poll just waits for the next one
    }
  }
  refresh();
  const timer = setInterval(refresh, 3000);

  let full = null;
  async function openFull(cam) {
    const opener = full ? full.opener : document.activeElement;
    closeFull(false);
    const video = h('video', { muted: true, autoplay: true, playsinline: true, controls: true });
    video.muted = true;
    const mode = chip('connecting…');
    const closeBtn = h('button', { class: 'icon-btn', 'aria-label': 'Close', onclick: () => closeFull() }, icon('i-close'));
    const overlay = h('div', { class: 'overlay', role: 'dialog', 'aria-label': cam.name, onclick: (e) => { if (e.target === overlay) closeFull(); } },
      h('div', { class: 'full' },
        h('div', { class: 'full-bar' }, h('strong', {}, cam.name), mode, closeBtn),
        video));
    document.body.append(overlay);
    closeBtn.focus();
    const me = { overlay, player: null, opener };
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

  // closeFull ends the full-screen view; refocus puts the keyboard back on the tile it came from.
  function closeFull(refocus = true) {
    if (!full) return;
    if (full.player) full.player.close();
    full.overlay.remove();
    if (refocus && full.opener && full.opener.isConnected) full.opener.focus();
    full = null;
  }

  const onKey = (e) => { if (e.key === 'Escape') closeFull(); };
  document.addEventListener('keydown', onKey);

  return () => {
    clearInterval(timer);
    document.removeEventListener('keydown', onKey);
    closeFull(false);
    players.forEach((p) => p.close());
  };
}
