// fMP4 parsing for the in-browser H.265 decoder (h265worker.js). An init segment (ftyp + moov)
// gives the codec, timescale and, for H.265, the parameter sets; a fragment (moof + mdat) gives its
// frames with their media times. MediaMTX's HLS segments and playback streams and ffmpeg's
// fragmented MP4 all have this shape. boxSplitter cuts a byte stream into those pieces.
import { be32, fourcc, findBox, descend, parseHvcC, toAnnexB } from './vendor/hevc/demux.js';

// u64 reads a big-endian 64-bit unsigned integer (exact below 2^53, which media times are).
const u64 = (u8, i) => be32(u8, i) * 2 ** 32 + be32(u8, i + 4);

const NON_SYNC = 0x10000; // sample_is_non_sync_sample, in a sample's flags

// parseInit reads the first track of an init segment: { codec ('hvc1', 'hev1', 'avc1', …),
// timescale, defaults (trex: dur, size, flags), lengthSize, paramSets } — lengthSize and paramSets
// (Annex-B, ready for the decoder) only for H.265. null when u8 is not an init segment.
export function parseInit(u8) {
  const mdia = descend(u8, 0, u8.length, ['moov', 'trak', 'mdia']);
  if (!mdia) return null;
  const mdhd = findBox(u8, mdia[0], mdia[1], 'mdhd');
  const stsd = descend(u8, mdia[0], mdia[1], ['minf', 'stbl', 'stsd']);
  if (!mdhd || !stsd) return null;
  const timescale = be32(u8, mdhd[0] + (u8[mdhd[0] + 8] === 1 ? 28 : 20));
  const codec = fourcc(u8, stsd[0] + 12); // after version/flags and entry_count: the entry's size, then type
  const defaults = { dur: 0, size: 0, flags: 0 };
  const mvex = descend(u8, 0, u8.length, ['moov', 'mvex']);
  const trex = mvex && findBox(u8, mvex[0], mvex[1], 'trex');
  if (trex) Object.assign(defaults, { dur: be32(u8, trex[0] + 20), size: be32(u8, trex[0] + 24), flags: be32(u8, trex[0] + 28) });
  const init = { codec, timescale, defaults, lengthSize: 0, paramSets: null };
  if (codec === 'hvc1' || codec === 'hev1') {
    const hv = parseHvcC(u8);
    if (!hv) return null;
    init.lengthSize = hv.lengthSize;
    init.paramSets = toAnnexB(hv.sets);
  }
  return init;
}

// parseFragment returns the frames of every moof + mdat pair in u8 (an HLS segment, or a piece of
// a playback stream): [{ t, dur, key, data }], t and dur in seconds, data the frame in Annex-B form
// (H.265; other codecs keep their bytes). init is parseInit's result. Only each moof's first track
// fragment is read: camorage's streams are video only.
export function parseFragment(u8, init) {
  const frames = [];
  for (let at = 0; at + 8 <= u8.length;) {
    const size = be32(u8, at);
    if (size < 8 || at + size > u8.length) break;
    if (fourcc(u8, at + 4) === 'moof') readMoof(u8, at, at + size, init, frames);
    at += size;
  }
  return frames;
}

function readMoof(u8, moof, end, init, frames) {
  const traf = findBox(u8, moof + 8, end, 'traf');
  const tfhd = traf && findBox(u8, traf[0] + 8, traf[1], 'tfhd');
  if (!tfhd) return;
  const hf = be32(u8, tfhd[0] + 8) & 0xffffff;
  // An absolute base_data_offset cannot be placed within a piece of a stream; MediaMTX and ffmpeg
  // (default_base_moof) never use it.
  if (hf & 0x1) return;
  let p = tfhd[0] + 16; // after version/flags and track_ID
  if (hf & 0x2) p += 4; // sample_description_index
  const dflt = { ...init.defaults };
  if (hf & 0x8) { dflt.dur = be32(u8, p); p += 4; }
  if (hf & 0x10) { dflt.size = be32(u8, p); p += 4; }
  if (hf & 0x20) { dflt.flags = be32(u8, p); p += 4; }
  const tfdt = findBox(u8, traf[0] + 8, traf[1], 'tfdt');
  let dts = !tfdt ? 0 : u8[tfdt[0] + 8] === 1 ? u64(u8, tfdt[0] + 12) : be32(u8, tfdt[0] + 12);
  let data = moof; // default-base-is-moof (and the rule for a first track fragment without it)
  for (let at = traf[0] + 8; at + 8 <= traf[1];) {
    const size = be32(u8, at);
    if (size < 8) return;
    if (fourcc(u8, at + 4) === 'trun') {
      const f = be32(u8, at + 8) & 0xffffff;
      const n = be32(u8, at + 12);
      let q = at + 16;
      if (f & 0x1) { data = moof + (be32(u8, q) | 0); q += 4; } // data_offset is signed
      let first = null;
      if (f & 0x4) { first = be32(u8, q); q += 4; }
      for (let i = 0; i < n; i++) {
        let dur = dflt.dur;
        let len = dflt.size;
        let flags = dflt.flags;
        if (f & 0x100) { dur = be32(u8, q); q += 4; }
        if (f & 0x200) { len = be32(u8, q); q += 4; }
        if (f & 0x400) { flags = be32(u8, q); q += 4; }
        if (f & 0x800) q += 4; // composition offset: cameras send no B-frames, display order = decode order
        if (i === 0 && first !== null) flags = first;
        if (data + len > u8.length) return; // cut short: keep the whole frames
        const sample = u8.subarray(data, data + len);
        frames.push({
          t: dts / init.timescale,
          dur: dur / init.timescale,
          key: !(flags & NON_SYNC),
          data: init.lengthSize ? annexB(sample, init.lengthSize) : sample,
        });
        data += len;
        dts += dur;
      }
    }
    at += size;
  }
}

// annexB turns a sample's length-prefixed NAL units into start-code-prefixed ones (a copy).
function annexB(sample, lengthSize) {
  const nals = [];
  for (let at = 0; at + lengthSize <= sample.length;) {
    let len = 0;
    for (let i = 0; i < lengthSize; i++) len = len * 256 + sample[at + i];
    at += lengthSize;
    if (len <= 0 || at + len > sample.length) break;
    nals.push(sample.subarray(at, at + len));
    at += len;
  }
  return toAnnexB(nals);
}

// boxSplitter cuts an fMP4 byte stream into pieces as bytes arrive: push(chunk) returns every piece
// it completed, [{ kind, data }] — 'init' is everything up to and including the moov, each
// 'fragment' everything after the previous piece up to and including an mdat (so a styp or prft
// before a moof travels with it). Trailing boxes after the last mdat (mfra) are no piece.
export function boxSplitter() {
  let buf = new Uint8Array(0);
  let start = 0; // where the piece being collected began in buf
  let at = 0; // the next box to look at
  let haveInit = false;
  return {
    push(chunk) {
      const next = new Uint8Array(buf.length - start + chunk.length);
      next.set(buf.subarray(start));
      next.set(chunk, buf.length - start);
      at -= start;
      start = 0;
      buf = next;
      const out = [];
      while (at + 8 <= buf.length) {
        let size = be32(buf, at);
        if (size === 1) {
          if (at + 16 > buf.length) break;
          size = u64(buf, at + 8);
        }
        if (size < 8) throw new Error('fmp4: bad box size');
        if (at + size > buf.length) break;
        const type = fourcc(buf, at + 4);
        at += size;
        if (!haveInit && type === 'moov') {
          out.push({ kind: 'init', data: buf.slice(start, at) });
          haveInit = true;
          start = at;
        } else if (haveInit && type === 'mdat') {
          out.push({ kind: 'fragment', data: buf.slice(start, at) });
          start = at;
        }
      }
      return out;
    },
  };
}
