// DOM and network helpers shared by the views.

const SVG_NS = 'http://www.w3.org/2000/svg';

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
// every 5 s, so a tile recovers by itself when its camera or MediaMTX comes back. A codec this
// browser cannot decode (e.g. H.265 in most Linux browsers) never recovers: it calls
// onUnplayable(reason) once instead.
export function playHLS(video, url, onUnplayable = () => {}) {
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
        const d = window.Hls.ErrorDetails;
        if ([d.MANIFEST_INCOMPATIBLE_CODECS_ERROR, d.BUFFER_INCOMPATIBLE_CODECS_ERROR, d.BUFFER_ADD_CODEC_ERROR].includes(data.details)) onUnplayable(data.reason);
        else retry();
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

// icon draws a symbol from the sprite in index.html: icon('i-live'), icon('logo', 'logo').
export function icon(name, cls = 'icon') {
  const svg = document.createElementNS(SVG_NS, 'svg');
  svg.setAttribute('class', cls);
  svg.setAttribute('aria-hidden', 'true');
  const use = document.createElementNS(SVG_NS, 'use');
  use.setAttribute('href', `#${name}`);
  svg.append(use);
  return svg;
}

// brand is the logo with the "cam◉rage" wordmark; screen readers read "camorage".
export function brand(big = false) {
  return h('span', { class: big ? 'brand big' : 'brand' },
    icon('logo', 'logo'),
    h('span', { class: 'sr' }, 'camorage'),
    h('span', { class: 'wordmark', 'aria-hidden': 'true' }, 'cam', icon('lens', 'lens'), 'rage'));
}

// pageHead is a page's title row: the title, a muted subtitle (the element with class "sub",
// which a view may update later) and action buttons.
export function pageHead(title, sub, ...actions) {
  return h('div', { class: 'page-head' },
    h('div', {}, h('h1', {}, title), h('p', { class: 'sub' }, sub)),
    h('div', { class: 'head-actions' }, ...actions));
}

// sectionHead is the small label over a group, with optional buttons on the right.
export function sectionHead(title, ...actions) {
  return h('div', { class: 'section-head' }, h('h2', { class: 'section' }, title), h('div', { class: 'head-actions' }, ...actions));
}

// chip is a short status label; kind is ok, warn, bad, rec, motion or muted.
export function chip(text, kind = 'muted') {
  return h('span', { class: `chip ${kind}` }, text);
}

// setChip changes a chip in place.
export function setChip(el, { text, kind }) {
  el.textContent = text;
  el.className = `chip ${kind}`;
}

// stat is a number tile: stat('9.8 GB', 'used by recordings').
export function stat(value, label) {
  return h('div', { class: 'stat' }, h('b', {}, value), h('span', {}, label));
}

// field is a form control with its label above and an optional hint below.
export function field(label, control, hint) {
  return h('label', { class: 'field' }, h('span', {}, label), control, hint ? h('small', { class: 'hint' }, hint) : null);
}

let segments = 0;

// segmented is a row of radio buttons drawn as one switch. options are [value, text] pairs;
// read() gives the chosen value.
export function segmented(label, options, value, onchange) {
  const name = `seg-${++segments}`;
  const el = h('div', { class: 'seg', role: 'radiogroup', 'aria-label': label }, options.map(([v, text]) => {
    const radio = h('input', { type: 'radio', name, value: v, onchange });
    radio.checked = v === value;
    return h('label', {}, radio, h('span', {}, text));
  }));
  return { el, read: () => el.querySelector('input:checked').value };
}

// skel is a shimmering placeholder for content that is still loading; kind sets its shape:
// stat, card, table, line, video or timeline.
export function skel(kind) {
  return h('div', { class: `skel ${kind}`, 'aria-hidden': 'true' });
}

// emptyState is what a page shows when it has nothing to list.
export function emptyState(iconName, text, action) {
  return h('div', { class: 'empty' }, icon(iconName, 'icon big'), h('p', {}, text), action);
}

let drawers = 0;

// drawer opens a side panel holding a form: a modal <dialog> on the right (full screen on
// phones). Esc, the × button and close() close it; it is then removed, its onClose functions run
// and focus goes back to what opened it. reset() reuses it for a next step (scan → connect →
// camera form). The form never submits itself: a view sets d.form.onsubmit.
export function drawer(title) {
  const titleEl = h('h2', { id: `drawer-${++drawers}` }, title);
  const body = h('div', { class: 'drawer-body' });
  const msg = h('p', { class: 'drawer-msg', role: 'status', hidden: true });
  const actions = h('div', { class: 'drawer-actions' });
  const form = h('form', { onsubmit: (e) => e.preventDefault() },
    h('div', { class: 'drawer-head' }, titleEl,
      h('button', { type: 'button', class: 'icon-btn', 'aria-label': 'Close', onclick: () => d.close() }, icon('i-close'))),
    body,
    h('div', { class: 'drawer-foot' }, msg, actions));
  const dlg = h('dialog', { class: 'drawer', 'aria-labelledby': titleEl.id }, form);
  const opener = document.activeElement;
  let cleanups = [];
  const cleanup = () => {
    const fns = cleanups;
    cleanups = [];
    fns.forEach((fn) => fn());
  };
  dlg.addEventListener('close', () => {
    cleanup();
    dlg.remove();
    if (opener && opener.isConnected) opener.focus();
  });
  const d = {
    dlg,
    form,
    body,
    close() {
      if (dlg.open) dlg.close();
    },
    reset(text) {
      cleanup();
      titleEl.textContent = text;
      body.replaceChildren();
      actions.replaceChildren();
      form.onsubmit = null;
      d.say('');
    },
    say(text, kind = 'muted') {
      msg.textContent = text;
      msg.className = `drawer-msg ${kind}`;
      msg.hidden = !text;
    },
    // footer puts in the usual buttons: [left…] … Cancel, <submit>; with no submit just Close.
    footer(submit, ...left) {
      actions.replaceChildren(...left, h('span', { class: 'spacer' }),
        h('button', { type: 'button', onclick: () => d.close() }, submit ? 'Cancel' : 'Close'),
        ...(submit ? [h('button', { type: 'submit', class: 'primary' }, submit)] : []));
    },
    onClose(fn) {
      cleanups.push(fn);
    },
  };
  document.body.append(dlg);
  dlg.showModal();
  return d;
}

// showError shows an error as a toast for 8 s. With a drawer open the toast goes inside it: a
// modal dialog covers everything else.
export function showError(err) {
  const host = document.querySelector('dialog[open]') || document.body;
  let region = [...host.children].find((c) => c.classList.contains('toasts'));
  if (!region) {
    region = h('div', { class: 'toasts' });
    host.append(region);
  }
  const t = h('div', { class: 'toast', role: 'alert' }, (err && err.message) || String(err));
  region.append(t);
  setTimeout(() => t.remove(), 8000);
}
