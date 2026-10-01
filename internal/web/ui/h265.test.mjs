// h265.js against a fake worker (node has no Worker, OffscreenCanvas or WebGL): the page side's
// handling of the worker's messages and of teardown.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';

const file = new Uint8Array(fs.readFileSync(new URL('./testdata/mtx-get.mp4', import.meta.url)));
globalThis.location = { href: 'http://portal/' };
globalThis.window = globalThis;
globalThis.document = { createElement: () => ({ transferControlToOffscreen: () => ({}), addEventListener() {} }) };
WebAssembly.compileStreaming = () => Promise.resolve({});
const workers = [];
globalThis.Worker = class {
  constructor() { this.got = []; this.terminated = false; workers.push(this); }
  postMessage(m) { this.got.push(m); }
  terminate() { this.terminated = true; }
};
const held = new Map(); // url → answer(), for responses a test releases itself
globalThis.fetch = (url) => {
  const u = String(url);
  if (u.includes('de265.wasm')) return Promise.resolve(new Response(new Uint8Array(8)));
  if (u.includes('hold')) return new Promise((r) => held.set(u, () => r(new Response(file))));
  return Promise.resolve(new Response(file));
};
const { CanvasMedia, playH265Live } = await import('./h265.js');
const settle = async () => { for (let i = 0; i < 10; i++) await new Promise((r) => setTimeout(r, 5)); };
const types = (w) => w.got.map((m) => m.type);

test('messages the worker sent for the previous chunk do not reach the next one', async () => {
  const cm = new CanvasMedia();
  const times = [];
  cm.addEventListener('timeupdate', () => times.push(cm.currentTime));
  cm.src = 'http://portal/chunkA';
  await cm.play();
  await settle();
  const w = workers[workers.length - 1];
  const genA = w.got.filter((m) => m.type === 'reset').pop().gen;
  w.onmessage({ data: { type: 'time', t: 300, gen: genA } });
  w.onmessage({ data: { type: 'buffered', seconds: 10.4, gen: genA } }); // reading ahead, as in steady state
  assert.equal(cm.currentTime, 300);
  w.got = [];
  cm.src = 'http://portal/chunkB-hold'; // a click on the timeline
  cm.play();
  // what the worker posted for chunk A before it saw the reset
  w.onmessage({ data: { type: 'time', t: 300.07, gen: genA } });
  w.onmessage({ data: { type: 'buffered', seconds: 10.33, gen: genA } });
  assert.equal(cm.currentTime, 0, 'a late time from chunk A moved chunk B');
  assert.deepEqual(times, [300]);
  await settle();
  held.get('http://portal/chunkB-hold')();
  await settle();
  assert.deepEqual(types(w).filter((t) => t !== 'play'), ['reset', 'init', 'fragment', 'fragment', 'end'], 'chunk B was read into the worker');
  cm.destroy();
});

test('closing a player terminates its worker', async () => {
  const cm = new CanvasMedia();
  await settle();
  const w = workers[workers.length - 1];
  cm.destroy();
  await settle();
  assert.equal(w.terminated, true, 'CanvasMedia.destroy');
  const live = playH265Live(document.createElement('canvas'), 'http://portal/live/index.m3u8-hold');
  await settle();
  const lw = workers[workers.length - 1];
  live.close();
  await settle();
  assert.equal(lw.terminated, true, 'playH265Live close');
});
