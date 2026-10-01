import { h, api, playHLS, icon, chip, setChip, pageHead, emptyState, skel } from './dom.js';
import { camStatus, plural, streamPaths, unplayableNote, isH265Reason } from './lib.js';
import { playWHEP } from './whep.js';
import { needsFallback, fallbackAvailable, playH265Live } from './h265.js';

// webrtcUnreachable remembers, for this page load, that WebRTC could not reach the phone (usually
// through Cloudflare from outside), so later full-screen views go straight to HLS.
let webrtcUnreachable = false;

// renderLive shows every enabled camera as a tile (substream over HLS) with a status chip.
// Clicking a tile opens the main stream full screen: WebRTC when the phone is directly reachable
// (home Wi-Fi, Tailscale), otherwise HLS.
export async function renderLive(root) {
  const wait = h('div', {}, pageHead('Live', ''), h('div', { class: 'grid' }, skel('video'), skel('video')));
  root.append(wait);
  const [all, st0] = await Promise.all([api('/api/cameras'), api('/api/status').catch(() => ({ cameras: [] }))])
    .finally(() => wait.remove());
  const codecs = new Map(st0.cameras.map((c) => [c.id, c.codecs || {}])); // kept fresh by refresh()
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
    const tile = h('figure', {
      class: 'tile loading',
      tabindex: 0,
      role: 'button',
      'aria-label': `${cam.name}: open full screen`,
      onclick: () => openFull(cam),
      onkeydown: (e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); openFull(cam); } },
    }, video, h('figcaption', {}, h('span', {}, cam.name), badge));
    grid.append(tile);
    loadingUntilPlaying(tile, video);
    const c = codecs.get(cam.id) || {};
    players.push(watch(tile, video, `/live/hls/${streamPaths(cam).tile}/index.m3u8`, cam.subUrl ? c.sub : c.main));
  }
  root.append(grid);

  async function refresh() {
    try {
      const st = await api('/api/status');
      let recording = 0;
      for (const c of st.cameras) {
        codecs.set(c.id, c.codecs || {});
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
    const box = h('div', { class: 'video-box loading' }, video);
    loadingUntilPlaying(box, video);
    const overlay = h('div', { class: 'overlay', role: 'dialog', 'aria-label': cam.name, onclick: (e) => { if (e.target === overlay) closeFull(); } },
      h('div', { class: 'full' },
        h('div', { class: 'full-bar' }, h('strong', {}, cam.name), mode, closeBtn),
        box));
    document.body.append(overlay);
    closeBtn.focus();
    const me = { overlay, player: null, opener };
    full = me;
    const path = streamPaths(cam).full;
    const hlsURL = `/live/hls/${path}/index.m3u8`;
    const tracks = (codecs.get(cam.id) || {}).main;
    const inBrowser = () => { mode.textContent = 'H.265 · in browser'; };
    let player = null;
    if (needsFallback(tracks)) {
      player = watch(box, video, hlsURL, tracks, inBrowser); // MediaMTX would refuse WebRTC this codec
      inBrowser();
    } else {
      if (!webrtcUnreachable) {
        // Start MediaMTX's HLS muxer now, so a fallback does not also wait for it to warm up.
        fetch(hlsURL, { credentials: 'same-origin' }).catch(() => {});
        try {
          player = await playWHEP(video, `/live/whep/${path}`, 4000, () => {
            if (full === me) openFull(cam); // the WebRTC connection died: reconnect (WebRTC, else HLS)
          });
          mode.textContent = 'WebRTC';
        } catch (err) {
          // e.g. through Cloudflare from outside: don't wait for it again. A codec this browser
          // cannot decode is this camera's problem, not WebRTC's; HLS (or the H.265 decoder) may.
          if (!err.codec) webrtcUnreachable = true;
        }
      }
      if (!player) {
        player = watch(box, video, hlsURL, tracks, inBrowser); // inBrowser() relabels it if hls.js reports H.265
        mode.textContent = 'HLS';
      }
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

  // watch plays url in box: through hls.js in video, or — for an H.265 stream this browser can't
  // decode — through the in-browser decoder on a canvas that takes video's place. A stream whose
  // codec was unknown (camera offline when the page opened) switches when hls.js reports H.265.
  // onInBrowser runs when the decoder takes over. Returns { close }.
  function watch(box, video, url, tracks, onInBrowser = () => {}) {
    let player;
    const inBrowser = () => {
      const canvas = h('canvas', {});
      video.replaceWith(canvas);
      box.classList.add('loading');
      onInBrowser();
      player = playH265Live(canvas, url, {
        onPlaying: () => box.classList.remove('loading'),
        onWaiting: () => box.classList.add('loading'),
        onUnplayable: (why) => unplayable(box, canvas, why),
      });
    };
    if (needsFallback(tracks)) inBrowser();
    else {
      player = playHLS(video, url, (why) => {
        if (isH265Reason(why) && fallbackAvailable()) inBrowser();
        else unplayable(box, video, why);
      });
    }
    return { close: () => player.close() };
  }

  // unplayable puts a note in place of a video (or canvas) this browser cannot play.
  function unplayable(box, el, why) {
    box.classList.remove('loading');
    el.replaceWith(h('p', { class: 'unplayable', role: 'status' }, icon('i-live'), unplayableNote(why)));
  }

  // loadingUntilPlaying shimmers box until video shows frames, and again whenever it starts
  // over (a retry after the camera or MediaMTX went away).
  function loadingUntilPlaying(box, video) {
    video.addEventListener('playing', () => box.classList.remove('loading'));
    video.addEventListener('emptied', () => { if (video.isConnected) box.classList.add('loading'); }); // not once replaced by a note
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
