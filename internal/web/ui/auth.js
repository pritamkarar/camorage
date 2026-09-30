import { h, brand, field } from './dom.js';

async function post(path, body) {
  const res = await fetch(path, {
    method: 'POST',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
  const data = await res.json().catch(() => ({}));
  if (!res.ok) throw new Error(data.error || `HTTP ${res.status}`);
  return data;
}

function say(el, cls, text) {
  el.className = cls;
  el.textContent = text;
}

// renderSetup is the first-run screen: the admin password and where recordings go.
export async function renderSetup(root, done) {
  const vols = await fetch('/api/setup/volumes').then((r) => r.json()).catch(() => []);
  const pw = h('input', { type: 'password', autocomplete: 'new-password', minlength: 8, required: true });
  const again = h('input', { type: 'password', autocomplete: 'new-password', minlength: 8, required: true });
  const other = h('input', { type: 'radio', name: 'vol', value: '' });
  other.checked = vols.length === 0;
  const custom = h('input', { placeholder: '/absolute/path/for/recordings', 'aria-label': 'Another folder', oninput: () => { other.checked = true; } });
  const choices = vols.map((v, i) => {
    const radio = h('input', { type: 'radio', name: 'vol', value: v.path });
    radio.checked = i === 0;
    return h('label', { class: 'choice' }, radio,
      h('span', {}, v.label, h('small', {}, `${(v.freeMB / 1024).toFixed(1)} GB free`), h('small', { class: 'mono' }, v.path)));
  });
  const msg = h('p', { class: 'muted' });
  const form = h('form', {
    class: 'panel auth',
    onsubmit: async (e) => {
      e.preventDefault();
      if (pw.value !== again.value) {
        say(msg, 'error', 'The passwords do not match.');
        return;
      }
      const picked = form.querySelector('input[name=vol]:checked');
      const recDir = picked && picked.value ? picked.value : custom.value.trim();
      try {
        await post('/api/setup', { password: pw.value, recDir });
        done();
      } catch (err) {
        say(msg, 'error', err.message);
      }
    },
  },
  brand(true),
  h('h1', {}, 'Welcome'),
  h('p', { class: 'lead' }, 'Choose the admin password. You will use it to sign in to this portal.'),
  field('Password (8+ characters)', pw),
  field('Password again', again),
  h('h2', { class: 'section' }, 'Where should recordings go?'),
  ...choices,
  h('label', { class: 'choice' }, other, h('span', { class: 'grow' }, 'Another folder', custom)),
  msg,
  h('button', { class: 'primary', type: 'submit' }, 'Start recording'));
  root.replaceChildren(h('div', { class: 'auth-wrap' }, form));
  pw.focus();
}

// renderLogin asks for the admin password.
export function renderLogin(root, done) {
  const pw = h('input', { type: 'password', autocomplete: 'current-password', required: true });
  const msg = h('p', { class: 'muted' });
  const form = h('form', {
    class: 'panel auth',
    onsubmit: async (e) => {
      e.preventDefault();
      try {
        await post('/api/login', { password: pw.value });
        done();
      } catch (err) {
        say(msg, 'error', err.message);
        pw.select();
      }
    },
  }, brand(true), field('Password', pw), msg, h('button', { class: 'primary', type: 'submit' }, 'Sign in'));
  root.replaceChildren(h('div', { class: 'auth-wrap' }, form));
  pw.focus();
}
