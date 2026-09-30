// camorage's service worker. Its only job: when the phone can't be reached, show offline.html
// instead of the browser's error page. It caches that page and the two files it uses, nothing
// else; the API, live video and recordings are never intercepted (streaming and Range requests
// stay the browser's business), and nothing private is ever stored.
const CACHE = 'camorage-offline';
const FILES = ['/offline.html', '/app.css', '/logo.svg'];

self.addEventListener('install', (e) => {
  e.waitUntil(caches.open(CACHE).then((c) => c.addAll(FILES)).then(() => self.skipWaiting()));
});

self.addEventListener('activate', (e) => {
  e.waitUntil(caches.keys()
    .then((keys) => Promise.all(keys.filter((k) => k !== CACHE).map((k) => caches.delete(k))))
    .then(() => self.clients.claim()));
});

const offline = () => caches.match('/offline.html');

self.addEventListener('fetch', (e) => {
  const req = e.request;
  if (req.method !== 'GET') return;
  const url = new URL(req.url);
  if (url.origin !== self.location.origin) return;
  if (req.mode === 'navigate') {
    // a 5xx is a proxy saying the phone is gone (Cloudflare answers 502/530 when the tunnel is down)
    e.respondWith(fetch(req).then((res) => (res.status >= 500 ? offline() : res)).catch(offline));
  } else if (FILES.includes(url.pathname)) {
    // network first, so the cached copies stay current; the cache when the network fails or a
    // proxy answers for the phone (the offline page would be unstyled otherwise). offline.html
    // itself is only fetched again when this file changes and the worker reinstalls.
    e.respondWith(fetch(req)
      .then((res) => {
        if (!res.ok) return caches.match(url.pathname).then((c) => c || res);
        const copy = res.clone();
        caches.open(CACHE).then((c) => c.put(url.pathname, copy));
        return res;
      })
      .catch(() => caches.match(url.pathname)));
  }
});
