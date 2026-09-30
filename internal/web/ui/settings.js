import { h, api, dl, showError } from './dom.js';
import { cloudflareSummary, tailscaleSummary } from './lib.js';

const TOKEN_HINT = 'eyJhIjoi… or the whole "cloudflared service install …" command';

// renderSettings shows phone health, remote access (Tailscale, Cloudflare Tunnel) and the
// background programs, and lets you change the admin password or sign out.
export async function renderSettings(root) {
  const healthBox = h('div', { class: 'card' });
  const procBox = h('div', { class: 'card' });

  // Tailscale: one button; the status line says what to do next.
  let tsOn = false;
  const tsLine = h('p');
  const tsBtn = h('button', {
    onclick: async () => {
      if (tsOn && !confirm('Turn off Tailscale? If you are connected through it, this page stops working.')) return;
      tsBtn.disabled = true;
      try {
        await api('/api/tunnels/tailscale', { method: 'PUT', body: { enabled: !tsOn } });
        await refresh();
      } catch (err) {
        if (err.message !== 'signed out') showError(root, err);
      } finally {
        tsBtn.disabled = false;
      }
    },
  }, 'Turn on Tailscale');
  const tsBox = h('div', { class: 'card' }, h('h4', {}, 'Tailscale'), tsLine,
    h('p', { class: 'muted' }, 'Needs MagicDNS and HTTPS certificates turned on in the Tailscale admin console. '
      + 'The first visit can take ~20 s while the certificate is issued.'),
    tsBtn);

  // Cloudflare Tunnel: the token is write-only; the hostname comes back only as a link.
  let fillHost = true;
  const cfLine = h('p');
  const cfHost = h('input', { type: 'text', placeholder: 'camorage.example.com', autocomplete: 'off', spellcheck: 'false' });
  const cfToken = h('input', { type: 'password', autocomplete: 'off', placeholder: TOKEN_HINT });
  const cfMsg = h('p', { class: 'muted' });
  const cfRemove = h('button', {
    type: 'button',
    class: 'danger',
    hidden: true,
    onclick: async () => {
      if (!confirm('Remove the Cloudflare tunnel? If you are connected through it, this page stops working.')) return;
      try {
        await api('/api/tunnels/cloudflare', { method: 'DELETE' });
        fillHost = true;
        say(cfMsg, 'ok', 'Removed.');
        await refresh();
      } catch (err) {
        say(cfMsg, 'error', err.message);
      }
    },
  }, 'Remove');
  const cfForm = h('form', {
    class: 'card form',
    onsubmit: async (e) => {
      e.preventDefault();
      try {
        await api('/api/tunnels/cloudflare', { method: 'PUT', body: { token: cfToken.value, hostname: cfHost.value } });
        cfToken.value = '';
        fillHost = true;
        say(cfMsg, 'ok', 'Saved. The tunnel connects within a few seconds.');
        await refresh();
      } catch (err) {
        say(cfMsg, 'error', err.message);
      }
    },
  }, h('h4', {}, 'Cloudflare Tunnel'), cfLine,
  h('p', { class: 'muted' }, 'In Cloudflare Zero Trust → Networks → Tunnels: create a tunnel, add a public hostname '
    + 'whose service is HTTP 127.0.0.1:8080, and paste the tunnel token here. Video over Cloudflare uses HLS.'),
  h('label', {}, 'Public hostname', cfHost), h('label', {}, 'Tunnel token', cfToken), cfMsg,
  h('button', { class: 'primary', type: 'submit' }, 'Save'), ' ', cfRemove);

  const cur = h('input', { type: 'password', autocomplete: 'current-password', required: true });
  const next = h('input', { type: 'password', autocomplete: 'new-password', minlength: 8, required: true });
  const again = h('input', { type: 'password', autocomplete: 'new-password', minlength: 8, required: true });
  const msg = h('p', { class: 'muted' });
  const pwForm = h('form', {
    class: 'card form',
    onsubmit: async (e) => {
      e.preventDefault();
      if (next.value !== again.value) {
        say(msg, 'error', 'The new passwords do not match.');
        return;
      }
      try {
        await api('/api/password', { method: 'POST', body: { current: cur.value, next: next.value } });
        pwForm.reset();
        say(msg, 'ok', 'Password changed. Other browsers have been signed out.');
      } catch (err) {
        say(msg, 'error', err.message);
      }
    },
  }, h('h3', {}, 'Change password'),
  h('label', {}, 'Current password', cur), h('label', {}, 'New password (8+ characters)', next), h('label', {}, 'New password again', again),
  msg, h('button', { class: 'primary', type: 'submit' }, 'Change password'));
  const signOut = h('button', {
    class: 'danger',
    onclick: async () => {
      await api('/api/logout', { method: 'POST' }).catch(() => {});
      location.hash = '';
      location.reload();
    },
  }, 'Sign out');
  root.append(h('h3', {}, 'Phone'), healthBox, h('h3', {}, 'Remote access'), tsBox, cfForm,
    h('h3', {}, 'Background programs'), procBox, pwForm, signOut);

  async function refresh() {
    try {
      const [st, tn] = await Promise.all([api('/api/status'), api('/api/tunnels')]);
      const hl = st.health;
      healthBox.replaceChildren(dl([
        ['camorage version', st.version || 'dev'],
        ['Phone time', st.time.replace('T', ' ')],
        ['Battery', hl.batteryPct < 0 ? 'unknown' : `${hl.batteryPct}% (${hl.charging || '?'}), ${hl.batteryTempC.toFixed(1)} °C`],
        ['Free memory', hl.memFreeMB < 0 ? 'unknown' : `${hl.memFreeMB} MB`],
        ['Recordings storage', hl.diskErr ? `error: ${hl.diskErr}`
          : `${(hl.diskFreeMB / 1024).toFixed(1)} GB free of ${(hl.diskTotalMB / 1024).toFixed(1)} GB`],
      ]));
      const procs = (st.processes || []).map((p) => dl([
        ['Program', p.name],
        ['State', p.running ? `running (pid ${p.pid})` : 'stopped'],
        ['Restarts', String(p.restarts)],
        ['Last exit', p.lastExit || '—'],
      ]));
      procBox.replaceChildren(...(procs.length ? procs : [h('p', { class: 'muted' }, 'Nothing running yet.')]));
      if (st.mediamtxError) procBox.append(h('p', { class: 'error' }, `MediaMTX: ${st.mediamtxError}`));

      tsOn = tn.tailscale.enabled;
      tsBtn.textContent = tsOn ? 'Turn off Tailscale' : 'Turn on Tailscale';
      showLine(tsLine, tailscaleSummary(tn.tailscale));
      showLine(cfLine, cloudflareSummary(tn.cloudflare));
      cfRemove.hidden = !tn.cloudflare.tokenSet;
      cfToken.placeholder = tn.cloudflare.tokenSet ? 'saved — paste a new token to replace it' : TOKEN_HINT;
      if (fillHost) { // only after load, save or remove: never while someone is typing
        cfHost.value = tn.cloudflare.hostname;
        fillHost = false;
      }
    } catch (err) {
      if (err.message !== 'signed out') showError(root, err);
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
  el.className = s.error ? 'error' : '';
  el.replaceChildren(s.text, ...(s.link ? [' ', h('a', { href: s.link, target: '_blank', rel: 'noopener' }, s.link)] : []));
}
