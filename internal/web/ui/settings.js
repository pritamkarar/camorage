import { h, api, icon, chip, setChip, stat, field, pageHead, sectionHead, drawer, showError } from './dom.js';
import { cloudflareSummary, tailscaleSummary, tunnelChip } from './lib.js';
import { install, installState } from './pwa.js';

const TOKEN_HINT = 'eyJhIjoi… or the whole "cloudflared service install …" command';
const REPO = 'https://github.com/pritamkarar/camorage';

// renderSettings shows phone health, remote access (Tailscale, Cloudflare Tunnel), the background
// programs, the password change and About (GitHub, installing the app); it signs out.
export async function renderSettings(root) {
  const signOut = h('button', {
    class: 'danger',
    onclick: async () => {
      await api('/api/logout', { method: 'POST' }).catch(() => {});
      location.hash = '';
      location.reload();
    },
  }, 'Sign out');
  const head = pageHead('Settings', '', signOut);
  const tiles = h('div', { class: 'stats' });

  // Tailscale: a switch; the status line says what to do next.
  const tsChip = chip('…');
  const tsLine = h('p', { class: 'muted small' });
  const tsSwitch = h('input', { type: 'checkbox', class: 'switch', role: 'switch', 'aria-label': 'Tailscale' });
  tsSwitch.addEventListener('change', async () => {
    const want = tsSwitch.checked;
    if (!want && !confirm('Turn off Tailscale? If you are connected through it, this page stops working.')) {
      tsSwitch.checked = true;
      return;
    }
    tsSwitch.disabled = true;
    try {
      await api('/api/tunnels/tailscale', { method: 'PUT', body: { enabled: want } });
      await refresh();
    } catch (err) {
      tsSwitch.checked = !want;
      if (err.message !== 'signed out') showError(err);
    } finally {
      tsSwitch.disabled = false;
    }
  });
  const tsBox = h('div', { class: 'panel' },
    h('div', { class: 'row' },
      h('div', { class: 'grow' }, h('div', { class: 'row wrap tight' }, h('h3', {}, 'Tailscale'), tsChip), tsLine),
      tsSwitch),
    h('p', { class: 'hint' }, 'Needs MagicDNS and HTTPS certificates turned on in the Tailscale admin console. The first visit can take ~20 s while the certificate is issued.'));

  // Cloudflare Tunnel: the token is write-only; the hostname comes back only as a link.
  let cf = { tokenSet: false, hostname: '' };
  const cfChip = chip('…');
  const cfLine = h('p', { class: 'muted small' });
  const cfBtn = h('button', { onclick: () => editCloudflare() }, 'Set up');
  const cfBox = h('div', { class: 'panel row' },
    h('div', { class: 'grow' }, h('div', { class: 'row wrap tight' }, h('h3', {}, 'Cloudflare Tunnel'), cfChip), cfLine),
    cfBtn);

  function editCloudflare() {
    const d = drawer('Cloudflare Tunnel');
    const host = h('input', { type: 'text', value: cf.hostname || '', placeholder: 'camorage.example.com', autocomplete: 'off', spellcheck: 'false' });
    const token = h('input', { type: 'password', autocomplete: 'off', placeholder: cf.tokenSet ? 'saved — paste a new token to replace it' : TOKEN_HINT });
    d.body.append(
      h('p', { class: 'hint' }, 'In Cloudflare Zero Trust → Networks → Tunnels: create a tunnel, add a public hostname whose service is HTTP 127.0.0.1:8080, and paste the tunnel token here. Video over Cloudflare uses HLS.'),
      field('Public hostname', host),
      field('Tunnel token', token));
    const remove = h('button', {
      type: 'button',
      class: 'danger',
      onclick: async () => {
        if (!confirm('Remove the Cloudflare tunnel? If you are connected through it, this page stops working.')) return;
        try {
          await api('/api/tunnels/cloudflare', { method: 'DELETE' });
          d.close();
          await refresh();
        } catch (err) {
          d.say(err.message, 'error');
        }
      },
    }, 'Remove');
    d.footer('Save', ...(cf.tokenSet ? [remove] : []));
    d.form.onsubmit = async () => {
      d.say('Saving…');
      try {
        await api('/api/tunnels/cloudflare', { method: 'PUT', body: { token: token.value, hostname: host.value } });
        d.close();
        await refresh();
      } catch (err) {
        d.say(err.message, 'error');
      }
    };
    host.focus();
  }

  const procBox = h('div');

  const cur = h('input', { type: 'password', autocomplete: 'current-password', required: true });
  const next = h('input', { type: 'password', autocomplete: 'new-password', minlength: 8, required: true });
  const again = h('input', { type: 'password', autocomplete: 'new-password', minlength: 8, required: true });
  const msg = h('p', { class: 'muted small', role: 'status' });
  const pwForm = h('form', {
    class: 'panel',
    onsubmit: async (e) => {
      e.preventDefault();
      if (next.value !== again.value) {
        say(msg, 'error small', 'The new passwords do not match.');
        return;
      }
      try {
        await api('/api/password', { method: 'POST', body: { current: cur.value, next: next.value } });
        pwForm.reset();
        say(msg, 'ok small', 'Password changed. Other browsers have been signed out.');
      } catch (err) {
        say(msg, 'error small', err.message);
      }
    },
  },
  field('Current password', cur),
  h('div', { class: 'inline-fields' }, field('New password (8+ characters)', next), field('New password again', again)),
  msg,
  h('button', { class: 'primary', type: 'submit' }, 'Change password'));

  const aboutVersion = h('small', {}, '');
  const installBox = h('div');
  const about = h('div', { class: 'panel stack' },
    h('a', { class: 'github', href: REPO, target: '_blank', rel: 'noopener' },
      icon('i-github', 'gh-mark'),
      h('span', {}, h('b', {}, 'Star camorage on GitHub'), aboutVersion),
      h('span', { class: 'star', 'aria-hidden': 'true' }, '★')),
    installBox);

  // showInstall offers to install camorage as an app, or says how; nothing when it already is one.
  function showInstall() {
    const s = installState();
    installBox.replaceChildren(
      s === 'ready' ? h('button', { onclick: async () => { await install(); showInstall(); } }, icon('i-download'), 'Install app')
        : s === 'https' ? h('p', { class: 'hint' }, 'To install camorage as an app, open it through its Tailscale or Cloudflare address (HTTPS).')
          : s === 'menu' ? h('p', { class: 'hint' }, "To install camorage as an app, use your browser's menu: Install app, or Share → Add to Home Screen on an iPhone.")
            : '');
  }

  root.append(head,
    sectionHead('Phone'), tiles,
    sectionHead('Remote access'), tsBox, cfBox,
    sectionHead('Background programs'), procBox,
    sectionHead('Account'), pwForm,
    sectionHead('About'), about);

  async function refresh() {
    try {
      const [st, tn] = await Promise.all([api('/api/status'), api('/api/tunnels')]);
      const hl = st.health;
      const version = st.version || 'dev';
      head.querySelector('.sub').textContent = `camorage ${version}`;
      aboutVersion.textContent = `Version ${version} · MIT licence`;
      tiles.replaceChildren(
        hl.batteryPct < 0 ? stat('unknown', 'battery')
          : stat(`${hl.batteryPct}%`, `battery · ${(hl.charging || '?').toLowerCase()} · ${hl.batteryTempC.toFixed(1)} °C`),
        stat(hl.memFreeMB < 0 ? 'unknown' : `${hl.memFreeMB} MB`, 'free memory'),
        hl.diskErr ? stat('error', hl.diskErr)
          : stat(`${(hl.diskFreeMB / 1024).toFixed(1)} GB`, `free for recordings, of ${(hl.diskTotalMB / 1024).toFixed(1)} GB`),
        stat(st.time.slice(11, 16), `phone time · ${st.time.slice(0, 10)}`));

      const procs = st.processes || [];
      procBox.replaceChildren(procs.length
        ? h('div', { class: 'panel flush table-wrap' }, h('table', { class: 'table' },
          h('thead', {}, h('tr', {}, ['Program', 'State', 'Restarts', 'Last exit'].map((t) => h('th', {}, t)))),
          h('tbody', {}, procs.map((p) => h('tr', {},
            h('td', {}, p.name),
            h('td', {}, p.running ? [chip('running', 'ok'), h('span', { class: 'muted small' }, ` pid ${p.pid}`)] : chip('stopped', 'bad')),
            h('td', {}, String(p.restarts)),
            h('td', { class: 'small' }, p.lastExit || '—'))))))
        : h('p', { class: 'muted' }, 'Nothing running yet.'));
      if (st.mediamtxError) procBox.append(h('p', { class: 'error' }, `MediaMTX: ${st.mediamtxError}`));

      if (!tsSwitch.disabled) tsSwitch.checked = tn.tailscale.enabled;
      const ts = tailscaleSummary(tn.tailscale);
      setChip(tsChip, tunnelChip(ts.state));
      showLine(tsLine, ts);
      cf = tn.cloudflare;
      const c = cloudflareSummary(cf);
      setChip(cfChip, tunnelChip(c.state));
      showLine(cfLine, c);
      cfBtn.textContent = cf.tokenSet ? 'Change' : 'Set up';
      showInstall();
    } catch (err) {
      if (err.message !== 'signed out') showError(err);
    }
  }
  refresh();
  const timer = setInterval(refresh, 5000);
  return () => clearInterval(timer);
}

function say(el, cls, text) {
  el.className = cls;
  el.textContent = text;
}

// showLine renders a {text, link, error} summary from lib.js.
function showLine(el, s) {
  el.className = s.error ? 'error small' : 'muted small';
  el.replaceChildren(s.text, ...(s.link ? [' ', h('a', { href: s.link, target: '_blank', rel: 'noopener' }, s.link)] : []));
}
