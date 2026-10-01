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

// renderSetup is the first-run screen: the admin password and where recordings go. An SD card
// Termux cannot write to yet is shown with what to do about it and a Check again button.
export async function renderSetup(root, done) {
  const volumes = () => fetch('/api/setup/volumes').then((r) => r.json()).catch(() => []);
  const pw = h('input', { type: 'password', autocomplete: 'new-password', minlength: 8, required: true });
  const again = h('input', { type: 'password', autocomplete: 'new-password', minlength: 8, required: true });
  const other = h('input', { type: 'radio', name: 'vol', value: '' });
  const custom = h('input', { placeholder: '/absolute/path/for/recordings', 'aria-label': 'Another folder', oninput: () => { other.checked = true; } });
  const list = h('div');
  const size = (v) => (v.totalMB > 0 ? h('small', {}, `${(v.freeMB / 1024).toFixed(1)} GB free`) : null);

  // showVolumes lists the choices and picks the first usable one (an SD card comes first),
  // unless another folder was typed in.
  function showVolumes(vols, rechecked) {
    list.replaceChildren(...vols.map((v) => {
      if (!v.ready) {
        const check = h('button', {
          type: 'button',
          onclick: async () => {
            check.disabled = true;
            check.textContent = 'Checking…';
            showVolumes(await volumes(), true);
            // the button was replaced: keep the keyboard on the card (or on it now that it works)
            const next = list.querySelector('.choice.off button') || list.querySelector('input[name=vol]:checked');
            if (next) next.focus();
          },
        }, 'Check again');
        return h('div', { class: 'choice off' }, h('input', { type: 'radio', name: 'vol', disabled: true, 'aria-label': v.label }),
          h('span', { class: 'grow' }, v.label, size(v),
            h('small', {}, rechecked ? 'Still not usable. ' : "Termux can't use it yet. ", 'In Termux, run ', h('code', {}, 'termux-setup-storage'), ' and tap Allow.'),
            check));
      }
      return h('label', { class: 'choice' }, h('input', { type: 'radio', name: 'vol', value: v.path }),
        h('span', {}, v.label, size(v), h('small', { class: 'mono' }, v.path)));
    }));
    if (other.checked && custom.value.trim()) return;
    const first = list.querySelector('input[name=vol]:not([disabled])');
    (first || other).checked = true;
  }
  showVolumes(await volumes(), false);
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
  list,
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
