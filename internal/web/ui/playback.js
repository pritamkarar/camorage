import { h, api, icon, pageHead, emptyState, showError, skel } from './dom.js';
import { CanvasMedia, needsFallback, fallbackAvailable, h265Supported } from './h265.js';
import { DAY_MS, addDays, autoStart, blocks, clock, cloudSpans, dayStart, eventSpans, isoAt, nextChunk, offsetOf, playFrom, plural, skipTarget, spanAt, toSpans, todayIn } from './lib.js';

const CHUNK_S = 600; // each <video> source is 10 minutes; the next one loads when it ends
const HOUR_MS = 3600 * 1000;
const SKIP_MS = 10000; // the player bar's back and forward buttons (and the ← → keys)
const SPEEDS = [1, 2, 4, 8]; // the speed button steps through these

// longDate shows a phone-local date (YYYY-MM-DD) in words, e.g. "Wed, 1 Oct 2026". Noon UTC of
// that date is the same calendar day in every timezone.
const longDate = (date) => new Date(`${date}T12:00:00Z`).toLocaleDateString(undefined,
  { weekday: 'short', day: 'numeric', month: 'short', year: 'numeric', timeZone: 'UTC' });

// renderPlayback shows a 24 h timeline (zoomable to 1 h) of what a camera recorded on a day, in
// the phone's local time. Clicking plays from that moment and continues chunk by chunk. The
// timeline is the only seekbar (a native one would only span the current chunk); the player has
// its own play/pause, 10 s skips, speed and fullscreen. Opening the page plays by itself: the camera
// with today's latest motion event from just before it, else the first camera's last minute
// recorded. Picking another camera or day plays its latest motion event (or last minute) too.
export async function renderPlayback(root) {
  const wait = h('div', {}, pageHead('Playback', ''), skel('video'), skel('timeline'));
  root.append(wait);
  let cams;
  let st;
  let first;
  try {
    [cams, st] = await Promise.all([api('/api/cameras'), api('/api/status')]);
    first = cams.length ? await latestMotionCam(cams, todayIn(offsetOf(st.time))) : null;
  } finally {
    wait.remove();
  }
  if (cams.length === 0) {
    root.append(pageHead('Playback', ''), emptyState('i-playback', 'No cameras yet.',
      h('a', { class: 'button primary', href: '#/cameras' }, icon('i-plus'), 'Add camera')));
    return () => {};
  }
  const off = offsetOf(st.time);
  // Cameras whose recordings go through the in-browser H.265 decoder: those /api/status says are
  // H.265 (in a browser that can't decode it), plus any learned from a failed chunk below.
  const fallbackCams = new Set(st.cameras.filter((c) => needsFallback((c.codecs || {}).main)).map((c) => c.id));
  const state = { cam: first.id, date: todayIn(off), spans: [], events: [], cloud: [], winStart: 0, winLen: DAY_MS, playhead: null };
  let chunkStart = null;
  let chunkCam = null; // the camera chunkStart belongs to
  let cloudClip = false; // a cloud copy is in the <video> (it does not chain into a next chunk)
  let clipFrom = null; // ms where the playing recording begins: the player bar's clock

  const camSel = h('select', { 'aria-label': 'Camera', onchange: () => { state.cam = camSel.value; load(true); } },
    cams.map((c) => h('option', { value: c.id }, c.name)));
  camSel.value = state.cam;
  const dateIn = h('input', { type: 'date', 'aria-label': 'Day', value: state.date, onchange: () => { state.date = dateIn.value; load(true); } });
  const prevBtn = h('button', { class: 'icon-btn', onclick: () => step(-1) }, icon('i-chevron-left'));
  const zoomBtn = h('button', { onclick: () => zoom() }, 'Zoom to 1 h');
  const nextBtn = h('button', { class: 'icon-btn', onclick: () => step(1) }, icon('i-chevron-right'));
  const head = pageHead('Playback', '', camSel, dateIn, zoomBtn);
  const sub = head.querySelector('.sub');
  const video = h('video', { class: 'player', playsinline: true, onclick: () => togglePlay() });
  let canvasMedia = null; // made the first time a camera needs the H.265 decoder
  let media = video; // what is in the player box now: the <video>, or canvasMedia
  const playBtn = h('button', { class: 'icon-btn', 'aria-label': 'Play', disabled: true, onclick: () => togglePlay() }, icon('i-play'));
  const backBtn = h('button', { class: 'icon-btn', 'aria-label': 'Back 10 seconds', title: 'Back 10 s (←)', disabled: true, onclick: () => skip(-1) }, icon('i-rewind'));
  const fwdBtn = h('button', { class: 'icon-btn', 'aria-label': 'Forward 10 seconds', title: 'Forward 10 s (→)', disabled: true, onclick: () => skip(1) }, icon('i-forward'));
  const timeEl = h('span', {});
  let speed = 1; // kept across chunks and cameras while the page is open
  const speedBtn = h('button', { class: 'icon-btn', 'aria-label': 'Speed 1×', title: 'Playback speed', onclick: () => faster() }, '1×');
  const fullBtn = h('button', { class: 'icon-btn', 'aria-label': 'Full screen', onclick: () => toggleFull() }, icon('i-fullscreen'));
  const box = h('div', { class: 'video-box player-box' }, video,
    h('div', { class: 'player-bar' }, backBtn, playBtn, fwdBtn, timeEl, h('span', { class: 'spacer' }), speedBtn, fullBtn));
  const bar = h('div', { class: 'timeline', onclick: (e) => click(e) });
  const ticks = h('div', { class: 'ticks' });
  const cloudKey = h('span', { class: 'cloud', hidden: true }, 'In the cloud');
  const dlText = h('span', {}, 'Download these 10 minutes');
  const download = h('a', { class: 'button', hidden: true, download: '' }, icon('i-download'), dlText);
  const label = h('p', { class: 'muted' }, 'Click the timeline to play.');
  let noteUntil = 0; // a note in label stays this long before "Playing …" may replace it
  const note = (text) => { label.textContent = text; noteUntil = Date.now() + 3000; };
  root.append(head, box, h('div', { class: 'scrub' }, prevBtn, h('div', {}, bar, ticks), nextBtn),
    h('div', { class: 'play-foot' },
      h('div', { class: 'legend' }, h('span', {}, 'Recorded'), h('span', { class: 'motion' }, 'Motion'), cloudKey),
      download),
    label);

  // load shows the picked camera and day. autoplay (opening the page, another camera or day) stops
  // what was playing and then plays the latest motion event (or last minute) of the new pick.
  let loads = 0;
  async function load(autoplay = false) {
    const mine = ++loads;
    if (autoplay) stop();
    state.winStart = dayStart(state.date, off);
    state.winLen = DAY_MS;
    bar.replaceChildren();
    bar.classList.add('loading');
    let spans = [];
    let events = [];
    try {
      const q = `cam=${encodeURIComponent(state.cam)}&date=${state.date}`;
      [spans, events] = await Promise.all([api(`/api/playback/spans?${q}`), api(`/api/playback/events?${q}`)]);
    } catch (err) {
      if (err.message !== 'signed out' && mine === loads) showError(err);
    }
    if (mine !== loads) return; // another camera or day was picked meanwhile
    bar.classList.remove('loading');
    state.spans = toSpans(spans);
    state.events = eventSpans(events);
    state.cloud = [];
    shown();
    loadCloud(state.cam, state.date); // answers later, or not at all: the phone's own recordings are drawn first
    if (!autoplay) return;
    const cam = cams.find((c) => c.id === state.cam);
    const t = autoStart(state.spans, state.events, ((cam && cam.motion && cam.motion.preRollSec) || 0) * 1000);
    if (t != null) play(t);
  }

  async function loadCloud(cam, date) {
    let cloud;
    try {
      cloud = cloudSpans(await api(`/api/playback/cloud-spans?cam=${encodeURIComponent(cam)}&date=${date}`));
    } catch {
      return; // unreachable: local playback is unaffected
    }
    if (cam !== state.cam || date !== state.date) return; // another camera or day was picked meanwhile
    state.cloud = cloud;
    shown();
  }

  function shown() {
    const n = state.events.length;
    const cam = cams.find((c) => c.id === state.cam);
    sub.textContent = `${cam ? cam.name : state.cam} · ${longDate(state.date)}`;
    label.textContent = !state.spans.length && !state.cloud.length ? 'No recordings on this day.'
      : `Click the timeline to play.${n ? ` ${plural(n, 'motion event')}.` : ''}`;
    cloudKey.hidden = !state.cloud.length;
    draw();
  }

  function draw() {
    const parts = blocks(state.spans, state.winStart, state.winLen).map((b) =>
      h('div', { class: 'span', style: { left: `${b.left}%`, width: `${Math.max(b.width, 0.2)}%` } }));
    parts.push(...blocks(state.events, state.winStart, state.winLen).map((b) =>
      h('div', { class: 'mark', style: { left: `${b.left}%`, width: `${Math.max(b.width, 0.3)}%` } })));
    parts.push(...blocks(state.cloud, state.winStart, state.winLen).map((b) =>
      h('div', { class: 'cloud', style: { left: `${b.left}%`, width: `${Math.max(b.width, 0.2)}%` } })));
    const p = state.playhead;
    if (p != null && p >= state.winStart && p < state.winStart + state.winLen) {
      parts.push(h('div', { class: 'playhead', style: { left: `${((p - state.winStart) / state.winLen) * 100}%` } }));
    }
    bar.replaceChildren(...parts);
    const step = state.winLen === DAY_MS ? 3 * HOUR_MS : 10 * 60000;
    const labels = [];
    for (let t = state.winStart; t <= state.winStart + state.winLen; t += step) labels.push(h('span', {}, clock(t, off).slice(0, 5)));
    ticks.replaceChildren(...labels);
    const zoomed = state.winLen !== DAY_MS;
    const day = dayStart(state.date, off);
    zoomBtn.textContent = zoomed ? 'Show 24 h' : 'Zoom to 1 h';
    prevBtn.disabled = zoomed && state.winStart <= day;
    nextBtn.disabled = zoomed ? state.winStart >= day + DAY_MS - HOUR_MS : state.date >= todayIn(off);
    prevBtn.setAttribute('aria-label', zoomed ? 'Previous hour' : 'Previous day');
    nextBtn.setAttribute('aria-label', zoomed ? 'Next hour' : 'Next day');
  }

  // step moves the timeline by a day in the 24 h view, by an hour when zoomed.
  function step(dir) {
    if (state.winLen !== DAY_MS) {
      shift(dir);
      return;
    }
    state.date = addDays(state.date, dir);
    dateIn.value = state.date;
    load(true);
  }

  function clampHour(start) {
    const day = dayStart(state.date, off);
    return Math.min(Math.max(start, day), day + DAY_MS - HOUR_MS);
  }

  function zoom() {
    if (state.winLen !== DAY_MS) {
      state.winStart = dayStart(state.date, off);
      state.winLen = DAY_MS;
    } else {
      const last = state.spans[state.spans.length - 1];
      const center = state.playhead ?? (last ? last.to : state.winStart + DAY_MS / 2);
      state.winLen = HOUR_MS;
      state.winStart = clampHour(center - HOUR_MS / 2);
    }
    draw();
  }

  function shift(dir) {
    state.winStart = clampHour(state.winStart + dir * HOUR_MS);
    draw();
  }

  function click(e) {
    const rect = bar.getBoundingClientRect();
    const t = state.winStart + ((e.clientX - rect.left) / rect.width) * state.winLen;
    const c = spanAt(state.cloud, t);
    if (c && !spanAt(state.spans, t)) { // gone from the phone, still in the cloud
      playCloud(c, t);
      return;
    }
    const from = playFrom(state.spans, t);
    if (from == null) {
      note('Nothing was recorded after this point.');
      return;
    }
    play(from);
  }

  // start plays url, a recording that begins at from (ms), in the player.
  function start(url, from) {
    clipFrom = from;
    noteUntil = 0;
    media.src = url;
    media.play().catch((err) => {
      if (err.name !== 'NotAllowedError') return;
      media.muted = true; // no click on the page yet: browsers allow only muted autoplay
      media.play().catch(() => {});
    });
    box.classList.add('loading');
    playBtn.disabled = backBtn.disabled = fwdBtn.disabled = false;
    timeEl.textContent = clock(from, off);
  }

  // stop empties the player, as before anything played.
  function stop() {
    chunkStart = null;
    cloudClip = false;
    clipFrom = null;
    state.playhead = null;
    use(video); // an emptied canvas would go on showing its last picture
    media.pause();
    media.removeAttribute('src');
    media.load();
    box.classList.remove('loading');
    playBtn.disabled = backBtn.disabled = fwdBtn.disabled = true;
    timeEl.textContent = '';
    download.hidden = true;
  }

  // skip moves the playhead SKIP_MS back (dir -1) or forward (1): a cloud clip within itself, a
  // recording by starting a new chunk there (a chunk is streamed, so it cannot seek), over gaps.
  function skip(dir) {
    if (!media.currentSrc) return;
    if (cloudClip) {
      video.currentTime = Math.max(0, video.currentTime + (dir * SKIP_MS) / 1000); // the browser stops at the end
      return;
    }
    const at = chunkStart != null ? chunkStart + media.currentTime * 1000 : state.playhead; // the playhead also after a failed chunk
    if (at == null) return;
    const t = skipTarget(state.spans, at, dir * SKIP_MS);
    if (t == null) note(dir > 0 ? 'Nothing was recorded after this point.' : 'Nothing was recorded before this point.');
    else play(t);
  }

  // faster steps the speed through SPEEDS, back to 1× after the last.
  function faster() {
    speed = SPEEDS[(SPEEDS.indexOf(speed) + 1) % SPEEDS.length];
    video.defaultPlaybackRate = video.playbackRate = speed; // the default survives a new source
    if (canvasMedia) canvasMedia.playbackRate = speed;
    speedBtn.textContent = `${speed}×`;
    speedBtn.setAttribute('aria-label', `Speed ${speed}×`);
  }

  function play(from) {
    chunkStart = from;
    chunkCam = state.cam;
    cloudClip = false;
    use(fallbackCams.has(state.cam) ? decoderMedia() : video);
    const q = `cam=${encodeURIComponent(state.cam)}&start=${encodeURIComponent(isoAt(from, off))}&duration=${CHUNK_S}`;
    start(`/api/playback/video?${q}&format=fmp4`, from);
    download.href = `/api/playback/video?${q}&format=mp4`;
    download.hidden = false;
    dlText.textContent = 'Download these 10 minutes';
    label.textContent = `Playing from ${clock(from, off)}`;
  }

  function playCloud(c, t) {
    chunkStart = null; // a cloud clip does not chain into the next chunk
    cloudClip = true;
    use(video); // cloud clips are plain MP4: always the <video>
    const url = `/api/playback/cloud?cam=${encodeURIComponent(state.cam)}&date=${state.date}&file=${encodeURIComponent(c.file)}`;
    start(url, c.from);
    video.addEventListener('loadedmetadata', () => { video.currentTime = Math.max(0, (t - c.from) / 1000); }, { once: true });
    download.href = `${url}&download=1`;
    dlText.textContent = 'Download this cloud clip';
    download.hidden = false;
    label.textContent = `Playing the cloud copy from ${clock(t, off)}`;
  }

  // use puts m (the <video> or the H.265 decoder's CanvasMedia) in the player box, stopping the
  // other one.
  function use(m) {
    if (m === media) return;
    media.pause();
    media.removeAttribute('src');
    media.load();
    (media.el || media).replaceWith(m.el || m);
    media = m;
  }

  function decoderMedia() {
    if (!canvasMedia) {
      canvasMedia = new CanvasMedia();
      canvasMedia.playbackRate = speed;
      canvasMedia.el.addEventListener('click', () => togglePlay());
      listen(canvasMedia);
    }
    return canvasMedia;
  }

  function togglePlay() {
    if (!media.currentSrc) return;
    if (media.paused) media.play().catch(() => {});
    else media.pause();
  }

  // toggleFull puts the player (with its bar) full screen. An iPhone can only do that with the
  // video itself, in its own player.
  function toggleFull() {
    if (document.fullscreenElement) document.exitFullscreen().catch(() => {});
    else if (box.requestFullscreen) box.requestFullscreen().catch(() => {});
    else if (media === video && video.webkitEnterFullscreen) video.webkitEnterFullscreen();
  }
  const onFullscreen = () => {
    const on = document.fullscreenElement === box;
    fullBtn.replaceChildren(icon(on ? 'i-fullscreen-exit' : 'i-fullscreen'));
    fullBtn.setAttribute('aria-label', on ? 'Exit full screen' : 'Full screen');
  };
  document.addEventListener('fullscreenchange', onFullscreen);
  // ← and → skip, unless the key is for a field (the camera list, the day).
  const onKey = (e) => {
    if ((e.key !== 'ArrowLeft' && e.key !== 'ArrowRight') || e.altKey || e.ctrlKey || e.metaKey || e.shiftKey) return;
    if (e.target.closest && e.target.closest('input, select, textarea, [contenteditable]')) return;
    e.preventDefault();
    skip(e.key === 'ArrowLeft' ? -1 : 1);
  };
  document.addEventListener('keydown', onKey);

  const playing = (on) => {
    playBtn.replaceChildren(icon(on ? 'i-pause' : 'i-play'));
    playBtn.setAttribute('aria-label', on ? 'Pause' : 'Play');
  };
  // listen wires the player bar, timeline and chunk chaining to m's events. Only the one in the box
  // counts: a stopped player may still fire a late event.
  function listen(m) {
    const on = (type, fn) => m.addEventListener(type, () => { if (m === media) fn(); });
    on('play', () => playing(true));
    on('pause', () => { playing(false); box.classList.remove('loading'); });
    on('waiting', () => box.classList.add('loading'));
    on('playing', () => box.classList.remove('loading'));
    on('timeupdate', () => {
      if (clipFrom != null) timeEl.textContent = clock(clipFrom + media.currentTime * 1000, off);
      if (chunkStart == null) return;
      state.playhead = chunkStart + media.currentTime * 1000;
      const last = state.spans[state.spans.length - 1];
      if (last && state.playhead > last.to) last.to = state.playhead; // still recording: it grew since the page loaded it
      if (Date.now() >= noteUntil) label.textContent = `Playing ${clock(state.playhead, off)}`;
      draw();
    });
    on('ended', () => {
      if (chunkStart == null) return;
      const next = nextChunk(state.spans, chunkStart, media.currentTime * 1000);
      if (next != null && next < dayStart(state.date, off) + DAY_MS) play(next);
      else label.textContent = 'End of the recordings for this day.';
    });
    // A chunk that fails (session ended, recording pruned, a browser that cannot stream it) must
    // say so instead of showing "Playing" over a black player. The status probe turns an ended
    // session into the login screen. A camera whose codec was unknown when the page opened may be
    // H.265: its chunk is tried once more through the decoder (which says not-h265 otherwise).
    // Both are about the chunk's own camera, which may no longer be the one picked.
    on('error', () => {
      box.classList.remove('loading');
      if (chunkStart == null) {
        // a cloud clip that fails (an H.265 camera's, in a browser without H.265) says so too
        if (cloudClip && media === video) {
          cloudClip = false;
          label.textContent = 'This recording could not be played here. Try another moment, or use Download.';
          api('/api/status').catch(() => {});
        }
        return;
      }
      if (media === video && chunkCam === state.cam && !fallbackCams.has(chunkCam) && fallbackAvailable() && !h265Supported()) {
        fallbackCams.add(chunkCam);
        play(chunkStart);
        return;
      }
      if (media !== video && media.error && media.error.message === 'not-h265') fallbackCams.delete(chunkCam);
      chunkStart = null;
      label.textContent = 'This recording could not be played here. Try another moment, or use Download.';
      api('/api/status').catch(() => {});
    });
  }
  listen(video);

  await load(true);
  return () => {
    chunkStart = null;
    document.removeEventListener('fullscreenchange', onFullscreen);
    document.removeEventListener('keydown', onKey);
    if (document.fullscreenElement === box) document.exitFullscreen().catch(() => {});
    media.removeAttribute('src');
    media.load();
    if (canvasMedia) canvasMedia.destroy();
  };
}

// latestMotionCam is the camera with the latest motion event on date, else the first camera.
// A camera whose events cannot be read counts as having none.
async function latestMotionCam(cams, date) {
  const latest = await Promise.all(cams.map((c) =>
    api(`/api/playback/events?cam=${encodeURIComponent(c.id)}&date=${date}`)
      .then((evs) => Math.max(...eventSpans(evs).map((e) => e.from)))
      .catch(() => -Infinity)));
  const i = latest.indexOf(Math.max(...latest));
  return latest[i] > -Infinity ? cams[i] : cams[0];
}
