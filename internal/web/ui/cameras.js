import { h, api, playHLS, icon, chip, field, segmented, pageHead, emptyState, drawer, skel } from './dom.js';
import { DAY_NAMES, camStatus, cameraSummary, cellAt, cleanWindows, compactMask, expandMask, plural, streamPaths } from './lib.js';

// renderCameras lists the cameras as cards. Adding, editing, deleting and the ONVIF scan happen
// in a drawer (the app closes it when the page goes away).
export async function renderCameras(root) {
  const head = pageHead('Cameras', '',
    h('button', { onclick: () => scan() }, icon('i-scan'), 'Scan network'),
    h('button', { class: 'primary', onclick: () => edit(null) }, icon('i-plus'), 'Add camera'));
  const list = h('div', {}, skel('card'), skel('card')); // until the first refresh
  root.append(head, list);
  let targets = [];

  async function refresh() {
    const [cams, st, s] = await Promise.all([
      api('/api/cameras'),
      api('/api/status').catch(() => ({ cameras: [] })),
      api('/api/storage').catch(() => ({ targets: [] })),
    ]);
    targets = s.targets;
    const byId = new Map(st.cameras.map((c) => [c.id, c]));
    head.querySelector('.sub').textContent = plural(cams.length, 'camera');
    list.replaceChildren(...(cams.length ? cams.map((c) => card(c, byId.get(c.id)))
      : [emptyState('i-cameras', 'No cameras yet. Add one by its RTSP address, or scan the network for ONVIF cameras.')]));
  }

  function card(cam, st) {
    const s = camStatus(st, cam.enabled);
    return h('div', { class: 'panel row' },
      h('span', { class: 'cam-icon' }, icon('i-live')),
      h('div', { class: 'grow' },
        h('div', { class: 'row wrap tight' }, h('h3', {}, cam.name), chip(s.text, s.kind), cam.mode === 'motion' ? chip('Motion only', 'warn') : null),
        h('p', { class: 'muted small' }, cameraSummary(cam, targets))),
      h('button', { onclick: () => edit(cam), 'aria-label': `Edit ${cam.name}` }, 'Edit'));
  }

  async function remove(cam, d) {
    if (!confirm(`Delete camera "${cam.name}"? Its recordings stay until they age out.`)) return;
    try {
      await api(`/api/cameras/${encodeURIComponent(cam.id)}`, { method: 'DELETE' });
      d.close();
      await refresh();
    } catch (err) {
      d.say(err.message, 'error');
    }
  }

  // edit shows the camera form: cam is null for a new camera; prefill seeds a new camera's fields;
  // reuse is the scan's drawer, which the form replaces.
  function edit(cam, prefill = {}, reuse = null) {
    const title = cam ? `Edit ${cam.name}` : 'Add camera';
    const d = reuse || drawer(title);
    if (reuse) d.reset(title);
    const base = cam || { name: '', enabled: true, mainUrl: '', subUrl: '', localDays: 1, schedule: [], mode: 'continuous', motion: {}, ...prefill };
    const m = base.motion || {};
    const name = h('input', { value: base.name, required: true, maxlength: 60 });
    const enabled = h('input', { type: 'checkbox', class: 'switch', role: 'switch' });
    enabled.checked = base.enabled;
    const mainUrl = h('input', { value: base.mainUrl, required: true, placeholder: 'rtsp://user:pass@192.168.1.10/stream' });
    const subUrl = h('input', { value: base.subUrl, placeholder: 'optional low-resolution stream' });
    const localDays = h('input', { type: 'number', min: 1, max: 365, value: base.localDays });
    const rows = h('div');
    const addRow = (w = { days: [1, 2, 3, 4, 5, 6, 7], start: '00:00', end: '00:00' }) => rows.append(windowRow(w));
    (base.schedule || []).forEach((w) => addRow(w));

    const mode = segmented('Recording mode', [['continuous', 'Continuous'], ['motion', 'Motion only']],
      base.mode === 'motion' ? 'motion' : 'continuous', () => syncMode());
    const sens = h('select', {}, h('option', { value: 'low' }, 'Low'), h('option', { value: 'medium' }, 'Medium'), h('option', { value: 'high' }, 'High'));
    sens.value = m.sensitivity || 'medium';
    const pre = h('input', { type: 'number', min: 1, max: 120, value: m.preRollSec || 10 });
    const post = h('input', { type: 'number', min: 1, max: 600, value: m.postRollSec || 30 });
    const maskSlot = h('div');
    let mask = null;
    const motionBox = h('div', {},
      h('h2', { class: 'section' }, 'Motion'),
      h('p', { class: 'hint' }, 'Footage with no motion is deleted after 1 hour. Recordings made before switching to motion mode are kept.'),
      field('Sensitivity', sens),
      h('div', { class: 'inline-fields' },
        field('Seconds to keep before motion', pre),
        field('Seconds to keep after motion', post)),
      h('p', { class: 'hint' }, 'Ignore areas: drag across the picture to mark places where movement should not count (trees, a busy road, the clock the camera prints). Drag from a marked block to unmark.'),
      maskSlot);
    function syncMode() {
      motionBox.hidden = mode.read() !== 'motion';
      if (motionBox.hidden || mask) return;
      if (!cam) {
        maskSlot.replaceChildren(h('p', { class: 'hint' }, 'Save the camera first, then edit it to mark ignore areas.'));
        return;
      }
      mask = maskEditor(cam, m.ignore);
      maskSlot.replaceChildren(mask.el);
    }

    const cloudTarget = h('select', {}, h('option', { value: '' }, 'No cloud copy'), targets.map((t) => h('option', { value: t.id }, t.name)));
    cloudTarget.value = base.cloud ? base.cloud.targetId : '';
    const cloudDays = h('input', { type: 'number', min: 1, max: 3650, value: (base.cloud && base.cloud.days) || 30 });

    d.body.append(
      h('h2', { class: 'section' }, 'Camera'),
      field('Name', name),
      h('label', { class: 'check' }, enabled, 'Enabled'),
      field('Main stream (RTSP URL)', mainUrl),
      field('Substream (RTSP URL)', subUrl, 'Optional: a low-resolution stream for the Live grid and motion detection.'),
      h('h2', { class: 'section' }, 'Recording'),
      mode.el,
      field('Days to keep on the phone', localDays),
      motionBox,
      h('h2', { class: 'section' }, 'Schedule'),
      h('p', { class: 'hint' }, 'No windows means record all the time. A window ending at or before its start runs past midnight.'),
      rows,
      h('button', { type: 'button', onclick: () => addRow() }, icon('i-plus'), 'Add window'),
      h('h2', { class: 'section' }, 'Cloud copy'),
      h('p', { class: 'hint' }, targets.length
        ? 'Copies what this camera records from now on: hour by hour, or event by event in motion mode.'
        : 'Add cloud storage on the Storage page first.'),
      field('Copy to', cloudTarget),
      field('Days to keep in the cloud', cloudDays));
    d.footer('Save', ...(cam ? [h('button', { type: 'button', class: 'danger', onclick: () => remove(cam, d) }, 'Delete')] : []));
    d.form.onsubmit = () => save();
    d.onClose(() => { if (mask) mask.close(); });
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
        mode: mode.read(),
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
      d.say('Saving…');
      try {
        if (cam) await api(`/api/cameras/${encodeURIComponent(cam.id)}`, { method: 'PUT', body });
        else await api('/api/cameras', { method: 'POST', body });
        d.close();
        await refresh();
      } catch (err) {
        d.say(err.message, 'error');
      }
    }
  }

  function windowRow(w) {
    const boxes = DAY_NAMES.map((dn, i) => {
      const box = h('input', { type: 'checkbox' });
      box.checked = w.days.includes(i + 1);
      return { box, label: h('label', { class: 'day' }, box, dn) };
    });
    const start = h('input', { type: 'time', value: w.start, required: true, 'aria-label': 'From' });
    const end = h('input', { type: 'time', value: w.end, required: true, 'aria-label': 'Until' });
    const row = h('div', { class: 'window' }, boxes.map((b) => b.label), start, '–', end,
      h('button', { type: 'button', class: 'icon-btn danger', 'aria-label': 'Remove window', onclick: () => row.remove() }, icon('i-close')));
    row.readWindow = () => ({
      days: boxes.flatMap((b, i) => (b.box.checked ? [i + 1] : [])),
      start: start.value,
      end: end.value,
    });
    return row;
  }

  async function scan() {
    const d = drawer('Scan network');
    d.footer(null);
    d.say('Looking for ONVIF cameras on this network…');
    let devs;
    try {
      devs = await api('/api/onvif/discover', { method: 'POST' });
    } catch (err) {
      d.say(err.message, 'error');
      return;
    }
    if (devs.length === 0) {
      d.say('No ONVIF camera answered. You can still add one by its RTSP URL.');
      return;
    }
    d.say(`${plural(devs.length, 'camera')} found.`);
    d.body.append(...devs.map((dev) => h('div', { class: 'found' },
      h('span', {}, h('strong', {}, dev.name || 'Camera'), h('span', { class: 'muted' }, ` · ${dev.ip}${dev.hardware ? ` · ${dev.hardware}` : ''}`)),
      h('button', { type: 'button', onclick: () => connect(dev, d) }, 'Use this camera'))));
  }

  function connect(dev, d) {
    d.reset(`Connect to ${dev.ip}`);
    const user = h('input', { placeholder: 'admin', autocomplete: 'off' });
    const pass = h('input', { type: 'password', autocomplete: 'off' });
    d.body.append(h('p', { class: 'hint' }, 'Leave both empty if the camera has no password.'), field('User name', user), field('Password', pass));
    d.footer('Get streams');
    d.form.onsubmit = async () => {
      d.say('Asking the camera for its streams…');
      try {
        const r = await api('/api/onvif/streams', { method: 'POST', body: { xaddr: dev.xaddr, user: user.value, pass: pass.value } });
        edit(null, { name: dev.name || `Camera ${dev.ip}`, mainUrl: r.mainUrl, subUrl: r.subUrl }, d);
      } catch (err) {
        d.say(err.message, 'error');
      }
    };
    user.focus();
  }

  await refresh();
  return () => {};
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
