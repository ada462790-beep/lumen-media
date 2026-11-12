// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

/* Lumen — die Web-App.
 *
 * Reines ES-Modul-JavaScript, kein Bauschritt, keine Fremdbibliothek. Das
 * ist Absicht: dieser Server steht bei Leuten zu Hause, oft ohne ständige
 * Internetverbindung, und soll Jahre laufen, ohne dass jemand ein
 * Abhängigkeitsgeflecht nachpflegen muss.
 *
 * Abgespielt wird über ein gewöhnliches <video>: der Server schickt dem
 * Browser ein Format, das er von Haus aus versteht.
 */

import { api, ApiError } from './api.js';
import { T, altersOptionen, schutzText, falschMeldung, frage, schutzDialog, mitteilung,
  einladungsFormular, einladungsBox } from './schutz.js';
// Handbearbeitung, Sammlungen, Übernahme aus Emby/Jellyfin, Untertitel.
import { U as FU, funktionenKlick, detailVerwaltung, detailUntertitel, staffelBearbeiten, folgeBearbeiten,
  einstellungenUmzug, metadatenUntertitel, playerUntertitelEintrag, utStand, utStandLaden,
  sammlungenLaden, sammlungLaden, sammlungenSeite, sammlungSeite, reihenReiter } from './funktionen.js';
// Bildschirmzeit der Kinderprofile (durchgesetzt vom Server).
import { grenzenBearbeiten } from './konto.js';
import { bzKontoText, bzBearbeiten, bzLaden, bzStand, bzIchZeile, bzBeimSchauen, bzSchluss, bzBand, bzTagText } from './bildschirmzeit.js';
import { sammlungKarte } from './sammlungen.js';
import { tastaturEinrichten } from './tastatur.js';
import { wunschFeld, wunschFeldEinrichten, wunschBild, wunschZusatz, wunschVerweise } from './wuensche.js';
import { tr, texte, sprachName, sprache, datum, datumZeit, uhrzeit as uhrzeitText, zahl,
  SPRACHEN as SPRACH_WAHL, sprachWechseln, beiWechsel, gemerkt, browserSprache } from './sprache.js';

const app = document.getElementById('app');
// Besitzerrechte: der Besitzer selbst oder jemand mit „Vollzugriff".
const voll = () => !!(S.me?.account?.isOwner || S.me?.account?.vollzugriff);
const $ = sel => app.querySelector(sel);

/* Was genau gemeint ist: Mit Verwaltungssperre zählt nur deren PIN,
   Passwort oder Code — das Passwort des Profils nicht mehr. Vorher stand
   „PIN, Passwort oder Code" da, und wer beides hatte, riet. */
function bestaetigenText() {
  switch (S.me?.account?.adminLock) {
    case 'pin': return tr('Zur Sicherheit noch einmal die PIN der Verwaltungssperre – nicht die deines Profils.');
    case 'password': return tr('Zur Sicherheit noch einmal das Passwort der Verwaltungssperre – nicht das deines Profils.');
    case 'totp': return tr('Zur Sicherheit noch einmal den Code aus deiner Authenticator-App.');
    default: return S.me?.account?.lockKind === 'password'
      ? tr('Zur Sicherheit noch einmal das Passwort deines Profils.')
      : tr('Zur Sicherheit noch einmal die PIN deines Profils.');
  }
}
function bestaetigenFeld() {
  return { pin: tr('PIN der Verwaltungssperre'), password: tr('Passwort der Verwaltungssperre'), totp: tr('Code aus der Authenticator-App') }[S.me?.account?.adminLock]
    || (S.me?.account?.lockKind === 'password' ? tr('Passwort') : 'PIN');
}

/* Rechte ändern verlangt den Code noch einmal. api.js fragt
   hier nach, wenn der Server „bestaetigen" antwortet. Ein echtes <dialog>:
   Esc bricht ab, der Fokus bleibt im Feld. */
api.bestaetigen = (hinweis = '') => new Promise(fertig => {
  const d = document.createElement('dialog');
  d.className = 'lumen glass glass--thick dialog-karte'; // .lumen bringt Schrift und Farben mit
  if (document.documentElement.classList.contains('hell')) d.classList.add('light');
  d.setAttribute('aria-labelledby', 'bestaetigen-t');
  // Ein <dialog> erbt die Schrift der Seite nicht (die Browser-Vorgabe ist
  // eine Serifenschrift) und scrollt, sobald der Inhalt um ein Pixel zu
  // breit ist — beides ausdrücklich abstellen.
  // .lumen setzt position:relative — das hebt die Mitte des Dialogs auf,
  // darum Lage und Höhe hier zurückholen.
  d.style.cssText = 'position:fixed;inset:0;margin:auto;height:fit-content;max-height:calc(100vh - 32px);' +
    'border:0;border-radius:22px;padding:22px;width:min(360px,calc(100vw - 32px));' +
    'box-sizing:border-box;overflow:hidden;color:var(--label)';
  d.innerHTML = `<form method="dialog" style="display:flex;flex-direction:column;gap:12px">
    <div id="bestaetigen-t" class="t-title" style="font-size:1.1875rem;font-weight:700">${tr('Änderung bestätigen')}</div>
    <div class="t-caption c-2" id="bestaetigen-x">${hinweis ? hinweis.replace(/[<&]/g, '') : bestaetigenText()}</div>
    <input type="password" name="code" autocomplete="current-password" required aria-label="${esc(bestaetigenFeld())}" aria-describedby="bestaetigen-x"
      ${['pin', 'totp'].includes(S.me?.account?.adminLock) ? 'inputmode="numeric"' : ''}
      style="width:100%;box-sizing:border-box;height:44px;border-radius:12px;border:0;outline:none;background:var(--fill);color:var(--label);padding:0 14px;font:inherit;font-size:1rem">
    <div style="display:flex;gap:10px;justify-content:flex-end">
      <button type="button" class="btn btn-pill" value="ab">${tr('Abbrechen')}</button>
      <button class="btn btn-pill btn-pill--fill" value="ok">${tr('Bestätigen')}</button></div></form>`;
  document.body.appendChild(d);
  const feld = d.querySelector('input');
  let wert = '';
  d.querySelector('button[value="ab"]').addEventListener('click', () => d.close());
  d.querySelector('form').addEventListener('submit', () => { wert = feld.value; });
  d.addEventListener('close', () => { d.remove(); fertig(wert); });
  d.showModal();
  feld.focus();
});

/* ---------------------------------------------------------------- Zustand */

const S = {
  view: 'start',
  me: null,          // { account, limits, prefs, libraries }
  data: null,        // was die aktuelle Ansicht braucht
  id: null,
  query: '',
  filter: 'Alle',
  busy: false,
  error: '',
  playing: null,
  logFilter: 'all',
  hello: null,
  delLib: null,      // Bibliothek, die auf die Bestätigung wartet
  bearbeiten: null,  // was gerade umbenannt wird: 'reihe' | 'liste'
  warteschlange: null, // { name, ids, i } — eine Liste, die am Stück läuft
  listeWeg: false,   // Rückfrage beim Löschen einer Liste
  redeemLock: 'pin',       // wie sich ein eingeladener Gast anmelden will (PIN ist am einfachsten)
  redeemForm: {},    // was im Einladungsformular schon getippt wurde
  browse: null,      // der offene Ordnerwähler
  // Zugeklappt oder nicht. Ohne eigene Wahl entscheidet die Breite:
  // auf dem Schreibtisch offen, auf einem schmalen Gerät als
  // Symbolspalte — 220 Pixel verdeckten dort den halben Bildschirm.
  navZu: (() => {
    try {
      const gemerkt = localStorage.getItem('lumen.navZu');
      if (gemerkt !== null) return gemerkt === '1';
    } catch {}
    return window.innerWidth <= 860;
  })(),
};

/* ---------------------------------------------------------------- Hilfen */

// Auch das einfache Hochkomma: sonst bräche ein Wert in einem Attribut
// mit '…' aus, und der Rest des Texts würde zu Markup. Steuerzeichen für
// die Schreibrichtung (U+202A–202E, U+2066–2069) fallen weg: Ein Dateiname
// mit „Rechts-nach-links" täuschte sonst einen anderen Namen vor.
const esc = s => String(s ?? '').replace(/[\u202A-\u202E\u2066-\u2069]/g, '').replace(/[&<>"']/g,
  c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));

// Staffel und Folge kurz: „S1 F2" auf Deutsch, „S1 E2" auf Englisch.
const sf = (s, f) => tr('S{s} F{f}', { s, f });

const svg = (d, s = 18, w = 1.8) =>
  `<svg width="${s}" height="${s}" viewBox="0 0 24 24" fill="none" stroke="currentColor"
    stroke-width="${w}" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${d}</svg>`;

const I = {
  search: '<circle cx="11" cy="11" r="6.5"/><path d="M16 16l4 4"/>',
  home: '<path d="M4 10.5 12 4l8 6.5V19a1 1 0 0 1-1 1h-4.5v-5.5h-5V20H5a1 1 0 0 1-1-1z"/>',
  film: '<rect x="4" y="4" width="16" height="16" rx="2.5"/><path d="M8 4v16M16 4v16M4 9h4M4 15h4M16 9h4M16 15h4"/>',
  tv: '<rect x="3" y="5" width="18" height="12" rx="2.5"/><path d="M8 20h8"/>',
  mark: '<path d="M7 4h10a1 1 0 0 1 1 1v15l-6-4-6 4V5a1 1 0 0 1 1-1z"/>',
  herz: '<path d="M12 20s-7-4.4-7-10a4 4 0 0 1 7-2.6A4 4 0 0 1 19 10c0 5.6-7 10-7 10z"/>',
  wish: '<circle cx="12" cy="12" r="9"/><path d="M12 8v8M8 12h8"/>',
  gear: '<circle cx="12" cy="12" r="3"/><path d="M12 3v2.5M12 18.5V21M3 12h2.5M18.5 12H21M5.6 5.6l1.8 1.8M16.6 16.6l1.8 1.8M5.6 18.4l1.8-1.8M16.6 7.4l1.8-1.8"/>',
  plus: '<path d="M12 5v14M5 12h14"/>',
  check: '<path d="M5 12.5l4.5 4.5L19 7.5"/>',
  back: '<path d="M15 5l-7 7 7 7"/>',
  chev: '<path d="M9 5l7 7-7 7"/>',
  x: '<path d="M6 6l12 12M18 6L6 18"/>',
  back10: '<path d="M11 6l-6 6 6 6M19 6l-6 6 6 6"/>',
  fwd10: '<path d="M13 6l6 6-6 6M5 6l6 6-6 6"/>',
  lock: '<path d="M8 10V7a4 4 0 0 1 8 0v3"/><rect x="6" y="10" width="12" height="10" rx="2.5"/>',
  naechste: '<path d="M5 5l9 7-9 7z"/><path d="M18 5v14"/>',
  glocke: '<path d="M6 16V11a6 6 0 0 1 12 0v5l1.5 2h-15z"/><path d="M10 20a2 2 0 0 0 4 0"/>',
  untertitel: '<path d="M4 5h16a1 1 0 0 1 1 1v10a1 1 0 0 1-1 1h-7l-4 3v-3H4a1 1 0 0 1-1-1V6a1 1 0 0 1 1-1z"/><path d="M7 10h6M15 10h2M7 13.5h3M12 13.5h5"/>',
  ton: '<path d="M4 14v3M8 9v8M12 5v12M16 10v7M20 13v4"/>',
  reihe: '<rect x="8" y="3.5" width="12" height="16" rx="2"/><path d="M5 6.5v13a1 1 0 0 0 1 1h10"/>',
  folgen: '<path d="M4 6h16M4 12h16M4 18h10"/>',
  lautsprecher: '<path d="M4 9.5h3.5L12 5.5v13l-4.5-4H4z"/><path d="M15.5 9a4.2 4.2 0 0 1 0 6M18 6.5a7.8 7.8 0 0 1 0 11"/>',
  stumm: '<path d="M4 9.5h3.5L12 5.5v13l-4.5-4H4z"/><path d="M16 9.5l5 5M21 9.5l-5 5"/>',
  vollbild: '<path d="M4 9V4h5M20 9V4h-5M4 15v5h5M20 15v5h-5"/>',
  kapitel: '<path d="M8 6h12M8 12h12M8 18h12"/><circle cx="4" cy="6" r="1"/><circle cx="4" cy="12" r="1"/><circle cx="4" cy="18" r="1"/>',
  vollbildAus: '<path d="M9 4v5H4M15 4v5h5M9 20v-5H4M15 20v-5h5"/>',
  vonVorn: '<path d="M4 12a8 8 0 1 0 2.4-5.7"/><path d="M4 4v4.5h4.5"/>',
  laden: '<path d="M12 4v11M7 10.5l5 5 5-5M5 19.5h14"/>',
  bildImBild: '<rect x="3" y="5" width="18" height="14" rx="2"/><rect x="12" y="11.5" width="6.5" height="5" rx="1" fill="currentColor" stroke="none"/>',
  liste: '<path d="M9 6h11M9 12h11M9 18h7"/><circle cx="4.6" cy="6" r=".9"/><circle cx="4.6" cy="12" r=".9"/><circle cx="4.6" cy="18" r=".9"/>',
  hoch: '<path d="M6 15l6-6 6 6"/>',
  runter: '<path d="M6 9l6 6 6-6"/>',
  aufsteigend: '<path d="M12 19V5M6.5 10.5 12 5l5.5 5.5"/>',
  absteigend: '<path d="M12 5v14M6.5 13.5 12 19l5.5-5.5"/>',
  mischen: '<path d="M3.5 7H7l10 10h3.5M3.5 17H7L17 7h3.5M18 4.5 20.5 7 18 9.5M18 14.5l2.5 2.5-2.5 2.5"/>',
  trash: '<path d="M4 7h16M9 7V5a1 1 0 0 1 1-1h4a1 1 0 0 1 1 1v2M6 7l1 12a1 1 0 0 0 1 1h8a1 1 0 0 0 1-1l1-12M10 11v6M14 11v6"/>',
};
const PLAY = s => `<svg width="${s}" height="${s}" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"><path d="M7 4.8v14.4c0 .8.9 1.3 1.6.9l11.3-7.2c.6-.4.6-1.4 0-1.8L8.6 3.9C7.9 3.5 7 4 7 4.8z"/></svg>`;
const PAUSE = s => `<svg width="${s}" height="${s}" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"><rect x="6" y="4.5" width="4.2" height="15" rx="1.3"/><rect x="13.8" y="4.5" width="4.2" height="15" rx="1.3"/></svg>`;

const ADMIN_VIEWS = ['admin', 'adminBackups', 'adminUmwandlung', 'adminLibraries', 'adminWishes', 'adminUpdate', 'adminGroups', 'adminLock', 'adminUnlock', 'sessions', 'logs', 'cleanup', 'accounts', 'metadata'];

/* Meldungen stehen in einer eigenen Live-Region (index.html), nicht in
   #app: der Bildschirmleser liest sie vor, aber nicht jede neue Seite.
   Fehler sind role="alert" und bleiben länger; unter der Maus bleibt
   jede Meldung stehen, bis die Maus wieder geht. */
/* Was der Server beim Anmelden einmal sagen will (api.anmeldeHinweis) –
   bisher nur, dass die Verwaltung den PIN zurückgesetzt und dabei den
   Verlauf gelöscht hat. Der Text kommt übersetzt vom Server. */
/* Nach einem Zurücksetzen durch die Verwaltung: Mit dem Einmal-Code gibt
   es erst eine Anmeldung, wenn die Person ihre eigene PIN festlegt
   (Server-Code „neue_pin_noetig"). anmelden(neu) schickt die Anmeldung
   mit dem neuen Schutz noch einmal. */
async function eigenePinFestlegen(text, anmelden, art = 'pin') {
  const ok = await schutzDialog({
    titel: art === 'password' ? tr('Eigenes Passwort festlegen') : tr('Eigene PIN festlegen'),
    hinweis: text,
    altArt: art,
    ohneErlaubt: false,
    senden: ({ art, neu }) => anmelden({ neu, neuArt: art }),
  });
  if (!ok) { S.error = ''; return render(); }
  S.me = await api.me();
  go('home');
  return anmeldeHinweis();
}

function anmeldeHinweis() {
  const h = api.anmeldeHinweis;
  api.anmeldeHinweis = null;
  if (!h?.text) return;
  return mitteilung(tr('PIN zurückgesetzt'), h.text);
}

/* Leise Ansage für Bildschirmleser (eigene Live-Region, damit sie keine
   Meldung verdrängt). */
function ansagen(text) {
  let el = document.getElementById('ansage');
  if (!el) {
    el = document.createElement('div');
    el.id = 'ansage'; el.className = 'sr-only';
    el.setAttribute('aria-live', 'polite'); el.setAttribute('aria-atomic', 'true');
    document.body.appendChild(el);
  }
  el.textContent = '';
  setTimeout(() => { el.textContent = text; }, 60);
}
// Suchtreffer: nach einer kurzen Pause die Zahl ansagen, nicht bei jedem Zeichen.
let ansageZeit = null, letzteAnsage = '';
function trefferAnsagen() {
  clearTimeout(ansageZeit);
  const q = S.query.trim();
  if (!q || S.busy || S.error) return;
  const d = S.data || {};
  const n = (d.items || []).length + (d.folgen || []).length;
  const text = n ? tr('{n} Treffer', { n }) : tr('Keine Treffer für „{q}“', { q });
  ansageZeit = setTimeout(() => { if (text !== letzteAnsage) { letzteAnsage = text; ansagen(text); } }, 500);
}

function toast(msg) {
  const bereich = document.getElementById('meldungen') || document.body;
  bereich.querySelector('.toast')?.remove();
  const el = document.createElement('div');
  const fehler = !!msg && msg === letzterFehlerText;
  el.className = 'toast' + (fehler ? ' toast--fehler' : '');
  el.setAttribute('role', fehler ? 'alert' : 'status');
  // Eine Meldung beginnt gross – auch eine alte, kleingeschriebene vom Server.
  el.textContent = msg ? String(msg).charAt(0).toUpperCase() + String(msg).slice(1) : msg;
  bereich.appendChild(el);
  let zeit = null;
  const weg = ms => { clearTimeout(zeit); zeit = setTimeout(() => { el.classList.add('toast--weg'); setTimeout(() => el.remove(), 300); }, ms); };
  el.addEventListener('mouseenter', () => { clearTimeout(zeit); el.classList.remove('toast--weg'); });
  el.addEventListener('mouseleave', () => weg(2500));
  weg(fehler ? 8000 : 5000);
}

function mins(ms) {
  if (!ms) return '';
  const m = Math.round(ms / 60000);
  return m >= 60 ? tr('{h} Std. {min} Min.', { h: Math.floor(m / 60), min: m % 60 }) : tr('{min} Min.', { min: m });
}

const clock = sec => {
  sec = Math.max(0, Math.floor(sec));
  const h = Math.floor(sec / 3600), m = Math.floor(sec / 60) % 60, s = sec % 60;
  return h ? `${h}:${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}`
           : `${m}:${String(s).padStart(2, '0')}`;
};

/* Bild eines Titels. Gibt es keins, steht der Titel selbst da — das ist
   ehrlicher als ein graues Rechteck und hilft beim Zuordnen. */
//
// Ohne Poster nimmt Lumen ein Bild aus dem Film selbst (it.frame). Es
// liegt über dem Titel: lässt es sich nicht machen, entfernt es sich und
// der Titel bleibt stehen.
//
// kind: 'poster' für Hochformat, 'wide' für Kopf- und Folgenbilder —
// dort lieber der Hintergrund, sonst das Poster, sonst das Standbild.
function artOf(it, kind = 'poster') {
  const img = url => `<img class="art-img" src="${esc(url)}${tokenSuffix(url)}" alt="" loading="lazy">`;
  // 'quer': eine breite Karte. Das Querformat aus der Sammlung
  // („landscape.jpg", mit Schriftzug) passt dort besser als der
  // Hintergrund; ohne es gilt dasselbe wie bei 'wide'.
  if (kind === 'quer') {
    if (it.landscape) return img(it.landscape);
    kind = 'wide';
  }
  if (kind === 'wide') {
    if (it.backdrop) return img(it.backdrop);
    // Bei Folgen ist das „Poster" das Standbild der Folge (breit) — es geht
    // dem Bild aus der Datei vor.
    if (it.kind === 'episode' && it.poster) return img(it.poster);
    if (it.frame) return `<div class="art-none"></div>${standbild(it, false)}`;
    if (it.poster) return img(it.poster);
  } else if (it.poster) {
    return img(it.poster);
  }
  const titel = `<div class="art-none"><span>${esc(it.title)}</span></div>`;
  if (!it.frame) return titel;
  return titel + standbild(it, true);
}
// Der Titel steht in der Karte ohnehin darunter; auf dem Bild selbst
// stünde er doppelt. data-nochmal: Entsteht das Bild noch (der Server
// antwortet dann gleich mit 503, statt die Verbindung zu belegen), wird
// es später noch einmal geladen.
function standbild(it, hoch) {
  return `<img class="art-img art-frame${hoch ? ' art-frame--poster' : ''}"
      src="${esc(it.frame)}${tokenSuffix(it.frame)}" alt="" loading="lazy" data-weg data-nochmal="0">`;
}
// Das Jahr eines Titels — bei Serien der Zeitraum: „2014–2020" für eine
// abgeschlossene, „seit 2014" für eine, die noch läuft.
function jahre(it) {
  if (!it || !it.year) return '';
  if (it.kind !== 'series' || it.laeuftNoch == null) return String(it.year);
  if (it.laeuftNoch) return tr('seit {jahr}', { jahr: it.year });
  return it.endYear && it.endYear !== it.year ? `${it.year}–${it.endYear}` : String(it.year);
}
// Nur http(s)-Adressen als Link — eine Quelle aus der Datenbank darf kein
// „javascript:" unterschieben.
function nurHttp(raw) {
  try { const u = new URL(raw); return (u.protocol === 'https:' || u.protocol === 'http:') ? u.href : '#'; }
  catch { return '#'; }
}
// Bilder bekommen den Bildschlüssel (bleibt über ein Neuladen gleich, der
// Browser behält sie), alles andere den Medienschlüssel.
function tokenSuffix(url) {
  if (url.includes('key=')) return '';
  const k = api.istBild(url) ? api.bildParam() : api.keyParam();
  return k ? (url.includes('?') ? '&' : '?') + k : '';
}

/* ---------------------------------------------------------------- Karten */

/* ---------------------------------------------------------------- Profilbilder
 *
 * Drei Arten: eine Farbe (av-a …), eine eingebaute Figur (fig-fuchs …)
 * oder ein eigenes Bild. Die Figuren sind kleine Strichzeichnungen —
 * eigene, keine fremden Marken. */
const FIGUREN = {
  fuchs:   ['#f59e5b', '<path d="M6 7l3 4h6l3-4-2 9c-1 2-3 3-4 3s-3-1-4-3z"/><circle cx="9.5" cy="13" r=".8" fill="#fff"/><circle cx="14.5" cy="13" r=".8" fill="#fff"/>'],
  eule:    ['#8b7bd1', '<circle cx="12" cy="13" r="7"/><circle cx="9.5" cy="12" r="2"/><circle cx="14.5" cy="12" r="2"/><path d="M12 14l-1 2h2z M7 7l2 2M17 7l-2 2"/>'],
  katze:   ['#e07aa0', '<path d="M6 6l2 4h8l2-4v9a6 6 0 0 1-12 0z"/><path d="M9 13h1M14 13h1M10 17c1 1 3 1 4 0"/>'],
  roboter: ['#6fb0e0', '<rect x="6" y="8" width="12" height="10" rx="2"/><circle cx="9.5" cy="12.5" r="1"/><circle cx="14.5" cy="12.5" r="1"/><path d="M12 4v4M9 16h6"/>'],
  rakete:  ['#ef6b6b', '<path d="M12 3c3 3 4 7 3 11l-3 3-3-3c-1-4 0-8 3-11z"/><path d="M9 14l-3 2 1-5M15 14l3 2-1-5M12 17v3"/>'],
  stern:   ['#f2c84b', '<path d="M12 4l2.4 5 5.6.8-4 3.9.9 5.5-4.9-2.6-4.9 2.6.9-5.5-4-3.9 5.6-.8z"/>'],
  wal:     ['#4fa3c7', '<path d="M4 13c2-5 9-6 14-3l2-3v7c-3 4-9 5-14 2z"/><path d="M15 12h.01M4 13l-1 3"/>'],
  blume:   ['#7fcf8b', '<circle cx="12" cy="10" r="2.5"/><circle cx="12" cy="5" r="2"/><circle cx="17" cy="10" r="2"/><circle cx="7" cy="10" r="2"/><circle cx="12" cy="15" r="2"/><path d="M12 17v4"/>'],
  pilz:    ['#d9775b', '<path d="M4 12c0-5 4-8 8-8s8 3 8 8z"/><path d="M9 12v6a3 3 0 0 0 6 0v-6"/>'],
  geist:   ['#b5b5c8', '<path d="M6 20V11a6 6 0 0 1 12 0v9l-2-2-2 2-2-2-2 2-2-2z"/><circle cx="9.5" cy="11" r=".8" fill="#fff"/><circle cx="14.5" cy="11" r=".8" fill="#fff"/>'],
  drache:  ['#5fb49c', '<path d="M5 15c2-6 7-9 13-8l-2 3 3 2-4 1c0 4-4 6-8 5z"/><path d="M8 18l-2 3M12 18l0 3"/>'],
  pinguin: ['#5c6b8a', '<ellipse cx="12" cy="13" rx="5" ry="7"/><path d="M10 12l2 2 2-2M7 12l-2 4M17 12l2 4"/>'],
};
function avatar(p, size, extra = '') {
  const a = p.avatar || 'av-a';
  const st = `--av:${size}px${extra ? ';' + extra : ''}`;
  if (a === 'bild' && p.id) {
    return `<span class="avatar avatar--bild" style="${st}" aria-hidden="true"><img src="/api/v1/avatar/${esc(p.id)}?v=${S.avatarStand || 0}${api.bildParam() ? '&' + api.bildParam() : ''}" alt="" data-ersatz="${esc((p.name || '?')[0])}"></span>`;
  }
  if (a.startsWith('fig-') && FIGUREN[a.slice(4)]) {
    const [farbe, pfad] = FIGUREN[a.slice(4)];
    return `<span class="avatar avatar--figur" style="${st};background:${farbe}" aria-hidden="true">
      <svg viewBox="0 0 24 24" fill="none" stroke="#fff" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round">${pfad}</svg></span>`;
  }
  return `<span class="avatar ${esc(a)}" style="${st}" aria-hidden="true">${esc((p.name || '?')[0])}</span>`;
}

/* Abzeichen auf der Karte: links „Neu" oder „3 neue Folgen", rechts
   4K und HDR. Nur, was die Daten hergeben — sonst nichts. */
function abzeichen(it) {
  // Eine ganz neue Serie ist „Neu", nicht „12 neue Folgen".
  // „Neu" nur, solange man es noch nicht angefangen hat — sonst stünde es
  // eine Woche lang auf allem, was man längst kennt.
  const links = it.neu && !it.finished && !(it.progress > 1) ? tr('Neu')
    : it.neueFolgen ? tr('{n} neue Folgen', { n: it.neueFolgen }) : '';
  const rechts = [it.qualitaet === '4K' ? '4K' : '', it.hdr ? (it.hdr === 'DV' ? 'Dolby Vision' : it.hdr) : ''].filter(Boolean);
  if (!links && !rechts.length) return '';
  return `<div class="abzeichen" aria-hidden="true">
    ${links ? `<span class="abz abz--neu">${esc(links)}</span>` : '<span></span>'}
    ${rechts.length ? `<span class="abz-rechts">${rechts.map(x => `<span class="abz">${esc(x)}</span>`).join('')}</span>` : ''}
  </div>`;
}

function posterCard(it, hier = false) {
  const pct = it.progress || 0;
  // Bei Serien steht unter dem Titel, wie weit man ist — „4 von 10 Folgen".
  const serie = it.kind === 'series' && it.folgen;
  const unter = serie && it.gesehen
    ? (it.gesehen >= it.folgen ? tr('alle {folgen} Folgen gesehen', { folgen: it.folgen }) : tr('{gesehen} von {folgen} Folgen', { gesehen: it.gesehen, folgen: it.folgen }))
    : `${jahre(it)}${it.imdbRating ? ` · ★ ${it.imdbRating}` : ''}`;
  return `<button class="card poster${hier === true ? ' poster--hier' : ''}" data-go="detail" data-id="${it.id}"
      aria-label="${esc(it.title)}${it.finished ? tr(', gesehen') : serie && it.gesehen ? tr(', {gesehen} von {folgen} Folgen gesehen', { gesehen: it.gesehen, folgen: it.folgen }) : ''}"${hier === true ? ' aria-current="true"' : ''}>
    <div class="thumb thumb--poster" style="position:relative;width:168px;height:252px;border-radius:12px;overflow:hidden">
      ${artOf(it)}
      ${abzeichen(it)}
      ${it.finished ? `<span class="watched" aria-hidden="true">${svg(I.check, 13, 2.6)}</span>` : ''}
      ${pct > 1 && !it.finished ? `<div class="progress progress--onart"><i style="width:${pct}%"></i></div>` : ''}
    </div>
    <div class="t-body clamp-1" style="margin-top:10px">${esc(it.title)}</div>
    <div class="t-sub c-2">${unter}</div>
  </button>`;
}

/* Wie eine Karte in „Weiterschauen" heisst: bei Folgen die Serie oben,
   darunter „S1 F2 · Folgentitel". Ohne Seriennamen stand dort nur
   „Pilot" — man wusste nicht, wovon. */
function folgenName(it) {
  return it.kind === 'episode' ? `${sf(it.seasonNo, it.episodeNo)} · ${it.title}` : it.title;
}
function weiterText(it) {
  const sek = (it.resumeMs || 0) / 1000;
  if (sek > 0 && !it.naechsteFolge) return `${tr('Weiter bei')} ${clock(sek)}`;
  if (it.naechsteFolge) return tr('Nächste Folge');
  return '';
}

/* Weiterschauen: ein Tipp spielt ab, an der gemerkten Stelle — dafür ist
   die Reihe da. Zur Titelseite geht es über das Info-Symbol daneben. */
function wideCard(it) {
  const pct = it.progress || 0;
  const oben = it.kind === 'episode' && it.seriesTitle ? it.seriesTitle : it.title;
  const unten = it.kind === 'episode'
    ? [it.seriesTitle ? folgenName(it) : sf(it.seasonNo, it.episodeNo), weiterText(it)].filter(Boolean).join(' · ')
    : [weiterText(it) || jahre(it)].filter(Boolean).join(' · ');
  const zurSeite = it.kind === 'episode' && it.seriesId ? it.seriesId : it.id;
  return `<div class="widecard-huelle" style="position:relative">
    <button class="card widecard" data-play="${esc(it.id)}" aria-label="${esc(tr('{titel} ansehen', { titel: [oben, unten].filter(Boolean).join(', ') }))}">
      <div class="thumb thumb--wide" style="position:relative;width:300px;height:169px;border-radius:12px;overflow:hidden">
        ${artOf(it, 'quer')}
        <span class="folge-play" aria-hidden="true">${PLAY(26)}</span>
        ${pct > 1 && !it.finished ? `<div class="progress progress--onart"><i style="width:${pct}%"></i></div>` : ''}
      </div>
      <div class="t-body clamp-1" style="margin-top:10px">${esc(oben)}</div>
      <div class="t-sub c-2 clamp-1">${esc(unten)}</div>
    </button>
    <button class="btn btn-round sm glass glass--clear widecard-info" data-go="detail" data-id="${esc(zurSeite)}"
      aria-label="${tr('Mehr zu {oben}', { oben: esc(oben) })}">${svg('<circle cx="12" cy="12" r="9"/><path d="M12 11v6M12 7.5v.5"/>', 18, 2)}</button>
  </div>`;
}

/* ---------------------------------------------------------------- Bewegte Vorschau
 *
 * Nur mit Maus (hover: hover): auf dem Handy wäre es Akku für nichts.
 * Hero: nach einer Sekunde Ruhe über dem Bild läuft stumm ein Schnipsel.
 * Karte: nach 700 ms Maus über einem Poster klappt eine breite Karte auf,
 * mit Schnipsel, Titel und Knöpfen. Der Ton bleibt aus, bis man ihn will. */
const mitMaus = () => matchMedia('(hover: hover) and (pointer: fine)').matches;
let hoverTimer = null, hoverKarte = null, hoverFuer = null;

const kannH264 = () => document.createElement('video').canPlayType('video/mp4; codecs="avc1.42E01E"') !== '';
function vorschauVideo(id, klass) {
  // Ohne H.264 (manche Linux-Browser) gibt es denselben Schnipsel als VP9.
  const src = api.artURL(id, 'vorschau') + (kannH264() ? '' : '&f=webm');
  return `<video class="${klass}" muted autoplay loop playsinline preload="none"
    src="${src}" data-weg></video>`;
}

function heroVorschau() {
  if (!mitMaus() || wenigBewegung()) return;
  const h = app.querySelector('[data-vorschau-hero]');
  if (!h) return;
  let t = null;
  h.addEventListener('mouseenter', () => {
    t = setTimeout(() => {
      if (h.querySelector('video')) return;
      h.insertAdjacentHTML('afterbegin', vorschauVideo(h.dataset.vorschauHero, 'hero-video'));
      const v = h.querySelector('video');
      v.addEventListener('playing', () => { h.classList.add('hero--laeuft'); const b = h.querySelector('[data-vorschau-ton]'); if (b) b.hidden = false; });
    }, 1000);
  });
  h.addEventListener('mouseleave', () => {
    clearTimeout(t);
    const v = h.querySelector('video');
    if (v) { v.remove(); h.classList.remove('hero--laeuft'); }
    const b = h.querySelector('[data-vorschau-ton]'); if (b) { b.hidden = true; b.setAttribute('aria-pressed', 'false'); }
  });
}

function hoverKarteWeg() {
  clearTimeout(hoverTimer);
  hoverTimer = null;
  if (hoverKarte) { hoverKarte.remove(); hoverKarte = null; hoverFuer = null; }
}

function hoverKarteZeigen(card) {
  const id = card.dataset.id;
  const it = titelAusDaten(id);
  if (!it) return;
  hoverKarteWeg();
  hoverFuer = id;
  const r = card.getBoundingClientRect();
  const breite = 320;
  // Nie über die Seitenleiste: links beginnt der Platz rechts von ihr.
  const leiste = document.querySelector('.sidebar')?.getBoundingClientRect();
  const minLeft = (leiste && leiste.width < innerWidth / 2 ? leiste.right : 0) + 12;
  let left = r.left + r.width / 2 - breite / 2;
  left = Math.max(minLeft, Math.min(left, innerWidth - breite - 12));
  const k = document.createElement('div');
  k.className = 'hoverkarte glass glass--regular' + (app.classList.contains('light') ? ' light' : '');
  k.style.left = left + 'px';
  // Oben bündig mit dem Poster, nicht darüber: Über einer Reihe stehen oft
  // Knöpfe (Filmreihe: Ansehen, Umbenennen, Auflösen), die die Karte sonst
  // verdeckte. Unten nicht aus dem Fenster laufen (Karte ≈ 300 px hoch).
  k.style.top = Math.max(12, Math.min(r.top - 6, innerHeight - 312)) + 'px';
  k.style.width = breite + 'px';
  k.dataset.id = id;
  const unter = [jahre(it), it.kind === 'series' && it.folgen ? (it.gesehen ? tr('{gesehen} von {folgen} Folgen', { gesehen: it.gesehen, folgen: it.folgen }) : tr('{folgen} Folgen', { folgen: it.folgen })) : '',
    (it.genres || []).slice(0, 2).join(', '), it.ageYears != null ? tr('ab {n}', { n: it.ageYears }) : ''].filter(Boolean).join(' · ');
  k.innerHTML = `
    <div class="hoverkarte-bild" data-hk-oeffnen="${esc(id)}">${artOf(it, 'quer')}${wenigBewegung() ? '' : vorschauVideo(id, 'hoverkarte-video')}
      ${wenigBewegung() ? '' : `<button class="btn btn-round sm glass glass--clear hk-ton" data-vorschau-ton aria-label="${tr('Ton der Vorschau')}" aria-pressed="false">${svg(I.ton, 16, 2)}</button>`}</div>
    <div class="hoverkarte-text">
      <button class="hoverkarte-titel" data-go="detail" data-id="${esc(id)}">${esc(it.title)}</button>
      <div class="t-caption c-2 clamp-1">${esc(unter)}</div>
      <div style="display:flex;gap:8px;margin-top:10px">
        <button class="btn btn-pill btn-pill--fill" style="height:32px;font-size:.8125rem" data-hk-play="${esc(id)}">${PLAY(14)} ${it.resumeMs || (it.kind === 'series' && it.gesehen) ? tr('Weiterschauen') : tr('Ansehen')}</button>
        <button class="btn btn-pill" style="height:32px;font-size:.8125rem" data-hk-merken="${esc(id)}" aria-pressed="${!!it.onList}">${it.onList ? tr('Gemerkt') : tr('Merken')}</button>
        <button class="btn btn-pill" style="height:32px;font-size:.8125rem" data-go="detail" data-id="${esc(id)}">${tr('Info')}</button>
      </div></div>`;
  document.body.appendChild(k);
  hoverKarte = k;
  k.addEventListener('mouseleave', hoverKarteWeg);
}

/* Den Titel zur Karte finden — er steckt in den Daten der Seite. */
function titelAusDaten(id) {
  const d = S.data || {};
  const listen = [...(d.rows || []).map(r => r.items), d.items, d.similar, d.collection?.items,
    ...(d.collections || []).map(c => c.items), d.episodes].filter(Boolean);
  for (const l of listen) { const x = l.find(i => i && i.id === id); if (x) return x; }
  return null;
}

app.addEventListener('mouseover', e => {
  if (!mitMaus() || S.view === 'player') return;
  const card = e.target.closest('.poster[data-id]');
  if (!card) return;
  if (hoverKarte && hoverFuer === card.dataset.id) return;
  clearTimeout(hoverTimer);
  hoverTimer = setTimeout(() => hoverKarteZeigen(card), 700);
});
app.addEventListener('mouseout', e => {
  const card = e.target.closest('.poster[data-id]');
  if (!card) return;
  const zu = e.relatedTarget;
  if (zu && (card.contains(zu) || (hoverKarte && hoverKarte.contains(zu)))) return;
  clearTimeout(hoverTimer);
  hoverTimer = null;
  // Die Karte bleibt, solange die Maus zu ihr wandert; sonst weg.
  setTimeout(() => { if (hoverKarte && !hoverKarte.matches(':hover')) hoverKarteWeg(); }, 120);
});
document.addEventListener('click', async e => {
  const ton = e.target.closest('[data-vorschau-ton]');
  if (ton) {
    const v = ton.parentElement.querySelector('video') || ton.closest('.hero')?.querySelector('video');
    if (v) { v.muted = !v.muted; ton.setAttribute('aria-pressed', String(!v.muted)); ton.classList.toggle('an', !v.muted); }
    return;
  }
  if (!hoverKarte || !hoverKarte.contains(e.target)) return;
  const play = e.target.closest('[data-hk-play]');
  if (play) { const id = play.dataset.hkPlay; hoverKarteWeg(); const it = titelAusDaten(id); return it?.kind === 'series' ? ersteOffeneFolge(id).then(f => f && startPlayer(f.id)) : startPlayer(id); }
  const merk = e.target.closest('[data-hk-merken]');
  if (merk) {
    const id = merk.dataset.hkMerken, it = titelAusDaten(id);
    try { await (it?.onList ? api.removeFromList(id) : api.addToList(id)); if (it) it.onList = !it.onList; merk.textContent = it?.onList ? tr('Gemerkt') : tr('Merken'); }
    catch (err) { toast(err.message); }
    return;
  }
  // Die Karte hängt am body, nicht in #app — dessen Klick-Verteiler sieht
  // sie nicht. Darum führen Titel, „Info" und das Bild selbst von hier
  // aus zur Titelseite.
  const go_ = e.target.closest('[data-go]') || e.target.closest('[data-hk-oeffnen]');
  if (go_) {
    const id = go_.dataset.id || go_.dataset.hkOeffnen;
    hoverKarteWeg();
    if (id) go('detail', { id });
  }
});

/* Pfeile an den Reihen — wie bei Netflix: sie erscheinen, wenn die Maus
   über der Reihe ist, und blättern um eine Bildschirmbreite. Nur mit
   Maus; auf dem Handy und Tablet wischt man, da wären sie im Weg. */
function reihenPfeile() {
  if (!matchMedia('(hover: hover) and (pointer: fine)').matches) return;
  for (const r of app.querySelectorAll('.rail')) {
    if (r.closest('.rail-huelle')) continue;
    const h = document.createElement('div');
    h.className = 'rail-huelle';
    r.replaceWith(h);
    h.append(r);
    for (const [seite, pfad, text] of [['l', 'M15 5l-7 7 7 7', tr('Zurück')], ['r', 'M9 5l7 7-7 7', tr('Weiter')]]) {
      const b = document.createElement('button');
      b.className = 'rail-pfeil rail-pfeil--' + seite;
      b.setAttribute('aria-label', text);
      b.innerHTML = svg(`<path d="${pfad}"/>`, 26, 2.4);
      b.addEventListener('click', () => r.scrollBy({ left: (seite === 'r' ? 1 : -1) * Math.max(200, r.clientWidth - 120), behavior: wenigBewegung() ? 'auto' : 'smooth' }));
      h.append(b);
    }
    const stand = () => {
      h.classList.toggle('am-anfang', r.scrollLeft <= 2);
      h.classList.toggle('am-ende', r.scrollLeft + r.clientWidth >= r.scrollWidth - 2);
      h.classList.toggle('passt', r.scrollWidth <= r.clientWidth + 2);
    };
    r.addEventListener('scroll', stand, { passive: true });
    // Kein eigener resize-Lauscher je Reihe: die blieben nach jedem
    // render() am window hängen. Ein einziger (unten) fragt alle Hüllen.
    h.pfeilStand = stand;
    stand();
  }
}

const rail = (title, items, wide) => !items.length ? '' : `
  <section class="sec"><h2 class="t-title3">${esc(title)}</h2>
    <div class="rail">${items.map(wide ? wideCard : posterCard).join('')}</div></section>`;

/* ---------------------------------------------------------------- Gerüst */

// Eine Funktion, keine Konstante: Die Namen gelten in der Sprache,
// die gerade eingestellt ist.
const NAV = () => [
  ['home', I.home, tr('Startseite')],
  ['movies', I.film, tr('Filme')],
  ['series', I.tv, tr('Serien')],
  // Merkliste, Gesehen und eigene Listen sind ein Bereich mit Reitern —
  // zwei Einträge dafür wirkten wie dasselbe zweimal.
  ['watchlist', I.mark, tr('Merkliste')],
  ['favorites', I.herz, tr('Favoriten')],
  ['collections', I.reihe, tr('Filmreihen')],
  ['wishes', I.wish, tr('Wünsche')],
  ['news', I.glocke, tr('Neuigkeiten')],
];

function sidebar() {
  const p = S.me?.account;
  if (!p) return '';
  const items = NAV().map(([v, d, label]) =>
    `<a href="${adresseFuer(v) || '#'}" class="nav" data-go="${v}" title=""${esc(label)}"${
      S.view === v || (v === 'watchlist' && ['gesehen', 'playlists', 'playlist'].includes(S.view))
        || (v === 'collections' && ['collection', 'sammlungen', 'sammlung'].includes(S.view)) ? ' aria-current="page"' : ''}>${svg(d, 20)}${label}${
      v === 'news' && S.me.neuigkeiten ? `<span class="punkt" aria-hidden="true">${S.me.neuigkeiten > 9 ? '9+' : S.me.neuigkeiten}</span><span class="sr-only">${tr(', {n} ungelesen', { n: S.me.neuigkeiten })}</span>` : ''}</a>`).join('');
  return `<aside class="sidebar glass glass--regular ${S.navZu ? 'sidebar--zu' : 'is-open'}"
    aria-label="${tr('Navigation')}">
    <div class="sidebar-top">
      <div class="sidebar-mark">Lumen</div>
      <button class="nav-zu" data-navzu aria-expanded="${!S.navZu}"
        aria-label="${S.navZu ? tr('Navigation aufklappen') : tr('Navigation zuklappen')}"
        title="${S.navZu ? tr('Aufklappen') : tr('Zuklappen')}">${svg(I.back, 18, 2)}</button>
    </div>
    <button class="sidebar-search" data-go="search" title="${tr('Suchen')}">${svg(I.search, 16, 2)}${tr('Suchen')}</button>
    ${items}
    <div class="sidebar-foot">
      ${verwaltungHier() ? `<a href="${adresseFuer('admin') || '#'}" class="nav nav--quiet" data-go="admin"${ADMIN_VIEWS.includes(S.view) ? ' aria-current="page"' : ''}>${svg(I.gear, 19)}${tr('Verwaltung')}</a>` : ''}
      <a href="${adresseFuer('settings') || '#'}" class="nav" data-go="settings"${S.view === 'settings' ? ' aria-current="page"' : ''}>
        ${avatar(p, 28)}${esc(p.name)}</a>
    </div>
  </aside>`;
}

/* Auf dem Handy gehört die Navigation nach unten, wie bei jeder App auf
   dem iPhone: dort reicht der Daumen hin, und die Seite behält ihre
   ganze Breite. Gezeigt wird sie nur auf schmalen Bildschirmen — das
   entscheidet das CSS, nicht dieser Code. */
const TABS = () => [
  ['home', I.home, tr('Startseite')],
  ['movies', I.film, tr('Filme')],
  // In der Leiste unten ist wenig Platz: eigene, kurze Wörter („TV"
  // statt „TV Programmes").
  ['series', I.tv, tr('Tab|Serien')],
  ['watchlist', I.mark, tr('Merkliste')],
  ['search', I.search, tr('Suchen')],
];
function tabbar() {
  const p = S.me?.account;
  if (!p) return '';
  const ich = ['settings', 'wishes', 'news', 'favorites', 'collections', 'collection', 'sammlungen', 'sammlung', 'playlists', 'playlist',
    ...ADMIN_VIEWS].includes(S.view);
  return `<nav class="tabbar glass glass--regular" aria-label="${tr('Navigation')}">
    ${TABS().map(([v, d, label]) => `<a href="${adresseFuer(v) || '#'}" class="tab" data-go="${v}"${
      S.view === v || (v === 'watchlist' && S.view === 'gesehen') ? ' aria-current="page"' : ''}>${svg(d, 22)}<span>${label}</span></a>`).join('')}
    <a href="${adresseFuer('settings') || '#'}" class="tab" data-go="settings"${ich ? ' aria-current="page"' : ''} style="position:relative">
      ${avatar(p, 24)}${S.me.neuigkeiten ? '<span class="punkt punkt--tab" aria-hidden="true"></span>' : ''}
      <span>${tr('Ich')}</span></a>
  </nav>`;
}

const shell = inner => `<a href="#inhalt" class="skip-link" data-skip>${tr('Zum Inhalt springen')}</a>${sidebar()}<main class="page" id="inhalt">${inner}</main>${tabbar()}`;
const loading = text => `<div class="loading"><span class="spin"></span>${esc(text || tr('Einen Moment …'))}</div>`;

/* ---------------------------------------------------------------- Router */

/* go wechselt die Ansicht und trägt sie in den Verlauf des Browsers ein.
 *
 * Vorher stand dort nichts: „Zurück" auf einer Detailseite rief
 * history.back() und führte aus Lumen hinaus, statt eine Seite zurück —
 * und die Zurück-Taste eines Android-Handys schloss die App. Jetzt hat
 * jede Ansicht ihren Eintrag. */
const OHNE_VERLAUF = ['start', 'setup', 'profiles', 'unlock', 'redeem', 'player'];
// Wie tief man in Lumen steht. Jeder Eintrag trägt seine Stelle; so
// stimmt die Zahl auch nach Vor- und Zurückspringen im Browser.
let verlaufTiefe = 0;

async function go(view, opts = {}) {
  // Kommt der Wechsel aus dem Verlauf, während ein Film läuft, erst den
  // Film anhalten — sonst liefe der Ton unter der neuen Seite weiter.
  if (S.playing && view !== 'player') await spielerAnhalten();
  // Nach der Anmeldung dorthin, wo man vorher war (abgelaufene Sitzung,
  // Lesezeichen) — nicht stur auf die Startseite.
  if (view === 'home' && S.me && S.nachAnmeldung) {
    const z = S.nachAnmeldung;
    S.nachAnmeldung = null;
    if (z.view !== 'home') return go(z.view, { id: z.id, query: z.query, ersetzen: true });
  }
  if (S.me) S.hinweis = '';
  // Die Verwaltung gibt es nur am Computer — auch über eine Adresse nicht.
  if (ADMIN_VIEWS.includes(view) && nurBeruehrung()) view = 'amComputer';
  // Wer nicht verwaltet, landet über eine Verwaltungs-Adresse auf der
  // Startseite — nicht in einer Fehlermeldung, bei der „Nochmal" nie hilft.
  if (ADMIN_VIEWS.includes(view) && S.me && !S.me.account?.isAdmin) { view = 'home'; opts = { ...opts, ersetzen: true }; }
  // Stand der Seite, die man verlässt (Scrollstelle, Filter), festhalten:
  // „Zurück" soll genau dorthin führen.
  standMerken();
  const vorher = S.view;
  S.view = view;
  S.id = opts.id ?? null;
  S.error = '';
  S.delLib = null;
  S.browse = null;
  S.bearbeiten = null;
  S.listeWeg = false;
  // Die Sucheingabe bleibt im Verlauf: wer von einem Treffer zurückkommt,
  // findet seine Suche wieder.
  if (view === 'search' && opts.query != null) S.query = opts.query;
  if (view !== 'search') S.query = '';
  if (view !== vorher) { S.filter = 'Alle'; S.buchstabe = ''; S.einladungKind = false; }
  if (opts.filter) S.filter = opts.filter;
  // Aus dem Verlauf: Filter, Sortierung und Scrollstelle wie verlassen.
  const stand = opts.ausVerlauf ? (verlaufsStand.get(opts.schluessel) || opts.stand || null) : null;
  if (stand) {
    if (stand.filter) S.filter = stand.filter;
    if (stand.buchstabe !== undefined) S.buchstabe = stand.buchstabe;
    if (stand.favArt !== undefined) S.favArt = stand.favArt;
  }
  S.wiederherstellen = stand;
  if (!opts.ausVerlauf || !opts.schluessel) {
    aktSchluessel = neuerSchluessel();
    const eintrag = { view, id: S.id, tiefe: verlaufTiefe, query: S.query, schluessel: aktSchluessel };
    const adresse = OHNE_VERLAUF.includes(view) ? undefined : adresseFuer(view, S.id, S.query) ?? undefined;
    try {
      if (OHNE_VERLAUF.includes(view) || opts.ersetzen || opts.ausVerlauf) {
        history.replaceState(eintrag, '', adresse);
      } else {
        verlaufTiefe++;
        eintrag.tiefe = verlaufTiefe;
        history.pushState(eintrag, '', adresse);
      }
    } catch {}
  } else {
    aktSchluessel = opts.schluessel;
    S.ausSpeicher = true;
  }
  await load();
}

addEventListener('popstate', e => {
  let st = e.state;
  // Von Hand geänderte Adresse: kein Zustand, aber eine lesbare Adresse.
  if (!st?.view) { const z = ansichtAusAdresse(); if (z) st = { ...z, tiefe: verlaufTiefe }; }
  verlaufTiefe = st && typeof st.tiefe === 'number' ? st.tiefe : 0;
  // Angemeldet zurück auf Profilauswahl oder PIN-Eingabe: die gelten nicht
  // mehr — sonst stand „Wer schaut?" da, obwohl man angemeldet ist.
  if (st && S.me && ['start', 'setup', 'profiles', 'unlock', 'redeem'].includes(st.view)) {
    go('home', { ersetzen: true });
    return;
  }
  if (st && st.view && S.me) go(st.view, { id: st.id, ausVerlauf: true, query: st.query, schluessel: st.schluessel, stand: st });
});

function zurueck() {
  if (verlaufTiefe > 0 && history.state?.view) history.back();
  else go('home');
}

/* load holt, was die Ansicht braucht, und zeichnet erst dann. Ein kurzer
   Ladehinweis erscheint nur, wenn es wirklich dauert — sonst flackert es. */
async function load() {
  // Jede Anfrage bekommt eine Nummer. Kommt eine ältere Antwort an,
  // nachdem schon eine neuere unterwegs ist (schnell weitergeklickt,
  // weitergetippt), wird sie verworfen — sonst überschriebe sie die
  // Ansicht, auf der man inzwischen steht.
  const nr = ++ladeNummer;
  const gemerkt = S.ausSpeicher ? datenSpeicher.get(aktSchluessel) : null;
  S.ausSpeicher = false;
  if (gemerkt && Date.now() - gemerkt.zeit < 5 * 60 * 1000) {
    S.data = gemerkt.data; S.error = ''; S.busy = false;
    return render();
  }
  const slow = setTimeout(() => { if (nr === ladeNummer) { S.busy = true; render(); } }, 220);
  try {
    // Der Zähler der Neuigkeiten hängt an /me — auf der Startseite
    // nebenbei auffrischen, damit der Punkt nicht bis zur nächsten
    // Anmeldung alt bleibt.
    if (S.me && S.view === 'home') api.me().then(me => { S.me = me; const n = app.querySelector('.sidebar'); if (n) n.outerHTML = sidebar(); }).catch(() => {});
    const daten = await fetchFor(S.view);
    if (nr !== ladeNummer) return;
    S.data = daten;
    S.error = '';
    S.fehlerStatus = 0;
    if (SPEICHER_ANSICHTEN.includes(S.view)) datenSpeicher.set(aktSchluessel, { data: daten, zeit: Date.now() });
  } catch (e) {
    if (nr !== ladeNummer) return;
    S.data = null;
    S.error = e instanceof ApiError ? e.message : tr('Unerwarteter Fehler.');
    S.fehlerStatus = e instanceof ApiError ? e.status : 0;
    if (e instanceof ApiError && e.status === 401) {
      clearTimeout(slow); S.busy = false; S.me = null;
      return go('profiles');
    }
    // Das eigene Profil hat keine PIN: die Verwaltung bleibt zu, bis eine
    // gesetzt ist. Unter „Ich" gleich danach fragen.
    if (e instanceof ApiError && e.code === 'profil_sperre_fehlt') {
      clearTimeout(slow); S.busy = false;
      S.schutzNoetig = true;
      return go('settings', { ersetzen: true });
    }
    // Die Verwaltung ist gesperrt: erst entsperren, dann dorthin zurück.
    if (e instanceof ApiError && e.sperre && S.view !== 'adminUnlock') {
      clearTimeout(slow); S.busy = false;
      S.sperre = { art: e.sperre, zurueck: S.view, id: S.id };
      return go('adminUnlock', { ersetzen: true });
    }
  } finally {
    clearTimeout(slow);
    if (nr === ladeNummer) S.busy = false;
  }
  render();
}

async function fetchFor(view) {
  switch (view) {
    case 'profiles':
      try { return { profiles: await api.profiles() }; }
      catch (e) { if (e.code === 'unterwegs') return { unterwegs: true }; throw e; }
    case 'home': {
      // Die Startseite zeigt höchstens zwölf Reihen — nur die holen, nicht alle.
      // Kinderprofil: auch die Bildschirmzeit — ist sie um, sagt es ein
      // Band oben (bzBand), nicht erst der Knopf „Ansehen".
      const [h, reihen] = await Promise.all([api.home(), api.call('/collections?limit=12').catch(() => []),
        S.me?.account?.isKid ? bzLaden() : null]);
      return { ...h, collections: reihen };
    }
    case 'collections': return { collections: await api.collections() };
    case 'collection':  return api.collection(S.id);
    case 'sammlungen':  return { sammlungen: await sammlungenLaden() };
    case 'sammlung':    return sammlungLaden(S.id);
    case 'playlists':   return { lists: await api.playlists() };
    case 'playlist':    return api.playlist(S.id);
    case 'movies':
    case 'series': {
      const kind = view === 'movies' ? 'movie' : 'series', genre = S.filter === 'Alle' ? '' : S.filter;
      const { key, dir } = sortierungFuer(view);
      const [seite, buchstaben] = await Promise.all([
        api.seite({ kind, genre, sort: key, dir, buchstabe: key === 'title' ? S.buchstabe : '' }),
        api.buchstaben(kind, genre).catch(() => ({}))]);
      return { ...seite, buchstaben };
    }
    case 'watchlist': return api.seite({ liste: 'merkliste', sort: '', limit: 2000 });
    case 'gesehen':   return api.seite({ liste: 'gesehen', sort: '', limit: 2000 });
    case 'favorites': return api.seite({ liste: 'favoriten', sort: '', kind: S.favArt || 'movie', limit: 2000 });
    case 'search': {
      if (!S.query.trim()) return { items: [], personen: [], genres: [] };
      return api.search(S.query);
    }
    case 'person': {
      const [items, info] = await Promise.all([api.items({ person: S.id, limit: 200 }),
        api.call('/persons/' + encodeURIComponent(S.id)).catch(() => null)]);
      return { items, info };
    }
    case 'news': {
      const d = await api.news();
      // Öffnen heisst gelesen — aber erst, nachdem die Liste da ist, damit
      // die neuen Einträge noch als neu markiert sind.
      if (d.ungelesen) { api.newsSeen().catch(() => {}); if (S.me) S.me.neuigkeiten = 0; }
      return d;
    }
    case 'detail': {
      const d = await api.item(S.id);
      // Bei einer Serie gleich die Folgen der ersten Staffel dazu — die
      // Seite soll nicht mit einer leeren Liste aufgehen.
      // Die Reihenfolge kommt vom Server, auf allen Geräten gleich:
      // Specials zuletzt.
      const staffeln = d.seasons || [];
      if (staffeln.length) {
        d.seasons = staffeln;
        // Welche Staffel offen ist: die zuletzt gewählte (nach einem Haken
        // oder zurück aus dem Player), sonst die der nächsten Folge —
        // nicht immer Staffel 1, wenn man längst in Staffel 4 ist.
        d.weiter = (await api.weiterInSerie(S.id).catch(() => null))?.item || null;
        const gemerkt = S.staffelWahl?.serie === S.id ? S.staffelWahl.staffel : null;
        const wahl = staffeln.find(x => x.id === gemerkt) || staffeln.find(x => x.id === d.weiter?.parentId) || staffeln[0];
        d.staffel = wahl.id;
        d.episodes = await api.children(wahl.id).catch(() => []);
      }
      // Ob dieses Gerät die Datei direkt abspielt — für die Technik-Zeile.
      const f = (d.files || []).find(x => !x.missing);
      if (f) d.plan = (await api.plan(f.id).catch(() => null))?.plan || null;
      // Herunterladen nur zeigen, wenn das Profil es darf.
      if (f) d.download = await api.downloadInfo(S.id).catch(() => null);
      return d;
    }
    case 'wishes':    return { wishes: await api.wishes() };
    case 'settings':  {
      const [devices, playbacks] = await Promise.all([api.devices(), api.myPlaybacks().catch(() => []),
        S.me?.account?.isKid ? bzLaden() : null]);
      return { devices, playbacks };
    }
    case 'admin':     return { ...await api.adminOverview(),
                                fern: await api.adminRemote().catch(() => null),
                                // Damit die Übersicht sagen kann, ob ein
                                // Schlüssel fehlt und wie viele Wünsche
                                // offen sind — sonst muss man überall
                                // einzeln nachsehen.
                                meta: await api.adminMetadata().catch(() => null),
                                wuensche: await api.adminWishes().catch(() => null),
                                update: voll() ? await api.adminUpdate().catch(() => null) : null,
                                gruppen: voll() ? await api.adminGroups().catch(() => null) : null };
    case 'adminUpdate': return api.adminUpdate();
    case 'adminBackups': return api.adminBackups();
    case 'adminUmwandlung': return api.adminUmwandlung();
    case 'adminGroups': { const [groups, libraries, accounts] = await Promise.all([api.adminGroups(), api.adminLibraries(), api.adminAccounts()]); return { groups, libraries, accounts }; }
    case 'adminLock':   return api.adminLock();
    case 'adminUnlock': return api.adminLock();
    case 'adminLibraries': return { libraries: await api.adminLibraries() };
    case 'adminWishes': return api.adminWishes();
    case 'sessions':  return api.adminSessions();
    case 'logs':      return { lines: await api.adminLog(S.logFilter) };
    case 'cleanup':   return { ...await api.adminCleanup(),
                                 reihen: await api.adminCollections().catch(() => []) };
    case 'accounts':  { const [accounts, libraries, einladungen] = await Promise.all([api.adminAccounts(), api.adminLibraries().catch(() => []), api.adminInvites().catch(() => [])]); return { accounts, libraries, einladungen }; }
    case 'metadata':  return api.adminMetadata();
    default:          return null;
  }
}

function render() {
  // Hat das Profil eine andere Sprache als die Oberfläche gerade zeigt (nach
  // dem Anmelden, nach einem Wechsel auf einem anderen Gerät), wechseln –
  // beiWechsel zeichnet danach neu.
  if (S.me?.sprache && S.me.sprache !== sprache()) sprachWechseln(S.me.sprache);
  let html;
  if (S.busy && S.view !== 'player') {
    html = S.me ? shell(`<div class="wrap">${loading()}</div>`) : `<div class="center">${loading()}</div>`;
  } else if (S.error && S.fehlerStatus === 404 && MIT_KENNUNG.includes(S.view)) {
    // Ein alter Link, ein gelöschter Titel: kein roher Serverfehler und
    // kein „Nochmal versuchen" (es käme dasselbe), sondern ein Weg zurück.
    html = shell(`<div class="wrap">${leer({ symbol: S.view === 'detail' ? I.film : I.search, h1: true,
      titel: S.view === 'detail' ? tr('Diesen Titel gibt es nicht (mehr)') : tr('Das gibt es hier nicht (mehr)'),
      text: esc(tr('Vielleicht wurde der Eintrag gelöscht, oder der Link ist nicht vollständig.')),
      aktion: `<button class="btn btn-pill btn-pill--fill" data-go="home">${tr('Zur Startseite')}</button>` })}</div>`);
  } else if (S.error && !['profiles', 'unlock', 'setup', 'redeem', 'start'].includes(S.view)) {
    html = shell(`<div class="wrap"><div style="padding-top:80px;max-width:520px">
      <h1 class="t-large">${tr('Das hat nicht geklappt')}</h1>
      <p class="t-body c-2" style="margin-top:10px">${esc(S.error)}</p>
      <button class="btn btn-primary" style="margin-top:20px" data-retry>${tr('Nochmal versuchen')}</button>
    </div></div>`);
  } else {
    switch (S.view) {
      case 'start':     html = `<div class="center">${loading(tr('Lumen wird geladen …'))}</div>`; break;
      case 'setup':     html = viewSetup(); break;
      case 'redeem':    html = viewRedeem(); break;
      case 'profiles':  html = viewProfiles(); break;
      case 'unlock':    html = viewUnlock(); break;
      case 'home':      html = viewHome(); break;
      case 'movies':    html = viewGrid(tr('Filme')); break;
      case 'series':    html = viewGrid(tr('Serien')); break;
      case 'watchlist': html = viewWatchlist(); break;
      case 'gesehen':   html = viewGesehen(); break;
      case 'favorites': html = viewFavorites(); break;
      case 'search':    html = viewSearch(); break;
      case 'person':    html = viewPerson(); break;
      case 'news':      html = viewNews(); break;
      case 'detail':    html = viewDetail(); break;
      case 'player':    html = viewPlayer(); break;
      case 'wishes':    html = viewWishes(); break;
      case 'collections': html = viewCollections(); break;
      case 'collection':  html = viewCollection(); break;
      case 'sammlungen':  html = sammlungenSeite(); break;
      case 'sammlung':    html = sammlungSeite(); break;
      case 'playlists':   html = viewPlaylists(); break;
      case 'playlist':    html = viewPlaylist(); break;
      case 'settings':  html = viewSettings(); break;
      case 'admin':     html = viewAdmin(); break;
      case 'adminLibraries': html = viewAdminLibraries(); break;
      case 'adminWishes': html = viewAdminWishes(); break;
      case 'adminUpdate': html = viewAdminUpdate(); break;
      case 'adminBackups': html = viewAdminBackups(); break;
      case 'adminUmwandlung': html = viewAdminUmwandlung(); break;
      case 'adminGroups': html = viewAdminGroups(); break;
      case 'adminLock':   html = viewAdminLock(); break;
      case 'adminUnlock': html = viewAdminUnlock(); break;
      case 'sessions':  html = viewSessions(); break;
      case 'logs':      html = viewLogs(); break;
      case 'cleanup':   html = viewCleanup(); break;
      case 'accounts':  html = viewAccounts(); break;
      case 'metadata':  html = viewMetadata(); break;
      case 'amComputer': html = viewAmComputer(); break;
      case 'schonDabei': html = viewSchonDabei(); break;
      default:          html = viewHome();
    }
  }
  // Der Player wird genau einmal aufgebaut. Würde er sich neu zeichnen,
  // entstünde jedes Mal ein neues <video> — das löst wieder ein Ereignis
  // aus, das neu zeichnet, und so fort. Darum ab hier nur noch Nachbessern.
  if (S.view === 'player' && S.playing?.mounted && app.querySelector('.player')) {
    patchChrome();
    return;
  }

  // Ansichten ohne Gerüst (Einrichtung, Einladung, Profilwahl) bekommen
  // trotzdem eine Hauptlandmarke — für Bildschirmleser der Einstieg.
  if (!html.includes('<main')) html = `<main id="inhalt">${html}</main>`;
  if (S.hinweis && !S.me && !S.busy) html =`<div class="anmelde-hinweis" role="alert">${esc(S.hinweis)}</div>` + html;
  const vorherFokus = fokusSchluessel();
  // Dieselbe Seite neu gezeichnet (Stern gesetzt, Folge abgehakt, Schalter
  // umgelegt): an derselben Stelle bleiben. Vorher sprang jede solche
  // Handlung ganz nach oben.
  const gleicheSeite = !S.wiederherstellen && `${S.view}|${S.id ?? ''}` === letzteAnsicht;
  const altSeite = gleicheSeite ? app.querySelector('.page') : null;
  const altScroll = altSeite ? altSeite.scrollTop : 0;
  const altReihen = gleicheSeite ? [...app.querySelectorAll('.rail')].map(r => r.scrollLeft) : null;
  app.innerHTML = html;
  randHinweise();
  hoverKarteWeg();
  reihenPfeile();
  heroVorschau();
  if (S.view === 'adminLibraries') bibTaktAn();
  if (S.view === 'movies' || S.view === 'series') mehrLadenEinrichten();
  else { mehrBeobachter?.disconnect(); mehrBeobachter = null; }
  if (S.view === 'search') {
    const q = $('#q');
    // Nach Enter bleibt die Tastatur des Handys zu (sie verdeckte die Treffer).
    if (q && !S.suchTastaturZu) { q.focus(); q.setSelectionRange(q.value.length, q.value.length); }
    S.suchTastaturZu = false;
    trefferAnsagen();
  }
  if (S.view === 'player') mountPlayer();
  else nachDemZeichnen(vorherFokus);
  if (gleicheSeite && !S.busy) {
    const seite = app.querySelector('.page');
    if (seite && altScroll) seite.scrollTop = altScroll;
    if (altReihen) app.querySelectorAll('.rail').forEach((r, i) => { if (altReihen[i]) r.scrollLeft = altReihen[i]; });
  }
}

/* Waagerecht scrollende Leisten (Segmente, Genres, A–Z): Ein
   Verlauf am Rand zeigt, dass es dort weitergeht – sonst sähe „Ält…"
   aus wie ein abgeschnittener Knopf. */
const ROLL_LEISTEN = '.segment, .chips--rollen, .az-leiste';
function randHinweis(el) {
  const mehr = el.scrollWidth - el.clientWidth;
  if (mehr <= 2) { el.removeAttribute('data-rand'); return; }
  const links = el.scrollLeft > 2, rechts = el.scrollLeft < mehr - 2;
  el.setAttribute('data-rand', links && rechts ? 'beide' : links ? 'links' : 'rechts');
}
function randHinweise() { for (const el of app.querySelectorAll(ROLL_LEISTEN)) randHinweis(el); }
app.addEventListener('scroll', e => {
  if (e.target instanceof Element && e.target.matches(ROLL_LEISTEN)) randHinweis(e.target);
}, { capture: true, passive: true });
addEventListener('resize', () => randHinweise(), { passive: true });

/* ---------------------------------------------------------------- Darstellung */

/* Hell oder dunkel. Voreingestellt ist „Wie das Gerät" — so, wie es
   jede Apple-App hält. Player und grosses Bild bleiben ohnehin dunkel. */
function darstellung() {
  try { return localStorage.getItem('lumen.darstellung') || 'auto'; } catch { return 'auto'; }
}
/* Textgrösse der Oberfläche, zusätzlich zur Einstellung von Browser und
   System (alle Schriften sind in rem). */
const TEXTGROESSEN = { normal: '100%', gross: '112.5%', sehrgross: '125%' };
function textgroesse() {
  try { const w = localStorage.getItem('lumen.textgroesse'); return TEXTGROESSEN[w] ? w : 'normal'; } catch { return 'normal'; }
}
function textgroesseAnwenden() {
  document.documentElement.style.fontSize = TEXTGROESSEN[textgroesse()];
}
textgroesseAnwenden();
function darstellungAnwenden() {
  const w = darstellung();
  const hell = w === 'hell' || (w === 'auto' && matchMedia('(prefers-color-scheme: light)').matches);
  app.classList.toggle('light', hell);
  document.documentElement.classList.toggle('hell', hell);
  document.querySelector('meta[name="theme-color"]')?.setAttribute('content', hell ? '#f2f2f7' : '#050507');
  // Eingebaute Bedienelemente (Radios, Häkchen, Zeitfelder, Rollbalken)
  // zeichnet der Browser nach color-scheme – sonst blieben sie im Hellen dunkel.
  document.querySelector('meta[name="color-scheme"]')?.setAttribute('content', hell ? 'light' : 'dark');
  document.documentElement.style.colorScheme = hell ? 'light' : 'dark';
}
try { matchMedia('(prefers-color-scheme: light)').addEventListener('change', darstellungAnwenden); } catch {}
darstellungAnwenden();

/* ---------------------------------------------------------------- Start */

// Namen der Sprachen, wie der Server sie nennt (jede in sich selbst).
const sprachWahl = () => (S.hello?.sprachen?.length ? S.hello.sprachen : SPRACH_WAHL);
const sprachNameVon = id => sprachWahl().find(s => s.id === id)?.name || id;

/* Erster Schritt der Einrichtung: Sprache und Region. Die Wahl gilt sofort
   für die Oberfläche, damit der Rest der Einrichtung schon in ihr spricht;
   der Server bekommt sie mit dem Einrichten (und belegt daraus auch die
   Sprache und Region der Metadaten vor). */
function viewSetupSprache() {
  const aktiv = S.setupSprache || sprache();
  return `<div class="center"><div class="panel">
    <div style="text-align:center;margin-bottom:8px">
      <h1 class="t-large" style="margin:0">${tr('Willkommen bei Lumen')}</h1>
      <p class="hint" style="margin-top:8px">${tr('Wähle Sprache und Region. Sie gilt für den ganzen Server; jedes Profil kann später seine eigene wählen.')}</p>
    </div>
    <form id="setupsprache" class="panel">
      <div role="radiogroup" aria-label="${tr('Sprache und Region')}" class="group group--inset" style="padding:0">
        ${sprachWahl().map(s => `<label class="row" style="min-height:50px;gap:12px;cursor:pointer" lang="${esc(s.id)}">
          <input type="radio" name="sprache" value="${esc(s.id)}"${s.id === aktiv ? ' checked' : ''} data-setupsprache>
          <span class="t-body" style="flex:1">${esc(s.name)}</span></label>`).join('')}
      </div>
      <button class="btn btn-primary" style="height:46px">${tr('Weiter')}</button>
    </form>
  </div></div>`;
}

function viewSetup() {
  if (!S.setupSpracheFertig) return viewSetupSprache();
  return `<div class="center"><div class="panel">
    <div style="text-align:center;margin-bottom:8px">
      <h1 class="t-large" style="margin:0">${tr('Willkommen bei Lumen')}</h1>
      <p class="hint" style="margin-top:8px">${tr('Du richtest diesen Server ein und wirst sein Besitzer. Später lädst du andere mit einem Link dazu ein — jede Person bekommt ein eigenes Profil.')}</p>
      <p class="hint" style="margin-top:6px">${tr('Lumen war hier schon in Gebrauch? Dann ist der Datenordner falsch eingehängt — bitte nicht einrichten, sondern zuerst die Compose-Datei prüfen.')}</p>
    </div>
    <form id="setupform" class="panel">
      <div class="field"><label for="sname">${tr('Dein Name')}</label>
        <input id="sname" name="name" autocomplete="name" required></div>
      <div class="field"><label for="spass">${tr('Passwort')}</label>
        <input id="spass" name="password" type="password" autocomplete="new-password"
               minlength="8" required>
        <span class="hint">${tr('Mindestens acht Zeichen. Damit meldest du dich auf allen Geräten an.')}</span></div>
      <div class="field"><span class="hint">${tr('Sprache und Region: {name}', { name: esc(sprachNameVon(sprache())) })} ·
        <a href="#" class="link" data-setupsprache-zurueck>${tr('Ändern')}</a></span></div>
      <div class="err">${esc(S.error)}</div>
      <button class="btn btn-primary" style="height:46px">${tr('Server einrichten')}</button>
    </form>
    <p class="hint" style="text-align:center">
      ${tr('Du wurdest eingeladen?')} <a href="#" class="link" data-go="redeem">${tr('Einladung einlösen')}</a></p>
  </div></div>`;
}

/* Eine Einladung einlösen.
 *
 * Diese Ansicht fehlte — der Link aus der Verwaltung führte auf die
 * Startseite, und dort gab es nichts einzulösen. Genau das war gemeint
 * mit „die Einladungslinks funktionieren nicht".
 *
 * Der Gast legt hier sein eigenes Profil an. Was er sehen darf, steht
 * schon in der Einladung; er wählt nur Namen und Anmeldeart.
 */
function viewRedeem() {
  const f = S.redeemForm || {};
  const art = S.redeemLock;
  const code = S.id || f.code || '';

  // Von unterwegs kein „Ohne Schutz": so ein Profil öffnet sich nur zu
  // Hause – wer es von unterwegs anlegt, sperrte sich aus.
  const wahl = [['pin', 'PIN'], ['password', tr('Passwort')], ['none', tr('Ohne Schutz')]]
    .filter(([v]) => v !== 'none' || !S.hello?.unterwegs).map(([v, n]) =>
    `<button type="button" class="tab-pill" role="radio" aria-checked="${art === v}"
       tabindex="${art === v ? 0 : -1}" data-lockkind="${v}" style="height:32px;font-size:.875rem">${n}</button>`).join('');

  const geheim = art === 'none' ? `
    <p class="hint">${tr('{schutzOhne} Das passt für den Fernseher im Wohnzimmer, weniger für ein Profil, das nur dir gehört.', { schutzOhne: esc(T.schutzOhne) })}</p>`
    : `<div class="field"><label for="rsecret">${art === 'pin' ? 'PIN' : tr('Passwort')}</label>
        <input id="rsecret" name="secret" type="password"
          inputmode="${art === 'pin' ? 'numeric' : 'text'}" autocomplete="new-password"
          minlength="${art === 'pin' ? 4 : 8}" required
          value="${esc(f.secret || '')}">
        <span class="hint">${art === 'pin'
          ? tr('Mindestens vier Ziffern.') : tr('Mindestens acht Zeichen.')}</span></div>`;

  return `<div class="center"><div class="panel">
    <div style="text-align:center;margin-bottom:8px">
      <h1 class="t-large" style="margin:0">${tr('Du bist eingeladen')}</h1>
      <p class="hint" style="margin-top:8px">${tr('Leg dir ein Profil an. Was du zu sehen bekommst, hat die Person festgelegt, die dich eingeladen hat.')}</p>
    </div>
    <form id="redeemform" class="panel">
      ${S.id ? `<input type="hidden" name="code" value="${esc(code)}">`
        // Kommt der Code aus dem Link, steht er schon fest — kein Feld dafür.
        : `<div class="field"><label for="rcode">${tr('Einladungscode')}</label>
        <input id="rcode" name="code" class="mono" required value="${esc(code)}" autofocus></div>`}
      <div class="field"><label for="rname">${tr('Dein Name')}</label>
        <input id="rname" name="name" autocomplete="name" required
          value="${esc(f.name || '')}" ${S.id ? 'autofocus' : ''}></div>
      <div class="field"><span id="ranmelden" style="font-size:.8125rem;color:var(--label-2)">${tr('Anmelden mit')}</span>
        <div role="radiogroup" aria-labelledby="ranmelden" data-radiogruppe style="display:inline-flex;padding:3px;border-radius:999px;
          background:var(--fill);gap:2px">${wahl}</div></div>
      ${geheim}
      <div class="err">${esc(S.error)}</div>
      <button class="btn btn-primary" style="height:46px">${tr('Profil anlegen')}</button>
    </form>
    <p class="hint" style="text-align:center">
      ${tr('Schon ein Profil?')} <a href="#" class="link" data-go="profiles">${tr('Zur Übersicht')}</a></p>
  </div></div>`;
}

function viewProfiles() {
  // Von unterwegs zeigt der Server keine Profile — sie verrieten die Namen
  // im Haushalt. Angemeldet wird mit Name und PIN oder Passwort.
  if (S.data?.unterwegs) {
    return `<div class="center">
      <form id="unterwegsform" class="panel" style="width:min(340px,100%)">
        <div class="t-title3" style="text-align:center">${tr('Von unterwegs anmelden')}</div>
        <p class="t-caption c-2" style="text-align:center;margin:6px 0 4px">${tr('Mit deinem Profilnamen und deiner PIN oder deinem Passwort. Profile ohne PIN lassen sich nur zu Hause öffnen.')}</p>
        <div class="field"><input id="uname" name="uname" autocomplete="username" placeholder="${tr('Profilname')}" required autofocus></div>
        <div class="field"><input id="usecret" name="usecret" type="password" autocomplete="current-password"
          placeholder="${tr('PIN oder Passwort')}" required></div>
        <div class="err" style="text-align:center">${esc(S.error)}</div>
        <button class="btn btn-primary" style="height:46px">${tr('Anmelden')}</button>
      </form></div>`;
  }
  const list = S.data?.profiles || [];
  const cards = list.map(p => `
    <button class="pcard" data-pick="${esc(p.id)}" data-lock="${esc(p.lockKind)}">
      ${avatar(p, 104)}
      <span class="t-body">${esc(p.name)}${p.lockKind !== 'none' ? ' ' + svg(I.lock, 13, 2.2) : ''}</span>
      ${p.isKid ? `<span class="badge badge--gruen">${esc(T.kinderprofil)}</span>` : ''}
    </button>`).join('');
  return `<div class="pwrap">
    <h1 class="t-large" style="margin:0">${tr('Wer schaut?')}</h1>
    <div class="pgrid">${cards || `<p class="hint">${tr('Noch keine Profile.')}</p>`}</div>
    <div class="err">${esc(S.error)}</div>
  </div>`;
}

function viewUnlock() {
  const p = (S.data?.profiles || []).find(x => x.id === S.id);
  if (!p) return viewProfiles();
  const pin = p.lockKind === 'pin';
  return `<div class="center">
    <button class="btn btn-round sm glass glass--regular" style="position:absolute;left:32px;top:32px;width:44px;height:44px"
      data-go="profiles" aria-label="${tr('Zurück')}">${svg(I.back, 18, 2)}</button>
    ${avatar(p, 96)}
    <form id="unlockform" class="panel" style="width:min(320px,100%)">
      <div class="t-title3" style="text-align:center">${esc(p.name)}</div>
      <div class="field">
        <label for="secret" class="sr-only">${pin ? 'PIN' : tr('Passwort')} ${tr('für {name}', { name: esc(p.name) })}</label>
        <input id="secret" name="secret" type="password" style="text-align:center;letter-spacing:.2em"
          inputmode="${pin ? 'numeric' : 'text'}" autocomplete="${pin ? 'one-time-code' : 'current-password'}"
          placeholder="${pin ? '••••' : tr('Passwort')}" required autofocus></div>
      <div class="err" style="text-align:center" role="alert">${esc(S.error)}</div>
      <button class="btn btn-primary" style="height:46px">${tr('Weiter')}</button>
    </form>
  </div>`;
}

/* ---------------------------------------------------------------- Mediathek */

/* Startseite bei kleiner Bibliothek: „Neu", „Gut bewertet", „Drama",
   „Noch nicht angefangen" zeigten dieselben acht Titel. Eine Reihe, die
   zu über 70 % wiederholt, was weiter oben schon stand, und weniger als
   vier eigene Titel hat, fällt weg. Weiterschauen bleibt immer. */
function reihenOhneWiederholung(rows) {
  const schon = new Set();
  return rows.filter(r => {
    const ids = (r.items || []).map(x => x.id);
    if (r.wide || r.key === 'resume') return true;
    const eigene = ids.filter(id => !schon.has(id)).length;
    if (ids.length && (ids.length - eigene) / ids.length > 0.7 && eigene < 4) return false;
    ids.forEach(id => schon.add(id));
    return true;
  });
}
/* „Neu" auf dem Poster sagt nur etwas, wenn es nicht auf allem steht: In
   der Reihe „Neu dazugekommen" sagt es die Reihe schon, und wo mehr als
   ein Drittel neu ist, fällt die Plakette weg. */
function neuGedaempft(r) {
  const items = r.items || [];
  const neu = items.filter(x => x.neu && !x.finished).length;
  return r.key === 'new' || neu > items.length / 3 ? items.map(x => (x.neu ? { ...x, neu: false } : x)) : items;
}

function viewHome() {
  const rows = reihenOhneWiederholung(S.data?.rows || []).map(r => ({ ...r, items: neuGedaempft(r) }));
  if (!rows.length) {
    return shell(`<div class="wrap"><div style="padding-top:90px;max-width:560px">
      <h1 class="t-large">${tr('Noch nichts da')}</h1>
      <p class="t-body c-2" style="margin-top:12px">
        ${verwaltungHier()
          ? ((S.me.libraries || []).length
            ? tr('Die Bibliotheken sind angelegt, aber noch nicht eingelesen — unter Verwaltung › Bibliotheken siehst du, wie weit es ist.')
            : tr('Lege eine Bibliothek an und zeige Lumen, wo deine Filme liegen.'))
          // Kinderprofil: Es gibt Filme, nur ist noch keiner für dieses
          // Alter freigegeben (oft fehlt die Altersangabe, solange TMDb
          // nicht eingerichtet ist). „Noch nichts da" stimmte dann nicht.
          : S.me?.account?.isKid
            ? tr('Für dein Profil ist noch nichts freigegeben. Frag jemanden, der Lumen verwaltet.')
            : tr('Sobald der Besitzer Filme hinzufügt, erscheinen sie hier.')}</p>
      ${verwaltungHier()
        ? `<button class="btn btn-primary" style="margin-top:22px" data-go="adminLibraries">${(S.me.libraries || []).length ? tr('Zu den Bibliotheken') : tr('Bibliothek anlegen')}</button>` : ''}
    </div></div>`);
  }
  const first = rows[0].items[0];
  // „Ansehen" spielt ab — bei Angefangenem an der gemerkten Stelle, bei
  // einer Serie die Folge, an der man ist. Die Titelseite gibt es daneben.
  const heroFolge = first?.kind === 'episode';
  const heroSek = (first?.resumeMs || 0) / 1000;
  const heroKnopf = !first ? '' : first.kind === 'series'
    ? `<button class="btn btn-primary" data-serie-play="${esc(first.id)}">${PLAY(16)} ${first.gesehen ? tr('Weiterschauen') : tr('Ansehen')}</button>`
    : `<button class="btn btn-primary" data-play="${esc(first.id)}">${PLAY(16)} ${heroSek > 0 && !first.naechsteFolge ? `${tr('Weiter bei')} ${clock(heroSek)}` : tr('Ansehen')}</button>`;
  const hero = first ? `
    <div class="hero${first.backdrop || first.poster || first.frame ? '' : ' hero--ohnebild'}" data-vorschau-hero="${first.id}">
      ${artOf(first, 'wide')}
      <div class="hero-scrim"></div>
      <div class="hero-text">
        <h2 class="t-hero">${esc(heroFolge && first.seriesTitle ? first.seriesTitle : first.title)}</h2>
        <div class="t-sub c-2" style="margin-top:8px">
          ${heroFolge ? esc(folgenName(first)) : [jahre(first), mins(first.runtimeMs), (first.genres || []).slice(0, 2).join(' · '),
             first.imdbRating ? `★ ${first.imdbRating}` : ''].filter(Boolean).join(' · ')}</div>
        ${first.overview ? `<p class="t-body c-2 clamp-2" style="margin-top:10px;max-width:520px">${esc(first.overview)}</p>` : ''}
        <div style="display:flex;gap:12px;margin-top:20px;flex-wrap:wrap">
          ${heroKnopf}
          <button class="btn btn-pill btn-pill--fill" data-go="detail" data-id="${esc(heroFolge && first.seriesId ? first.seriesId : first.id)}">${tr('Mehr Infos')}</button>
        </div>
      </div>
      <button class="btn btn-round sm glass glass--clear hero-ton" data-vorschau-ton aria-label="${tr('Ton der Vorschau')}" aria-pressed="false" hidden>${svg(I.ton, 18, 2)}</button>
    </div>` : '';
  const reihen = S.data?.collections || [];
  const reihenRail = reihen.length ? `
    <section class="sec"><h2 class="t-title3">
      <a href="#" class="titel-link" data-go="collections">${tr('Filmreihen')} ${svg(I.chev, 16, 2.4)}</a></h2>
      <div class="rail">${reihen.slice(0, 12).map(reiheKarte).join('')}</div></section>` : '';
  return shell(`<div class="wrap"><h1 class="sr-only">${tr('Startseite')}</h1>${S.me?.account?.isKid ? bzBand() : ''}${hero}
    ${rows.slice(0, 1).map(r => rail(r.title, r.items, r.wide)).join('')}
    ${reihenRail}
    ${rows.slice(1).map(r => rail(r.title, r.items, r.wide)).join('')}
    <div style="height:40px"></div></div>`);
}

// Sortieren und nach Buchstaben springen.
// Die Wahl gilt je Ansicht und steht im Profil (sort.movie, sort.series
// als „schlüssel:richtung") — so ist sie beim nächsten Mal und auf dem
// nächsten Gerät wieder da. Die Richtung, die eine Sortierung von sich
// aus hat: Titel von A, Hinzugefügt und Erscheinen das Neueste zuerst.
const SORTIER_ART = { movies: 'movie', series: 'series' };
const SORTIER_VORGABE = { title: 'asc', added: 'desc', premiered: 'desc', rating: 'desc', runtime: 'asc',
  lastwatched: 'desc', unwatched: 'asc', random: 'asc' };
const SORTIERUNGEN = serien => [['title', tr('Titel')], ['added', tr('Hinzugefügt')],
  ['premiered', tr('Erscheinungsdatum')], ['rating', tr('Bewertung')], ...(serien ? [] : [['runtime', tr('Laufzeit')]]),
  ['lastwatched', tr('Zuletzt gesehen')], ['unwatched', tr('Ungesehene zuerst')], ['random', tr('Zufällig')]];
const BUCHSTABEN = ['#', ...'ABCDEFGHIJKLMNOPQRSTUVWXYZ'];

function sortierungFuer(view) {
  const gemerkt = (S.me?.prefs || {})['sort.' + SORTIER_ART[view]] || '';
  let [key, dir] = gemerkt.split(':');
  if (!(key in SORTIER_VORGABE) || (key === 'runtime' && view !== 'movies')) key = 'title';
  if (dir !== 'asc' && dir !== 'desc') dir = SORTIER_VORGABE[key];
  return { key, dir };
}

function sortierungWaehlen(view, key, dir) {
  if (!SORTIER_ART[view]) return;
  S.buchstabe = '';
  // merken trägt die Wahl auch gleich in S.me.prefs ein.
  merken('sort.' + SORTIER_ART[view], key + ':' + dir);
  return load();
}

// Die Auswahl: ein Aufklappmenü statt acht Knöpfen — passt auch auf ein
// Telefon — und daneben die Richtung (bei „Zufällig": neu mischen).
function sortierWahl(view) {
  const { key, dir } = sortierungFuer(view);
  const optionen = SORTIERUNGEN(view === 'series').map(([k, n]) =>
    `<option value="${k}"${k === key ? ' selected' : ''}>${esc(n)}</option>`).join('');
  const richtung = dir === 'asc' ? tr('Aufsteigend') : tr('Absteigend');
  const knopf = key === 'random'
    ? `<button type="button" class="sortier-knopf" data-sortmischen aria-label="${tr('Neu mischen')}" title="${tr('Neu mischen')}">${svg(I.mischen, 17, 2)}</button>`
    : `<button type="button" class="sortier-knopf" data-sortrichtung aria-label="${richtung}" title="${richtung}">${svg(dir === 'asc' ? I.aufsteigend : I.absteigend, 17, 2)}</button>`;
  return `<div class="sortier-leiste" role="group" aria-label="${tr('Sortieren')}">
    <span class="sortier-wahl"><select data-sortwahl aria-label="${tr('Sortieren')}">${optionen}</select>${svg(I.runter, 14, 2.2)}</span>${knopf}</div>`;
}

function viewGrid(title) {
  const d = S.data || {};
  const items = d.items || [];
  const genres = d.genres || [];
  const vorhanden = d.buchstaben || {};
  const serien = S.view === 'shows' || S.view === 'series';
  const sortierung = sortierWahl(S.view);
  const { key, dir } = sortierungFuer(S.view);
  const gesamt = d.gesamt ?? items.length;
  // Die Leiste nur nach Titel: bei „Hinzugefügt" springt ein Buchstabe ins
  // Leere. Absteigend von Z nach A, wie das Raster. Und nicht, wenn es gar
  // keine Titel gibt — 27 graue Knöpfe sagen nichts.
  const buchstaben = dir === 'desc' ? [...BUCHSTABEN.slice(1).reverse(), '#'] : BUCHSTABEN;
  const leiste = key === 'title' && (gesamt || S.buchstabe) ? `
    <nav aria-label="${tr('Nach Anfangsbuchstaben')}" class="az-leiste">
      <button class="tab-pill" aria-pressed="${!S.buchstabe}" data-buchstabe="" style="min-width:38px">${tr('Alle')}</button>
      ${buchstaben.map(b => `<button class="tab-pill" aria-pressed="${S.buchstabe === b}" data-buchstabe="${b}"
        style="min-width:30px;padding:0 8px"${vorhanden[b] ? '' : ' disabled'}
        aria-label="${b === '#' ? tr('Zahlen und Zeichen') : tr('Buchstabe {b}', { b })}${vorhanden[b] ? ', ' + tr('{n} Titel', { n: vorhanden[b] }) : ''}">${b}</button>`).join('')}
    </nav>` : '';
  const chips = ['Alle', ...genres].map(g =>
    `<button class="tab-pill" aria-pressed="${S.filter === g}" data-filter="${esc(g)}">${esc(g === 'Alle' ? tr('Alle') : g)}</button>`).join('');
  const gefiltert = S.filter !== 'Alle' || S.buchstabe;
  const leerHtml = gefiltert
    ? leer({ symbol: serien ? I.tv : I.film, titel: tr('Nichts in dieser Auswahl'),
        text: tr('Kein Titel passt zu Genre und Anfangsbuchstabe zusammen.'),
        aktion: `<button class="btn btn-pill btn-pill--fill" data-alle-zeigen>${tr('Alle zeigen')}</button>` })
    : leer({ symbol: serien ? I.tv : I.film, titel: serien ? tr('Noch keine Serien') : tr('Noch keine Filme'),
        text: verwaltungHier() ? tr('Lege unter Verwaltung › Bibliotheken einen Ordner an, dann erscheinen sie hier.')
          : tr('Sobald welche dazukommen, erscheinen sie hier.') });
  return shell(`<div class="wrap wrap--flat">
    <h1 class="t-large">${esc(title)}</h1>
    ${gesamt || gefiltert ? sortierung : ''}
    <div class="t-sub c-2" style="margin:4px 0 20px">${tr('{gesamt} Titel', { gesamt })}${
      S.filter !== 'Alle' ? ' · ' + esc(S.filter) : ''}</div>
    ${genres.length ? `<div role="group" aria-label="${tr('Genre')}" class="chips chips--rollen">${chips}</div>` : ''}
    ${leiste}
    ${items.length ? `<div class="grid" id="raster">${neuGedaempft({ items }).map(posterCard).join('')}</div>` : leerHtml}
    ${d.weiter ? '<div id="mehr" class="loading" style="padding:30px 0"><span class="spin"></span></div>' : ''}
    <div style="height:40px"></div></div>`);
}

/* Beim Herunterscrollen die nächste Seite holen und ans Raster hängen —
   ohne die Seite neu zu zeichnen, sonst spränge man wieder nach oben. */
let mehrBeobachter = null;
function mehrLadenEinrichten() {
  mehrBeobachter?.disconnect();
  mehrBeobachter = null;
  const ziel = document.getElementById('mehr');
  if (!ziel || !('IntersectionObserver' in window)) return;
  let laeuft = false;
  mehrBeobachter = new IntersectionObserver(async eintraege => {
    if (!eintraege.some(e => e.isIntersecting) || laeuft) return;
    const d = S.data;
    if (!d?.weiter) return;
    laeuft = true;
    try {
      const kind = S.view === 'movies' ? 'movie' : 'series';
      const { key, dir } = sortierungFuer(S.view);
      // Bei „Zufällig" mit dem Samen der ersten Seite — sonst käme beim
      // Nachladen eine neue Mischung und Titel doppelt.
      const naechste = await api.seite({ kind, genre: S.filter === 'Alle' ? '' : S.filter,
        sort: key, dir, seed: d.seed || 0, buchstabe: key === 'title' ? S.buchstabe : '', offset: d.offset + d.items.length });
      if (S.data !== d) return; // inzwischen woanders
      d.items.push(...naechste.items);
      d.weiter = naechste.weiter;
      document.getElementById('raster')?.insertAdjacentHTML('beforeend', neuGedaempft({ items: naechste.items }).map(posterCard).join(''));
      if (!d.weiter) { ziel.remove(); mehrBeobachter?.disconnect(); }
    } catch (err) { toast(err.message); }
    finally { laeuft = false; }
  }, { root: document.querySelector('.page'), rootMargin: '600px' });
  mehrBeobachter.observe(ziel);
}

// Favoriten: was einem richtig gut gefallen hat — getrennt nach
// Filmen und Serien. Nur das eigene Profil sieht sie.
function viewFavorites() {
  const items = S.data?.items || [];
  const art = S.favArt || 'movie';
  const tabs = segment(tr('Art'), [['movie', tr('Filme')], ['series', tr('Serien')]].map(([k, n]) =>
    ({ name: n, an: art === k, attr: `data-favart="${k}"` })));
  return shell(`<div class="wrap wrap--flat">
    <h1 class="t-large">${tr('Favoriten')}</h1>
    ${tabs}
    <div class="t-sub c-2" style="margin:4px 0 24px">${offenesProfil() ? tr('Was dir besonders gefallen hat · alle in diesem Profil sehen das') : tr('Was dir besonders gefallen hat · nur du siehst das')}</div>
    ${items.length ? `<div class="grid">${items.map(posterCard).join('')}</div>`
      : leer({ symbol: I.herz, titel: art === 'movie' ? tr('Noch keine Lieblingsfilme') : tr('Noch keine Lieblingsserien'),
          text: tr('Tippe auf einer Titelseite auf das Herz, dann steht der Titel hier.') })}
    <div style="height:40px"></div></div>`);
}

// Reiter über Merkliste, Gesehen und Listen.
function merkReiter(aktiv) {
  // Die Reiter wechseln die Seite — darum aria-current, nicht „gedrückt".
  return `<nav class="segment" aria-label="${tr('Merkliste und Listen')}">${
    [['watchlist', tr('Merkliste')], ['gesehen', tr('Gesehen')], ['playlists', tr('Listen')]].map(([v, n]) =>
      `<button type="button" class="segment-knopf" data-go="${v}"${aktiv === v ? ' aria-current="page"' : ''}>${n}</button>`).join('')}</nav>`;
}

function viewGesehen() {
  const items = S.data?.items || [];
  return shell(`<div class="wrap wrap--flat">
    <h1 class="t-large">${tr('Gesehen')}</h1>
    ${merkReiter('gesehen')}
    <div class="t-sub c-2" style="margin:4px 0 24px">${tr('Was du fertig geschaut hast, zuletzt Beendetes zuerst — Serien, sobald alle Folgen gesehen sind.')} ${offenesProfil() ? tr('Alle in diesem Profil sehen das.') : tr('Nur du siehst das.')}</div>
    ${items.length ? `<div class="grid">${items.map(posterCard).join('')}</div>`
      : leer({ symbol: I.check, titel: tr('Noch nichts zu Ende geschaut'), text: tr('Was du fertig schaust, steht hier — nur für dich.') })}
    <div style="height:40px"></div></div>`);
}

function viewWatchlist() {
  const items = (S.data?.items || []).filter(i => i.onList);
  return shell(`<div class="wrap wrap--flat">
    <h1 class="t-large">${tr('Merkliste')}</h1>
    ${merkReiter('watchlist')}
    <div class="t-sub c-2" style="margin:4px 0 24px">${offenesProfil() ? tr('Alle in diesem Profil sehen diese Liste') : tr('Nur du siehst diese Liste')}</div>
    ${items.length ? `<div class="grid">${items.map(posterCard).join('')}</div>`
      : leer({ symbol: I.mark, titel: tr('Noch nichts gemerkt'), text: tr('Auf einer Titelseite legst du mit „Merken“ etwas hierher.'),
          aktion: `<button class="btn btn-primary" data-go="movies">${tr('Filme ansehen')}</button>` })}
    <div style="height:40px"></div></div>`);
}

function viewSearch() {
  const d = S.data || {};
  const items = d.items || [], personen = d.personen || [], genres = d.genres || [];
  const reihen = d.reihen || [], sammlungen = d.sammlungen || [], folgen = d.folgen || [];
  const q = S.query.trim();
  const de = d.deutung || {};
  const deutung = [de.person ? tr('mit {person}', { person: de.person }) : '', de.genre || '',
    de.jahrVon ? (de.jahrVon === de.jahrBis ? String(de.jahrVon) : `${de.jahrVon}–${de.jahrBis}`) : '',
    de.text ? `„${de.text}“` : ''].filter(Boolean).join(' · ');
  const gedeutet = de.person || de.genre || de.jahrVon;

  const personenHtml = personen.length ? `<section style="margin-bottom:26px">
      <div class="section-h" style="padding-left:0">${tr('Personen')}</div>
      <div class="rail" style="gap:14px">${personen.map(p => `
        <button class="person card" data-go="person" data-id="${esc(p.name)}" aria-label="${tr('Alles mit {name}', { name: esc(p.name) })}">
          <div class="person-bild">${p.bild ? `<img src="${esc(p.bild)}${tokenSuffix(p.bild)}" alt="" loading="lazy" data-weg>` : ''}
            <span>${esc(p.name[0])}</span></div>
          <div class="t-caption clamp-1" style="margin-top:6px">${esc(p.name)}</div>
          <div class="t-caption c-2">${tr('{n} Titel', { n: p.anzahl })}</div>
        </button>`).join('')}</div></section>` : '';
  // Filmreihen und eigene Sammlungen, deren Name passt — „Harry Potter"
  // findet die Reihe, nicht nur die einzelnen Filme.
  const reihenHtml = reihen.length || sammlungen.length ? `<section style="margin-bottom:26px">
      <div class="section-h" style="padding-left:0">${tr('Filmreihen und Sammlungen')}</div>
      <div class="rail">${reihen.map(reiheKarte).join('')}${sammlungen.map(sammlungKarte).join('')}</div></section>` : '';
  // Folgen, deren Titel passt — als breite Karten, Klick spielt ab.
  const folgenHtml = folgen.length ? `<section style="margin-top:30px">
      <div class="section-h" style="padding-left:0">${tr('Folgen')}</div>
      <div class="rail">${folgen.map(wideCard).join('')}</div></section>` : '';
  const genresHtml = genres.length ? `<div style="display:flex;gap:8px;flex-wrap:wrap;margin-bottom:22px">${genres.map(g =>
      `<button class="tab-pill" style="background:var(--fill)" data-genre="${esc(g)}">${esc(g)}</button>`).join('')}</div>` : '';

  // Kein genauer Treffer, aber ein naher: der Server hat mit der nächsten
  // Schreibweise gesucht und sagt, mit welcher.
  const meintest = d.meintest && q ? `<p class="t-body" style="margin:0 0 18px">${tr('Meintest du')}
      <button type="button" class="link-knopf" data-meintest="${esc(d.meintest)}">„${esc(d.meintest)}“</button>?</p>` : '';
  return shell(`<div class="wrap wrap--flat">
    <h1 class="t-large" style="margin:0 0 16px">${tr('Suchen')}</h1>
    <form id="searchform" class="sfield" autocomplete="off" role="search">
      ${svg(I.search, 18, 2)}
      <input id="q" name="q" type="search" enterkeyhint="search" autocapitalize="off" autocorrect="off" spellcheck="false"
        value="${esc(S.query)}" placeholder="${tr('Titel, Schauspieler, Genre, Jahr …')}" aria-label="${tr('Suchen')}">
      ${q ? `<button type="button" class="btn btn-round sm" data-clear aria-label="${tr('Leeren')}">${svg(I.x, 15, 2.2)}</button>` : ''}
    </form>
    <div style="margin-top:28px">
      ${!q ? leer({ symbol: I.search, titel: tr('Wonach suchst du?'), text: tr('Titel, Schauspieler, Genre oder Jahr — auch „Komödie 2019“, „Filme mit Tom Hanks“ oder „Thriller 80er“.') })
        : `${reihenHtml}${personenHtml}${genresHtml}${meintest}
           ${items.length ? `<div class="t-sub c-2" style="margin-bottom:18px">${tr('{n} Treffer', { n: items.length })}${gedeutet ? ' · ' + esc(deutung) : ''}</div>
            <div class="grid">${items.map(posterCard).join('')}</div>${folgenHtml}`
           : folgen.length ? folgenHtml
           : personen.length || genres.length || reihen.length || sammlungen.length ? `<p class="hint">${gedeutet ? tr('Kein Titel passt genau — gesucht wurde {deutung}.', { deutung: esc(deutung) }) : tr('Kein Titel passt genau — aber die Personen und Genres oben.')}</p>`
           : leer({ symbol: I.search, titel: tr('Keine Treffer für „{q}“', { q }),
               text: tr('Prüf die Schreibweise oder versuch es mit einem anderen Wort.'),
               aktion: `<button class="btn btn-pill btn-pill--fill" data-wish="${esc(q)}">${tr('„{q}“ wünschen', { q: esc(q) })}</button>
                 <p class="hint" style="margin:10px 0 0">${tr('Der Besitzer sieht den Wunsch und sonst nichts.')}</p>` })}`}
    </div>
    <div style="height:40px"></div></div>`);
}

/* Neuigkeiten: neue Folgen zu meinen Serien, neue Filme in meinen Reihen,
   neu Dazugekommenes. Alles aus dem eigenen Stand. */
function viewNews() {
  const d = S.data || {};
  const gelesen = d.gelesenBis ? new Date(d.gelesenBis).getTime() : 0;
  const zeile = n => {
    const neu = new Date(n.at).getTime() > gelesen;
    const ziel = n.itemId ? `data-go="detail" data-id="${esc(n.itemId)}"` : n.art === 'server' && verwaltungHier() ? 'data-go="admin"' : 'data-go="home"';
    const symbol = n.art === 'folgen' ? I.tv : n.art === 'reihe' ? I.reihe : I.film;
    return `<button class="row orow-link" ${ziel} style="width:100%;min-height:62px;text-align:left;gap:14px">
      <span style="width:38px;height:38px;border-radius:12px;background:${neu ? 'rgba(10,132,255,.18)' : 'var(--fill)'};color:${neu ? '#4aa3ff' : 'var(--label-2)'};display:inline-flex;align-items:center;justify-content:center;flex:none">${svg(symbol, 18)}</span>
      <span style="flex:1;min-width:0">
        <span class="t-body clamp-1" style="display:block${neu ? ';font-weight:600' : ''}">${esc(n.titel)}</span>
        <span class="t-caption c-2 clamp-1" style="display:block">${esc(n.text)} · ${new Date(n.at).toLocaleDateString(sprache(), { day: 'numeric', month: 'short' })}</span></span>
      ${neu ? `<span class="sr-only">${tr(', neu')}</span>` : ''}${svg(I.chev, 14, 2.4)}</button>`;
  };
  const liste = d.eintraege || [];
  return shell(`<div class="wrap wrap--flat" style="max-width:760px">
    <h1 class="t-large">${tr('Neuigkeiten')}</h1>
    <div class="t-sub c-2" style="margin:4px 0 20px">${tr('Neue Folgen deiner Serien, neue Filme deiner Reihen, neu Dazugekommenes — nur für dich.')}</div>
    <div class="group group--inset">${liste.map(zeile).join('') || `<p class="hint" style="padding:18px 16px;margin:0">${tr('Nichts Neues — Lumen meldet sich, sobald eine Serie, die du schaust, neue Folgen bekommt.')}</p>`}</div>
    <div style="height:40px"></div></div>`);
}

/* Alles mit einer Person — die Seite hinter dem Porträt in der Suche. */
// Personenseite: Foto, Lebensdaten, Biografie mit Quelle — und
// darunter, worin die Person in der eigenen Mediathek mitspielt.
function viewPerson() {
  const items = S.data?.items || [];
  const p = S.data?.info || {};
  // Lebensdaten in der Schreibweise der Sprache; nur ein Jahr bleibt ein Jahr.
  const tag = d => { if (!d) return ''; const [j, , t] = d.split('-'); return t ? datum(d + 'T12:00:00', { day: 'numeric', month: 'long', year: 'numeric' }) : j; };
  const leben = [p.geboren ? (p.geburtsort ? tr('geboren {tag} in {ort}', { tag: tag(p.geboren), ort: esc(p.geburtsort) }) : tr('geboren {tag}', { tag: tag(p.geboren) })) : '',
    p.gestorben ? tr('gestorben {tag}', { tag: tag(p.gestorben) }) : ''].filter(Boolean).join(' · ');
  const quellen = (p.quellen || []).map(q => `<a href="${esc(nurHttp(q.url))}" target="_blank" rel="noopener noreferrer">${esc(q.name)}</a>`).join(', ');
  return shell(`<div class="wrap wrap--flat">
    <button class="btn btn-pill btn-pill--fill" style="height:32px;font-size:.8125rem;gap:6px" data-back>${svg(I.back, 16, 2)}${tr('Zurück')}</button>
    <div style="display:flex;gap:24px;align-items:flex-start;margin-top:14px;flex-wrap:wrap">
      ${p.bild ? `<img src="${esc(p.bild)}${tokenSuffix(p.bild)}" alt="" data-weg
        style="width:150px;height:200px;object-fit:cover;border-radius:16px;flex:none;background:var(--fill)">` : ''}
      <div style="flex:1;min-width:260px;max-width:760px">
        <h1 class="t-large" style="margin:0">${esc(S.id || '')}</h1>
        ${leben ? `<div class="t-sub c-2" style="margin-top:4px">${leben}</div>` : ''}
        ${p.bio ? `<p class="t-body c-2 clamp-6" style="margin:12px 0 0;line-height:1.5" id="bio">${esc(p.bio)}</p>
          ${p.bio.length > 500 ? `<button class="btn" style="padding:4px 0;color:var(--accent)" data-bioganz>${tr('Mehr lesen')}</button>` : ''}` : ''}
        ${quellen ? `<div class="t-caption c-3" style="margin-top:10px">${tr('Quelle: {quellen}', { quellen })}${p.bioSprache && p.bioSprache !== sprache().slice(0, 2) ? ' · ' + tr('Biografie nur auf {sprache} verfügbar', { sprache: esc(sprachName(p.bioSprache)) }) : ''}</div>` : ''}
      </div>
    </div>
    <div class="t-sub c-2" style="margin:22px 0 20px">${tr('{n} Titel in deiner Mediathek', { n: items.length })}</div>
    <div class="grid">${items.map(posterCard).join('') || `<p class="hint">${tr('Zu dieser Person gibt es hier keine Titel.')}</p>`}</div>
    <div style="height:40px"></div></div>`);
}


// Dateigrösse einheitlich: ab 1 GB mit einer Nachkommastelle, darunter MB.
function groesse(bytes) {
  if (!bytes) return '';
  return bytes >= 1e9
    ? (bytes / 1e9).toLocaleString(sprache(), { maximumFractionDigits: 1 }) + ' GB'
    : Math.max(1, Math.round(bytes / 1e6)) + ' MB';
}

function fassungName(f) {
  // Breitformat zählt nach der Breite: 1920×800 ist ein 1080p-Film.
  const h = f.height || 0, w = f.width || 0;
  const q = h >= 2000 || w >= 3400 ? '4K' : h >= 1000 || w >= 1800 ? '1080p' : h >= 700 || w >= 1200 ? '720p' : h ? `${h}p` : '';
  const gb = groesse(f.sizeBytes);
  return [q, (f.container || '').toUpperCase(), gb].filter(Boolean).join(' · ');
}

function fassungsWahl(it, fassungen, resumeSec) {
  const admin = S.me?.account?.isAdmin;
  const zeile = (f, i) => `<div class="row" style="min-height:56px;gap:12px">
      <button class="btn" style="flex:1;justify-content:flex-start;gap:12px;padding:0;text-align:left"
        data-play="${esc(it.id)}" data-file="${esc(f.id)}"${resumeSec > 60 ? '' : ' data-from="0"'} title="${esc(f.path || '')}">
        <span style="display:flex;width:30px;height:30px;border-radius:50%;background:var(--fill);align-items:center;justify-content:center;flex:none">${PLAY(12)}</span>
        <span><span class="t-body">${esc(fassungName(f))}</span>
          ${i === 0 ? `<span class="t-caption c-2"> ${tr('· Standard')}</span>` : ''}</span></button>
      ${resumeSec > 60 ? `<button class="btn btn-pill" style="height:28px;font-size:.75rem" data-play="${esc(it.id)}" data-file="${esc(f.id)}" data-from="0">${tr('Von vorn')}</button>` : ''}
      ${admin && i > 0 ? `<button class="btn btn-pill" style="height:28px;font-size:.75rem" data-fassungstandard="${esc(it.id)}:${esc(f.id)}"
          title="${tr('Diese Qualität für alle als Standard nehmen')}">${tr('Als Standard')}</button>` : ''}
    </div>`;
  return `<div class="group group--inset listen-panel" style="max-width:560px;margin-top:14px" role="group" aria-label="${tr('Qualität wählen')}">
    <div class="row" style="min-height:40px"><span class="t-caption c-2">${tr('In welcher Qualität?')}</span></div>
    ${fassungen.map(zeile).join('')}</div>`;
}

function viewDetail() {
  const d = S.data;
  if (!d) return shell(`<div class="wrap">${loading()}</div>`);
  const it = d.item;
  const files = d.files || [];
  // Mehrere Fassungen desselben Films (MKV in 1080p, AVI in 400p): Sie
  // sind ein Film. „Ansehen" fragt dann zuerst nach der Qualität
  // (Auflösung, Format, Grösse). Oben steht die Standard-Fassung — die vom
  // Besitzer gewählte, sonst die mit der höchsten Auflösung. Teile eines
  // Films (CD1, CD2, a/b) sind keine Fassungen.
  const fassungen = files.filter(f => !f.missing && (f.partNo || 1) === 1);
  const playable = files.find(f => !f.missing);
  const resumeSec = it.resumeMs ? it.resumeMs / 1000 : 0;

  const meta = [
    jahre(it), it.ageYears != null ? tr('ab {n}', { n: it.ageYears }) : null, mins(it.runtimeMs),
    (it.genres || []).join(' · '),
    it.imdbRating ? `IMDb ${it.imdbRating}${it.imdbVotes ? ` (${it.imdbVotes.toLocaleString(sprache())})` : ''}` : null,
  ].filter(Boolean).join(' · ');
  // Rotten Tomatoes: Anteil guter Kritiken. Ab 60 % „frisch" (Tomate),
  // darunter „faul" — ein brauner Klecks, kein grüner Punkt, der wie
  // „gut" aussähe.
  const tomaten = it.rottenTomatoes == null ? '' : `${meta ? ' · ' : ''}<span class="rt">${it.rottenTomatoes >= 60
    ? '<span aria-hidden="true">🍅</span>'
    : `<svg class="rt-faul" width="14" height="14" viewBox="0 0 24 24" aria-hidden="true"><path fill="currentColor" d="M12 3c1.2 2.4 3.6 1.6 4.6 3.4 1 1.8-.6 3 .8 4.6 1.4 1.6 3.6 1.4 3.6 3.6 0 2.2-2.6 2.2-3.6 3.8-1 1.6.2 3.6-2.2 3.6-2.4 0-2-2-3.2-2s-1.2 2-3.4 1.6C6.4 21.2 7.6 19 6.2 18c-1.4-1-4.2-.6-4.2-2.8 0-2.2 2.6-2 3-3.8.4-1.8-2.2-3.2-.8-4.8 1.4-1.6 3.4.2 4.8-.8C10.4 4.8 10.8 3 12 3z"/></svg>`}
    ${tr('{rottenTomatoes} % Rotten Tomatoes', { rottenTomatoes: it.rottenTomatoes })}${it.rottenTomatoes >= 60 ? '' : ` <span class="sr-only">${tr('(faul)')}</span>`}</span>`;

  // Die Technik-Zeile: was die Datei kann — Auflösung, HDR, Ton,
  // Sprachen, Untertitel, und ob dieses Gerät sie direkt abspielt.
  const technik = (() => {
    if (!playable && !it.qualitaet) return '';
    const spuren = playable?.tracks || [];
    // Bild-Untertitel zählen mit: der Player bietet sie an (eingebrannt).
    const ton = spuren.filter(t => t.kind === 'audio'), text = spuren.filter(t => t.kind === 'subtitle' && (!t.bitmap || !t.external));
    const kanaele = Math.max(0, ...ton.map(t => t.channels || 0));
    const sprachen = [...new Set(ton.map(t => (t.language || 'und').toUpperCase()).filter(x => x !== 'UND'))];
    const utSprachen = [...new Set(text.map(t => (t.language || 'und').toUpperCase()).filter(x => x !== 'UND'))];
    const q = it.qualitaet || '';
    const hdr = it.hdr === 'DV' ? 'Dolby Vision' : it.hdr || '';
    const kan = kanaele || it.kanaele || 0;
    const abz = [q, hdr, kan > 2 ? `${kan - 1}.1` : kan === 2 ? tr('Stereo') : '',
      sprachen.length ? sprachen.join(' · ') : ''].filter(Boolean).map(x => `<span class="tech">${esc(x)}</span>`);
    if (text.length) abz.push(`<span class="tech">${svg(I.untertitel, 13, 1.8)}${tr('Untertitel')}${utSprachen.length ? ' ' + esc(utSprachen.join(' · ')) : ''}</span>`);
    if (d.plan) abz.push(d.plan.direct ? `<span class="tech tech--gut">${tr('Direkt abspielbar')}</span>` : `<span class="tech tech--leise">${tr('Wird umgewandelt')}</span>`);
    return abz.length ? `<div class="technik" aria-label="${tr('Technische Daten')}">${abz.join('')}</div>` : '';
  })();

  const stars = [1, 2, 3, 4, 5].map(n =>
    `<button class="btn btn-round stern" data-rate="${n}" aria-label="${tr('{n} von 5 Sternen', { n })}"
      aria-pressed="${(it.myRating || 0) === n}" style="color:${(it.myRating || 0) >= n ? '#ffd60a' : 'var(--label-3)'}">★</button>`).join('');

  // Staffeln als Reiter, darunter die Folgen der gewählten — wie man es
  // von Emby kennt: Bild, Nummer und Name, Länge, kurze Beschreibung,
  // und was schon gesehen ist, trägt den Haken.
  const seasons = (d.seasons || []).map(s =>
    `<button class="tab-pill" aria-pressed="${d.staffel === s.id}" data-season="${esc(s.id)}">
      ${(s.seasonNo || 0) === 0 ? tr('Specials') : esc(s.title)}</button>`).join('');

  const episodes = (d.episodes || []).map(e => `
    <div class="orow folge" style="height:auto;padding:12px 16px;gap:14px">
      <button class="card folge-bild" data-play="${esc(e.id)}" aria-label="${esc(tr('{titel} ansehen', { titel: e.title }))}">
        <div class="thumb" style="position:relative;width:160px;height:90px;border-radius:8px;overflow:hidden;flex:none">
          ${artOf(e, 'wide')}
          <span class="folge-play" aria-hidden="true">${PLAY(22)}</span>
          ${e.progress > 1 && !e.finished ? `<div class="progress progress--onart"><i style="width:${e.progress}%"></i></div>` : ''}
        </div>
      </button>
      <div style="flex:1;min-width:0">
        <div class="t-body" style="display:flex;align-items:center;gap:8px">
          <span class="c-2 tnum" style="flex:none">${e.episodeNoEnd > e.episodeNo ? `${e.episodeNo}–${e.episodeNoEnd}` : e.episodeNo}</span>
          <span class="clamp-1">${esc(e.title)}</span>
          ${e.finished ? `<span class="c-2" style="flex:none;display:inline-flex" title="${tr('gesehen')}">${svg(I.check, 14, 2.6)}</span>` : ''}
        </div>
        <div class="t-caption c-2">${[mins(e.runtimeMs), e.premiered ? new Date(e.premiered).toLocaleDateString(sprache()) : ''].filter(Boolean).join(' · ')}</div>
        ${e.overview ? `<p class="t-sub c-2 clamp-2" style="margin-top:4px">${esc(e.overview)}</p>` : ''}
      </div>
      <button class="btn btn-pill btn-pill--fill nur-breit" style="flex:none" data-play="${esc(e.id)}">
        ${e.progress > 1 && !e.finished ? tr('Weiter') : tr('Ansehen')}</button>
      ${folgeBearbeiten(e)}
      <button class="btn btn-round sm folge-haken${e.finished ? ' folge-haken--an' : ''}" style="flex:none"
        data-folge-gesehen="${esc(e.id)}" aria-pressed="${!!e.finished}"
        aria-label="${esc(e.title)}: ${e.finished ? tr('gesehen') : tr('als gesehen markieren')}"
        title="${e.finished ? tr('Gesehen – nochmal tippen zum Zurücknehmen') : tr('Als gesehen markieren')}">${svg(I.check, 16, 2.6)}</button>
    </div>`).join('');

  // Die gewählte Staffel: ganz gesehen oder nicht — ein Knopf für alle Folgen darin.
  const staffelJetzt = (d.seasons || []).find(x => x.id === d.staffel);
  const staffelFertig = (d.episodes || []).length > 0 && d.episodes.every(e => e.finished);
  const staffelKnopf = staffelJetzt && (d.episodes || []).length ? `
    <button class="btn btn-pill" style="height:32px;font-size:.8125rem" data-staffel-gesehen="${esc(staffelJetzt.id)}" aria-pressed="${staffelFertig}">
      ${svg(I.check, 14, 2.6)} ${staffelFertig ? tr('Staffel nicht mehr als gesehen') : tr('Staffel als gesehen markieren')}</button>` : '';
  // Nach einem Haken an einer späten Folge: auch die davor?
  const vorherFrage = d.vorherigeFrage ? `
    <div class="vorher-frage surface" role="status">
      <span class="t-sub" style="flex:1;min-width:180px">${tr('Auch alle Folgen davor als gesehen markieren?')}</span>
      <button class="btn btn-pill btn-pill--fill" style="height:32px;font-size:.8125rem" data-vorherige-ja="${esc(d.vorherigeFrage)}">${tr('Ja, alle davor')}</button>
      <button class="btn btn-pill" style="height:32px;font-size:.8125rem" data-vorherige-nein>${tr('Nein')}</button>
    </div>` : '';
  // „Weiterschauen" auf der Serienseite: die Folge nach der zuletzt
  // geschauten — der Server weiss sie (dieselbe wie auf der Startseite).
  const w = d.weiter;
  const serienKnopfText = !w ? tr('Ansehen')
    // Alles gesehen: nicht „Weiter mit S1 F1", sondern ehrlich von vorn.
    : d.item.finished && !(w.resumeMs > 0) ? tr('Nochmal ansehen')
    : w.resumeMs > 0 ? `${tr('Weiter bei')} ${clock(w.resumeMs / 1000)} · ${sf(w.seasonNo, w.episodeNo)}`
    : (d.item.gesehen || 0) > 0 ? tr('Weiter mit S{seasonNo} F{episodeNo}', { seasonNo: w.seasonNo, episodeNo: w.episodeNo }) : tr('Ansehen');

  // Zu jeder Datei, was drin ist: Bild, Ton, Untertitel, Laufzeit,
  // Bitrate — wie bei Emby unter „Medieninfo".
  const kanaele = n => ({ 1: tr('Mono'), 2: tr('Stereo'), 6: '5.1', 8: '7.1' }[n] || (n ? tr('{n} Kanäle', { n }) : ''));
  const spracheN = l => {
    if (!l || l === 'und') return '';
    try { return new Intl.DisplayNames([sprache()], { type: 'language' }).of(l) || l; } catch { return l; }
  };
  const codecN = c => ({ h264: 'H.264', hevc: 'HEVC', h265: 'HEVC', mpeg4: 'MPEG-4', av1: 'AV1', vp9: 'VP9', mpeg2video: 'MPEG-2',
    aac: 'AAC', ac3: tr('Dolby Digital'), eac3: tr('Dolby Digital Plus'), truehd: tr('Dolby TrueHD'), dts: 'DTS', mp3: 'MP3', flac: 'FLAC', opus: tr('Opus'),
    subrip: 'SRT', ass: 'ASS', mov_text: tr('Text'), webvtt: 'WebVTT', hdmv_pgs_subtitle: tr('PGS (Bild)'), dvd_subtitle: tr('DVD (Bild)') }[c] || (c || '').toUpperCase());
  const spur = t => [spracheN(t.language), codecN(t.codec), t.kind === 'audio' ? kanaele(t.channels) : '',
    t.forced ? tr('erzwungen') : '', t.hearingImpaired ? tr('für Hörgeschädigte') : '', t.external ? tr('eigene Datei') : '',
    t.title && !/^(stereo|surround|5\.1|2\.0)$/i.test(t.title) ? `„${t.title}“` : ''].filter(Boolean).join(' · ');
  const fileRows = files.map(f => {
    const ton = (f.tracks || []).filter(t => t.kind === 'audio');
    const ut = (f.tracks || []).filter(t => t.kind === 'subtitle');
    const zeile = (titel, inhalt) => inhalt ? `<div style="display:flex;gap:12px"><span class="c-3" style="width:86px;flex:none">${titel}</span>
      <span class="c-2" style="flex:1;min-width:0">${inhalt}</span></div>` : '';
    return `
    <div class="datei-info" style="padding:12px 16px;display:flex;flex-direction:column;gap:5px">
      <span class="mono c-2" style="overflow-wrap:anywhere">${/* Verwalter bekommen vom Server den vollen Pfad, alle anderen nur den Dateinamen */ ''}${esc(f.path)}${f.missing ? ' · ' + tr('fehlt') : ''}</span>
      <div class="t-caption" style="display:flex;flex-direction:column;gap:3px;margin-top:4px">
        ${zeile(tr('Bild'), [f.width && f.height ? `${f.width}×${f.height}` : (f.height ? f.height + 'p' : ''), codecN(f.videoCodec), f.hdr].filter(Boolean).join(' · '))}
        ${zeile(tr('Ton'), ton.map(t => esc(spur(t))).join('<br>'))}
        ${zeile(tr('Untertitel'), ut.map(t => esc(spur(t))).join('<br>'))}
        ${zeile(tr('Datei'), [(f.container || '').toUpperCase(), groesse(f.sizeBytes),
          f.bitrate ? (f.bitrate / 1e6).toLocaleString(sprache(), { maximumFractionDigits: 1 }) + ` ${tr('Mbit/s')}` : '',
          f.durationMs ? clock(f.durationMs / 1000) : ''].filter(Boolean).join(' · '))}
      </div>
    </div>`;
  }).join('');

  return shell(`<div class="wrap">
    <div class="hero">
      ${artOf(it, 'wide')}
      <div class="hero-scrim"></div>
      <button class="btn btn-round sm glass glass--regular" style="position:absolute;top:28px;width:44px;height:44px;z-index:3"
        data-back data-heldzurueck aria-label="${tr('Zurück')}">${svg(I.back, 18, 2)}</button>
      <div class="hero-text">
        ${it.logo
          // Der Schriftzug aus der Sammlung („logo.png") statt des Titels als
          // Text; für Vorleseprogramme bleibt der Titel der Name.
          ? `<h1 class="t-hero hero-logo" style="margin:0"><img src="${esc(it.logo)}${tokenSuffix(it.logo)}" alt="${esc(it.title)}"
              style="display:block;max-width:min(460px,70%);max-height:150px;width:auto;height:auto;object-fit:contain;object-position:left bottom"
              data-ersatz="${esc(it.title)}"></h1>`
          : `<h1 class="t-hero">${esc(it.title)}</h1>`}
        ${it.originalTitle && it.originalTitle !== it.title
          ? `<div class="t-sub c-2" style="margin-top:4px">${esc(it.originalTitle)}</div>` : ''}
        ${d.nfo?.tagline ? `<div class="t-sub" style="margin-top:6px;font-style:italic">${esc(d.nfo.tagline)}</div>` : ''}
        <div class="t-sub c-2" style="margin-top:8px">${esc(meta)}${tomaten}</div>
        ${technik}
        <div style="display:flex;gap:12px;margin-top:22px;flex-wrap:wrap">
          ${playable ? (fassungen.length > 1
            // Mehrere Qualitäten: „Ansehen" fragt zuerst, welche.
            ? `<button class="btn btn-primary" data-fassungmenu="${esc(it.id)}" aria-expanded="${S.fassungMenu === it.id}">
                ${PLAY(16)} ${resumeSec > 60 ? `${tr('Weiter bei')} ${clock(resumeSec)}` : tr('Ansehen')}
                <span style="opacity:.6;font-weight:500;margin-left:2px">${tr('· {n} Qualitäten', { n: fassungen.length })}</span></button>`
            // „Ansehen" heisst von vorn: Unter einer Minute zeigt der Knopf
            // keine Stelle an — dann darf er auch nicht still dort weitermachen.
            : `<button class="btn btn-primary" data-play="${it.id}" data-file="${esc(playable.id)}"${resumeSec > 60 ? '' : ' data-from="0"'}>
                ${PLAY(16)} ${resumeSec > 60 ? `${tr('Weiter bei')} ${clock(resumeSec)}` : tr('Ansehen')}</button>`) : ''}
          ${it.kind === 'series' && (d.seasons || []).length ? `<button class="btn btn-primary" data-serie-play="${esc(it.id)}">
              ${PLAY(16)} ${esc(serienKnopfText)}</button>` : ''}
          ${'' /* Eine Hauptaktion (Ansehen), daneben runde Symbole mit kurzer
                 Beschriftung — wie in der TV-App von Apple. Vorher standen
                 sechs gleich grosse Kapseln da, am Handy auf drei Zeilen. */}
          <div class="aktionen">
          ${resumeSec > 60 && fassungen.length <= 1 ? `<button class="aktion" data-play="${it.id}" data-file="${esc(playable?.id || '')}" data-from="0">
              <span class="aktion-rund">${svg(I.vonVorn, 18, 2)}</span><span class="aktion-text">${tr('Aktion|Von vorn')}</span></button>` : ''}
          <button class="aktion" data-list="${it.onList ? 'off' : 'on'}" aria-pressed="${!!it.onList}">
            <span class="aktion-rund">${svg(it.onList ? I.check : I.plus, 18, 2.2)}</span><span class="aktion-text">${it.onList ? tr('Gemerkt') : tr('Merken')}</span></button>
          ${it.kind === 'movie' || it.kind === 'series' ? `<button class="aktion herz-knopf" data-fav="${it.favorite ? 'off' : 'on'}"
              aria-pressed="${!!it.favorite}"><span class="aktion-rund">${it.favorite
                ? svg(I.herz, 18, 2).replace('fill="none"', 'fill="currentColor"') : svg(I.herz, 18, 2)}</span><span class="aktion-text">${tr('Favorit')}</span></button>` : ''}
          ${it.finished
            ? `<button class="aktion" data-fokus="gesehen" data-unseen aria-pressed="true" aria-label="${tr('Gesehen')}" title="${tr('Nicht mehr als gesehen')}">
                <span class="aktion-rund">${svg(I.check, 18, 2.6)}</span><span class="aktion-text">${tr('Gesehen')}</span></button>`
            : `<button class="aktion" data-fokus="gesehen" data-seen aria-pressed="false" aria-label="${tr('Gesehen')}" title="${tr('Als gesehen markieren')}">
                <span class="aktion-rund">${svg(I.check, 18, 2.6)}</span><span class="aktion-text">${tr('Gesehen')}</span></button>`}
          ${it.kind === 'movie' || it.kind === 'series'
            ? `<button class="aktion" data-zurliste aria-expanded="false">
                <span class="aktion-rund">${svg(I.liste, 18, 2)}</span><span class="aktion-text">${tr('Zur Liste')}</span></button>` : ''}
          ${(() => {
            // Herunterladen: die Fassung, die „Ansehen" spielt, sonst die erste.
            const dl = d.download?.erlaubt ? (d.download.dateien || []) : [];
            const datei = dl.find(x => x.fileId === playable?.id) || dl[0];
            if (!datei) return '';
            const gb = datei.groesse ? `${(datei.groesse / 1e9).toLocaleString(sprache(), { maximumFractionDigits: 1 })} GB` : '';
            return `<a class="aktion" href="${esc(api.downloadURL(datei.fileId))}" download
              title="${esc(gb ? tr('Originaldatei herunterladen ({gb})', { gb }) : tr('Originaldatei herunterladen'))}">
              <span class="aktion-rund">${svg(I.laden, 18, 2)}</span><span class="aktion-text">${tr('Herunterladen')}</span></a>`;
          })()}
          </div>
          ${detailUntertitel(it, files)}
        </div>
      </div>
    </div>

    ${S.fassungMenu === it.id && fassungen.length > 1 ? fassungsWahl(it, fassungen, resumeSec) : ''}
    <div id="listenpanel"></div>
    ${verwaltungHier() && (it.kind === 'movie' || it.kind === 'series') ? `
      <div style="display:flex;gap:10px;flex-wrap:wrap;margin-top:18px;align-items:center">
        <div class="mehr-huelle">
          <button class="btn btn-pill btn-pill--fill" data-mehrmenue aria-haspopup="menu" aria-expanded="false"
            aria-controls="titel-mehr" aria-label="${tr('Verwaltung: weitere Aktionen')}" title="${tr('Verwaltung')}">${svg('<circle cx="5" cy="12" r="1.3"/><circle cx="12" cy="12" r="1.3"/><circle cx="19" cy="12" r="1.3"/>', 18, 2)} ${tr('Verwaltung')}</button>
          <div class="mehr-menue popover glass" id="titel-mehr" role="menu" aria-label="${tr('Verwaltung')}" hidden>
            <button role="menuitem" tabindex="-1" class="mehr-eintrag" data-titelneu="${esc(it.id)}">${tr('Metadaten neu laden')}</button>
            <button role="menuitem" tabindex="-1" class="mehr-eintrag" data-zuweisen="${esc(it.id)}">${tr('Anderen Titel zuweisen')}</button>
            ${detailVerwaltung(it, true)}
          </div></div>
        ${it.matchState === 'manual' ? `<span class="t-caption c-2">${tr('von Hand zugewiesen')}</span>`
          : it.matchState === 'lokal' ? `<span class="t-caption c-2">${tr('aus der NFO-Datei neben der Datei')}</span>`
          : it.matchState === 'unmatched' ? `<span class="t-caption" style="color:#ffb340">${tr('nicht erkannt — Lumen war sich nicht sicher')}</span>` : ''}
      </div>
      <div id="zuweisen"></div>` : ''}
    <div class="detail-spalten${it.overview || (d.cast || []).length || seasons ? '' : ' detail-spalten--schmal'}" style="margin-top:34px">
      <div>
        ${it.overview ? `<p class="t-body" style="max-width:640px;line-height:1.6">${esc(it.overview)}</p>` : ''}
        ${nfoAngaben(d.nfo)}
        ${(d.cast || []).length ? `<section style="margin-top:26px"><div class="section-h" style="padding-left:0">${tr('Besetzung')}</div>
          <div class="rail" style="gap:14px">${d.cast.map(p => `
            <button class="person card" data-go="person" data-id="${esc(p.name)}" aria-label="${tr('Alles mit {name}', { name: esc(p.name) })}">
              <div class="person-bild">${p.bild
                ? `<img src="${esc(p.bild)}${tokenSuffix(p.bild)}" alt="" loading="lazy" data-weg>` : ''}
                <span>${esc((p.name || '?')[0])}</span></div>
              <div class="t-caption clamp-1" style="margin-top:6px">${esc(p.name)}</div>
              ${p.rolle ? `<div class="t-caption c-2 clamp-1">${esc(p.rolle)}</div>` : ''}
            </button>`).join('')}</div></section>` : ''}
        ${extrasAbschnitt(it, d.extras)}
        ${seasons ? `<section style="margin-top:28px">
            <div style="display:flex;gap:10px;align-items:center;flex-wrap:wrap">
              <div role="group" aria-label="${tr('Staffel')}" class="chips" style="display:inline-flex;padding:3px;border-radius:999px;background:var(--fill);gap:2px;max-width:100%;overflow-x:auto">${seasons}</div>${staffelBearbeiten(d.staffel)}
              ${staffelKnopf}</div>
            ${vorherFrage}
            <div class="group group--inset" style="margin-top:14px">${episodes
              || `<p class="hint" style="padding:18px 16px;margin:0">${tr('In dieser Staffel liegt noch keine Folge.')}</p>`}</div></section>` : ''}
      </div>
      <div>
        <section><div class="section-h">${tr('Deine Bewertung')}</div>
          <div class="surface" style="padding:14px 16px;border-radius:14px;display:flex;align-items:center;gap:4px">
            ${stars}
            <span style="flex:1"></span>
            ${it.myRating ? `<button class="btn btn-pill" style="height:28px;font-size:.75rem" data-rate="0">${tr('Löschen')}</button>` : ''}
          </div>
          <p class="t-caption c-2" style="margin:8px 16px 0">${(S.me?.prefs || {}).shareRatings === '1'
            ? tr('Im Haushalt sichtbar, mit deinem Namen – abschalten unter „Ich“ › Privatsphäre.')
            : tr('Nur für dich sichtbar.')}</p></section>
        ${(d.sharedRatings || []).length ? `<section style="margin-top:22px"><div class="section-h">${tr('Im Haushalt')}</div>
            <div class="group group--inset">${d.sharedRatings.map(r => `
              <div class="row" style="min-height:44px">
                ${avatar(r, 26)}
                <span class="t-body" style="flex:1">${esc(r.name)}</span>
                <span style="color:#ffd60a">${'★'.repeat(r.stars)}</span></div>`).join('')}</div></section>` : ''}
        ${fileRows ? `<details class="technik-klapp" style="margin-top:22px"${verwaltungHier() ? ' open' : ''}>
            <summary class="section-h">${tr('Technische Angaben')}</summary>
            <div class="group group--inset">${fileRows}</div></details>` : ''}
        ${(d.links || []).length ? `<section style="margin-top:22px"><div class="section-h">${tr('Links')}</div>
            <div class="t-body" style="padding:0 16px;display:flex;flex-wrap:wrap;gap:6px 14px">${d.links
              .filter(l => /^https:\/\//.test(l.url))
              .map(l => `<a href="${esc(l.url)}" target="_blank" rel="noopener noreferrer">${esc(l.name)}</a>`).join('')}</div></section>` : ''}
      </div>
    </div>
    ${d.collection ? `
      <section class="sec"><h2 class="t-title3">
        <a href="#" class="titel-link" data-go="collection" data-id="${esc(d.collection.id)}">
          ${esc(d.collection.name)} ${svg(I.chev, 16, 2.4)}</a>
        <span class="t-sub c-2" style="font-weight:400;margin-left:6px">${tr('{n} Filme', { n: d.collection.anzahl })}</span></h2>
        <div class="rail">${d.collection.items.map(x => posterCard(x, x.id === it.id)).join('')}</div></section>` : ''}
    ${(d.similar || []).length ? `<section class="sec" style="margin-top:34px">
        <h2 class="t-title3">${tr('Ähnliche Titel')}</h2>
        <div class="rail">${d.similar.map(x => posterCard(x)).join('')}</div></section>` : ''}
    <div style="height:40px"></div></div>`);
}

/* Was die NFO-Datei ausserdem weiss: Regie, Drehbuch, Studio, Land,
   Schlagworte — als kurze Zeilen unter der Beschreibung. */
function nfoAngaben(n) {
  if (!n) return '';
  const zeilen = [[tr('Regie'), n.regie], [tr('Drehbuch'), n.drehbuch], [tr('Studio'), n.studios],
    [tr('Land'), n.laender], [tr('Schlagworte'), n.tags]].filter(([, v]) => (v || []).length);
  if (!zeilen.length) return '';
  return `<dl class="t-sub nfo-angaben" style="margin:18px 0 0;display:grid;grid-template-columns:max-content 1fr;gap:4px 14px;max-width:640px">
    ${zeilen.map(([k, v]) => `<dt class="c-3">${k}</dt><dd class="c-2" style="margin:0">${v.map(esc).join(', ')}</dd>`).join('')}</dl>`;
}

/* Extras aus den Ordnern neben dem Film (Trailer, Featurettes …). Sie
   laufen direkt im Browser, in einem eigenen kleinen Fenster — der grosse
   Player gehört dem Film. */
const EXTRA_ART = () => ({ trailer: tr('Trailer'), featurette: tr('Featurette'), behindthescenes: tr('Hinter den Kulissen'),
  deleted: tr('Entfallene Szene'), interview: tr('Interview'), scene: tr('Szene'), short: tr('Kurzfilm'), clip: tr('Ausschnitt'), other: tr('Extra') });
function extrasAbschnitt(it, extras) {
  if (!(extras || []).length) return '';
  const art = EXTRA_ART();
  return `<section style="margin-top:26px"><div class="section-h" style="padding-left:0">${tr('Extras · {n}', { n: extras.length })}</div>
    <div class="group group--inset">${extras.map(x => `
      <button class="row orow-link" style="width:100%;min-height:52px;text-align:left;gap:12px" data-extra="${esc(x.url)}" data-extra-titel="${esc(x.titel || art[x.art] || art.other)}">
        <span style="display:flex;color:var(--label-2)">${PLAY(16)}</span>
        <span class="t-body clamp-1" style="flex:1;min-width:0">${esc(x.titel || art[x.art] || art.other)}</span>
        ${x.titel ? `<span class="t-caption c-2" style="flex:none">${esc(art[x.art] || art.other)}</span>` : ''}</button>`).join('')}</div></section>`;
}
function extraAbspielen(url, titel) {
  document.getElementById('extra-fenster')?.remove();
  const d = document.createElement('dialog');
  d.id = 'extra-fenster';
  d.className = 'surface';
  d.setAttribute('aria-label', titel);
  d.style.cssText = 'padding:0;border:0;border-radius:14px;max-width:min(960px,94vw);width:100%;background:#000;color:#fff';
  d.innerHTML = `<div style="display:flex;align-items:center;gap:10px;padding:10px 14px">
      <span class="t-body clamp-1" style="flex:1">${esc(titel)}</span>
      <button class="btn btn-round sm" data-extra-zu aria-label="${tr('Schliessen')}">${svg(I.x, 16, 2.2)}</button></div>
    <video controls autoplay playsinline style="display:block;width:100%;max-height:78vh;background:#000" src="${esc(url)}${tokenSuffix(url)}"></video>
    <p class="t-caption" data-extra-fehler hidden style="padding:10px 14px;margin:0;color:#ffb340">${tr('Dieses Extra kann der Browser nicht direkt abspielen.')}</p>`;
  document.body.appendChild(d);
  const v = d.querySelector('video');
  v.addEventListener('error', () => { d.querySelector('[data-extra-fehler]').hidden = false; });
  const zu = () => { v.pause(); v.removeAttribute('src'); v.load(); d.close(); d.remove(); };
  d.addEventListener('click', e => { if (e.target === d || e.target.closest('[data-extra-zu]')) zu(); });
  d.addEventListener('cancel', e => { e.preventDefault(); zu(); });
  d.showModal();
}

/* ---------------------------------------------------------------- Player */

/* ---------------------------------------------------------------- Ton und Untertitel
 *
 * Die Datei bringt ihre Spuren mit (eingebettet oder als .srt daneben).
 * Die Tonspur wählt der Server beim Umpacken (?spur=), Untertitel kommen
 * als WebVTT in ein <track>. Was jemand wählt, merkt sich Lumen als
 * Sprache fürs nächste Mal — pro Konto, nicht pro Film. */
// Namen der Sprachen kommen aus Intl.DisplayNames – in der Sprache der
// Oberfläche („Deutsch" / „German"), ohne eigene Liste.

/* Sagt der Titel der Spur nur, was ohnehin dasteht — Sprache, „erzwungen",
   „Standard" —, fällt er weg. Sonst hiess es „Deutsch (Deutsch (erzwungen),
   erzwungen)". */
function nurSpracheUndMerkmale(titel, name, code) {
  let rest = ` ${String(titel).toLowerCase()} `;
  for (const w of [name, code, sprachName(code || ''), 'forced', 'erzwungen', tr('erzwungen'), 'default', 'standard', 'sdh', 'cc', 'full', 'subtitles', 'subtitle', 'untertitel', 'stereo', 'surround']) {
    if (!w) continue;
    rest = rest.split(String(w).toLowerCase()).join(' ');
  }
  return !rest.replace(/[\s()\[\]\-–—_.,:;/|]+/g, '');
}

function spurName(t) {
  let name = t.language && t.language !== 'und' ? sprachName(t.language) : tr('Unbekannt');
  const z = [];
  if (t.title && !nurSpracheUndMerkmale(t.title, name, t.language)) z.push(t.title);
  if (t.forced) z.push(tr('erzwungen'));
  if (t.hearingImpaired) z.push(tr('für Hörgeschädigte'));
  // Bild-Untertitel (PGS, DVD) rechnet der Server ins Bild — die Wahl
  // startet den Strom neu, darum steht es dabei.
  if (t.kind === 'subtitle' && t.bitmap) z.push(tr('ins Bild gerechnet'));
  if (t.kind === 'audio' && t.channels > 2) z.push(`${t.channels - 1}.1`);
  if (t.external) z.push(tr('Datei'));
  return z.length ? `${name} (${z.join(', ')})` : name;
}
const tonspuren = p => (p.tracks || []).filter(t => t.kind === 'audio');
const textspuren = p => (p.tracks || []).filter(t => t.kind === 'subtitle' && !t.bitmap);
/* Bild-Untertitel: nur eingebettete — eine .sup-Datei daneben kann der
   Server nicht einbrennen (wiedergabe.go nimmt nur !External). */
const bildspuren = p => (p.tracks || []).filter(t => t.kind === 'subtitle' && t.bitmap && !t.external);
const alleUntertitel = p => [...textspuren(p), ...bildspuren(p)];
/* Welcher Bild-Untertitel eingebrannt wird (-1: keiner) — steht in der
   Adresse des Stroms, also ändert eine Wahl hier den Strom. */
const einbrennenVon = p => (p.untertitel != null && bildspuren(p).some(t => t.index === p.untertitel) ? p.untertitel : -1);

/* Vorbelegen nach den gemerkten Sprachen: Ton in der gewünschten Sprache,
   Untertitel nur, wenn jemand sie will. Erzwungene Untertitel laufen
   immer mit, wenn die Tonsprache nicht die Untertitelsprache ist — dafür
   sind sie da. */
/* Ton und Untertitel merkt sich Lumen je Serie: Wer einen Anime auf
   Japanisch mit deutschen Untertiteln schaut, will das in der nächsten
   Folge wieder — aber nicht im nächsten Film. Die Wahl im Profil bleibt
   der Rückfall für alles andere. */
const serienSpurKey = id => 'spuren:' + id;
function serienSpuren(seriesId) {
  if (!seriesId) return null;
  try { return JSON.parse((S.me?.prefs || {})[serienSpurKey(seriesId)] || 'null'); } catch { return null; }
}
function spurenMerken(p, teil) {
  if (p.seriesId) {
    const neu = { ...(serienSpuren(p.seriesId) || {}), ...teil };
    merken(serienSpurKey(p.seriesId), JSON.stringify(neu));
    return;
  }
  if (teil.ton !== undefined) merken('tonSprache', teil.ton);
  if (teil.ut !== undefined) merken('untertitel', teil.ut);
}

function spurenVorbelegen(p) {
  const serie = serienSpuren(p.seriesId) || {};
  const profil = S.me?.prefs || {};
  const prefs = {
    tonSprache: serie.ton ?? profil.tonSprache,
    untertitel: serie.ut ?? profil.untertitel,
  };
  const ton = tonspuren(p), text = textspuren(p), bild = bildspuren(p);
  if (prefs.tonSprache) {
    const t = ton.find(x => x.language === prefs.tonSprache);
    if (t) p.spur = t.index;
  }
  // Text vor Bild: Text stellt der Browser selbst dar, Bild muss der
  // Server neu rechnen. Bild nur, wenn es die Sprache als Text nicht gibt.
  if (prefs.untertitel && prefs.untertitel !== 'aus') {
    const u = text.find(x => x.language === prefs.untertitel && !x.forced)
      || text.find(x => x.language === prefs.untertitel)
      || bild.find(x => x.language === prefs.untertitel && !x.forced)
      || bild.find(x => x.language === prefs.untertitel);
    if (u) p.untertitel = u.index;
  } else {
    // Erzwungene in der Tonsprache laufen mit — so hielt es der Server
    // bisher von sich aus mit Bild-Untertiteln; jetzt sagt es die App
    // ausdrücklich, damit die Spurwahl zeigt, was läuft.
    const aktuell = ton.find(x => x.index === p.spur) || ton[0];
    const passt = x => x.forced && aktuell && x.language && x.language === aktuell.language;
    const erzwungen = text.find(passt) || bild.find(passt);
    if (erzwungen) p.untertitel = erzwungen.index;
  }
}

/* Kapitel, wie der Player sie anbietet: mindestens zwei, nach Beginn
   sortiert, ohne winzige Reste (manche Rips haben ein 0-Sekunden-Kapitel
   am Ende). */
function kapitelListe(file) {
  const k = (file?.chapters || []).filter(c => c.endMs - c.startMs >= 1000)
    .sort((a, b) => a.startMs - b.startMs);
  return k.length >= 2 ? k : [];
}

/* Kleine Striche auf der Zeitleiste, wo ein Kapitel beginnt. */
function kapitelMarken(p, total) {
  if (!p.kapitel?.length || !total) return '';
  return p.kapitel.filter(k => k.startMs > 0 && k.startMs / 1000 < total)
    .map(k => `<s style="left:${(k.startMs / 10 / total).toFixed(2)}%"></s>`).join('');
}

function spurenPanel(p) {
  const ton = tonspuren(p);
  const zeile = (aktiv, attr, name) => `
    <button class="spur${aktiv ? ' spur--aktiv' : ''}" ${attr} role="menuitemradio" aria-checked="${aktiv}">
      <span class="spur-name">${esc(name)}</span>${aktiv ? svg('<path d="M4 12.5 9.5 18 20 6.5"/>', 18, 2.6) : ''}</button>`;
  const abschnitt = (titel, inhalt) => `<div class="spuren-abschnitt"><div class="spuren-titel">${titel}</div>${inhalt}</div>`;
  let html = '';
  if (p.spurenOffen === 'folgen') {
    return `<div class="spuren spuren--folgen" role="group" aria-label="${tr('Folgen')}">${folgenPanel(p)}</div>`;
  }
  if (p.spurenOffen === 'kapitel') {
    const jetzt = (p.offset + p.current) * 1000;
    const aktiv = p.kapitel.reduce((a, k, i) => (k.startMs <= jetzt + 500 ? i : a), 0);
    return `<div class="spuren spuren--kapitel" role="menu" aria-label="${tr('Kapitel')}">${abschnitt(tr('Kapitel'),
      p.kapitel.map((k, i) => zeile(i === aktiv, `data-kapitel="${k.startMs}"`, `${clock(k.startMs / 1000)}  ${k.title || tr('Kapitel {n}', { n: i + 1 })}`)).join(''))}</div>`;
  }
  if (p.spurenOffen === 'tempo') {
    return `<div class="spuren" role="menu" aria-label="${tr('Wiedergabetempo')}">${abschnitt(tr('Tempo'),
      TEMPI.map(t => zeile((p.tempo || 1) === t, `data-tempo="${t}"`, tempoText(t))).join(''))}</div>`;
  }
  if (p.spurenOffen === 'untertitel') {
    html = abschnitt(tr('Untertitel'),
      zeile(p.untertitel == null, 'data-untertitel="aus"', tr('Aus')) +
      alleUntertitel(p).map(t => zeile(p.untertitel === t.index, `data-untertitel="${t.index}"`, spurName(t))).join('') +
      (p.utVorbereitung ? `<div class="spuren-hinweis" role="status">${tr('Untertitel werden vorbereitet …')}${p.utVorbereitung.fortschritt > 0 ? ` ${Math.round(p.utVorbereitung.fortschritt * 100)} %` : ''}</div>` : '') +
      playerUntertitelEintrag(p));
  } else {
    if (ton.length > 1) {
      html = abschnitt(tr('Tonspur'),
        ton.map((t, i) => zeile(p.spur === t.index || (p.spur < 0 && i === 0), `data-spur="${t.index}"`, spurName(t))).join(''));
    }
    if (S.me?.account?.isAdmin && p.kind === 'episode' && p.seriesId) {
      const m = p.introMarke;
      const vh = p.vonHand || {};
      const knopf = (attr, text) => `<button class="spur" ${attr}><span class="spur-name">${text}</span></button>`;
      const stand = p.intro ? `<div class="spuren-hinweis">${p.intro.quelle === 'kapitel' ? tr('Aus den Kapiteln der Datei') : p.intro.quelle === 'automatisch' ? tr('Automatisch erkannt') : tr('Markiert')}: ${clock(p.intro.vonMs / 1000)}–${clock(p.intro.bisMs / 1000)}</div>`
        : (vh.folgeVorspann === 'keiner' || vh.serieVorspann === 'keiner') ? `<div class="spuren-hinweis">${tr('Kein Vorspann (von Hand)')}</div>` : '';
      html += abschnitt(tr('Vorspann für alle'), stand + (m == null
        ? knopf('data-intro-von', tr('Vorspann beginnt hier'))
        : `<div class="spuren-hinweis">${tr('Beginn bei {zeit} gemerkt', { zeit: clock(m) })}</div>
           ${knopf('data-intro-bis', tr('… und endet hier'))}
           ${knopf('data-intro-abbruch', tr('Abbrechen'))}`)
        + (vh.folgeVorspann !== 'keiner' ? knopf('data-vonhand="intro:folge:keiner"', tr('Kein Vorspann bei dieser Folge')) : '')
        + (vh.serieVorspann !== 'keiner' ? knopf('data-vonhand="intro:serie:keiner"', tr('Kein Vorspann bei dieser Serie')) : ''));
      const abStand = p.creditsMs ? `<div class="spuren-hinweis">${tr('Abspann ab {zeit}', { zeit: clock(p.creditsMs / 1000) })}</div>`
        : (vh.folgeAbspann === 'keiner' || vh.serieAbspann === 'keiner') ? `<div class="spuren-hinweis">${tr('Kein Abspann (von Hand)')}</div>` : '';
      html += abschnitt(tr('Abspann für alle'), abStand
        + knopf('data-vonhand="abspann:folge:hier"', tr('Abspann beginnt hier (diese Folge)'))
        + (vh.folgeAbspann !== 'keiner' ? knopf('data-vonhand="abspann:folge:keiner"', tr('Kein Abspann bei dieser Folge')) : '')
        + (vh.serieAbspann !== 'keiner' ? knopf('data-vonhand="abspann:serie:keiner"', tr('Kein Abspann bei dieser Serie')) : '')
        + (vh.folgeVorspann || vh.folgeAbspann ? knopf('data-vonhand="weg:folge"', tr('Markierungen dieser Folge entfernen')) : '')
        + (vh.serieVorspann || vh.serieAbspann ? knopf('data-vonhand="weg:serie"', tr('Markierungen der Serie entfernen')) : ''));
    }
  }
  return `<div class="spuren" role="menu">${html}</div>`;
}

/* Vorspann und Abspann für die Datei, die läuft: aus jeDatei, wenn der
   Server die Folge in mehreren Fassungen kennt, sonst wie bisher. */
function vorspannFuer(d, fileId) {
  const jd = d.jeDatei?.[fileId];
  return jd ? { intro: jd.intro || null, creditsMs: jd.creditsMs || 0 }
    : { intro: d.intro || null, creditsMs: d.creditsMs || 0 };
}

/* Nach einer Änderung von Hand: die Zeiten neu vom Server holen — was
   gilt, entscheidet er (Folge vor Serie, Kapitel, Erkanntes). */
async function vorspannNeuLaden(p) {
  const d = await api.item(p.itemId).catch(() => null);
  if (!d || S.playing !== p) return;
  Object.assign(p, vorspannFuer(d, (p.teile ? p.teile[0] : p.fileId)), { vonHand: d.vonHand || null });
}

/* Die Knöpfe „Kein Vorspann …", „Abspann beginnt hier" usw. im Menü der
   Verwaltung. art:wo:was, etwa „intro:serie:keiner". */
async function vonHandSetzen(wert) {
  const p = S.playing;
  if (!p) return;
  const [art, wo, was] = wert.split(':');
  const id = wo === 'serie' ? p.seriesId : p.itemId;
  try {
    if (art === 'weg') {
      await api.adminClearIntro(id);
      await api.adminClearAbspann(id);
    } else if (art === 'intro') {
      await api.adminKeinIntro(id);
    } else if (was === 'hier') {
      await api.adminSetAbspann(id, Math.round((p.offset + p.current) * 1000));
    } else {
      await api.adminKeinAbspann(id);
    }
    await vorspannNeuLaden(p);
    toast(art === 'weg' ? tr('Markierung entfernt') : tr('Gespeichert'));
    patchChrome();
  } catch (err) { toast(err.message); }
}

/* Die Folgen im Player: Staffeln als Pillen, darunter die Folgen mit
   Bild, Haken und Balken; die laufende hell. Geladen beim ersten Öffnen. */
function folgenPanel(p) {
  const f = p.folgen;
  if (!f) { folgenLaden(p); return `<div class="spuren-titel">${tr('Folgen')}</div><div class="loading" style="padding:20px"><span class="spin"></span></div>`; }
  const pillen = f.seasons.map(st => `<button class="tab-pill" role="tab" aria-selected="${f.staffel === st.id}" data-pl-staffel="${esc(st.id)}" style="height:28px;font-size:.8125rem">
      ${(st.seasonNo || 0) === 0 ? tr('Specials') : esc(st.title)}</button>`).join('');
  const zeilen = (f.episodes || []).map(e => {
    const aktuell = e.id === p.itemId;
    return `<button class="folge-zeile${aktuell ? ' folge-zeile--aktuell' : ''}" data-pl-folge="${esc(e.id)}" ${aktuell ? 'aria-current="true"' : ''}>
      <span class="folge-thumb">${artOf(e, 'wide')}
        ${e.progress > 1 && !e.finished ? `<span class="progress progress--onart"><i style="width:${e.progress}%"></i></span>` : ''}</span>
      <span style="flex:1;min-width:0">
        <span class="t-body" style="display:flex;gap:8px;align-items:center${aktuell ? ';font-weight:600' : ''}">
          <span class="tnum" style="opacity:.6;width:18px;flex:none">${e.episodeNo}</span><span class="clamp-1">${esc(e.title)}</span>
          ${e.finished ? svg(I.check, 14, 2.6) : ''}</span>
        <span class="t-caption" style="display:block;opacity:.6;margin-top:2px">${aktuell ? tr('läuft') : mins(e.runtimeMs) || ''}</span></span>
    </button>`;
  }).join('');
  return `<div class="spuren-kopf"><span class="spuren-titel" style="flex:1;padding:0">${tr('Folgen')}</span>
      <span style="display:flex;gap:4px;flex-wrap:wrap;justify-content:flex-end">${pillen}</span></div>
    <div class="folgen-liste">${zeilen || `<div class="spuren-hinweis">${tr('Keine Folgen')}</div>`}</div>`;
}

async function folgenLaden(p, staffelId) {
  if (p.folgenLaedt) return;
  p.folgenLaedt = true;
  try {
    let f = p.folgen;
    if (!f) {
      const serie = p.seriesId ? await api.item(p.seriesId) : null;
      const seasons = serie?.seasons || [];
      f = { seasons, staffel: p.parentId, episodes: [] };
    }
    f.staffel = staffelId || f.staffel;
    f.episodes = await api.children(f.staffel).catch(() => []);
    p.folgen = f;
  } finally { p.folgenLaedt = false; }
  p.spurenHtml = '';
  patchChrome();
  // Zur laufenden Folge scrollen.
  app.querySelector('.folge-zeile--aktuell')?.scrollIntoView({ block: 'center' });
}

/* Untertitel ins Video legen.
 *
 * Nicht als <track>: der Strom beginnt nach einem Sprung nicht bei 0,
 * sondern an der Sprungstelle (ffmpeg -ss), und die Zeiten in der
 * VTT-Datei gelten für die ganze Datei. Darum lädt Lumen die Datei einmal,
 * liest die Einträge selbst und legt sie um die Startstelle versetzt in
 * eine eigene Textspur — bei jedem neuen Strom neu versetzt. */
function vttLesen(text) {
  const zeit = t => { const m = t.trim().match(/^(?:(\d+):)?(\d+):(\d+)[.,](\d+)/); if (!m) return null;
    return (m[1] ? Number(m[1]) * 3600 : 0) + Number(m[2]) * 60 + Number(m[3]) + Number(m[4].padEnd(3, '0').slice(0, 3)) / 1000; };
  const out = [];
  for (const block of text.replace(/\r/g, '').split(/\n\n+/)) {
    const zeilen = block.split('\n');
    const i = zeilen.findIndex(z => z.includes('-->'));
    if (i < 0) continue;
    const [a, b] = zeilen[i].split('-->');
    const von = zeit(a), bis = zeit(b);
    if (von == null || bis == null) continue;
    const inhalt = zeilen.slice(i + 1).join('\n').replace(/<[^>]+>/g, '').trim();
    if (inhalt) out.push({ von, bis, text: inhalt });
  }
  return out;
}

/* Eine Textspur holen. Eingebettete Untertitel zieht der Server beim
   ersten Mal aus der Datei; dauert das, antwortet er 202 mit
   { code: 'wird_vorbereitet', fortschritt } und Retry-After. Dann nach der
   genannten Zeit noch einmal fragen — mit Hinweis im Player —, bis die
   Spur da ist, ein Fehler kommt oder jemand anders wählt. Gibt den Text
   zurück, oder null (abgebrochen oder gescheitert). */
async function untertitelHolen(p, idx) {
  const nochGewollt = () => S.playing === p && p.untertitel === idx;
  const hinweis = stand => {
    p.utVorbereitung = stand;
    p.spurenHtml = '';
    patchChrome();
  };
  let gemeldet = false;
  try {
    for (let versuch = 0; versuch < 200; versuch++) {
      let r;
      try { r = await fetch(api.subtitleURL(p.fileId, idx)); }
      catch { throw new Error(tr('Der Server ist nicht erreichbar.')); }
      if (!nochGewollt()) return null;
      if (r.ok && r.status !== 202) return await r.text();
      let info = {};
      try { info = await r.json(); } catch {}
      if ((r.status === 202 || r.status === 503) && info.code === 'wird_vorbereitet') {
        // Einmal Bescheid geben; danach steht der Stand still im Menü.
        if (!gemeldet) { toast(tr('Untertitel werden vorbereitet …')); gemeldet = true; }
        hinweis({ fortschritt: Number(info.fortschritt) || 0 });
        const sek = Math.min(30, Math.max(1, Number(r.headers.get('Retry-After')) || 3));
        await new Promise(res => setTimeout(res, sek * 1000));
        if (!nochGewollt()) return null;
        continue;
      }
      throw new Error(info.error ? info.error.charAt(0).toUpperCase() + info.error.slice(1) + '.' : tr('Diese Untertitel lassen sich nicht lesen.'));
    }
    throw new Error(tr('Die Untertitel brauchen zu lange.'));
  } catch (err) {
    if (nochGewollt()) { toast(err.message); p.untertitel = null; p.spurenHtml = ''; patchChrome(); }
    return null;
  } finally {
    if (p.utVorbereitung) hinweis(null);
  }
}

async function untertitelAnwenden() {
  const p = S.playing, v = $('#v');
  if (!p || !v) return;
  if (!p.textspur) { p.textspur = v.addTextTrack('subtitles', tr('Untertitel'), sprache().slice(0, 2)); }
  const spur = p.textspur;
  // Jeder Aufruf bekommt eine Nummer; nur der jüngste darf Zeilen setzen.
  // Zwei überlappende Aufrufe (Start und gleich danach ein Sprung, bevor
  // die .vtt da war) legten sonst jede Zeile doppelt hin.
  const nr = p.utLauf = (p.utLauf || 0) + 1;
  const leeren = () => { for (const c of [...(spur.cues || [])]) spur.removeCue(c); };
  // Aus, oder ein Bild-Untertitel — der steckt schon im Bild des Stroms.
  if (p.untertitel == null || einbrennenVon(p) >= 0) { leeren(); spur.mode = 'disabled'; return; }
  const gewuenscht = p.untertitel;
  if (p.cuesFuer !== gewuenscht) {
    const text = await untertitelHolen(p, gewuenscht);
    if (S.playing !== p || p.untertitel !== gewuenscht || nr !== p.utLauf) return;
    if (text == null) return;
    p.cues = vttLesen(text); p.cuesFuer = gewuenscht;
  }
  if (S.playing !== p || p.untertitel !== gewuenscht || nr !== p.utLauf) return;
  // Erst jetzt, direkt vor dem Setzen, leeren — nach dem Warten.
  leeren();
  for (const c of p.cues) {
    const von = c.von - p.offset, bis = c.bis - p.offset;
    if (bis <= 0) continue;
    spur.addCue(new VTTCue(Math.max(0, von), bis, c.text));
  }
  spur.mode = 'showing';
  untertitelLage();
}

/* Untertitel über der Steuerleiste: Ist die Leiste zu sehen, rücken die
   Zeilen über sie (wie bei Netflix und YouTube) — vorher standen sie
   hinter den Zeiten bzw. am Handy zwischen den Knöpfen. */
function untertitelLage() {
  const p = S.playing, v = $('#v');
  const spur = p?.textspur;
  if (!p || !v || !spur || spur.mode !== 'showing' || !spur.cues) return;
  let linie = 'auto';
  if (p.ui) {
    const bot = app.querySelector('.pl-bot')?.getBoundingClientRect();
    const vr = v.getBoundingClientRect();
    if (bot && vr.height) linie = Math.max(10, Math.min(92, Math.floor((bot.top - vr.top) / vr.height * 100) - 2));
  }
  if (p.utLinie === linie && p.utLinieZahl === spur.cues.length) return;
  p.utLinie = linie; p.utLinieZahl = spur.cues.length;
  for (const c of [...spur.cues]) {
    if (linie === 'auto') { c.snapToLines = true; c.line = 'auto'; }
    else { c.snapToLines = false; c.line = linie; c.lineAlign = 'end'; }
  }
}

async function spurWaehlen(idx) {
  const p = S.playing;
  if (!p || p.spur === idx) return;
  p.spur = idx;
  const t = tonspuren(p).find(x => x.index === idx);
  if (t?.language) spurenMerken(p, { ton: t.language });
  // Der Ton steckt im Strom: neu öffnen, an derselben Stelle.
  const pos = p.offset + p.current;
  await openStream(pos);
  patchChrome();
}

function untertitelWaehlen(wert) {
  const p = S.playing;
  if (!p) return;
  const vorher = einbrennenVon(p);
  p.untertitel = wert === 'aus' ? null : Number(wert);
  const t = alleUntertitel(p).find(x => x.index === p.untertitel);
  if (t) p.letzteUntertitel = t.index;
  spurenMerken(p, { ut: t ? (t.language || 'und') : 'aus' });
  // Ein Bild-Untertitel steckt im Strom: an- oder abwählen heisst, den
  // Strom an der aktuellen Stelle neu zu holen (mit ?untertitel=<index>
  // bzw. -1). Textspuren legt die App selbst darüber.
  if (einbrennenVon(p) !== vorher) {
    openStream(p.offset + p.current).then(() => patchChrome());
  } else {
    untertitelAnwenden();
  }
  p.spurenHtml = '';
  patchChrome();
}

/* Neue Untertitel aus „Untertitel suchen": Spurenliste ersetzen und die
   neue Spur gleich einschalten. */
function spurenNachHolen(tracks, neu) {
  const p = S.playing;
  if (!p || !Array.isArray(tracks) || !tracks.length) return;
  p.tracks = tracks;
  p.cuesFuer = null;
  if (neu != null && neu >= 0) p.untertitel = neu;
  p.spurenOffen = false;
  p.spurenHtml = '';
  untertitelAnwenden();
  patchChrome();
}

function merken(key, wert) {
  if (!S.me) return;
  S.me.prefs = { ...(S.me.prefs || {}), [key]: wert };
  api.updateMe({ prefs: { [key]: wert } }).catch(() => {});
}

/* Der Player.
 *
 * Unten eine Leiste wie bei Apple TV: Zeitleiste (ein echter Schieberegler,
 * per Tab und Pfeiltasten bedienbar), Zeiten, rechts Folgen, Untertitel,
 * Ton, Tempo, Lautstärke und Vollbild. Ausgeblendet wird die Leiste nur,
 * wenn der Film läuft und nichts darin den Tastaturfokus hat — dann aber
 * richtig (visibility + inert), damit Tab nicht in Unsichtbares springt. */
const TEMPI = [0.5, 0.75, 1, 1.25, 1.5, 2];
// Tempo mit dem Dezimalzeichen der Sprache: 1,5× / 1.5×.
const tempoZahl = t => zahl(t, { maximumFractionDigits: 2 }) + '×';
const tempoText = t => (t === 1 ? tr('Normal') : tempoZahl(t));
const vollbildDa = () => !!(document.fullscreenEnabled || document.webkitFullscreenEnabled);
const imVollbild = () => !!(document.fullscreenElement || document.webkitFullscreenElement);
// Auf Geräten ohne Maus (iPhone, iPad) regelt die Hardware die Lautstärke;
// iOS ignoriert volume ohnehin. Dort nur der Stumm-Knopf.
const lautRegler = () => matchMedia('(hover: hover) and (pointer: fine)').matches;
// Bild im Bild: Standard-API (Chrome, Edge, Safari 13.1+) oder die ältere
// von Safari. Firefox hat keine, dort fehlt der Knopf.
const bildImBildDa = () => !!(document.pictureInPictureEnabled || HTMLVideoElement.prototype.webkitSetPresentationMode);
async function bildImBildUmschalten() {
  const v = $('#v'), p = S.playing;
  if (!v || !p) return;
  try {
    if (document.pictureInPictureElement) await document.exitPictureInPicture();
    else if (v.webkitPresentationMode === 'picture-in-picture') v.webkitSetPresentationMode('inline');
    else if (document.pictureInPictureEnabled && v.requestPictureInPicture) {
      await v.requestPictureInPicture();
      // Chrome zeigt Text-Untertitel im kleinen Fenster nicht.
      if (p.textspur?.mode === 'showing') toast(tr('Im Bild-im-Bild-Fenster erscheinen die Untertitel nicht.'));
    } else if (v.webkitSetPresentationMode) v.webkitSetPresentationMode('picture-in-picture');
  } catch { toast(tr('Bild im Bild ist hier nicht möglich.')); }
}
function bildImBildVerlassen() {
  if (document.pictureInPictureElement) document.exitPictureInPicture().catch(() => {});
}

/* Film in Teilen (cd1, cd2): Die Leiste zeigt den ganzen Film, nicht nur
   den laufenden Teil — „Weiter bei 1:43" auf der Titelseite findet man so
   im Player wieder. Intern bleibt alles je Teil (offset, duration). */
const filmVor = p => (p.teile ? p.vorher || 0 : 0);
const filmDauer = p => (p.teile && p.gesamt ? p.gesamt : p.duration || 0);
const filmPos = p => filmVor(p) + p.offset + p.current;
/* Ein Sprung auf der Leiste, in Sekunden des ganzen Films. Liegt das Ziel
   in einem anderen Teil, startet dieser an der richtigen Stelle. */
function filmSprung(sek) {
  const p = S.playing;
  if (!p) return;
  if (!p.teile) return seekTo(sek);
  const vor = filmVor(p);
  if (sek >= vor && sek < vor + (p.duration || 0) - 1) return seekTo(sek - vor);
  const v = $('#v');
  if (v) { v.pause(); v.removeAttribute('src'); v.load(); }
  S.playing = null;
  startPlayer(p.itemId, Math.max(0, Math.min(sek, filmDauer(p) - 2)));
}

function zeitleisteText(pos, total) {
  return total ? tr('{pos} von {gesamt}', { pos: clock(pos), gesamt: clock(total) }) : clock(pos);
}

function viewPlayer() {
  const p = S.playing;
  if (!p) return viewHome();
  const hid = p.ui ? '' : ' pl-hide';
  const inert = p.ui ? '' : ' inert';
  const pos = filmPos(p);
  const total = filmDauer(p);
  const pct = total ? Math.min(100, pos / total * 100) : 0;
  const v = $('#v');
  const stumm = v ? v.muted : !!p.stumm;
  const laut = v ? v.volume : (p.lautstaerke ?? 1);

  const prefs = S.me?.prefs || {};
  return `<div class="player${p.ui ? '' : ' pl-nocursor'} ut-${prefs.utGroesse || 'normal'}${prefs.utHintergrund === '0' ? ' ut-ohne' : ''}"
    role="dialog" aria-modal="true" aria-label="${esc(tr('{titel} – Wiedergabe', { titel: p.title }))}">
    <video id="v" class="pl-video" playsinline autoplay
      style="position:absolute;inset:0;width:100%;height:100%;background:#000;object-fit:contain"></video>
    <div class="pl-surface" data-surface style="position:absolute;inset:0;z-index:2"></div>
    <div class="pl-scrim${hid}" style="pointer-events:none"></div>

    <div class="pl-top${hid}"${inert}>
      <button class="btn btn-round sm glass glass--clear pl-zu" data-stop aria-label="${tr('Schliessen')}">
        ${svg(I.back, 18, 2)}</button>
      <div style="min-width:0;flex:1">
        <div class="t-body clamp-1">${esc(p.title)}</div>
        ${p.sub ? `<div class="t-caption c-2 clamp-1">${esc(p.sub)}</div>` : ''}
      </div>
      ${p.mode === 'transcode'
        ? `<span class="badge pl-umw">${tr('Wird umgewandelt')}</span>` : ''}
    </div>
    ${p.spurenOffen ? spurenPanel(p) : ''}

    <div class="pl-mid${hid}"${inert}>
      <button class="btn btn-round glass glass--clear" style="width:56px;height:56px" data-seek="-10" aria-label="${tr('10 Sekunden zurück')}">${svg(I.back10, 24, 2)}</button>
      <button class="btn btn-round glass glass--regular" style="width:76px;height:76px" data-toggle
        aria-label="${p.paused ? tr('Abspielen') : tr('Pause')}">${p.paused ? PLAY(28) : PAUSE(28)}</button>
      <button class="btn btn-round glass glass--clear" style="width:56px;height:56px" data-seek="10" aria-label="${tr('10 Sekunden vor')}">${svg(I.fwd10, 24, 2)}</button>
    </div>

    <div class="pl-bot${hid}"${inert}>
      <div class="pl-knoepfe">
        <span class="pl-laut-gruppe">
          <button class="btn btn-round sm glass glass--clear pl-knopf" data-stumm aria-pressed="${stumm}"
            aria-label="${stumm ? tr('Ton an') : tr('Stumm')}">${svg(stumm || laut === 0 ? I.stumm : I.lautsprecher, 20, 2)}</button>
          ${lautRegler() ? `<input type="range" class="pl-laut" data-laut min="0" max="1" step="0.05" value="${stumm ? 0 : laut}"
            aria-label="${tr('Lautstärke')}" aria-valuetext="${Math.round((stumm ? 0 : laut) * 100)} %">` : ''}
        </span>
        <span style="flex:1"></span>
        ${p.kind === 'episode' ? `<button class="btn btn-pill glass glass--clear pl-knopf pl-knopf--text" data-naechste-folge>${svg(I.naechste, 18, 2)}<span>${tr('Nächste Folge')}</span></button>
          <button class="btn btn-round sm glass glass--clear pl-knopf${p.spurenOffen === 'folgen' ? ' pl-knopf--an' : ''}" data-spuren="folgen"
            aria-label="${tr('Folgen')}" aria-expanded="${p.spurenOffen === 'folgen'}">${svg(I.folgen, 22, 2)}</button>` : ''}
        ${alleUntertitel(p).length || utStand.verfuegbar ? `<button class="btn btn-round sm glass glass--clear pl-knopf${p.spurenOffen === 'untertitel' ? ' pl-knopf--an' : ''}" data-spuren="untertitel"
          aria-label="${tr('Untertitel')}" aria-expanded="${p.spurenOffen === 'untertitel'}">${svg(I.untertitel, 22, 1.7)}</button>` : ''}
        ${tonspuren(p).length > 1 || (S.me?.account?.isAdmin && p.kind === 'episode') ? `<button class="btn btn-round sm glass glass--clear pl-knopf${p.spurenOffen === 'ton' ? ' pl-knopf--an' : ''}" data-spuren="ton"
          aria-label="${tr('Ton')}" aria-expanded="${p.spurenOffen === 'ton'}">${svg(I.ton, 22, 2)}</button>` : ''}
        ${p.kapitel?.length ? `<button class="btn btn-round sm glass glass--clear pl-knopf${p.spurenOffen === 'kapitel' ? ' pl-knopf--an' : ''}" data-spuren="kapitel"
          aria-label="${tr('Kapitel')}" aria-expanded="${p.spurenOffen === 'kapitel'}">${svg(I.kapitel, 21, 2)}</button>` : ''}
        <button class="btn btn-pill glass glass--clear pl-knopf pl-knopf--tempo${p.spurenOffen === 'tempo' ? ' pl-knopf--an' : ''}" data-spuren="tempo"
          aria-label="${esc(tr('Wiedergabetempo: {tempo}', { tempo: tempoText(p.tempo || 1) }))}" aria-expanded="${p.spurenOffen === 'tempo'}">${esc(tempoZahl(p.tempo || 1))}</button>
        ${bildImBildDa() ? `<button class="btn btn-round sm glass glass--clear pl-knopf" data-pip aria-label="${tr('Bild im Bild')}" title="${tr('Bild im Bild (P)')}">${svg(I.bildImBild, 20, 2)}</button>` : ''}
        <button class="btn btn-round sm glass glass--clear pl-knopf" data-vollbild
          aria-label="${imVollbild() ? tr('Vollbild beenden') : tr('Vollbild')}">${svg(imVollbild() ? I.vollbildAus : I.vollbild, 20, 2)}</button>
      </div>
      <div class="pl-bar" data-bar role="slider" tabindex="0" aria-label="${tr('Wiedergabeposition')}"
        aria-valuemin="0" aria-valuemax="${Math.round(total)}" aria-valuenow="${Math.round(pos)}"
        aria-valuetext="${esc(zeitleisteText(pos, total))}">${kapitelMarken(p, total)}<i style="width:${pct}%"></i><b style="left:${pct}%"></b>
        <span class="pl-vorschau" aria-hidden="true" hidden><span class="pl-vorschau-bild"></span><span class="pl-vorschau-zeit tnum"></span></span></div>
      <div style="display:flex;justify-content:space-between;margin-top:8px">
        <span class="t-sub tnum pl-zeit" aria-hidden="true">${clock(pos)}</span>
        <span class="t-sub tnum c-2 pl-rest" aria-hidden="true">${total ? '−' + clock(total - pos) : ''}</span>
      </div>
    </div>
  </div>`;
}

let idleTimer = null;

async function startPlayer(itemId, fromSec, fileId = null) {
  try {
    const d = await api.item(itemId);
    const it = d.item;
    let file = (fileId && (d.files || []).find(f => f.id === fileId && !f.missing)) ||
      (d.files || []).find(f => !f.missing);
    if (!file) { toast(tr('Zu diesem Titel gibt es keine Datei.')); return; }

    // Ein Film in Teilen („Apocalypse Now (1979) a.avi", „b.avi", „c.avi"):
    // Die Teile laufen nacheinander, als wäre es ein Film. Der
    // Stand zählt über alle Teile, damit „Weiter bei" in Teil 2 landet.
    const teile = [];
    for (let nr = 1; ; nr++) {
      const t = nr === 1 && (file.partNo || 1) === 1 ? file
        : (d.files || []).find(f => !f.missing && (f.partNo || 1) === nr);
      if (!t) break;
      teile.push(t);
    }
    let vorher = 0, teil = 0, gesamt = 0;
    let startGesamt = fromSec != null ? fromSec : (it.resumeMs ? it.resumeMs / 1000 : 0);
    if (teile.length > 1) {
      const dauer = t => (t.durationMs || 0) / 1000;
      gesamt = teile.reduce((a, t) => a + dauer(t), 0);
      // Fast am Ende gespeichert → von vorn. Aber nur beim Fortsetzen, nicht
      // beim Sprung in den nächsten Teil (sonst bei kurzen Teilen eine Schleife).
      if (fromSec == null && gesamt > 0 && startGesamt > gesamt - 5) startGesamt = 0;
      while (teil < teile.length - 1 && startGesamt >= vorher + dauer(teile[teil])) {
        vorher += dauer(teile[teil]);
        teil++;
      }
      file = teile[teil];
      fromSec = startGesamt - vorher;
    }

    // Ist die Bildschirmzeit um, sagt es schon der Plan — dann gar nicht
    // erst den Player öffnen.
    const plan = await api.plan(file.id).catch(e => { if (e?.code === 'bildschirmzeit') throw e; return null; });
    const dur = (file.durationMs || it.runtimeMs || 0) / 1000;

    let start = fromSec != null ? fromSec : (it.resumeMs ? it.resumeMs / 1000 : 0);
    // Ein Stand, der hinter dem Ende der Datei liegt, passt nicht mehr dazu —
    // etwa weil die Datei durch eine kürzere Fassung ersetzt wurde. Dann von
    // vorn, statt im Leeren hängenzubleiben.
    if (dur > 0 && start > dur - 5) start = 0;

    S.playing = {
      itemId: it.id, fileId: file.id,
      title: it.title,
      kind: it.kind, parentId: it.parentId || null,
      sub: S.warteschlange
        ? `${S.warteschlange.name} · ${tr('{nr} von {n}', { nr: S.warteschlange.i + 1, n: S.warteschlange.ids.length })}`
        : it.kind === 'episode' ? [it.seriesTitle, tr('Staffel {seasonNo}, Folge {episodeNo}', { seasonNo: it.seasonNo, episodeNo: it.episodeNo })].filter(Boolean).join(' · ')
        : teile.length > 1 ? tr('Teil {teil} von {n}', { teil: teil + 1, n: teile.length }) : '',
      teile: teile.length > 1 ? teile.map(t => t.id) : null, teil, vorher, gesamt,
      // Die gemessene Datei zählt, nicht die Laufzeit aus den Metadaten:
      // gespielt wird die Datei. Weichen beide ab — ein Schnitt, eine
      // andere Fassung —, stünde die Leiste sonst falsch.
      duration: dur,
      offset: 0, current: 0, paused: false, ui: true, loading: true,
      mounted: false,
      mode: plan?.plan?.direct ? 'direct' : 'transcode',
      // Ton und Untertitel: was die Datei hat, und was gewählt ist.
      tracks: file.tracks || [],
      // Kapitel der Datei (Blu-ray-Rips haben oft zwanzig und mehr). Bei
      // Filmen in Teilen gehören sie zu einem Teil — dann keine.
      kapitel: teile.length > 1 ? [] : kapitelListe(file),
      spur: -1, untertitel: null, spurenOffen: false,
      // Vorspann und Abspann, falls bekannt — für „Überspringen" und „Als Nächstes".
      // Je Fassung eigene Zeiten (jeDatei): Kapitel und Erkanntes gehören
      // zur Datei, die wirklich läuft.
      ...vorspannFuer(d, (teile[0] || file).id), seriesId: d.seriesId || null,
      vonHand: d.vonHand || null,
      seriesTitle: it.seriesTitle || '',
      introUebersprungen: false, naechsterAngeboten: false,
      // Tempo gilt für die Sitzung am Stück (die nächste Folge läuft gleich
      // schnell), Lautstärke für dieses Gerät.
      tempo: S.tempo || 1,
      gesichertUm: 0,
      // Zufallskennung dieses Players: Endet sein Strom zu früh, fragt er
      // unter /browser/grund nach, warum (stromAbgerissen).
      lauf: zufallsKennung(),
    };
    spurenVorbelegen(S.playing);
    // Die Vorschaubilder fürs Spulen gleich mitholen – beim ersten
    // Überfahren der Leiste sollen sie schon da sein.
    trickplayLaden(S.playing);
    // Ob es „Untertitel suchen" gibt — einmal gefragt, dann gemerkt.
    if (utStand.verfuegbar === null) utStandLaden().then(() => { if (S.playing?.mounted) { S.playing.spurenHtml = ''; patchChrome(); } });
    // Ein eigener Eintrag im Verlauf (ohne neue Adresse): „Zurück" im
    // Browser oder am Android-Handy schliesst den Film und führt auf die
    // Seite, von der aus er lief — vorher sprang es eine Seite zu weit.
    if (!history.state?.spieler) {
      try {
        verlaufTiefe++;
        history.pushState({ ...(history.state || {}), tiefe: verlaufTiefe, spieler: true }, '');
      } catch {}
    }
    // Wohin der Fokus nach dem Schliessen zurückkehrt: auf den Knopf, mit
    // dem der Film gestartet wurde.
    if (S.view !== 'player') S.fokusNachPlayer = fokusSchluessel();
    S.view = 'player';
    render();          // baut den Player auf, mountPlayer hängt sich ein
    // Kinderprofil: leise Bescheid geben, wenn die Zeit knapp wird.
    if (S.me?.account?.isKid) {
      const p = S.playing;
      bzLaden().then(() => bzBeimSchauen(() => S.playing === p && S.view === 'player',
        async meldung => { await stopPlayer(); bzSchluss(meldung); }));
    }
    await openStream(start);
  } catch (e) {
    S.playing = null;
    if (e?.code === 'bildschirmzeit') return bzSchluss(e.message);
    toast(e.message || tr('Wiedergabe nicht möglich.'));
  }
}

/* openStream lädt den Strom ab einer Stelle.
 *
 * Erst fragt die App, wo der Strom wirklich beginnen wird — beim blossen
 * Umpacken ist der Einstieg an Schlüsselbilder gebunden. Diese Auskunft
 * startet nichts; sie kostet den Server eine Handvoll Millisekunden.
 * (Früher holte die App dafür den Strom selbst und las die Kopfzeile —
 * das zählte als zweite Wiedergabe und löste die eigene Grenze aus.) */
async function openStream(atSec, { abspielen = false } = {}) {
  // Frischer Medienschlüssel vor jeder Wiedergabe: Ein Film dauert.
  try { await api.medienSchluessel(); } catch {}
  const p = S.playing;
  if (!p) return;
  p.loading = true;
  patchChrome();

  const v = $('#v');
  if (!v) return;
  // Wer pausiert und springt, will an der neuen Stelle stehen, nicht
  // losspielen. (Der Wechsel der Quelle meldet selbst eine Pause — darum
  // vorher merken.)
  const warPausiert = !abspielen && p.paused && p.mounted && !!v.src;

  p.offset = atSec || 0;
  // Die alte Stelle im alten Strom gilt nicht mehr — sonst sicherte die
  // Pause beim Umschalten „neuer Anfang + alte Spielzeit".
  p.current = 0;
  if (atSec > 0) {
    try {
      const sp = await api.seekPoint(p.fileId, atSec, einbrennenVon(p));
      p.offset = sp.start ?? atSec;
      p.mode = sp.mode || p.mode;
    } catch { /* dann eben die gewünschte Stelle */ }
  }

  // Eingebrannt wird immer neu gerechnet — auch ohne Sprung.
  if (einbrennenVon(p) >= 0) p.mode = 'transcode';
  v.autoplay = !warPausiert;
  p.strom = (p.strom || 0) + 1;
  v.src = api.streamURL(p.fileId, p.offset, p.spur, { untertitel: einbrennenVon(p), lauf: p.lauf });
  untertitelAnwenden();
  v.load();
  // load() setzt das Tempo zurück — nach jedem Sprung wieder anlegen.
  v.defaultPlaybackRate = v.playbackRate = p.tempo || 1;
  if (warPausiert) { p.paused = true; patchChrome(); return; }
  try { await v.play(); } catch {}
}

/* Wie weit vor dem Ende ein „ended" noch als Ende des Films gilt. Die
   gemessene Länge und das letzte Bild weichen um Sekunden ab; ein
   abgerissener Strom endet dagegen irgendwo mittendrin. */
const ENDE_TOLERANZ = 15;

function zufallsKennung() {
  // crypto.randomUUID gibt es nur über https — Lumen läuft im Heimnetz
  // oft über http. getRandomValues geht überall.
  const b = new Uint8Array(12);
  crypto.getRandomValues(b);
  return 'w' + [...b].map(x => x.toString(16).padStart(2, '0')).join('');
}

/* Der Strom ist zu früh zu Ende oder gescheitert. Das <video> sieht keinen
   Status, also fragt die App den Server, was war:
     - abgelaufen (lange Pause) oder unbekannt: still an der aktuellen
       Stelle neu ansetzen — höchstens einmal je Fehler, sonst Meldung;
     - beendet: sagen und den Player schliessen;
     - ersetzt (dieselbe Anmeldung spielt anderes): sagen und schliessen;
     - fehler: den Text des Servers zeigen (keine Umwandlung erlaubt,
       zu viele Geräte, Server ausgelastet …). */
async function stromAbgerissen(p, lief) {
  if (S.playing !== p || p.abriss) return;
  const v = $('#v');
  const strom = p.strom;
  const pos = p.offset + (v ? v.currentTime : p.current);
  p.abriss = true;
  p.loading = false;
  patchChrome();
  let g = {};
  try { g = await api.browserGrund(p.fileId, p.lauf) || {}; } catch {}
  p.abriss = false;
  // Inzwischen neu angesetzt (Sprung, andere Spur) oder geschlossen.
  if (S.playing !== p || p.strom !== strom) return;
  switch (g.code) {
    case 'wiedergabe_ersetzt':
      // Dieselbe Anmeldung (etwa ein zweiter Tab) spielt jetzt etwas
      // anderes – sagen, statt still mitten im Film zu schliessen.
      toast(tr('Auf diesem Gerät läuft jetzt etwas anderes.'));
      return stopPlayer();
    case 'wiedergabe_beendet':
      toast(tr('Die Wiedergabe wurde beendet.'));
      return stopPlayer();
    case 'bildschirmzeit':
      // Die Zeit ist um (Fenster zu oder Tagesgrenze erreicht).
      await stopPlayer();
      return bzSchluss(g.error);
    case 'fehler':
      p.paused = true; patchChrome(); showChrome(true);
      return toast(g.error ? g.error.charAt(0).toUpperCase() + g.error.slice(1) + '.' : tr('Wiedergabe nicht möglich.'));
  }
  // Abgelaufen, noch laufend (der Browser hat sich verschluckt) oder
  // unbekannt (Neustart des Servers): neu ansetzen. Aber nur einmal je
  // Fehler — reisst auch der neue Strom gleich wieder ab (innert 30 s),
  // hilft ein weiterer Versuch nicht.
  if (p.neuAngesetzt && Date.now() - p.neuAngesetzt < 30000) {
    p.paused = true; patchChrome(); showChrome(true);
    return toast(tr('Der Strom ist abgerissen.'));
  }
  p.neuAngesetzt = Date.now();
  await openStream(pos, { abspielen: lief });
}

function mountPlayer() {
  const v = $('#v');
  const p = S.playing;
  if (!v || !p || p.mounted) return;
  p.mounted = true;

  p.textspur = null; // gehört zum neuen <video>
  lautstaerkeAnwenden(v);
  v.defaultPlaybackRate = v.playbackRate = p.tempo || 1;
  v.onloadedmetadata = () => { p.loading = false; v.playbackRate = p.tempo || 1; patchChrome(); };
  v.onplaying = () => { p.loading = false; clearTimeout(p.wartenZeit); p.wartenZeit = null; p.paused = false; patchChrome(); showChrome(); };
  // Pause und Sprung sind gute Momente zum Sichern: Wer pausiert, steht
  // vielleicht gleich auf und schaut auf dem Handy weiter.
  v.onpause = () => { p.paused = true; showChrome(true); if (!v.ended) standSichern(); };
  v.onseeked = () => { standSichern(); };
  v.onvolumechange = () => { lautstaerkeMerken(v); patchLautstaerke(); };
  // Hängt der Strom länger, sagt Lumen es — vorher drehte sich der Kringel
  // nach einem Verbindungsabbruch wortlos weiter.
  const wartenAus = () => { clearTimeout(p.wartenZeit); p.wartenZeit = null; };
  v.onwaiting = () => {
    p.loading = true; patchChrome();
    if (p.wartenZeit) return;
    p.wartenZeit = setTimeout(() => {
      p.wartenZeit = null;
      if (S.playing === p && p.loading && !p.paused) toast(tr('Die Verbindung ist gerade langsam. Lumen lädt weiter …'));
    }, 10000);
  };
  v.oncanplay = () => { p.loading = false; wartenAus(); patchChrome(); };
  v.ontimeupdate = () => { p.current = v.currentTime; patchPlayer(); saveSoon(); };
  // Ein Strom ohne Länge endet auch, wenn der Server ihn beendet: nach
  // einer langen Pause (abgelaufen), auf Wunsch (Ich → Geräte) oder weil
  // dasselbe Gerät inzwischen anderes spielt. Für den Browser ist das ein
  // gewöhnliches Ende — weit vor dem Abspann ist es aber keins.
  v.onended = () => {
    const pos = p.offset + v.currentTime;
    if (p.duration && pos < p.duration - ENDE_TOLERANZ) return stromAbgerissen(p, true);
    amEnde();
  };
  v.onerror = () => {
    // Ein Fehler des alten Stroms nach einem Sprung zählt nicht.
    if (!v.getAttribute('src')) return;
    stromAbgerissen(p, !p.paused);
  };
  showChrome();
  // Der Fokus geht in den Player (auf Abspielen/Pause): Tastatur und
  // Bildschirmleser wissen so, dass er offen ist.
  app.querySelector('[data-toggle]')?.focus({ preventScroll: true });
}

/* patchChrome bessert die Steuerung nach: ein-/ausblenden, Pause-Symbol,
   Ladekringel. Alles ohne das Video anzufassen. */
function patchChrome() {
  const p = S.playing;
  if (!p || S.view !== 'player') return;

  for (const sel of ['.pl-scrim', '.pl-top', '.pl-mid', '.pl-bot']) {
    const el = app.querySelector(sel);
    if (!el) continue;
    el.classList.toggle('pl-hide', !p.ui);
    // inert: ausgeblendete Knöpfe sind weder per Tab noch für den
    // Screenreader erreichbar. Die Scrim hat keine Knöpfe.
    if (sel !== '.pl-scrim') el.inert = !p.ui;
  }
  app.querySelector('.player')?.classList.toggle('pl-nocursor', !p.ui);
  untertitelLage();

  const toggle = app.querySelector('[data-toggle]');
  if (toggle) {
    toggle.innerHTML = p.paused ? PLAY(28) : PAUSE(28);
    toggle.setAttribute('aria-label', p.paused ? tr('Abspielen') : tr('Pause'));
  }
  const panel = app.querySelector('.spuren');
  if (p.spurenOffen && p.ui) {
    const html = spurenPanel(p);
    // Nur neu zeichnen, wenn sich etwas geändert hat — sonst verschwindet
    // der Knopf unter dem Finger, während das Video seine Ereignisse feuert.
    if (html !== p.spurenHtml) {
      if (panel) panel.outerHTML = html;
      else app.querySelector('.player')?.insertAdjacentHTML('beforeend', html);
      p.spurenHtml = html;
    }
  } else if (panel) {
    panel.remove();
    p.spurenOffen = false;
    p.spurenHtml = '';
  }
  for (const b of app.querySelectorAll('[data-spuren]')) {
    const an = p.spurenOffen === b.dataset.spuren;
    b.classList.toggle('pl-knopf--an', an);
    b.setAttribute('aria-expanded', an ? 'true' : 'false');
  }

  let spin = app.querySelector('.pl-busy');
  if (p.loading && !spin) {
    spin = document.createElement('div');
    spin.className = 'pl-busy';
    spin.style.cssText = 'position:absolute;inset:0;display:flex;align-items:center;' +
      'justify-content:center;z-index:4;pointer-events:none';
    spin.innerHTML = '<span class="spin" style="width:34px;height:34px"></span>';
    app.querySelector('.player')?.appendChild(spin);
  } else if (!p.loading && spin) {
    spin.remove();
  }
  patchPlayer();
}

/* patchPlayer bewegt nur den Balken — ein vollständiges Neuzeichnen
   würde das Video jede Sekunde neu laden. */
function patchPlayer() {
  const p = S.playing;
  if (!p || S.view !== 'player') return;
  const pos = filmPos(p);
  const total = filmDauer(p);
  const pct = total ? Math.min(100, pos / total * 100) : 0;
  const bar = $('.pl-bar > i'), thumb = $('.pl-bar > b');
  // Während jemand zieht, zeigt die Leiste die Zielstelle, nicht den Film.
  if (!p.ziehen) {
    if (bar) bar.style.width = pct + '%';
    if (thumb) thumb.style.left = pct + '%';
  }
  const slider = $('.pl-bar');
  const sek = Math.round(pos);
  if (slider && slider.getAttribute('aria-valuenow') !== String(sek)) {
    slider.setAttribute('aria-valuenow', String(sek));
    slider.setAttribute('aria-valuemax', String(Math.round(total)));
    slider.setAttribute('aria-valuetext', zeitleisteText(pos, total));
  }
  const zeit = app.querySelector('.pl-zeit'), rest = app.querySelector('.pl-rest');
  if (zeit) zeit.textContent = clock(pos);
  if (rest) rest.textContent = total ? '−' + clock(total - pos) : '';
  // Vorspann und Abspann gehören zur Datei, die gerade läuft.
  vorspannPruefen(p, p.offset + p.current);
  abspannPruefen(p, p.offset + p.current, p.duration || 0);
}

/* „Vorspann überspringen": solange die Stelle im Vorspann liegt, steht
   der Knopf da; wer es in den Einstellungen so will, springt von selbst. */
function vorspannPruefen(p, pos) {
  const i = p.intro;
  // Wer übersprungen hat und dann in den Vorspann zurückspult, bekommt den
  // Knopf wieder — nur das automatische Überspringen bleibt aus. (Kurz
  // nach dem Sprung steht die alte Stelle noch da; die zählt nicht.)
  const imVorspann = i && pos >= i.vonMs / 1000 + 0.5 && pos < i.bisMs / 1000 - 1;
  const drin = imVorspann && (!p.introUebersprungen
    || (pos < i.bisMs / 1000 - 3 && Date.now() - (p.introSprungUm || 0) > 3000));
  const pill = document.getElementById('vorspann');
  if (drin && !pill) {
    const b = document.createElement('button');
    b.id = 'vorspann'; b.className = 'nextpill glass glass--regular btn';
    b.dataset.skipintro = '1';
    b.innerHTML = `${svg(I.fwd10, 18, 2)}<span class="t-body">${tr('Vorspann überspringen')}</span>`;
    document.querySelector('.player')?.appendChild(b);
    if ((S.me?.prefs || {}).skipIntro === '1' && !p.introUebersprungen) vorspannUeberspringen();
  } else if (!drin && pill) {
    pill.remove();
  }
}

function vorspannUeberspringen() {
  const p = S.playing;
  if (!p?.intro) return;
  p.introUebersprungen = true;
  p.introSprungUm = Date.now();
  document.getElementById('vorspann')?.remove();
  seekTo(p.intro.bisMs / 1000);
}

/* Beginnt der Abspann — laut Kapitel, oder in den letzten 40 Sekunden —,
   steht „Als Nächstes" schon da, mit dem, was kommt, und zählt bis zum
   Ende herunter. Nur bei Folgen und Listen; ein einzelner Film endet. */
function abspannPruefen(p, pos, total) {
  if (!total) return;
  const ab = p.creditsMs ? p.creditsMs / 1000 : total - 40;
  // Aus dem Abspann zurückgespult: Die Karte geht weg und kommt beim
  // nächsten Abspann wieder (vorher blieb sie über der Handlung stehen).
  if (p.naechsterAngeboten && !p.abgelehnt && pos < ab - 1 && document.getElementById('naechster')) {
    naechsterWeg();
    p.naechsterAngeboten = false;
    return;
  }
  if (p.naechsterAngeboten) return;
  if (pos < ab || pos > total - 2) return;
  if (p.kind !== 'episode' && !S.warteschlange) return;
  p.naechsterAngeboten = true;
  naechstesAnbieten(p, Math.max(10, Math.round(total - pos)));
}

function showChrome(keepOn) {
  const p = S.playing;
  if (!p) return;
  p.ui = true;
  patchChrome();
  clearTimeout(idleTimer);
  if (!keepOn && !p.paused) {
    idleTimer = setTimeout(function ausblenden() {
      const q = S.playing;
      if (!q || q.paused || q.spurenOffen || q.ziehen) return;
      // Wer mit der Tastatur in der Leiste steht, verliert sie nicht unter
      // den Händen. (Ein Mausklick setzt kein :focus-visible.)
      if (app.querySelector('.player :focus-visible')) { idleTimer = setTimeout(ausblenden, 2800); return; }
      q.ui = false;
      // Der Fokus darf nicht in einer unsichtbaren Leiste zurückbleiben.
      if (app.querySelector('.player')?.contains(document.activeElement)) document.activeElement.blur();
      patchChrome();
    }, 2800);
  }
}

/* ---------------------------------------------------------------- Stand sichern
 *
 * Früher startete jedes timeupdate (vier pro Sekunde) einen 5-s-Zeitgeber
 * neu — er lief also nie ab, und gesichert wurde nur beim Schliessen.
 * Jetzt: alle zehn Sekunden Wiedergabe, dazu bei Pause, Sprung, Schliessen
 * und wenn die Seite in den Hintergrund geht. */
const SICHERN_ALLE_MS = 10000;
let saveTimer = null; // bleibt, damit ältere Aufrufe (clearTimeout) nicht ins Leere greifen

function standVon(p) {
  return {
    positionMs: Math.round(((p.vorher || 0) + p.offset + p.current) * 1000),
    durationMs: Math.round((p.gesamt || p.duration || 0) * 1000),
  };
}

/* Ab dem Abspann (laut Kapitel) oder ab 90 % gilt ein Titel als gesehen —
   auch wenn jemand vorher „Jetzt" drückt oder schliesst. */
function istGesehen(p) {
  if (p.teile && p.teil < p.teile.length - 1) return false;
  const pos = p.offset + p.current, total = p.duration || 0;
  if (!total) return false;
  if (p.creditsMs && pos >= p.creditsMs / 1000) return true;
  return pos / total >= 0.9;
}

function saveSoon() {
  const p = S.playing;
  if (!p || p.loading) return;
  const jetzt = Date.now();
  if (!p.gesichertUm) { p.gesichertUm = jetzt; return; } // nicht gleich beim Start
  if (jetzt - p.gesichertUm < SICHERN_ALLE_MS) return;
  standSichern();
}

function standSichern() {
  const p = S.playing;
  if (!p || !p.duration) return Promise.resolve();
  p.gesichertUm = Date.now();
  const st = standVon(p);
  if (st.positionMs <= 0) return Promise.resolve();
  // Geht es nicht durch (Verbindung weg), bleibt der Stand im Browser
  // liegen und wird nachgereicht (api.standMelden).
  return api.standMelden(p.itemId, st.positionMs, st.durationMs).catch(() => {});
}

/* Zum Schluss: gesehen melden oder die Stelle sichern. Jede Art, den
   Player zu verlassen, geht hier durch — Schliessen, „Jetzt", „Nächste
   Folge", eine andere Folge aus der Liste. */
async function standAbschliessen(p) {
  if (!p) return;
  if (istGesehen(p)) {
    if (!p.fertigGemeldet) { p.fertigGemeldet = true; await api.standMelden(p.itemId, 0, 0, true).catch(() => {}); }
    return;
  }
  const st = standVon(p);
  if (st.positionMs > 0) await api.standMelden(p.itemId, st.positionMs, st.durationMs).catch(() => {});
}

/* Beim Weggehen (Tab zu, App in den Hintergrund): fetch mit keepalive statt
   sendBeacon — nur so geht das Token im Kopf mit statt in der Adresse. Der
   Stand liegt vorher schon im Browser: Kommt er nicht an, reicht ihn der
   nächste Start nach. */
function standImHintergrund() {
  const p = S.playing;
  if (!p || !p.duration) return;
  const st = standVon(p);
  if (st.positionMs <= 0) return;
  const fertig = istGesehen(p);
  try {
    api.standVormerken(p.itemId, st.positionMs, st.durationMs, fertig);
    api.standNachreichen({ keepalive: true });
    p.gesichertUm = Date.now();
  } catch {}
}

/* ---------------------------------------------------------------- Lautstärke, Tempo, Vollbild */

function lautstaerkeAnwenden(v) {
  try {
    const l = Number(localStorage.getItem('lumen.lautstaerke'));
    if (localStorage.getItem('lumen.lautstaerke') !== null && l >= 0 && l <= 1) v.volume = l;
    v.muted = localStorage.getItem('lumen.stumm') === '1';
  } catch {}
}
function lautstaerkeMerken(v) {
  try {
    localStorage.setItem('lumen.lautstaerke', String(v.volume));
    localStorage.setItem('lumen.stumm', v.muted ? '1' : '0');
  } catch {}
}
function patchLautstaerke() {
  const v = $('#v');
  if (!v) return;
  const aus = v.muted || v.volume === 0;
  const knopf = app.querySelector('[data-stumm]');
  if (knopf) {
    knopf.setAttribute('aria-pressed', String(v.muted));
    knopf.setAttribute('aria-label', v.muted ? tr('Ton an') : tr('Stumm'));
    knopf.innerHTML = svg(aus ? I.stumm : I.lautsprecher, 20, 2);
  }
  const regler = app.querySelector('[data-laut]');
  if (regler && document.activeElement !== regler) regler.value = String(v.muted ? 0 : v.volume);
  if (regler) regler.setAttribute('aria-valuetext', `${Math.round((v.muted ? 0 : v.volume) * 100)} %`);
}
function lautstaerkeAendern(delta) {
  const v = $('#v');
  if (!v) return;
  v.muted = false;
  v.volume = Math.max(0, Math.min(1, Math.round((v.volume + delta) * 20) / 20));
  toast(`${tr('Lautstärke')} ${Math.round(v.volume * 100)} %`);
}
function stummUmschalten() {
  const v = $('#v');
  if (!v) return;
  v.muted = !v.muted;
  if (!v.muted && v.volume === 0) v.volume = 0.5;
}

function tempoSetzen(t) {
  const p = S.playing, v = $('#v');
  if (!p) return;
  p.tempo = t;
  S.tempo = t;
  if (v) v.defaultPlaybackRate = v.playbackRate = t;
  const k = app.querySelector('[data-spuren="tempo"]');
  if (k) { k.textContent = tempoZahl(t); k.setAttribute('aria-label', tr('Wiedergabetempo: {tempo}', { tempo: tempoText(t) })); }
  // Eine Wahl, fertig: das Menü geht zu, der Fokus zurück auf den Knopf.
  p.spurenOffen = false;
  p.spurenHtml = '';
  patchChrome();
  k?.focus({ preventScroll: true });
}

async function vollbildUmschalten() {
  const el = app.querySelector('.player'), v = $('#v');
  if (!el) return;
  try {
    if (imVollbild()) {
      await (document.exitFullscreen ? document.exitFullscreen() : document.webkitExitFullscreen?.());
    } else if (vollbildDa()) {
      // Das ganze Player-Feld, nicht nur das Video: sonst fehlten im
      // Vollbild Leiste, Untertitel und „Als Nächstes".
      await (el.requestFullscreen ? el.requestFullscreen({ navigationUI: 'hide' }) : el.webkitRequestFullscreen?.());
    } else if (v?.webkitEnterFullscreen) {
      // iPhone: Vollbild gibt es nur für das Video selbst, mit der
      // Steuerung von iOS.
      v.webkitEnterFullscreen();
    }
  } catch { toast(tr('Vollbild ist hier nicht möglich.')); }
}
function vollbildVerlassen() {
  if (imVollbild()) (document.exitFullscreen ? document.exitFullscreen() : document.webkitExitFullscreen?.())?.catch?.(() => {});
}
for (const ev of ['fullscreenchange', 'webkitfullscreenchange']) {
  document.addEventListener(ev, () => {
    const k = app.querySelector('[data-vollbild]');
    if (!k) return;
    k.setAttribute('aria-label', imVollbild() ? tr('Vollbild beenden') : tr('Vollbild'));
    k.innerHTML = svg(imVollbild() ? I.vollbildAus : I.vollbild, 20, 2);
  });
}

/* Untertitel an/aus mit „C": zurück zur zuletzt gewählten Spur, sonst
   zur ersten in der gewohnten Sprache, sonst zur ersten überhaupt. */
function untertitelUmschalten() {
  const p = S.playing;
  if (!p) return;
  const text = alleUntertitel(p);
  if (!text.length) return toast(tr('Keine Untertitel in dieser Datei'));
  if (p.untertitel != null) { untertitelWaehlen('aus'); return toast(tr('Untertitel aus')); }
  const sprache = serienSpuren(p.seriesId)?.ut || S.me?.prefs?.untertitel;
  const t = text.find(x => x.index === p.letzteUntertitel)
    || text.find(x => x.language === sprache && !x.forced) || text.find(x => !x.forced) || text[0];
  untertitelWaehlen(String(t.index));
  toast(tr('Untertitel: {name}', { name: spurName(t) }));
}

/* ---------------------------------------------------------------- Zeitleiste
 *
 * Ziehen mit Maus, Finger oder Stift; beim Überfahren und Ziehen ein
 * Vorschaubild aus dem Trickplay-Blatt (falls der Server eins hat) und
 * die Zeit. Gesprungen wird erst beim Loslassen — sonst setzte jeder
 * Pixel einen neuen Strom an. */
function barSekunde(bar, clientX) {
  const p = S.playing;
  const r = bar.getBoundingClientRect();
  const anteil = Math.max(0, Math.min(1, (clientX - r.left) / (r.width || 1)));
  return anteil * (p ? filmDauer(p) : 0);
}

function vorschauZeigen(bar, sek) {
  const p = S.playing;
  const box = bar.querySelector('.pl-vorschau');
  if (!p || !box || !filmDauer(p)) return;
  const anteil = sek / filmDauer(p);
  p.vorschauBei = sek;
  box.hidden = false;
  box.querySelector('.pl-vorschau-zeit').textContent = clock(sek);
  const bild = box.querySelector('.pl-vorschau-bild');
  // Vorschaubilder gibt es je Datei — bei Filmen in Teilen nur für den laufenden.
  const imTeil = sek - filmVor(p);
  const t = imTeil >= 0 && imTeil < (p.duration || 0) ? p.trick : null;
  sek = imTeil;
  if (t && t.blaetter?.length && t.intervall > 0) {
    const n = Math.min(Math.floor(sek / t.intervall), (t.bilder || 1) - 1);
    const jeBlatt = (t.spalten || 1) * (t.zeilen || 1);
    const blatt = t.blaetter[Math.floor(n / jeBlatt)];
    const k = n % jeBlatt;
    const breite = 160, hoehe = Math.round(160 * (t.hoehe || 90) / (t.breite || 160));
    const skala = breite / (t.breite || 160);
    if (blatt) {
      bild.hidden = false;
      bild.style.width = breite + 'px';
      bild.style.height = hoehe + 'px';
      bild.style.backgroundImage = `url("${blatt}${tokenSuffix(blatt)}")`;
      bild.style.backgroundSize = `${(t.spalten || 1) * breite}px ${(t.zeilen || 1) * hoehe}px`;
      bild.style.backgroundPosition = `-${(k % (t.spalten || 1)) * (t.breite || 160) * skala}px -${Math.floor(k / (t.spalten || 1)) * (t.hoehe || 90) * skala}px`;
    } else bild.hidden = true;
  } else {
    bild.hidden = true;
  }
  // Am Rand nicht aus dem Bild laufen.
  const r = bar.getBoundingClientRect();
  const halb = (box.offsetWidth || 60) / 2;
  const x = Math.max(halb, Math.min(r.width - halb, anteil * r.width));
  box.style.left = x + 'px';
}
function vorschauWeg(bar) {
  const box = bar?.querySelector('.pl-vorschau');
  if (box) box.hidden = true;
}

/* Trickplay holen, einmal je Datei. 404 (gibt es nicht) und 202 (wird
   gerade erstellt) zeigen still nichts; 202 fragt später noch einmal. */
async function trickplayLaden(p) {
  if (!p || p.trickLaedt || p.trick || (p.trickNichtVor && Date.now() < p.trickNichtVor)) return;
  p.trickLaedt = true;
  try {
    const t = await api.trickplay(p.fileId);
    if (t && Array.isArray(t.blaetter) && t.blaetter.length && t.intervall > 0) {
      p.trick = t;
      // Die Vorschau stand schon da, bevor die Bilder kamen (erstes
      // Überfahren): jetzt mit Bild nachzeichnen, statt bis zur nächsten
      // Mausbewegung nur die Zeit zu zeigen.
      const bar = app.querySelector('[data-bar]');
      const box = bar?.querySelector('.pl-vorschau');
      if (S.playing === p && box && !box.hidden && p.vorschauBei != null) vorschauZeigen(bar, p.vorschauBei);
    } else p.trickNichtVor = Date.now() + 60000;
  } catch {
    p.trickNichtVor = Date.now() + 10 * 60000;
  } finally { p.trickLaedt = false; }
}

app.addEventListener('pointerdown', e => {
  const bar = e.target instanceof Element && e.target.closest('[data-bar]');
  const p = S.playing;
  if (!bar || !p || !p.duration || e.button > 0) return;
  e.preventDefault();
  p.ziehen = true;
  try { bar.setPointerCapture(e.pointerId); } catch {}
  trickplayLaden(p);
  showChrome(true);
  const zeigen = x => {
    const sek = barSekunde(bar, x);
    p.ziel = sek;
    const pct = sek / filmDauer(p) * 100;
    const i = bar.querySelector('i'), b = bar.querySelector('b');
    if (i) i.style.width = pct + '%';
    if (b) b.style.left = pct + '%';
    vorschauZeigen(bar, sek);
  };
  zeigen(e.clientX);
  const weiter = ev => zeigen(ev.clientX);
  const ende = ev => {
    bar.removeEventListener('pointermove', weiter);
    bar.removeEventListener('pointerup', ende);
    bar.removeEventListener('pointercancel', ende);
    p.ziehen = false;
    vorschauWeg(bar);
    if (ev.type === 'pointerup' && p.ziel != null) filmSprung(p.ziel);
    p.ziel = null;
    showChrome();
  };
  bar.addEventListener('pointermove', weiter);
  bar.addEventListener('pointerup', ende);
  bar.addEventListener('pointercancel', ende);
});
// Mit der Maus: schon beim Überfahren die Vorschau.
app.addEventListener('pointermove', e => {
  if (e.pointerType !== 'mouse' || S.view !== 'player') return;
  const p = S.playing;
  const bar = e.target instanceof Element && e.target.closest('[data-bar]');
  if (!p || p.ziehen) return;
  if (bar) { trickplayLaden(p); vorschauZeigen(bar, barSekunde(bar, e.clientX)); }
  else vorschauWeg(app.querySelector('[data-bar]'));
});

/* Tasten auf der fokussierten Zeitleiste: Pfeile springen zehn Sekunden
   (wie überall im Player), Bild auf/ab eine Minute, Pos1/Ende an Anfang
   und Ende. */
function zeitleisteTaste(e) {
  const p = S.playing, v = $('#v');
  if (!p || !v) return false;
  const jetzt = filmVor(p) + p.offset + v.currentTime;
  const ziel = { ArrowLeft: jetzt - 10, ArrowDown: jetzt - 10, ArrowRight: jetzt + 10, ArrowUp: jetzt + 10,
    PageDown: jetzt - 60, PageUp: jetzt + 60, Home: 0, End: filmDauer(p) - 2 }[e.key];
  if (ziel == null) return false;
  e.preventDefault();
  filmSprung(Math.max(0, ziel));
  return true;
}

/* ---------------------------------------------------------------- „Schaust du noch?"
 *
 * Nach drei Folgen am Stück ohne jede Eingabe fragt Lumen nach, statt die
 * Nacht durchzuspielen. Abschaltbar im Profil. Gezählt wird nur hier im
 * Browser, nirgends gespeichert. */
const NOCH_DA_NACH = 3;
let folgenOhneEingabe = 0;
const nochDaFragen = () => (S.me?.prefs?.nochDaFrage ?? '1') !== '0';
function eingabeGesehen() { folgenOhneEingabe = 0; }

function nochDaZeigen(id) {
  const v = $('#v');
  if (v) v.pause();
  naechsterWeg();
  document.getElementById('nochda')?.remove();
  const box = document.createElement('div');
  box.id = 'nochda';
  box.className = 'nochda';
  box.setAttribute('role', 'alertdialog');
  box.setAttribute('aria-modal', 'true');
  box.setAttribute('aria-labelledby', 'nochda-titel');
  box.innerHTML = `<div class="nochda-karte glass glass--regular">
      <h2 id="nochda-titel" class="t-title3">${tr('Schaust du noch?')}</h2>
      <p class="t-sub c-2" style="margin-top:6px">${tr('Drei Folgen liefen am Stück ohne Eingabe.')}</p>
      <div style="display:flex;gap:10px;margin-top:18px;flex-wrap:wrap">
        <button class="btn btn-primary" data-nochda-weiter="${esc(id)}">${tr('Weiterschauen')}</button>
        <button class="btn btn-pill btn-pill--fill" data-nochda-aus>${tr('Beenden')}</button>
      </div></div>`;
  document.querySelector('.player')?.appendChild(box);
  showChrome(true);
  box.querySelector('[data-nochda-weiter]')?.focus();
}



/* Wohin es nach dem Player geht: bei einer Folge zur Serie, mit der
   Staffel dieser Folge offen; sonst zur Titelseite des Films. */
function zielNachPlayer(p) {
  if (p.kind === 'episode' && p.seriesId) {
    S.staffelWahl = { serie: p.seriesId, staffel: p.parentId };
    return p.seriesId;
  }
  return p.itemId;
}

async function markDoneAndLeave() {
  const p = S.playing;
  if (!p) return;
  if (!p.fertigGemeldet) { p.fertigGemeldet = true; await api.standMelden(p.itemId, 0, 0, true).catch(() => {}); }
  const liste = S.warteschlange?.zurueck;
  const ziel = zielNachPlayer(p);
  vollbildVerlassen();
  S.playing = null;
  S.warteschlange = null;
  // Eine Liste am Stück: danach zurück zur Liste, nicht zum letzten Film.
  if (liste) return go('playlist', { id: liste, ersetzen: true });
  go('detail', { id: ziel, ersetzen: true });
}

/* ---------------------------------------------------------------- Am Stück
 *
 * Was nach dem Ende kommt: in einer Liste der nächste Titel, bei einer
 * Folge die nächste Folge — sofern „Folgen automatisch weiter" an ist.
 * Vorher endete jeder Film auf seiner Titelseite, und der Schalter in den
 * Einstellungen tat im Browser nichts.
 */

// Staffeln und Folgen kommen vom Server schon in der einen Reihenfolge,
// die alle Geräte zeigen: Staffel (Specials zuletzt), Folgennummer (ohne
// Nummer zuletzt). Weiter geht es über Staffelgrenzen nur in den
// regulären Staffeln — ein Making-of soll nicht von selbst starten.
const regulaer = st => (st.seasonNo || 0) > 0;

// Die Folge, mit der es in einer Serie weitergeht: die nach der zuletzt
// geschauten (oder die angefangene) — das weiss der Server, und er sagt
// allen Geräten dasselbe. Nur wenn er nicht antwortet, sucht der Browser
// selbst die erste ungesehene.
async function ersteOffeneFolge(serieId) {
  try {
    const w = await api.weiterInSerie(serieId);
    if (w?.item?.id) return w.item;
  } catch {}
  const d = await api.item(serieId);
  const staffeln = (d.seasons || []).filter(regulaer);
  let erste = null;
  for (const st of staffeln) {
    const folgen = await api.children(st.id).catch(() => []);
    if (!erste && folgen.length) erste = folgen[0];
    const offen = folgen.find(f => !f.finished);
    if (offen) return offen;
  }
  return erste;
}

// Die Folge nach dieser — auch über das Ende einer Staffel hinweg.
async function folgeDanach(folgeId, staffelId) {
  if (!staffelId) return null;
  const folgen = await api.children(staffelId).catch(() => []);
  const i = folgen.findIndex(f => f.id === folgeId);
  if (i >= 0 && i + 1 < folgen.length) return folgen[i + 1];
  const staffel = (await api.item(staffelId).catch(() => null))?.item;
  if (!staffel?.parentId) return null;
  const serie = await api.item(staffel.parentId).catch(() => null);
  const staffeln = serie?.seasons || [];
  const j = staffeln.findIndex(x => x.id === staffelId);
  if (j < 0 || !regulaer(staffeln[j])) return null;
  for (const st of staffeln.slice(j + 1).filter(regulaer)) {
    const f = await api.children(st.id).catch(() => []);
    if (f.length) return f[0];
  }
  return null;
}

// Aus einem Listeneintrag das, was sich abspielen lässt. Ein Film ist
// sich selbst genug; bei einer Serie die erste offene Folge.
async function abspielbar(x) {
  if (x.kind === 'series') {
    const f = await ersteOffeneFolge(x.id).catch(() => null);
    return f ? { id: f.id, title: `${x.title} · ${sf(f.seasonNo, f.episodeNo)}` } : null;
  }
  return { id: x.id, title: x.title };
}

async function naechsterTitel(p) {
  const w = S.warteschlange;
  if (w) {
    while (w.i + 1 < w.ids.length) {
      w.i++;
      const x = w.items[w.i];
      const a = x ? await abspielbar(x) : null;
      if (a) return a;
    }
    return null;
  }
  if (p.kind === 'episode' && (S.me?.prefs?.autoplay ?? '1') !== '0') {
    const f = await folgeDanach(p.itemId, p.parentId).catch(() => null);
    const serie = f?.seriesTitle || p.seriesTitle;
    if (f) return { id: f.id, title: serie ? `${serie} · ${folgenName(f)}` : folgenName(f) };
  }
  return null;
}

let naechsterTimer = null;
function naechsterWeg() {
  clearInterval(naechsterTimer);
  naechsterTimer = null;
  document.getElementById('naechster')?.remove();
}

async function amEnde() {
  const p = S.playing;
  if (!p) return;
  // Noch ein Teil übrig: gleich weiter, ohne Countdown — es ist derselbe Film.
  if (p.teile && p.teil < p.teile.length - 1) {
    const weiter = (p.vorher || 0) + (p.duration || 0) + 0.5;
    S.playing = null;
    return startPlayer(p.itemId, weiter);
  }
  if (!p.fertigGemeldet) {
    p.fertigGemeldet = true; // markDoneAndLeave soll es nicht zweimal senden
    await api.standMelden(p.itemId, 0, 0, true).catch(() => {});
  }
  // Steht „Als Nächstes" schon da (seit dem Abspann), geht es jetzt los.
  const box = document.getElementById('naechster');
  if (box?.dataset.id) return naechsterStarten(box.dataset.id, true);
  if (box) return; // noch am Laden
  if (p.abgelehnt) return markDoneAndLeave();
  naechstesAnbieten(p, 5);
}

/* naechstesAnbieten zeigt, was kommt, mit Countdown — am Ende, oder schon
   im Abspann. Wer nicht weiterschauen will, bricht ab, bevor es anfängt. */
async function naechstesAnbieten(p, sekunden) {
  const n = await naechsterTitel(p).catch(() => null);
  if (S.playing !== p) return;
  if (!n) {
    const v = $('#v');
    if (v && v.ended) markDoneAndLeave();
    return;
  }
  naechsterWeg();
  // Ein offenes Seitenfeld (Folgen, Spuren) lag über der Karte, und ihre
  // Knöpfe waren auf dem Handy nicht zu erreichen — es macht Platz.
  if (p.spurenOffen) { p.spurenOffen = false; p.spurenHtml = ''; patchChrome(); }
  let rest = sekunden;
  const box = document.createElement('div');
  box.id = 'naechster';
  box.className = 'naechster glass glass--regular';
  box.dataset.id = n.id;
  box.innerHTML = `${n.id ? `<img class="naechster-bild" src="${api.artURL(n.id, 'frame')}" alt="" data-weg>` : ''}
    <div style="min-width:0;flex:1">
      <div class="t-caption c-2">${tr('Als Nächstes · in')} <span data-rest>${rest}</span> s</div>
      <div class="t-body clamp-1">${esc(n.title)}</div></div>
    <button class="btn btn-pill btn-pill--fill" style="height:34px" data-naechster-jetzt>${tr('Jetzt')}</button>
    <button class="btn btn-pill" style="height:34px" data-naechster-nein>${tr('Abbrechen')}</button>`;
  document.querySelector('.player')?.appendChild(box);
  showChrome(true);
  naechsterTimer = setInterval(() => {
    const v = $('#v');
    // Im Abspann zählt die Zeit des Films, nicht die Uhr: wer pausiert,
    // bekommt mehr Zeit; wer springt, weniger.
    const p2 = S.playing;
    if (p2 && p2.duration && v && !v.ended) {
      rest = Math.max(0, Math.round(p2.duration - (p2.offset + v.currentTime)));
    } else {
      rest--;
    }
    const z = box.querySelector('[data-rest]');
    if (z) z.textContent = String(Math.max(rest, 0));
    if (rest <= 0) { naechsterWeg(); naechsterStarten(n.id, true); }
  }, 1000);
}

/* Die nächste Folge (oder der nächste Titel einer Liste) startet. Vorher
   wird die laufende abgeschlossen: ab Abspann bzw. 90 % als gesehen,
   sonst mit ihrer Stelle — „Jetzt" im Abspann liess sie früher halb
   geschaut zurück. automatisch: niemand hat gedrückt, der Countdown lief
   ab — das zählt für „Schaust du noch?". */
async function naechsterStarten(id, automatisch = false) {
  naechsterWeg();
  const p = S.playing;
  if (automatisch && nochDaFragen()) {
    folgenOhneEingabe++;
    if (folgenOhneEingabe >= NOCH_DA_NACH) {
      if (p) await standAbschliessen(p);
      return nochDaZeigen(id);
    }
  } else if (!automatisch) {
    eingabeGesehen();
  }
  const v = $('#v');
  if (v) { v.pause(); v.removeAttribute('src'); v.load(); }
  S.playing = null;
  await standAbschliessen(p);
  await startPlayer(id, null);
}

async function spielerAnhalten() {
  const p = S.playing;
  if (!p) return null;
  naechsterWeg();
  bildImBildVerlassen();
  clearTimeout(p.wartenZeit);
  document.getElementById('nochda')?.remove();
  vollbildVerlassen();
  S.warteschlange = null;
  S.playing = null;
  clearTimeout(saveTimer);
  const v = $('#v');
  if (v) { v.pause(); v.removeAttribute('src'); v.load(); }
  await standAbschliessen(p);
  return p;
}

async function stopPlayer() {
  const p = await spielerAnhalten();
  if (!p) return;
  // Lief der Film von genau der Seite aus, auf die es zurückgeht: den
  // eigenen Verlaufseintrag des Players zurücknehmen — sonst stünde die
  // Seite danach zweimal im Verlauf.
  const st = history.state;
  if (st?.spieler && st.view === 'detail' && st.id === zielNachPlayer(p)) { history.back(); return; }
  // Der Player hatte keinen eigenen Eintrag im Verlauf; zurück geht es
  // auf die Seite, von der aus er gestartet wurde — bei einer Folge die
  // Serie mit ihrer Staffel, nicht die nackte Folge.
  go('detail', { id: zielNachPlayer(p), ersetzen: true });
}

/* ---------------------------------------------------------------- Filmreihen */

/* Eine Reihe als Stapel ihrer Poster — der erste Film vorn. */
function reiheKarte(r) {
  // Hat die Reihe ein eigenes Poster (von TMDb), steht es vorn; die Filme
  // dahinter bleiben als Stapel sichtbar.
  const blaetter = (r.items || []).slice(0, r.poster ? 2 : 3);
  const vorn = r.poster ? `<div class="reihe-blatt reihe-blatt--0">${artOf({ poster: r.poster, title: r.name })}</div>` : '';
  const hinten = blaetter.map((it, i) =>
    `<div class="reihe-blatt reihe-blatt--${i + (r.poster ? 1 : 0)}">${artOf(it)}</div>`).reverse().join('');
  return `<button class="card reihe-karte" data-go="collection" data-id="${esc(r.id)}"
      aria-label="${tr('{name}, {n} Filme', { name: esc(r.name), n: r.anzahl })}">
    <div class="reihe-stapel" aria-hidden="true">${hinten}${vorn}</div>
    <div class="t-body clamp-1" style="margin-top:10px">${esc(r.name)}</div>
    <div class="t-sub c-2">${tr('{n} Filme', { n: r.anzahl })}</div>
  </button>`;
}

const kopf = (titel, unter, aktionen = '') => `
  <header>
    <button class="btn btn-pill btn-pill--fill" style="height:32px;font-size:.8125rem;gap:6px" data-back>
      ${svg(I.back, 16, 2)}${tr('Zurück')}</button>
    <h1 class="t-large" style="margin:14px 0 0">${titel}</h1>
    <div class="t-sub c-2" style="margin-top:4px">${unter}</div>
    ${aktionen ? `<div style="display:flex;gap:10px;flex-wrap:wrap;margin-top:18px">${aktionen}</div>` : ''}
  </header>`;

function viewCollections() {
  const rs = S.data?.collections || [];
  return shell(`<div class="wrap wrap--flat">
    <h1 class="t-large">${tr('Filmreihen')}</h1>
    ${reihenReiter('collections')}
    <div class="t-sub c-2" style="margin:4px 0 26px">
      ${rs.length ? tr('{n} Reihen · Lumen stellt sie selbst zusammen', { n: rs.length }) : ''}</div>
    ${rs.length ? `<div class="reihen-grid">${rs.map(reiheKarte).join('')}</div>`
      : `<div class="group group--inset" style="max-width:620px"><p class="hint" style="padding:18px 16px;margin:0">
          ${tr('Noch keine Reihen. Lumen erkennt sie von selbst, sobald zwei Filme einer Reihe da sind — am Titel („Teil 2“, „II“, „Der Herr der Ringe: …“) oder, mit TMDb-Schlüssel, sicher über TMDb.')}</p></div>`}
    <div style="height:40px"></div></div>`);
}

function viewCollection() {
  const r = S.data;
  if (!r) return shell(`<div class="wrap">${loading()}</div>`);
  const besitzer = S.me.account.isAdmin;
  const naechster = r.items.find(x => !x.finished) || r.items[0];
  const gesehen = r.items.filter(x => x.finished).length;
  const unter = [tr('{n} Filme', { n: r.anzahl }),
    gesehen ? tr('{n} gesehen', { n: gesehen }) : '',
    r.source === 'tmdb' ? tr('zusammengestellt nach TMDb') : r.source === 'nfo' ? tr('aus den NFO-Dateien (Sammlung aus Emby, Jellyfin oder Kodi)') : tr('von Lumen am Titel erkannt'),
    r.hidden ? tr('aufgelöst') : ''].filter(Boolean).join(' · ');

  const titel = S.bearbeiten === 'reihe' && besitzer
    ? `<form id="reiheform" style="display:flex;gap:10px;align-items:center;flex-wrap:wrap">
         <input name="name" class="titel-feld" value="${esc(r.name)}" required maxlength="80" autofocus
           aria-label="${tr('Name der Reihe')}">
         <button class="btn btn-pill btn-pill--fill">${tr('Sichern')}</button>
         <button type="button" class="btn btn-pill" data-bearbeiten-aus>${tr('Abbrechen')}</button></form>`
    : esc(r.name);

  const aktionen = [
    naechster ? `<button class="btn btn-primary" data-play="${esc(naechster.id)}">
      ${PLAY(16)} ${gesehen && gesehen < r.anzahl ? `${tr('Weiter mit')} ` + esc(naechster.title) : tr('Ansehen')}</button>` : '',
    besitzer && S.bearbeiten !== 'reihe'
      ? `<button class="btn btn-pill btn-pill--fill" data-bearbeiten="reihe">${tr('Umbenennen')}</button>` : '',
    besitzer ? (r.hidden
      ? `<button class="btn btn-pill btn-pill--fill" data-reihe-zeigen>${tr('Wiederherstellen')}</button>`
      : `<button class="btn btn-pill" style="color:#ff6961" data-reihe-aufloesen>${tr('Reihe auflösen')}</button>`) : '',
  ].join('');

  // Mit eigenem Hintergrund der Reihe wird aus der Seite ein Kopf wie
  // bei einem Film; ohne bleibt es die schlichte Überschrift.
  const kopfbild = r.backdrop ? `<div class="hero" style="height:min(44vh,420px);min-height:280px">
      ${artOf({ backdrop: r.backdrop, title: r.name }, 'wide')}<div class="hero-scrim"></div>
      <div class="hero-text" style="bottom:28px">
        <button class="btn btn-pill btn-pill--fill" style="height:32px;font-size:.8125rem;gap:6px;margin-bottom:14px" data-back>
          ${svg(I.back, 16, 2)}${tr('Zurück')}</button>
        <h1 class="t-hero">${titel}</h1>
        <div class="t-sub c-2" style="margin-top:6px">${esc(unter)}</div>
        ${aktionen ? `<div style="display:flex;gap:10px;flex-wrap:wrap;margin-top:18px">${aktionen}</div>` : ''}
      </div></div>` : '';
  return shell(`<div class="wrap${r.backdrop ? '' : ' wrap--flat'}">
    ${kopfbild || kopf(titel, esc(unter), aktionen)}
    ${r.overview ? `<p class="t-body c-2" style="max-width:640px;line-height:1.6;margin-top:${r.backdrop ? 26 : 18}px">${esc(r.overview)}</p>` : ''}
    <div class="grid" style="margin-top:30px">${r.items.map(x => posterCard(x)).join('')}</div>
    ${besitzer ? `<p class="t-caption c-2" style="margin-top:26px;max-width:640px">
      ${tr('Auflösen nimmt nur die Reihe weg, keinen Film. Lumen bildet sie danach nicht wieder — unter „Aufräumen“ kannst du sie zurückholen.')}</p>` : ''}
    <div style="height:40px"></div></div>`);
}

/* ---------------------------------------------------------------- Listen */

function listenStatus(l) {
  if (!l.meine) return tr('von {name}', { name: esc(l.von.name) }) + (l.gemeinsam ? ` ${tr('· du darfst mitschreiben')}` : '');
  return l.gemeinsam ? tr('geteilt · alle dürfen mitschreiben') : l.geteilt ? tr('im Haushalt geteilt') : tr('nur für dich');
}

function listeKarte(l) {
  const v = l.vorschau || [];
  const kachel = v.length >= 4
    ? `<div class="liste-mosaik">${v.slice(0, 4).map(x => `<div>${artOf(x, 'wide')}</div>`).join('')}</div>`
    : v.length ? artOf(v[0], 'wide')
    : `<div class="art-none" style="color:var(--label-3)">${svg(I.liste, 34, 1.6)}</div>`;
  return `<button class="card liste-karte" data-go="playlist" data-id="${esc(l.id)}" aria-label="${esc(l.name)}">
    <div class="thumb liste-bild">${kachel}</div>
    <div class="t-body clamp-1" style="margin-top:10px">${esc(l.name)}</div>
    <div class="t-sub c-2 clamp-1">${tr('{n} Titel', { n: l.anzahl })} · ${listenStatus(l)}</div>
  </button>`;
}

function viewPlaylists() {
  const alle = S.data?.lists || [];
  const meine = alle.filter(l => l.meine), andere = alle.filter(l => !l.meine);
  return shell(`<div class="wrap wrap--flat">
    <h1 class="t-large">${tr('Listen')}</h1>
    ${merkReiter('playlists')}
    <div class="t-sub c-2" style="margin:4px 0 22px">${tr('Eigene Zusammenstellungen — für dich oder mit anderen im Haushalt')}</div>
    <form id="listeform" class="sfield" style="max-width:520px" autocomplete="off">
      ${svg(I.plus, 18, 2)}<input name="name" placeholder="${tr('Neue Liste, etwa „Filmabend“')}" maxlength="80"
        aria-label="${tr('Name der neuen Liste')}">
      <button class="btn btn-pill btn-pill--fill" style="height:32px;font-size:.8125rem">${tr('Anlegen')}</button></form>
    <section style="margin-top:30px"><div class="section-h" style="padding-left:0">${tr('Meine Listen')}</div>
      ${meine.length ? `<div class="listen-grid">${meine.map(listeKarte).join('')}</div>`
        : `<p class="hint">${tr('Noch keine. Leg oben eine an, oder auf einer Titelseite mit „Zur Liste“.')}</p>`}</section>
    ${andere.length ? `<section style="margin-top:34px"><div class="section-h" style="padding-left:0">${tr('Im Haushalt geteilt')}</div>
      <div class="listen-grid">${andere.map(listeKarte).join('')}</div></section>` : ''}
    <p class="t-sub c-2" style="margin:30px 2px 0;display:flex;align-items:flex-start;gap:8px;max-width:640px">
      ${svg(I.lock, 14, 2.2)}<span>${tr('Eine Liste sieht nur, wer sie angelegt hat — bis sie geteilt wird. Auch die Verwaltung sieht sie vorher nicht.')}</span></p>
    <div style="height:40px"></div></div>`);
}

const lswRow = (label, on, key) => `
  <div class="row" style="min-height:50px"><span class="t-body" style="flex:1">${esc(label)}</span>
    <button class="switch" role="switch" aria-checked="${!!on}" data-lsw="${key}"
      aria-label="${esc(label)}"><i></i></button></div>`;

function viewPlaylist() {
  const l = S.data;
  if (!l) return shell(`<div class="wrap">${loading()}</div>`);
  const items = l.items || [];

  const titel = S.bearbeiten === 'liste' && l.meine
    ? `<form id="listenameform" style="display:flex;gap:10px;align-items:center;flex-wrap:wrap">
         <input name="name" class="titel-feld" value="${esc(l.name)}" required maxlength="80" autofocus
           aria-label="${tr('Name der Liste')}">
         <button class="btn btn-pill btn-pill--fill">${tr('Sichern')}</button>
         <button type="button" class="btn btn-pill" data-bearbeiten-aus>${tr('Abbrechen')}</button></form>`
    : esc(l.name);

  const aktionen = [
    items.length ? `<button class="btn btn-primary" data-liste-abspielen>${PLAY(16)} ${tr('Ansehen')}</button>` : '',
    l.meine && S.bearbeiten !== 'liste'
      ? `<button class="btn btn-pill btn-pill--fill" data-bearbeiten="liste">${tr('Umbenennen')}</button>` : '',
    l.meine ? (S.listeWeg
      ? `<button class="btn btn-pill" style="background:rgba(255,69,58,.16);color:#ff6961" data-liste-weg-ok>${tr('Wirklich löschen')}</button>
         <button class="btn btn-pill" data-liste-weg-nein>${tr('Abbrechen')}</button>`
      : `<button class="btn btn-pill" style="color:#ff6961" data-liste-weg>${tr('Löschen')}</button>`) : '',
  ].join('');

  const zeilen = items.map((x, i) => `
    <div class="orow" style="height:auto;padding:10px 16px;gap:14px">
      <button class="card" data-go="detail" data-id="${esc(x.id)}" style="display:flex;gap:14px;align-items:center;flex:1;min-width:0">
        <div class="thumb" style="position:relative;width:46px;height:69px;border-radius:7px;overflow:hidden;flex:none">${artOf(x)}</div>
        <div style="min-width:0">
          <div class="t-body clamp-1">${esc(x.title)}</div>
          <div class="t-caption c-2">${[x.year, x.kind === 'series' ? tr('Serie') : '',
            !l.meine || l.geteilt ? (x.hinzugefuegtVon ? tr('von {name}', { name: esc(x.hinzugefuegtVon) }) : '') : '']
            .filter(Boolean).join(' · ')}</div>
        </div>
      </button>
      ${l.darfAendern ? `<div style="display:flex;gap:4px;flex:none">
        <button class="btn btn-round sm" data-lhoch="${esc(x.id)}" aria-label="${tr('Nach oben')}" ${i === 0 ? 'disabled' : ''}>${svg(I.hoch, 16, 2.2)}</button>
        <button class="btn btn-round sm" data-lrunter="${esc(x.id)}" aria-label="${tr('Nach unten')}" ${i === items.length - 1 ? 'disabled' : ''}>${svg(I.runter, 16, 2.2)}</button>
        <button class="btn btn-round sm" style="color:#ff6961" data-lweg="${esc(x.id)}" aria-label="${tr('Aus der Liste nehmen')}">${svg(I.x, 15, 2.2)}</button>
      </div>` : ''}
    </div>`).join('');

  return shell(`<div class="wrap wrap--flat" style="max-width:900px">
    ${kopf(titel, `${tr('{n} Titel', { n: items.length })} · ${listenStatus(l)}`, aktionen)}
    ${l.meine ? `<section style="margin-top:28px">${grp(tr('Teilen'),
        lswRow(tr('Im Haushalt zeigen'), l.geteilt, 'geteilt') +
        lswRow(tr('Alle dürfen mitschreiben'), l.gemeinsam, 'gemeinsam'),
        l.geteilt ? tr('Wer sie sieht, sieht nur die Titel, die er ohnehin sehen darf.')
                  : offenesProfil() ? tr('Alle in diesem Profil sehen diese Liste.') : tr('Nur du siehst diese Liste.'))}</section>` : ''}
    <section style="margin-top:${l.meine ? 4 : 28}px"><div class="group group--inset">
      ${zeilen || `<p class="hint" style="padding:18px 16px;margin:0">${tr('Noch leer. Auf einer Titelseite legst du mit „Zur Liste“ etwas hinein.')}</p>`}</div></section>
    <div style="height:40px"></div></div>`);
}

/* „Anderen Titel zuweisen": sucht bei der Quelle und lässt wählen.
   Auf der Titelseite im Feld #zuweisen, unter „Aufräumen" im Feld unter
   dem jeweiligen Titel. modus 'id': gleich für eine Kennung oder einen
   Link (IMDb, TMDb, TheTVDB) — die Antwort ist dann genau ein Eintrag
   als Vorschau, der mit „Nehmen" zugewiesen wird. */
function zuweisenBox(itemId) {
  return document.querySelector(`[data-zuo-box="${CSS.escape(itemId)}"]`) || document.getElementById('zuweisen');
}
async function zuweisenPanel(itemId, suche, modus = 'suche') {
  const box = zuweisenBox(itemId);
  if (!box) return;
  const idModus = modus === 'id';
  box.innerHTML = `<div class="group group--inset listen-panel" style="max-width:640px">
    <form data-zuweisenform data-item="${esc(itemId)}" class="row" style="min-height:52px;gap:10px" autocomplete="off">
      <span style="color:var(--label-2);display:flex">${svg(I.search, 16, 2)}</span>
      <input name="q" value="${esc(idModus ? '' : suche)}" placeholder="${idModus ? tr('tt0133093, TMDb-Nummer oder Link einfügen …') : tr('Titel suchen, oder Link/Kennung von TMDb, IMDb, TheTVDB einfügen …')}" aria-label="${idModus ? tr('Kennung oder Link') : tr('Suchen')}"
        style="flex:1;min-width:0;border:0;outline:0;background:none;color:var(--label);font:inherit;font-size:1rem">
      ${idModus ? '' : `<input name="year" placeholder="${tr('Jahr')}" inputmode="numeric" aria-label="${tr('Jahr')}"
        style="width:64px;border:0;outline:0;background:var(--fill);border-radius:8px;padding:4px 8px;color:var(--label);font:inherit;font-size:.875rem">`}
      <button class="btn btn-pill btn-pill--fill" style="height:30px;font-size:.8125rem">${idModus ? tr('Laden') : tr('Suchen')}</button>
      <button type="button" class="btn btn-pill" style="height:30px;font-size:.8125rem" data-zuweisen-aus="${esc(itemId)}">${tr('Zu')}</button></form>
    <div data-treffer-box>${idModus
      ? `<p class="t-caption c-2" style="padding:10px 16px;margin:0;line-height:1.5">${tr('Möglich sind: IMDb-Kennung (tt…), TMDb-Nummer, TheTVDB-Nummer (tvdb:121361) oder ein Link von imdb.com, themoviedb.org oder thetvdb.com. Bei TheTVDB steht die Nummer auf der Seite der Serie unter „TheTVDB.com Series ID“.')}</p>`
      : `<p class="hint" style="padding:12px 16px;margin:0">${loading(tr('Suche …'))}</p>`}</div></div>`;
  if (idModus) { box.querySelector('input[name=q]')?.focus(); return; }
  return zuweisenSuchen(itemId, suche, '');
}
async function zuweisenSuchen(itemId, q, year) {
  const box = zuweisenBox(itemId)?.querySelector('[data-treffer-box]');
  if (!box) return;
  if (!q) return;
  box.innerHTML = `<p class="hint" style="padding:12px 16px;margin:0">${loading(tr('Suche …'))}</p>`;
  let t;
  try { t = await api.adminItemSearch(itemId, q, year); }
  catch (err) {
    // Ohne (gültigen) TMDb-Schlüssel lässt sich eine Kennung trotzdem
    // speichern: Poster und Beschreibungen kommen, sobald einer da ist.
    const ohne = err.code === 'ohne_schluessel' || err.code === 'schluessel_abgelehnt';
    box.innerHTML = `<p class="hint" style="padding:12px 16px;margin:0">${esc(err.message)}</p>${ohne ? `
      <div style="display:flex;gap:10px;flex-wrap:wrap;padding:0 16px 12px">
        <button class="btn btn-pill btn-pill--fill" style="height:30px;font-size:.8125rem" data-kennung-merken="${esc(itemId)}" data-q="${esc(q)}">${tr('Nur die Kennung speichern')}</button>
        ${S.me?.account?.isAdmin ? `<button class="btn btn-pill" style="height:30px;font-size:.8125rem" data-go="metadata">${tr('TMDb-Schlüssel eintragen')}</button>` : ''}</div>` : ''}`;
    return;
  }
  box.innerHTML = t.length ? t.map(x => `
    <button class="row orow-link" style="width:100%;min-height:64px;text-align:left;gap:12px" data-treffer="${esc(x.sourceId)}" data-item="${esc(itemId)}">
      <div class="thumb" style="position:relative;width:40px;height:60px;border-radius:6px;overflow:hidden;flex:none;background:var(--fill)">
        ${x.poster ? `<img class="art-img" src="${esc(x.poster)}" alt="" loading="lazy" referrerpolicy="no-referrer">` : ''}</div>
      <div style="flex:1;min-width:0">
        <div class="t-body clamp-1">${esc(x.title)}${x.year ? ` <span class="c-2">(${x.year})</span>` : ''}${
          x.original && x.original !== x.title ? ` <span class="t-caption c-2">· ${esc(x.original)}</span>` : ''}</div>
        ${x.overview ? `<div class="t-caption c-2 clamp-2">${esc(x.overview)}</div>` : ''}
      </div>
      <span class="t-caption c-2" style="flex:none">${tr('Nehmen')}</span></button>`).join('')
    : `<p class="hint" style="padding:12px 16px;margin:0">${tr('Nichts gefunden. Anders schreiben, oder das Jahr weglassen.')}</p>`;
}

/* Das Feld „Zur Liste" auf der Detailseite. Klappt unter den Knöpfen
   auf, ohne die Seite neu zu zeichnen. */
async function listenPanel(itemId) {
  const box = document.getElementById('listenpanel');
  if (!box) return;
  let listen;
  try { listen = (await api.playlists(itemId)).filter(l => l.darfAendern); }
  catch (err) { return toast(err.message); }
  box.innerHTML = `<div class="group group--inset listen-panel">
    ${listen.map(l => `<button class="row orow-link" style="width:100%;min-height:50px;text-align:left"
        data-lpanel="${esc(l.id)}" data-drin="${l.enthaelt ? '1' : ''}" aria-pressed="${!!l.enthaelt}">
        <span class="haken${l.enthaelt ? ' haken--an' : ''}" aria-hidden="true">${l.enthaelt ? svg(I.check, 13, 2.8) : ''}</span>
        <span class="t-body" style="flex:1">${esc(l.name)}</span>
        <span class="t-caption c-2">${l.meine ? '' : tr('von {name}', { name: esc(l.von.name) })}</span></button>`).join('')}
    <form id="neuelisteform" class="row" style="min-height:52px;gap:10px" autocomplete="off">
      <span style="color:var(--label-2);display:flex">${svg(I.plus, 16, 2)}</span>
      <input name="name" placeholder="${tr('Neue Liste …')}" maxlength="80" aria-label="${tr('Neue Liste')}"
        style="flex:1;min-width:0;border:0;outline:0;background:none;color:var(--label);font:inherit;font-size:1rem">
      <button class="btn btn-pill btn-pill--fill" style="height:30px;font-size:.8125rem">${tr('Anlegen')}</button></form>
  </div>`;
  document.querySelector('[data-zurliste]')?.setAttribute('aria-expanded', 'true');
}

/* ---------------------------------------------------------------- Wünsche */

function viewWishes() {
  const list = S.data?.wishes || [];
  const owner = S.me.account.isAdmin;
  const COL = { open: ['rgba(120,120,128,.24)', 'var(--label-2)'], working: ['rgba(10,132,255,.16)', '#4aa3ff'],
    done: ['rgba(48,209,88,.16)', '#30d158'], declined: ['rgba(255,69,58,.14)', '#ff6961'] };
  const NAME = { open: tr('offen'), working: tr('in Arbeit'), done: tr('erfüllt'), declined: tr('abgelehnt') };

  const rows = list.map(w => `
    <div class="orow wunsch-zeile" style="height:auto;padding:12px 16px;flex-wrap:wrap">
      ${wunschBild(w)}
      <div style="flex:1;min-width:140px">
        <div class="t-body wunsch-titel">${esc(w.title)}</div>
        ${wunschZusatz(w) ? `<div class="t-caption c-2">${wunschZusatz(w)}</div>` : ''}
        <div class="t-caption c-2">${w.dabei ? tr('Du hast dich angeschlossen') : tr('von {name}', { name: esc(w.by) }) + (w.mine ? ' ' + tr('(du)') : '')}</div>
      </div>
      <div class="wunsch-rechts">
      <span class="badge" style="border:0;background:${COL[w.state][0]};color:${COL[w.state][1]};flex:none">${NAME[w.state]}</span>
      ${wunschVerweise(w, { extern: false })}
      </div>
      ${owner && w.state !== 'done' ? `<div style="display:flex;gap:8px;flex:none">
          <button class="btn btn-pill btn-pill--fill" style="height:30px;font-size:.8125rem" data-wishstate="${w.id}:working">${tr('Annehmen')}</button>
          <button class="btn btn-pill" style="height:30px;font-size:.8125rem;background:rgba(255,69,58,.14);color:#ff6961" data-wishstate="${w.id}:declined">${tr('Ablehnen')}</button>
        </div>` : ''}
    </div>`).join('');

  return shell(`<div class="wrap wrap--flat" style="max-width:820px">
    <h1 class="t-large">${tr('Wünsche')}</h1>
    <div class="t-sub c-2" style="margin:4px 0 24px">
      ${owner ? tr('Was sich der Haushalt wünscht') : tr('Was du dir gewünscht hast, und wie es steht')}</div>
    ${wunschFeld({ id: 'wishform', symbol: svg(I.plus, 18, 2) })}
    <p class="t-caption c-2" style="margin:8px 2px 0">
      ${tr('Tipp einen Namen und wähl einen Treffer — oder wünsch nur den Namen.')}
      ${tr('Der Besitzer sieht den Titel und wer ihn gewünscht hat — sonst nichts.')}</p>
    <section style="margin-top:24px">${rows ? `<div class="group group--inset">${rows}</div>`
      : leer({ symbol: I.wish, titel: tr('Noch keine Wünsche'), text: tr('Fehlt dir ein Film oder eine Serie? Wünsch sie dir oben.') })}</section>
    <div style="height:40px"></div></div>`);
}

/* ---------------------------------------------------------------- Einstellungen */

// Ein Profil ohne PIN öffnet zu Hause jede Person – „nur du siehst das"
// wäre dort nicht ehrlich.
const offenesProfil = () => (S.me?.account?.lockKind || 'none') === 'none';

const swRow = (label, on, value, key) => `
  <div class="row" style="min-height:50px"><span class="t-body" style="flex:1">${esc(label)}</span>
    ${on === null ? `<span style="color:var(--label-2);font-size:.9375rem">${esc(value)}</span>`
      : `<button class="switch" role="switch" aria-checked="${!!on}" ${key ? `data-pref="${key}"` : 'data-sw="1"'}
          aria-label="${esc(label)}"><i></i></button>`}</div>`;

const grp = (title, rows, foot = '') => `
  <section style="margin-bottom:24px"><div class="section-h">${esc(title)}</div>
    <div class="group group--inset">${rows}</div>
    ${foot ? `<p class="t-caption c-2" style="margin:8px 16px 0">${esc(foot)}</p>` : ''}</section>`;

/* Die Fundstelle des Quelltexts, aber nur wenn sie eine Web-Adresse ist.
 * Ein Server, der hier etwas anderes einträgt — javascript:, data: —, soll
 * in der Oberfläche keinen anklickbaren Link erzeugen. */
function quelle() {
  const raw = (S.hello?.sourceUrl || '').trim();
  if (!raw) return '';
  try {
    const u = new URL(raw);
    return (u.protocol === 'https:' || u.protocol === 'http:') ? u.href : '';
  } catch { return ''; }
}

function viewSettings() {
  const a = S.me.account, lim = S.me.limits || {}, prefs = S.me.prefs || {};
  const devices = (S.data?.devices || []).map(geraetZeile).join('');
  // Was in diesem Profil gerade läuft — nur hier, nur für dieses Profil.
  // Ein vergessener Fernseher lässt sich so selbst beenden.
  const laufend = (S.data?.playbacks || []).map(p => `
    <div class="row" style="min-height:52px;gap:12px">
      <span style="flex:1;min-width:0"><span class="t-body">${esc(p.deviceName)}</span>
        <span class="t-caption c-2" style="display:block">${p.mode === 'direct' ? tr('direkt') : tr('wird umgewandelt')}${p.detail ? ' · ' + esc(p.detail) : ''}</span></span>
      <button class="btn btn-pill" style="height:32px;font-size:.8125rem;background:rgba(255,69,58,.14);color:#ff6961;flex:none"
        data-meinstop="${esc(p.id)}">${tr('Beenden')}</button></div>`).join('');

  return shell(`<div class="wrap wrap--flat" style="max-width:1100px">
    <header style="display:flex;align-items:center;gap:18px">
      ${avatar(a, 64)}
      <div><h1 class="t-large" style="margin:0">${esc(a.name)}</h1>
        <div class="t-sub c-2" style="margin-top:2px">${a.isOwner ? tr('Besitzer dieses Servers') : a.vollzugriff ? tr('Vollzugriff') : a.isAdmin ? tr('Mitverwalter') + (a.groupName ? ' · ' + esc(a.groupName) : '') : a.isKid ? tr('Kinderprofil') : a.groupName ? esc(a.groupName) : tr('Haushalt')}</div></div>
      <span style="flex:1"></span>
      <button class="btn btn-pill btn-pill--fill" data-signout>${tr('Abmelden')}</button>
    </header>
    ${S.schutzNoetig ? `<div class="surface" role="alert" style="margin-top:22px;padding:16px 18px;border-radius:16px;display:flex;gap:14px;align-items:center;flex-wrap:wrap">
      ${svg(I.lock, 18, 2.2)}<span class="t-body" style="flex:1;min-width:220px">${esc(T.verwaltungBrauchtSchutz)}</span>
      <button class="btn btn-pill btn-pill--fill" data-schutz>${tr('PIN festlegen')}</button></div>` : ''}
    <div class="nur-handy" style="margin-top:24px">
      ${grp(tr('Mehr'), linkRow(tr('Neuigkeiten'), S.me?.neuigkeiten ? tr('{n} ungelesen', { n: S.me.neuigkeiten }) : tr('was dazugekommen ist'), 'news')
        + linkRow(tr('Favoriten'), tr('was dir besonders gefallen hat'), 'favorites')
        + linkRow(tr('Filmreihen'), tr('von Lumen zusammengestellt'), 'collections')
        + linkRow(tr('Listen'), tr('eigene und geteilte'), 'playlists')
        + linkRow(tr('Wünsche'), tr('ansehen und wünschen'), 'wishes')
        + (verwaltungHier() ? linkRow(tr('Verwaltung'), tr('Bibliotheken, Konten, Server'), 'admin') : ''))}
    </div>
    <div style="display:grid;grid-template-columns:repeat(auto-fit,minmax(min(360px,100%),1fr));gap:0 40px;margin-top:30px">
      <div>
        ${grp(tr('Wiedergabe'),
          swRow(tr('Folgen automatisch weiter'), prefs.autoplay !== '0', null, 'autoplay') +
          swRow(tr('Vorspann überspringen'), prefs.skipIntro === '1', null, 'skipIntro') +
          swRow(tr('Nach drei Folgen fragen, ob du noch schaust'), prefs.nochDaFrage !== '0', null, 'nochDaFrage') +
          swRow(tr('Qualität unterwegs'), null, lim.remoteQuality === 'original' ? tr('Original') : lim.remoteQuality || '1080p'))}
        ${grp(tr('Profilbild'), `<div class="avatar-wahl">
            ${['av-a', 'av-b', 'av-c', 'av-d', 'av-k'].map(c => `<button class="avatar ${c}${a.avatar === c ? ' gewaehlt' : ''}" style="--av:44px;border:0" data-avatar="${c}" aria-label="${tr('Farbe {nr}', { nr: c.slice(3).toUpperCase() })}">${esc(a.name[0])}</button>`).join('')}
            ${Object.keys(FIGUREN).map(f => `<button class="avatar avatar--figur${a.avatar === 'fig-' + f ? ' gewaehlt' : ''}" style="--av:44px;border:0;background:${FIGUREN[f][0]}" data-avatar="fig-${f}" aria-label="${f}">
              <svg viewBox="0 0 24 24" fill="none" stroke="#fff" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round">${FIGUREN[f][1]}</svg></button>`).join('')}
            <label class="avatar avatar-hochladen${a.avatar === 'bild' ? ' gewaehlt' : ''}" style="--av:44px;background:var(--fill-2);color:var(--label-2);cursor:pointer">
              <span class="sr-only">${tr('Eigenes Bild hochladen')}</span>
              ${a.avatar === 'bild' ? `<img src="/api/v1/avatar/${esc(a.id)}?v=${S.avatarStand || 0}" alt="" style="width:100%;height:100%;object-fit:cover;border-radius:50%">` : svg('<path d="M12 5v14M5 12h14"/>', 18, 2.2)}
              <input type="file" accept="image/*" data-avatarbild class="sr-only"></label>
          </div>`, tr('Eine Farbe, eine Figur oder ein eigenes Bild — Lumen verkleinert es auf 256 Pixel.'))}
        ${grp(tr('Untertitel'), `<div class="row" style="min-height:54px"><span class="t-body" style="flex:1" id="ut-groesse">${tr('Grösse')}</span>
            ${segment(tr('Grösse der Untertitel'), [['klein', tr('Klein')], ['normal', tr('Normal')], ['gross', tr('Gross')]].map(([v, n]) =>
              ({ name: n, an: (prefs.utGroesse || 'normal') === v, attr: `data-prefwert="utGroesse:${v}"` })))}
            </div>`
          + swRow(tr('Dunkler Hintergrund hinter dem Text'), prefs.utHintergrund !== '0', null, 'utHintergrund'),
          tr('Gilt im Browser und in den Apps.'))}
        ${grp(tr('Sprache'), `<div class="row" style="min-height:54px;gap:12px">
            <label for="prefsprache" class="sr-only">${tr('Sprache und Region')}</label>
            <select id="prefsprache" class="takt" data-prefsprache style="flex:1;min-width:0">
              <option value="">${tr('Wie der Server – {name}', { name: esc(sprachNameVon(S.hello?.sprache || 'de-CH')) })}</option>
              ${sprachWahl().map(s => `<option value="${esc(s.id)}" lang="${esc(s.id)}"${prefs.sprache === s.id ? ' selected' : ''}>${esc(s.name)}</option>`).join('')}
            </select></div>`, tr('Gilt für dein Profil auf allen Geräten.'))}
        ${grp(tr('Darstellung'), `<div class="row" style="min-height:54px">
            ${segment(tr('Darstellung'), [['dunkel', tr('Dunkel')], ['hell', tr('Hell')], ['auto', tr('Wie das Gerät')]].map(([v, n]) =>
              ({ name: n, an: darstellung() === v, attr: `data-darstellung="${v}"` })))}
            </div>
            <div class="row" style="min-height:54px;flex-wrap:wrap"><span class="t-body" style="flex:1">${tr('Textgrösse')}</span>
            ${segment(tr('Textgrösse'), [['normal', tr('Normal')], ['gross', tr('Gross')], ['sehrgross', tr('Sehr gross')]].map(([v, n]) =>
              ({ name: n, an: textgroesse() === v, attr: `data-textgroesse="${v}"` })))}
            </div>`, tr('Gilt für diesen Browser. Die Schrift wächst auch mit der Textgrösse von Browser und System.'))}
        ${a.isKid && bzStand()?.aktiv ? grp(tr('Bildschirmzeit'), bzIchZeile(),
          bzTagText()) : ''}
        ${grp(tr('Privatsphäre'),
          swRow(tr('Bewertungen im Haushalt zeigen'), prefs.shareRatings === '1', null, 'shareRatings'),
          [a.isKid && bzStand()?.aktiv
            ? tr('Dein Verlauf und deine Bewertungen gehören dir. Die Verwaltung sieht nur ungefähr, wie viel Bildschirmzeit du heute gebraucht hast, und erst eine Weile später — nicht, was du geschaut hast, und nicht, wann.')
            : tr('Dein Verlauf und deine Bewertungen gehören dir. Auch der Besitzer des Servers kann nicht sehen, wann oder wie lange du etwas schaust.'),
           prefs.shareRatings === '1' ? tr('Deine Bewertungen sieht der Haushalt mit deinem Namen; ausschalten nimmt auch die bisherigen zurück.') : '',
           offenesProfil() ? tr('Ohne PIN kann jede Person zu Hause dieses Profil öffnen und alles darin sehen.') : ''].filter(Boolean).join(' '))}
      </div>
      <div>
        ${grp(tr('Profil schützen'),
          `<div class="row" style="min-height:54px;gap:12px">
             <span class="t-body" style="flex:1">${esc(schutzText(a.lockKind))}</span>
             <button class="btn btn-pill btn-pill--fill" style="flex:none" data-schutz>${a.lockKind === 'none' ? tr('PIN festlegen') : tr('Ändern')}</button></div>`,
          a.isAdmin ? tr('Mit Zugang zur Verwaltung braucht dein Profil eine PIN oder ein Passwort.') : tr('Wer die PIN vergessen hat, kann sie von der Verwaltung zurücksetzen lassen.'))}
        ${grp(tr('Umzug'), einstellungenUmzug(),
          tr('Nur für dein Profil. Lumen bewahrt weder Passwort noch Zugang auf, und die Verwaltung sieht davon nichts.'))}
        ${grp(tr('Fernseher anmelden'), `<form id="codeform" class="row" style="min-height:58px;gap:10px;flex-wrap:wrap">
            <label for="tvcode" class="t-body" style="flex:1;min-width:180px">${tr('Code vom Fernseher')}
              <span class="t-caption c-2" style="display:block">${tr('Der Fernseher meldet sich dann mit deinem Profil an.')}</span></label>
            <input id="tvcode" name="code" inputmode="numeric" autocomplete="one-time-code" placeholder="123 456" maxlength="7"
              style="width:120px;height:38px;border-radius:10px;border:0;background:var(--fill);color:var(--label);font:inherit;font-size:1.125rem;text-align:center;letter-spacing:.12em">
            <button class="btn btn-pill btn-pill--fill" type="submit">${tr('Anmelden')}</button></form>`)}
        ${laufend ? grp(tr('Läuft gerade in deinem Profil'), laufend) : ''}
        ${grp(tr('Geräte'), devices +
          `<button class="btn row" style="width:100%;justify-content:flex-start;min-height:48px;color:var(--rot)" data-signoutall>
             <span class="t-body">${tr('Überall abmelden')}</span></button>`, tr('Zuletzt aktiv steht nur auf den Tag genau da — und nur hier, in deinem Profil.'))}
        ${grp(tr('Über Lumen'),
          `<div class="row" style="min-height:50px"><span class="t-body" style="flex:1">${tr('Fassung')}</span>
             <span style="color:var(--label-2);font-size:.9375rem">${esc(S.hello?.version || '')}</span></div>
           <div class="row" style="min-height:50px"><span class="t-body" style="flex:1">${tr('Lizenz')}</span>
             <span style="color:var(--label-2);font-size:.9375rem">${esc(S.hello?.license || 'AGPL-3.0-only')}</span></div>` +
          (quelle() ? `<a class="row orow-link" style="min-height:50px;text-decoration:none;color:inherit"
              href="${esc(quelle())}" target="_blank" rel="noopener noreferrer">
             <span class="t-body" style="flex:1">${tr('Quelltext')}</span>
             <span style="display:inline-flex;align-items:center;gap:6px;color:#4aa3ff;font-size:.9375rem">
               ${tr('ansehen')}${svg(I.chev, 14, 2.4)}</span></a>` : ''),
          tr('Lumen ist freie Software. Du darfst sie benutzen, verändern und weitergeben — wer sie verändert und betreibt, muss seine Änderungen ebenfalls offenlegen.'))}
        ${grp(tr('Verlauf'),
          `<button class="btn row" style="width:100%;justify-content:flex-start;min-height:48px;color:var(--rot)" data-clearhistory>
             <span class="t-body">${tr('Verlauf löschen')}</span></button>`,
          tr('Entfernt alle Fortschritte und Haken. Die Dateien bleiben.'))}
      </div>
    </div><div style="height:40px"></div></div>`);
}

/* ---------------------------------------------------------------- Verwaltung */

const ohead = (title, sub, back = 'admin') => `
  <header>
    ${back ? `<button class="btn btn-pill btn-pill--fill" style="height:32px;font-size:.8125rem;gap:6px;margin-bottom:14px" data-go="${back}">
      ${svg(I.back, 16, 2)}${back === 'admin' ? tr('Verwaltung') : tr('Startseite')}</button>` : ''}
    <h1 class="t-large" style="margin:0">${esc(title)}</h1>
    <div class="t-sub c-2" style="margin-top:4px">${esc(sub)}</div>
  </header>`;

const stat = (t, v, sub, bar) => `
  <div class="surface" style="padding:18px 20px;border-radius:16px">
    <div class="t-sub c-2">${esc(t)}</div><div class="t-title2" style="margin-top:6px">${esc(v)}</div>
    ${bar != null ? `<div class="progress" style="margin-top:12px;height:4px"><i style="width:${bar}%"></i></div>` : ''}
    <div class="t-caption c-2" style="margin-top:${bar != null ? 8 : 6}px">${esc(sub)}</div></div>`;

const linkRow = (label, value, view) =>
  `<button class="row orow-link" data-go="${view}" style="width:100%;min-height:50px;text-align:left">
     <span class="t-body" style="flex:1">${esc(label)}</span>
     <span style="display:inline-flex;align-items:center;gap:6px;color:var(--label-2);font-size:.9375rem">${esc(value)}${svg(I.chev, 14, 2.4)}</span></button>`;

/* Der Fernzugriff in der Verwaltung.
 *
 * Was hier steht, beschreibt die Leitung: verbunden oder nicht, seit wann,
 * zu welchem Vermittler. Nicht, wer von aussen zuschaut — der Vermittler
 * wüsste es selbst nicht. */
function fernzugriffGruppe(f) {
  if (!f) return '';
  if (!f.an) {
    return grp(tr('Von aussen erreichbar'),
      `<div class="row" style="min-height:52px">
         <span class="t-body" style="flex:1">${tr('Fernzugriff')}</span>
         <span style="color:var(--label-2);font-size:.9375rem">${tr('nicht eingerichtet')}</span></div>`,
      f.hinweis || '');
  }
  const steht = !!f.verbunden;
  const seit = steht && f.seit
    ? tr('seit {zeit}', { zeit: datumZeit(f.seit, { dateStyle: 'short', timeStyle: 'short' }) })
    : '';
  return grp(tr('Von aussen erreichbar'),
    `<div class="row" style="min-height:52px">
       <span style="color:${steht ? '#30d158' : '#ff9f0a'};display:flex">
         ${svg(steht ? I.check : '<path d="M12 4 2.8 19.5h18.4z"/><path d="M12 10v4.5M12 17v.01"/>', 18)}</span>
       <div style="flex:1;min-width:0">
         <div class="t-body">${steht ? tr('Verbunden') : tr('Getrennt')}</div>
         <div class="t-sub c-2 clamp-1">${esc(f.vermittler || '')}${seit ? ' · ' + seit : ''}</div></div></div>` +
    (!steht && f.letzterFehler
      ? `<div class="row" style="min-height:44px"><span class="t-sub c-2 clamp-2">
           ${esc(f.letzterFehler)}${f.versuche ? ` ${tr('· {versuche}. Versuch', { versuche: f.versuche })}` : ''}</span></div>`
      : '') +
    (f.eigenerFingerabdruck
      ? `<div class="row" style="min-height:62px;align-items:flex-start;padding-top:12px">
           <div style="flex:1;min-width:0">
             <div class="t-body">${tr('Fingerabdruck dieses Servers')}</div>
             <div class="t-caption c-2 mono" style="margin-top:4px;word-break:break-all;
                  line-height:1.5">${esc(f.eigenerFingerabdruck)}</div></div>
           <button class="btn btn-pill btn-pill--fill" style="flex:none;margin-top:2px"
                   data-kopieren="${esc(f.eigenerFingerabdruck)}">${tr('Kopieren')}</button></div>`
      : ''),
    tr('Der Vermittler reicht nur verschlüsselte Bytes durch. Was jemand schaut, erfährt er nicht — auch wenn du ihn selbst betreibst. Den Fingerabdruck brauchen die Apps einmal, um diesen Server wiederzuerkennen.'));
}

/* §13 der AGPL verlangt, dass wer Lumen über das Netz benutzt, an den
 * Quelltext kommt. Lumen nennt ihn von selbst; hier steht nur, woher.
 *
 * Hier stand erst eine Warnung, solange die Voreinstellung galt. Das war
 * richtig, als dort ein Platzhalter stand. Jetzt zeigt die Voreinstellung
 * auf den echten Quelltext — und eine Warnung, die bei jedem unveränderten
 * Server angeht, lehrt nur, Warnungen zu übersehen. */
function quelltextZeile(q) {
  if (!q || !q.url) return '';
  let wo = q.url;
  try { const u = new URL(q.url); wo = u.host + u.pathname; } catch {}
  return grp(tr('Quelltext'),
    `<a class="row orow-link" style="min-height:50px;text-decoration:none;color:inherit"
        href="${esc(q.url)}" target="_blank" rel="noopener noreferrer">
       <span class="t-body" style="flex:1">${q.eigen ? tr('Deine Fassung') : tr('Unverändert')}</span>
       <span style="display:inline-flex;align-items:center;gap:6px;color:#4aa3ff;font-size:.9375rem">
         ${esc(wo)}${svg(I.chev, 14, 2.4)}</span></a>`,
    q.eigen
      ? tr('Diese Adresse bekommen alle zu sehen, die den Server benutzen.')
      : tr('Änderst du etwas am Quelltext, trag deine eigene Fundstelle über LUMEN_SOURCE_URL ein — das verlangt die Lizenz.'));
}

/* Was in der Übersicht neben „Poster und Beschreibungen" steht. Die
   Zeile soll von selbst sagen, ob noch etwas fehlt. */
function metaZeile(m) {
  if (!m) return tr('TMDb-Schlüssel eintragen');
  if (m.schluessel === 'abgelehnt') return tr('TMDb lehnt den Schlüssel ab');
  if (!m.tmdb?.configured) {
    // Ohne Schlüssel zählen die Poster aus der Sammlung (NFO, poster.jpg).
    if (m.titel && m.mitPoster >= m.titel) return tr('kein Schlüssel — alle Poster aus der Sammlung');
    if (m.titel) return tr('kein Schlüssel — {n} von {gesamt} Titeln mit Poster', { n: m.mitPoster || 0, gesamt: m.titel });
    return tr('kein Schlüssel');
  }
  return m.imdb?.configured ? tr('TMDb und OMDb hinterlegt') : tr('TMDb hinterlegt');
}

function viewAdmin() {
  const d = S.data || {};
  const c = d.cleanup || {};
  const load = d.load || {};
  // Die Seitenleiste führt schon zur Startseite; ein eigener Knopf dafür
  // war doppelt. Breite wie unter „Ich", Kacheln ohne Löcher.
  return shell(`<div class="wrap wrap--flat wrap--verwaltung">
    ${ohead(tr('Verwaltung'), `Lumen ${esc(d.version)} · ${laufzeitText(d.uptimeMs)} · ${tr('nur für dich sichtbar')}`, null)}
    <div class="kacheln" style="margin-top:28px">
      ${stat(tr('Gerade aktiv'), tr('{n} Wiedergaben', { n: load.total || 0 }),
        load.transcodes ? tr('{n} davon umgewandelt', { n: load.transcodes }) : tr('alle direkt'))}
      ${stat(tr('Bibliotheken'), String((d.libraries || []).length),
        tr('{n} Titel', { n: (d.libraries || []).reduce((n, l) => n + l.count, 0) }))}
      ${stat(tr('Konten'), String(d.accounts || 0), tr('im Haushalt'))}
      ${stat('FFmpeg', d.ffmpeg ? tr('vorhanden') : tr('fehlt'),
        d.ffmpeg ? tr('Umwandeln ist möglich') : tr('nur direktes Abspielen'))}
      ${d.platz && d.platz.datenMB ? stat(tr('Freier Platz'), platzText(d.platz.datenMB),
        d.platz.datenMB < 5120 ? tr('knapp — bald aufräumen') : tr('für Datenbank und Bilder')) : ''}
      ${voll() ? stat(tr('Sicherung'), d.sicherung ? tagText(d.sicherung) : tr('noch keine'),
        tr('zwei Generationen')) : ''}
    </div>
    <p class="t-sub c-2" style="margin:22px 0 0;display:flex;align-items:center;gap:8px">
      ${svg(I.lock, 14, 2.2)}${tr('Lumen zeigt nicht, wer was schaut. Verlauf und Bewertungen gehören jeder Person selbst.')}</p>

    <div style="display:grid;grid-template-columns:repeat(auto-fit,minmax(min(380px,100%),1fr));gap:0 40px;margin-top:28px">
      <div>
        ${grp(tr('Mediathek'),
          linkRow(tr('Bibliotheken'), tr('{n} eingerichtet', { n: (d.libraries || []).length }), 'adminLibraries') +
          // Der Name allein sagte nicht, dass hier die Schlüssel von TMDb
          // und OMDb hingehören — gesucht wurde danach anderswo.
          linkRow(tr('Poster und Beschreibungen'), metaZeile(d.meta), 'metadata') +
          linkRow(tr('Wünsche'), d.wuensche
            ? (d.wuensche.offen
                ? tr('{n} offen', { n: d.wuensche.offen })
                : tr('{n} insgesamt, nichts offen', { n: (d.wuensche.wishes || []).length }))
            : tr('ansehen'), 'adminWishes') +
          linkRow(tr('Aufräumen'),
            tr('{doppel} in Teilen · {fehlen} fehlen', { doppel: c.duplicates || 0, fehlen: c.missing || 0 }), 'cleanup'))}
      </div>
      <div>
        ${grp(tr('System'),
          linkRow(tr('Aktive Wiedergaben'), tr('{n} laufen', { n: load.total || 0 }), 'sessions') +
          linkRow(tr('Protokoll'), tr('ansehen'), 'logs') +
          linkRow(tr('Konten und Rechte'), tr('{n} Personen', { n: d.accounts || 0 }), 'accounts') +
          (voll() ? linkRow(tr('Gruppen'), d.gruppen ? tr('{n} Gruppen', { n: d.gruppen.length }) : tr('Rechte bündeln'), 'adminGroups') : '') +
          linkRow(tr('Verwaltungssperre'), sperreName(S.me.account.adminLock), 'adminLock') +
          (voll() ? linkRow(tr('Updates'), updateKurz(d.update, d.version), 'adminUpdate') : '') +
          (voll() ? linkRow(tr('Sicherung'), d.sicherung ? tr('zuletzt {wann}', { wann: tagText(d.sicherung) }) : tr('noch keine'), 'adminBackups') : '') +
          (voll() ? linkRow(tr('Umwandeln'), tr('Hardware, Vorschaubilder'), 'adminUmwandlung') : ''))}
        ${fernzugriffGruppe(d.fern)}
        ${voll() ? grp(tr('Name dieses Servers'), `<form id="servernameform" class="row" style="min-height:54px;gap:10px">
            <label for="servername" class="sr-only">${tr('Name dieses Servers')}</label>
            <input id="servername" name="name" maxlength="40" value="${esc(S.hello?.name || '')}" placeholder="${tr('z. B. Mein NAS')}"
              style="flex:1;min-width:0;height:36px;border:0;border-radius:10px;background:var(--fill);color:var(--label);font:inherit;padding:0 12px">
            <button class="btn btn-pill btn-pill--fill" type="submit">${tr('Speichern')}</button></form>`,
            tr('So heisst der Server in den Apps, wenn jemand mehrere gespeichert hat.')) : ''}
        ${voll() ? grp(tr('Sprache des Servers'), `<div class="row" style="min-height:54px;gap:12px">
            <label for="serversprache" class="sr-only">${tr('Sprache und Region')}</label>
            <select id="serversprache" class="takt" data-serversprache style="flex:1;min-width:0">
              ${sprachWahl().map(s => `<option value="${esc(s.id)}" lang="${esc(s.id)}"${(S.hello?.sprache || 'de-CH') === s.id ? ' selected' : ''}>${esc(s.name)}</option>`).join('')}
            </select></div>`,
            tr('Gilt für die Profilauswahl, neue Geräte und alle Profile ohne eigene Wahl. Sprache und Region der Metadaten stellst du unter „Poster und Beschreibungen“ ein.')) : ''}
        ${quelltextZeile(d.source)}
        ${d.meta && (!d.meta.tmdb?.configured || d.meta.schluessel === 'abgelehnt') ? grp(tr('Hinweis'),
          `<div class="row" style="min-height:52px">
             <span style="color:${d.meta.schluessel === 'abgelehnt' ? '#ff6961' : '#ff9f0a'};display:flex">${svg('<path d="M12 4 2.8 19.5h18.4z"/><path d="M12 10v4.5M12 17v.01"/>', 20)}</span>
             <div style="flex:1"><div class="t-body">${d.meta.schluessel === 'abgelehnt' ? tr('TMDb lehnt den Schlüssel ab') : tr('Kein TMDb-Schlüssel hinterlegt')}</div>
               <div class="t-sub c-2">${d.meta.schluessel === 'abgelehnt'
                 ? tr('Bitte den langen API Read Access Token eintragen')
                 : d.meta.titel && d.meta.mitPoster >= d.meta.titel
                   ? tr('Poster und Beschreibungen kommen aus der Sammlung (NFO und Bilder)')
                   : tr('{n} von {gesamt} Titeln haben ein Poster aus der Sammlung — für die übrigen braucht es den Schlüssel', { n: d.meta.mitPoster || 0, gesamt: d.meta.titel || 0 })}</div></div>
             <button class="btn btn-pill btn-pill--fill" data-go="metadata">${d.meta.schluessel === 'abgelehnt' ? tr('Schlüssel ändern') : tr('Eintragen')}</button></div>`) : ''}
        ${c.pending && d.meta?.tmdb?.configured && d.meta.schluessel !== 'abgelehnt' ? grp(tr('Hinweis'),
          `<div class="row" style="min-height:52px">
             <span style="color:#ff9f0a;display:flex">${svg('<path d="M12 4 2.8 19.5h18.4z"/><path d="M12 10v4.5M12 17v.01"/>', 20)}</span>
             <div style="flex:1"><div class="t-body">${tr('{pending} Titel ohne Metadaten', { pending: c.pending })}</div>
               <div class="t-sub c-2">${tr('Beschreibungen und Poster fehlen noch')}</div></div>
             <button class="btn btn-pill btn-pill--fill" data-resolve>${tr('Jetzt holen')}</button></div>`) : ''}
      </div>
    </div><div style="height:40px"></div></div>`);
}

/* Eine Zeile pro Bibliothek. Eigene Funktion, weil der Fortschritt
   während eines Durchlaufs fortgeschrieben wird, ohne die ganze Ansicht
   neu zu zeichnen — sonst verlöre das Formular darunter, was jemand
   gerade eingetippt hat. */
const hinweisZeile = text => `
  <div class="t-caption" style="margin-top:8px;color:#ffb340;display:flex;gap:7px">
    ${svg('<path d="M12 4 2.8 19.5h18.4z"/><path d="M12 10v4.5M12 17v.01"/>', 14, 2.2)}
    <span>${esc(text)}</span></div>`;

function bibZeile(l) {
  const st = l.stand || {};
  const geprueft = l.scannedAt
    ? tr('zuletzt geprüft {wann}', { wann: datumZeit(l.scannedAt) })
    : tr('noch nie geprüft');
  const e = l.lastResult;
  const ergebnis = e ? `${e.auto ? tr('von selbst') : tr('auf Knopfdruck')}: ${
    [e.added ? tr('{n} neu', { n: e.added }) : '', e.updated ? tr('{updated} geändert', { updated: e.updated }) : '',
     e.missing ? tr('{n} fehlen', { n: e.missing }) : '', e.unmatched ? tr('{unmatched} nicht erkannt', { unmatched: e.unmatched }) : '']
      .filter(Boolean).join(', ') || tr('nichts Neues')}` : '';
  // Takt in Minuten — oder „zeit": täglich zu einer festen Uhrzeit.
  const fest = !!l.scanAt;
  const takt = [[0, tr('nur auf Knopfdruck')], [15, tr('alle 15 Minuten')], [30, tr('alle 30 Minuten')], [60, tr('jede Stunde')],
    [120, tr('alle 2 Stunden')], [360, tr('alle 6 Stunden')], [720, tr('alle 12 Stunden')], [1440, tr('alle 24 Stunden')],
    ['zeit', tr('täglich um …')]].map(([m, n]) =>
    `<option value="${m}"${(fest ? m === 'zeit' : l.intervalMin === m) ? ' selected' : ''}>${n}</option>`).join('');
  const uhrzeit = `<input type="time" class="takt" data-taktzeit="${esc(l.id)}" value="${esc(l.scanAt || '04:00')}"
    aria-label="${tr('Uhrzeit, zu der Lumen täglich nachsieht')}" style="${fest ? '' : 'display:none'}">`;

  let mitte = '';
  if (st.laeuft) {
    // gesamt kommt aus dem Vorlauf und steht fest; gefunden wächst
    // mit. Nur mit gesamt lässt sich ein Anteil zeigen — fehlt er noch,
    // pulsiert der Balken, statt eine Zahl zu erfinden.
    const ges = st.gesamt || 0, fer = st.fertig || 0;
    const anteil = ges ? Math.min(100, Math.round(fer / ges * 100)) : 0;
    const sek = st.seitMs ? Math.max(0, Math.round((Date.now() - st.seitMs) / 1000)) : 0;
    const dauer = sek < 60 ? `${sek} s` : `${Math.floor(sek / 60)} min`;
    mitte = `
      <div style="margin-top:10px">
        <div style="height:4px;border-radius:2px;background:var(--fill);overflow:hidden">
          <div style="height:100%;width:${ges ? anteil : 100}%;border-radius:2px;background:var(--accent);
            transition:width .4s var(--ease)${ges ? '' : ';animation:pulse 1.4s ease-in-out infinite'}"></div></div>
        <div class="t-caption c-2" style="margin-top:6px;display:flex;gap:8px">
          <span class="spin spin--s" style="flex:none"></span>
          <span class="clamp-1">${ges
            ? `${tr('{fer} von {ges} eingelesen', { fer, ges })}${anteil ? ` · ${anteil} %` : ''}${
                st.datei ? ' · ' + esc(st.datei) : ''}`
            : tr('sucht nach Videodateien …')} · ${tr('läuft seit {dauer}', { dauer })}</span></div>
      </div>`;
  } else if (l.wartet) {
    mitte = `<div class="t-caption c-2" style="margin-top:10px;display:flex;gap:8px">
        <span class="spin spin--s" style="flex:none"></span>
        <span>${tr('Angenommen — wartet, bis die andere Arbeit fertig ist, dann geht es von selbst los.')}</span></div>`;
  } else if (l.metadaten?.laeuft) {
    const m = l.metadaten;
    const anteil = m.gesamt ? Math.min(100, Math.round(m.fertig / m.gesamt * 100)) : 0;
    mitte = `
      <div style="margin-top:10px">
        <div style="height:4px;border-radius:2px;background:var(--fill);overflow:hidden">
          <div style="height:100%;width:${anteil}%;border-radius:2px;background:var(--accent);transition:width .4s var(--ease)"></div></div>
        <div class="t-caption c-2" style="margin-top:6px;display:flex;gap:8px">
          <span class="spin spin--s" style="flex:none"></span>
          <span class="clamp-1">${tr('Metadaten: {fertig} von {gesamt}', { fertig: m.fertig, gesamt: m.gesamt })}${m.titel ? ' · ' + esc(m.titel) : ''}</span></div>
      </div>`;
  } else if (!l.erreichbar) {
    mitte = hinweisZeile(l.problem || tr('Der Ordner ist nicht erreichbar.'));
  } else if (l.scannedAt && !l.count) {
    // Geprüft und nichts gefunden: fast immer steht die Art falsch oder
    // der Ordner ist eine Ebene daneben. Das soll hier stehen und nicht
    // im Protokoll, wo niemand nachsieht.
    mitte = hinweisZeile(tr('Geprüft, aber nichts gefunden. Liegen die Dateien wirklich hier — und sind es Videodateien? Unterordner liest Lumen mit.'));
  }

  const knoepfe = S.delLib === l.id
    ? `<button class="btn btn-pill" style="flex:none;background:rgba(255,69,58,.16);color:#ff6961"
         data-delok="${esc(l.id)}">${tr('Wirklich löschen')}</button>
       <button class="btn btn-pill" style="flex:none" data-delno>${tr('Abbrechen')}</button>`
    : `<button class="btn btn-pill btn-pill--fill" style="flex:none" data-scan="${esc(l.id)}"
         ${st.laeuft || l.wartet ? 'disabled' : ''}>${st.laeuft ? tr('läuft …') : l.wartet ? tr('wartet …') : tr('Prüfen')}</button>
       <button class="btn btn-pill" style="flex:none;color:#ff6961" data-del="${esc(l.id)}"
         aria-label="${tr('Bibliothek löschen')}" title="${tr('Löschen')}">${svg(I.trash, 16)}</button>`;

  return `<div style="display:flex;align-items:flex-start;gap:12px">
    <div style="flex:1;min-width:0">
      <div class="t-body">${esc(l.name)}${
        // Nur dazusagen, was der Name nicht schon sagt — „Serien · Serien"
        // ist keine Auskunft.
        l.kind === 'series' && !/serie/i.test(l.name) ? ` ${tr('· Serien')}` : ''}</div>
      <div class="t-caption c-2 clamp-1 mono">${esc(l.path)}</div>
      <div class="t-caption c-2">${tr('{n} Titel · {geprueft}', { n: l.count, geprueft })}${ergebnis ? ' · ' + esc(ergebnis) : ''}</div>
      ${mitte}
      <div style="display:flex;gap:10px;align-items:center;flex-wrap:wrap;margin-top:8px">
        <label class="t-caption c-2" style="display:inline-flex;align-items:center;gap:6px">${tr('Von selbst nachsehen')}
          <select class="takt" data-takt="${esc(l.id)}" aria-label="${tr('Wie oft Lumen von selbst nachsieht')}">${takt}</select>${uhrzeit}</label>
        <button class="btn btn-pill" style="height:28px;font-size:.75rem" data-metaneu="${esc(l.id)}">${tr('Metadaten neu laden')}</button>
      </div>
    </div>
    <div style="display:flex;gap:8px;flex:none;padding-top:2px">${knoepfe}</div>
  </div>`;
}

function bibListe(libs) {
  if (!libs.length) return `<p class="hint" style="padding:18px 16px">${tr('Noch keine Bibliothek.')}</p>`;
  return libs.map(l => `<div class="orow" data-lib="${esc(l.id)}"
    style="height:auto;padding:14px 16px;display:block">${bibZeile(l)}</div>`).join('');
}

/* Der Ordnerwähler. Den Pfad blind eintippen ging oft daneben — ein
   Tippfehler sah hinterher aus wie eine leere Bibliothek. Hier wird
   angeklickt, was der Server wirklich sieht. */
function bibWaehler() {
  const b = S.browse;
  if (!b) return '';
  const zeilen = (b.ordner || []).map(o => `
    <button type="button" class="orow orow-link" style="width:100%;text-align:left;height:auto;padding:11px 16px" data-into="${esc(o.pfad)}"
      aria-label="${esc(o.name)}${o.freigabe ? ', ' + tr('für Lumen freigegeben') : ''}${o.gesperrt ? ', ' + tr('kein Zugriff') : o.filme ? ', ' + tr('{n} Videos', { n: o.filme >= 200 ? '200+' : o.filme }) : ''}">
      <span style="color:var(--label-2);display:flex;flex:none">
        ${svg('<path d="M3 7.5A1.5 1.5 0 0 1 4.5 6h4L10 8h9.5A1.5 1.5 0 0 1 21 9.5v8A1.5 1.5 0 0 1 19.5 19h-15A1.5 1.5 0 0 1 3 17.5z"/>', 17)}</span>
      <span style="flex:1;min-width:0">
        <span class="t-body clamp-1" style="display:block">${esc(o.name)}</span>
        ${o.freigabe ? `<span class="t-caption c-2">${tr('für Lumen freigegeben')}</span>` : ''}</span>
      ${o.gesperrt
        ? `<span class="t-caption" style="flex:none;color:#ffb340">${tr('kein Zugriff')}</span>`
        : o.filme
        ? `<span class="badge" style="flex:none">${tr('{n} Videos', { n: o.filme >= 200 ? '200+' : o.filme })}</span>`
        : `<span class="t-caption c-3" style="flex:none">${tr('keine Videos')}</span>`}
      <span style="color:var(--label-3);display:flex;flex:none">${svg(I.chev, 15)}</span>
    </button>`).join('');

  return `<div class="group group--inset" style="padding:0;margin-top:4px" role="group" aria-label="${tr('Ordner wählen')}">
    <div class="orow" style="height:auto;padding:12px 16px;gap:10px;background:var(--fill)">
      <span class="${b.pfad ? 'mono ' : ''}t-caption c-2 clamp-1" style="flex:1;min-width:0" id="waehlerpfad" tabindex="-1">
        ${esc(b.pfad || tr('Wo möchtest du anfangen?'))}</span>
      ${b.pfad ? `<button type="button" class="btn btn-pill" style="flex:none;height:30px;font-size:.8125rem"
        data-into="${esc(b.oben || '')}">${tr('Höher')}</button>` : ''}
      ${b.pfad ? `<button type="button" class="btn btn-pill btn-pill--fill" style="flex:none;height:30px;font-size:.8125rem"
        data-take="${esc(b.pfad)}">${tr('Diesen nehmen')}</button>` : ''}
      <button type="button" class="btn btn-pill" style="flex:none;height:30px;font-size:.8125rem" data-browseoff>${tr('Schliessen')}</button>
    </div>
    ${zeilen || `<p class="hint" style="padding:14px 16px">${tr('Hier liegen keine Unterordner.')}</p>`}
    ${!b.pfad && b.imContainer ? `<p class="t-caption c-2" style="padding:10px 16px 12px;margin:0">
      ${tr('Lumen läuft in einem Container und sieht vom NAS nur die Ordner, die ihm freigegeben sind — in der docker-compose.yml unter „volumes“, etwa')} <span class="mono">/volume1/video:/volume1/video:ro</span>${tr('. Fehlt einer, dort eintragen und den Container neu starten.')}</p>` : ''}
    ${b.hier ? `<p class="t-caption c-2" style="padding:10px 16px 12px;margin:0">
      ${b.hier >= 200 ? tr('Hier und in den Unterordnern liegen über 200 Videodateien.') : tr('Hier und in den Unterordnern liegen {n} Videodateien.', { n: b.hier })}</p>` : ''}
  </div>`;
}

function viewAdminLibraries() {
  const libs = S.data?.libraries || [];

  return shell(`<div class="wrap wrap--flat wrap--verwaltung">
    ${ohead(tr('Bibliotheken'), tr('Wo deine Filme und Serien liegen'))}
    <section style="margin-top:28px"><div class="group group--inset" id="biblist">
      ${bibListe(libs)}</div></section>
    ${libs.length ? `<div style="display:flex;gap:10px;margin-top:12px;flex-wrap:wrap">
      <button class="btn btn-pill btn-pill--fill" data-metaneu="">${tr('Alle Metadaten neu laden')}</button>
      <span class="t-caption c-2" style="align-self:center">${tr('Holt Beschreibungen, Besetzung und Bilder für alle Titel noch einmal. Von Hand zugewiesene bleiben.')}</span></div>` : ''}
    ${voll() ? `<section style="margin-top:26px"><div class="section-h">${tr('Neue Bibliothek')}</div>
      <form id="libform" class="group group--inset" style="padding:16px;display:grid;grid-template-columns:minmax(0,1fr);gap:14px">
        <div class="field"><label for="lname">${tr('Name')}</label>
          <input id="lname" name="name" placeholder="${tr('Filme')}" required></div>
        <div class="field"><label for="lkind">${tr('Art')}</label>
          <select id="lkind" name="kind">
            <option value="movie">${tr('Filme')}</option><option value="series">${tr('Serien')}</option></select>
          <span class="hint">${tr('Bei Filmen und Serien nur eine Angabe darüber, was hier liegt. Folgen erkennt Lumen am Namen (S01E02) oder am Staffelordner darüber — auch wenn hier „Filme“ steht.')}</span></div>
        <div class="field"><label for="lpath">${tr('Ordner auf dem Server')}</label>
          <div style="display:flex;gap:8px">
            <input id="lpath" name="path" class="mono" style="flex:1;min-width:0"
              placeholder="${tr('/volume1/video/Filme')}" required>
            <button type="button" class="btn btn-pill" style="flex:none" data-browse>${tr('Durchsuchen')}</button>
          </div>
          <span class="hint">${tr('Der Pfad, unter dem der Server die Dateien sieht — bei Docker der Pfad im Container. Unterordner werden mitgelesen.')}</span></div>
        <div id="bibwaehler">${bibWaehler()}</div>
        <button class="btn btn-primary" style="height:44px">${tr('Anlegen und prüfen')}</button>
      </form></section>` : ''}
    <div style="height:40px"></div></div>`);
}

/* Zeichnet Liste und Ordnerwähler neu — aber nicht das Formular. Wer
   gerade einen Namen eingetippt hat, soll ihn nicht verlieren, nur weil
   er daneben einen Ordner aufklappt. */
// Ordnerwähler schliessen; der Fokus geht zurück auf „Durchsuchen".
function waehlerZu() {
  if (!S.browse) return false;
  S.browse = null;
  bibMalen();
  app.querySelector('[data-browse]')?.focus();
  return true;
}

function bibMalen() {
  const liste = document.getElementById('biblist');
  if (liste) liste.innerHTML = bibListe(S.data?.libraries || []);
  const w = document.getElementById('bibwaehler');
  if (w) w.innerHTML = bibWaehler();
}

/* Schreibt nur die Zeilen fort, solange etwas läuft. Kein render(): das
   Formular darunter soll stehen bleiben, auch halb ausgefüllt. */
let bibTakt = null;
// Fehlversuche in Folge: der Takt wird dann langsamer (bis 30 s), statt
// einen kranken Server alle 1,2 s zu fragen.
let bibFehler = 0;
function bibTaktAn() {
  clearTimeout(bibTakt);
  if (S.view !== 'adminLibraries') return;
  if (!(S.data?.libraries || []).some(l => l.stand?.laeuft || l.metadaten?.laeuft || l.wartet)) return;
  bibTakt = setTimeout(async () => {
    if (S.view !== 'adminLibraries') return;
    let libs;
    try { libs = await api.adminLibraries(); bibFehler = 0; }
    catch (err) {
      // Abgelaufen: sitzungAbgelaufen führt zur Anmeldung, hier nichts mehr.
      if (err?.status === 401) return;
      bibFehler++;
      return bibTaktAn();
    }
    S.data = { ...(S.data || {}), libraries: libs };
    const liste = document.getElementById('biblist');
    if (!liste) return;
    // Kommt eine Bibliothek dazu oder fällt eine weg, muss die Liste neu
    // gebaut werden; sonst genügt es, jede Zeile für sich zu füllen.
    if (liste.querySelectorAll('[data-lib]').length !== libs.length) {
      liste.innerHTML = bibListe(libs);
    } else {
      for (const l of libs) {
        const z = liste.querySelector(`[data-lib="${CSS.escape(l.id)}"]`);
        if (z) z.innerHTML = bibZeile(l);
      }
    }
    bibTaktAn();
  }, Math.min(30000, 1200 * 2 ** bibFehler));
}

/* Die Wünsche des Haushalts, für den Besitzer.
 *
 * Es gibt sie auch in der gewöhnlichen Wunschliste — dort stehen sie
 * aber zwischen den eigenen, und beim Besitzer ist das gerade die
 * Verwechslung: was will ich selbst, was wollen die anderen von mir.
 * Hier stehen sie nach Zustand sortiert und sonst nichts.
 *
 * Was hier NICHT steht: wann jemand etwas geschaut hat. Ein Wunsch ist
 * etwas, das jemand von Hand äussert; alles andere bleibt bei ihm.
 */
function viewAdminWishes() {
  const alle = S.data?.wishes || [];
  const COL = { open: ['rgba(120,120,128,.24)', 'var(--label-2)'], working: ['rgba(10,132,255,.16)', '#4aa3ff'],
    done: ['rgba(48,209,88,.16)', '#30d158'], declined: ['rgba(255,69,58,.14)', '#ff6961'] };
  const NAME = { open: tr('offen'), working: tr('in Arbeit'), done: tr('erfüllt'), declined: tr('abgelehnt') };

  const knopf = (id, zu, text, rot) =>
    `<button class="btn btn-pill${rot ? '' : ' btn-pill--fill'}" style="height:30px;font-size:.8125rem;flex:none${
      rot ? ';background:rgba(255,69,58,.14);color:#ff6961' : ''}"
      data-wishstate="${esc(id)}:${zu}">${text}</button>`;

  const zeile = w => `
    <div class="orow wunsch-zeile" style="height:auto;padding:12px 16px;gap:12px;flex-wrap:wrap">
      ${wunschBild(w)}
      <div style="flex:1;min-width:140px">
        <div class="t-body wunsch-titel">${esc(w.title)}</div>
        ${wunschZusatz(w) ? `<div class="t-caption c-2">${wunschZusatz(w)}</div>` : ''}
        <div class="t-caption c-2" style="display:flex;align-items:center;gap:6px;flex-wrap:wrap">
          ${avatar({ avatar: w.avatar, name: w.by }, 18, 'flex:none')}
          ${tr('von {name}', { name: esc(w.by) })}${w.mine ? ' ' + tr('(du)') : ''}
          ${w.auch?.length ? ' · ' + tr('auch: {namen}', { namen: esc(w.auch.join(', ')) }) : ''}
          · ${datum(w.createdAt)}</div>
        ${wunschVerweise(w)}
        ${w.note ? `<div class="t-caption c-2" style="margin-top:3px">${tr('Notiz: {note}', { note: esc(w.note) })}</div>` : ''}
      </div>
      <span class="badge" style="border:0;flex:none;background:${COL[w.state][0]};color:${COL[w.state][1]}">
        ${NAME[w.state]}</span>
      <div style="display:flex;gap:8px;flex:none">
        ${w.state === 'open' ? knopf(w.id, 'working', tr('Annehmen')) : ''}
        ${w.state !== 'done' ? knopf(w.id, 'done', tr('Erfüllt')) : ''}
        ${w.state !== 'declined' && w.state !== 'done' ? knopf(w.id, 'declined', tr('Ablehnen'), true) : ''}
        ${w.state === 'done' || w.state === 'declined' ? knopf(w.id, 'open', tr('Wieder offen')) : ''}
      </div>
    </div>`;

  const gruppe = (titel, zustaende) => {
    const teil = alle.filter(w => zustaende.includes(w.state || 'open'));
    if (!teil.length) return '';
    return `<section style="margin-top:24px">
      <div class="section-h">${titel} · ${teil.length}</div>
      <div class="group group--inset">${teil.map(zeile).join('')}</div></section>`;
  };

  return shell(`<div class="wrap wrap--flat wrap--verwaltung">
    ${ohead(tr('Wünsche'), tr('Was sich der Haushalt wünscht'))}
    ${alle.length ? '' : `<section style="margin-top:28px"><div class="group group--inset">
      <p class="hint" style="padding:18px 16px;margin:0">${tr('Noch hat sich niemand etwas gewünscht.')}</p></div></section>`}
    ${gruppe(tr('Offen'), ['open'])}
    ${gruppe(tr('In Arbeit'), ['working'])}
    ${gruppe(tr('Erledigt'), ['done', 'declined'])}
    <p class="t-sub c-2" style="margin:22px 2px 0;display:flex;align-items:flex-start;gap:8px;max-width:760px">
      ${svg(I.lock, 14, 2.2)}<span>${tr('Ein Wunsch ist das, was jemand dir ausdrücklich sagt. Was er sonst schaut, steht hier nicht — und nirgends sonst.')}</span></p>
    <div style="height:40px"></div></div>`);
}

/* ---------------------------------------------------------------- Updates
   Lumen sagt, ob es eine neuere Fassung gibt, und setzt sie auf
   Wunsch selbst ein — jetzt oder nachts, aber nie, während jemand schaut.
   Dafür zählt nur die Zahl der Wiedergaben, keine Namen. */
function updateKurz(u, version) {
  if (!u) return version || '';
  if (u.neuer) return tr('{version} verfügbar', { version: u.neueste.version });
  if (u.fehler) return tr('Prüfen klappte nicht');
  return `${u.installiert} · ${tr('aktuell')}`;
}

const zeit = t => t ? datumZeit(t, { dateStyle: 'medium', timeStyle: 'short' }) : '–';

function viewAdminUpdate() {
  const u = S.data || {};
  const e = u.einstellung || {};
  const warn = svg('<path d="M12 4 2.8 19.5h18.4z"/><path d="M12 10v4.5M12 17v.01"/>', 20);
  const haken = svg('<path d="M4 12.5 9.5 18 20 6.5"/>', 20);
  const uhr = (name, wert) => `<input type="time" name="${name}" value="${esc(wert || '')}" style="width:auto">`;

  let stand;
  if (u.neuer) {
    stand = `<div class="row" style="min-height:52px;align-items:flex-start;padding-top:12px;padding-bottom:12px">
      <span style="color:#ff9f0a;display:flex;flex:none">${warn}</span>
      <div style="flex:1;min-width:0">
        <div class="t-body">${tr('Lumen {version} ist da — du hast {installiert}', { version: esc(u.neueste.version), installiert: esc(u.installiert) })}</div>
        <div class="t-sub c-2">${tr('veröffentlicht {wann}', { wann: zeit(u.neueste.published) })} ·
          <a href="${esc(u.neueste.url)}" target="_blank" rel="noopener" class="link">${tr('Was sich geändert hat')}</a></div>
        ${u.container ? `<div class="t-caption c-2" style="margin-top:8px">${esc(u.anleitung)}</div>` : ''}
      </div>
      ${u.container ? '' : `<button class="btn btn-pill btn-pill--fill" data-updjetzt style="flex:none"${u.laeuft ? ' disabled' : ''}>
        ${u.laeuft ? tr('Läuft …') : tr('Jetzt aktualisieren')}</button>`}
    </div>`;
  } else {
    stand = `<div class="row" style="min-height:52px">
      <span style="color:${u.fehler ? '#ff6961' : '#30d158'};display:flex;flex:none">${u.fehler ? warn : haken}</span>
      <div style="flex:1;min-width:0">
        <div class="t-body">Lumen ${esc(u.installiert)}${u.fehler || !u.neueste ? ''
          : u.installiert === 'dev' ? ` ${tr('— aus dem Quelltext gebaut, neueste Veröffentlichung ist {version}', { version: esc(u.neueste.version) })}` : ` ${tr('— auf dem neuesten Stand')}`}</div>
        <div class="t-sub c-2">${u.fehler ? esc(u.fehler) : tr('zuletzt geprüft {wann}', { wann: zeit(u.geprueft) })}</div>
      </div></div>`;
  }

  const protokoll = (u.protokoll || []).length ? (u.protokoll || []).map(p => `
    <div class="row" style="min-height:44px">
      <span class="t-caption c-2 tnum" style="flex:none;width:130px">${zeit(p.at)}</span>
      <span class="t-body" style="flex:1">${esc(p.von)} → ${esc(p.nach)}${p.auto ? ` <span class="t-caption c-2">${tr('von selbst')}</span>` : ''}</span>
      <span class="badge" style="border:0;flex:none;background:${p.ergebnis === 'ok' ? 'rgba(48,209,88,.16)' : p.ergebnis === 'fehler' ? 'rgba(255,69,58,.14)' : 'rgba(120,120,128,.24)'};
        color:${p.ergebnis === 'ok' ? '#30d158' : p.ergebnis === 'fehler' ? '#ff6961' : 'var(--label-2)'}" title="${esc(p.notiz || '')}">
        ${p.ergebnis === 'ok' ? tr('eingesetzt') : p.ergebnis === 'fehler' ? tr('fehlgeschlagen') : tr('verschoben')}</span>
    </div>${p.notiz ? `<div class="t-caption c-2" style="padding:0 16px 10px 146px">${esc(p.notiz)}</div>` : ''}`).join('')
    : `<p class="hint" style="padding:16px;margin:0">${tr('Noch keine Aktualisierung.')}</p>`;

  return shell(`<div class="wrap wrap--flat wrap--verwaltung">
    ${ohead(tr('Updates'), tr('Neue Fassungen von Lumen — nachsehen, einsetzen, planen'))}
    <section style="margin-top:28px"><div class="section-h">${tr('Stand')}</div>
      <div class="group group--inset">${stand}
        <div class="row" style="min-height:48px;gap:10px;flex-wrap:wrap">
          <button class="btn btn-pill" style="height:30px;font-size:.8125rem" data-updpruefen>${tr('Jetzt prüfen')}</button>
          <a class="btn btn-pill" style="height:30px;font-size:.8125rem;text-decoration:none" href="${esc(u.releaseSeite)}" target="_blank" rel="noopener">${tr('Alle Veröffentlichungen')}</a>
          <span class="t-caption c-2" style="margin-left:auto">${u.wiedergaben ? tr('{n} Wiedergaben laufen', { n: u.wiedergaben }) : tr('gerade schaut niemand')}</span>
        </div></div></section>

    <form id="updform" style="margin-top:24px">
    <section><div class="section-h">${tr('Von selbst')}</div>
      <div class="group group--inset">
        <label class="row" style="min-height:52px">
          <span class="t-body" style="flex:1">${tr('Täglich nachsehen')}</span>
          <select name="pruefen" class="takt"><option value="aus"${e.pruefen === 'aus' ? ' selected' : ''}>${tr('aus')}</option>
            <option value="taeglich"${e.pruefen !== 'aus' ? ' selected' : ''}>${tr('täglich um')}</option></select>
          ${uhr('pruefZeit', e.pruefZeit)}</label>
        <label class="row" style="min-height:52px;flex-wrap:wrap">
          <span class="t-body" style="flex:1">${tr('Von selbst aktualisieren')}</span>
          <select name="auto" class="takt"${u.container ? ' disabled' : ''}><option value="aus"${e.auto !== 'fenster' ? ' selected' : ''}>${tr('aus')}</option>
            <option value="fenster"${e.auto === 'fenster' ? ' selected' : ''}>${tr('nur im Zeitfenster')}</option></select>
          <span class="t-sub c-2">${tr('von')}</span>${uhr('fensterVon', e.fensterVon)}<span class="t-sub c-2">${tr('bis')}</span>${uhr('fensterBis', e.fensterBis)}</label>
        <div class="row" style="min-height:40px"><span class="t-caption c-2">${tr('Läuft im Fenster eine Wiedergabe, wartet Lumen und versucht es später nochmal.')}
          ${u.container ? ` ${tr('Im Container aktualisiert man das Abbild von aussen — darum ist das hier aus.')}` : ''}</span></div>
      </div></section>
    <section style="margin-top:24px"><div class="section-h">${tr('Zugang')}</div>
      ${u.tokenUngueltig ? `<p class="t-body" style="color:#ff6961;margin:0 2px 10px">${tr('GitHub nimmt das Token nicht an — meist ist es abgelaufen. Trag hier ein neues ein, dann kommen die Updates wieder durch. Bist du nicht Besitzer des Repositorys, nimm ein {klassisches} Token mit dem Bereich „repo“ — feingranulare gelten nur für eigene Repositories.', { klassisches: `<b>${tr('klassisches')}</b>` })}</p>` : ''}
      <div class="group group--inset"><div class="row" style="min-height:52px;flex-wrap:wrap;gap:10px">
        <div style="flex:1;min-width:200px"><div class="t-body">${tr('GitHub-Token')}</div>
          <div class="t-caption c-2">${u.tokenQuelle === 'umgebung'
            ? tr('Kommt aus LUMEN_GITHUB_TOKEN (docker-compose.yml). Ein Token hier hat Vorrang — so lässt sich ein abgelaufenes ersetzen, ohne den Container anzufassen.')
            : u.tokenGesetzt
              ? tr('Ist eingetragen. Ein neues ersetzt das alte; es wird gespeichert, aber nie wieder angezeigt.')
              : tr('Optional — nur nötig, wenn die Updates aus einem privaten Repository kommen. Wird gespeichert, aber nie wieder angezeigt.')}</div></div>
        <span class="field" style="width:min(320px,100%);margin:0"><input type="password" name="token" class="mono" aria-label="${tr('GitHub-Token')}" placeholder="${u.tokenGesetzt ? tr('neues Token, ghp_… oder github_pat_…') : tr('ghp_… oder github_pat_…')}" autocomplete="off" style="width:100%;box-sizing:border-box;height:40px"></span></div>
        ${u.tokenQuelle === 'verwaltung' ? `<button type="button" class="btn row" style="width:100%;justify-content:flex-start;min-height:46px;color:#ff6961" data-tokenweg>
          <span class="t-body">${tr('Token entfernen')}</span></button>` : ''}</div></section>
    <div style="display:flex;justify-content:flex-end;margin-top:14px">
      <button class="btn btn-pill btn-pill--fill" type="submit">${tr('Speichern')}</button></div>
    </form>

    <section style="margin-top:24px"><div class="section-h">${tr('Protokoll')}</div>
      <div class="group group--inset">${protokoll}</div></section>
    <div id="updfrage"></div>
    <div style="height:40px"></div></div>`);
}

function updateFrage(n) {
  const b = document.getElementById('updfrage');
  if (!b) return;
  b.innerHTML = `<div class="group group--inset" style="margin-top:16px;border:1px solid rgba(255,159,10,.5)">
    <div class="row" style="min-height:56px;flex-wrap:wrap;gap:10px">
      <div style="flex:1;min-width:200px"><div class="t-body">${tr('{n} Wiedergaben laufen gerade', { n })}</div>
        <div class="t-caption c-2">${tr('Beim Neustart bricht sie ab. Trotzdem jetzt aktualisieren?')}</div></div>
      <button class="btn btn-pill" data-updnein>${tr('Lieber später')}</button>
      <button class="btn btn-pill btn-pill--fill" data-updtrotzdem>${tr('Trotzdem')}</button></div></div>`;
  b.scrollIntoView({ block: 'nearest' });
}

/* ------------------------------------------------- Verwaltungssperre
   Wer in die Verwaltung darf, schützt sie mit eigenem PIN,
   Passwort oder einem Code aus der Authenticator-App. Kein E-Mail, kein
   SMS. Nach dem Entsperren gilt ein Token eine Viertelstunde. */
const sperreName = k => ({ pin: 'PIN', password: tr('Passwort'), totp: tr('Authenticator-App') })[k] || tr('keine');

function viewAdminUnlock() {
  const art = S.sperre?.art || S.data?.sperre || 'pin';
  const pin = art === 'pin', app = art === 'totp';
  return shell(`<div class="wrap wrap--flat" style="max-width:520px">
    ${ohead(tr('Verwaltung gesperrt'), app ? tr('Den Code aus deiner Authenticator-App eingeben')
      : pin ? tr('Deine PIN für die Verwaltung eingeben') : tr('Dein Passwort für die Verwaltung eingeben'), 'home')}
    <form id="adminunlock" class="panel" style="margin-top:28px">
      <div class="field">
        <input id="asecret" name="code" type="${app ? 'text' : 'password'}" style="text-align:center;letter-spacing:.2em"
          inputmode="${pin || app ? 'numeric' : 'text'}" autocomplete="${app ? 'one-time-code' : pin ? 'off' : 'current-password'}"
          placeholder="${app ? '123 456' : pin ? '••••' : tr('Passwort')}" required autofocus></div>
      <div class="err" style="text-align:center">${esc(S.sperreFehler || '')}</div>
      <button class="btn btn-primary" style="height:46px">${tr('Entsperren')}</button>
    </form>
    <p class="t-caption c-2" style="margin-top:14px;text-align:center">${tr('Gilt eine Viertelstunde, dann fragt Lumen wieder.')}</p>
    <div style="height:40px"></div></div>`);
}

function viewAdminLock() {
  const d = S.data || {};
  const art = d.sperre || 'none';
  const wahl = [['none', tr('Keine'), tr('Die Verwaltung geht direkt auf')], ['pin', 'PIN', tr('Mindestens vier Ziffern')],
    ['password', tr('Passwort'), tr('Mindestens acht Zeichen')], ['totp', tr('Authenticator-App'), tr('Sechs Ziffern, alle 30 Sekunden neu')]];
  return shell(`<div class="wrap wrap--flat wrap--verwaltung">
    ${ohead(tr('Verwaltungssperre'), tr('Ein zweiter Riegel vor der Verwaltung — nur für dich'))}
    <p class="t-sub c-2" style="margin:20px 0 0">${tr('Jede Person mit Zugang zur Verwaltung setzt sich ihre eigene Sperre. Deine ist zurzeit:')} <b>${sperreName(art)}</b>.</p>
    <form id="lockform" style="margin-top:24px">
      ${grp(tr('Womit'), wahl.map(([k, n, h]) => `
        <label class="row" style="min-height:52px;cursor:pointer">
          <input type="radio" name="kind" value="${k}"${k === art ? ' checked' : ''} style="margin-right:12px">
          <div style="flex:1"><div class="t-body">${n}</div><div class="t-caption c-2">${h}</div></div></label>`).join(''))}
      <div id="lockdetail">${lockDetail(art)}</div>
      <div style="display:flex;justify-content:flex-end;gap:10px;margin-top:14px">
        ${art !== 'none' ? `<button class="btn btn-pill" type="button" data-relock>${tr('Jetzt sperren')}</button>` : ''}
        <button class="btn btn-pill btn-pill--fill" type="submit">${tr('Speichern')}</button></div>
    </form>
    <div style="height:40px"></div></div>`);
}

function lockDetail(k) {
  if (k === 'pin' || k === 'password') return `<div class="group group--inset" style="margin-top:-10px"><div style="padding:16px" class="field">
    <label for="lsecret">${k === 'pin' ? tr('Neue PIN') : tr('Neues Passwort')}</label>
    <input id="lsecret" name="secret" type="password" inputmode="${k === 'pin' ? 'numeric' : 'text'}" autocomplete="new-password"
      placeholder="${k === 'pin' ? tr('mindestens vier Ziffern') : tr('mindestens acht Zeichen')}"></div></div>`;
  if (k === 'totp') return `<div class="group group--inset" style="margin-top:-10px"><div style="padding:16px" id="totpbox">
    <button class="btn btn-pill btn-pill--fill" type="button" data-totpqr>${tr('QR-Code anzeigen')}</button>
    <p class="t-caption c-2" style="margin:10px 0 0">${tr('Mit Google Authenticator, Microsoft Authenticator, Aegis oder 1Password scannen, dann den Code aus der App unten eintragen. Lumen speichert die Sperre erst, wenn ein Code stimmt.')}</p></div></div>`;
  return '';
}

// Die festen Gruppen der Rollen (store.GruppeMitverwalter/-Vollzugriff).
const ROLLEN_GRUPPEN = { grp_mitverwalter: 'verwalten', grp_vollzugriff: 'voll' };
const rollenName = id => (ROLLEN_GRUPPEN[id] === 'voll' ? tr('Vollzugriff') : tr('Verwaltet mit'));

function viewAdminGroups() {
  const d = S.data || {};
  const libs = d.libraries || [], groups = d.groups || [], accounts = (d.accounts || []).filter(a => !a.isOwner);
  const g = S.gruppe || null; // die, die gerade bearbeitet wird
  const lim = g?.limits || { maxStreams: 2, maxAge: 18, allowUnrated: true, allowTranscode: true, allowDownload: true, maxDownloads: 20, remoteQuality: '1080p' };
  // Die festen Gruppen hinter „Verwaltet mit" und „Vollzugriff" sind
  // Rollen, keine Gruppen: Sie stehen hier nicht zur Wahl und lassen sich
  // nicht auflösen – Rechte vergibt man unter „Konten und Rechte".
  const eigene = groups.filter(x => !x.rolle);
  const rows = eigene.map(x => `
    <div class="orow" style="height:auto;padding:14px 16px;gap:12px;flex-wrap:wrap">
      <div style="flex:1;min-width:160px">
        <div class="t-body">${esc(x.name)}${x.vollzugriff ? ` <span class="badge" style="border:0;background:rgba(255,159,10,.18);color:#ff9f0a">${tr('Vollzugriff')}</span>`
          : x.verwaltung ? ` <span class="badge" style="border:0;background:rgba(10,132,255,.16);color:#4aa3ff">${tr('Verwaltung')}</span>` : ''}</div>
        <div class="t-caption c-2">${tr('{n} Personen', { n: x.mitglieder })} · ${tr('{n} Bibliotheken', { n: x.libraries.length })}
          · ${x.verwaltung || x.vollzugriff ? tr('ohne Grenzen') : tr('höchstens {maxStreams} gleichzeitig', { maxStreams: x.limits.maxStreams })}</div></div>
      <button class="btn btn-pill" style="height:30px;font-size:.8125rem" data-grpedit="${esc(x.id)}">${tr('Bearbeiten')}</button>
      <button class="btn btn-pill" style="height:30px;font-size:.8125rem;background:rgba(255,69,58,.14);color:#ff6961" data-grpdel="${esc(x.id)}">${tr('Auflösen')}</button>
    </div>`).join('');
  const zuweisung = accounts.map(a => `
    <label class="row" style="min-height:48px">
      ${avatar(a, 30, 'flex:none;margin-right:10px')}
      <span class="t-body" style="flex:1">${esc(a.name)}</span>
      ${ROLLEN_GRUPPEN[a.groupId] ? `<span class="t-caption c-2">${tr('Rolle „{rolle}“ – unter Konten und Rechte', { rolle: rollenName(a.groupId) })}</span>`
        : `<select class="takt" data-grpacc="${esc(a.id)}" aria-label="${tr('Gruppe von {name}', { name: esc(a.name) })}">
        <option value="">${tr('keine Gruppe')}</option>
        ${eigene.filter(x => !a.isKid || !x.verwaltung || a.groupId === x.id)
          .map(x => `<option value="${esc(x.id)}"${a.groupId === x.id ? ' selected' : ''}>${esc(x.name)}</option>`).join('')}</select>`}</label>`).join('');
  const form = `
    <form id="grpform" style="margin-top:24px">
      <!-- nicht name="id": das würde form.id überdecken -->
      <input type="hidden" name="gid" value="${esc(g?.id || '')}">
      ${grp(g ? tr('„{name}“ bearbeiten', { name: esc(g.name) }) : tr('Neue Gruppe'), `
        <div style="padding:16px;display:grid;gap:14px">
          <div class="field"><label for="gname">${tr('Name')}</label><input id="gname" name="name" value="${esc(g?.name || '')}" placeholder="${tr('z. B. Familie, Gäste, Mitverwalter')}" required maxlength="60"></div>
          <div class="field"><label for="grecht">${tr('Rechte in der Verwaltung')}</label>
            <select id="grecht" name="recht">
              <option value="schauen"${!g?.verwaltung ? ' selected' : ''}>${tr('Keine — nur schauen')}</option>
              <option value="verwalten"${g?.verwaltung && !g?.vollzugriff ? ' selected' : ''}>${tr('Verwaltet mit — alles ausser Gruppen, Rechten, Updates')}</option>
              <option value="voll"${g?.vollzugriff ? ' selected' : ''}>${tr('Vollzugriff — alles wie der Besitzer')}</option></select>
            <span class="hint">${tr('Wer was schaut, sieht auch hier niemand.')}</span></div>
          <div class="field"><label>${tr('Bibliotheken')}</label>
            ${libs.length ? libs.map(l => `<label class="row" style="min-height:36px;padding:0;cursor:pointer"><input type="checkbox" name="lib" value="${esc(l.id)}"${(g ? (g.libraries || []).includes(l.id) : true) ? ' checked' : ''} style="margin-right:10px"><span class="t-body">${esc(l.name)}</span></label>`).join('')
              : `<span class="hint">${tr('Noch keine Bibliotheken.')}</span>`}</div>
          <div style="display:grid;grid-template-columns:repeat(auto-fit,minmax(150px,1fr));gap:12px">
            <div class="field"><label for="gstreams">${tr('Gleichzeitig')}</label><input id="gstreams" name="maxStreams" type="number" min="1" max="20" value="${lim.maxStreams}"></div>
            <div class="field"><label for="gage">${tr('Bis Altersfreigabe')}</label><select id="gage" name="maxAge">${altersOptionen(lim.maxAge)}</select></div>
            <div class="field"><label for="gdl">${tr('Downloads')}</label><input id="gdl" name="maxDownloads" type="number" min="0" max="500" value="${lim.maxDownloads}"></div>
          </div>
          <div style="display:flex;gap:18px;flex-wrap:wrap">
            <label class="t-sub" style="display:flex;gap:8px;align-items:center"><input type="checkbox" name="allowUnrated"${lim.allowUnrated ? ' checked' : ''}>${tr('Ungeprüfte Titel')}</label>
            <label class="t-sub" style="display:flex;gap:8px;align-items:center"><input type="checkbox" name="allowTranscode"${lim.allowTranscode ? ' checked' : ''}>${tr('Umwandeln')}</label>
            <label class="t-sub" style="display:flex;gap:8px;align-items:center"><input type="checkbox" name="allowDownload"${lim.allowDownload ? ' checked' : ''}>${tr('Herunterladen')}</label>
          </div>
          <span class="hint">${tr('Ohne „Herunterladen“ gibt es keinen Download-Knopf, und Lumen liefert Filme nur etwa doppelt so schnell aus, wie sie laufen (Kinderprofile mit Bildschirmzeit ungefähr in Echtzeit). Wer technisch versiert ist, kann einen Strom trotzdem mitschneiden.')}</span>
        </div>`)}
      <div style="display:flex;justify-content:flex-end;gap:10px;margin-top:-10px">
        ${g ? `<button class="btn btn-pill" type="button" data-grpneu>${tr('Abbrechen')}</button>` : ''}
        <button class="btn btn-pill btn-pill--fill" type="submit">${g ? tr('Speichern') : tr('Anlegen')}</button></div>
    </form>`;
  return shell(`<div class="wrap wrap--flat wrap--verwaltung">
    ${ohead(tr('Gruppen'), tr('Rechte bündeln und Konten zuordnen'))}
    <p class="t-sub c-2" style="margin:20px 0 0;display:flex;align-items:flex-start;gap:8px">${svg(I.lock, 14, 2.2)}
      <span>${tr('Wer in einer Gruppe ist, bekommt deren Bibliotheken und Grenzen; die eigenen Einträge am Konto ruhen solange. Was jemand schaut, sieht keine Gruppe — auch keine mit Verwaltung.')}</span></p>
    <section style="margin-top:22px"><div class="section-h">${tr('Gruppen')}</div>
      <div class="group group--inset">${rows || `<p class="hint" style="padding:16px;margin:0">${tr('Noch keine Gruppe.')}</p>`}</div></section>
    ${form}
    ${accounts.length ? `<section style="margin-top:24px"><div class="section-h">${tr('Wer gehört wohin')}</div>
      <div class="group group--inset">${zuweisung}</div></section>` : ''}
    <div style="height:40px"></div></div>`);
}

function platzText(mb) {
  return mb >= 1024 ? `${zahl(mb / 1024, { maximumFractionDigits: mb >= 102400 ? 0 : 1 })} GB` : `${zahl(mb)} MB`;
}
// Heute in der Zeitzone des Geräts (toISOString rechnete in UTC — zwischen
// Mitternacht und 1 Uhr stand dann „gestern" da).
const heuteTag = () => { const d = new Date(); return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`; };
function tagText(tag) {
  const heute = heuteTag();
  if (tag === heute) return tr('heute');
  // Mittag statt Mitternacht: so kippt das Datum in keiner Zeitzone.
  return datum(tag + 'T12:00:00', { day: 'numeric', month: 'numeric', year: 'numeric' });
}

/* Wann eine Sicherung entstand: „heute, 22:07" oder „3.11.2026, 04:31".
   Zwei Sicherungen vom selben Abend hiessen vorher beide „heute". */
function sicherungWann(b) {
  if (!b?.zeit) return tagText(b?.tag);
  const uhr = uhrzeitText(b.zeit, { hour: '2-digit', minute: '2-digit' });
  return b.tag === heuteTag() ? tr('heute, {uhr}', { uhr }) : `${tagText(b.tag)}, ${uhr}`;
}
function sicherungGroesse(b) {
  const bytes = b.bytes ?? (b.mb || 0) * 1048576;
  return bytes >= 1048576 ? `${zahl(bytes / 1048576, { maximumFractionDigits: 1 })} MB` : `${zahl(Math.max(1, Math.round(bytes / 1024)))} KB`;
}
/* Nach dem Wiederherstellen: ein Hinweis über der Seite, bis der Server
   wieder antwortet — dann neu laden und sagen, dass es geklappt hat. */
function neustartAbwarten(text, danach) {
  const el = document.createElement('div');
  el.className = 'neustart-hinweis';
  el.setAttribute('role', 'status');
  el.innerHTML = `<div class="neustart-karte"><span class="spin"></span><p class="t-body" style="margin:0">${esc(text)}</p></div>`;
  document.body.appendChild(el);
  try { sessionStorage.setItem('lumen.nachNeustart', danach); } catch {}
  const start = Date.now();
  let warWeg = false;
  const fragen = async () => {
    let da = false;
    try { const r = await fetch('/api/v1/hello', { cache: 'no-store' }); da = r.ok; } catch {}
    if (!da) warWeg = true;
    // Erst wenn er einmal weg war (oder nach 20 s), gilt er als neu gestartet.
    if (da && (warWeg || Date.now() - start > 20000)) return location.reload();
    if (Date.now() - start > 120000) { el.remove(); return toast(tr('Lumen antwortet noch nicht. Lade die Seite gleich selbst neu.')); }
    setTimeout(fragen, 1000);
  };
  setTimeout(fragen, 1500);
}
try {
  const nachNeustart = sessionStorage.getItem('lumen.nachNeustart');
  if (nachNeustart) { sessionStorage.removeItem('lumen.nachNeustart'); setTimeout(() => toast(nachNeustart), 800); }
} catch {}

function viewAdminBackups() {
  const d = S.data || {};
  const rows = (d.sicherungen || []).map(b => `
    <div class="row" style="min-height:56px;gap:12px">
      <span style="flex:1"><span class="t-body">${b.nr === 1 ? tr('Neueste') : tr('Davor')}</span>
        <span class="t-caption c-2" style="display:block">${esc(sicherungWann(b))} · ${esc(sicherungGroesse(b))}</span></span>
      <button class="btn btn-pill btn-pill--fill" style="height:32px;font-size:.8125rem" data-wiederherstellen="${Number(b.nr)}"
        aria-label="${tr('Stand von {wann} wiederherstellen', { wann: esc(sicherungWann(b)) })}">${tr('Wiederherstellen')}</button>
    </div>`).join('');
  return shell(`<div class="wrap wrap--flat wrap--verwaltung">
    ${ohead(tr('Sicherung'), tr('Einmal am Tag, nach 4:30 Uhr'))}
    ${d.fehler ? `<p class="t-body" style="color:var(--text-rot);margin-top:18px">${tr('Die letzte Sicherung ist gescheitert: {fehler}', { fehler: esc(d.fehler) })}</p>` : ''}
    <section style="margin-top:24px"><div class="group group--inset">
      ${rows || `<p class="hint" style="padding:18px 16px">${tr('Noch keine Sicherung.')}</p>`}
      <button class="btn row" style="width:100%;justify-content:flex-start;min-height:48px" data-sichern>
        <span class="t-body" style="color:var(--accent)">${tr('Jetzt sichern')}</span></button>
    </div></section>
    <p class="t-sub c-2" style="margin:16px 2px 0;max-width:760px">${esc(d.hinweis || '')}
      ${tr('Wiederherstellen startet Lumen neu; laufende Wiedergaben brechen dabei ab.')}</p>
    <div style="height:40px"></div></div>`);
}

// Umwandeln: welche Hardware rechnet, und was im Hintergrund vorbereitet
// wird. Jeder Satz steht für sich, damit er sich übersetzen lässt.
const HW_NAMEN = texte({ auto: 'Automatisch', aus: 'Aus (nur Software)', vaapi: 'VAAPI (Intel, AMD)', qsv: 'Intel Quick Sync', nvenc: 'NVIDIA NVENC' });
function viewAdminUmwandlung() {
  const d = S.data || {};
  const hw = d.hardware || {};
  const verfuegbar = hw.verfuegbar || [];
  const optionen = ['auto', 'aus', 'vaapi', 'qsv', 'nvenc'].map(v =>
    `<option value="${v}"${hw.wahl === v ? ' selected' : ''}>${esc(HW_NAMEN[v])}${v !== 'auto' && v !== 'aus' && !verfuegbar.includes(v) ? ` ${tr('– nicht gefunden')}` : ''}</option>`).join('');
  const jetzt = hw.pruefungLaeuft ? tr('Die Hardware wird gerade geprüft.')
    : hw.aktiv ? `${tr('Umgewandelt wird mit')} ${HW_NAMEN[hw.aktiv] || hw.aktiv}.`
    : tr('Umgewandelt wird in Software.');
  const gefunden = verfuegbar.length
    ? `${tr('Gefunden:')} ${verfuegbar.map(v => HW_NAMEN[v] || v).join(', ')}.`
    : tr('Keine Hardware-Beschleunigung gefunden.');
  const tonemap = { zscale: tr('HDR wird sauber auf SDR umgerechnet (zscale).'),
    colorspace: tr('HDR wird vereinfacht umgerechnet (colorspace) – zscale fehlt in diesem FFmpeg.'),
    keins: tr('Dieses FFmpeg kann HDR nicht umrechnen.') }[d.tonemapping] || '';
  const vorrat = d.vorrat || {};
  const erkennung = d.vorspannErkennung || {};
  return shell(`<div class="wrap wrap--flat wrap--verwaltung">
    ${ohead(tr('Umwandeln'), tr('Hardware-Beschleunigung und Vorbereitung'))}
    <form id="umwandlungform">
      <section style="margin-top:24px"><div class="group group--inset">
        <div class="row" style="min-height:56px;gap:12px">
          <label for="hwwahl" class="t-body" style="flex:1;min-width:0">${tr('Hardware-Beschleunigung')}</label>
          <select id="hwwahl" name="hardware"${d.hardwareAusUmgebung ? ' disabled' : ''}
            style="height:34px;border:0;border-radius:10px;background:var(--fill);color:var(--label);font:inherit;padding:0 10px;max-width:58%;min-width:0">${optionen}</select>
        </div>
        <div class="row" style="min-height:52px;gap:12px">
          <span class="t-sub c-2" style="flex:1">${esc(jetzt)} ${esc(gefunden)}${hw.ruht?.length ? ' ' + esc(`${tr('Ruht nach Fehlern:')} ` + hw.ruht.map(v => HW_NAMEN[v] || v).join(', ') + '.') : ''}</span>
          <button class="btn btn-pill" type="button" style="height:32px;font-size:.8125rem" data-hwpruefen>${tr('Neu prüfen')}</button>
        </div>
        ${d.hardwareAusUmgebung ? `<p class="hint" style="padding:12px 16px">${esc(tr('Das Verfahren ist in der Umgebung (LUMEN_HARDWARE) festgelegt und lässt sich nur dort ändern.'))}</p>` : ''}
      </div></section>
      <section style="margin-top:20px"><div class="group group--inset">
        <div class="row" style="min-height:52px;gap:12px">
          <span style="flex:1"><span class="t-body" id="uw-trickplay">${tr('Vorschaubilder beim Spulen')}</span>
            <span class="t-caption c-2" style="display:block">${esc(tr('Für {n} Dateien vorbereitet.', { n: vorrat.trickplay || 0 }))}</span></span>
          <button type="button" class="switch" role="switch" aria-checked="${!!d.trickplay}" data-sw="1" data-feld="trickplay" aria-labelledby="uw-trickplay"><i></i></button></div>
        <div class="row" style="min-height:52px;gap:12px">
          <span style="flex:1"><span class="t-body" id="uw-untertitel">${tr('Untertitel vorbereiten')}</span>
            <span class="t-caption c-2" style="display:block">${esc(tr('{n} Untertitelspuren liegen bereit.', { n: vorrat.untertitel || 0 }))}</span></span>
          <button type="button" class="switch" role="switch" aria-checked="${!!d.untertitelVorbereiten}" data-sw="1" data-feld="untertitel" aria-labelledby="uw-untertitel"><i></i></button></div>
        <div class="row" style="min-height:52px;gap:12px">
          <span style="flex:1"><span class="t-body" id="uw-vorspann">${tr('Vorspann und Abspann erkennen')}</span>
            <span class="t-caption c-2" style="display:block">${esc(tr('{n} Folgen untersucht.', { n: erkennung.untersucht || 0 }))} ${esc(tr('Vorspann in {n} erkannt.', { n: erkennung.mitVorspann || 0 }))}</span></span>
          <button type="button" class="switch" role="switch" aria-checked="${!!erkennung.an}" data-sw="1" data-feld="vorspann" aria-labelledby="uw-vorspann"><i></i></button></div>
        <div class="row" style="min-height:52px;gap:12px">
          <span style="flex:1"><span class="t-body" id="uw-vorspann-nachts">${tr('Nur nachts (1–7 Uhr)')}</span>
            <span class="t-caption c-2" style="display:block">${esc(tr('Die Erkennung liest jede Folge an. Sie wartet ausserdem, solange etwas abgespielt wird.'))}</span></span>
          <button type="button" class="switch" role="switch" aria-checked="${erkennung.nurNachts !== false}" data-sw="1" data-feld="vorspann-nachts" aria-labelledby="uw-vorspann-nachts"><i></i></button></div>
      </div></section>
      <p class="t-sub c-2" style="margin:16px 2px 0;max-width:760px">${esc(tonemap)}
        ${esc(tr('Höchstens {n} Umwandlungen laufen gleichzeitig.', { n: d.maxUmwandlungen || 2 }))}
        ${esc(tr('Vorbereitet wird mit niedrigster Priorität und nur, wenn gerade nichts umgewandelt wird.'))}</p>
      <div style="margin-top:18px"><button class="btn btn-primary" type="submit">${tr('Speichern')}</button></div>
    </form>
    <div style="height:40px"></div></div>`);
}

function viewSessions() {
  const d = S.data || {};
  // Bewusst ohne Person, Gerät, Ort und Startzeit: nur die Last. Ob zwei
  // Zeilen aus demselben Profil kommen, ist nicht zu erkennen.
  const rows = (d.sessions || []).map((s, i) => `
    <div class="orow" style="height:auto;padding:16px;align-items:flex-start;gap:14px">
      <span style="width:38px;height:38px;border-radius:50%;background:var(--fill);display:inline-flex;align-items:center;justify-content:center;flex:none">${svg(I.film, 18, 2)}</span>
      <div style="flex:1;min-width:0">
        <div style="display:flex;align-items:center;gap:10px;flex-wrap:wrap">
          <span class="t-body">${tr('Wiedergabe {nr}', { nr: i + 1 })}</span>
          <span class="badge" style="border:0;background:${s.mode === 'direct' ? 'rgba(48,209,88,.16)' : 'rgba(10,132,255,.16)'};color:${s.mode === 'direct' ? '#30d158' : '#4aa3ff'}">
            ${s.mode === 'direct' ? tr('Direkt') : tr('Wird umgewandelt')}</span>
        </div>
      </div>
      <button class="btn btn-pill" style="height:32px;font-size:.8125rem;background:rgba(255,69,58,.14);color:#ff6961;flex:none"
        data-kill="${esc(s.id)}">${tr('Beenden')}</button>
    </div>`).join('');

  return shell(`<div class="wrap wrap--flat wrap--verwaltung">
    ${ohead(tr('Aktive Wiedergaben'), tr('{n} laufen · Last {last} — beenden, wenn der Server zu viel rechnet', { n: (d.sessions || []).length, last: ({ gering: tr('gering'), mittel: tr('mittel'), hoch: tr('hoch') })[d.load?.last] || tr('gering') }))}
    <section style="margin-top:28px"><div class="group group--inset">
      ${rows || `<p class="hint" style="padding:18px 16px">${tr('Gerade läuft nichts.')}</p>`}</div></section>
    <p class="t-sub c-2" style="margin:16px 2px 0;display:flex;align-items:flex-start;gap:8px;max-width:760px">
      ${svg(I.lock, 14, 2.2)}<span>${esc(d.note || '')}</span></p>
    <div style="height:40px"></div></div>`);
}

function viewLogs() {
  const COL = { info: 'var(--label-2)', warn: '#ffb340', error: '#ff6961' };
  const TAG = { info: 'rgba(120,120,128,.2)', warn: 'rgba(255,159,10,.18)', error: 'rgba(255,69,58,.18)' };
  const lines = (S.data?.lines || []).map(l => `
    <div style="display:flex;gap:12px;align-items:baseline;min-height:28px;padding:3px 0">
      <span class="mono tnum c-3" style="flex:none;width:118px">${l.area === 'stream'
        // Störungen beim Abspielen kommen als Tageszahl, ohne Uhrzeit.
        ? datum(l.at)
        : uhrzeitText(l.at, { hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false })}</span>
      <span class="badge" style="border:0;flex:none;width:56px;justify-content:center;background:${TAG[l.level]};color:${COL[l.level]};text-transform:uppercase;font-size:.625rem">${l.level}</span>
      <span class="mono" style="flex:none;width:86px;color:var(--label-3)">${esc(l.area)}</span>
      <span class="mono" style="color:${l.level === 'info' ? 'var(--label-2)' : COL[l.level]};flex:1">${esc(l.message)}</span></div>`).join('');

  const tabs = [['all', tr('Alles')], ['error', tr('Fehler')], ['warn', tr('Warnung')], ['info', tr('Info')]].map(([v, n]) =>
    `<button class="tab-pill" role="tab" aria-selected="${S.logFilter === v}" data-logf="${v}" style="height:30px;font-size:.8125rem">${n}</button>`).join('');

  return shell(`<div class="wrap wrap--flat">
    ${ohead(tr('Protokoll'), tr('Was der Server tut — und wo es hakt'))}
    <div role="tablist" style="display:inline-flex;padding:3px;border-radius:999px;background:var(--fill);gap:2px;margin:24px 0 14px">${tabs}</div>
    <div class="group group--inset" style="padding:10px 16px">
      ${lines || `<p class="hint" style="padding:10px 0">${tr('Keine Einträge.')}</p>`}</div>
    <p class="t-caption c-2" style="margin:10px 2px 0">
      ${tr('Hier steht, was der Server macht — Geräte, Platten, Umwandlungen. Nie, welcher Titel läuft.')}</p>
    <div style="height:40px"></div></div>`);
}

function viewCleanup() {
  const d = S.data || {};
  const aufgeloest = (d.reihen || []).filter(r => r.hidden);
  // Je Titel: jede Datei mit vollem Pfad, Auflösung, Grösse. Bei Fassungen
  // („American Psycho" als 1080p-MKV und 400p-AVI) wählt man, welche gilt;
  // eine falsch zugeordnete Datei lässt sich abtrennen; ein Film in Teilen
  // (a/b/c, CD1/CD2) lässt sich zu einer Datei zusammenführen.
  // Nur Filme in Teilen: Fassungen (dieselbe Datei in zwei Qualitäten)
  // sind kein Aufräumfall — man wählt sie beim Film unter „Ansehen".
  const istTeile = x => x.files.every((f, i) => f.partNo === i + 1);
  const nurTeile = (d.duplicates || []).filter(x => x.files.length > 1 && istTeile(x));
  // Fassungen (derselbe Film mehrfach, auch gemischt mit Teilen) zeigt die
  // Seite ebenfalls — sonst meldete die Übersicht „1 Doppel", und hier
  // stand nichts dazu.
  const fassungen = (d.duplicates || []).filter(x => x.files.length > 1 && !istTeile(x));
  const doppelZeile = x => {
    const teile = istTeile(x);
    return `
    <div class="orow" style="height:auto;padding:14px 16px;display:block">
      <div style="display:flex;align-items:center;gap:10px;flex-wrap:wrap">
        <div class="t-body" style="flex:1 1 200px">${esc(x.title)}${x.year ? ` (${x.year})` : ''}
          <span class="t-caption c-2">${teile ? ` ${tr('· {n} Teile eines Films', { n: x.files.length })}` : ` ${tr('· {n} Fassungen', { n: x.files.length })}`}</span></div>
        ${teile && voll() ? `<button class="btn btn-pill btn-pill--fill" style="height:30px;font-size:.8125rem" data-teile="${esc(x.itemId)}">${tr('Zu einer Datei zusammenführen')}</button>` : ''}
      </div>
      ${x.files.map(f => `<div style="display:flex;gap:10px;align-items:center;min-height:40px;flex-wrap:wrap">
        <span class="mono c-2" style="flex:1;min-width:240px;word-break:break-all;font-size:.75rem">${esc(f.path)}</span>
        <span class="t-caption c-2 tnum">${[teile ? tr('Teil {nr}', { nr: f.partNo }) : '', f.height ? f.height + 'p' : '', f.container ? f.container.toUpperCase() : '',
          groesse(f.sizeBytes)].filter(Boolean).join(' · ')}</span>
        ${teile ? '' : f.preferred
          ? `<span class="badge badge--gruen">${tr('gewählt')}</span>`
          : `<button class="btn btn-pill" style="height:28px;font-size:.75rem" data-fassung="${esc(x.itemId)}:${esc(f.id)}">${tr('Diese nehmen')}</button>`}
        ${teile ? '' : `<button class="btn btn-pill" style="height:28px;font-size:.75rem;background:rgba(120,120,128,.2)" data-abtrennen="${esc(f.id)}"
          title="${tr('Ist ein anderer Film — als eigenen Titel führen')}">${tr('Abtrennen')}</button>`}
      </div>`).join('')}
    </div>`;
  };
  const dup = nurTeile.map(doppelZeile).join('');
  const dupFassungen = fassungen.map(doppelZeile).join('');

  const missing = (d.missing || []).map(f => `
    <div class="orow">
      <span style="color:#ff9f0a;display:flex;flex:none">${svg('<path d="M12 4 2.8 19.5h18.4z"/><path d="M12 10v4.5M12 17v.01"/>', 19)}</span>
      <span class="mono c-2 clamp-1" style="flex:1">${esc(f.path)}</span>
      ${voll() ? `<button class="btn btn-pill" style="height:28px;font-size:.75rem;flex:none;background:rgba(120,120,128,.2)" data-fehlend-weg="${esc(f.id)}">${tr('Endgültig entfernen')}</button>` : ''}</div>`).join('');
  const nFehlend = d.offen?.missing ?? (d.missing || []).length;

  // Nicht erkannt: wonach Lumen gesucht hat (Titel und Jahr aus dem
  // Dateinamen), warum es nicht gereicht hat, und gleich die Werkzeuge
  // zum Zuordnen.
  const GRUND = {
    keine_treffer: tr('TMDb kennt keinen Titel mit diesem Namen.'),
    unsicher: tr('Kein Treffer passt gut genug — lieber nichts als der falsche Film.'),
    zu_nah: tr('Mehrere Treffer passen gleich gut (etwa eine Neuverfilmung).'),
    fehler: tr('TMDb war nicht erreichbar — Lumen versucht es später noch einmal.'),
    schluessel: tr('TMDb lehnt den Schlüssel ab — der Titel wartet, bis ein gültiger eingetragen ist.'),
  };
  // Ohne (gültigen) Schlüssel wartet ein Titel nicht auf TMDb, sondern auf
  // den Schlüssel — das soll da stehen, nicht „nicht erreichbar".
  const schl = d.schluessel || 'ok';
  const offenText = schl === 'fehlt' ? tr('Wartet auf einen TMDb-Schlüssel.')
    : schl === 'abgelehnt' ? GRUND.schluessel : tr('Noch nicht geprüft.');
  const kurzPfad = p => esc(p.split('/').filter(Boolean).slice(-4).join(' / '));
  const zeileOffen = i => {
    const g = i.grund || null;
    const gesucht = (g?.gesucht || []).slice(0, 4).map(x => `„${esc(x)}“`).join(', ');
    return `
    <div class="orow" style="height:auto;padding:12px 16px;display:block">
      <div style="display:flex;gap:12px;align-items:flex-start;flex-wrap:wrap">
        <div style="flex:1;min-width:220px">
          <div class="t-body">${esc(i.title)}${i.year ? ` <span class="c-2">(${i.year})</span>` : ''}
            <span class="t-caption c-2">· ${i.kind === 'movie' ? tr('Film') : i.kind === 'series' ? tr('Serie') : esc(i.kind)}</span></div>
          ${i.pfad ? `<div class="t-caption c-3" style="font-family:ui-monospace,Menlo,monospace;font-size:.7188rem;margin-top:3px;overflow-wrap:anywhere" title="${esc(i.pfad)}">${kurzPfad(i.pfad)}</div>` : ''}
          <div class="t-caption c-2" style="margin-top:4px;line-height:1.45">${g
            ? `${(g.art === 'fehler' && schl === 'abgelehnt' ? GRUND.schluessel : GRUND[g.art]) || ''}${gesucht ? ' ' + tr('Gesucht: {was}.', { was: gesucht }) : ''}${g.vorschlag ? ' ' + tr('Am nächsten: {titel} ({prozent} %).', { titel: esc(g.vorschlag), prozent: Math.round((g.wert || 0) * 100) }) : ''}`
            : i.matchState === 'pending' ? offenText : tr('Seit dem letzten Neustart nicht mehr geprüft.')}</div>
        </div>
        <div style="display:flex;gap:8px;flex:none;flex-wrap:wrap">
          <button class="btn btn-pill btn-pill--fill" style="height:30px;font-size:.8125rem" data-zuo-suchen="${esc(i.id)}" data-titel="${esc(i.title)}">${tr('Suchen')}</button>
          <button class="btn btn-pill" style="height:30px;font-size:.8125rem" data-zuo-id="${esc(i.id)}">${tr('ID eingeben')}</button>
          <button class="btn btn-pill" style="height:30px;font-size:.8125rem" data-go="detail" data-id="${esc(i.id)}">${tr('Öffnen')}</button>
        </div>
      </div>
      <div data-zuo-box="${esc(i.id)}" style="margin-top:6px"></div>
    </div>`;
  };
  const unmatched = (d.unmatched || []).map(zeileOffen).join('');
  const ausstehend = (d.ausstehend || []).map(zeileOffen).join('');
  const nUnmatched = d.offen?.unmatched ?? (d.unmatched || []).length;
  const nAusstehend = d.offen?.pending ?? (d.ausstehend || []).length;

  // Was der letzte Durchlauf mit Absicht ausgelassen hat.
  const AUSLASS = {
    beiwerk: [tr('Wie Beiwerk benannt'), tr('Der Name endet auf „-trailer“, „-sample“, „-featurette“ … oder ist nur „sample“. Heisst ein Film wirklich so, die Datei umbenennen.')],
    beiwerk_ordner: [tr('Extras-Ordner'), tr('Ordner wie „Extras“, „Trailers“, „Featurettes“ liest Lumen nicht als Filme ein.')],
    endung: [tr('Endung, die Lumen nicht abspielt'), tr('Etwa ISO-Abbilder, .strm oder .rmvb. In MKV oder MP4 umwandeln, dann kommen sie dazu.')],
    unlesbar: [tr('Nicht lesbar'), tr('Lumen hat keine Leserechte (NAS-Gruppe prüfen) oder die Platte meldet einen Fehler.')],
    verweis: [tr('Verweis auf einen Ordner'), tr('Einem Verweis (Symlink) zurück in die Bibliothek, in Lumens Datenordner oder auf einen schon gelesenen Ordner folgt Lumen nicht.')],
    defekt_leer: [tr('Leere Datei'), tr('Die Datei hat 0 Byte – vielleicht wird sie noch kopiert. Ist sie fertig, liest Lumen sie beim nächsten Durchlauf ein.')],
    defekt_gesperrt: [tr('Keine Leserechte'), tr('Lumen darf die Datei nicht öffnen (Rechte auf dem NAS prüfen). Sie wird nicht angeboten, bis sie lesbar ist.')],
    defekt_kaputt: [tr('Defekte Datei'), tr('FFmpeg erkennt darin kein Video (abgebrochener Download, beschädigte Datei). Sie wird nicht angeboten.')],
  };
  const ausgelassen = (d.ausgelassen || []).map(b => b.liste.map(a => `
    <details class="orow" style="height:auto;padding:12px 16px;display:block">
      <summary style="cursor:pointer;display:flex;gap:10px;align-items:baseline;flex-wrap:wrap">
        <span class="t-body" style="flex:1">${esc((AUSLASS[a.grund] || [a.grund])[0])}</span>
        <span class="t-caption c-2">${esc(b.name)} · ${tr('{n} Einträge', { n: a.anzahl })}</span></summary>
      <p class="t-caption c-2" style="margin:6px 0">${esc((AUSLASS[a.grund] || ['', ''])[1])}</p>
      <div class="mono c-2" style="font-size:.7188rem;line-height:1.6;overflow-wrap:anywhere">${a.pfade.map(esc).join('<br>')}${a.anzahl > a.pfade.length ? '<br>…' : ''}</div>
    </details>`).join('')).join('');

  // Der Schlüssel fehlt oder wird abgelehnt: einmal oben sagen, was das
  // heisst und wo er hingehört — statt bei jedem Titel „nicht erreichbar".
  const schluesselHinweis = schl === 'ok' ? '' : `
    <div class="group group--inset" style="margin-top:22px">
      <div class="row" style="min-height:56px;gap:12px;align-items:flex-start;padding:12px 16px">
        <span style="color:${schl === 'abgelehnt' ? '#ff6961' : '#ff9f0a'};display:flex;flex:none;margin-top:2px">${svg('<path d="M12 4 2.8 19.5h18.4z"/><path d="M12 10v4.5M12 17v.01"/>', 19)}</span>
        <div style="flex:1;min-width:0">
          <div class="t-body">${schl === 'abgelehnt' ? tr('TMDb lehnt den Schlüssel ab') : tr('Kein TMDb-Schlüssel eingetragen')}</div>
          <div class="t-caption c-2" style="line-height:1.5;margin-top:2px">${schl === 'abgelehnt'
            ? tr('Meist ist die kurze API-Kennung statt des langen API Read Access Token eingetragen. Bis ein gültiger Schlüssel da ist, gelten Titel mit vollständiger NFO-Datei als erkannt; die übrigen warten.')
            : tr('Titel mit vollständiger NFO-Datei gelten auch so als erkannt, mit Postern aus der Sammlung. Für die übrigen holt Lumen Poster und Beschreibungen, sobald ein Schlüssel eingetragen ist.')}
            ${d.vorgemerkt ? ' ' + tr('{n} gespeicherte Kennungen warten auf den Schlüssel.', { n: d.vorgemerkt }) : ''}</div></div>
        <button class="btn btn-pill btn-pill--fill" style="flex:none" data-go="metadata">${schl === 'abgelehnt' ? tr('Schlüssel ändern') : tr('Eintragen')}</button>
      </div></div>`;
  return shell(`<div class="wrap wrap--flat wrap--verwaltung">
    ${ohead(tr('Aufräumen'), tr('Filme in Teilen, fehlende Dateien, nicht erkannte Titel'))}
    ${schluesselHinweis}
    ${dup ? `<section style="margin-top:26px"><div class="section-h">${tr('Filme in Teilen · {n}', { n: nurTeile.length })}</div>
      <div class="group group--inset">${dup}</div>
      <p class="t-caption c-2" style="margin:8px 16px 0">${tr('Ein Film, auf mehrere Dateien verteilt (a/b/c, CD1/CD2). Im Browser laufen die Teile nacheinander; zusammengeführt läuft der Film überall am Stück. Zusammenführen hängt die Teile ohne Neurechnen aneinander; die Teile wandern in den Ordner „.lumen-teile“.')}</p></section>` : ''}
    ${dupFassungen ? `<section style="margin-top:22px"><div class="section-h">${tr('Mehrere Fassungen · {n}', { n: fassungen.length })}</div>
      <div class="group group--inset">${dupFassungen}</div>
      <p class="t-caption c-2" style="margin:8px 16px 0">${tr('Derselbe Film in mehreren Dateien, etwa in zwei Qualitäten. Das ist in Ordnung: Beim Film wählt man unter „Ansehen“, welche läuft. Gehört eine Datei zu einem anderen Film, trenne sie ab.')}</p></section>` : ''}
    ${missing ? `<section style="margin-top:22px"><div class="section-h">${tr('Fehlende Dateien · {n}', { n: nFehlend })}</div>
      <div class="group group--inset">${missing}</div>
      <p class="t-caption c-2" style="margin:8px 16px 0">${tr('Die Titel bleiben einen Tag lang stehen – eine abgehängte Platte soll die Mediathek nicht leeren. Danach blendet Lumen sie aus; hier lassen sie sich endgültig entfernen. Die Dateien selbst bleiben unberührt.')}</p>
      ${voll() ? `<div style="margin:10px 16px 0"><button class="btn btn-pill" style="height:32px;font-size:.8125rem" data-fehlende-weg-alle>${tr('Alle entfernen, die länger als einen Tag fehlen')}</button></div>` : ''}</section>` : ''}
    ${unmatched ? `<section style="margin-top:22px"><div class="section-h">${tr('Nicht erkannt · {n}', { n: nUnmatched })}</div>
      <div class="group group--inset">${unmatched}</div>
      <p class="t-caption c-2" style="margin:8px 16px 0;line-height:1.5">${tr('Hier war die Zuordnung zu unsicher. Lieber nichts als der falsche Film. „Suchen“ sucht frei bei TMDb, „ID eingeben“ nimmt eine IMDb-, TMDb- oder TheTVDB-Kennung oder einen Link. Zugewiesenes gilt als von Hand zugeordnet und bleibt so.')}
        ${tr('Tipp: Eine Kennung im Ordner- oder Dateinamen, etwa „Matrix (1999) {imdb-tt0133093}“, ordnet beim nächsten Durchlauf ohne Suche zu.')}</p>
      <div style="margin:10px 16px 0"><button class="btn btn-pill btn-pill--fill" style="height:32px;font-size:.8125rem" data-fk-uebernahme>${tr('Zuordnungen aus Emby oder Jellyfin übernehmen …')}</button></div></section>` : ''}
    ${ausstehend ? `<section style="margin-top:22px"><div class="section-h">${tr('Wartet auf die Zuordnung · {n}', { n: nAusstehend })}</div>
      <div class="group group--inset">${ausstehend}</div>
      <p class="t-caption c-2" style="margin:8px 16px 0">${schl === 'ok' ? tr('Diese Titel prüft der nächste Metadaten-Durchgang.')
        : tr('Diese Titel haben keine vollständige NFO-Datei. „ID eingeben“ speichert eine Kennung auch ohne Schlüssel.')}</p></section>` : ''}
    ${ausgelassen ? `<section style="margin-top:22px"><div class="section-h">${tr('Ausgelassene Dateien')}</div>
      <div class="group group--inset">${ausgelassen}</div>
      <p class="t-caption c-2" style="margin:8px 16px 0">${tr('Was der letzte Durchlauf jeder Bibliothek mit Absicht ausgelassen hat. Die Liste gilt bis zum nächsten Durchlauf oder Neustart.')}</p></section>` : ''}
    ${aufgeloest.length ? `<section style="margin-top:22px"><div class="section-h">${tr('Aufgelöste Filmreihen · {n}', { n: aufgeloest.length })}</div>
      <div class="group group--inset">${aufgeloest.map(r => `
        <div class="orow" style="height:auto;padding:12px 16px">
          <div style="flex:1;min-width:0"><div class="t-body">${esc(r.name)}</div>
            <div class="t-caption c-2">${tr('{n} Filme', { n: r.anzahl })}</div></div>
          <button class="btn btn-pill btn-pill--fill" style="flex:none" data-reihe-zurueck="${esc(r.id)}">${tr('Wiederherstellen')}</button>
        </div>`).join('')}</div>
      <p class="t-caption c-2" style="margin:8px 16px 0">${tr('Lumen bildet sie nicht wieder, bis du sie zurückholst.')}</p></section>` : ''}
    ${!dup && !missing && !unmatched && !ausstehend && !ausgelassen && !aufgeloest.length && !schluesselHinweis ? `<p class="hint" style="margin-top:30px">${tr('Hier ist alles in Ordnung.')}</p>` : ''}
    <div style="height:40px"></div></div>`);
}

function viewAccounts() {
  // Rechte direkt am Konto: nur schauen, mitverwalten oder Vollzugriff
  // (alles wie der Besitzer). Ändern verlangt den Code noch einmal.
  const rolle = a => a.vollzugriff ? 'voll' : a.isAdmin ? 'verwalten' : 'schauen';
  const ich = S.me?.account?.id;
  // PIN zurücksetzen: nie beim Besitzer, nicht beim eigenen Profil (das
  // geht unter „Ich"), und ein Mitverwalter nur bei Konten ohne Verwaltung.
  // verwaltbar: Mitverwaltende mit nur einem Teil der Bibliotheken
  // ändern nur Konten ganz innerhalb ihres Bereichs (der Server prüft es).
  const darfPin = a => !a.isOwner && a.id !== ich && (voll() || !a.isAdmin) && a.verwaltbar !== false;
  const rows = (S.data?.accounts || []).map(a => `
    <div class="orow" style="height:auto;padding:14px 16px;gap:12px;flex-wrap:wrap">
      ${avatar(a, 34, 'flex:none')}
      <div style="flex:1;min-width:180px">
        <div class="t-body">${esc(a.name)}</div>
        <div class="t-caption c-2">${a.isOwner ? tr('Besitzer') : ROLLEN_GRUPPEN[a.groupId] ? esc(rollenName(a.groupId)) : a.isKid ? esc(tr(T.kinderprofilBis, { n: a.limits.maxAge })) : tr('Haushalt')}${a.groupName && !ROLLEN_GRUPPEN[a.groupId] ? ` ${tr('· Gruppe „{groupName}“', { groupName: esc(a.groupName) })}` : ''}
          · ${tr('{n} Bibliotheken', { n: a.libraries.length })} · ${tr('{n} Geräte', { n: a.devices })} · ${a.isOwner || a.isAdmin ? tr('ohne Grenzen') : tr('höchstens {maxStreams} gleichzeitig', { maxStreams: a.limits.maxStreams })} · ${esc(schutzText(a.lockKind, true))}${bzKontoText(a) ? ` · ${esc(bzKontoText(a))}` : ''}</div>
      </div>
      <div class="konto-knoepfe">
      ${!a.isOwner && !a.isAdmin && a.id !== ich && a.verwaltbar !== false ? `<button class="btn btn-pill" style="height:30px;font-size:.8125rem;flex:none" data-grenzen="${esc(a.id)}" aria-label="${tr('Was {name} darf', { name: esc(a.name) })}">${tr('Bearbeiten')}</button>` : ''}
      ${a.bildschirmzeit && a.verwaltbar !== false ? `<button class="btn btn-pill" style="height:30px;font-size:.8125rem;flex:none" data-bildschirmzeit="${esc(a.id)}">${tr('Bildschirmzeit')}</button>` : ''}
      ${darfPin(a) ? `<button class="btn btn-pill" style="height:30px;font-size:.8125rem;flex:none" data-pinreset="${esc(a.id)}">${tr('PIN zurücksetzen')}</button>` : ''}
      ${!a.isOwner && a.id !== ich && voll() ? `<button class="btn btn-pill" style="height:30px;font-size:.8125rem;flex:none;background:rgba(255,69,58,.14);color:#ff6961" data-kontoweg="${esc(a.id)}">${tr('Entfernen')}</button>` : ''}
      ${a.isOwner ? `<span class="t-caption c-2">${tr('alle Rechte')}</span>` : voll() ? `
        <select class="takt" data-rolle="${esc(a.id)}" aria-label="${tr('Rechte von {name}', { name: esc(a.name) })}">
          ${[['schauen', tr('Nur schauen')], ['verwalten', tr('Verwaltet mit')], ['voll', tr('Vollzugriff')]]
            // Ein Kinderprofil bekommt keine Verwaltung (der Server lehnt
            // es ab) – dann gar nicht erst anbieten.
            .filter(([k]) => !a.isKid || k === 'schauen' || rolle(a) === k).map(([k, n]) =>
            `<option value="${k}"${rolle(a) === k ? ' selected' : ''}>${n}</option>`).join('')}</select>`
        : `<span class="t-caption c-2">${{ schauen: tr('Nur schauen'), verwalten: tr('Verwaltet mit'), voll: tr('Vollzugriff') }[rolle(a)]}</span>`}
      </div>
    </div>`).join('');
  return shell(`<div class="wrap wrap--flat wrap--verwaltung">
    ${ohead(tr('Konten und Rechte'), tr('Wer dazugehört und was er sehen darf'))}
    <p class="t-sub c-2" style="margin:22px 0 0;display:flex;align-items:center;gap:8px">
      ${svg(I.lock, 14, 2.2)}${tr('Du legst fest, was jemand sehen darf. Was er davon schaut, siehst du nicht.')}</p>
    <section style="margin-top:22px"><div class="group group--inset">${rows}</div>
      <p class="t-caption c-2" style="margin:8px 16px 0"><b>${tr('Verwaltet mit:')}</b> ${tr('Bibliotheken prüfen, Konten ohne Verwaltung, Wünsche, Protokoll.')}
        <b>${tr('Vollzugriff:')}</b> ${tr('alles wie der Besitzer — auch Gruppen, Rechte, Updates, Sicherungen und neue Bibliotheken; nur dein eigenes Konto nicht. Wer was schaut, sieht niemand.')}</p></section>
    <section style="margin-top:22px"><div class="section-h">${tr('Jemanden einladen')}</div>
      <div class="group group--inset" style="padding:16px">
        ${einladungsFormular(S.data?.libraries || [], S.einladungKind)}
        <p class="t-caption c-2" style="margin-top:12px">${tr('Gilt sieben Tage und nur einmal.')}</p>
        <div id="invitebox"></div></div></section>
    ${offeneEinladungen(S.data?.einladungen || [])}
    <div style="height:40px"></div></div>`);
}

/* Offene Einladungen: wann sie ablaufen, für wen ungefähr (Kind oder
   Erwachsen, wie viele Bibliotheken) — und Zurückziehen, etwa für einen
   Link, der im falschen Chat gelandet ist. */
function offeneEinladungen(liste) {
  if (!liste.length) return '';
  return `<section id="offene-einladungen" style="margin-top:22px"><h2 class="section-h" style="margin:0">${tr('Offene Einladungen')}</h2>
    <div class="group group--inset">${liste.map(e => `
      <div class="row" style="min-height:56px;gap:12px;flex-wrap:wrap">
        <span style="flex:1;min-width:200px"><span class="t-body">${e.kinderprofil ? esc(T.kinderprofil) : tr('Erwachsenes Profil')}
          <span class="mono c-2" style="font-size:.8125rem">…${esc(e.ende)}</span></span>
          <span class="t-caption c-2" style="display:block">${tr('gilt bis {wann}', { wann: esc(datumZeit(e.expiresAt, { day: 'numeric', month: 'long', hour: '2-digit', minute: '2-digit' })) })}
            · ${tr('{n} Bibliotheken', { n: e.bibliotheken })}${e.von ? ` · ${tr('von {name}', { name: esc(e.von) })}` : ''}</span></span>
        <button class="btn btn-pill btn-pill--fill" style="height:32px;font-size:.8125rem;flex:none" data-einladung-weg="${esc(e.kennung)}"
          aria-label="${tr('Einladung …{ende} zurückziehen', { ende: esc(e.ende) })}">${tr('Zurückziehen')}</button>
      </div>`).join('')}</div>
    <p class="t-caption c-2" style="margin:8px 16px 0">${tr('Eine zurückgezogene Einladung lässt sich nicht mehr einlösen.')}</p></section>`;
}

// „CH" → „Schweiz" / „Switzerland".
const regionName = r => { try { return new Intl.DisplayNames([sprache()], { type: 'region' }).of(r) || r; } catch { return r; } };

function viewMetadata() {
  const d = S.data || {};
  return shell(`<div class="wrap wrap--flat wrap--verwaltung">
    ${ohead(tr('Poster und Beschreibungen'), tr('Woher Lumen sie holt — und wo die Schlüssel dafür hingehören'))}
    <p class="t-sub c-2" style="margin:20px 0 0">
      ${tr('Du richtest das einmal ein, dann gilt es für alle. Niemand im Haushalt braucht ein eigenes Konto.')}</p>
    <form id="metaform" style="margin-top:26px">
      ${grp(tr('TMDb — Filme und Serien'),
        `<div style="padding:16px" class="field">
          <label for="tmdb">${tr('Schlüssel (API Read Access Token)')}</label>
          <input id="tmdb" name="tmdbKey" class="mono" placeholder="${d.tmdb?.configured ? tr('•••••••• — hinterlegt') : tr('eyJhbGciOi…')}">
          ${d.schluessel === 'abgelehnt' ? `<span class="hint" role="status" style="color:#ff6961">${tr('TMDb lehnt den hinterlegten Schlüssel ab. Bitte neu eintragen.')}</span>` : ''}
          <span class="hint">${tr('Ohne diesen Schlüssel kommen Poster und Beschreibungen nur aus NFO-Dateien und Bildern neben den Filmen. Kostenlos: auf {seite} anmelden, dann unter {menue} den {token} kopieren — den langen, der mit {anfang} anfängt, nicht die kurze API-Kennung.', {
              seite: '<a href="https://www.themoviedb.org/signup" target="_blank" rel="noopener noreferrer">themoviedb.org</a>',
              menue: `<span class="mono">${tr('Einstellungen → API')}</span>`, token: '<b>API Read Access Token</b>',
              anfang: '<span class="mono">eyJ</span>' })}</span></div>`)}
      ${grp(tr('IMDb-Wertung (über OMDb)'),
        `<div style="padding:16px" class="field">
          <label for="omdb">${tr('Schlüssel')}</label>
          <input id="omdb" name="omdbKey" class="mono" placeholder="${d.imdb?.configured ? tr('•••••••• — hinterlegt') : tr('optional')}">
          <span class="hint">${tr('Nur für die Note am Titel; ohne ihn bleibt alles andere unverändert. Kostenlos auf {seite} — der Schlüssel kommt per E-Mail und muss über den Link darin einmal bestätigt werden.', {
              seite: '<a href="https://www.omdbapi.com/apikey.aspx" target="_blank" rel="noopener noreferrer">omdbapi.com</a>' })}</span></div>`)}
      ${grp(tr('Sprache und Region'),
        `<div style="padding:16px;display:grid;grid-template-columns:minmax(0,1fr);gap:14px">
          <div class="field"><label for="lang">${tr('Sprache der Metadaten')}</label>
            <select id="lang" name="language">
              ${['de-DE', 'de-CH', 'de-AT', 'en-US', 'en-GB', 'fr-FR', 'it-IT'].map(l =>
                `<option value="${l}"${d.language === l ? ' selected' : ''}>${esc(sprachName(l))}</option>`).join('')}</select></div>
          <div class="field"><label for="region">${tr('Region für die Altersfreigabe')}</label>
            <select id="region" name="region">
              ${['CH', 'DE', 'AT', 'US', 'GB'].map(r =>
                `<option value="${r}"${d.region === r ? ' selected' : ''}>${esc(regionName(r))}</option>`).join('')}</select></div>
        </div>`,
        tr('Die Region entscheidet, welche Altersfreigabe gilt — in der Schweiz eine andere als in den USA.'))}
      ${grp(tr('Lokale Metadaten'),
        `<label class="row" style="padding:12px 16px;gap:12px;cursor:pointer;align-items:flex-start">
          <input type="checkbox" name="nfoBevorzugen"${d.nfoBevorzugen !== false ? ' checked' : ''} style="margin-top:3px">
          <span><span class="t-body">${tr('Lokale Metadaten (NFO) bevorzugen')}</span>
            <span class="t-caption c-2" style="display:block;margin-top:2px">${tr('Liegen neben den Dateien NFO-Dateien (Kodi, Emby, Jellyfin, tinyMediaManager), gelten Titel, Beschreibung, Genres und Freigabe daraus vor denen von TMDb. Eine Kennung in der NFO wird immer genutzt.')}</span></span></label>`,
        tr('Poster, Hintergründe und Logos neben den Dateien (poster.jpg, fanart.jpg …) gehen immer vor.'))}
      <button class="btn btn-primary" style="height:46px;width:100%">${tr('Speichern')}</button>
    </form>
    ${d.ready ? `<button class="btn btn-pill btn-pill--fill" style="margin-top:18px" data-resolve>
        ${tr('Fehlende Metadaten jetzt holen')}</button>` : ''}
    ${metadatenUntertitel() ? `<div style="margin-top:26px">${grp(tr('Untertitel'), metadatenUntertitel(),
      tr('Damit alle im Player und auf der Titelseite Untertitel suchen können.'))}</div>` : ''}
    <div style="height:40px"></div></div>`);
}

/* ---------------------------------------------------------------- Klicks */

app.addEventListener('click', async e => {
  const hit = sel => e.target.closest(`[${sel}]`);
  let n;
  // Handbearbeitung, Sammlungen, Übernahme, Untertitel (funktionen.js).
  if (funktionenKlick(e, { nachSpuren: spurenNachHolen })) return;

  if ((n = hit('data-pick'))) {
    const id = n.dataset.pick;
    if (n.dataset.lock === 'none') {
      try { S.me = { account: await api.unlock(id, '') }; S.me = await api.me(); go('home'); return anmeldeHinweis(); }
      catch (err) { S.error = err.message; return render(); }
    }
    S.id = id; S.view = 'unlock'; S.error = ''; return render();
  }
  if (hit('data-navzu')) {
    S.navZu = !S.navZu;
    try { localStorage.setItem('lumen.navZu', S.navZu ? '1' : '0'); } catch {}
    // Nur die Leiste neu setzen: ein ganzes render() würde die Seite
    // darunter neu aufbauen und etwa die Stelle im Raster verlieren.
    const leiste = document.querySelector('.sidebar');
    if (leiste) {
      leiste.outerHTML = sidebar();
      return;
    }
    return render();
  }
  /* Am Stück */
  if (hit('data-naechster-jetzt')) {
    const id = document.getElementById('naechster')?.dataset.id;
    return id ? naechsterStarten(id) : null;
  }
  if (hit('data-naechster-nein')) {
    naechsterWeg();
    const v = $('#v');
    // Im Abspann abgebrochen: der Film läuft zu Ende, danach bleibt es still.
    if (S.playing && v && !v.ended) { S.playing.naechsterAngeboten = true; S.playing.abgelehnt = true; return; }
    return markDoneAndLeave();
  }
  if (hit('data-skipintro')) return vorspannUeberspringen();
  if ((n = hit('data-spuren'))) {
    const p = S.playing;
    if (p) { p.spurenOffen = p.spurenOffen === n.dataset.spuren ? false : n.dataset.spuren; p.spurenHtml = ''; showChrome(true); }
    return;
  }
  if ((n = hit('data-spur'))) return spurWaehlen(Number(n.dataset.spur));
  if ((n = hit('data-untertitel'))) return untertitelWaehlen(n.dataset.untertitel);
  if (hit('data-intro-von')) { const p = S.playing; if (p) { p.introMarke = p.offset + p.current; patchChrome(); } return; }
  if (hit('data-intro-abbruch')) { const p = S.playing; if (p) { p.introMarke = null; patchChrome(); } return; }
  if (hit('data-intro-bis')) {
    const p = S.playing;
    if (!p || p.introMarke == null) return;
    const von = Math.round(p.introMarke * 1000), bis = Math.round((p.offset + p.current) * 1000);
    try {
      await api.adminSetIntro(p.seriesId, von, bis);
      p.introMarke = null; p.introUebersprungen = false;
      await vorspannNeuLaden(p);
      toast(tr('Vorspann für die ganze Serie gemerkt')); patchChrome();
    } catch (err) { toast(err.message); }
    return;
  }
  if ((n = hit('data-vonhand'))) return vonHandSetzen(n.dataset.vonhand);
  if (hit('data-liste-abspielen')) {
    const items = S.data?.items || [];
    S.warteschlange = { name: S.data?.name || tr('Liste'), ids: items.map(x => x.id), items, i: -1,
      zurueck: S.view === 'playlist' ? S.id : null };
    const p = await naechsterTitel({}).catch(() => null);
    if (!p) { S.warteschlange = null; return toast(tr('In dieser Liste ist nichts zum Abspielen.')); }
    return startPlayer(p.id, 0);
  }

  /* Umbenennen von Reihen und Listen */
  if ((n = hit('data-bearbeiten'))) { S.bearbeiten = n.dataset.bearbeiten; return render(); }
  if (hit('data-bearbeiten-aus')) { S.bearbeiten = null; return render(); }

  /* Filmreihen */
  if (hit('data-reihe-aufloesen') || hit('data-reihe-zeigen')) {
    const weg = !!hit('data-reihe-aufloesen');
    try { await api.adminSetCollection(S.id, { aufgeloest: weg }); }
    catch (err) { return toast(err.message); }
    toast(weg ? tr('Reihe aufgelöst — die Filme bleiben') : tr('Reihe wiederhergestellt'));
    return weg ? go('collections', { ersetzen: true }) : load();
  }
  if ((n = hit('data-reihe-zurueck'))) {
    try { await api.adminSetCollection(n.dataset.reiheZurueck, { aufgeloest: false }); }
    catch (err) { return toast(err.message); }
    toast(tr('Reihe wiederhergestellt'));
    return load();
  }

  /* Listen */
  if ((n = hit('data-lsw'))) {
    const an = n.getAttribute('aria-checked') !== 'true';
    try { await api.updatePlaylist(S.id, { [n.dataset.lsw]: an }); }
    catch (err) { return toast(err.message); }
    return load();
  }
  if (hit('data-liste-weg')) { S.listeWeg = true; return render(); }
  if (hit('data-liste-weg-nein')) { S.listeWeg = false; return render(); }
  if (hit('data-liste-weg-ok')) {
    try { await api.deletePlaylist(S.id); } catch (err) { return toast(err.message); }
    toast(tr('Liste gelöscht'));
    return go('playlists', { ersetzen: true });
  }
  if ((n = hit('data-lweg'))) {
    try { await api.removeFromPlaylist(S.id, n.dataset.lweg); } catch (err) { return toast(err.message); }
    return load();
  }
  if ((n = hit('data-lhoch')) || (n = hit('data-lrunter'))) {
    const ids = (S.data?.items || []).map(x => x.id);
    const id = n.dataset.lhoch || n.dataset.lrunter;
    const i = ids.indexOf(id), j = n.dataset.lhoch ? i - 1 : i + 1;
    if (i < 0 || j < 0 || j >= ids.length) return;
    [ids[i], ids[j]] = [ids[j], ids[i]];
    try { await api.orderPlaylist(S.id, ids); } catch (err) { return toast(err.message); }
    return load();
  }
  if ((n = hit('data-zurliste'))) {
    const box = document.getElementById('listenpanel');
    if (box && box.innerHTML.trim()) {
      box.innerHTML = '';
      n.setAttribute('aria-expanded', 'false');
      return;
    }
    return listenPanel(S.id);
  }
  if ((n = hit('data-lpanel'))) {
    const drin = n.dataset.drin === '1';
    try {
      await (drin ? api.removeFromPlaylist(n.dataset.lpanel, S.id) : api.addToPlaylist(n.dataset.lpanel, S.id));
    } catch (err) { return toast(err.message); }
    toast(drin ? tr('Aus der Liste genommen') : tr('In die Liste gelegt'));
    return listenPanel(S.id);
  }

  if (hit('data-schutz')) {
    const a = S.me.account;
    const ok = await schutzDialog({
      titel: a.lockKind === 'none' ? tr('Profil schützen') : tr('PIN oder Passwort ändern'),
      mitAlt: true, altArt: a.lockKind, ohneErlaubt: !a.isAdmin,
      senden: async ({ alt, art, neu }) => {
        try { await api.meineSperre(alt, art, neu); }
        catch (err) { if (err.code === 'falsch') err.message = err.message || tr('Das stimmt nicht.'); throw err; }
      },
    });
    if (ok) {
      S.me = await api.me().catch(() => S.me);
      const war = S.schutzNoetig; S.schutzNoetig = false;
      toast(S.me.account.lockKind === 'none' ? tr('Dein Profil ist jetzt ohne Schutz') : tr('Gespeichert'));
      if (war && S.me.account.lockKind !== 'none') return go('admin');
      return render();
    }
    return;
  }
  if ((n = hit('data-kontoweg'))) {
    const a = (S.data?.accounts || []).find(x => x.id === n.dataset.kontoweg);
    if (!a) return;
    const ok = await frage(tr('{name} entfernen?', { name: a.name }),
      tr('Das Profil verschwindet von allen Geräten, samt Verlauf, Merkliste, Bewertungen und eigenen Listen. Was {name} in geteilte Listen anderer gelegt hat, bleibt dort. Das lässt sich nicht rückgängig machen.', { name: a.name }),
      tr('Entfernen'));
    if (!ok) return;
    try { await api.adminDeleteAccount(a.id); toast(tr('{name} ist entfernt', { name: a.name })); return load(); }
    catch (err) { if (!err?.abgebrochen) toast(err.message); return; }
  }
  if ((n = hit('data-grenzen'))) {
    const a = (S.data?.accounts || []).find(x => x.id === n.dataset.grenzen);
    if (a) grenzenBearbeiten(a, S.data?.libraries || [], () => { if (S.view === 'accounts') load(); });
    return;
  }
  if ((n = hit('data-bildschirmzeit'))) {
    const a = (S.data?.accounts || []).find(x => x.id === n.dataset.bildschirmzeit);
    if (a) bzBearbeiten(a, () => { if (S.view === 'accounts') load(); });
    return;
  }
  if ((n = hit('data-pinreset'))) {
    const a = (S.data?.accounts || []).find(x => x.id === n.dataset.pinreset);
    if (!a) return;
    // Die Verwaltung wählt die PIN nicht mehr selbst: Lumen vergibt einen
    // Einmal-Code, und beim Anmelden legt die Person ihre eigene PIN fest.
    // Der Server löscht beim Zurücksetzen den privaten Stand des Profils
    // (store/vergessen.go) – das muss vorher dastehen.
    const ok = await rueckfrage({
      titel: tr('PIN von {name} zurücksetzen?', { name: a.name }),
      text: tr('Lumen zeigt dir gleich einen Einmal-Code für {name}. Damit meldet sich {name} an und legt dabei eine eigene PIN fest – du kennst sie danach nicht. Verlauf, Merkliste und Bewertungen dieses Profils werden gelöscht, sonst könnte die Verwaltung sie einsehen.', { name: a.name }),
      ja: tr('Zurücksetzen'), gefaehrlich: true,
    });
    if (!ok) return;
    try {
      const r = await api.adminKontoSperre(a.id, 'pin');
      await mitteilung(tr('Einmal-Code für {name}', { name: a.name }),
        tr('{code} – gib diesen Code {name} weiter. Er gilt für eine einzige Anmeldung innerhalb von 72 Stunden; dabei legt {name} eine eigene PIN fest. Lumen zeigt ihn nur jetzt.', { code: r?.einmalCode || '', name: a.name }));
      return load();
    } catch (err) { if (!err?.abgebrochen) toast(err.message); return; }
  }
  if ((n = hit('data-einladungsart'))) {
    // Nur das Feld ein- oder ausblenden: Neu zeichnen verlöre die gewählte
    // Altersgrenze und die Häkchen bei den Bibliotheken.
    S.einladungKind = n.value === 'kind';
    const feld = n.form?.querySelector('[data-einladung-alter]');
    if (feld) feld.hidden = !S.einladungKind;
    return;
  }
  if ((n = hit('data-lockkind'))) {
    const f = document.getElementById('redeemform');
    if (f) S.redeemForm = { code: f.code.value, name: f.name.value };
    S.redeemLock = n.dataset.lockkind;
    S.error = '';
    return render();
  }
  if ((n = hit('data-setupsprache-zurueck'))) {
    e.preventDefault();
    S.setupSpracheFertig = false;
    return render();
  }
  if ((n = hit('data-go'))) {
    e.preventDefault();
    return go(n.dataset.go, { id: n.dataset.id });
  }
  if ((n = hit('data-kopieren'))) {
    const text = n.dataset.kopieren;
    try {
      // navigator.clipboard gibt es nur über https oder auf localhost.
      // Von unterwegs ist das gegeben, im Heimnetz über http nicht —
      // dann der alte Weg über ein unsichtbares Feld.
      if (navigator.clipboard?.writeText) await navigator.clipboard.writeText(text);
      else {
        const feld = document.createElement('textarea');
        feld.value = text;
        feld.style.position = 'fixed';
        feld.style.opacity = '0';
        document.body.appendChild(feld);
        feld.select();
        document.execCommand('copy');
        feld.remove();
      }
      return toast(tr('Fingerabdruck kopiert'));
    } catch { return toast(tr('Kopieren ging nicht — von Hand markieren')); }
  }
  if (hit('data-retry')) return load();
  if (hit('data-back')) return zurueck();

  /* Titelseite */
  if ((n = hit('data-play'))) {
    S.warteschlange = null;
    const from = n.dataset.from != null ? Number(n.dataset.from) : undefined;
    S.fassungMenu = null;
    return startPlayer(n.dataset.play, from, n.dataset.file || null);
  }
  if ((n = hit('data-season'))) {
    S.staffelWahl = { serie: S.id, staffel: n.dataset.season };
    const eps = await api.children(n.dataset.season).catch(() => []);
    S.data = { ...S.data, episodes: eps, staffel: n.dataset.season };
    return render();
  }
  if ((n = hit('data-serie-play'))) {
    S.warteschlange = null;
    const f = await ersteOffeneFolge(n.dataset.seriePlay).catch(() => null);
    if (!f) return toast(tr('In dieser Serie liegt noch keine Folge.'));
    return startPlayer(f.id);
  }
  if (hit('data-bioganz')) {
    document.getElementById('bio')?.classList.remove('clamp-6');
    document.querySelector('[data-bioganz]')?.remove();
    return;
  }
  if ((n = hit('data-fav'))) {
    const on = n.dataset.fav === 'on';
    try {
      await (on ? api.addFavorite(S.id) : api.removeFavorite(S.id));
      toast(on ? tr('Zu den Favoriten hinzugefügt') : tr('Aus den Favoriten entfernt'));
      return load();
    } catch (err) { return toast(err.message); }
  }
  if (hit('data-sortrichtung')) {
    const { key, dir } = sortierungFuer(S.view);
    return sortierungWaehlen(S.view, key, dir === 'asc' ? 'desc' : 'asc');
  }
  // Neu mischen: ohne Samen fragen, der Server würfelt einen neuen.
  if (hit('data-sortmischen')) return load();
  if ((n = hit('data-buchstabe'))) { S.buchstabe = n.dataset.buchstabe; return load(); }
  if ((n = hit('data-fassungmenu'))) {
    S.fassungMenu = S.fassungMenu === n.dataset.fassungmenu ? null : n.dataset.fassungmenu;
    return render();
  }
  if ((n = hit('data-fassungstandard'))) {
    const [item, datei] = n.dataset.fassungstandard.split(':');
    try {
      await api.call(`/admin/items/${encodeURIComponent(item)}/fassung`, { method: 'PUT', body: { fileId: datei } });
      toast(tr('Gilt jetzt für alle'));
      return load();
    } catch (err) {
      return toast(err.status === 404 || err.status === 403 ? tr('Erst die Verwaltung entsperren, dann geht das.') : err.message);
    }
  }
  if ((n = hit('data-favart'))) { S.favArt = n.dataset.favart; return load(); }
  if ((n = hit('data-list'))) {
    const on = n.dataset.list === 'on';
    try {
      await (on ? api.addToList(S.id) : api.removeFromList(S.id));
      toast(on ? tr('Zur Merkliste hinzugefügt') : tr('Aus der Merkliste entfernt'));
      return load();
    } catch (err) { return toast(err.message); }
  }
  // Die Erfolgsmeldung nur, wenn es geklappt hat — vorher überschrieb
  // sie die Fehlermeldung, und man glaubte, es sei gespeichert.
  if (hit('data-seen')) {
    // Bei einer Serie setzt der Server den Haken an jeder Folge.
    try { await api.markFinished(S.id); } catch (err) { return toast(err.message); }
    toast(S.data?.item?.kind === 'series' ? tr('Alle Folgen als gesehen markiert — nur für dich sichtbar') : tr('Als gesehen markiert — nur für dich sichtbar'));
    return load();
  }
  if (hit('data-unseen')) {
    try { await api.clearFinished(S.id); } catch (err) { return toast(err.message); }
    toast(tr('Nicht mehr als gesehen markiert'));
    return load();
  }
  if ((n = hit('data-staffel-gesehen'))) {
    const an = n.getAttribute('aria-pressed') !== 'true';
    S.staffelWahl = { serie: S.id, staffel: n.dataset.staffelGesehen };
    try { await (an ? api.markFinished(n.dataset.staffelGesehen) : api.clearFinished(n.dataset.staffelGesehen)); }
    catch (err) { return toast(err.message); }
    toast(an ? tr('Staffel als gesehen markiert') : tr('Staffel nicht mehr als gesehen markiert'));
    return load();
  }
  if ((n = hit('data-folge-gesehen'))) {
    const id = n.dataset.folgeGesehen;
    const an = n.getAttribute('aria-pressed') !== 'true';
    try { await (an ? api.markFinished(id) : api.clearFinished(id)); } catch (err) { return toast(err.message); }
    S.staffelWahl = { serie: S.id, staffel: S.data?.staffel };
    // Gibt es davor noch Ungesehenes, einmal nachfragen — „bis hier gesehen"
    // ist der häufige Fall, wenn man eine Serie anderswo angefangen hat.
    const eps = S.data?.episodes || [];
    const i = eps.findIndex(e => e.id === id);
    const folge = eps[i];
    let davorOffen = eps.slice(0, Math.max(0, i)).some(e => !e.finished);
    if (an && !davorOffen && folge && folge.seasonNo > 1) {
      const frueher = (S.data?.seasons || []).filter(x => (x.seasonNo || 0) > 0 && x.seasonNo < folge.seasonNo);
      const listen = await Promise.all(frueher.map(x => api.children(x.id).catch(() => [])));
      davorOffen = listen.flat().some(e => !e.finished);
    }
    await load();
    if (an && davorOffen && folge && folge.seasonNo > 0 && S.data) { S.data.vorherigeFrage = id; render(); }
    return;
  }
  if ((n = hit('data-vorherige-ja'))) {
    try { await api.markFinished(n.dataset.vorherigeJa, { vorherige: true }); } catch (err) { return toast(err.message); }
    toast(tr('Alle Folgen bis hier als gesehen markiert'));
    return load();
  }
  if (hit('data-vorherige-nein')) { if (S.data) S.data.vorherigeFrage = null; return render(); }
  if ((n = hit('data-rate'))) {
    const stars = Number(n.dataset.rate);
    const geteilt = (S.me.prefs || {}).shareRatings === '1';
    try { await api.rate(S.id, stars, geteilt); toast(!stars ? tr('Bewertung gelöscht') : geteilt ? tr('Bewertung gespeichert — im Haushalt sichtbar') : tr('Bewertung gespeichert — nur für dich sichtbar')); }
    catch (err) { toast(err.message); }
    return load();
  }

  /* Suche und Wünsche */
  if ((n = hit('data-filter'))) { S.filter = n.dataset.filter; return load(); }
  if ((n = hit('data-genre'))) return go('movies', { filter: n.dataset.genre });
  if (hit('data-back')) return history.length > 1 ? history.back() : go('home');
  if (hit('data-clear')) { S.query = ''; return load(); }
  if ((n = hit('data-wish'))) {
    // Statt gleich zu wünschen: das Wunschfeld mit dem Suchwort und den
    // Treffern von TMDb darunter (wuensche.js).
    const ort = n.closest('.leer-aktion') || n.parentElement;
    ort.innerHTML = `<div class="wunsch-aus-suche">${wunschFeld({ id: 'wunsch-suche', wert: n.dataset.wish, symbol: svg(I.plus, 18, 2) })}
      <p class="hint" style="margin:10px 0 0">${tr('Wähl einen Treffer oder wünsch nur den Namen. Der Besitzer sieht den Wunsch und sonst nichts.')}</p></div>`;
    wunschVerhalten.nachZeichnen();
    return;
  }
  if ((n = hit('data-wishstate'))) {
    const [id, state] = n.dataset.wishstate.split(':');
    await api.adminWishState(id, state).catch(err => toast(err.message));
    return load();
  }

  /* Player */
  if (hit('data-stop')) return stopPlayer();
  if (hit('data-toggle')) {
    const v = $('#v');
    if (!v) return;
    if (v.paused) { await v.play().catch(() => {}); showChrome(); }
    else { v.pause(); }
    return;
  }
  if ((n = hit('data-seek'))) {
    const v = $('#v'), p = S.playing;
    if (!v || !p) return;
    const target = Math.max(0, p.offset + v.currentTime + Number(n.dataset.seek));
    return seekTo(target);
  }
  // Die Zeitleiste bedient pointerdown/-up (Ziehen mit Vorschau); ein
  // Klick darauf darf nicht noch einmal springen.
  if (hit('data-bar')) return;
  if (hit('data-stumm')) return stummUmschalten();
  if (hit('data-vollbild')) return vollbildUmschalten();
  if (hit('data-pip')) return bildImBildUmschalten();
  if ((n = hit('data-tempo'))) { tempoSetzen(Number(n.dataset.tempo)); return; }
  if ((n = hit('data-kapitel'))) {
    const p = S.playing;
    if (!p) return;
    p.spurenOffen = false; p.spurenHtml = '';
    seekTo(Number(n.dataset.kapitel) / 1000);
    patchChrome();
    app.querySelector('[data-spuren="kapitel"]')?.focus({ preventScroll: true });
    return;
  }
  if ((n = hit('data-nochda-weiter'))) {
    eingabeGesehen();
    document.getElementById('nochda')?.remove();
    return naechsterStarten(n.dataset.nochdaWeiter);
  }
  if (hit('data-nochda-aus')) { eingabeGesehen(); return stopPlayer(); }
  if (S.view === 'player' && e.target.closest('[data-surface]')) {
    const p = S.playing;
    if (!p.ui) return showChrome();
    // Ein Tipp neben ein offenes Menü schliesst es — und hält den Film nicht an.
    if (p.spurenOffen) { p.spurenOffen = false; p.spurenHtml = ''; return patchChrome(); }
    const v = $('#v');
    if (v) { v.paused ? v.play().catch(() => {}) : v.pause(); }
    return;
  }

  /* Einstellungen */
  if ((n = hit('data-pref'))) {
    const key = n.dataset.pref;
    const on = n.getAttribute('aria-checked') !== 'true';
    n.setAttribute('aria-checked', String(on));
    S.me.prefs = { ...(S.me.prefs || {}), [key]: on ? '1' : '0' };
    api.updateMe({ prefs: { [key]: on ? '1' : '0' } }).catch(err => toast(err.message));
    return;
  }
  if ((n = hit('data-sw'))) {
    n.setAttribute('aria-checked', String(n.getAttribute('aria-checked') !== 'true'));
    return;
  }
  if ((n = hit('data-textgroesse'))) {
    try { localStorage.setItem('lumen.textgroesse', n.dataset.textgroesse); } catch {}
    textgroesseAnwenden();
    return render();
  }
  if ((n = hit('data-darstellung'))) {
    try { localStorage.setItem('lumen.darstellung', n.dataset.darstellung); } catch {}
    darstellungAnwenden();
    return render();
  }
  if (hit('data-signout')) {
    if (!(await rueckfrage(abmeldeFrage(false)))) return;
    // Auch beim Server abmelden — sonst bliebe das Gerät dort eingetragen.
    await api.call('/me/device', { method: 'DELETE' }).catch(() => {});
    api.signOut(); S.me = null; abgemeldet(); return go('profiles');
  }
  if (hit('data-signoutall')) {
    if (!(await rueckfrage(abmeldeFrage(true)))) return;
    await api.signOutEverywhere().catch(() => {});
    api.signOut(); S.me = null; abgemeldet(); return go('profiles');
  }
  if (hit('data-clearhistory')) {
    if (!(await rueckfrage({ titel: tr('Verlauf löschen?'), text: tr('Alle Fortschritte und Haken, „Weiterschauen“ und die gemerkten Spuren je Serie verschwinden. Merkliste, Favoriten und Bewertungen bleiben, die Dateien auch.'), ja: tr('Löschen'), gefaehrlich: true }))) return;
    try { await api.clearHistory(); toast(tr('Verlauf gelöscht')); }
    catch (err) { toast(err.message); }
    return;
  }
  if ((n = hit('data-extra'))) return extraAbspielen(n.dataset.extra, n.dataset.extraTitel || '');

  /* Verwaltung */
  if ((n = hit('data-scan'))) {
    try { await api.adminScan(n.dataset.scan); } catch (err) { return toast(err.message); }
    // Kein Warten mehr ins Blaue: der Takt holt den Stand, und die Zeile
    // zeigt von selbst, wie weit es ist.
    await load();
    return;
  }

  if ((n = hit('data-titelneu'))) {
    toast(tr('Wird neu geholt …'));
    try { await api.adminItemReload(n.dataset.titelneu); } catch (err) { return toast(err.message); }
    return load();
  }
  if ((n = hit('data-zuweisen'))) return zuweisenPanel(n.dataset.zuweisen, S.data?.item?.title || '');
  if ((n = hit('data-zuo-suchen'))) return zuweisenPanel(n.dataset.zuoSuchen, n.dataset.titel || '');
  if ((n = hit('data-zuo-id'))) return zuweisenPanel(n.dataset.zuoId, '', 'id');
  if ((n = hit('data-kennung-merken'))) {
    const box = zuweisenBox(n.dataset.kennungMerken)?.querySelector('[data-treffer-box]');
    try { await api.adminItemKennung(n.dataset.kennungMerken, n.dataset.q || ''); }
    catch (err) { if (box) box.innerHTML = `<p class="hint" style="padding:12px 16px;margin:0">${esc(err.message)}</p>`; return; }
    toast(tr('Kennung gespeichert — Poster und Beschreibungen kommen, sobald ein gültiger TMDb-Schlüssel eingetragen ist.'));
    return load();
  }
  if ((n = hit('data-zuweisen-aus'))) { const b = zuweisenBox(n.dataset.zuweisenAus); if (b) b.innerHTML = ''; return; }
  if ((n = hit('data-treffer'))) {
    toast(tr('Wird zugewiesen …'));
    try { await api.adminItemAssign(n.dataset.item || S.id, n.dataset.treffer); } catch (err) { return toast(err.message); }
    toast(tr('Zugewiesen'));
    return load();
  }
  if ((n = hit('data-metaneu'))) {
    try {
      const r = await api.adminMetadataReload(n.dataset.metaneu);
      toast(tr('{titel} Titel werden neu geholt — das kann dauern', { titel: r.titel }));
    } catch (err) { toast(err.message); }
    return;
  }

  if (hit('data-updpruefen')) {
    toast(tr('Sieht bei GitHub nach …'));
    try { S.data = await api.adminUpdateCheck(); } catch (err) { return toast(err.message); }
    return render();
  }
  if (hit('data-updjetzt') || hit('data-updtrotzdem')) {
    const trotzdem = !!hit('data-updtrotzdem');
    try {
      const r = await api.adminUpdateRun(trotzdem);
      toast(tr('Lumen {nach} wird eingesetzt — gleich startet es neu', { nach: r.nach }));
      // Scheitert das Einsetzen (kein Paket, falsche Prüfsumme), startet
      // nichts neu — dann soll das nicht nur unten im Protokoll stehen.
      const seit = Date.now() - 5000;
      const nachsehen = async (rest) => {
        let u = null;
        try { u = await api.adminUpdate(); } catch { /* startet gerade neu */ }
        const letzter = u?.protokoll?.[0];
        if (letzter && letzter.ergebnis === 'fehler' && Date.parse(letzter.at) >= seit) {
          if (S.view === 'adminUpdate') { S.data = u; render(); }
          return toast(tr('Das Update liess sich nicht einsetzen: {grund}', { grund: letzter.notiz || '' }));
        }
        if (rest > 0 && (!u || u.laeuft)) return setTimeout(() => nachsehen(rest - 1), 2000);
        if (S.view === 'adminUpdate') load();
      };
      setTimeout(() => nachsehen(6), 2000);
    } catch (err) {
      if (err.wiedergaben) return updateFrage(err.wiedergaben);
      toast(err.message);
    }
    return;
  }
  if ((n = hit('data-avatar'))) {
    try { S.me = await api.updateMe({ avatar: n.dataset.avatar }); toast(tr('Profilbild geändert')); return render(); }
    catch (err) { return toast(err.message); }
  }
  if ((n = hit('data-prefwert'))) {
    const [key, wert] = n.dataset.prefwert.split(':');
    S.me.prefs = { ...(S.me.prefs || {}), [key]: wert };
    api.updateMe({ prefs: { [key]: wert } }).catch(err => toast(err.message));
    return render();
  }
  if ((n = hit('data-grpedit'))) { S.gruppe = (S.data?.groups || []).find(x => x.id === n.dataset.grpedit) || null; render(); document.getElementById('gname')?.focus(); return; }
  if (hit('data-grpneu')) { S.gruppe = null; return render(); }
  if ((n = hit('data-grpdel'))) {
    try { await api.adminDeleteGroup(n.dataset.grpdel); toast(tr('Gruppe aufgelöst — die Konten behalten ihre eigenen Einträge')); }
    catch (err) { return toast(err.message); }
    S.gruppe = null; return load();
  }
  if (hit('data-relock')) {
    try { await api.adminRelock(); } catch {}
    api.verwaltungsToken(null); toast(tr('Verwaltung gesperrt')); return go('home');
  }
  if (hit('data-totpqr')) {
    try {
      const q = await api.adminLockTOTP();
      const box = document.getElementById('totpbox');
      if (box) box.innerHTML = `<div style="display:flex;gap:18px;flex-wrap:wrap;align-items:flex-start">
        <img src="${q.qr}" alt="${tr('QR-Code für die Authenticator-App')}" width="180" height="180" style="border-radius:12px;background:#fff;padding:6px">
        <div style="flex:1;min-width:200px" class="field">
          <span class="t-caption c-2">${tr('Falls Scannen nicht geht, von Hand:')} <span class="mono" style="user-select:all">${esc(q.geheimnis)}</span></span>
          <label for="lcode" style="margin-top:10px">${tr('Code aus der App')}</label>
          <input id="lcode" name="code" inputmode="numeric" autocomplete="one-time-code" placeholder="123 456" required></div></div>`;
    } catch (err) { toast(err.message); }
    return;
  }
  if (hit('data-naechste-folge')) {
    const p = S.playing;
    if (!p) return;
    const f = await folgeDanach(p.itemId, p.parentId).catch(() => null);
    if (!f) return toast(tr('Das war die letzte Folge.'));
    return naechsterStarten(f.id);
  }
  if ((n = hit('data-pl-folge'))) { if (n.dataset.plFolge !== S.playing?.itemId) return naechsterStarten(n.dataset.plFolge); return; }
  if ((n = hit('data-pl-staffel'))) { if (S.playing) folgenLaden(S.playing, n.dataset.plStaffel); return; }
  if (hit('data-updnein')) { const b = document.getElementById('updfrage'); if (b) b.innerHTML = ''; return; }

  /* Bibliothek löschen — in zwei Schritten. Kein Browser-Dialog: der
     fragt mitten über der Seite und sagt nicht, um welche es geht. */
  if ((n = hit('data-del'))) { S.delLib = n.dataset.del; return bibMalen(); }
  if (hit('data-delno')) { S.delLib = null; return bibMalen(); }
  if ((n = hit('data-delok'))) {
    const id = n.dataset.delok;
    S.delLib = null;
    try { await api.adminDeleteLibrary(id); } catch (err) { S.delLib = null; bibMalen(); return toast(err.message); }
    toast(tr('Bibliothek gelöscht — die Dateien bleiben liegen'));
    return load();
  }

  /* Ordnerwähler */
  if ((n = hit('data-into')) || hit('data-browse')) {
    const pfad = n ? n.dataset.into : '';
    try { S.browse = await api.adminBrowse(pfad); }
    catch (err) { return toast(err.message); }
    bibMalen();
    // Der Fokus geht mit: auf den ersten Ordner, sonst auf den Pfad.
    const w = document.getElementById('bibwaehler');
    (w?.querySelector('[data-into]:not(.btn)') || w?.querySelector('#waehlerpfad'))?.focus();
    return;
  }
  if (hit('data-browseoff')) return waehlerZu();
  if ((n = hit('data-take'))) {
    const pfad = n.dataset.take;
    const feld = $('#lpath'), name = $('#lname');
    if (feld) feld.value = pfad;
    // Den Namen vorschlagen, aber nichts überschreiben, was schon dasteht.
    if (name && !name.value.trim()) name.value = pfad.split(/[\\/]/).filter(Boolean).pop() || '';
    S.browse = null;
    bibMalen();
    feld?.focus();
    return;
  }
  if ((n = hit('data-meinstop'))) {
    await api.stopMyPlayback(n.dataset.meinstop).catch(err => toast(err.message));
    toast(tr('Wiedergabe beendet'));
    return load();
  }
  if ((n = hit('data-kill'))) {
    await api.adminStopSession(n.dataset.kill).catch(err => toast(err.message));
    toast(tr('Wiedergabe beendet'));
    return load();
  }
  if ((n = hit('data-fassung'))) {
    const [item, datei] = n.dataset.fassung.split(':');
    try { await api.call(`/admin/items/${encodeURIComponent(item)}/fassung`, { method: 'PUT', body: { fileId: datei } }); toast(tr('Fassung gewählt')); }
    catch (err) { toast(err.message); }
    return load();
  }
  if ((n = hit('data-fehlend-weg')) || (n = hit('data-fehlende-weg-alle'))) {
    const ids = n.dataset.fehlendWeg ? [n.dataset.fehlendWeg] : [];
    try { await api.call('/admin/cleanup/fehlende-entfernen', { method: 'POST', body: { fileIds: ids } }); toast(tr('Endgültig entfernt')); }
    catch (err) { toast(err.message); }
    return load();
  }
  if ((n = hit('data-abtrennen'))) {
    try { await api.call(`/admin/files/${encodeURIComponent(n.dataset.abtrennen)}/abtrennen`, { method: 'POST' }); toast(tr('Als eigener Titel geführt')); }
    catch (err) { toast(err.message); }
    return load();
  }
  if ((n = hit('data-teile'))) {
    try { const r = await api.call(`/admin/items/${encodeURIComponent(n.dataset.teile)}/teile-zusammenfuehren`, { method: 'POST' }); toast(r?.status || tr('Läuft …')); }
    catch (err) { toast(err.message); }
    return;
  }
  if (hit('data-tokenweg')) {
    try { S.data = await api.adminUpdateSettings({ ...(S.data?.einstellung || {}), token: '' }); toast(tr('Token entfernt')); return render(); }
    catch (err) { return toast(err.message); }
  }
  if (hit('data-hwpruefen')) {
    try { await api.adminUmwandlungPruefen(); toast(tr('Die Hardware wird geprüft …')); } catch (err) { return toast(err.message); }
    setTimeout(load, 6000);
    return;
  }
  if ((n = hit('data-einladung-weg'))) {
    if (!(await rueckfrage({ titel: tr('Einladung zurückziehen?'), text: tr('Wer den Link oder den Code hat, kann damit kein Profil mehr anlegen.'), ja: tr('Zurückziehen'), gefaehrlich: true }))) return;
    try { await api.adminInviteZurueckziehen(n.dataset.einladungWeg); toast(tr('Einladung zurückgezogen')); }
    catch (err) { toast(err.message); }
    return load();
  }
  if (hit('data-sichern')) {
    // Es gibt nur zwei Sicherungen. „Jetzt sichern" schiebt die ältere
    // hinaus — das soll niemand aus Versehen tun, etwa direkt nach einem
    // Fehler, wenn gerade die ältere die gute ist.
    const alle = S.data?.sicherungen || [];
    if (alle.length >= 2) {
      const davor = alle.find(b => b.nr === 2), neueste = alle.find(b => b.nr === 1);
      const ok = await rueckfrage({ titel: tr('Jetzt sichern?'),
        text: tr('Lumen behält nur zwei Sicherungen. Die Sicherung von {davor} wird dabei gelöscht; die von {neueste} wird zur älteren. Willst du zu einem früheren Stand zurück, stelle ihn zuerst wieder her.',
          { davor: sicherungWann(davor), neueste: sicherungWann(neueste) }),
        ja: tr('Sichern') });
      if (!ok) return;
    }
    try { await api.adminBackupNow(); toast(tr('Gesichert')); } catch (err) { toast(err.message); }
    return load();
  }
  if ((n = hit('data-wiederherstellen'))) {
    const b = (S.data?.sicherungen || []).find(x => String(x.nr) === n.dataset.wiederherstellen);
    const wann = sicherungWann(b);
    const ok = await rueckfrage({ titel: tr('Stand von {wann} wiederherstellen?', { wann }),
      text: tr('Alles, was seither geändert wurde – neue Profile, Einladungen, Einstellungen, Bewertungen und wer wo im Film stand –, geht verloren. Lumen startet dafür neu; laufende Wiedergaben brechen ab.'),
      ja: tr('Wiederherstellen'), gefaehrlich: true });
    if (!ok) return;
    try {
      await api.adminRestore(n.dataset.wiederherstellen);
    } catch (err) { return toast(err.message); }
    neustartAbwarten(tr('Lumen startet neu und nimmt die Sicherung von {wann}.', { wann }),
      tr('Die Sicherung von {wann} ist wiederhergestellt.', { wann }));
    return;
  }
  if ((n = hit('data-logf'))) { S.logFilter = n.dataset.logf; return load(); }
  if (hit('data-resolve')) {
    await api.adminResolve().catch(err => toast(err.message));
    toast(tr('Metadaten werden geholt …'));
    setTimeout(load, 4000);
    return;
  }
});

function seekTo(sec) {
  const p = S.playing, v = $('#v');
  if (!p || !v) return;
  sec = Math.max(0, Math.min(sec, (p.duration || sec) - 1));

  // Liegt die Stelle in dem, was schon da ist, reicht ein Sprung im Video.
  // Sonst muss der Server neu ansetzen.
  //
  // Massgeblich ist buffered — also das, was wirklich da ist.
  //
  // Weder duration noch seekable taugen: ein laufender Strom hat keine
  // Länge, und der Browser meldet für beide gern Unendlich. Danach zu
  // gehen hiesse, jeden Sprung nach vorn für erreichbar zu halten — und
  // der Film bliebe stehen, wo er war.
  const end = v.buffered && v.buffered.length
    ? v.buffered.end(v.buffered.length - 1) : 0;
  const inStream = sec >= p.offset && sec <= p.offset + end;

  if (inStream) {
    v.currentTime = sec - p.offset;
    p.current = v.currentTime;
    patchPlayer();
    showChrome();
  } else {
    // Ein neuer Strom meldet kein „seeked" — die neue Stelle darum hier sichern.
    openStream(sec).then(() => standSichern());
  }
}

/* ---------------------------------------------------------------- Formulare */

// Das Wunschfeld mit Vorschlägen von TMDb (wuensche.js) hat seine eigenen
// Ereignisse — vor denen hier, damit Enter in der Liste wählt statt abschickt.
const wunschVerhalten = wunschFeldEinrichten(app, {
  async gewuenscht(w, dabei) {
    toast(dabei ? tr('Schon gewünscht – du bist jetzt mit dabei') : tr('Wunsch an den Besitzer geschickt'));
    if (S.view === 'wishes') await load();
  },
  ansehen: id => go('detail', { id }),
  fehler: text => toast(text),
});

// Jedes Formular ist gesperrt, solange seine Anfrage läuft: ein
// Doppelklick auf „Wünschen" oder „Anlegen" schickte sonst alles zweimal.
app.addEventListener('submit', e => {
  e.preventDefault();
  if (e.target.id === 'searchform') return formularAbschicken(e);
  formularGesperrt(e.target, () => formularAbschicken(e));
});
async function formularAbschicken(e) {
  const f = e.target;

  if (f.id === 'umwandlungform') {
    const an = name => f.querySelector(`[data-feld="${name}"]`)?.getAttribute('aria-checked') === 'true';
    const e = { trickplay: an('trickplay'), untertitelVorbereiten: an('untertitel'), vorspannErkennen: an('vorspann'),
      vorspannNurNachts: an('vorspann-nachts') };
    if (!f.hardware.disabled) e.hardware = f.hardware.value;
    try { S.data = await api.adminUmwandlungSetzen(e); toast(tr('Gespeichert')); return render(); }
    catch (err) { return toast(err.message); }
  }
  if (f.id === 'servernameform') {
    try {
      const r = await api.call('/admin/server-name', { method: 'PUT', body: { name: f.name.value } });
      if (S.hello) S.hello.name = r.name;
      return toast(r.name ? tr('Der Server heisst jetzt „{name}“', { name: r.name }) : tr('Name entfernt'));
    } catch (err) { return toast(err.message); }
  }
  if (f.id === 'codeform') {
    try {
      // Erst nachsehen, welches Gerät fragt. Kommt die Anfrage von
      // unterwegs, ausdrücklich nachfragen — „gib mir mal den Code" ist
      // ein bekannter Trick, um an ein fremdes Profil zu kommen.
      const info = await api.call('/code/approve', { method: 'POST', body: { code: f.code.value, pruefen: true } });
      if (info.unterwegs && !await frage(tr('Anmeldung von unterwegs'), tr(T.unterwegsFrage, { geraet: info.geraet }), tr('Trotzdem freigeben'))) return;
      const r = await api.call('/code/approve', { method: 'POST', body: { code: f.code.value } });
      f.code.value = '';
      return toast(tr('{geraet} ist angemeldet', { geraet: r.geraet }));
    } catch (err) { return toast(err.message); }
  }
  if (f.id === 'inviteform') {
    const fd = new FormData(f);
    const kind = fd.get('art') === 'kind';
    try {
      const inv = await api.adminInvite(fd.getAll('lib'), {}, 7,
        kind ? { kinderprofil: true, altersgrenze: Number(fd.get('alter')) } : {});
      const url = `${location.origin}/?einladung=${encodeURIComponent(inv.code)}`;
      const box = document.getElementById('invitebox');
      if (box) box.innerHTML = einladungsBox(url, inv.code);
      // Die Liste der offenen Einladungen nachführen, ohne das Formular neu zu zeichnen.
      api.adminInvites().then(liste => {
        if (S.view !== 'accounts' || !S.data) return;
        S.data.einladungen = liste;
        const alt = document.getElementById('offene-einladungen');
        const html = offeneEinladungen(liste);
        if (alt) alt.outerHTML = html; else box?.closest('section')?.insertAdjacentHTML('afterend', html);
      }).catch(() => {});
      return toast(kind ? tr('Einladung für ein Kinderprofil erzeugt') : tr('Einladung erzeugt'));
    } catch (err) { return toast(err.message); }
  }
  if (f.id === 'adminunlock') {
    try {
      const r = await api.adminUnlock(f.code.value.trim());
      api.verwaltungsToken(r.token); S.sperreFehler = '';
      const z = S.sperre || {}; S.sperre = null;
      return go(z.zurueck || 'admin', { id: z.id, ersetzen: true });
    } catch (err) { S.sperreFehler = err.message; return render(); }
  }
  if (f.id === 'lockform') {
    const kind = f.kind.value;
    const body = { kind, secret: f.secret?.value || '', code: f.code?.value || '' };
    if (kind === 'totp' && !f.code) return toast(tr('Zuerst den QR-Code anzeigen und scannen'));
    try {
      const r = await api.adminSetLock(body);
      if (r.token) api.verwaltungsToken(r.token);
      S.me = await api.me();
      toast(kind === 'none' ? tr('Keine Sperre mehr') : `${tr('Sperre gesetzt:')} ` + sperreName(kind));
      return load();
    } catch (err) { return toast(err.message); }
  }
  if (f.id === 'grpform') {
    const fd = new FormData(f);
    const recht = fd.get('recht');
    const g = { name: fd.get('name'), verwaltung: recht === 'verwalten' || recht === 'voll', vollzugriff: recht === 'voll', libraries: fd.getAll('lib'),
      limits: { maxStreams: Number(fd.get('maxStreams')) || 2, maxAge: Number(fd.get('maxAge')), maxDownloads: Number(fd.get('maxDownloads')) || 0,
        allowUnrated: !!fd.get('allowUnrated'), allowTranscode: !!fd.get('allowTranscode'), allowDownload: !!fd.get('allowDownload'), remoteQuality: '1080p' } };
    // Eine Gruppe ohne Bibliothek: Wer darin ist, sieht gar nichts –
    // früher ohne jeden Hinweis (ein Kind stand dann vor „Noch nichts da").
    if (!g.libraries.length && !(await rueckfrage({ titel: tr('Keine Bibliothek gewählt'),
      text: tr('Wer in dieser Gruppe ist, sieht dann gar nichts. Trotzdem speichern?'), ja: tr('Speichern') }))) return;
    try {
      if (fd.get('gid')) await api.adminUpdateGroup(fd.get('gid'), g); else await api.adminCreateGroup(g);
      toast(tr('Gespeichert')); S.gruppe = null; return load();
    } catch (err) { return toast(err.message); }
  }
  if (f.id === 'updform') {
    const e = Object.fromEntries(new FormData(f).entries());
    if (!e.token) delete e.token;
    try { S.data = await api.adminUpdateSettings(e); toast(tr('Gespeichert')); return render(); }
    catch (err) { return toast(err.message); }
  }

  if (f.id === 'setupsprache') {
    S.setupSpracheFertig = true;
    await sprachWechseln(f.sprache.value);
    return render();
  }
  if (f.id === 'setupform') {
    const name = f.name.value.trim(), pass = f.password.value;
    try {
      await api.setup(name, pass, tr('Browser'), sprache());
      S.me = await api.me();
      return go('adminLibraries');
    } catch (err) { S.error = err.message; return render(); }
  }

  if (f.id === 'redeemform') {
    const art = S.redeemLock;
    try {
      await api.redeemInvite(f.code.value.trim(), f.name.value.trim(), art,
        art === 'none' ? '' : f.secret.value);
      S.me = await api.me();
      S.redeemForm = {};
      // Den Code aus der Adresszeile nehmen: er ist verbraucht, und beim
      // nächsten Laden soll nicht wieder das Einladungsformular kommen.
      try { history.replaceState(null, '', location.pathname); } catch {}
      return go('home');
    } catch (err) {
      S.redeemForm = { code: f.code.value, name: f.name.value };
      S.error = err.message;
      return render();
    }
  }

  if (f.id === 'unterwegsform') {
    const name = f.uname.value.trim(), geheim = f.usecret.value;
    try {
      await api.unlockName(name, geheim);
      S.me = await api.me();
      go('home');
      return anmeldeHinweis();
    } catch (err) {
      if (err.code === 'neue_pin_noetig') return eigenePinFestlegen(err.message, neu => api.unlockName(name, geheim, neu), err.art);
      S.error = err.code === 'falsch' ? falschMeldung(err) : err.message; return render();
    }
  }
  if (f.id === 'unlockform') {
    const id = S.id, geheim = f.secret.value;
    try {
      await api.unlock(id, geheim);
      S.me = await api.me();
      go('home');
      return anmeldeHinweis();
    } catch (err) {
      if (err.code === 'neue_pin_noetig') return eigenePinFestlegen(err.message, neu => api.unlock(id, geheim, neu), err.art);
      S.error = err.code === 'falsch' ? falschMeldung(err) : err.message; return render();
    }
  }

  if (f.id === 'searchform') { $('#q')?.blur(); S.suchTastaturZu = true; return load(); }

  if (f.id === 'libform') {
    try {
      const lib = await api.adminAddLibrary(f.name.value.trim(), f.kind.value, f.path.value.trim());
      await api.adminScan(lib.id);
      f.reset();
      toast(tr('Angelegt — wird jetzt eingelesen'));
      return load();
    } catch (err) { return toast(err.message); }
  }

  if (f.id === 'reiheform') {
    try { await api.adminSetCollection(S.id, { name: f.name.value }); }
    catch (err) { return toast(err.message); }
    S.bearbeiten = null;
    return load();
  }
  if (f.id === 'listeform') {
    const name = f.name.value.trim();
    if (!name) return;
    try {
      const l = await api.createPlaylist(name);
      return go('playlist', { id: l.id });
    } catch (err) { return toast(err.message); }
  }
  if (f.id === 'listenameform') {
    try { await api.updatePlaylist(S.id, { name: f.name.value }); }
    catch (err) { return toast(err.message); }
    S.bearbeiten = null;
    return load();
  }
  if (f.matches('[data-zuweisenform]')) {
    return zuweisenSuchen(f.dataset.item, f.q.value.trim(), f.year ? f.year.value.trim() : '');
  }
  if (f.id === 'neuelisteform') {
    const name = f.name.value.trim();
    if (!name) return;
    try {
      const l = await api.createPlaylist(name);
      await api.addToPlaylist(l.id, S.id);
      toast(tr('In „{name}“ gelegt', { name }));
    } catch (err) { return toast(err.message); }
    return listenPanel(S.id);
  }

  if (f.id === 'metaform') {
    const patch = { language: f.language.value, region: f.region.value, nfoBevorzugen: f.nfoBevorzugen.checked };
    if (f.tmdbKey.value.trim()) patch.tmdbKey = f.tmdbKey.value.trim();
    if (f.omdbKey.value.trim()) patch.omdbKey = f.omdbKey.value.trim();
    try { await api.adminSetMetadata(patch); toast(tr('Gespeichert')); return load(); }
    catch (err) { return toast(err.message); }
  }
}

/* Auswahlfelder: das Intervall einer Bibliothek. */
app.addEventListener('change', async e => {
  const z = e.target;
  if (z.matches && z.matches('input[data-avatarbild]') && z.files?.[0]) {
    toast(tr('Bild wird hochgeladen …'));
    try { S.me = await api.uploadAvatar(z.files[0]); S.avatarStand = Date.now(); toast(tr('Profilbild gesetzt')); return render(); }
    catch (err) { return toast(err.message); }
  }
  // Einrichtung: die Wahl gleich zeigen – die Seite spricht sofort so.
  if (z.matches && z.matches('input[data-setupsprache]')) {
    S.setupSprache = z.value;
    await sprachWechseln(z.value);
    return;
  }
  if (z.matches && z.matches('select[data-sortwahl]')) {
    // Eine neue Sortierung beginnt in ihrer üblichen Richtung.
    return sortierungWaehlen(S.view, z.value, SORTIER_VORGABE[z.value] || 'asc');
  }
  if (z.matches && z.matches('select[data-prefsprache]')) {
    try {
      S.me = await api.updateMe({ prefs: { sprache: z.value } });
      await sprachWechseln(S.me.sprache || S.hello?.sprache);
      toast(tr('Sprache geändert'));
      return render();
    } catch (err) { return toast(err.message); }
  }
  if (z.matches && z.matches('select[data-serversprache]')) {
    try {
      const r = await api.call('/admin/sprache', { method: 'PUT', body: { sprache: z.value } });
      if (S.hello) { S.hello.sprache = r.sprache; S.hello.sprachen = r.sprachen || S.hello.sprachen; }
      S.me = await api.me();
      await sprachWechseln(S.me.sprache || r.sprache);
      toast(tr('Sprache des Servers: {name}', { name: sprachNameVon(r.sprache) }));
      return render();
    } catch (err) { return toast(err.message); }
  }
  if (z.matches && z.matches('select[data-rolle]')) {
    try {
      await api.call(`/admin/accounts/${encodeURIComponent(z.dataset.rolle)}/rolle`, { method: 'PUT', body: { rolle: z.value } });
      toast({ schauen: tr('Nur noch schauen'), verwalten: tr('Verwaltet jetzt mit'), voll: tr('Hat jetzt Vollzugriff') }[z.value] || tr('Gespeichert'));
    } catch (err) { toast(err.message); }
    return load();
  }
  if (z.matches && z.matches('select[data-grpacc]')) {
    // Eine Gruppe ohne Bibliothek nimmt der Person alles: vorher fragen.
    const ziel = (S.data?.groups || []).find(x => x.id === z.value);
    if (ziel && !(ziel.libraries || []).length && !(await rueckfrage({ titel: tr('Gruppe ohne Bibliothek'),
      text: tr('„{gruppe}“ sieht keine Bibliothek – wer darin ist, sieht danach nichts mehr. Trotzdem zuordnen?', { gruppe: ziel.name }), ja: tr('Zuordnen') }))) return load();
    try { await api.adminSetGroup(z.dataset.grpacc, z.value); toast(z.value ? tr('Zugeordnet') : tr('Aus der Gruppe genommen')); return load(); }
    // Abgelehnt (etwa: Kinderprofil in eine Gruppe mit Verwaltung): die
    // Auswahl springt zurück, statt etwas anzuzeigen, das nicht gilt.
    catch (err) { toast(err.message); return load(); }
  }
  if (z.matches && z.matches('input[name=kind]') && z.form?.id === 'lockform') {
    const box = document.getElementById('lockdetail'); if (box) box.innerHTML = lockDetail(z.value); return;
  }
  if (z.matches && (z.matches('select[data-takt]') || z.matches('input[data-taktzeit]'))) {
    const id = z.dataset.takt || z.dataset.taktzeit;
    const wahl = z.closest('label').querySelector('select[data-takt]');
    const uhr = z.closest('label').querySelector('input[data-taktzeit]');
    const fest = wahl.value === 'zeit';
    uhr.style.display = fest ? '' : 'none';
    try {
      await api.adminSetLibrary(id, { intervalMin: fest ? 1440 : Number(wahl.value), scanAt: fest ? uhr.value : '' });
      toast(fest ? tr('Lumen sieht täglich um {value} nach', { value: uhr.value }) : Number(wahl.value) ? tr('Lumen sieht von selbst nach') : tr('Nur noch auf Knopfdruck'));
    } catch (err) { toast(err.message); }
  }
});

let searchTimer = null;
app.addEventListener('input', e => {
  if (e.target.id !== 'q') return;
  S.query = e.target.value;
  try { history.replaceState({ ...(history.state || {}), view: 'search', query: S.query }, '', adresseFuer('search', null, S.query)); } catch {}
  clearTimeout(searchTimer);
  searchTimer = setTimeout(load, 220);
});

// Nur die Maus: Ein Tipp auf dem Handy schickt vor dem Klick ein
// nachgemachtes mousemove. Das holte die Leiste hervor, und der Klick
// danach hielt den Film an — der erste Tipp sollte nur die Leiste zeigen.
app.addEventListener('pointermove', e => { if (S.view === 'player' && e.pointerType === 'mouse') showChrome(); });
// Fokus in der Leiste (per Tab) holt sie hervor.
app.addEventListener('focusin', e => {
  if (S.view === 'player' && e.target instanceof Element && e.target.closest('.player')) showChrome();
});
// Jede echte Eingabe setzt „Schaust du noch?" zurück; blosses Mausbewegen nicht.
document.addEventListener('pointerdown', e => { if (e.isTrusted) eingabeGesehen(); }, true);
app.addEventListener('input', e => {
  const z = e.target;
  if (!(z instanceof HTMLInputElement) || !z.matches('[data-laut]')) return;
  const v = $('#v');
  if (!v) return;
  v.volume = Number(z.value);
  v.muted = v.volume === 0;
});

/* Tippt die Person gerade in ein Feld?
 *
 * Ohne diese Frage greift jedes Tastenkürzel überall — und „/" ist dann
 * nicht mehr zu tippen, sondern springt in die Suche. Wer eine Bibliothek
 * anlegen will, kann ihren Pfad nicht eingeben, denn ein Pfad fängt mit
 * genau diesem Zeichen an. Die Leertaste und die Pfeiltasten hätten
 * dasselbe Problem. */
function tipptGerade(e) {
  const z = e.target;
  if (!z || !z.tagName) return false;
  if (z.isContentEditable) return true;
  return z.tagName === 'INPUT' || z.tagName === 'TEXTAREA' || z.tagName === 'SELECT';
}

document.addEventListener('keydown', e => {
  // Ein offenes Fenster (Bearbeiten, Untertitel …) hat die Tasten für sich:
  // Esc schliesst das Fenster, nicht den Film.
  if (document.querySelector('dialog[open]')) return;
  // Beim Tippen gehört jede Taste dem Feld, keine dem Programm. Einzige
  // Ausnahme: Escape gibt das Feld wieder frei.
  if (tipptGerade(e)) {
    if (e.key === 'Escape') e.target.blur();
    return;
  }

  if (S.view === 'player') {
    if (e.isTrusted) eingabeGesehen();
    showChrome();
    return playerTaste(e);
  } else if (e.key === 'Escape' && S.view === 'detail') {
    escAufTitelseite();
  } else if (e.key === 'Escape') {
    if (S.view === 'adminLibraries' && waehlerZu()) return;
    mehrMenueZu(true);
  } else if (e.key === '/' && S.me && S.view !== 'search') {
    e.preventDefault(); go('search');
  }
});

/* Tastenkürzel im Player — wie man sie von YouTube und Netflix kennt:
   Leertaste/K Pause, ←/→ und J/L zehn Sekunden, ↑/↓ Lautstärke, F
   Vollbild, M stumm, N nächste Folge, S Vorspann überspringen, C
   Untertitel, Esc zurück. Steht der Fokus auf einem Knopf, gehören
   Leertaste und Enter dem Knopf. */
function playerTaste(e) {
  const v = $('#v'), p = S.playing;
  if (!p || e.ctrlKey || e.metaKey || e.altKey) return;
  const fokus = e.target instanceof Element ? e.target : null;
  if (document.getElementById('nochda')) {
    if (e.key === 'Escape') { e.preventDefault(); stopPlayer(); }
    return; // die Rückfrage hat ihre eigenen Knöpfe
  }
  if (e.key === 'Escape') {
    e.preventDefault();
    // Erst ein offenes Menü schliessen, dann den Player.
    if (p.spurenOffen) { p.spurenOffen = false; p.spurenHtml = ''; patchChrome(); app.querySelector('[data-spuren]')?.focus(); return; }
    return stopPlayer();
  }
  if (fokus?.matches('[data-bar]') && zeitleisteTaste(e)) return;
  const aufKnopf = fokus && fokus.closest('button, [role="menuitemradio"]');
  if ((e.key === ' ' || e.key === 'Enter') && aufKnopf) return;
  // Im offenen Menü bewegen die Pfeile den Fokus, nicht den Film.
  if (fokus?.closest('.spuren') && e.key.startsWith('Arrow')) {
    const knoepfe = [...app.querySelectorAll('.spuren button')];
    const i = knoepfe.indexOf(fokus.closest('button'));
    if (i >= 0 && (e.key === 'ArrowDown' || e.key === 'ArrowUp')) {
      e.preventDefault();
      knoepfe[(i + (e.key === 'ArrowDown' ? 1 : knoepfe.length - 1)) % knoepfe.length]?.focus();
    }
    return;
  }
  const taste = e.key.length === 1 ? e.key.toLowerCase() : e.key;
  const jetzt = () => p.offset + (v ? v.currentTime : p.current);
  switch (taste) {
    case ' ': case 'k':
      e.preventDefault();
      if (v) v.paused ? v.play().catch(() => {}) : v.pause();
      return;
    case 'ArrowLeft': case 'j':
      e.preventDefault(); return seekTo(jetzt() - 10);
    case 'ArrowRight': case 'l':
      e.preventDefault(); return seekTo(jetzt() + 10);
    case 'ArrowUp':
      e.preventDefault(); return lautstaerkeAendern(0.05);
    case 'ArrowDown':
      e.preventDefault(); return lautstaerkeAendern(-0.05);
    case 'f':
      e.preventDefault(); return vollbildUmschalten();
    case 'p':
      if (!bildImBildDa()) return;
      e.preventDefault(); return bildImBildUmschalten();
    case 'm':
      e.preventDefault(); stummUmschalten(); return toast($('#v')?.muted ? tr('Stumm') : tr('Ton an'));
    case 'n':
      if (p.kind !== 'episode' && !S.warteschlange) return;
      e.preventDefault();
      app.querySelector('[data-naechste-folge]')?.click();
      if (S.warteschlange) { const box = document.getElementById('naechster'); if (box?.dataset.id) naechsterStarten(box.dataset.id); }
      return;
    case 's':
      if (document.getElementById('vorspann')) { e.preventDefault(); vorspannUeberspringen(); }
      return;
    case 'c':
      e.preventDefault(); return untertitelUmschalten();
  }
}

/* Beim Weggehen den Stand sichern — sonst beginnt der Film beim nächsten
   Mal wieder dort, wo zuletzt gespeichert wurde. pagehide für Tab zu und
   Wegnavigieren, visibilitychange für „App in den Hintergrund" auf dem
   Handy (dort kommt pagehide oft gar nicht mehr). */
addEventListener('pagehide', standImHintergrund);
document.addEventListener('visibilitychange', () => {
  if (document.visibilityState === 'hidden') standImHintergrund();
});

/* ---------------------------------------------------------------- Adressen, Verlauf, Fokus
 *
 * Alles in diesem Abschnitt hält die Web-App auf dem Boden: Jede Ansicht
 * hat eine echte Adresse (F5 und Lesezeichen führen zurück), „Zurück"
 * landet an derselben Stelle der Liste, der Fokus springt nach einem
 * Seitenwechsel auf die Überschrift statt auf <body>, und eine Anfrage,
 * die zu spät kommt, überschreibt nichts mehr. */

let ladeNummer = 0;            // die jüngste Anfrage von load()
let aktSchluessel = null;      // Kennung des Verlaufseintrags, auf dem man steht
let letzteAnsicht = '';        // um einen Seitenwechsel von einem Neuzeichnen zu unterscheiden
let letzteKarte = null;        // zuletzt angeklickte Karte — Safari fokussiert Knöpfe beim Klick nicht
let letzterFehlerText = '';    // die letzte Fehlermeldung des Servers (für role="alert")
// Stand je Verlaufseintrag (Scrollstelle, Filter) und — für Raster, die
// seitenweise nachladen — die schon geholten Daten. Ohne die Daten stünde
// man nach „Zurück" an Stelle 3000 einer Liste, die nur 60 Titel hat.
const verlaufsStand = new Map();
const datenSpeicher = new Map();
const SPEICHER_ANSICHTEN = ['movies', 'series', 'search', 'person', 'collections', 'collection'];

const wenigBewegung = () => { try { return matchMedia('(prefers-reduced-motion: reduce)').matches; } catch { return false; } };

/* Ein Gerät nur mit Finger: Handy, iPad, Tablet — auch quer. Entschieden
   wird nach der Eingabeart, nicht nach der Breite: ein iPad quer ist
   breiter als mancher Laptop. iPadOS gibt sich als Mac aus; einen Mac mit
   Berührungsbildschirm gibt es aber nicht. */
function nurBeruehrung() {
  try { if (matchMedia('(pointer: coarse) and (hover: none)').matches) return true; } catch {}
  return /Macintosh/.test(navigator.userAgent) && navigator.maxTouchPoints > 1;
}
// Die Verwaltung gibt es im Browser nur am Computer (und in der Mac-App).
const verwaltungHier = () => !!S.me?.account?.isAdmin && !nurBeruehrung();

const ADRESSEN = [
  ['home', 'startseite'], ['movies', 'filme'], ['series', 'serien'], ['watchlist', 'merkliste'],
  ['gesehen', 'gesehen'], ['favorites', 'favoriten'], ['collections', 'filmreihen'], ['collection', 'filmreihe'],
  ['playlists', 'listen'], ['playlist', 'liste'], ['wishes', 'wuensche'], ['news', 'neuigkeiten'],
  ['search', 'suche'], ['person', 'person'], ['detail', 'titel'], ['settings', 'ich'],
  ['admin', 'verwaltung'], ['adminLibraries', 'verwaltung/bibliotheken'], ['adminWishes', 'verwaltung/wuensche'],
  ['adminUpdate', 'verwaltung/updates'], ['adminBackups', 'verwaltung/sicherung'], ['adminGroups', 'verwaltung/gruppen'],
  ['adminLock', 'verwaltung/sperre'], ['sessions', 'verwaltung/wiedergaben'], ['logs', 'verwaltung/protokoll'],
  ['cleanup', 'verwaltung/aufraeumen'], ['accounts', 'verwaltung/konten'], ['metadata', 'verwaltung/metadaten'],
  ['adminUmwandlung', 'verwaltung/umwandeln'],
  ['sammlungen', 'sammlungen'], ['sammlung', 'sammlung'],
];
// Ansichten ohne eigene Adresse: Anmelden, Einrichten, Player und
// Zwischenschritte, die nur aus einer anderen Ansicht heraus Sinn haben.
// tools/adressen_pruefen.py hält ADRESSEN und render() deckungsgleich.
const OHNE_ADRESSE = ['start', 'setup', 'redeem', 'profiles', 'unlock', 'player', 'adminUnlock', 'amComputer', 'schonDabei'];
const MIT_KENNUNG = ['detail', 'collection', 'playlist', 'person', 'sammlung'];

// Die Adresse einer Ansicht, etwa „#/titel/abc" oder „#/suche?q=sonne".
// Hash statt Pfad: der Server muss dafür nichts wissen, und ein Lesezeichen
// funktioniert auch hinter einem Vermittler.
function adresseFuer(view, id, query) {
  const e = ADRESSEN.find(([v]) => v === view);
  if (!e) return null;
  let a = '#/' + e[1];
  if (id && MIT_KENNUNG.includes(view)) a += '/' + encodeURIComponent(id);
  if (view === 'search' && query) a += '?q=' + encodeURIComponent(query);
  return a;
}

function ansichtAusAdresse(hash = location.hash) {
  const m = /^#\/([^?]*)(?:\?(.*))?$/.exec(hash || '');
  if (!m) return null;
  const teile = m[1].split('/').filter(Boolean);
  // Längere Pfade zuerst: „verwaltung/konten" vor „verwaltung".
  for (const [view, pfad] of [...ADRESSEN].sort((a, b) => b[1].length - a[1].length)) {
    const p = pfad.split('/');
    if (!p.every((x, i) => teile[i] === x)) continue;
    const rest = teile.slice(p.length);
    if (MIT_KENNUNG.includes(view) && !rest.length) return null;
    if (!MIT_KENNUNG.includes(view) && rest.length) continue;
    let id = null;
    try { id = rest.length ? decodeURIComponent(rest.join('/')) : null; } catch { return null; }
    // Kennungen aus der Adresse landen in API-Pfaden. Nur, was Lumen selbst
    // vergibt ([A-Za-z0-9_-]); „../admin/…" oder ein „?" in einem
    // untergeschobenen Link führt so nirgendwohin. Personen tragen ihren
    // Namen in der Adresse – der wird beim Abrufen kodiert (api.js).
    if (id != null && view !== 'person' && !/^[A-Za-z0-9_-]{1,64}$/.test(id)) return null;
    const query = view === 'search' ? (new URLSearchParams(m[2] || '').get('q') || '') : null;
    return { view, id, query };
  }
  return null;
}

let schluesselZaehler = 0;
const neuerSchluessel = () => `${Date.now().toString(36)}-${++schluesselZaehler}`;

/* Stand der aktuellen Seite merken: Scrollstelle, waagerechte Reihen,
   zuletzt gewählte Karte, Filter. inVerlauf schreibt ihn zusätzlich in
   den Eintrag des Browsers — dann übersteht er auch ein Neuladen. Nicht
   bei jedem Scrollen: Safari erlaubt nur wenige replaceState pro Sekunde. */
function standMerken(inVerlauf = true) {
  if (!aktSchluessel || !S.me || S.view === 'player') return;
  const seite = app.querySelector('.page');
  const stand = {
    scroll: seite ? Math.round(seite.scrollTop) : 0,
    reihen: [...app.querySelectorAll('.rail')].map(r => Math.round(r.scrollLeft)),
    fokusId: letzteKarte || verlaufsStand.get(aktSchluessel)?.fokusId || null,
    filter: S.filter, buchstabe: S.buchstabe, favArt: S.favArt,
  };
  verlaufsStand.set(aktSchluessel, stand);
  if (inVerlauf) {
    try { if (history.state?.schluessel === aktSchluessel) history.replaceState({ ...history.state, ...stand }, ''); } catch {}
  }
  // Nur die jüngsten Einträge behalten — ein langer Abend soll den
  // Speicher nicht füllen.
  for (const m of [verlaufsStand, datenSpeicher]) {
    while (m.size > 30) m.delete(m.keys().next().value);
  }
}

let standZeit = null;
app.addEventListener('scroll', () => {
  clearTimeout(standZeit);
  standZeit = setTimeout(() => standMerken(false), 150);
}, { capture: true, passive: true });
app.addEventListener('click', e => {
  const k = e.target instanceof Element && e.target.closest('[data-go][data-id]');
  if (k) letzteKarte = k.dataset.id;
}, true);
addEventListener('pagehide', () => standMerken(true));

// Ein einziger Lauscher für alle Reihenpfeile (siehe reihenPfeile).
addEventListener('resize', () => {
  for (const h of app.querySelectorAll('.rail-huelle')) h.pfeilStand?.();
}, { passive: true });

/* Wie man das gerade fokussierte Element nach dem Neuzeichnen wiederfindet:
   über seine Kennung oder sein erstes data-Attribut — erst mit Wert, dann
   ohne (der Wert von „Merken" wechselt beim Drücken von on zu off). */
function fokusSchluessel() {
  const a = document.activeElement;
  if (!a || a === document.body || !app.contains(a)) return null;
  const q = v => String(v).replace(/["\\]/g, '\\$&');
  const out = [];
  if (a.id) out.push('#' + CSS.escape(a.id));
  for (const at of a.attributes) {
    if (!at.name.startsWith('data-') || at.name === 'data-id') continue;
    if (a.dataset.id) out.push(`[${at.name}][data-id="${q(a.dataset.id)}"]`);
    out.push(`[${at.name}="${q(at.value)}"]`, `[${at.name}]`);
    break;
  }
  return out;
}

/* Nach jedem render(): Titel des Tabs, Scrollstelle aus dem Verlauf, und
   der Fokus — auf die Überschrift einer neuen Seite, zurück auf die Karte,
   von der man kam, oder auf den Knopf, der gerade gedrückt wurde. */
function nachDemZeichnen(vorherFokus) {
  const h1 = app.querySelector('h1');
  const titel = h1 ? h1.textContent.replace(/\s+/g, ' ').trim() : '';
  document.title = titel ? `${titel} – Lumen` : 'Lumen';
  if (S.busy) return;
  const jetzt = `${S.view}|${S.id ?? ''}`;
  const neu = jetzt !== letzteAnsicht;
  letzteAnsicht = jetzt;
  const nachPlayer = S.view !== 'player' ? S.fokusNachPlayer : null;
  if (nachPlayer) S.fokusNachPlayer = null;
  const fokusAuf = sels => {
    for (const sel of sels || []) {
      let el = null;
      try { el = app.querySelector(sel); } catch {}
      if (el) { el.focus({ preventScroll: true }); return true; }
    }
    return false;
  };
  const w = S.wiederherstellen;
  if (w && !S.error) {
    S.wiederherstellen = null;
    const seite = app.querySelector('.page');
    if (seite && w.scroll) seite.scrollTop = w.scroll;
    if (w.reihen) app.querySelectorAll('.rail').forEach((r, i) => { if (w.reihen[i]) r.scrollLeft = w.reihen[i]; });
    if (nachPlayer && fokusAuf(nachPlayer)) return;
    const karte = w.fokusId && app.querySelector(`[data-go][data-id="${CSS.escape(w.fokusId)}"]`);
    if (karte && S.view !== 'search') { karte.focus({ preventScroll: true }); return; }
  }
  if (S.view === 'search') return; // dort gehört der Fokus ins Suchfeld (render)
  if (neu) {
    letzteKarte = null;
    if (nachPlayer && fokusAuf(nachPlayer)) return;
    const auto = app.querySelector('[autofocus]');
    if (auto) { auto.focus(); return; }
    if (h1) { h1.setAttribute('tabindex', '-1'); h1.focus({ preventScroll: true }); }
    return;
  }
  for (const sel of vorherFokus || []) {
    let el = null;
    try { el = app.querySelector(sel); } catch {}
    if (el) { el.focus({ preventScroll: true }); return; }
  }
}

/* Ein Formular, das gerade abgeschickt wird, nimmt keinen zweiten Klick an. */
async function formularGesperrt(f, arbeit) {
  if (f.dataset.laeuft) return;
  f.dataset.laeuft = '1';
  f.setAttribute('aria-busy', 'true');
  const vorher = document.activeElement;
  const knoepfe = [...f.querySelectorAll('button:not([disabled])')];
  knoepfe.forEach(k => { k.disabled = true; });
  try { await arbeit(); }
  finally {
    delete f.dataset.laeuft;
    f.removeAttribute('aria-busy');
    knoepfe.forEach(k => { k.disabled = false; });
    if (f.isConnected && vorher?.isConnected && document.activeElement === document.body) vorher.focus();
  }
}

/* Jede Antwort des Servers geht hier durch: Fehlertexte merken (für
   role="alert"), und eine abgelaufene Anmeldung führt mit Hinweis zur
   Profilauswahl — statt still auf einer leeren Seite zu enden. */
{
  const urCall = api.call;
  api.call = async function (pfad, ...rest) {
    try { return await urCall.call(this, pfad, ...rest); }
    catch (e) {
      if (e?.message) letzterFehlerText = e.message;
      if (e instanceof ApiError && e.status === 401 && S.me && !String(pfad).startsWith('/admin/unlock')) sitzungAbgelaufen();
      throw e;
    }
  };
}

function sitzungAbgelaufen() {
  if (!S.me) return;
  if (!OHNE_VERLAUF.includes(S.view) && !['amComputer', 'schonDabei'].includes(S.view)) {
    S.nachAnmeldung = { view: S.view, id: S.id, query: S.query };
  }
  S.me = null;
  S.hinweis = tr('Deine Anmeldung ist abgelaufen. Bitte melde dich wieder an.');
  aufraeumen();
  go('profiles');
}

// Was nach dem Abmelden nicht weiterlaufen soll: Takte, Beobachter,
// gemerkte Daten des alten Profils.
function aufraeumen() {
  clearTimeout(bibTakt); bibTakt = null;
  clearTimeout(standZeit);
  hoverKarteWeg();
  mehrBeobachter?.disconnect(); mehrBeobachter = null;
  verlaufsStand.clear(); datenSpeicher.clear();
  aktSchluessel = null; letzteKarte = null;
}
function abgemeldet() {
  aufraeumen();
  S.nachAnmeldung = null;
  // Die Adresse gehörte zum alten Profil.
  try { history.replaceState(null, '', location.pathname); } catch {}
}

/* Ein Segmented Control wie bei Apple: eine Spur, das gewählte Feld
   hebt sich ab. Knöpfe mit aria-pressed in einer benannten Gruppe —
   ohne Tabpanel wäre role="tab" eine falsche Auskunft. */
function segment(label, eintraege, extraKlasse = '') {
  return `<div class="segment ${extraKlasse}" role="group" aria-label="${esc(label)}">${eintraege.map(e =>
    `<button type="button" class="segment-knopf" aria-pressed="${!!e.an}" ${e.attr}>${esc(e.name)}</button>`).join('')}</div>`;
}

/* Der leere Zustand, überall gleich: Symbol, Überschrift, ein Satz, und
   wenn es etwas zu tun gibt, ein Knopf. text und aktion sind Markup. */
function leer({ symbol = I.film, titel, text = '', aktion = '', h1 = false }) {
  const h = h1 ? 'h1' : 'h2';
  return `<div class="leer">
    <span class="leer-symbol" aria-hidden="true">${svg(symbol, 40, 1.6)}</span>
    <${h} class="t-title2 leer-titel">${esc(titel)}</${h}>
    ${text ? `<p class="t-body c-2 leer-text">${text}</p>` : ''}
    ${aktion ? `<div class="leer-aktion">${aktion}</div>` : ''}</div>`;
}

// „läuft seit 0 Std." sagte nichts. Ganze Sätze, damit die Übersetzung
// später nicht an zusammengesetzten Stücken hängt.
function laufzeitText(ms) {
  const min = Math.floor((ms || 0) / 60000);
  if (min < 15) return tr('läuft seit wenigen Minuten');
  if (min < 60) return tr('läuft seit {min} Minuten', { min });
  const std = Math.floor(min / 60);
  if (std < 24) return std === 1 ? tr('läuft seit einer Stunde') : tr('läuft seit {std} Stunden', { std });
  const tage = Math.floor(std / 24);
  return tage === 1 ? tr('läuft seit einem Tag') : tr('läuft seit {tage} Tagen', { tage });
}

/* ---------------------------------------------------------------- Geräte unter „Ich" */

const GERAETE_ARTEN = texte({ browser: 'Webbrowser', tv: 'Fernseher', phone: 'Handy', tablet: 'Tablet', mac: 'Mac',
  ios: 'iPhone oder iPad', android: 'Android', appletv: 'Apple TV', androidtv: 'Android TV' });

// Nur der Tag — der Server speichert ohnehin nichts Genaueres.
function zuletztAktiv(t) {
  const d = new Date(t);
  if (isNaN(d)) return '';
  const heute = new Date(); heute.setHours(0, 0, 0, 0);
  const tag = new Date(d); tag.setHours(0, 0, 0, 0);
  const diff = Math.round((heute - tag) / 864e5);
  if (diff <= 0) return tr('heute aktiv');
  if (diff === 1) return tr('gestern aktiv');
  return tr('zuletzt aktiv am {tag}', { tag: datum(d, { day: 'numeric', month: 'long',
    ...(d.getFullYear() !== heute.getFullYear() ? { year: 'numeric' } : {}) }) });
}

function geraetName(d) {
  const n = (d.name || '').trim();
  if (!n || /^browser$/i.test(n)) return GERAETE_ARTEN[d.kind] || tr('Unbekanntes Gerät');
  // „Safari · iPhone" liest sich als „Safari auf iPhone".
  return n.replace(' · ', ' ' + tr('auf') + ' ');
}

function geraetZeile(d) {
  const name = geraetName(d);
  const unter = [d.name && GERAETE_ARTEN[d.kind] ? GERAETE_ARTEN[d.kind] : '', zuletztAktiv(d.lastSeen)].filter(Boolean).join(' · ');
  return `<div class="row" style="min-height:58px;gap:12px">
    <span style="flex:1;min-width:0">
      <span class="t-body" style="display:flex;align-items:center;gap:8px;flex-wrap:wrap">${esc(name)}${
        d.diesesGeraet ? `<span class="dieses-geraet">${tr('Dieses Gerät')}</span>` : ''}</span>
      <span class="t-caption c-2" style="display:block">${esc(unter)}</span></span>
    ${d.diesesGeraet
      ? `<button class="btn btn-pill btn-pill--fill" style="flex:none" data-signout>${tr('Abmelden')}</button>`
      : `<button class="btn btn-pill btn-pill--fill" style="flex:none" data-geraetweg="${esc(d.id)}" data-name="${esc(name)}"
          aria-label="${esc(tr('{name} abmelden', { name }))}">${tr('Abmelden')}</button>`}
  </div>`;
}

/* ---------------------------------------------------------------- Rückfragen */

/* Eine kurze Rückfrage als echtes <dialog>: Esc bricht ab, der Fokus
   steht auf „Abbrechen" — wer aus Versehen Enter drückt, verliert nichts. */
function rueckfrage({ titel, text = '', ja = 'OK', gefaehrlich = false }) {
  return new Promise(fertig => {
    const vorher = document.activeElement;
    const d = document.createElement('dialog');
    d.className = 'lumen rueckfrage' + (app.classList.contains('light') ? ' light' : '');
    d.setAttribute('aria-labelledby', 'rueckfrage-t');
    if (text) d.setAttribute('aria-describedby', 'rueckfrage-x');
    d.innerHTML = `<form method="dialog">
      <h2 id="rueckfrage-t" class="t-title3" style="margin:0">${esc(titel)}</h2>
      ${text ? `<p id="rueckfrage-x" class="t-sub c-2" style="margin:8px 0 0">${esc(text)}</p>` : ''}
      <div class="rueckfrage-knoepfe">
        <button value="nein" class="btn btn-pill btn-pill--fill">${tr('Abbrechen')}</button>
        <button value="ja" class="btn btn-pill ${gefaehrlich ? 'btn-pill--rot' : 'btn-pill--stark'}">${esc(ja)}</button></div></form>`;
    document.body.appendChild(d);
    d.addEventListener('close', () => {
      const ok = d.returnValue === 'ja';
      d.remove();
      if (vorher?.isConnected) vorher.focus();
      fertig(ok);
    });
    d.showModal();
    d.querySelector('button[value="nein"]').focus();
  });
}

function abmeldeFrage(alle) {
  const ohneCode = S.me?.account?.lockKind === 'none';
  if (alle) {
    return { titel: tr('Überall abmelden?'), ja: tr('Überall abmelden'), gefaehrlich: true,
      text: ohneCode ? tr('Alle Geräte – auch dieses – zeigen danach wieder die Profilauswahl.')
        : tr('Alle Geräte – auch dieses – brauchen danach wieder deine PIN oder dein Passwort.') };
  }
  return { titel: tr('Abmelden?'), ja: tr('Abmelden'),
    text: ohneCode ? tr('Danach zeigt dieses Gerät wieder die Profilauswahl.')
      : tr('Danach brauchst du auf diesem Gerät wieder deine PIN oder dein Passwort.') };
}

/* ---------------------------------------------------------------- Menüs und Esc */

function mehrMenueZu(fokusZurueck) {
  const k = app.querySelector('[data-mehrmenue][aria-expanded="true"]');
  if (!k) return false;
  const m = document.getElementById(k.getAttribute('aria-controls'));
  if (m) m.hidden = true;
  k.setAttribute('aria-expanded', 'false');
  if (fokusZurueck) k.focus();
  return true;
}
function mehrMenueAuf(k) {
  const m = document.getElementById(k.getAttribute('aria-controls'));
  if (!m) return;
  m.hidden = false;
  k.setAttribute('aria-expanded', 'true');
  m.querySelector('[role="menuitem"]')?.focus();
}

/* Esc auf der Titelseite: erst schliessen, was offen ist — dann eine
   Seite zurück, wie der Zurück-Knopf. Kein neuer Eintrag im Verlauf. */
function escAufTitelseite() {
  if (mehrMenueZu(true)) return;
  if (hoverKarte) { hoverKarteWeg(); return; }
  if (S.fassungMenu) {
    S.fassungMenu = null; render();
    app.querySelector('[data-fassungmenu]')?.focus();
    return;
  }
  const liste = document.getElementById('listenpanel');
  if (liste && liste.innerHTML.trim()) {
    liste.innerHTML = '';
    const k = app.querySelector('[data-zurliste]');
    k?.setAttribute('aria-expanded', 'false'); k?.focus();
    return;
  }
  const zuweisen = document.getElementById('zuweisen');
  if (zuweisen && zuweisen.innerHTML.trim()) {
    zuweisen.innerHTML = '';
    app.querySelector('[data-mehrmenue]')?.focus();
    return;
  }
  zurueck();
}

// Pfeiltasten und Fernbedienung (räumliche Navigation, Zurück-Taste, F6).
tastaturEinrichten({ app, imPlayer: () => S.view === 'player', zurueck: () => { if (S.me && S.view !== 'home') zurueck(); } });

app.addEventListener('keydown', e => {
  const eintrag = e.target instanceof Element && e.target.closest('[role="menuitem"]');
  if (!eintrag) return;
  const alle = [...eintrag.closest('[role="menu"]').querySelectorAll('[role="menuitem"]')];
  const i = alle.indexOf(eintrag);
  const ziel = { ArrowDown: alle[(i + 1) % alle.length], ArrowUp: alle[(i - 1 + alle.length) % alle.length],
    Home: alle[0], End: alle[alle.length - 1] }[e.key];
  if (ziel) { e.preventDefault(); ziel.focus(); }
  if (e.key === 'Tab') mehrMenueZu(false);
});

/* ---------------------------------------------------------------- Kleine eigene Ansichten */

function viewAmComputer() {
  return shell(`<div class="wrap wrap--flat">${leer({ symbol: I.gear, h1: true,
    titel: tr('Die Verwaltung gibt es am Computer'),
    text: tr('Bibliotheken, Konten und Server richtest du im Browser am Computer oder in der Mac-App ein. Auf Handy und Tablet bleibt Lumen zum Schauen.'),
    aktion: `<button class="btn btn-primary" data-go="home">${tr('Zur Startseite')}</button>` })}</div>`);
}

// Den Einladungslink noch einmal geöffnet, aber schon angemeldet.
function viewSchonDabei() {
  const a = S.me?.account || { name: '' };
  return `<div class="center"><div class="panel" style="align-items:center;text-align:center">
    ${avatar(a, 88)}
    <h1 class="t-large" style="margin:0">${tr('Du bist schon dabei, {name}', { name: esc(a.name) })}</h1>
    <p class="hint" style="margin:0">${tr('Dein Profil gibt es schon — diese Einladung brauchst du nicht mehr.')}</p>
    <button class="btn btn-primary" style="height:46px" data-zu-meinen-filmen>${tr('Zu meinen Filmen')}</button>
    <button class="btn btn-pill" data-einladung-trotzdem>${tr('Ein weiteres Profil anlegen')}</button>
  </div></div>`;
}

/* Klicks auf das, was dieser Abschnitt neu mitbringt. Der grosse
   Verteiler oben bleibt, wie er ist. */
app.addEventListener('click', async e => {
  const t = e.target instanceof Element ? e.target : null;
  if (!t) return;
  let n;
  if (t.closest('[data-skip]')) {
    e.preventDefault();
    const h1 = app.querySelector('main h1') || app.querySelector('main');
    if (h1) { h1.setAttribute('tabindex', '-1'); h1.focus(); }
    return;
  }
  if ((n = t.closest('[data-mehrmenue]'))) {
    if (n.getAttribute('aria-expanded') === 'true') mehrMenueZu(true); else mehrMenueAuf(n);
    return;
  }
  if (t.closest('.mehr-menue [role="menuitem"]')) { mehrMenueZu(false); return; }
  if (t.closest('[data-alle-zeigen]')) { S.filter = 'Alle'; S.buchstabe = ''; return load(); }
  if ((n = t.closest('[data-meintest]'))) {
    S.query = n.dataset.meintest;
    try { history.replaceState({ ...(history.state || {}), view: 'search', query: S.query }, '', adresseFuer('search', null, S.query)); } catch {}
    return load();
  }
  if ((n = t.closest('[data-geraetweg]'))) {
    if (!(await rueckfrage({ titel: `${n.dataset.name} abmelden?`, ja: tr('Abmelden'), gefaehrlich: true,
      text: tr('Das Gerät braucht danach wieder deine PIN oder dein Passwort.') }))) return;
    try { await api.call('/me/devices/' + encodeURIComponent(n.dataset.geraetweg), { method: 'DELETE' }); toast(tr('Gerät abgemeldet')); }
    catch (err) { toast(err.message); }
    return load();
  }
  if (t.closest('[data-zu-meinen-filmen]')) {
    try { history.replaceState(null, '', location.pathname); } catch {}
    return go('home', { ersetzen: true });
  }
  if (t.closest('[data-einladung-trotzdem]')) return go('redeem', { id: S.einladungsCode });
  // Das grosse Bild der Startseite führt zum Film — wie das Poster.
  const held = t.closest('[data-vorschau-hero]');
  if (held && !t.closest('button, a')) return go('detail', { id: held.dataset.vorschauHero });
});
document.addEventListener('click', e => {
  if (e.target instanceof Element && !e.target.closest('.mehr-huelle')) mehrMenueZu(false);
});

/* ---------------------------------------------------------------- Start */

async function boot() {
  try {
    const hello = await api.hello();
    S.hello = hello;
    // Noch nicht eingerichtet: die Sprache des Browsers als Vorschlag.
    if (!hello.setUp) {
      await sprachWechseln(browserSprache(), false);
      return go('setup');
    }
    // Vor der Anmeldung: die zuletzt auf diesem Gerät genutzte Sprache,
    // sonst die des Servers. Nach der Anmeldung die des Profils (render).
    await sprachWechseln(gemerkt() || hello.sprache);

    // Ein Code in der Adresse geht vor. Auch wenn auf diesem Gerät schon
    // jemand angemeldet ist: wer den Link öffnet, will ihn einlösen —
    // etwa auf dem Fernseher im Wohnzimmer, an dem schon jemand hängt.
    // Zurück zum alten Profil kommt man jederzeit über die Übersicht.
    const code = new URLSearchParams(location.search).get('einladung');
    // Schon angemeldet und den Link noch einmal geöffnet: nicht ins
    // Formular (und nicht in „einladung wurde schon benutzt"), sondern
    // freundlich sagen, dass man schon dabei ist.
    if (code && api.signedIn) {
      try {
        S.me = await api.me();
        await api.medienSchluessel();
        S.einladungsCode = code;
        return go('schonDabei', { ersetzen: true });
      } catch { S.me = null; }
    }
    if (code) return go('redeem', { id: code });

    // Die Adresse sagt, wo man war — auch nach F5 oder aus einem Lesezeichen.
    const ziel = ansichtAusAdresse();
    if (api.signedIn) {
      try {
        // Beides zugleich: unterwegs kostet jede Runde eine halbe Sekunde.
        [S.me] = await Promise.all([api.me(), api.medienSchluessel()]);
        const st = history.state;
        if (st?.view && ziel && st.view === ziel.view) {
          verlaufTiefe = typeof st.tiefe === 'number' ? st.tiefe : 0;
          return go(st.view, { id: st.id, query: st.query, ausVerlauf: true, stand: st });
        }
        if (ziel && ziel.view !== 'home') return go(ziel.view, { id: ziel.id, query: ziel.query, ersetzen: true });
        return go('home', { ersetzen: true });
      } catch (e) {
        // Nur eine abgelaufene Anmeldung meldet ab — ohne Netz (unterwegs,
        // im Funkloch) bleibt man angemeldet und es geht weiter, sobald
        // die Verbindung zurück ist.
        if (!(e instanceof ApiError) || e.status !== 0) api.signOut();
        else throw e;
      }
    }
    if (ziel && ziel.view !== 'home') S.nachAnmeldung = ziel;
    return go('profiles');
  } catch (e) {
    S.error = e.message;
    S.view = 'profiles';
    render();
  }
}

// Die Hilfen dieser Datei für die Module der Fenster (fenster.js).
Object.assign(FU, { S, esc, svg, I, toast, artOf, posterCard, shell, loading, go, load, render, PLAY });

// Wechselt die Sprache (Ich › Einstellungen, ein anderes Profil, die
// Verwaltung), zeichnet die Oberfläche neu – ohne die Seite neu zu laden.
// Den Player lässt das in Ruhe: Er zeichnet seine Leiste selbst.
beiWechsel(() => { if (S.view !== 'player') render(); });

boot();

// Die Verbindung ist zurück: Eine Fehlerseite lädt sich von selbst neu —
// niemand soll auf „Nochmal versuchen" tippen müssen, nachdem der Zug aus
// dem Tunnel ist.
addEventListener('online', () => {
  if (!S.error || S.view === 'player') return;
  if (S.me) load(); else boot();
});

// Die Hülle der App für den Fall ohne Netz (sw.js). Nur über https oder
// localhost — anders lassen Browser keinen Service Worker zu.
if ('serviceWorker' in navigator && window.isSecureContext) {
  const swAnmelden = () => { navigator.serviceWorker.register('/sw.js').catch(() => {}); };
  if (document.readyState === 'complete') swAnmelden(); else addEventListener('load', swAnmelden);
}

// Den Medienschlüssel vor dem Ablaufen erneuern; abgelaufen hiesse: keine
// Bilder mehr, und eine laufende Wiedergabe bräche ab.
setInterval(() => { if (api.signedIn) api.medienSchluessel().catch(() => {}); }, 15 * 60 * 1000);

// Statt Inline-Handlern (die die Sicherheitsrichtlinie der Seite verbietet):
// Bilder und Videos, die nicht laden, verschwinden; ein Profilbild, das
// fehlt, wird zum Anfangsbuchstaben. „error" steigt nicht auf, darum in der
// Einfangphase.
// Standbilder (data-nochmal) bekommen vorher noch einige Versuche, mit
// wachsendem Abstand (zusammen rund anderthalb Minuten): Sie entstehen
// erst, wenn jemand sie sehen will, eins nach dem anderen.
const NOCHMAL_SEK = [2, 3, 4, 5, 6, 8, 10, 12, 15, 20];
document.addEventListener('error', e => {
  const el = e.target;
  if (!(el instanceof Element)) return;
  if (el.hasAttribute('data-ersatz')) { el.parentNode.textContent = el.getAttribute('data-ersatz'); return; }
  const n = Number(el.getAttribute('data-nochmal'));
  if (el.hasAttribute('data-nochmal') && n < NOCHMAL_SEK.length) {
    el.setAttribute('data-nochmal', String(n + 1));
    const src = el.getAttribute('src');
    setTimeout(() => { if (el.isConnected) el.setAttribute('src', src); }, NOCHMAL_SEK[n] * 1000);
    return;
  }
  if (el.hasAttribute('data-weg')) el.remove();
}, true);
document.addEventListener('click', e => {
  const el = e.target instanceof Element && e.target.closest('[data-auswahl]');
  if (el && typeof el.select === 'function') el.select();
});
