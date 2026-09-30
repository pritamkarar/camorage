import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

// The colour tokens in app.css: the first :root block is light, the one inside the
// prefers-color-scheme: dark query is dark.
const css = readFileSync(new URL('./app.css', import.meta.url), 'utf8');
const block = (from) => css.slice(from, css.indexOf('}', from));
const hexes = (text) => Object.fromEntries([...text.matchAll(/--([\w-]+):\s*(#[0-9a-f]{6})\b/gi)].map((m) => [m[1], m[2]]));
const light = hexes(block(css.indexOf(':root {')));
const dark = hexes(block(css.indexOf(':root {', css.indexOf('prefers-color-scheme: dark'))));

// ratio is the WCAG contrast ratio of two #rrggbb colours.
function ratio(a, b) {
  const lum = (hex) => {
    const [r, g, bl] = [1, 3, 5].map((i) => parseInt(hex.slice(i, i + 2), 16) / 255)
      .map((v) => (v <= 0.03928 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4));
    return 0.2126 * r + 0.7152 * g + 0.0722 * bl;
  };
  const [hi, lo] = [lum(a), lum(b)].sort((x, y) => y - x);
  return (hi + 0.05) / (lo + 0.05);
}

test('text and muted text meet WCAG AA (4.5:1) on the page, cards and sidebar, light and dark', () => {
  for (const [mode, t] of [['light', light], ['dark', dark]]) {
    for (const fg of ['fg', 'muted']) {
      for (const bg of ['bg', 'surface', 'sidebar']) {
        const r = ratio(t[fg], t[bg]);
        assert.ok(r >= 4.5, `${mode}: --${fg} ${t[fg]} on --${bg} ${t[bg]} is ${r.toFixed(2)}:1`);
      }
    }
  }
});
