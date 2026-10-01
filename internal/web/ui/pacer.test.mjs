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
