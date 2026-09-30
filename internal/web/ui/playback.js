import { h, api, showError } from './dom.js';
import { DAY_MS, blocks, clock, cloudSpans, dayStart, eventSpans, isoAt, nextChunk, offsetOf, playFrom, spanAt, toSpans, todayIn } from './lib.js';

const CHUNK_S = 600; // each <video> source is 10 minutes; the next one loads when it ends
const HOUR_MS = 3600 * 1000;

// renderPlayback shows a 24 h timeline (zoomable to 1 h) of what a camera recorded on a day, in
// the phone's local time. Clicking plays from that moment and continues chunk by chunk.
export async function renderPlayback(root) {
  const [cams, st] = await Promise.all([api('/api/cameras'), api('/api/status')]);
  if (cams.length === 0) {
    root.append(h('p', { class: 'empty' }, 'No cameras yet.'));
    return () => {};
  }
  const off = offsetOf(st.time);
  const state = { cam: cams[0].id, date: todayIn(off), spans: [], events: [], cloud: [], winStart: 0, winLen: DAY_MS, playhead: null };
  let chunkStart = null;

  const camSel = h('select', { onchange: () => { state.cam = camSel.value; load(); } },
    cams.map((c) => h('option', { value: c.id }, c.name)));
  const dateIn = h('input', { type: 'date', value: state.date, onchange: () => { state.date = dateIn.value; load(); } });
  const prevBtn = h('button', { onclick: () => shift(-1), hidden: true, title: 'Previous hour' }, '◀');
  const zoomBtn = h('button', { onclick: () => zoom() }, 'Zoom to 1 h');
  const nextBtn = h('button', { onclick: () => shift(1), hidden: true, title: 'Next hour' }, '▶');
  const bar = h('div', { class: 'timeline', onclick: (e) => click(e) });
  const ticks = h('div', { class: 'ticks' });
  const label = h('p', { class: 'muted' }, 'Click the timeline to play.');
  const video = h('video', { class: 'player', controls: true, playsinline: true });
  const download = h('a', { class: 'button', hidden: true, download: '' }, 'Download these 10 minutes');
  root.append(h('div', { class: 'toolbar' }, camSel, dateIn, prevBtn, zoomBtn, nextBtn), bar, ticks, label, video, download);

  async function load() {
    state.winStart = dayStart(state.date, off);
    state.winLen = DAY_MS;
    try {
      const q = `cam=${encodeURIComponent(state.cam)}&date=${state.date}`;
      const [spans, events] = await Promise.all([api(`/api/playback/spans?${q}`), api(`/api/playback/events?${q}`)]);
      state.spans = toSpans(spans);
      state.events = eventSpans(events);
    } catch (err) {
      state.spans = [];
      state.events = [];
      showError(root, err);
    }
    state.cloud = [];
    shown();
    loadCloud(state.cam, state.date); // answers later, or not at all: the phone's own recordings are drawn first
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
    label.textContent = !state.spans.length && !state.cloud.length ? 'No recordings on this day.'
      : `Click the timeline to play.${n ? ` ${n} motion event${n === 1 ? '' : 's'} (red marks).` : ''}${state.cloud.length ? ' Green: copies in the cloud.' : ''}`;
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
    zoomBtn.textContent = zoomed ? 'Show 24 h' : 'Zoom to 1 h';
    prevBtn.hidden = !zoomed;
    nextBtn.hidden = !zoomed;
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
      label.textContent = 'Nothing was recorded after this point.';
      return;
    }
    play(from);
  }

  function play(from) {
    chunkStart = from;
    const q = `cam=${encodeURIComponent(state.cam)}&start=${encodeURIComponent(isoAt(from, off))}&duration=${CHUNK_S}`;
    video.src = `/api/playback/video?${q}&format=fmp4`;
    video.play().catch(() => {});
    download.href = `/api/playback/video?${q}&format=mp4`;
    download.hidden = false;
    download.textContent = 'Download these 10 minutes';
    label.textContent = `Playing from ${clock(from, off)}`;
  }

  function playCloud(c, t) {
    chunkStart = null; // a cloud clip does not chain into the next chunk
    const url = `/api/playback/cloud?cam=${encodeURIComponent(state.cam)}&date=${state.date}&file=${encodeURIComponent(c.file)}`;
    video.src = url;
    video.addEventListener('loadedmetadata', () => { video.currentTime = Math.max(0, (t - c.from) / 1000); }, { once: true });
    video.play().catch(() => {});
    download.href = `${url}&download=1`;
    download.textContent = 'Download this cloud clip';
    download.hidden = false;
    label.textContent = `Playing the cloud copy from ${clock(t, off)}`;
  }

  video.addEventListener('timeupdate', () => {
    if (chunkStart == null) return;
    state.playhead = chunkStart + video.currentTime * 1000;
    label.textContent = `Playing ${clock(state.playhead, off)}`;
    draw();
  });
  video.addEventListener('ended', () => {
    if (chunkStart == null) return;
    const next = nextChunk(state.spans, chunkStart, video.currentTime * 1000);
    if (next != null && next < dayStart(state.date, off) + DAY_MS) play(next);
    else label.textContent = 'End of the recordings for this day.';
  });
  // A chunk that fails (session ended, recording pruned, a browser that cannot stream it) must
  // say so instead of showing "Playing" over a black player. The status probe turns an ended
  // session into the login screen.
  video.addEventListener('error', () => {
    if (chunkStart == null) return;
    chunkStart = null;
    label.textContent = 'This recording could not be played here. Try another moment, or use Download.';
    api('/api/status').catch(() => {});
  });

  await load();
  return () => {
    chunkStart = null;
    video.removeAttribute('src');
    video.load();
  };
}
