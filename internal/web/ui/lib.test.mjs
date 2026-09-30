import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  DAY_MS, offsetOf, dayStart, todayIn, isoAt, clock, toSpans, blocks, playFrom,
  scheduleSummary, cleanWindows, streamPaths, nextChunk, tailscaleSummary, cloudflareSummary, expandMask, compactMask, cellAt, eventSpans, cloudSpans, spanAt,
} from './lib.js';

const IST = '+05:30';

test('offsetOf reads the offset at the end of an RFC 3339 time', () => {
  assert.equal(offsetOf('2026-09-30T14:03:00+05:30'), '+05:30');
  assert.equal(offsetOf('2026-09-30T08:33:00Z'), '+00:00');
  assert.equal(offsetOf(''), '+00:00');
});

test('days and clocks use the phone offset, not the browser timezone', () => {
  assert.equal(new Date(dayStart('2026-09-30', IST)).toISOString(), '2026-09-29T18:30:00.000Z');
  // 19:00 UTC on the 29th is already 00:30 on the 30th in IST
  assert.equal(todayIn(IST, Date.parse('2026-09-29T19:00:00Z')), '2026-09-30');
  assert.equal(isoAt(Date.parse('2026-09-30T08:32:50Z'), IST), '2026-09-30T14:02:50+05:30');
  assert.equal(clock(Date.parse('2026-09-30T08:32:50Z'), IST), '14:02:50');
});

test('blocks clips spans to the window and returns percentages', () => {
  const start = dayStart('2026-09-30', IST);
  const spans = toSpans([
    { start: '2026-10-01T01:00:00+05:30', durationSec: 60 }, // next day: dropped
    { start: '2026-09-30T12:00:00+05:30', durationSec: 3600 },
    { start: '2026-09-29T23:00:00+05:30', durationSec: 7200 }, // straddles midnight
  ]);
  assert.equal(spans[0].from, Date.parse('2026-09-29T23:00:00+05:30'), 'toSpans sorts by start');
  const b = blocks(spans, start);
  assert.equal(b.length, 2);
  assert.deepEqual(b[0], { left: 0, width: (3600000 / DAY_MS) * 100 });
  assert.deepEqual(b[1], { left: 50, width: (3600000 / DAY_MS) * 100 });
});

test('playFrom plays a recorded moment, snaps a gap forward, gives up after the last span', () => {
  const spans = [{ from: 100, to: 200 }, { from: 500, to: 600 }];
  assert.equal(playFrom(spans, 150), 150);
  assert.equal(playFrom(spans, 300), 500);
  assert.equal(playFrom(spans, 50), 100);
  assert.equal(playFrom(spans, 650), null);
  assert.equal(playFrom([], 10), null);
});

test('scheduleSummary describes windows in words', () => {
  assert.equal(scheduleSummary([]), 'Always');
  assert.equal(scheduleSummary(null), 'Always');
  assert.equal(scheduleSummary([{ days: [1, 2, 3, 4, 5], start: '20:00', end: '08:00' }]), 'Mon–Fri 20:00–08:00');
  assert.equal(scheduleSummary([{ days: [7, 6], start: '00:00', end: '00:00' }]), 'Sat, Sun 00:00–00:00');
  assert.equal(scheduleSummary([{ days: [1, 2, 3, 4, 5, 6, 7], start: '09:00', end: '17:00' }]), 'Every day 09:00–17:00');
});

test('cleanWindows drops rows without days and sorts days', () => {
  assert.deepEqual(
    cleanWindows([{ days: [3, 1, 1], start: '08:00', end: '09:00' }, { days: [], start: '10:00', end: '11:00' }]),
    [{ days: [1, 3], start: '08:00', end: '09:00' }],
  );
});

test('streamPaths uses the substream for tiles when there is one', () => {
  assert.deepEqual(streamPaths({ id: 'cam1', subUrl: 'rtsp://x/sub' }), { tile: 'cam1_sub', full: 'cam1' });
  assert.deepEqual(streamPaths({ id: 'cam2', subUrl: '' }), { tile: 'cam2', full: 'cam2' });
});

test('nextChunk continues after a played chunk and never re-requests an empty one', () => {
  const spans = [{ from: 0, to: 1_200_000 }, { from: 2_000_000, to: 3_000_000 }];
  assert.equal(nextChunk(spans, 0, 600_000), 600_000); // 10 minutes played: continue
  assert.equal(nextChunk(spans, 600_000, 600_000), 2_000_000); // the span ended: jump the gap
  assert.equal(nextChunk(spans, 2_000_000, 0), null); // nothing played: stop instead of looping
  assert.equal(nextChunk(spans, 2_900_000, 100_000), null); // after the last span
});

test('tailscaleSummary names the next step', () => {
  assert.deepEqual(tailscaleSummary({ enabled: false }), { text: 'Off.' });
  assert.equal(tailscaleSummary({ enabled: true, state: 'Running', url: 'https://p.t.ts.net/' }).link, 'https://p.t.ts.net/');
  const s = tailscaleSummary({ enabled: true, state: 'NeedsLogin', authUrl: 'https://login.tailscale.com/a/x' });
  assert.equal(s.link, 'https://login.tailscale.com/a/x');
  assert.match(s.text, /Sign in/);
  assert.match(tailscaleSummary({ enabled: true, state: 'NeedsMachineAuth' }).text, /Approve/);
  assert.deepEqual(tailscaleSummary({ enabled: true, state: '', error: 'waiting for tailscaled: x' }),
    { text: 'waiting for tailscaled: x', error: true });
  assert.deepEqual(tailscaleSummary({ enabled: true, state: 'Starting' }), { text: 'Starting…' });
});

test('cloudflareSummary links the public hostname', () => {
  assert.deepEqual(cloudflareSummary({ tokenSet: false }), { text: 'Off.' });
  assert.deepEqual(cloudflareSummary({ tokenSet: true, hostname: 'cams.example.com', connected: true }),
    { text: 'Connected.', link: 'https://cams.example.com/' });
  const c = cloudflareSummary({ tokenSet: true, hostname: '', connected: false });
  assert.equal(c.link, undefined);
  assert.match(c.text, /Connecting/);
});

test('ignore masks round-trip between the editor and the API', () => {
  assert.equal(expandMask([]).length, 144);
  assert.ok(expandMask(undefined).every((c) => c === false));
  const cells = expandMask([]);
  cells[17] = true;
  const stored = compactMask(cells);
  assert.equal(stored.length, 144);
  assert.equal(stored[17], true);
  assert.deepEqual(expandMask(stored), cells);
  assert.deepEqual(compactMask(expandMask([])), []); // nothing ignored is stored as []
});

test('cellAt maps a point on the picture to its block', () => {
  assert.equal(cellAt(0, 0, 640, 360), 0);
  assert.equal(cellAt(639, 359, 640, 360), 143);
  assert.equal(cellAt(45, 45, 640, 360), 17); // column 1, row 1
  assert.equal(cellAt(640, 10, 640, 360), -1);
  assert.equal(cellAt(-1, 10, 640, 360), -1);
});

test('eventSpans sorts events and runs an open one until now', () => {
  const now = Date.parse('2026-10-01T10:05:00+05:30');
  assert.deepEqual(eventSpans([
    { start: '2026-10-01T10:00:20+05:30', end: '2026-10-01T10:00:40+05:30' },
    { start: '2026-10-01T10:00:00+05:30', end: '2026-10-01T10:00:05+05:30', open: true },
  ], now), [
    { from: Date.parse('2026-10-01T10:00:00+05:30'), to: now },
    { from: Date.parse('2026-10-01T10:00:20+05:30'), to: Date.parse('2026-10-01T10:00:40+05:30') },
  ]);
});

test('cloudSpans turns listed clips into timeline spans', () => {
  assert.deepEqual(cloudSpans([
    { file: '10-00-00_hour_3600s.mp4', start: '2026-10-01T10:00:00+05:30', durationSec: 3600, size: 9 },
    { file: '09-15-00_motion_40s.mp4', start: '2026-10-01T09:15:00+05:30', durationSec: 40, size: 3 },
  ]), [
    { from: Date.parse('2026-10-01T09:15:00+05:30'), to: Date.parse('2026-10-01T09:15:40+05:30'), file: '09-15-00_motion_40s.mp4' },
    { from: Date.parse('2026-10-01T10:00:00+05:30'), to: Date.parse('2026-10-01T11:00:00+05:30'), file: '10-00-00_hour_3600s.mp4' },
  ]);
});

test('spanAt finds the span covering a moment', () => {
  const spans = [{ from: 100, to: 200, file: 'a' }, { from: 300, to: 400, file: 'b' }];
  assert.equal(spanAt(spans, 150).file, 'a');
  assert.equal(spanAt(spans, 300).file, 'b');
  assert.equal(spanAt(spans, 200), null); // the end is exclusive
  assert.equal(spanAt(spans, 250), null);
});
