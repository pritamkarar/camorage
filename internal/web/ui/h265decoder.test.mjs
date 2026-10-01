import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import createDe265 from './vendor/hevc/de265.js';
import { openDecoder } from './h265decoder.js';
import { parseInit, parseFragment, boxSplitter } from './fmp4.js';

const fixture = (name) => new Uint8Array(fs.readFileSync(new URL(`./testdata/${name}`, import.meta.url)));
const wasmBinary = fs.readFileSync(new URL('./vendor/hevc/de265.wasm', import.meta.url));

// framesOf returns a file's init and its frames, fragment by fragment.
function framesOf(name) {
  const [init, ...frags] = boxSplitter().push(fixture(name));
  const i = parseInit(init.data);
  return { init: i, fragments: frags.map((f) => parseFragment(f.data, i)) };
}

test('one 320x240 picture per frame, from ffmpeg and from a MediaMTX playback stream', async () => {
  for (const name of ['hevc-ffmpeg.mp4', 'mtx-get.mp4']) {
    const M = await createDe265({ wasmBinary });
    const dec = openDecoder(M);
    const { init, fragments } = framesOf(name);
    dec.configure(init.paramSets);
    let frames = 0;
    let pictures = 0;
    for (const f of fragments.flat()) {
      frames++;
      const pic = dec.decode(f.data);
      if (!pic) continue;
      pictures++;
      assert.equal(pic.w, 320);
      assert.equal(pic.h, 240);
      assert.equal(pic.planes.length, 3);
      assert.ok(pic.planes[0].stride >= 320 && pic.planes[1].w === 160 && pic.planes[1].h === 120);
      assert.ok(pic.planes[0].data.some((b) => b !== 0), 'a test pattern is not all black');
      dec.release();
    }
    assert.equal(pictures, frames, name);
    dec.destroy();
  }
});

test('after reset and configure, decoding restarts cleanly at a key frame', async () => {
  const M = await createDe265({ wasmBinary });
  const dec = openDecoder(M);
  const { init, fragments } = framesOf('hevc-ffmpeg.mp4');
  dec.configure(init.paramSets);
  for (const f of fragments[0].slice(0, 5)) { if (dec.decode(f.data)) dec.release(); } // part of a GOP
  dec.reset();
  dec.configure(init.paramSets);
  let pictures = 0;
  for (const f of fragments[1]) { if (dec.decode(f.data)) { pictures++; dec.release(); } } // starts with a key frame
  assert.equal(pictures, fragments[1].length);
  dec.destroy();
});
