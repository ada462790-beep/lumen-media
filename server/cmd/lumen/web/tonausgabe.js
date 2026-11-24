// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

/* „Tonausgabe": über welches Gerät der Player spielt — nur in Lumen für
 * Windows (desktop/windows).
 *
 * Filme spielt dort inzwischen meist der eigene Player (mpv,
 * desktopplayer.js) — mit eigener Geräteliste. Diese Wahl gilt für das, was
 * die Seite selbst abspielt: Trailer, Extras und Filme, wenn libmpv fehlt.
 *
 * Warum es das gibt: Programme wie Logi MIXLINE verteilen den Ton nach
 * Programm. Den Ton einer WebView2-App spielt aber nicht die App selbst,
 * sondern msedgewebview2.exe — er kommt bei solchen Mischern oft nicht
 * an. Wählt man das Gerät hier ausdrücklich (etwa „MIXLINE" oder den
 * Kopfhörer), öffnet der Player genau dieses Gerät (setSinkId) und hängt
 * nicht mehr an der Zuordnung „Standardgerät pro Programm".
 *
 * Im Browser gibt es die Wahl nicht: Firefox und Safari kennen setSinkId
 * nicht, und Chrome nennt die Geräte erst, wenn die Seite das Mikrofon
 * benutzen darf — eine Mikrofon-Frage, um den Lautsprecher zu wählen,
 * wäre verwirrend. Die App erteilt diese Erlaubnis selbst, still und nur
 * für den eigenen Server (lumenDesktop.tonausgabe.freigeben); ein
 * Mikrofon wird dabei nie geöffnet.
 *
 * Die Wahl gilt für dieses Gerät (localStorage), nicht fürs Profil: Am
 * Fernseher gibt es den Kopfhörer vom PC nicht. Gemerkt werden Kennung und
 * Name. Die Kennung bleibt für diesen Server gleich; fehlt das Gerät
 * (abgesteckt, umbenannt), sucht der Player es am Namen und spielt sonst
 * über den Systemstandard — mit einem kurzen Hinweis. */

const SCHLUESSEL = 'lumen.tonausgabe';

const desktop = () => typeof window.lumenDesktop?.tonausgabe?.freigeben === 'function';
const sinkDa = () => typeof HTMLMediaElement !== 'undefined' && 'setSinkId' in HTMLMediaElement.prototype;
const geraeteDa = () => typeof navigator.mediaDevices?.enumerateDevices === 'function';

/* Die Einstellung erscheint nur in der Windows-App. */
export const tonausgabeAngeboten = () => desktop();

/* Ob sie dort auch wirkt. Über http (Lumen im Heimnetz) braucht die App
 * dafür einen Neustart, nachdem der Server dazugekommen ist: Erst dann
 * gilt seine Adresse als sicher, und erst dann gibt es setSinkId und die
 * Geräteliste. */
export function tonausgabeStand() {
  if (!desktop()) return 'nein';
  if (!window.isSecureContext || !geraeteDa()) return 'neustart';
  if (!sinkDa()) return 'nein';
  return 'ja';
}

export function tonausgabeGewaehlt() {
  try {
    const w = JSON.parse(localStorage.getItem(SCHLUESSEL) || 'null');
    return w && typeof w.id === 'string' && w.id ? { id: w.id, name: String(w.name || '') } : null;
  } catch { return null; }
}

function merken(w) {
  try {
    if (w) localStorage.setItem(SCHLUESSEL, JSON.stringify({ id: w.id, name: w.name || '' }));
    else localStorage.removeItem(SCHLUESSEL);
  } catch {}
}

/* Die Geräte, die zuletzt gefunden wurden: [{ id, name }]. */
export const tonGeraete = { liste: [], geladen: false };
let laden = null;

/* Geräte holen. Die App erlaubt vorher (still) die Geräteliste für
 * diesen Server; ohne das gäbe es nur „Standard" ohne Namen. Ist kein
 * Gerät gewählt, nimmt sie die Erlaubnis danach gleich wieder zurück: Sie
 * bleibt nur, solange der Player sie für setSinkId braucht. */
export function tonausgabeLaden(neu = false) {
  if (tonausgabeStand() !== 'ja') return Promise.resolve(tonGeraete);
  if (laden && !neu) return laden;
  laden = (async () => {
    try { await window.lumenDesktop.tonausgabe.freigeben(); } catch { /* dann eben ohne Namen */ }
    let alle = [];
    try { alle = await navigator.mediaDevices.enumerateDevices(); } catch { alle = []; }
    // „default" und „communications" sind keine eigenen Geräte, sondern
    // Verweise auf das, was Windows gerade als Standard führt.
    const aus = alle.filter(d => d.kind === 'audiooutput' && d.deviceId && d.deviceId !== 'default' && d.deviceId !== 'communications');
    tonGeraete.liste = aus.map((d, i) => ({ id: d.deviceId, name: d.label || `#${i + 1}` }));
    tonGeraete.geladen = true;
    if (!tonausgabeGewaehlt()) await zuruecknehmen();
    return tonGeraete;
  })();
  return laden;
}

if (desktop() && geraeteDa()) {
  // Kopfhörer eingesteckt, MIXLINE gestartet: die Liste neu holen.
  navigator.mediaDevices.addEventListener?.('devicechange', () => { if (tonGeraete.geladen) tonausgabeLaden(true); });
}

/* Wählen: id '' heisst Systemstandard. Dann braucht der Server auch die
 * Geräteliste nicht mehr — die App nimmt die Erlaubnis zurück. */
async function zuruecknehmen() {
  try { await window.lumenDesktop?.tonausgabe?.zuruecknehmen(); } catch {}
}

export async function tonausgabeWaehlen(id) {
  if (!id) {
    merken(null);
    await zuruecknehmen();
    laden = null;
    return null;
  }
  const g = tonGeraete.liste.find(x => x.id === id);
  const w = { id, name: g ? g.name : '' };
  merken(w);
  try { await window.lumenDesktop?.tonausgabe?.freigeben(); } catch {}
  return w;
}

/* Auf ein <audio>/<video> anwenden. Gibt zurück, was geschah:
 *   { ok: true }                    wie gewählt (oder Systemstandard)
 *   { ok: false, fehlt: 'Name' }    Gerät weg, es spielt der Systemstandard
 * Wirft nie. */
export async function tonausgabeAnwenden(el) {
  if (!el || typeof el.setSinkId !== 'function' || tonausgabeStand() !== 'ja') return { ok: true };
  const w = tonausgabeGewaehlt();
  const ziel = w ? w.id : '';
  if (el.sinkId === ziel) return { ok: true };
  try {
    await el.setSinkId(ziel);
    return { ok: true };
  } catch { /* gleich weiter unten */ }
  if (!w) return { ok: true };
  // Die Kennung passt nicht mehr: am Namen suchen.
  if (w.name) {
    const { liste } = await tonausgabeLaden(true);
    const g = liste.find(x => x.name === w.name && x.id !== w.id);
    if (g) {
      try { await el.setSinkId(g.id); merken({ id: g.id, name: g.name }); return { ok: true }; } catch {}
    }
  }
  try { if (el.sinkId !== '') await el.setSinkId(''); } catch {}
  return { ok: false, fehlt: w.name || '?' };
}
