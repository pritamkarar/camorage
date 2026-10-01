// Pure helpers for the camorage UI. No DOM access, so `node --test` can load this file.
// All times are shown in the phone's timezone, given as its UTC offset (e.g. "+05:30").

export const DAY_MS = 24 * 3600 * 1000;
export const DAY_NAMES = ['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun'];

// offsetOf returns the "+05:30"-style offset at the end of an RFC 3339 time.
export function offsetOf(rfc3339) {
  const m = /(Z|[+-]\d\d:\d\d)$/.exec(rfc3339 || '');
  return !m || m[1] === 'Z' ? '+00:00' : m[1];
}

function offsetMs(offset) {
  const m = /^([+-])(\d\d):(\d\d)$/.exec(offset);
  if (!m) return 0;
  return (m[1] === '-' ? -1 : 1) * (Number(m[2]) * 60 + Number(m[3])) * 60000;
}

// dayStart is local midnight of date "YYYY-MM-DD" on the phone, as epoch ms.
export function dayStart(date, offset) {
  return Date.parse(`${date}T00:00:00${offset}`);
}

// todayIn is today's date on the phone.
export function todayIn(offset, now = Date.now()) {
  return new Date(now + offsetMs(offset)).toISOString().slice(0, 10);
}

// isoAt formats epoch ms as RFC 3339 in the phone's offset (what the playback API expects).
export function isoAt(ms, offset) {
  return new Date(ms + offsetMs(offset)).toISOString().slice(0, 19) + offset;
}

// clock formats epoch ms as "HH:MM:SS" on the phone's clock.
export function clock(ms, offset) {
  return new Date(ms + offsetMs(offset)).toISOString().slice(11, 19);
}

// toSpans turns API spans [{start, durationSec}] into [{from, to}] epoch ms, sorted by start.
export function toSpans(apiSpans) {
  return apiSpans
    .map((s) => {
      const from = Date.parse(s.start);
      return { from, to: from + s.durationSec * 1000 };
    })
    .sort((a, b) => a.from - b.from);
}

// blocks positions spans inside the window [start, start+len) as percentages of its width.
export function blocks(spans, start, len = DAY_MS) {
  const end = start + len;
  return spans
    .filter((s) => s.to > start && s.from < end)
    .map((s) => {
      const from = Math.max(s.from, start);
      const to = Math.min(s.to, end);
      return { left: ((from - start) / len) * 100, width: ((to - from) / len) * 100 };
    });
}

// playFrom is where playback starts for a click at t: t itself if recorded, else the start of the
// next recorded span, else null.
export function playFrom(spans, t) {
  for (const s of spans) {
    if (t >= s.from && t < s.to) return t;
    if (s.from > t) return s.from;
  }
  return null;
}

// nextChunk is where playback continues after a chunk that started at chunkStart played for
// playedMs: the next recorded moment, or null. A chunk that played (almost) nothing ends the
// chain, so a failing or empty chunk can never re-request itself forever.
export function nextChunk(spans, chunkStart, playedMs) {
  if (playedMs < 1000) return null;
  return playFrom(spans, chunkStart + playedMs);
}

// scheduleSummary describes schedule windows, e.g. "Mon–Fri 20:00–08:00"; no windows = "Always".
export function scheduleSummary(windows) {
  if (!windows || windows.length === 0) return 'Always';
  return windows.map((w) => `${daysLabel(w.days)} ${w.start}–${w.end}`).join('; ');
}

function daysLabel(days) {
  const d = [...days].sort((a, b) => a - b);
  if (d.length === 7) return 'Every day';
  const contiguous = d.every((x, i) => i === 0 || x === d[i - 1] + 1);
  if (contiguous && d.length > 2) return `${DAY_NAMES[d[0] - 1]}–${DAY_NAMES[d[d.length - 1] - 1]}`;
  return d.map((x) => DAY_NAMES[x - 1]).join(', ');
}

// cleanWindows turns schedule-editor rows into API windows: days sorted and de-duplicated,
// rows without days or with malformed times dropped.
export function cleanWindows(rows) {
  return rows
    .map((r) => ({ days: [...new Set(r.days)].sort((a, b) => a - b), start: r.start, end: r.end }))
    .filter((w) => w.days.length > 0 && /^\d\d:\d\d$/.test(w.start) && /^\d\d:\d\d$/.test(w.end));
}

// streamPaths gives the MediaMTX paths for a camera's live tile (substream if any) and full view.
export function streamPaths(cam) {
  return { tile: cam.subUrl ? `${cam.id}_sub` : cam.id, full: cam.id };
}

// tailscaleSummary turns the tailscale part of GET /api/tunnels into one status line, with the
// link to follow when there is one, and a state for its chip (see tunnelChip).
export function tailscaleSummary(ts) {
  if (!ts.enabled) return { text: 'Off.', state: 'off' };
  if (ts.url) return { text: 'On. The portal on your tailnet:', link: ts.url, state: 'ok' };
  if (ts.state === 'NeedsLogin' && ts.authUrl) {
    return { text: 'Sign in to add this phone to your tailnet (link expired? turn Tailscale off and on):', link: ts.authUrl, state: 'action' };
  }
  if (ts.state === 'NeedsMachineAuth') return { text: 'Approve this phone in the Tailscale admin console.', state: 'action' };
  if (ts.error) return { text: ts.error, error: true, state: 'error' };
  if (ts.state === 'Running') return { text: 'On, but with no HTTPS name: turn on MagicDNS and HTTPS in the Tailscale admin console.', state: 'action' };
  return { text: 'Starting…', state: 'starting' };
}

// cloudflareSummary does the same for the cloudflare part.
export function cloudflareSummary(cf) {
  if (!cf.tokenSet) return { text: 'Off.', state: 'off' };
  const link = cf.hostname ? `https://${cf.hostname}/` : undefined;
  if (cf.connected) return { text: 'Connected.', link, state: 'ok' };
  return { text: 'Connecting… If this stays, check the token and the tunnel’s public hostname in Cloudflare.', link, state: 'starting' };
}

const TUNNEL_CHIPS = {
  off: ['Off', 'muted'], ok: ['Connected', 'ok'], action: ['Needs action', 'warn'], starting: ['Connecting', 'warn'], error: ['Error', 'bad'],
};

// tunnelChip gives the words and colour of a remote-access chip for a summary's state.
export function tunnelChip(state) {
  const [text, kind] = TUNNEL_CHIPS[state] || TUNNEL_CHIPS.starting;
  return { text, kind };
}

// The motion detection grid: 16×9 blocks, row-major.
export const GRID_COLS = 16;
export const GRID_ROWS = 9;

// expandMask gives the editor one boolean per block (the API stores [] when nothing is ignored).
export function expandMask(mask) {
  return Array.from({ length: GRID_COLS * GRID_ROWS }, (_, i) => Boolean(mask && mask[i]));
}

// compactMask is what the API stores: [] when nothing is ignored, else one boolean per block.
export function compactMask(cells) {
  return cells.some(Boolean) ? cells.map(Boolean) : [];
}

// cellAt is the block under point (x, y) of a w×h picture, or -1 outside it.
export function cellAt(x, y, w, h) {
  if (x < 0 || y < 0 || x >= w || y >= h) return -1;
  return Math.floor((y / h) * GRID_ROWS) * GRID_COLS + Math.floor((x / w) * GRID_COLS);
}

// eventSpans turns API events [{start, end, open}] into [{from, to}] epoch ms, sorted; an event
// still in progress runs until now.
// autoStart is where the Playback page starts playing by itself: preRollMs before the day's last
// motion event (moved forward into recorded footage), else the last minute recorded; null when
// nothing was recorded. spans and events are sorted (toSpans, eventSpans).
export function autoStart(spans, events, preRollMs = 0) {
  if (!spans.length) return null;
  if (events.length) {
    const t = playFrom(spans, events[events.length - 1].from - preRollMs);
    if (t != null) return t;
  }
  const last = spans[spans.length - 1];
  return Math.max(last.from, last.to - 60000);
}

export function eventSpans(events, now = Date.now()) {
  return events
    .map((e) => ({ from: Date.parse(e.start), to: e.open ? Math.max(now, Date.parse(e.end)) : Date.parse(e.end) }))
    .sort((a, b) => a.from - b.from);
}

// cloudSpans turns GET /api/playback/cloud-spans into [{from, to, file}] epoch ms, sorted.
export function cloudSpans(files) {
  return files
    .map((f) => {
      const from = Date.parse(f.start);
      return { from, to: from + f.durationSec * 1000, file: f.file };
    })
    .sort((a, b) => a.from - b.from);
}

// spanAt is the span covering moment t ([from, to)), or null.
export function spanAt(spans, t) {
  return spans.find((s) => t >= s.from && t < s.to) || null;
}

// plural counts things: plural(1, 'camera') is "1 camera", plural(2, 'camera') "2 cameras".
export function plural(n, word) {
  return `${n} ${word}${n === 1 ? '' : 's'}`;
}

const gbOf = (mb) => (mb / 1024).toFixed(1);

// diskUse describes how full the recordings storage is, for the sidebar's storage bar. health is
// the "health" part of GET /api/status. The level turns warn above 85 % used and bad above 95 %.
export function diskUse(health) {
  if (health.diskErr) return { text: `error: ${health.diskErr}`, pct: 0, level: 'bad' };
  const total = health.diskTotalMB;
  if (!(total > 0)) return { text: 'unknown', pct: 0, level: 'ok' };
  const used = Math.max(0, total - health.diskFreeMB);
  const pct = Math.min(100, Math.round((used / total) * 100));
  return { text: `${gbOf(used)} of ${gbOf(total)} GB used`, pct, level: pct > 95 ? 'bad' : pct > 85 ? 'warn' : 'ok' };
}

// camStatus gives a camera's status chip from its entry in GET /api/status (undefined when there
// is none): the same words on the Live tiles and the Cameras page.
export function camStatus(st, enabled = true) {
  if (!enabled || (st && st.enabled === false)) return { text: 'Disabled', kind: 'muted' };
  if (!st || !st.available) return { text: 'Offline', kind: 'bad' };
  if (st.motion) return { text: '● Motion', kind: 'motion' };
  if (st.recording) return { text: '● REC', kind: 'rec' };
  return { text: 'Live', kind: 'ok' };
}

// addDays moves a YYYY-MM-DD date by n calendar days (no timezone involved).
export function addDays(date, n) {
  const d = new Date(`${date}T00:00:00Z`);
  d.setUTCDate(d.getUTCDate() + n);
  return d.toISOString().slice(0, 10);
}

// unplayableNote explains a stream this browser cannot decode. reason is hls.js's error reason,
// which lists the stream's codecs (H.265 shows as hvc1 or hev1).
export function unplayableNote(reason = '') {
  const what = /\b(hvc1|hev1)\./.test(reason) ? 'H.265 video' : "this camera's video";
  return `This browser can't play ${what}. Recording still works.`;
}

// storageSplit gives the Storage page's bar: the percentage of the disk used by recordings, by
// other files, and free. s is GET /api/storage.
export function storageSplit(s) {
  if (!(s.totalMB > 0)) return { recordings: 0, other: 0, free: 100 };
  const pct = (mb) => Math.max(0, Math.min(100, (mb / s.totalMB) * 100));
  const recordings = pct(s.usedMB);
  const free = pct(s.freeMB);
  return { recordings, other: Math.max(0, 100 - recordings - free), free };
}

// cameraSummary is the one line under a camera's name on the Cameras page, e.g.
// "Continuous · all day · 1 day on the phone · Google Drive, 7 days".
export function cameraSummary(cam, targets = []) {
  const when = scheduleSummary(cam.schedule);
  const parts = [cam.mode === 'motion' ? 'Motion only' : 'Continuous', when === 'Always' ? 'all day' : when,
    `${plural(cam.localDays, 'day')} on the phone`];
  if (cam.cloud && cam.cloud.targetId) {
    const t = targets.find((x) => x.id === cam.cloud.targetId);
    parts.push(`${t ? t.name : cam.cloud.targetId}, ${plural(cam.cloud.days, 'day')}`);
  }
  return parts.join(' · ');
}

// installChoice says what Settings → About offers for installing camorage as an app: 'installed'
// (running as the app), 'ready' (the browser offered to install it), 'https' (only an HTTPS address
// can install it) or 'menu' (the browser's own menu, e.g. Share → Add to Home Screen on an iPhone).
export function installChoice({ standalone, offered, secure }) {
  if (standalone) return 'installed';
  if (offered) return 'ready';
  if (!secure) return 'https';
  return 'menu';
}
