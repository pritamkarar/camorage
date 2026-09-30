import { h, api, dl, showError } from './dom.js';

const TYPES = { drive: 'Google Drive', s3: 'S3-compatible', local: 'Folder on the phone' };
const gb = (mb) => `${(mb / 1024).toFixed(1)} GB`;

// renderStorage shows what recordings take and how long they fit, how cloud copies are doing,
// and the cloud storage targets; it adds Google Drive and S3-compatible targets.
export async function renderStorage(root) {
  const local = h('div', { class: 'card' });
  const uploads = h('div', { class: 'card' });
  const targets = h('div');
  const panel = h('div');
  root.append(h('h3', {}, 'On the phone'), local, h('h3', {}, 'Cloud copies'), uploads,
    h('h3', {}, 'Cloud storage'), targets,
    h('div', { class: 'toolbar' },
      h('button', { onclick: () => addDrive() }, 'Add Google Drive'),
      h('button', { onclick: () => addS3() }, 'Add S3-compatible storage')),
    panel);

  async function refresh() {
    const s = await api('/api/storage');
    local.replaceChildren(dl([
      ['Recordings folder', s.recDir || '—'],
      ['Used by recordings', gb(s.usedMB)],
      ['Recorded per day lately', s.perDayMB ? gb(s.perDayMB) : 'not known yet'],
      ['Free', `${gb(s.freeMB)} of ${gb(s.totalMB)}`],
      ['Days that fit', s.daysFit ? s.daysFit.toFixed(1) : 'not known yet'],
    ]));
    const ups = Object.entries(s.uploads || {});
    uploads.replaceChildren(...(ups.length ? ups.map(([cam, u]) => dl([
      ['Camera', cam],
      ['Waiting to upload', String(u.queued)],
      ['Last upload', u.lastUpload ? u.lastUpload.replace('T', ' ').slice(0, 19) : '—'],
      ['Last error', u.lastError || '—'],
    ])) : [h('p', { class: 'muted' }, 'No camera copies to the cloud yet: choose a storage in a camera\'s settings.')]));
    targets.replaceChildren(...(s.targets.length ? s.targets.map(row) : [h('p', { class: 'muted' }, 'None yet.')]));
  }

  function row(t) {
    const msg = h('span', { class: 'muted' });
    const test = async () => {
      msg.className = 'muted';
      msg.textContent = 'Testing…';
      try {
        await api(`/api/storage/${encodeURIComponent(t.id)}/test`, { method: 'POST' });
        msg.className = 'ok';
        msg.textContent = 'Works.';
      } catch (err) {
        msg.className = 'error';
        msg.textContent = err.message;
      }
    };
    const remove = async () => {
      if (!confirm(`Remove "${t.name}"? Clips already copied there stay.`)) return;
      try {
        await api(`/api/storage/${encodeURIComponent(t.id)}`, { method: 'DELETE' });
        await refresh();
      } catch (err) {
        showError(root, err);
      }
    };
    return h('div', { class: 'card' },
      h('div', {}, h('strong', {}, t.name), ' ', h('span', { class: 'muted' }, TYPES[t.type] || t.type)),
      h('div', { class: 'actions' }, h('button', { onclick: test }, 'Test'), h('button', { class: 'danger', onclick: remove }, 'Remove'), msg));
  }

  function addDrive() {
    const name = h('input', { value: 'Google Drive', required: true });
    const clientId = h('input', { required: true, autocomplete: 'off', spellcheck: 'false' });
    const secret = h('input', { type: 'password', required: true, autocomplete: 'off' });
    const msg = h('p', { class: 'muted' });
    const step2 = h('div');
    const step1 = h('form', {
      class: 'card form',
      onsubmit: async (e) => {
        e.preventDefault();
        try {
          const r = await api('/api/storage/drive/start', { method: 'POST', body: { name: name.value.trim(), clientId: clientId.value.trim(), clientSecret: secret.value.trim() } });
          msg.textContent = '';
          step2.replaceChildren(finishForm(r.authUrl));
        } catch (err) {
          msg.className = 'error';
          msg.textContent = err.message;
        }
      },
    }, h('h3', {}, 'Add Google Drive'),
    h('p', { class: 'muted' }, 'Use your own OAuth client of type "Desktop app" (Google Cloud Console → APIs & Services → Credentials), with the Google Drive API enabled. Publish its consent screen ("In production"): while it is "Testing", Google ends the sign-in after 7 days. Clips go to a "camorage" folder; the portal can only see files it created.'),
    h('label', {}, 'Name', name), h('label', {}, 'Client ID', clientId), h('label', {}, 'Client secret', secret), msg,
    h('button', { class: 'primary', type: 'submit' }, 'Next'));
    panel.replaceChildren(step1, step2);
  }

  function finishForm(authUrl) {
    const pasted = h('input', { required: true, autocomplete: 'off', placeholder: 'http://127.0.0.1:53682/?state=…&code=…' });
    const msg = h('p', { class: 'muted' });
    return h('form', {
      class: 'card form',
      onsubmit: async (e) => {
        e.preventDefault();
        msg.className = 'muted';
        msg.textContent = 'Checking with Google…';
        try {
          await api('/api/storage/drive/finish', { method: 'POST', body: { url: pasted.value.trim() } });
          panel.replaceChildren();
          await refresh();
        } catch (err) {
          msg.className = 'error';
          msg.textContent = err.message;
        }
      },
    }, h('p', {}, h('a', { href: authUrl, target: '_blank', rel: 'noopener' }, 'Sign in with Google'),
      '. After you allow access, the browser opens a page that does not load (its address starts with http://127.0.0.1:53682/). Copy that whole address from the address bar and paste it here.'),
    h('label', {}, 'Address of the page that did not load', pasted), msg,
    h('button', { class: 'primary', type: 'submit' }, 'Finish'));
  }

  function addS3() {
    const name = h('input', { value: 'Cloud storage', required: true });
    const provider = h('select', {},
      h('option', { value: 'AWS' }, 'Amazon S3'),
      h('option', { value: 'Other' }, 'Other S3-compatible (Backblaze B2, Cloudflare R2, Wasabi, MinIO …)'));
    const endpoint = h('input', { autocomplete: 'off', spellcheck: 'false', placeholder: 'e.g. s3.us-west-004.backblazeb2.com (not for Amazon S3)' });
    const region = h('input', { autocomplete: 'off', placeholder: 'optional, e.g. us-east-1' });
    const bucket = h('input', { required: true, autocomplete: 'off', spellcheck: 'false' });
    const keyId = h('input', { required: true, autocomplete: 'off', spellcheck: 'false' });
    const secret = h('input', { type: 'password', required: true, autocomplete: 'off' });
    const msg = h('p', { class: 'muted' });
    panel.replaceChildren(h('form', {
      class: 'card form',
      onsubmit: async (e) => {
        e.preventDefault();
        msg.className = 'muted';
        msg.textContent = 'Checking the bucket…';
        try {
          await api('/api/storage/s3', {
            method: 'POST',
            body: { name: name.value.trim(), provider: provider.value, endpoint: endpoint.value.trim(), region: region.value.trim(), bucket: bucket.value.trim(), accessKeyId: keyId.value.trim(), secretAccessKey: secret.value },
          });
          panel.replaceChildren();
          await refresh();
        } catch (err) {
          msg.className = 'error';
          msg.textContent = err.message;
        }
      },
    }, h('h3', {}, 'Add S3-compatible storage'),
    h('label', {}, 'Name', name), h('label', {}, 'Provider', provider), h('label', {}, 'Endpoint', endpoint),
    h('label', {}, 'Region', region), h('label', {}, 'Bucket (must exist)', bucket),
    h('label', {}, 'Access key ID', keyId), h('label', {}, 'Secret access key', secret), msg,
    h('button', { class: 'primary', type: 'submit' }, 'Add')));
  }

  await refresh();
  const timer = setInterval(() => refresh().catch(() => {}), 10000);
  return () => clearInterval(timer);
}
