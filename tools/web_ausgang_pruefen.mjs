// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

// Prüft den Stand-Ausgang der Web-App (api.js) gegen einen nachgebauten
// Server — ohne Browser:
//
//     node tools/web_ausgang_pruefen.mjs
//
// Ein nachgereichter Stand darf nicht überschreiben, was ein anderes Gerät
// inzwischen gemeldet hat, nicht nach „Verlauf löschen" zurückkommen, nach
// sechs Stunden verfallen und nach einem Haken von Hand nicht mehr gelten.

const speicher = new Map();
globalThis.localStorage = { getItem: k => speicher.has(k) ? speicher.get(k) : null, setItem: (k, v) => speicher.set(k, String(v)), removeItem: k => speicher.delete(k) };
Object.defineProperty(globalThis, 'navigator', { value: { onLine: true, language: 'de-CH', languages: ['de-CH'], userAgent: 'node' }, configurable: true });
globalThis.window = globalThis; globalThis.document = { documentElement: { lang: 'de' }, addEventListener() {}, querySelector() { return null; } };
globalThis.addEventListener = () => {};
// Server-Attrappe: Marker je Titel, Stand je Titel
const server = { stand: {}, pos: {}, netz: true, log: [] };
let n = 0;
globalThis.fetch = async (url, opt = {}) => {
  if (!server.netz) throw new TypeError('offline');
  const u = new URL(url, 'http://x'); const body = opt.body ? JSON.parse(opt.body) : {};
  server.log.push([opt.method, u.pathname + u.search, body]);
  const m = u.pathname.match(/items\/([^/]+)\/(progress|finished)/);
  const h = new Headers();
  if (u.pathname === '/api/v1/me/progress' && opt.method === 'DELETE') { server.stand = {}; server.pos = {}; return new Response(null, { status: 204 }); }
  if (m) {
    const id = m[1]; const basis = m[2] === 'progress' ? body.basis : u.searchParams.get('basis');
    if (basis !== undefined && basis !== null && (server.stand[id] ?? '') !== basis) { h.set('X-Lumen-Stand', server.stand[id] ?? ''); h.set('X-Lumen-Veraltet', '1'); return new Response(null, { status: 204, headers: h }); }
    server.pos[id] = m[2] === 'progress' ? body.positionMs : 'fertig'; server.stand[id] = 's' + (++n);
    h.set('X-Lumen-Stand', server.stand[id]); return new Response(null, { status: 204, headers: h });
  }
  return new Response('{}', { status: 200, headers: { 'Content-Type': 'application/json' } });
};
const ziel = process.argv[2] || new URL("../server/cmd/lumen/web/api.js", import.meta.url).href;
const { api } = await import(ziel);
api.token = 'T1';
let fehler = 0; const pruefe = (n, b) => { if (!b) { fehler++; console.log('✗', n); } else console.log('✓', n); };
const realNow = Date.now; let jetzt = realNow(); Date.now = () => jetzt;

await api.standMelden('film', 1000, 9000);          // online, Marker s1
pruefe('online gemeldet', server.pos.film === 1000);
server.netz = false;
await api.standMelden('film', 2000, 9000);          // offline -> Ausgang mit basis s1
jetzt += 60_000;
// ein anderes Gerät schaut weiter
server.stand.film = 'anders'; server.pos.film = 5000;
server.netz = true;
await api.standNachreichen();
pruefe('veralteter Stand überschreibt nicht', server.pos.film === 5000);
pruefe('Ausgang leer', !localStorage.getItem('lumen.ausgang'));

// Verlauf löschen: Ausgang weg
server.netz = false;
await api.standMelden('serie1', 3000, 9000);
server.netz = true;
await api.clearHistory();
await api.standNachreichen();
pruefe('nach Verlauf löschen nichts zurück', server.pos.serie1 === undefined);

// zu alt (über 6 h): verfällt
server.netz = false;
await api.standMelden('alt', 4000, 9000);
jetzt += 7 * 3600 * 1000;
server.netz = true;
await api.standNachreichen();
pruefe('nach 6 h verfallen', server.pos.alt === undefined);

// frisch nachgereicht ohne Konflikt: gilt
await api.standMelden('neu', 100, 9000);
server.netz = false; await api.standMelden('neu', 200, 9000); jetzt += 60_000; server.netz = true;
await api.standNachreichen();
pruefe('passender Marker: gilt', server.pos.neu === 200);
// markFinished räumt den Ausgang
server.netz = false; await api.standMelden('neu', 300, 9000); server.netz = true;
await api.markFinished('neu').catch(e => console.log(e));
pruefe('Haken räumt den Ausgang', !JSON.stringify(JSON.parse(localStorage.getItem('lumen.ausgang') || '{}')).includes('"neu"'));
process.exit(fehler ? 1 : 0);
