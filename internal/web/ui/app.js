import { h } from './dom.js';
import { renderSetup, renderLogin } from './auth.js';
import { renderLive } from './live.js';
import { renderPlayback } from './playback.js';
import { renderCameras } from './cameras.js';
import { renderSettings } from './settings.js';
import { renderStorage } from './storage.js';

const views = { live: renderLive, playback: renderPlayback, cameras: renderCameras, storage: renderStorage, settings: renderSettings };
const main = document.getElementById('view');
const bar = document.getElementById('bar');
let cleanup = null;
let generation = 0; // a view that finishes rendering after the user moved on is discarded

function teardown() {
  if (cleanup) cleanup();
  cleanup = null;
}

async function route() {
  const mine = ++generation;
  teardown();
  const name = location.hash.replace(/^#\/?/, '').split('/')[0] || 'live';
  for (const a of bar.querySelectorAll('nav a')) a.classList.toggle('active', a.getAttribute('href') === `#/${name}`);
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
  bar.hidden = true;
  const health = await fetch('/api/health').then((r) => r.json());
  if (!health.setupDone) return renderSetup(main, boot);
  const probe = await fetch('/api/status', { credentials: 'same-origin' });
  if (probe.status === 401) return renderLogin(main, boot);
  bar.hidden = false;
  return route();
}

window.addEventListener('hashchange', () => { if (!bar.hidden) route(); });
window.addEventListener('camorage:signed-out', () => { if (!bar.hidden) boot(); });
// Pages that do not poll (playback, cameras) would not notice an ended session; check when the
// tab becomes visible again.
document.addEventListener('visibilitychange', () => {
  if (document.visibilityState !== 'visible' || bar.hidden) return;
  fetch('/api/status', { credentials: 'same-origin' })
    .then((r) => { if (r.status === 401) boot(); })
    .catch(() => {});
});
boot();
