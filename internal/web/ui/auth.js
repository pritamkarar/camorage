import { h } from './dom.js';

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

// renderSetup is the first-run screen: the admin password and where recordings go.
export async function renderSetup(root, done) {
  const vols = await fetch('/api/setup/volumes').then((r) => r.json()).catch(() => []);
  const pw = h('input', { type: 'password', autocomplete: 'new-password', minlength: 8, required: true });
  const again = h('input', { type: 'password', autocomplete: 'new-password', minlength: 8, required: true });
  const custom = h('input', { placeholder: '/absolute/path/for/recordings' });
  const choices = vols.map((v, i) => {
    const radio = h('input', { type: 'radio', name: 'vol', value: v.path });
    radio.checked = i === 0;
    return h('label', { class: 'inline' }, radio, `${v.label} — ${(v.freeMB / 1024).toFixed(1)} GB free`, h('code', {}, v.path));
  });
  const other = h('input', { type: 'radio', name: 'vol', value: '' });
  other.checked = vols.length === 0;
  const msg = h('p', { class: 'muted' });
  const form = h('form', {
    class: 'card form narrow',
    onsubmit: async (e) => {
      e.preventDefault();
      if (pw.value !== again.value) {
        msg.className = 'error';
        msg.textContent = 'The passwords do not match.';
        return;
      }
      const picked = form.querySelector('input[name=vol]:checked');
      const recDir = picked && picked.value ? picked.value : custom.value.trim();
      try {
        await post('/api/setup', { password: pw.value, recDir });
        done();
      } catch (err) {
        msg.className = 'error';
        msg.textContent = err.message;
      }
    },
  },
  h('h2', {}, 'Welcome to camorage'),
  h('p', {}, 'Choose the admin password. You will use it to sign in to this portal.'),
  h('label', {}, 'Password (8+ characters)', pw),
  h('label', {}, 'Password again', again),
  h('fieldset', {}, h('legend', {}, 'Where should recordings go?'), ...choices,
    h('label', { class: 'inline' }, other, 'Another folder:', custom)),
  msg,
  h('button', { class: 'primary', type: 'submit' }, 'Start recording'));
  root.replaceChildren(form);
  pw.focus();
}

// renderLogin asks for the admin password.
export function renderLogin(root, done) {
  const pw = h('input', { type: 'password', autocomplete: 'current-password', required: true });
  const msg = h('p', { class: 'muted' });
  const form = h('form', {
    class: 'card form narrow',
    onsubmit: async (e) => {
      e.preventDefault();
      try {
        await post('/api/login', { password: pw.value });
        done();
      } catch (err) {
        msg.className = 'error';
        msg.textContent = err.message;
        pw.select();
      }
    },
  }, h('h2', {}, 'camorage'), h('label', {}, 'Password', pw), msg, h('button', { class: 'primary', type: 'submit' }, 'Sign in'));
  root.replaceChildren(form);
  pw.focus();
}
