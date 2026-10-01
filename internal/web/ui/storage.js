import { h, api, icon, chip, stat, field, pageHead, sectionHead, drawer, showError, skel } from './dom.js';
import { storageSplit } from './lib.js';

const TYPES = { drive: 'Google Drive', s3: 'S3-compatible', local: 'Folder on the phone' };
const gb = (mb) => `${(mb / 1024).toFixed(1)} GB`;

// renderStorage shows what recordings take and how long they fit, how cloud copies are doing,
// and the cloud storage targets; Google Drive and S3-compatible targets are added in a drawer.
export async function renderStorage(root) {
  const head = pageHead('Storage', '');
  // skeletons until the first refresh, which can take a while: it measures the recordings
  const stats = h('div', { class: 'stats' }, skel('stat'), skel('stat'), skel('stat'), skel('stat'));
  const recFill = h('i');
  const otherFill = h('i', { class: 'other' });
  const meter = h('div', { class: 'meter big loading', role: 'img' }, recFill, otherFill);
  const uploads = h('div', {}, skel('table'));
  const targets = h('div', {}, skel('card'));
  root.append(head, stats, meter,
    h('div', { class: 'legend' }, h('span', {}, 'Recordings'), h('span', { class: 'other' }, 'Other files'), h('span', { class: 'free' }, 'Free')),
    sectionHead('Cloud copies'), uploads,
    sectionHead('Cloud storage',
      h('button', { onclick: () => addDrive() }, icon('i-plus'), 'Google Drive'),
      h('button', { onclick: () => addS3() }, icon('i-plus'), 'S3-compatible')),
    targets);

  async function refresh() {
    const [s, cams] = await Promise.all([api('/api/storage'), api('/api/cameras').catch(() => [])]);
    const names = new Map(cams.map((c) => [c.id, c.name]));
    head.querySelector('.sub').textContent = s.recDir || 'No recordings folder yet';
    stats.replaceChildren(
      stat(gb(s.usedMB), 'used by recordings'),
      s.perDayMB ? stat(gb(s.perDayMB), 'recorded per day') : stat('—', 'per day: not known yet'),
      stat(gb(s.freeMB), `free of ${gb(s.totalMB)}`),
      s.daysFit ? stat(`${s.daysFit.toFixed(1)} days`, 'still fit') : stat('—', 'days that fit: not known yet'));
    const split = storageSplit(s);
    meter.classList.remove('loading');
    recFill.style.width = `${split.recordings}%`;
    otherFill.style.width = `${split.other}%`;
    meter.setAttribute('aria-label', `${Math.round(split.recordings)}% recordings, ${Math.round(split.other)}% other files, ${Math.round(split.free)}% free`);
    const ups = Object.entries(s.uploads || {});
    uploads.replaceChildren(ups.length
      ? h('div', { class: 'panel flush table-wrap' }, h('table', { class: 'table' },
        h('thead', {}, h('tr', {}, ['Camera', 'Waiting', 'Last upload', 'Status'].map((t) => h('th', {}, t)))),
        h('tbody', {}, ups.map(([cam, u]) => h('tr', {},
          h('td', {}, names.get(cam) || cam),
          h('td', {}, String(u.queued)),
          h('td', {}, u.lastUpload ? u.lastUpload.replace('T', ' ').slice(0, 19) : '—'),
          h('td', {}, u.lastError ? [chip('Error', 'bad'), ' ', h('span', { class: 'small' }, u.lastError)] : chip('OK', 'ok')))))))
      : h('p', { class: 'muted' }, "No camera copies to the cloud yet: choose a storage in a camera's settings."));
    targets.replaceChildren(...(s.targets.length ? s.targets.map(row) : [h('p', { class: 'muted' }, 'None yet.')]));
  }

  function row(t) {
    const msg = h('span', { role: 'status' });
    const test = async () => {
      msg.className = 'muted';
      msg.textContent = ' · Testing…';
      try {
        await api(`/api/storage/${encodeURIComponent(t.id)}/test`, { method: 'POST' });
        msg.className = 'ok';
        msg.textContent = ' · Works.';
      } catch (err) {
        msg.className = 'error';
        msg.textContent = ` · ${err.message}`;
      }
    };
    const remove = async () => {
      if (!confirm(`Remove "${t.name}"? Clips already copied there stay.`)) return;
      try {
        await api(`/api/storage/${encodeURIComponent(t.id)}`, { method: 'DELETE' });
        await refresh();
      } catch (err) {
        if (err.message !== 'signed out') showError(err);
      }
    };
    return h('div', { class: 'panel row wrap' },
      h('span', { class: 'cam-icon' }, icon('i-cloud')),
      h('div', { class: 'grow' }, h('h3', {}, t.name), h('p', { class: 'muted small' }, TYPES[t.type] || t.type, msg)),
      h('div', { class: 'head-actions' }, h('button', { onclick: test }, 'Test'), h('button', { class: 'danger', onclick: remove }, 'Remove')));
  }

  function addDrive() {
    const d = drawer('Add Google Drive');
    const name = h('input', { value: 'Google Drive', required: true });
    const clientId = h('input', { required: true, autocomplete: 'off', spellcheck: 'false' });
    const secret = h('input', { type: 'password', required: true, autocomplete: 'off' });
    d.body.append(
      h('p', { class: 'hint' }, 'Use your own OAuth client of type "Desktop app" (Google Cloud Console → APIs & Services → Credentials), with the Google Drive API enabled. Publish its consent screen ("In production"): while it is "Testing", Google ends the sign-in after 7 days. Clips go to a "camorage" folder; the portal can only see files it created.'),
      field('Name', name), field('Client ID', clientId), field('Client secret', secret));
    d.footer('Next');
    d.form.onsubmit = async () => {
      d.say('Starting…');
      try {
        const r = await api('/api/storage/drive/start', { method: 'POST', body: { name: name.value.trim(), clientId: clientId.value.trim(), clientSecret: secret.value.trim() } });
        finishDrive(d, r.authUrl);
      } catch (err) {
        d.say(err.message, 'error');
      }
    };
    name.focus();
  }

  function finishDrive(d, authUrl) {
    d.reset('Add Google Drive: sign in');
    const pasted = h('input', { required: true, autocomplete: 'off', placeholder: 'http://127.0.0.1:53682/?state=…&code=…' });
    d.body.append(
      h('p', {}, h('a', { href: authUrl, target: '_blank', rel: 'noopener' }, 'Sign in with Google ', icon('i-external')),
        '. After you allow access, the browser opens a page that does not load (its address starts with http://127.0.0.1:53682/). Copy that whole address from the address bar and paste it here.'),
      field('Address of the page that did not load', pasted));
    d.footer('Finish');
    d.form.onsubmit = async () => {
      d.say('Checking with Google…');
      try {
        await api('/api/storage/drive/finish', { method: 'POST', body: { url: pasted.value.trim() } });
        d.close();
        await refresh();
      } catch (err) {
        d.say(err.message, 'error');
      }
    };
  }

  function addS3() {
    const d = drawer('Add S3-compatible storage');
    const name = h('input', { value: 'Cloud storage', required: true });
    const provider = h('select', {},
      h('option', { value: 'AWS' }, 'Amazon S3'),
      h('option', { value: 'Other' }, 'Other S3-compatible (Backblaze B2, Cloudflare R2, Wasabi, MinIO …)'));
    const endpoint = h('input', { autocomplete: 'off', spellcheck: 'false', placeholder: 'e.g. s3.us-west-004.backblazeb2.com' });
    const region = h('input', { autocomplete: 'off', placeholder: 'optional, e.g. us-east-1' });
    const bucket = h('input', { required: true, autocomplete: 'off', spellcheck: 'false' });
    const keyId = h('input', { required: true, autocomplete: 'off', spellcheck: 'false' });
    const secret = h('input', { type: 'password', required: true, autocomplete: 'off' });
    d.body.append(
      field('Name', name), field('Provider', provider), field('Endpoint', endpoint, 'Not needed for Amazon S3.'),
      field('Region', region), field('Bucket', bucket, 'It must exist already.'),
      field('Access key ID', keyId), field('Secret access key', secret));
    d.footer('Add');
    d.form.onsubmit = async () => {
      d.say('Checking the bucket…');
      try {
        await api('/api/storage/s3', {
          method: 'POST',
          body: { name: name.value.trim(), provider: provider.value, endpoint: endpoint.value.trim(), region: region.value.trim(), bucket: bucket.value.trim(), accessKeyId: keyId.value.trim(), secretAccessKey: secret.value },
        });
        d.close();
        await refresh();
      } catch (err) {
        d.say(err.message, 'error');
      }
    };
    name.focus();
  }

  await refresh();
  const timer = setInterval(() => refresh().catch(() => {}), 10000);
  return () => clearInterval(timer);
}
