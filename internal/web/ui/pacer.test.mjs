import { test } from 'node:test';
import assert from 'node:assert/strict';
import { next, anchor, dueAt, LIVE_LATE_MS } from './pacer.js';

// q makes a queue of frames at 15 fps starting at t0, key frames at the given indexes.
const q = (n, t0 = 0, keys = [0]) => Array.from({ length: n }, (_, i) => ({ t: t0 + i / 15, key: keys.includes(i) }));

test('nothing queued is idle; the first frame decodes now and anchors the clock', () => {
  assert.deepEqual(next([], null, 1000, 'live'), { do: 'idle' });
  assert.deepEqual(next(q(3, 5), null, 1000, 'complete'), { do: 'decode', clock: { media: 5, wall: 1000 } });
});

test('a frame waits until it is due, then decodes on the same clock', () => {
  const clock = anchor(0, 1000);
  const queue = [{ t: 0.5, key: false }];
  assert.equal(dueAt(clock, 0.5), 1500);
  assert.deepEqual(next(queue, clock, 1200, 'live'), { do: 'wait', ms: 300 });
  assert.deepEqual(next(queue, clock, 1500, 'live'), { do: 'decode', clock });
  assert.deepEqual(next(queue, clock, 1530, 'complete'), { do: 'decode', clock }, 'a little late keeps the clock');
});

test('a frame more than SLIP_MS late plays at once and the clock follows it (no catch-up burst)', () => {
  const clock = anchor(0, 1000);
  for (const mode of ['live', 'complete']) {
    assert.deepEqual(next([{ t: 0.5, key: false }], clock, 1800, mode), { do: 'decode', clock: { media: 0.5, wall: 1800 } }, mode);
  }
});

test('live skips to the newest key frame when more than LIVE_LATE_MS late', () => {
  const clock = anchor(0, 1000);
  const queue = q(40, 0, [0, 15, 30]);
  assert.deepEqual(next(queue, clock, 1000 + LIVE_LATE_MS + 1, 'live'), { do: 'drop', keep: 30 });
});

test('live skips when more than maxQueuedS is queued, even on time', () => {
  const queue = q(100, 0, [0, 45, 90]); // 6.6 s queued
  assert.deepEqual(next(queue, anchor(0, 1000), 1000, 'live', 6), { do: 'drop', keep: 90 });
  assert.deepEqual(next(queue, anchor(0, 1000), 1000, 'live', 7), { do: 'decode', clock: anchor(0, 1000) });
});

test('live without a newer key frame plays on, late', () => {
  const late = next(q(10, 0, [0]), anchor(0, 1000), 5000, 'live');
  assert.equal(late.do, 'decode');
  assert.deepEqual(late.clock, { media: 0, wall: 5000 });
});

test('complete never drops', () => {
  const queue = q(200, 0, [0, 15, 30, 45]);
  const r = next(queue, anchor(0, 1000), 60000, 'complete', 1);
  assert.equal(r.do, 'decode');
});

// A machine too slow for the stream (80 ms per 15 fps frame) with 2.7 s HLS segments (one GOP
// each): Live skips ahead now and then and stays within a few seconds of real time. Key frames come
// once a segment, so a skip lands on the newest segment's start: about two segments is the floor.
test('live on a too-slow machine stays within about two segments of real time', () => {
  const FPS = 15;
  const SEG = 2.7;
  const DECODE_MS = 80;
  const N = Math.round(SEG * FPS);
  let now = 0;
  let clock = null;
  let seg = 0;
  let behind = 0;
  const queue = [];
  const arrive = () => {
    for (let i = 0; i < N; i++) queue.push({ t: seg * SEG + i / FPS, key: i === 0 });
    seg++;
  };
  arrive();
  while (now < 120000) {
    while (now >= seg * SEG * 1000) arrive();
    const r = next(queue, clock, now, 'live', 2 * 3);
    if (r.do === 'idle') { now += 5; continue; }
    if (r.do === 'wait') { now += r.ms; continue; }
    if (r.do === 'drop') { queue.splice(0, r.keep); clock = null; continue; }
    clock = r.clock;
    const f = queue.shift();
    now += DECODE_MS;
    behind = Math.max(behind, now / 1000 - f.t); // frame t is captured at wall time t here
  }
  assert.ok(behind < 2 * SEG, `fell ${behind.toFixed(1)} s behind real time`);
});

test('at 2x media time runs twice as fast: a frame 0.5 s on is due 250 ms later', () => {
  const clock = anchor(0, 1000);
  assert.equal(dueAt(clock, 0.5, 2), 1250);
  assert.equal(dueAt(clock, 1, 4), 1250);
  const queue = [{ t: 0.5, key: false }];
  assert.deepEqual(next(queue, clock, 1200, 'complete', 6, 2), { do: 'wait', ms: 50 });
  assert.deepEqual(next(queue, clock, 1250, 'complete', 6, 2), { do: 'decode', clock });
});
