import { h, api, playHLS, showError } from './dom.js';
import { DAY_NAMES, cellAt, cleanWindows, compactMask, expandMask, scheduleSummary, streamPaths } from './lib.js';

// renderCameras lists cameras with edit/delete, and adds cameras by RTSP URL or by ONVIF scan.
export async function renderCameras(root) {
  const panel = h('div');
  const list = h('div');
  let targets = [];
  let closeForm = () => {}; // stops the ignore-area editor's live video when the form goes away
  const show = (...kids) => {
    closeForm();
    closeForm = () => {};
    panel.replaceChildren(...kids);
  };
  root.append(h('div', { class: 'toolbar' },
    h('button', { class: 'primary', onclick: () => edit(null) }, 'Add camera'),
    h('button', { onclick: () => scan() }, 'Scan network')), panel, list);

  async function refresh() {
    targets = (await api('/api/storage').catch(() => ({ targets: [] }))).targets;
    const cams = await api('/api/cameras');
    list.replaceChildren(...(cams.length ? cams.map(row) : [h('p', { class: 'empty' }, 'No cameras yet.')]));
  }

  function row(cam) {
    const mode = cam.mode === 'motion' ? 'motion only' : 'continuous';
    return h('div', { class: 'card' },
      h('div', {}, h('strong', {}, cam.name), ' ', h('span', { class: 'muted' }, cam.id), ' ',
        cam.enabled ? '' : h('span', { class: 'badge bad' }, 'disabled')),
      h('div', { class: 'muted' },
        `Records ${mode}: ${scheduleSummary(cam.schedule)} · keeps ${cam.localDays} day${cam.localDays === 1 ? '' : 's'} on the phone`),
      h('div', { class: 'actions' },
        h('button', { onclick: () => edit(cam) }, 'Edit'),
        h('button', { class: 'danger', onclick: () => remove(cam) }, 'Delete')));
  }

  async function remove(cam) {
    if (!confirm(`Delete camera "${cam.name}"? Its recordings stay until they age out.`)) return;
    try {
      await api(`/api/cameras/${encodeURIComponent(cam.id)}`, { method: 'DELETE' });
      await refresh();
    } catch (err) {
      showError(root, err);
    }
  }

  // edit shows the camera form: cam is null for a new camera; prefill seeds a new camera's fields.
  function edit(cam, prefill = {}) {
    const base = cam || { name: '', enabled: true, mainUrl: '', subUrl: '', localDays: 1, schedule: [], mode: 'continuous', motion: {}, ...prefill };
    const m = base.motion || {};
    const name = h('input', { value: base.name, required: true, maxlength: 60 });
    const enabled = h('input', { type: 'checkbox' });
    enabled.checked = base.enabled;
    const mainUrl = h('input', { value: base.mainUrl, required: true, placeholder: 'rtsp://user:pass@192.168.1.10/stream' });
    const subUrl = h('input', { value: base.subUrl, placeholder: 'optional low-resolution stream' });
    const localDays = h('input', { type: 'number', min: 1, max: 365, value: base.localDays });
    const rows = h('div');
    const addRow = (w = { days: [1, 2, 3, 4, 5, 6, 7], start: '00:00', end: '00:00' }) => rows.append(windowRow(w));
    (base.schedule || []).forEach((w) => addRow(w));

    const mode = h('select', {},
      h('option', { value: 'continuous' }, 'Continuous: keep everything'),
      h('option', { value: 'motion' }, 'Motion only: keep footage around motion'));
    mode.value = base.mode === 'motion' ? 'motion' : 'continuous';
    const sens = h('select', {}, h('option', { value: 'low' }, 'Low'), h('option', { value: 'medium' }, 'Medium'), h('option', { value: 'high' }, 'High'));
    sens.value = m.sensitivity || 'medium';
    const pre = h('input', { type: 'number', min: 1, max: 120, value: m.preRollSec || 10 });
    const post = h('input', { type: 'number', min: 1, max: 600, value: m.postRollSec || 30 });
    const maskSlot = h('div');
    let mask = null;
    const motionBox = h('fieldset', {}, h('legend', {}, 'Motion'),
      h('p', { class: 'muted' }, 'Footage with no motion is deleted after 1 hour. Recordings made before switching to motion mode are kept.'),
      h('label', {}, 'Sensitivity', sens),
      h('label', {}, 'Seconds to keep before motion', pre),
      h('label', {}, 'Seconds to keep after motion', post),
      h('p', {}, 'Ignore areas: drag across the picture to mark places where movement should not count (trees, a busy road, the clock the camera prints). Drag from a marked block to unmark.'),
      maskSlot);
    const syncMode = () => {
      motionBox.hidden = mode.value !== 'motion';
      if (motionBox.hidden || mask) return;
      if (!cam) {
        maskSlot.replaceChildren(h('p', { class: 'muted' }, 'Save the camera first, then edit it to mark ignore areas.'));
        return;
      }
      mask = maskEditor(cam, m.ignore);
      maskSlot.replaceChildren(mask.el);
    };
    mode.addEventListener('change', syncMode);

    const cloudTarget = h('select', {}, h('option', { value: '' }, 'No cloud copy'), targets.map((t) => h('option', { value: t.id }, t.name)));
    cloudTarget.value = base.cloud ? base.cloud.targetId : '';
    const cloudDays = h('input', { type: 'number', min: 1, max: 3650, value: (base.cloud && base.cloud.days) || 30 });
    const cloudBox = h('fieldset', {}, h('legend', {}, 'Cloud copy'),
      h('p', { class: 'muted' }, targets.length
        ? 'Copies what this camera records from now on: hour by hour, or event by event in motion mode.'
        : 'Add cloud storage on the Storage page first.'),
      h('label', {}, 'Copy to', cloudTarget),
      h('label', {}, 'Days to keep in the cloud', cloudDays));
    const form = h('form', { class: 'card form', onsubmit: (e) => { e.preventDefault(); save(); } },
      h('h3', {}, cam ? `Edit ${cam.name}` : 'Add camera'),
      h('label', {}, 'Name', name),
      h('label', { class: 'inline' }, enabled, 'Enabled'),
      h('label', {}, 'Main stream (RTSP URL)', mainUrl),
      h('label', {}, 'Substream (RTSP URL)', subUrl),
      h('label', {}, 'Days to keep on the phone', localDays),
      h('label', {}, 'Recording mode', mode),
      motionBox,
      cloudBox,
      h('fieldset', {}, h('legend', {}, 'Recording schedule'),
        h('p', { class: 'muted' }, 'No windows means record all the time. A window ending at or before its start runs past midnight.'),
        rows,
        h('button', { type: 'button', onclick: () => addRow() }, 'Add window')),
      h('div', { class: 'actions' },
        h('button', { class: 'primary', type: 'submit' }, 'Save'),
        h('button', { type: 'button', onclick: () => show() }, 'Cancel')));
    show(form);
    closeForm = () => { if (mask) mask.close(); };
    syncMode();
    name.focus();

    async function save() {
      const body = {
        ...(cam || {}),
        name: name.value.trim(),
        enabled: enabled.checked,
        mainUrl: mainUrl.value.trim(),
        subUrl: subUrl.value.trim(),
        localDays: Number(localDays.value) || 1,
        mode: mode.value,
        cloud: cloudTarget.value ? { ...(cam && cam.cloud), targetId: cloudTarget.value, days: Number(cloudDays.value) || 30 } : null,
        motion: {
          ...m,
          source: 'phone',
          sensitivity: sens.value,
          preRollSec: Number(pre.value) || 10,
          postRollSec: Number(post.value) || 30,
          ignore: mask ? mask.read() : (m.ignore || []),
        },
        schedule: cleanWindows([...rows.children].map((r) => r.readWindow())),
      };
      try {
        if (cam) await api(`/api/cameras/${encodeURIComponent(cam.id)}`, { method: 'PUT', body });
        else await api('/api/cameras', { method: 'POST', body });
        show();
        await refresh();
      } catch (err) {
        showError(panel, err);
      }
    }
  }

  function windowRow(w) {
    const boxes = DAY_NAMES.map((d, i) => {
      const box = h('input', { type: 'checkbox' });
      box.checked = w.days.includes(i + 1);
      return { box, label: h('label', { class: 'day' }, box, d) };
    });
    const start = h('input', { type: 'time', value: w.start, required: true });
    const end = h('input', { type: 'time', value: w.end, required: true });
    const row = h('div', { class: 'window' }, boxes.map((b) => b.label), start, '–', end,
      h('button', { type: 'button', class: 'danger', title: 'Remove window', onclick: () => row.remove() }, '✕'));
    row.readWindow = () => ({
      days: boxes.flatMap((b, i) => (b.box.checked ? [i + 1] : [])),
      start: start.value,
      end: end.value,
    });
    return row;
  }

  async function scan() {
    const box = h('div', { class: 'card' }, h('p', {}, 'Looking for ONVIF cameras on this network…'));
    show(box);
    let devs;
    try {
      devs = await api('/api/onvif/discover', { method: 'POST' });
    } catch (err) {
      box.replaceChildren(h('p', { class: 'error' }, err.message));
      return;
    }
    if (devs.length === 0) {
      box.replaceChildren(h('p', {}, 'No ONVIF camera answered. You can still add one by its RTSP URL.'));
      return;
    }
    box.replaceChildren(h('h3', {}, 'Cameras found'), ...devs.map((d) => h('div', { class: 'found' },
      h('span', {}, `${d.name || 'Camera'} · ${d.ip}${d.hardware ? ` · ${d.hardware}` : ''}`),
      h('button', { onclick: () => connect(d) }, 'Use this camera'))));
  }

  function connect(dev) {
    const user = h('input', { placeholder: 'admin', autocomplete: 'off' });
    const pass = h('input', { type: 'password', autocomplete: 'off' });
    const msg = h('p', { class: 'muted' }, 'Leave both empty if the camera has no password.');
    show(h('form', {
      class: 'card form',
      onsubmit: async (e) => {
        e.preventDefault();
        msg.className = 'muted';
        msg.textContent = 'Asking the camera for its streams…';
        try {
          const r = await api('/api/onvif/streams', { method: 'POST', body: { xaddr: dev.xaddr, user: user.value, pass: pass.value } });
          edit(null, { name: dev.name || `Camera ${dev.ip}`, mainUrl: r.mainUrl, subUrl: r.subUrl });
        } catch (err) {
          msg.className = 'error';
          msg.textContent = err.message;
        }
      },
    }, h('h3', {}, `Connect to ${dev.ip}`), h('label', {}, 'User name', user), h('label', {}, 'Password', pass), msg,
    h('div', { class: 'actions' },
      h('button', { class: 'primary', type: 'submit' }, 'Get streams'),
      h('button', { type: 'button', onclick: () => show() }, 'Cancel'))));
    user.focus();
  }

  await refresh();
  return () => closeForm();
}

// maskEditor paints the 16×9 blocks motion detection ignores, over the camera's live substream
// (the picture detection looks at). A drag marks blocks; a drag that starts on a marked block
// unmarks. Works with a mouse and with touch.
function maskEditor(cam, ignore) {
  const cells = expandMask(ignore);
  const video = h('video', { muted: true, autoplay: true, playsinline: true });
  video.muted = true;
  const grid = h('div', { class: 'mask-grid' }, cells.map((on) => h('div', { class: on ? 'cell on' : 'cell' })));
  let painting = null; // the value being painted while a pointer is down
  const paint = (e) => {
    const r = grid.getBoundingClientRect();
    const i = cellAt(e.clientX - r.left, e.clientY - r.top, r.width, r.height);
    if (i < 0) return;
    if (painting === null) painting = !cells[i];
    cells[i] = painting;
    grid.children[i].classList.toggle('on', painting);
  };
  grid.addEventListener('pointerdown', (e) => {
    e.preventDefault();
    grid.setPointerCapture(e.pointerId);
    painting = null;
    paint(e);
  });
  grid.addEventListener('pointermove', (e) => { if (painting !== null) paint(e); });
  const stop = () => { painting = null; };
  grid.addEventListener('pointerup', stop);
  grid.addEventListener('pointercancel', stop);
  const player = playHLS(video, `/live/hls/${streamPaths(cam).tile}/index.m3u8`);
  return { el: h('div', { class: 'mask' }, video, grid), read: () => compactMask(cells), close: () => player.close() };
}
