import { h, api, brand } from './dom.js';
import { diskUse } from './lib.js';
import './pwa.js';
import { renderSetup, renderLogin } from './auth.js';
import { renderLive } from './live.js';
import { renderPlayback } from './playback.js';
import { renderCameras } from './cameras.js';
import { renderSettings } from './settings.js';
import { renderStorage } from './storage.js';

const views = { live: renderLive, playback: renderPlayback, cameras: renderCameras, storage: renderStorage, settings: renderSettings };
const main = document.getElementById('view');
const shell = document.getElementById('shell');
const diskText = document.getElementById('disk-text');
const diskFill = document.getElementById('disk-fill');
const sideVersion = document.getElementById('side-version');
for (const a of shell.querySelectorAll('a.home')) a.replaceChildren(brand());
let cleanup = null;
let generation = 0; // a view that finishes rendering after the user moved on is discarded
let shellTimer = null;

// teardown ends the current view. It also closes any open drawer: a modal dialog stays above
// everything else, the sign-in screen and the next page included.
function teardown() {
  if (cleanup) cleanup();
  cleanup = null;
  for (const d of document.querySelectorAll('dialog[open]')) d.close();
}

// shellStatus fills the sidebar's storage bar and version; a failed read keeps the last values.
async function shellStatus() {
  try {
    const st = await api('/api/status');
    const d = diskUse(st.health);
    diskText.textContent = d.text;
    diskFill.style.width = `${d.pct}%`;
    diskFill.className = d.level;
    sideVersion.textContent = `camorage ${st.version || 'dev'}`;
  } catch {
    // a sign-out is handled by the app; the pages report other problems
  }
}

async function route() {
  const mine = ++generation;
  teardown();
  const name = location.hash.replace(/^#\/?/, '').split('/')[0] || 'live';
  for (const a of shell.querySelectorAll('nav a')) {
    const here = a.getAttribute('href') === `#/${name}`;
    a.classList.toggle('active', here);
    if (here) a.setAttribute('aria-current', 'page');
    else a.removeAttribute('aria-current');
  }
  shellStatus();
  const container = h('div');
  main.replaceChildren(container);
  try {
    const done = (await (views[name] || views.live)(container)) || null;
    if (mine !== generation) {
      if (done) done();
      return;
    }
    cleanup = done;
  } catch (err) {
    if (mine === generation && err.message !== 'signed out') container.append(h('p', { class: 'error' }, err.message));
  }
}

async function boot() {
  generation++;
  teardown();
  clearInterval(shellTimer);
  shell.hidden = true;
  const health = await fetch('/api/health').then((r) => r.json());
  if (!health.setupDone) return renderSetup(main, boot);
  const probe = await fetch('/api/status', { credentials: 'same-origin' });
  if (probe.status === 401) return renderLogin(main, boot);
  shell.hidden = false;
  shellTimer = setInterval(shellStatus, 30000);
  return route();
}

window.addEventListener('hashchange', () => { if (!shell.hidden) route(); });
window.addEventListener('camorage:signed-out', () => { if (!shell.hidden) boot(); });
// Pages that do not poll (playback, cameras) would not notice an ended session; check when the
// tab becomes visible again.
document.addEventListener('visibilitychange', () => {
  if (document.visibilityState !== 'visible' || shell.hidden) return;
  fetch('/api/status', { credentials: 'same-origin' })
    .then((r) => { if (r.status === 401) boot(); })
    .catch(() => {});
});
boot();
