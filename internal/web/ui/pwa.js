// The installable-app parts: the service worker (it shows offline.html when the phone can't be
// reached) and the browser's offer to install camorage, used by Settings → About.
import { installChoice } from './lib.js';

let offer = null;
window.addEventListener('beforeinstallprompt', (e) => {
  e.preventDefault(); // keep it for the Install app button instead of the browser's own banner
  offer = e;
});
window.addEventListener('appinstalled', () => { offer = null; });
// navigator.serviceWorker exists only on secure origins: HTTPS (Tailscale, Cloudflare) and localhost
if ('serviceWorker' in navigator) navigator.serviceWorker.register('/sw.js').catch(() => {});

// installState is installChoice for this page.
export function installState() {
  return installChoice({
    standalone: window.matchMedia('(display-mode: standalone)').matches,
    offered: offer != null,
    secure: window.isSecureContext,
  });
}

// install shows the browser's install dialog; an offer can be used once.
export async function install() {
  const o = offer;
  offer = null;
  if (o) await o.prompt();
}
