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
// link to follow when there is one.
export function tailscaleSummary(ts) {
  if (!ts.enabled) return { text: 'Off.' };
  if (ts.url) return { text: 'On. The portal on your tailnet:', link: ts.url };
  if (ts.state === 'NeedsLogin' && ts.authUrl) {
    return { text: 'Sign in to add this phone to your tailnet (link expired? turn Tailscale off and on):', link: ts.authUrl };
  }
  if (ts.state === 'NeedsMachineAuth') return { text: 'Approve this phone in the Tailscale admin console.' };
  if (ts.error) return { text: ts.error, error: true };
  if (ts.state === 'Running') return { text: 'On, but with no HTTPS name: turn on MagicDNS and HTTPS in the Tailscale admin console.' };
  return { text: 'Starting…' };
}

// cloudflareSummary does the same for the cloudflare part.
export function cloudflareSummary(cf) {
  if (!cf.tokenSet) return { text: 'Off.' };
  const link = cf.hostname ? `https://${cf.hostname}/` : undefined;
  if (cf.connected) return { text: 'Connected.', link };
  return { text: 'Connecting… If this stays, check the token and the tunnel’s public hostname in Cloudflare.', link };
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
