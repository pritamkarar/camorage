# hevc-wasm v0.2.0 (vendored, unmodified)

camorage's web UI decodes H.265 in browsers that cannot (for example Chrome on Linux without a
VA-API GPU decoder) with these files, copied byte-for-byte from
https://github.com/OpenIPC/hevc-wasm at tag `v0.2.0` (commit `d43ba2504a4a9e18e6658bcd15ad62a84188153f`,
2026-09-10), folder `dist/`:

| File | Licence | sha256 |
|---|---|---|
| `de265.wasm` | LGPL-3.0 (libde265) | `2ae7dff455a2cfc45614c7201a673c298da7d6c229ae7dfef69218ee1c183dbd` |
| `de265.js` | MIT (emscripten loader) | `1fcbf42ce5e0b1664ae2366d8e43d95eefb9f5aefb668f8c08ea5c93006e201f` |
| `paint.js` | MIT | `37fb6de20b424927eb18be95cee27acd882713ea016b46bc6913637da893da48` |
| `demux.js` | MIT | `dc4a02ec82d2df3bbe16b3cb9277fb5d1824be485409d32e21d029832c200c04` |

`COPYING.libde265` is libde265's licence (LGPL-3.0, with the GPL-3.0 text it refers to);
`LICENSE-hevc-wasm` is the MIT notice for the rest. camorage's own code (`h265*.js`, `fmp4.js`,
`pacer.js`) is MIT like the rest of camorage.

## Rebuilding de265.wasm (your LGPL right to relink)

The corresponding source of `de265.wasm` is:

- **libde265**: https://github.com/OpenIPC/libde265 (a fork of strukturag/libde265) at commit
  `4f010a9a18e875ddc2aff41227c77a55a2fc8935` (2021-05-12, the fork's `master`; it has not changed
  since, so it is the revision hevc-wasm v0.2.0 was built from);
- **the wrapper and build script**: hevc-wasm `src/de265_wrapper.c` and `tools/build.sh` at commit
  `d43ba2504a4a9e18e6658bcd15ad62a84188153f` (tag `v0.2.0`).

It is built with emscripten (hevc-wasm does not record which version), single-threaded with WASM
SIMD, as hevc-wasm's README describes:

```sh
git clone https://github.com/OpenIPC/libde265 ../libde265
git -C ../libde265 checkout 4f010a9a18e875ddc2aff41227c77a55a2fc8935
emcmake cmake -H../libde265 -B../libde265/build-wasm \
  -DCMAKE_BUILD_TYPE=Release -DBUILD_SHARED_LIBS=OFF \
  -DDISABLE_TOOLS=ON -DENABLE_SDL=OFF -DENABLE_THREADS=OFF \
  -DCMAKE_C_FLAGS="-O3 -msimd128" -DCMAKE_CXX_FLAGS="-O3 -msimd128"
cmake --build ../libde265/build-wasm -j
sh tools/build.sh   # in a checkout of hevc-wasm v0.2.0; writes dist/de265.js and dist/de265.wasm
```

Replace `de265.wasm` (and `de265.js`, which must come from the same build) in this folder and
rebuild camorage (`go build ./cmd/camorage`): the UI is embedded in the binary.
