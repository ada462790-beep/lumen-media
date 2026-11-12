// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

/* Der Service Worker: nur die Hülle der App, sonst nichts.
 *
 * Wer Lumen unterwegs ohne Netz öffnet (oder als App vom Startbildschirm),
 * soll Lumen sehen — mit „Du bist offline" — und nicht die Fehlerseite des
 * Browsers. Dafür liegen index.html, die Skripte, die Gestaltung und die
 * Symbole hier. Was persönlich ist oder gross, bleibt draussen: keine
 * Antworten der Schnittstelle, keine Bilder, keine Filme.
 *
 * Updates: Fassung und Dateiliste setzt der Server beim Ausliefern ein
 * (web.go). Kommt Lumen neu, ändert sich diese Datei, der Browser
 * installiert sie neu, und der alte Vorrat wird weggeräumt. Seiten holt
 * der Worker immer zuerst aus dem Netz; nur wenn das nicht geht, nimmt er
 * die aufbewahrte Hülle. So bleibt niemand auf einer alten Fassung sitzen.
 */

const FASSUNG = '__LUMEN_FASSUNG__';
const DATEIEN = "__LUMEN_DATEIEN__";
// Ohne eingesetzte Fassung (beim Entwickeln mit --web) tut der Worker nichts.
const AN = /^[0-9a-f]{12}$/.test(FASSUNG) && Array.isArray(DATEIEN);
const VORRAT = 'lumen-huelle-' + FASSUNG;
// So lange darf das Netz für eine Seite brauchen, bevor die Hülle kommt.
const SEITE_FRIST = 8000;

self.addEventListener('install', e => {
  if (!AN) { self.skipWaiting(); return; }
  e.waitUntil((async () => {
    const c = await caches.open(VORRAT);
    // Einzeln, damit eine fehlende Datei nicht alles verhindert. Was der
    // Browser schon hat (die Seite hat sie eben geladen), kommt aus seinem
    // Zwischenspeicher — nichts wird doppelt übers Netz geholt.
    await Promise.all(['/', ...DATEIEN].map(u => c.add(new Request(u, { cache: 'default' })).catch(() => {})));
    self.skipWaiting();
  })());
});

self.addEventListener('activate', e => {
  e.waitUntil((async () => {
    for (const k of await caches.keys()) {
      if (k.startsWith('lumen-huelle-') && k !== VORRAT) await caches.delete(k);
    }
    await self.clients.claim();
  })());
});

self.addEventListener('fetch', e => {
  if (!AN) return;
  const req = e.request;
  if (req.method !== 'GET') return;
  const url = new URL(req.url);
  if (url.origin !== self.location.origin) return;
  // Schnittstelle, Bilder, Filme: nie anfassen.
  if (url.pathname.startsWith('/api/')) return;

  if (req.mode === 'navigate') {
    e.respondWith(seite(req));
    return;
  }
  // Dateien mit der richtigen Fassung ändern sich nie: erst im Vorrat
  // nachsehen. Sprachkataloge kommen dazu, sobald sie einmal geladen sind.
  if (url.searchParams.get('v') === FASSUNG) {
    e.respondWith((async () => {
      const c = await caches.open(VORRAT);
      const da = await c.match(req);
      if (da) return da;
      const res = await fetch(req);
      if (res.ok && url.pathname.startsWith('/sprachen/')) c.put(req, res.clone()).catch(() => {});
      return res;
    })());
  }
});

// Eine Seite: zuerst das Netz (neue Fassungen kommen sofort an), sonst die
// aufbewahrte Hülle. Die App merkt dann selbst, dass sie offline ist.
async function seite(req) {
  const ctrl = new AbortController();
  const t = setTimeout(() => ctrl.abort(), SEITE_FRIST);
  try {
    return await fetch(req, { signal: ctrl.signal });
  } catch (err) {
    const c = await caches.open(VORRAT);
    const huelle = await c.match('/');
    if (huelle) return huelle;
    throw err;
  } finally {
    clearTimeout(t);
  }
}
