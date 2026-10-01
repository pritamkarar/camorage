// The in-browser H.265 decoder, page side. For a browser that cannot decode H.265 itself, a
// camera's video is decoded in a worker (h265worker.js: de265.wasm, libde265 in WebAssembly) and
// painted on a <canvas>. Live follows MediaMTX's HLS playlist (playH265Live); Playback streams a
// recording chunk (CanvasMedia, which looks like the parts of <video> the Playback page uses).
// Works over plain HTTP: single-threaded WebAssembly, no WebCodecs, no SharedArrayBuffer.
import { api } from './dom.js';
import { firstVariant, mediaPlaylist, usesH265 } from './lib.js';
import { boxSplitter } from './fmp4.js';

const PROBE = 'video/mp4; codecs="hvc1.1.6.L150.90"';
const RETRY_MS = 5000;
const BUFFER_S = 10; // Playback reads ahead this far, then waits for the worker to catch up

let supported;
let available;
let decoder = null;

// h265Supported tells whether this browser decodes H.265 itself (then nothing here is used).
export function h265Supported() {
  if (supported === undefined) {
    supported = window.MediaSource
      ? MediaSource.isTypeSupported(PROBE)
      : document.createElement('video').canPlayType(PROBE) !== ''; // iPhone Safari plays HLS itself
  }
  return supported;
}

// fallbackAvailable tells whether the in-browser decoder can run here: WebAssembly, workers, and
// a canvas a worker can draw on with WebGL.
export function fallbackAvailable() {
  if (available === undefined) {
    try {
      available = typeof WebAssembly === 'object' && typeof Worker === 'function'
        && typeof OffscreenCanvas === 'function' && !!new OffscreenCanvas(1, 1).getContext('webgl');
    } catch {
      available = false;
    }
  }
  return available;
}

// needsFallback: this stream (its codec list from /api/status) is H.265, this browser can't decode
// that, and the in-browser decoder can run.
export const needsFallback = (tracks) => usesH265(tracks) && !h265Supported() && fallbackAvailable();

// loadDecoder compiles de265.wasm once per page; every player's worker gets the compiled module
// (the UI's files are fetched afresh on each load, so this saves 436 KB per extra player).
function loadDecoder() {
  if (!decoder) {
    const url = '/vendor/hevc/de265.wasm';
    decoder = WebAssembly.compileStreaming(fetch(url, { credentials: 'same-origin' }))
      .catch(() => fetch(url, { credentials: 'same-origin' }).then((r) => r.arrayBuffer()).then((b) => WebAssembly.compile(b)));
    decoder.catch(() => { decoder = null; }); // the next player tries again
  }
  return decoder;
}

// startWorker hands canvas to a new decoder worker; onMessage gets the worker's messages.
async function startWorker(canvas, mode, onMessage) {
  const module = await loadDecoder();
  const worker = new Worker(new URL('./h265worker.js', import.meta.url), { type: 'module' });
  worker.onmessage = (e) => onMessage(e.data);
  worker.onerror = (e) => onMessage({ type: 'error', reason: `worker: ${e.message || 'failed'}` });
  const off = canvas.transferControlToOffscreen();
  worker.postMessage({ type: 'start', canvas: off, module, mode }, [off]);
  return worker;
}

// fatal tells whether a worker's error means the decoder cannot run here at all (no point retrying).
const fatal = (reason) => /^(wasm|worker|no-webgl|not-h265)/.test(reason);

// fetchOK fetches url, throwing on an HTTP error; a 401 lets the app show the sign-in screen.
async function fetchOK(url, signal, as) {
  const res = await fetch(url, { credentials: 'same-origin', signal });
  if (res.status === 401) api('/api/status').catch(() => {});
  if (!res.ok) throw new Error(`HTTP ${res.status}`);
  return as === 'text' ? res.text() : res.arrayBuffer();
}

const sleep = (ms, signal) => new Promise((resolve) => {
  const t = setTimeout(resolve, ms);
  signal.addEventListener('abort', () => { clearTimeout(t); resolve(); }, { once: true });
});

// playH265Live shows a camera's live HLS stream (MediaMTX, fMP4) on canvas, staying current. Like
// playHLS it retries every 5 s after a failure. hooks: onPlaying() at the first frame (and after a
// stall or retry), onWaiting() when it stalls or retries, onUnplayable(reason) when the decoder
// cannot run here at all. Returns { close }.
export function playH265Live(canvas, url, { onPlaying = () => {}, onWaiting = () => {}, onUnplayable = () => {} } = {}) {
  let worker = null;
  let closed = false;
  let ctl = null;
  let timer = null;
  let gen = 0; // messages stamped with an older generation were meant for a stream since replaced
  const reset = () => worker.postMessage({ type: 'reset', gen: ++gen });

  const stop = () => {
    if (ctl) ctl.abort();
    ctl = null;
    clearTimeout(timer);
  };
  const retry = () => {
    if (closed) return;
    stop();
    onWaiting();
    reset();
    timer = setTimeout(follow, RETRY_MS);
  };
  const giveUp = (reason) => {
    close();
    onUnplayable(`H.265 decoder: ${reason}`);
  };

  // follow reads the playlist and hands the worker the init and each new segment, starting at the
  // newest one.
  async function follow() {
    stop();
    const mine = new AbortController();
    ctl = mine;
    try {
      const base = new URL(url, location.href).href;
      const variant = firstVariant(await fetchOK(base, mine.signal, 'text'), base);
      if (!variant) throw new Error('no variant');
      let initURL = null;
      let last = -1;
      while (!mine.signal.aborted) {
        const pl = mediaPlaylist(await fetchOK(variant, mine.signal, 'text'), variant);
        if (pl.init && pl.init !== initURL) {
          if (initURL) reset();
          initURL = pl.init;
          const data = await fetchOK(initURL, mine.signal);
          // more than two segments queued means a whole segment behind (one would trip on jitter)
          worker.postMessage({ type: 'init', data, maxQueuedS: 2 * (pl.target || 3) }, [data]);
        }
        const fresh = pl.segments.filter((s) => s.seq > last);
        for (const s of last < 0 ? fresh.slice(-1) : fresh) {
          const data = await fetchOK(s.url, mine.signal);
          worker.postMessage({ type: 'fragment', data }, [data]);
          last = s.seq;
        }
        await sleep(Math.max(500, (pl.target || 2) * 500), mine.signal);
      }
    } catch {
      if (!mine.signal.aborted) retry();
    }
  }

  function close() {
    if (closed) return;
    closed = true;
    stop();
    if (worker) worker.terminate();
  }

  startWorker(canvas, 'live', (m) => {
    if (m.gen !== undefined && m.gen !== gen) return;
    if (m.type === 'playing') onPlaying();
    else if (m.type === 'waiting') onWaiting();
    else if (m.type === 'error') {
      if (fatal(m.reason)) giveUp(m.reason);
      else retry();
    }
  }).then((w) => {
    worker = w;
    if (closed) w.terminate();
    else follow();
  }, (err) => giveUp(`wasm: ${err.message}`));

  return { close };
}

// CanvasMedia plays recording chunks (fMP4, /api/playback/video?format=fmp4) through the in-browser
// decoder on a <canvas>, every frame in turn, reading ahead at most BUFFER_S. It looks like the
// parts of <video> the Playback page uses: src, currentSrc, play(), pause(), paused, currentTime
// (seconds since the chunk's first frame), muted, error, load(), removeAttribute('src'), and the
// events play, pause, playing, waiting, timeupdate, ended, error.
export class CanvasMedia extends EventTarget {
  constructor() {
    super();
    this.el = document.createElement('canvas');
    this.el.className = 'player';
    this.paused = true;
    this.currentTime = 0;
    this.muted = true;
    this.error = null;
    this._src = '';
    this._run = 0;
    this._ctl = null;
    this._buffered = 0;
    this._wake = null; // resolves the reader's wait when the worker has caught up
    this._gen = 0; // the worker's messages for an older generation were meant for a replaced chunk
    this._worker = startWorker(this.el, 'complete', (m) => this._message(m));
    this._worker.catch((err) => this._fail(`wasm: ${err.message}`));
  }

  get src() { return this._src; }

  set src(url) {
    this._stop();
    this._src = url ? new URL(url, location.href).href : '';
    this.currentTime = 0;
    this.error = null;
    this.paused = true;
    if (this._src) this._read(++this._run);
  }

  get currentSrc() { return this._src; }

  removeAttribute(name) {
    if (name === 'src') this.src = '';
  }

  load() {}

  async play() {
    if (!this._src) return;
    this.paused = false;
    this._emit('play');
    (await this._worker).postMessage({ type: 'play' });
  }

  pause() {
    if (this.paused) return;
    this.paused = true;
    this._worker.then((w) => w.postMessage({ type: 'pause' }), () => {});
    this._emit('pause');
  }

  destroy() {
    this._stop();
    this._worker.then((w) => w.terminate(), () => {});
  }

  _emit(type) {
    this.dispatchEvent(new Event(type));
  }

  _stop() {
    this._run++;
    if (this._ctl) this._ctl.abort();
    this._ctl = null;
    this._buffered = 0;
    this._wakeReader();
    const gen = ++this._gen;
    this._worker.then((w) => w.postMessage({ type: 'reset', gen }), () => {});
  }

  _wakeReader() {
    const wake = this._wake;
    this._wake = null;
    if (wake) wake();
  }

  _fail(reason) {
    this._stop();
    this.error = { code: 3, message: reason }; // 3: MEDIA_ERR_DECODE
    this.paused = true;
    this._emit('error');
  }

  _message(m) {
    if (m.gen !== undefined && m.gen !== this._gen) return; // posted for a chunk already replaced
    if (m.type === 'playing') this._emit('playing');
    else if (m.type === 'waiting') this._emit('waiting');
    else if (m.type === 'time') {
      this.currentTime = m.t;
      this._emit('timeupdate');
    } else if (m.type === 'buffered') {
      this._buffered = m.seconds;
      if (m.seconds < BUFFER_S) this._wakeReader();
    } else if (m.type === 'ended') {
      this.paused = true;
      this._emit('pause');
      this._emit('ended');
    } else if (m.type === 'error') this._fail(m.reason);
  }

  // _read streams the chunk into the worker as init + fragments, pausing while BUFFER_S are queued.
  async _read(run) {
    const ctl = new AbortController();
    this._ctl = ctl;
    try {
      const worker = await this._worker;
      const res = await fetch(this._src, { credentials: 'same-origin', signal: ctl.signal });
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      const split = boxSplitter();
      const reader = res.body.getReader();
      for (;;) {
        while (this._buffered >= BUFFER_S && run === this._run) await new Promise((r) => { this._wake = r; });
        if (run !== this._run) return;
        const { done, value } = await reader.read();
        if (done) break;
        for (const piece of split.push(value)) {
          if (piece.kind === 'fragment') this._buffered += 1; // about a second each, until the worker says
          worker.postMessage({ type: piece.kind, data: piece.data.buffer }, [piece.data.buffer]);
        }
      }
      if (run === this._run) worker.postMessage({ type: 'end' });
    } catch (err) {
      if (run === this._run && !ctl.signal.aborted) this._fail(err.message);
    }
  }
}
