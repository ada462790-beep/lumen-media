// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

/* Sprache und Region der Oberfläche.
 *
 * tr('Weiterschauen') gibt den Text in der wirksamen Sprache. Der
 * Schlüssel ist der deutsche Text in Schweizer Schreibweise, so wie er
 * vorher im Code stand: Man liest an der Stelle, was gemeint ist, und ein
 * Text ohne Übersetzung erscheint als verständlicher deutscher Satz statt
 * als Kennung. Werte kommen über Platzhalter: tr('{n} Treffer', { n }).
 * Steht im Katalog für einen Schlüssel ein Objekt mit Formen
 * ({ one, other }), wählt Intl.PluralRules nach einer Zahl: nach dem Wert,
 * den „zahl" nennt ({ zahl: 'folgen', one, other }), sonst nach n, sonst
 * nach dem einzigen Zahlenwert unter den Platzhaltern – so braucht
 * tr('{gesamt} Titel', { gesamt }) keinen künstlichen Namen n.
 *
 * Wo ein deutsches Wort zwei Bedeutungen hat, steht davor ein Zusammenhang:
 * tr('Musik|{n} Titel') ist „{n} songs", tr('{n} Titel') „{n} titles". Ohne
 * Eintrag im Katalog fällt der Zusammenhang samt „|" weg.
 *
 * Warum „tr" und nicht „t": In Lumen heissen viele kurze Variablen t
 * (Spur, Zeit, Teil). Eine Übersetzung in einem solchen Block riefe sonst
 * stillschweigend die Variable auf. tr ist frei – und Qt nennt es auch so.
 * t gibt es als zweiten Namen für die Schnittstelle nach aussen.
 *
 * Kataloge (sprachen/<id>.json) erben: de-DE nimmt alles aus de-CH und
 * überschreibt nur, was in Deutschland anders geschrieben wird; de-AT
 * erbt von de-DE, en-GB von en-US. de-CH selbst enthält nur Mehrzahlen –
 * die übrigen Texte sind ihre eigenen Schlüssel. Eine weitere Sprache ist
 * eine neue Datei und ein Eintrag in SPRACHEN.
 *
 * Geladen wird mit top-level await, bevor irgendein Modul seinen ersten
 * Text braucht: zuerst die Sprache, die dieses Gerät zuletzt hatte. app.js
 * wechselt nach /hello und /me, wenn sie nicht stimmt (sprachWechseln), und
 * zeichnet neu – ohne die Seite neu zu laden.
 */

// Die Auswahl, je in ihrer eigenen Sprache (wie /hello sie liefert).
export const SPRACHEN = [
  { id: 'de-CH', name: 'Deutsch (Schweiz)' },
  { id: 'de-DE', name: 'Deutsch (Deutschland)' },
  { id: 'de-AT', name: 'Deutsch (Österreich)' },
  { id: 'en-US', name: 'English (United States)' },
  { id: 'en-GB', name: 'English (United Kingdom)' },
];
export const VORGABE = 'de-CH';
const ERBT = { 'de-DE': 'de-CH', 'de-AT': 'de-DE', 'en-GB': 'en-US' };
const MERKER = 'lumen.sprache';

let aktiv = VORGABE;
let kette = [];            // Kataloge in Suchreihenfolge, eigener zuerst
let mehrzahl = new Intl.PluralRules(VORGABE);
const geladen = new Map(); // id → Katalog
const beobachter = new Set();

export const gueltig = id => SPRACHEN.some(s => s.id === id);

// Aus einer Angabe wie „de", „de-LI", „en-AU" eine der fünf – wie
// sprache.Normalisieren im Server.
export function normalisieren(s) {
  if (!s) return '';
  const [haupt, region] = String(s).replace('_', '-').split('-');
  const id = `${(haupt || '').toLowerCase()}-${(region || '').toUpperCase()}`;
  if (gueltig(id)) return id;
  if (id === 'de-LI') return 'de-CH';
  if (['en-IE', 'en-AU', 'en-NZ', 'en-ZA', 'en-IN'].includes(id)) return 'en-GB';
  if (haupt?.toLowerCase() === 'de') return 'de-DE';
  if (haupt?.toLowerCase() === 'en') return 'en-US';
  return '';
}

// Was der Browser sich wünscht – für die Vorauswahl bei der Einrichtung.
export function browserSprache() {
  for (const l of navigator.languages || [navigator.language]) {
    const id = normalisieren(l);
    if (id) return id;
  }
  return VORGABE;
}

export function gemerkt() {
  try { return normalisieren(localStorage.getItem(MERKER)); } catch { return ''; }
}

async function katalog(id) {
  if (geladen.has(id)) return geladen.get(id);
  let k = {};
  try {
    const r = await fetch(`sprachen/${id}.json`, { cache: 'no-cache' });
    if (r.ok) k = await r.json();
  } catch { /* ohne Katalog bleibt der deutsche Text */ }
  geladen.set(id, k);
  return k;
}

/* Wechselt die Sprache: lädt die Kette der Kataloge, merkt sich die Wahl
   auf diesem Gerät und sagt allen Beobachtern Bescheid (app.js zeichnet
   dann neu). merken = false: nur anzeigen, nicht als „zuletzt genutzt"
   festhalten – so beim allerersten Laden, wenn noch niemand weiss, welche
   Sprache der Server spricht. */
export async function sprachWechseln(id, merken = true) {
  id = gueltig(id) ? id : VORGABE;
  const ids = [];
  for (let x = id; x; x = ERBT[x]) ids.push(x);
  if (!ids.includes(VORGABE) && id.startsWith('de')) ids.push(VORGABE);
  // de-CH gehört immer ans Ende: Dort stehen die Mehrzahlen der Schlüssel.
  if (!ids.includes(VORGABE)) ids.push(VORGABE);
  const neu = await Promise.all(ids.map(katalog));
  const geaendert = id !== aktiv;
  aktiv = id;
  kette = neu;
  mehrzahl = new Intl.PluralRules(id);
  document.documentElement.lang = id;
  if (merken) try { localStorage.setItem(MERKER, id); } catch {}
  if (geaendert) for (const f of beobachter) { try { f(id); } catch {} }
  return id;
}

export const sprache = () => aktiv;
export const beiWechsel = f => { beobachter.add(f); return () => beobachter.delete(f); };

/* Der Text zu schluessel in der wirksamen Sprache; {name} wird aus werte
   ersetzt (einmal, von links – ein Wert mit „{x}" darin bleibt, wie er
   ist). */
export function tr(schluessel, werte) {
  let text = schluessel.replace(/^[\w-]+\|/, '');
  for (const k of kette) {
    const e = k[schluessel];
    if (e == null) continue;
    if (typeof e === 'object') {
      const n = zaehlwert(e, werte);
      text = e[mehrzahl.select(Number.isFinite(n) ? n : 0)] ?? e.other ?? schluessel;
    } else text = e;
    break;
  }
  text = text.replace(/^[\w-]+\|/, '');
  if (!werte) return text;
  return text.replace(/\{(\w+)\}/g, (ganz, k) => (k in werte ? String(werte[k] ?? '') : ganz));
}
export { tr as t };

// Nach welcher Zahl sich die Mehrzahl richtet (siehe oben).
function zaehlwert(e, werte) {
  if (!werte) return NaN;
  if (e.zahl) return Number(werte[e.zahl]);
  if ('n' in werte) return Number(werte.n);
  const zahlen = Object.values(werte).filter(v => typeof v === 'number' && Number.isFinite(v));
  return zahlen.length === 1 ? zahlen[0] : NaN;
}

/* Texttabellen: texte({ a: 'Deutsch', … }).a übersetzt beim Lesen. So
   bleiben Tabellen auf Modulebene richtig, auch wenn die Sprache später
   wechselt. Das Prüfskript liest die Werte darin als Schlüssel. */
export const texte = quelle => new Proxy(quelle, {
  get: (o, k) => (typeof o[k] === 'string' ? tr(o[k]) : o[k]),
});

/* ---------------------------------------------------------------- Formate
   Zahlen, Daten und Uhrzeiten immer über Intl in der wirksamen Sprache –
   de-CH schreibt 1’234, de-DE 1.234, en-US 1,234; de-AT sagt „Jänner". */

export const zahl = (n, opt) => Number(n || 0).toLocaleString(aktiv, opt);
export const datum = (d, opt) => new Date(d).toLocaleDateString(aktiv, opt);
export const datumZeit = (d, opt) => new Date(d).toLocaleString(aktiv, opt);
export const uhrzeit = (d, opt) => new Date(d).toLocaleTimeString(aktiv, opt);
export const liste = (teile, art = 'conjunction') => {
  try { return new Intl.ListFormat(aktiv, { style: 'long', type: art }).format(teile); } catch { return teile.join(', '); }
};
// Name einer Sprache („de" → „Deutsch" / „German").
export const sprachName = code => {
  try { return new Intl.DisplayNames([aktiv], { type: 'language' }).of(code) || code; } catch { return code; }
};

// Vor dem ersten Zeichnen: die zuletzt auf diesem Gerät genutzte Sprache
// (oder die des Browsers). Ohne Eintrag de-CH – die Vorgabe des Servers
// kennt erst /hello, und app.js wechselt dann.
await sprachWechseln(gemerkt() || VORGABE, false);
