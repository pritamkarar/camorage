import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import { parseInit, parseFragment, boxSplitter } from './fmp4.js';

const fixture = (name) => new Uint8Array(fs.readFileSync(new URL(`./testdata/${name}`, import.meta.url)));

// whole splits a complete file into its init and fragments, the way the Playback stream reader does.
function whole(u8) {
  const pieces = boxSplitter().push(u8);
  return { init: pieces[0], fragments: pieces.slice(1) };
}

// mdatBytes sums the payload of every mdat in u8: all sample bytes, which with 4-byte NAL length
// prefixes is exactly the Annex-B size of all frames.
function mdatBytes(u8) {
  let n = 0;
  for (let at = 0; at + 8 <= u8.length;) {
    const size = new DataView(u8.buffer, u8.byteOffset + at).getUint32(0);
    if (String.fromCharCode(...u8.subarray(at + 4, at + 8)) === 'mdat') n += size - 8;
    at += size;
  }
  return n;
}

const near = (a, b) => Math.abs(a - b) < 1e-6;

test('parseInit reads codec, timescale and H.265 parameter sets', () => {
  const ff = parseInit(whole(fixture('hevc-ffmpeg.mp4')).init.data);
  assert.equal(ff.codec, 'hvc1');
  assert.ok(ff.timescale > 0);
  assert.equal(ff.lengthSize, 4);
  assert.deepEqual([...ff.paramSets.subarray(0, 4)], [0, 0, 0, 1], 'Annex-B start code');
  const mtx = parseInit(fixture('mtx-hls-init.mp4'));
  assert.ok(mtx.codec === 'hvc1' || mtx.codec === 'hev1', mtx.codec);
  assert.equal(mtx.timescale, 90000);
  const avc = parseInit(whole(fixture('avc-ffmpeg.mp4')).init.data);
  assert.equal(avc.codec, 'avc1');
  assert.equal(avc.paramSets, null, 'no H.265 parameter sets: the worker refuses it');
  assert.equal(parseInit(fixture('mtx-hls-seg.mp4')), null, 'a fragment is not an init');
});

test('parseFragment: ffmpeg layout (tfhd defaults, trun sizes only), every frame, key every 15', () => {
  const { init, fragments } = whole(fixture('hevc-ffmpeg.mp4'));
  const i = parseInit(init.data);
  assert.equal(fragments.length, 2);
  const frames = fragments.flatMap((f) => parseFragment(f.data, i));
  assert.equal(frames.length, 30);
  frames.forEach((f, n) => {
    assert.equal(f.key, n % 15 === 0, `frame ${n} key`);
    assert.ok(near(f.t, n / 15), `frame ${n} t=${f.t}`);
    assert.ok(near(f.dur, 1 / 15));
    assert.deepEqual([...f.data.subarray(0, 4)], [0, 0, 0, 1]);
  });
  assert.equal(frames.reduce((n, f) => n + f.data.length, 0), mdatBytes(fixture('hevc-ffmpeg.mp4')));
});

test('parseFragment: a MediaMTX HLS segment is one GOP of 1/15 s frames', () => {
  const i = parseInit(fixture('mtx-hls-init.mp4'));
  const seg = fixture('mtx-hls-seg.mp4');
  const frames = parseFragment(seg, i);
  assert.ok(frames.length >= 10, `${frames.length} frames`);
  assert.equal(frames[0].key, true);
  assert.ok(frames.slice(1).every((f) => !f.key));
  for (let n = 1; n < frames.length; n++) assert.ok(near(frames[n].t - frames[n - 1].t, 1 / 15), `step ${n}`);
  assert.equal(frames.reduce((n, f) => n + f.data.length, 0), mdatBytes(seg));
});

test('parseFragment: a MediaMTX playback stream starts at 0 and runs 2 s', () => {
  const { init, fragments } = whole(fixture('mtx-get.mp4'));
  const i = parseInit(init.data);
  assert.ok(fragments.length >= 2);
  const frames = fragments.flatMap((f) => parseFragment(f.data, i));
  assert.equal(frames[0].t, 0);
  assert.equal(frames[0].key, true);
  assert.ok(frames.length >= 28 && frames.length <= 32, `${frames.length} frames`);
  for (let n = 1; n < frames.length; n++) assert.ok(frames[n].t > frames[n - 1].t);
  assert.equal(parseFragment(init.data, i).length, 0, 'an init has no frames');
});

test('boxSplitter gives the same pieces however the bytes arrive', () => {
  const file = fixture('mtx-get.mp4');
  const once = boxSplitter().push(file);
  const split = boxSplitter();
  const bits = [];
  for (let at = 0; at < file.length; at += 1000) bits.push(...split.push(file.subarray(at, at + 1000)));
  assert.equal(bits.length, once.length);
  bits.forEach((p, n) => {
    assert.equal(p.kind, n === 0 ? 'init' : 'fragment');
    assert.deepEqual(p.data, once[n].data);
  });
  assert.equal(once.reduce((n, p) => n + p.data.length, 0), file.length, 'nothing lost or doubled');
  const ff = boxSplitter().push(fixture('hevc-ffmpeg.mp4'));
  assert.deepEqual(ff.map((p) => p.kind), ['init', 'fragment', 'fragment'], 'the trailing mfra is no piece');
});
