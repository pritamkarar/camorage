import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  DAY_MS, offsetOf, dayStart, todayIn, isoAt, clock, toSpans, blocks, playFrom,
  scheduleSummary, cleanWindows, streamPaths, nextChunk, tailscaleSummary, cloudflareSummary, expandMask, compactMask, cellAt, eventSpans, cloudSpans, spanAt,
  plural, diskUse, camStatus, storageSplit, cameraSummary, tunnelChip, installChoice, unplayableNote, addDays, autoStart,
  isH265Reason, usesH265, firstVariant, mediaPlaylist, skipTarget,
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

test('tailscaleSummary names the next step and a state for its chip', () => {
  assert.deepEqual(tailscaleSummary({ enabled: false }), { text: 'Off.', state: 'off' });
  const on = tailscaleSummary({ enabled: true, state: 'Running', url: 'https://p.t.ts.net/' });
  assert.equal(on.link, 'https://p.t.ts.net/');
  assert.equal(on.state, 'ok');
  const s = tailscaleSummary({ enabled: true, state: 'NeedsLogin', authUrl: 'https://login.tailscale.com/a/x' });
  assert.equal(s.link, 'https://login.tailscale.com/a/x');
  assert.match(s.text, /Sign in/);
  assert.equal(s.state, 'action');
  const approve = tailscaleSummary({ enabled: true, state: 'NeedsMachineAuth' });
  assert.match(approve.text, /Approve/);
  assert.equal(approve.state, 'action');
  assert.equal(tailscaleSummary({ enabled: true, state: 'Running' }).state, 'action'); // no HTTPS name
  assert.deepEqual(tailscaleSummary({ enabled: true, state: '', error: 'waiting for tailscaled: x' }),
    { text: 'waiting for tailscaled: x', error: true, state: 'error' });
  assert.deepEqual(tailscaleSummary({ enabled: true, state: 'Starting' }), { text: 'Starting…', state: 'starting' });
});

test('cloudflareSummary links the public hostname and gives a state', () => {
  assert.deepEqual(cloudflareSummary({ tokenSet: false }), { text: 'Off.', state: 'off' });
  assert.deepEqual(cloudflareSummary({ tokenSet: true, hostname: 'cams.example.com', connected: true }),
    { text: 'Connected.', link: 'https://cams.example.com/', state: 'ok' });
  const c = cloudflareSummary({ tokenSet: true, hostname: '', connected: false });
  assert.equal(c.link, undefined);
  assert.match(c.text, /Connecting/);
  assert.equal(c.state, 'starting');
});

test('tunnelChip turns a state into chip words and colour', () => {
  assert.deepEqual(tunnelChip('off'), { text: 'Off', kind: 'muted' });
  assert.deepEqual(tunnelChip('ok'), { text: 'Connected', kind: 'ok' });
  assert.deepEqual(tunnelChip('action'), { text: 'Needs action', kind: 'warn' });
  assert.deepEqual(tunnelChip('starting'), { text: 'Connecting', kind: 'warn' });
  assert.deepEqual(tunnelChip('error'), { text: 'Error', kind: 'bad' });
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

test('plural counts things', () => {
  assert.equal(plural(1, 'camera'), '1 camera');
  assert.equal(plural(0, 'camera'), '0 cameras');
  assert.equal(plural(3, 'day'), '3 days');
});

test('diskUse describes the recordings storage for the sidebar bar', () => {
  assert.deepEqual(diskUse({ diskFreeMB: 6246, diskTotalMB: 16282 }), { text: '9.8 of 15.9 GB used', pct: 62, level: 'ok' });
  assert.equal(diskUse({ diskFreeMB: 100, diskTotalMB: 1000 }).level, 'warn'); // 90 % used
  assert.equal(diskUse({ diskFreeMB: 150, diskTotalMB: 1000 }).level, 'ok'); // exactly 85 %: not yet
  assert.equal(diskUse({ diskFreeMB: 40, diskTotalMB: 1000 }).level, 'bad'); // 96 % used
  assert.deepEqual(diskUse({ diskFreeMB: 0, diskTotalMB: 0 }), { text: 'unknown', pct: 0, level: 'ok' });
  assert.deepEqual(diskUse({ diskFreeMB: 0, diskTotalMB: 0, diskErr: 'no such file' }), { text: 'error: no such file', pct: 0, level: 'bad' });
  assert.equal(diskUse({ diskFreeMB: 2000, diskTotalMB: 1000 }).pct, 0); // free > total: never negative
});

test('camStatus gives a camera the same chip on every page', () => {
  assert.deepEqual(camStatus({ available: true, recording: true, motion: false }), { text: '● REC', kind: 'rec' });
  assert.deepEqual(camStatus({ available: true, recording: true, motion: true }), { text: '● Motion', kind: 'motion' });
  assert.deepEqual(camStatus({ available: true, recording: false }), { text: 'Live', kind: 'ok' });
  assert.deepEqual(camStatus({ available: false }), { text: 'Offline', kind: 'bad' });
  assert.deepEqual(camStatus(undefined), { text: 'Offline', kind: 'bad' });
  assert.deepEqual(camStatus({ available: true }, false), { text: 'Disabled', kind: 'muted' });
  assert.deepEqual(camStatus({ enabled: false, available: false }), { text: 'Disabled', kind: 'muted' });
});

test('storageSplit divides the disk for the Storage page bar', () => {
  assert.deepEqual(storageSplit({ usedMB: 8000, freeMB: 4000, totalMB: 16000 }), { recordings: 50, other: 25, free: 25 });
  assert.deepEqual(storageSplit({ usedMB: 9000, freeMB: 8000, totalMB: 16000 }), { recordings: 56.25, other: 0, free: 50 }); // never negative
  assert.deepEqual(storageSplit({ usedMB: 0, freeMB: 0, totalMB: 0 }), { recordings: 0, other: 0, free: 100 });
});

test('cameraSummary describes a camera in one line', () => {
  const targets = [{ id: 'google-drive', name: 'Google Drive' }];
  assert.equal(cameraSummary({ mode: 'continuous', schedule: [], localDays: 1, cloud: { targetId: 'google-drive', days: 7 } }, targets),
    'Continuous · all day · 1 day on the phone · Google Drive, 7 days');
  assert.equal(cameraSummary({ mode: 'motion', schedule: [{ days: [1, 2, 3, 4, 5, 6, 7], start: '22:00', end: '06:00' }], localDays: 3 }),
    'Motion only · Every day 22:00–06:00 · 3 days on the phone');
  assert.equal(cameraSummary({ mode: 'continuous', schedule: null, localDays: 2, cloud: { targetId: 'gone', days: 1 } }, targets),
    'Continuous · all day · 2 days on the phone · gone, 1 day');
});

test('installChoice says what Settings can offer for installing the app', () => {
  assert.equal(installChoice({ standalone: true, offered: true, secure: true }), 'installed');
  assert.equal(installChoice({ standalone: false, offered: true, secure: true }), 'ready');
  assert.equal(installChoice({ standalone: false, offered: false, secure: false }), 'https');
  assert.equal(installChoice({ standalone: false, offered: false, secure: true }), 'menu');
});

test('unplayableNote names H.265 from hls.js\'s codec error, else stays general', () => {
  assert.equal(unplayableNote('one or more CODECS in variant not supported: ["hvc1.1.6.L93.90"]'), "This browser can't play H.265 video. Recording still works.");
  assert.equal(unplayableNote('one or more CODECS in variant not supported: ["hev1.1.6.L93.90"]'), "This browser can't play H.265 video. Recording still works.");
  assert.equal(unplayableNote(undefined), "This browser can't play this camera's video. Recording still works.");
});

test('addDays steps a calendar date across months, years and leap days', () => {
  assert.equal(addDays('2026-10-01', -1), '2026-09-30');
  assert.equal(addDays('2026-12-31', 1), '2027-01-01');
  assert.equal(addDays('2028-02-28', 1), '2028-02-29');
  assert.equal(addDays('2028-03-01', -1), '2028-02-29');
});

test('autoStart plays just before the last motion event, else the last minute recorded', () => {
  const S = 1000;
  const day = [{ from: 0, to: 3600 * S }];
  const evs = [{ from: 600 * S, to: 660 * S }, { from: 1800 * S, to: 1850 * S }];
  assert.equal(autoStart(day, evs, 5 * S), 1795 * S, 'last event, pre-roll earlier');
  const gappy = [{ from: 0, to: 1000 * S }, { from: 2000 * S, to: 3000 * S }];
  assert.equal(autoStart(gappy, [{ from: 2002 * S, to: 2010 * S }], 5 * S), 2000 * S, 'pre-roll in a gap: first recorded moment after it');
  assert.equal(autoStart(gappy, [{ from: 3500 * S, to: 3510 * S }], 5 * S), 2940 * S, 'event after the recordings: last minute');
  assert.equal(autoStart(day, [], 5 * S), 3540 * S, 'no motion: last minute');
  assert.equal(autoStart([{ from: 0, to: 1000 * S }, { from: 2000 * S, to: 2030 * S }], []), 2000 * S, 'newest recording shorter than a minute: its start');
  assert.equal(autoStart([], evs, 5 * S), null, 'nothing recorded');
});

test('isH265Reason recognises hls.js codec errors and the fallback’s own reasons', () => {
  assert.equal(isH265Reason('one or more CODECS in variant not supported: ["hvc1.1.6.L93.90"]'), true);
  assert.equal(isH265Reason('one or more CODECS in variant not supported: ["hev1.1.6.L93.90"]'), true);
  assert.equal(isH265Reason('H.265 decoder: no-webgl'), true);
  assert.equal(isH265Reason('one or more CODECS in variant not supported: ["avc1.4d0028"]'), false);
  assert.equal(isH265Reason(undefined), false);
  assert.equal(unplayableNote('H.265 decoder: no-webgl'), "This browser can't play H.265 video. Recording still works.");
});

test('usesH265 reads the codec lists of /api/status', () => {
  assert.equal(usesH265(['H265']), true);
  assert.equal(usesH265(['H265', 'Opus']), true);
  assert.equal(usesH265(['H264']), false);
  assert.equal(usesH265(null), false);
  assert.equal(usesH265(undefined), false);
});

test('firstVariant and mediaPlaylist read MediaMTX playlists', () => {
  const master = '#EXTM3U\n#EXT-X-VERSION:10\n#EXT-X-INDEPENDENT-SEGMENTS\n\n#EXT-X-STREAM-INF:BANDWIDTH=141541,CODECS="hvc1.1.6.L93.90",RESOLUTION=640x720\nvideo1_stream.m3u8?session=abc\n';
  const base = 'http://phone:8080/live/hls/cam_sub/index.m3u8';
  const variant = firstVariant(master, base);
  assert.equal(variant, 'http://phone:8080/live/hls/cam_sub/video1_stream.m3u8?session=abc');
  assert.equal(firstVariant('#EXTM3U\n', base), null);
  const media = '#EXTM3U\n#EXT-X-VERSION:10\n#EXT-X-TARGETDURATION:3\n#EXT-X-MEDIA-SEQUENCE:8\n#EXT-X-MAP:URI="9d32_video1_init.mp4?session=abc"\n#EXTINF:2.66500,\n9d32_video1_seg8.mp4?session=abc\n#EXTINF:2.67500,\n9d32_video1_seg9.mp4?session=abc\n';
  assert.deepEqual(mediaPlaylist(media, variant), {
    init: 'http://phone:8080/live/hls/cam_sub/9d32_video1_init.mp4?session=abc',
    target: 3,
    segments: [
      { seq: 8, url: 'http://phone:8080/live/hls/cam_sub/9d32_video1_seg8.mp4?session=abc' },
      { seq: 9, url: 'http://phone:8080/live/hls/cam_sub/9d32_video1_seg9.mp4?session=abc' },
    ],
  });
});

test('skipTarget: 10 s steps through a day of recordings, over the gaps', () => {
  const S = 1000;
  const spans = [{ from: 0, to: 100 * S }, { from: 200 * S, to: 300 * S }];
  assert.equal(skipTarget(spans, 50 * S, 10 * S), 60 * S, 'forward inside a recording');
  assert.equal(skipTarget(spans, 50 * S, -10 * S), 40 * S, 'back inside a recording');
  assert.equal(skipTarget(spans, 95 * S, 10 * S), 200 * S, 'forward over a gap: the next recording');
  assert.equal(skipTarget(spans, 205 * S, -10 * S), 90 * S, 'back over a gap: 10 s before the previous one ends');
  assert.equal(skipTarget([{ from: 0, to: 5 * S }, { from: 200 * S, to: 300 * S }], 205 * S, -10 * S), 0, 'a previous recording shorter than 10 s: its start');
  assert.equal(skipTarget(spans, 295 * S, 10 * S), null, 'forward past the last recording: nowhere');
  assert.equal(skipTarget(spans, 5 * S, -10 * S), 0, 'back before the first recording: its start');
  assert.equal(skipTarget(spans, 0, -10 * S), null, 'back from the very start: nowhere');
});
