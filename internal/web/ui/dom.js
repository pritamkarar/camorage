// DOM and network helpers shared by the views.

// h builds an element: h('button', {class: 'primary', onclick: fn}, 'Save').
// attrs.style may be an object (applied through CSSOM, which the CSP allows).
// Children are nodes or text; text is never parsed as HTML (camera names come from users).
export function h(tag, attrs = {}, ...kids) {
  const el = document.createElement(tag);
  for (const [k, v] of Object.entries(attrs)) {
    if (k === 'style' && v && typeof v === 'object') Object.assign(el.style, v);
    else if (k.startsWith('on') && typeof v === 'function') el.addEventListener(k.slice(2), v);
    else if (v === true) el.setAttribute(k, '');
    else if (v !== false && v != null) el.setAttribute(k, String(v));
  }
  for (const k of kids.flat()) {
    if (k != null && k !== false) el.append(k instanceof Node ? k : String(k));
  }
  return el;
}

// api calls the portal's JSON API. A 401 means the session ended: the app shows the login screen.
export async function api(path, { method = 'GET', body } = {}) {
  const res = await fetch(path, {
    method,
    credentials: 'same-origin',
    headers: body === undefined ? {} : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  if (res.status === 401) {
    window.dispatchEvent(new Event('camorage:signed-out'));
    throw new Error('signed out');
  }
  const data = (res.headers.get('Content-Type') || '').includes('json') ? await res.json() : null;
  if (!res.ok) throw new Error((data && data.error) || `HTTP ${res.status}`);
  return data;
}

// playHLS plays an HLS URL with hls.js (or natively on Safari). After a fatal error it retries
// every 5 s, so a tile recovers by itself when its camera or MediaMTX comes back.
export function playHLS(video, url) {
  let hls = null;
  let timer = null;
  let closed = false;
  const retry = () => { timer = setTimeout(start, 5000); };
  function start() {
    if (closed) return;
    if (window.Hls && window.Hls.isSupported()) {
      hls = new window.Hls({ liveSyncDurationCount: 2 });
      hls.on(window.Hls.Events.ERROR, (_event, data) => {
        if (!data.fatal) return;
        hls.destroy();
        hls = null;
        retry();
      });
      hls.loadSource(url);
      hls.attachMedia(video);
    } else if (video.canPlayType('application/vnd.apple.mpegurl')) {
      video.onerror = retry;
      video.src = url;
    }
    video.play().catch(() => {});
  }
  start();
  return {
    close() {
      closed = true;
      clearTimeout(timer);
      if (hls) hls.destroy();
      video.onerror = null;
      video.removeAttribute('src');
      video.load();
    },
  };
}

// showError puts an error line at the top of el for a few seconds.
export function showError(el, err) {
  const line = h('p', { class: 'error', role: 'alert' }, (err && err.message) || String(err));
  el.prepend(line);
  setTimeout(() => line.remove(), 8000);
}

// dl renders [label, value] pairs as a definition list.
export function dl(pairs) {
  return h('dl', {}, pairs.flatMap(([k, v]) => [h('dt', {}, k), h('dd', {}, v)]));
}
