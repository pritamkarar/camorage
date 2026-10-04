// The in-browser H.265 decoder's worker (see h265.js). It owns the decoder and the canvas, so no
// picture ever crosses a thread. The page sends an init segment and fragments; frames are decoded
// with de265.wasm and painted at their media times (pacer.js).
//   in:  start {canvas: OffscreenCanvas, module: WebAssembly.Module, mode: 'live' | 'complete'},
//        init {data, maxQueuedS?}, fragment {data}, end, play, pause, rate {rate} (the speed, kept
//        across resets), reset (a new stream, a new init comes next), destroy
//   out: playing, waiting, time {t: seconds since the stream's first frame}, buffered {seconds},
//        ended, error {reason} — each stamped with gen, the generation of the last reset, so the
//        page can drop what was posted for a stream it already replaced
// A worker's timers are not throttled like a hidden page's animation frames, so a background tab
// keeps decoding instead of piling frames up.
import createDe265 from './vendor/hevc/de265.js';
import { makePainter } from './vendor/hevc/paint.js';
import { parseInit, parseFragment } from './fmp4.js';
import { openDecoder } from './h265decoder.js';
import { next } from './pacer.js';

let M = null;
let paint = null;
let dec = null;
let init = null;
let mode = 'live';
let maxQueuedS = 6;
let rate = 1;
let queue = [];
let clock = null;
let paused = true;
let ended = false;
let waiting = false;
let painted = false;
let failed = false;
let gen = 0;
let firstT = null;
let timer = null;
let lastDecodeMs = 0;

const post = (type, data) => self.postMessage({ type, ...data, gen });

function fail(reason) {
  if (failed) return;
  failed = true;
  clearTimeout(timer);
  timer = null;
  post('error', { reason });
}

// Messages are handled one at a time, in order: start is asynchronous (it instantiates the
// decoder) and whatever arrives meanwhile waits for it.
let chain = Promise.resolve();
self.onmessage = (e) => {
  chain = chain.then(() => handle(e.data)).catch((err) => fail((err && err.message) || String(err)));
};

async function handle(m) {
  if (m.type === 'start') {
    mode = m.mode;
    paused = mode !== 'live'; // live plays at once; complete waits for play
    M = await createDe265({
      instantiateWasm(imports, receive) {
        WebAssembly.instantiate(m.module, imports).then((instance) => receive(instance, m.module), (err) => fail(`wasm: ${err.message}`));
        return {};
      },
    });
    const gl = m.canvas.getContext('webgl', { alpha: false, antialias: false, depth: false });
    if (!gl) throw new Error('no-webgl');
    paint = makePainter(gl);
  } else if (m.type === 'init') {
    init = parseInit(new Uint8Array(m.data));
    if (!init) throw new Error('bad-init');
    if (!init.paramSets) throw new Error('not-h265');
    if (m.maxQueuedS) maxQueuedS = m.maxQueuedS;
    if (dec) dec.reset();
    else dec = openDecoder(M);
    dec.configure(init.paramSets);
  } else if (m.type === 'fragment') {
    if (!init || failed) return;
    for (const f of parseFragment(new Uint8Array(m.data), init)) queue.push(f);
    buffered();
    schedule(0);
  } else if (m.type === 'end') {
    ended = true;
    schedule(0);
  } else if (m.type === 'play') {
    paused = false;
    clock = null;
    schedule(0);
  } else if (m.type === 'rate') {
    rate = m.rate;
    clock = null; // the next frame anchors a clock at the new speed
  } else if (m.type === 'pause') {
    paused = true;
    clearTimeout(timer);
    timer = null;
  } else if (m.type === 'reset') {
    gen = m.gen;
    clearTimeout(timer);
    timer = null;
    paused = mode !== 'live'; // a new chunk waits for play, as at start
    queue = [];
    clock = null;
    init = null;
    ended = false;
    waiting = false;
    painted = false;
    failed = false;
    firstT = null;
  } else if (m.type === 'destroy') {
    clearTimeout(timer);
    if (dec) dec.destroy();
    self.close();
  }
}

function buffered() {
  const last = queue[queue.length - 1];
  post('buffered', { seconds: last ? last.t + last.dur - queue[0].t : 0 });
}

function schedule(ms) {
  if (timer !== null || paused || failed) return;
  timer = setTimeout(tick, ms);
}

function tick() {
  timer = null;
  if (paused || failed) return;
  try {
    const r = next(queue, clock, performance.now(), mode, maxQueuedS, rate);
    if (r.do === 'idle') {
      if (ended) {
        paused = true;
        post('ended');
      } else if (painted && !waiting) {
        waiting = true;
        post('waiting');
      }
      return; // the next fragment (or end) schedules again
    }
    if (r.do === 'wait') {
      schedule(Math.max(0, r.ms - lastDecodeMs));
      return;
    }
    if (r.do === 'drop') {
      queue.splice(0, r.keep);
      clock = null;
      dec.reset();
      dec.configure(init.paramSets);
      buffered();
      schedule(0);
      return;
    }
    clock = r.clock;
    const f = queue.shift();
    const t0 = performance.now();
    const pic = dec.decode(f.data);
    lastDecodeMs = performance.now() - t0;
    if (pic) {
      paint(pic.planes, pic.w, pic.h);
      dec.release();
      if (firstT === null) firstT = f.t;
      if (!painted || waiting) post('playing');
      painted = true;
      waiting = false;
      post('time', { t: f.t - firstT });
    }
    if (mode === 'complete') buffered();
    schedule(0);
  } catch (err) {
    fail(err.message || String(err));
  }
}
