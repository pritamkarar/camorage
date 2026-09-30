import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import vm from 'node:vm';

// worker runs sw.js with a fake service-worker global: the network answers net(url) (throw for
// "no network"), the cache starts with cached. It returns a function that dispatches a fetch
// event and gives what the worker answered (undefined: it let the browser handle the request).
function worker(net, cached = {}) {
  const handlers = {};
  const store = new Map(Object.entries(cached));
  const ctx = {
    URL,
    self: { location: { origin: 'https://cams.example.com' }, addEventListener: (type, fn) => { handlers[type] = fn; }, skipWaiting() {}, clients: { claim() {} } },
    caches: {
      open: async () => ({ put: async (k, v) => { store.set(k, v); }, addAll: async () => {} }),
      match: async (k) => store.get(k),
      keys: async () => [...store.keys()],
      delete: async (k) => store.delete(k),
    },
    fetch: async (req) => net(req.url),
  };
  vm.runInNewContext(readFileSync(new URL('./sw.js', import.meta.url), 'utf8'), ctx);
  const dispatch = async (url, { mode = 'no-cors', method = 'GET' } = {}) => {
    let answer;
    handlers.fetch({ request: { url, mode, method }, respondWith: (p) => { answer = p; } });
    return answer && answer;
  };
  dispatch.store = store;
  return dispatch;
}

const O = 'https://cams.example.com';
const res = (status, body) => ({ status, ok: status >= 200 && status < 300, body, clone() { return { ...this }; } });
const offline = { '/offline.html': res(200, 'offline page'), '/app.css': res(200, 'cached css'), '/logo.svg': res(200, 'cached logo') };
const down = () => { throw new TypeError('Failed to fetch'); };

test('a page load shows the offline page when there is no network or a proxy answers 5xx', async () => {
  assert.equal((await worker(down, offline)(`${O}/`, { mode: 'navigate' })).body, 'offline page');
  assert.equal((await worker(() => res(530, 'cloudflare error'), offline)(`${O}/`, { mode: 'navigate' })).body, 'offline page');
  assert.equal((await worker(() => res(200, 'portal'), offline)(`${O}/`, { mode: 'navigate' })).body, 'portal');
});

// Cloudflare (tunnel down) and Tailscale Serve (camorage down) answer the offline page's own
// files with 502/530 too: they must come from the cache, or the offline page is unstyled.
test("the offline page's files come from the cache when the network fails or answers 5xx", async () => {
  assert.equal((await worker(down, offline)(`${O}/app.css`)).body, 'cached css');
  assert.equal((await worker(() => res(530, 'cloudflare error'), offline)(`${O}/app.css`)).body, 'cached css');
  assert.equal((await worker(() => res(502, 'bad gateway'), offline)(`${O}/logo.svg`)).body, 'cached logo');
});

test('a fresh copy of an offline file refreshes the cache', async () => {
  const w = worker(() => res(200, 'new css'), offline);
  assert.equal((await w(`${O}/app.css`)).body, 'new css');
  await new Promise((r) => setTimeout(r, 0));
  assert.equal(w.store.get('/app.css').body, 'new css');
});

test('the API, live video, recordings and other files are never answered by the worker', async () => {
  const w = worker(() => res(200, 'x'), offline);
  for (const path of ['/api/status', '/live/hls/front-door/index.m3u8', '/api/playback/video?cam=a', '/app.js', '/sw.js']) {
    assert.equal(await w(`${O}${path}`), undefined, path);
  }
  assert.equal(await w(`${O}/live/whep/front-door`, { method: 'POST' }), undefined, 'WHEP offer');
  assert.equal(await w('https://github.com/pritamkarar/camorage', { mode: 'navigate' }), undefined, 'another site');
});
