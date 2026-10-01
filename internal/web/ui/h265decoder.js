// openDecoder wraps the C API of de265.wasm (vendor/hevc: libde265 in WebAssembly): one frame's
// Annex-B bytes in, its picture out. Camera streams have no B-frames, so every frame gives its
// picture straight away. M is the module createDe265() resolves to.
export function openDecoder(M) {
  // -1: libde265's default acceleration (its WASM SIMD path): about a third faster than scalar,
  // and pixel-identical on a real camera's 2304x2592 stream (checked 2026-10-02).
  const dec = M._de_create(-1);
  if (!dec) throw new Error('decoder-create');
  const push = (u8) => {
    const p = M._de_malloc(u8.length);
    M.HEAPU8.set(u8, p);
    M._de_push(dec, p, u8.length);
    M._de_free(p);
  };
  // picture returns the waiting picture's planes as paint.js takes them.
  const picture = () => {
    const w = M._de_width(dec);
    const h = M._de_height(dec);
    const sp = M._de_malloc(4);
    const planes = [0, 1, 2].map((c) => {
      const ptr = M._de_plane(dec, c, sp);
      const stride = M.getValue(sp, 'i32');
      const ph = c ? (h + 1) >> 1 : h;
      return { data: M.HEAPU8.subarray(ptr, ptr + stride * ph), stride, w: c ? (w + 1) >> 1 : w, h: ph };
    });
    M._de_free(sp);
    return { w, h, planes };
  };
  return {
    // configure hands the decoder the stream's parameter sets (VPS, SPS, PPS from the init).
    configure(paramSets) {
      push(paramSets);
    },
    // decode returns the frame's picture, or null when it gave none; release() it after painting.
    decode(data) {
      push(data);
      M._de_end_frame(dec);
      for (let guard = 0; guard < 64; guard++) {
        const flags = M._de_step(dec, 1000);
        if (flags & 8) throw new Error('decoder-error');
        if (flags & 2) return picture();
        if (!(flags & 1)) return null;
      }
      return null;
    },
    release() {
      M._de_release(dec);
    },
    // reset drops every reference picture; configure again and continue from a key frame.
    reset() {
      M._de_reset(dec);
    },
    destroy() {
      M._de_destroy(dec);
    },
  };
}
