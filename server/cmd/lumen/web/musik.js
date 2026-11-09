// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

/* Musik — Ansichten und der eigene Audio-Player.
 *
 * Der Player lebt ausserhalb von #app: app.js zeichnet bei jedem
 * Seitenwechsel alles neu, und ein <audio> darin hörte jedes Mal auf. Hier
 * hängt er am <body> und läuft weiter, während man durch Lumen geht.
 *
 * Gehört wird, was dieses Profil hört. Gemeldet wird nur „ganz gehört" —
 * ohne Uhrzeit, für „Zuletzt gehört" und „Beliebt" in diesem Profil. Die
 * Warteschlange liegt nur in diesem Browser.
 */

import { tr, texte } from './sprache.js';

let C = null; // was app.js mitgibt: S, api, go, render, shell, esc, svg, I, toast, …

/* Texte an einer Stelle, damit sie sich später übersetzen lassen. */
const T = texte({
  musik: 'Musik', start: 'Start', alben: 'Alben', interpreten: 'Interpreten', titel: 'Musik|Titel',
  genres: 'Genres', listen: 'Playlists', favoriten: 'Favoriten',
  zuletztGehoert: 'Zuletzt gehört', neu: 'Neu hinzugefügt', abspielen: 'Abspielen', zufall: 'Zufall',
  alsNaechstes: 'Als Nächstes spielen', spaeter: 'Später spielen', zurListe: 'Zur Playlist …',
  zumAlbum: 'Zum Album', zumInterpreten: 'Zum Interpreten', verschiedene: 'Verschiedene Interpreten',
  beliebt: 'Beliebt bei dir', erscheintAuf: 'Erscheint auf', mehrVon: 'Mehr von',
  warteschlange: 'Als Nächstes', leer: 'Hier ist noch nichts.', explizit: 'E',
  hoerverlaufLoeschen: 'Hörverlauf löschen',
});

const MUSIK_ANSICHTEN = ['musik', 'musikAlben', 'musikAlbum', 'musikInterpreten', 'musikInterpret', 'musikTitel',
  'musikGenres', 'musikGenre', 'musikListen', 'musikListe', 'musikFavoriten'];

const NOTE = '<path d="M9 18V6l11-2v12"/><circle cx="6.5" cy="18" r="2.5"/><circle cx="17.5" cy="16" r="2.5"/>';
const IC = {
  note: NOTE,
  zufall: '<path d="M3 7h3.5c2.5 0 3.8 1.2 5.5 5s3 5 5.5 5H21M18 14l3 3-3 3M3 17h3.5c1.2 0 2.1-.3 2.9-1M14.6 8c.8-.7 1.7-1 2.9-1H21M18 4l3 3-3 3"/>',
  wiederholen: '<path d="M4 11V9a3 3 0 0 1 3-3h12M16 3l3 3-3 3M20 13v2a3 3 0 0 1-3 3H5M8 21l-3-3 3-3"/>',
  zurueck: '<path d="M19 5v14l-9-7z"/><path d="M6 5v14"/>',
  vor: '<path d="M5 5v14l9-7z"/><path d="M18 5v14"/>',
  mehr: '<circle cx="5.5" cy="12" r="1.3"/><circle cx="12" cy="12" r="1.3"/><circle cx="18.5" cy="12" r="1.3"/>',
  liste: '<path d="M4 6h11M4 12h11M4 18h7M18 15v6M15 18h6"/>',
  schlange: '<path d="M4 6h16M4 12h16M4 18h10"/>',
  laut: '<path d="M4 9.5h3.5L12 6v12l-4.5-3.5H4z"/><path d="M15.5 9a4 4 0 0 1 0 6M18 6.5a7.5 7.5 0 0 1 0 11"/>',
  leise: '<path d="M4 9.5h3.5L12 6v12l-4.5-3.5H4z"/><path d="M16 9.5l5 5M21 9.5l-5 5"/>',
  runter: '<path d="M6 9l6 6 6-6"/>',
  griff: '<path d="M8 7h8M8 12h8M8 17h8"/>',
  weg: '<path d="M6 6l12 12M18 6L6 18"/>',
};

/* ---------------------------------------------------------------- Hilfen */

const esc = s => C.esc(s);
const svg = (d, s, w) => C.svg(d, s, w);

function dauer(ms) {
  const s = Math.round((ms || 0) / 1000);
  return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')}`;
}
function dauerLang(ms) {
  if ((ms || 0) < 60000) return tr('{n} Sekunden', { n: Math.round((ms || 0) / 1000) });
  const min = Math.round((ms || 0) / 60000);
  if (min < 60) return tr('{min} Minuten', { min });
  const h = Math.floor(min / 60), m = min % 60;
  return m ? tr('{h} Std. {min} Min.', { h, min: m }) : tr('{h} Std.', { h });
}
const mitKey = url => {
  if (!url) return '';
  const k = C.api.keyParam();
  return k ? url + (url.includes('?') ? '&' : '?') + k : url;
};
const coverUrl = (cover, g = 'mittel') => cover ? mitKey(`${cover}?g=${g}`) : '';
const interpretVon = a => a.compilation ? T.verschiedene : (a.interpret || '');

function coverBild(cover, g, alt = '', klasse = '') {
  return `<div class="mu-cover ${klasse}">${cover
    ? `<img src="${esc(coverUrl(cover, g))}" alt="${esc(alt)}" loading="lazy" decoding="async" data-weg>` : ''}
    <span class="mu-cover-leer" aria-hidden="true">${svg(NOTE, 34, 1.4)}</span></div>`;
}

const herz = (an, art, id, gross = false) =>
  `<button class="btn mu-herz${an ? ' mu-herz--an' : ''}${gross ? ' btn-round btn-round--fill' : ''}" data-mu-herz="${art}:${esc(id)}"
     aria-pressed="${!!an}" aria-label="${an ? tr('Aus den Favoriten nehmen') : tr('Zu den Favoriten')}" title="${an ? tr('Favorit') : tr('Zu den Favoriten')}">
     ${svg(C.I.herz, gross ? 19 : 16, 2)}</button>`;

/* ---------------------------------------------------------------- Bereiche */

let bereiche = { musik: false, fotos: false, fotoOrt: false };
let bereicheFuer = null;

/* Ob es Musik und Fotos für dieses Profil gibt — einmal je Anmeldung. */
export async function bereicheLaden(erzwingen = false) {
  const id = C.S.me?.account?.id || null;
  if (!id) { bereiche = { musik: false, fotos: false }; bereicheFuer = null; return bereiche; }
  if (!erzwingen && bereicheFuer === id) return bereiche;
  try { bereiche = await C.api.call('/mediathek'); bereicheFuer = id; } catch { /* bleibt, wie es war */ }
  return bereiche;
}
export const bereich = () => bereiche;

/* ---------------------------------------------------------------- Schnittstelle zu app.js */

export function einrichten(ctx) {
  C = ctx;
  spielerAufbauen();
  document.addEventListener('click', klick);
  document.addEventListener('keydown', taste);
  document.addEventListener('submit', absenden, true);
  // Startet ein Film, hält die Musik an — beides gleichzeitig will niemand.
  document.addEventListener('play', e => {
    if (e.target instanceof HTMLVideoElement && Q.spielt) pausieren();
  }, true);
}

export function nav() {
  return bereiche.musik ? [['musik', NOTE, T.musik, MUSIK_ANSICHTEN]] : [];
}

export function ichZeile(linkRow) {
  return bereiche.musik ? linkRow(T.musik, tr('Alben, Interpreten, Playlists'), 'musik') : '';
}

export const ansichten = {
  musik:            { laden: () => C.api.call('/musik/start'), zeichnen: () => vStart() },
  musikAlben:       { laden: () => C.api.call(`/musik/alben?sort=${encodeURIComponent(C.S.muSort || 'name')}&limit=1000`), zeichnen: () => vAlben() },
  musikAlbum:       { laden: () => C.api.call('/musik/alben/' + encodeURIComponent(C.S.id)), zeichnen: () => vAlbum() },
  musikInterpreten: { laden: () => C.api.call('/musik/interpreten'), zeichnen: () => vInterpreten() },
  musikInterpret:   { laden: () => C.api.call('/musik/interpreten/' + encodeURIComponent(C.S.id)), zeichnen: () => vInterpret() },
  musikTitel:       { laden: () => titelLaden(), zeichnen: () => vTitel() },
  musikGenres:      { laden: () => C.api.call('/musik/genres'), zeichnen: () => vGenres() },
  musikGenre:       { laden: () => C.api.call(`/musik/alben?genre=${encodeURIComponent(C.S.id)}&limit=1000`), zeichnen: () => vGenre() },
  musikListen:      { laden: () => C.api.call('/musik/listen'), zeichnen: () => vListen() },
  musikListe:       { laden: () => { C.S.muListeWeg = false; C.S.muListeUmbenennen = false; return C.api.call('/musik/listen/' + encodeURIComponent(C.S.id)); }, zeichnen: () => vListe() },
  musikFavoriten:   { laden: () => C.api.call('/musik/favoriten'), zeichnen: () => vFavoriten() },
};

/* Für die Suche: Abschnitt „Musik". */
export async function suche(q) {
  if (!bereiche.musik || !q.trim()) return null;
  try { return await C.api.call('/musik/suche?q=' + encodeURIComponent(q) + '&limit=8'); } catch { return null; }
}
export function suchTreffer(d) {
  return !!d && ((d.titel || []).length + (d.alben || []).length + (d.interpreten || []).length) > 0;
}
export function suchAbschnitt(d) {
  if (!suchTreffer(d)) return '';
  merkeTitel(d.titel);
  return `<section class="mu-suche" style="margin-bottom:30px">
    <div class="section-h" style="padding-left:0;display:flex;align-items:center;gap:6px">${svg(NOTE, 14, 2)}${T.musik}</div>
    ${(d.interpreten || []).length ? `<div class="rail" style="gap:16px;margin-bottom:14px">${d.interpreten.map(interpretKarte).join('')}</div>` : ''}
    ${(d.alben || []).length ? `<div class="rail" style="gap:16px;margin-bottom:14px">${d.alben.map(a => albumKarte(a, true)).join('')}</div>` : ''}
    ${(d.titel || []).length ? `<div class="mu-titelliste mu-titelliste--kompakt">${d.titel.map((t, i) => titelZeile(t, i, { cover: true, album: true, kontext: d.titel })).join('')}</div>` : ''}
  </section>`;
}

/* Nach jedem Zeichnen: Leiste sichtbar oder nicht, Profilwechsel merken. */
export function nachRender() {
  const id = C.S.me?.account?.id || null;
  if (id !== Q.konto) kontoGewechselt(id);
  // Wer gerade eine Bibliothek angelegt hat, soll Musik und Fotos gleich
  // in der Navigation finden.
  if (C.S.view === 'adminLibraries') bereicheLaden(true);
  leisteZeigen();
  markiereLaufenden();
}

/* ---------------------------------------------------------------- Ansichten */

const REITER = () => [['musik', T.start], ['musikAlben', T.alben], ['musikInterpreten', T.interpreten], ['musikTitel', T.titel],
  ['musikGenres', T.genres], ['musikListen', T.listen], ['musikFavoriten', T.favoriten]];

function reiter(aktiv) {
  return `<nav class="mu-reiter" aria-label="${tr('Musik')}">${REITER().map(([v, n]) =>
    `<button class="tab-pill" aria-pressed="${aktiv === v}" data-go="${v}">${n}</button>`).join('')}</nav>`;
}

function seite(aktiv, inhalt, titel = T.musik) {
  return C.shell(`<div class="wrap wrap--flat mu-seite">
    <h1 class="t-large" style="margin:0 0 14px">${esc(titel)}</h1>
    ${reiter(aktiv)}
    ${inhalt}
    <div style="height:40px"></div></div>`);
}

function albumKarte(a, klein = false) {
  return `<button class="card mu-albumkarte${klein ? ' mu-albumkarte--klein' : ''}" data-go="musikAlbum" data-id="${esc(a.id)}" aria-label="${esc(a.name)}">
    ${coverBild(a.cover, 'mittel', '', 'thumb')}
    <div class="t-body clamp-1" style="margin-top:8px">${esc(a.name)}</div>
    <div class="t-sub c-2 clamp-1">${esc(interpretVon(a))}${a.jahr && !klein ? ' · ' + a.jahr : ''}</div></button>`;
}

function interpretKarte(m) {
  return `<button class="card mu-interpretkarte" data-go="musikInterpret" data-id="${esc(m.id)}" aria-label="${esc(m.name)}">
    <div class="mu-rund">${m.cover ? `<img src="${esc(coverUrl(m.cover, 'mittel'))}" alt="" loading="lazy" data-weg>` : ''}
      <span>${esc((m.name || '?')[0])}</span></div>
    <div class="t-body clamp-1" style="margin-top:8px;text-align:center">${esc(m.name)}</div></button>`;
}

const albenRaster = alben => alben.length
  ? `<div class="mu-raster">${alben.map(a => albumKarte(a)).join('')}</div>`
  : `<p class="hint">${T.leer}</p>`;

const rail = (titel, alben, ziel) => !alben?.length ? '' : `
  <section class="sec"><h2 class="t-title3">${ziel ? `<a href="#" class="titel-link" data-go="${ziel}">${esc(titel)} ${svg(C.I.chev, 16, 2.4)}</a>` : esc(titel)}</h2>
    <div class="rail">${alben.map(a => albumKarte(a)).join('')}</div></section>`;

function leerOderLaden() { return C.S.data ? null : seite('musik', C.loading()); }

function vStart() {
  const d = C.S.data || {};
  if (!d.anzahlTitel) {
    return seite('musik', `<div class="mu-leer">${svg(NOTE, 44, 1.4)}
      <p class="t-body">${tr('Noch keine Musik — oder noch nicht eingelesen.')}</p>
      <p class="hint">${tr('Die Verwaltung legt unter „Bibliotheken“ eine Bibliothek der Art „Musik“ an.')}</p></div>`);
  }
  const listen = d.listen || [];
  return seite('musik', `
    <div class="mu-zahlen t-sub c-2">${tr('{n} Alben', { n: d.anzahlAlben })} · ${tr('{n} Interpreten', { n: d.anzahlInterpreten })} · ${tr('Musik|{n} Titel', { n: d.anzahlTitel })}</div>
    <div style="display:flex;gap:10px;flex-wrap:wrap;margin-top:14px">
      <button class="btn btn-primary" data-mu-allezufall>${svg(IC.zufall, 17, 2)} ${tr('Alles zufällig')}</button></div>
    ${rail(T.zuletztGehoert, d.zuletztGehoert)}
    ${rail(T.neu, d.neu, 'musikAlben')}
    ${listen.length ? `<section class="sec"><h2 class="t-title3"><a href="#" class="titel-link" data-go="musikListen">${T.listen} ${svg(C.I.chev, 16, 2.4)}</a></h2>
      <div class="rail">${listen.map(listeKarte).join('')}</div></section>` : ''}
    ${(d.zuletztGehoert || []).length ? `<p class="t-caption c-2" style="margin-top:36px;max-width:560px">
      ${tr('„Zuletzt gehört“ und „Beliebt“ kommen aus deinem eigenen Hören. Niemand sonst sieht das — auch nicht die Verwaltung.')}
      <button class="btn mu-link" data-mu-verlaufweg>${T.hoerverlaufLoeschen}</button></p>` : ''}`);
}

const SORT_ALBEN = () => [['name', 'A–Z'], ['interpret', tr('Interpret')], ['jahr', tr('Jahr')], ['neu', tr('Neu')]];

function vAlben() {
  const d = C.S.data || {}, s = C.S.muSort || 'name';
  return seite('musikAlben', `
    <div class="mu-leiste-oben"><span class="t-sub c-2">${d.gesamt || 0} ${d.gesamt === 1 ? tr('Album') : tr('Alben')}</span>
      <span style="flex:1"></span>
      <div class="mu-segment" role="group" aria-label="${tr('Sortieren')}">${SORT_ALBEN().map(([v, n]) =>
        `<button class="tab-pill" aria-pressed="${s === v}" data-mu-sort="${v}">${n}</button>`).join('')}</div></div>
    ${albenRaster(d.alben || [])}`);
}

function vAlbum() {
  const d = C.S.data;
  if (!d) return leerOderLaden();
  const a = d.album, titel = d.titel || [];
  merkeTitel(titel);
  const kopfInterpret = a.compilation ? `<span>${T.verschiedene}</span>`
    : `<a href="#" data-go="musikInterpret" data-id="${esc(a.interpretId || '')}">${esc(a.interpret)}</a>`;
  let disc = null;
  const zeilen = titel.map((t, i) => {
    let kopf = '';
    if (d.mehrereDiscs && (t.disc || 1) !== disc) {
      disc = t.disc || 1;
      kopf = `<div class="mu-disc t-sub-em c-2">${tr('Disc {disc}', { disc })}</div>`;
    }
    return kopf + titelZeile(t, i, { nummer: true, interpretWennAnders: a.interpret, kontext: titel, quelle: a.name });
  }).join('');
  return C.shell(`<div class="wrap wrap--flat mu-seite">
    <button class="btn btn-pill btn-pill--fill mu-zurueck" data-back>${svg(C.I.back, 16, 2)}${tr('Zurück')}</button>
    <header class="mu-albumkopf">
      ${coverBild(a.cover, 'gross', a.name, 'mu-albumcover thumb')}
      <div class="mu-albumtext">
        <h1 class="t-large">${esc(a.name)}</h1>
        <div class="t-title3 mu-akzent">${kopfInterpret}</div>
        <div class="t-sub c-2" style="margin-top:4px">${[a.genre, a.jahr].filter(Boolean).map(esc).join(' · ')}</div>
        <div class="mu-knoepfe">
          <button class="btn btn-primary" data-mu-spielen="0">${C.PLAY(16)} ${T.abspielen}</button>
          <button class="btn btn-pill btn-pill--fill" style="height:44px;padding:0 20px;font-size:15px" data-mu-zufall>${svg(IC.zufall, 17, 2)} ${T.zufall}</button>
          ${herz(a.favorit, 'album', a.id, true)}
          <button class="btn btn-round btn-round--fill" data-mu-albummenue="${esc(a.id)}" aria-label="${tr('Mehr')}" title="${tr('Mehr')}">${svg(IC.mehr, 20, 2)}</button>
        </div>
      </div>
    </header>
    <div class="mu-titelliste" style="margin-top:26px">${zeilen}</div>
    <p class="t-sub c-2" style="margin:14px 4px 0">${tr('Musik|{n} Titel', { n: titel.length })}, ${dauerLang(a.dauerMs)}</p>
    ${a.interpretId ? rail(`${T.mehrVon} ${a.interpret}`, d.mehr) : ''}
    <div style="height:40px"></div></div>`);
}

function vInterpreten() {
  const d = C.S.data || {}, alle = d.interpreten || [];
  return seite('musikInterpreten', alle.length
    ? `<div class="mu-interpretenraster">${alle.map(interpretKarte).join('')}</div>` : `<p class="hint">${T.leer}</p>`);
}

function vInterpret() {
  const d = C.S.data;
  if (!d) return leerOderLaden();
  const m = d.interpret, beliebt = d.beliebt || [];
  merkeTitel(beliebt);
  return C.shell(`<div class="wrap wrap--flat mu-seite">
    <button class="btn btn-pill btn-pill--fill mu-zurueck" data-back>${svg(C.I.back, 16, 2)}${tr('Zurück')}</button>
    <header class="mu-interpretkopf">
      <div class="mu-rund mu-rund--gross">${m.cover ? `<img src="${esc(coverUrl(m.cover, 'gross'))}" alt="" data-weg>` : ''}<span>${esc((m.name || '?')[0])}</span></div>
      <div>
        <h1 class="t-large">${esc(m.name)}</h1>
        <div class="t-sub c-2">${tr('{n} Alben', { n: d.alben.length })} · ${tr('Musik|{n} Titel', { n: d.anzahlTitel })}</div>
        <div class="mu-knoepfe">
          <button class="btn btn-primary" data-mu-interpretspielen="${esc(m.id)}">${C.PLAY(16)} ${T.abspielen}</button>
          <button class="btn btn-pill btn-pill--fill" style="height:44px;padding:0 20px;font-size:15px" data-mu-interpretspielen="${esc(m.id)}" data-zufall="1">${svg(IC.zufall, 17, 2)} ${T.zufall}</button>
          ${herz(m.favorit, 'interpret', m.id, true)}
        </div>
      </div>
    </header>
    ${beliebt.length ? `<section class="sec"><h2 class="t-title3">${T.beliebt}</h2>
      <div class="mu-titelliste">${beliebt.map((t, i) => titelZeile(t, i, { cover: true, album: true, kontext: beliebt })).join('')}</div></section>` : ''}
    <section class="sec"><h2 class="t-title3">${T.alben}</h2>${albenRaster(d.alben)}</section>
    ${(d.erscheintAuf || []).length ? `<section class="sec"><h2 class="t-title3">${T.erscheintAuf}</h2>
      <div class="rail">${d.erscheintAuf.map(a => albumKarte(a)).join('')}</div></section>` : ''}
    <div style="height:40px"></div></div>`);
}

const SORT_TITEL = () => [['titel', tr('Titel')], ['interpret', tr('Interpret')], ['album', tr('Album')], ['jahr', tr('Jahr')], ['dauer', tr('Dauer')], ['neu', tr('Neu')]];

function titelLaden() {
  const s = C.S.muTitelSort || 'titel', ab = C.S.muTitelAb ? '1' : '';
  return C.api.call(`/musik/titel?sort=${s}&ab=${ab}&limit=500`);
}

function vTitel() {
  const d = C.S.data || {}, titel = d.titel || [], s = C.S.muTitelSort || 'titel';
  merkeTitel(titel);
  return seite('musikTitel', `
    <div class="mu-leiste-oben"><span class="t-sub c-2">${tr('Musik|{n} Titel', { n: d.gesamt || 0 })}</span><span style="flex:1"></span>
      <div class="mu-segment" role="group" aria-label="${tr('Sortieren')}">${SORT_TITEL().map(([v, n]) =>
        `<button class="tab-pill" aria-pressed="${s === v}" data-mu-titelsort="${v}">${n}${s === v ? (C.S.muTitelAb ? ' ↓' : ' ↑') : ''}</button>`).join('')}</div></div>
    <div class="mu-titelliste mu-titelliste--tabelle">${titel.map((t, i) => titelZeile(t, i, { cover: true, album: true, jahr: true, kontext: titel })).join('')}</div>
    ${d.weiter ? `<button class="btn btn-pill btn-pill--fill" style="margin-top:16px" data-mu-mehrtitel>${tr('Mehr laden')}</button>` : ''}`);
}

function vGenres() {
  const g = C.S.data?.genres || [];
  return seite('musikGenres', g.length ? `<div class="mu-genres">${g.map((x, i) => `
    <button class="card mu-genre mu-genre--${i % 6}" data-go="musikGenre" data-id="${esc(x.name)}">
      ${x.cover ? `<img src="${esc(coverUrl(x.cover, 'mittel'))}" alt="" loading="lazy" data-weg>` : ''}
      <span class="t-title3">${esc(x.name)}</span><span class="t-caption">${x.alben} ${x.alben === 1 ? tr('Album') : tr('Alben')}</span></button>`).join('')}</div>`
    : `<p class="hint">${tr('Keine Genres in den Tags.')}</p>`);
}

function vGenre() {
  const d = C.S.data || {};
  return seite('musikGenres', `<h2 class="t-title2" style="margin:0 0 16px">${esc(C.S.id)}</h2>${albenRaster(d.alben || [])}`);
}

/* ---------------------------------------------------------------- Listen */

function listeKarte(l) {
  return `<button class="card mu-albumkarte" data-go="musikListe" data-id="${esc(l.id)}" aria-label="${esc(l.name)}">
    <div class="mu-cover thumb mu-listenbild">${svg(IC.liste, 38, 1.5)}</div>
    <div class="t-body clamp-1" style="margin-top:8px">${esc(l.name)}</div>
    <div class="t-sub c-2 clamp-1">${tr('Musik|{n} Titel', { n: l.anzahl })}${l.meine ? (l.geteilt ? ' · ' + tr('geteilt') : '') : ' · ' + tr('von {name}', { name: esc(l.von.name) })}</div></button>`;
}

function vListen() {
  const alle = C.S.data?.listen || [];
  const meine = alle.filter(l => l.meine), andere = alle.filter(l => !l.meine);
  return seite('musikListen', `
    <form id="mu-listeform" class="sfield" style="max-width:520px" autocomplete="off">
      ${svg(C.I.plus, 18, 2)}<input name="name" placeholder="${tr('Neue Playlist')}" maxlength="80" aria-label="${tr('Name der neuen Playlist')}">
      <button class="btn btn-pill btn-pill--fill" style="height:32px;font-size:13px">${tr('Anlegen')}</button></form>
    <section style="margin-top:26px"><div class="section-h" style="padding-left:0">${tr('Meine Playlists')}</div>
      ${meine.length ? `<div class="mu-raster">${meine.map(listeKarte).join('')}</div>` : `<p class="hint">${tr('Noch keine. Leg oben eine an — oder bei einem Titel über „…“.')}</p>`}</section>
    ${andere.length ? `<section style="margin-top:30px"><div class="section-h" style="padding-left:0">${tr('Im Haushalt geteilt')}</div>
      <div class="mu-raster">${andere.map(listeKarte).join('')}</div></section>` : ''}
    <p class="t-sub c-2" style="margin-top:26px;display:flex;gap:8px;max-width:620px">${svg(C.I.lock, 14, 2.2)}
      <span>${tr('Eine Playlist sieht nur, wer sie angelegt hat — bis sie geteilt wird. Auch die Verwaltung sieht sie vorher nicht.')}</span></p>`);
}

function vListe() {
  const d = C.S.data;
  if (!d) return leerOderLaden();
  const l = d.liste, titel = d.titel || [], mosaik = d.mosaik || [];
  merkeTitel(titel);
  const bild = mosaik.length >= 4
    ? `<div class="mu-cover mu-albumcover thumb mu-mosaik">${mosaik.map(c => `<img src="${esc(coverUrl(c, 'mittel'))}" alt="" data-weg>`).join('')}</div>`
    : coverBild(mosaik[0] || '', 'gross', '', 'mu-albumcover thumb');
  const schalter = (label, an, key) => `<div class="row" style="min-height:48px"><span class="t-body" style="flex:1">${label}</span>
      <button class="switch" role="switch" aria-checked="${!!an}" data-mu-listesw="${key}" aria-label="${label}"><i></i></button></div>`;
  return C.shell(`<div class="wrap wrap--flat mu-seite">
    <button class="btn btn-pill btn-pill--fill mu-zurueck" data-back>${svg(C.I.back, 16, 2)}${tr('Zurück')}</button>
    <header class="mu-albumkopf">${bild}
      <div class="mu-albumtext">
        ${C.S.muListeUmbenennen ? `<form id="mu-umbenennen"><input class="titel-feld t-large" name="name" value="${esc(l.name)}" maxlength="80" aria-label="${tr('Name')}"></form>`
          : `<h1 class="t-large">${esc(l.name)}</h1>`}
        <div class="t-sub c-2" style="margin-top:4px">${l.meine ? (l.gemeinsam ? tr('geteilt · alle dürfen mitschreiben') : l.geteilt ? tr('im Haushalt geteilt') : tr('nur für dich')) : tr('von {name}', { name: esc(l.von.name) })}
          · ${tr('Musik|{n} Titel', { n: titel.length })}${d.dauerMs ? ', ' + dauerLang(d.dauerMs) : ''}</div>
        <div class="mu-knoepfe">
          <button class="btn btn-primary" data-mu-spielen="0" ${titel.length ? '' : 'disabled'}>${C.PLAY(16)} ${T.abspielen}</button>
          <button class="btn btn-pill btn-pill--fill" style="height:44px;padding:0 20px;font-size:15px" data-mu-zufall ${titel.length ? '' : 'disabled'}>${svg(IC.zufall, 17, 2)} ${T.zufall}</button>
          ${l.meine ? `<button class="btn btn-pill btn-pill--fill" style="height:44px" data-mu-umbenennen>${tr('Umbenennen')}</button>
            <button class="btn btn-pill" style="height:44px;color:#ff6961" data-mu-listeweg>${C.S.muListeWeg ? tr('Wirklich löschen?') : tr('Löschen')}</button>` : ''}
        </div>
      </div></header>
    ${l.meine ? `<div class="group group--inset" style="max-width:520px;margin-top:22px">
      ${schalter(tr('Im Haushalt teilen'), l.geteilt, 'geteilt')}${l.geteilt ? schalter(tr('Andere dürfen mitschreiben'), l.gemeinsam, 'gemeinsam') : ''}</div>` : ''}
    <div class="mu-titelliste" style="margin-top:22px" data-mu-sortierbar="${l.darfAendern ? '1' : ''}">
      ${titel.map((t, i) => titelZeile(t, i, { cover: true, album: true, kontext: titel, liste: l.darfAendern ? l.id : '' })).join('')
        || `<p class="hint" style="padding:12px 4px">${tr('Leer. Bei einem Titel oder Album über „…“ → „{zurListe}“ hinzufügen.', { zurListe: T.zurListe })}</p>`}</div>
    <div style="height:40px"></div></div>`);
}

function vFavoriten() {
  const d = C.S.data || {}, titel = d.titel || [];
  merkeTitel(titel);
  const leer = !titel.length && !(d.alben || []).length && !(d.interpreten || []).length;
  return seite('musikFavoriten', leer ? `<p class="hint">${tr('Mit dem Herz bei Titeln, Alben und Interpreten sammelst du hier, was dir gefällt.')}</p>` : `
    ${(d.interpreten || []).length ? `<section class="sec" style="margin-top:6px"><h2 class="t-title3">${T.interpreten}</h2>
      <div class="rail">${d.interpreten.map(interpretKarte).join('')}</div></section>` : ''}
    ${(d.alben || []).length ? `<section class="sec"><h2 class="t-title3">${T.alben}</h2>${albenRaster(d.alben)}</section>` : ''}
    ${titel.length ? `<section class="sec"><h2 class="t-title3" style="display:flex;align-items:center;gap:12px">${T.titel}
        <button class="btn btn-pill btn-pill--fill" data-mu-spielen="0">${C.PLAY(13)} ${T.abspielen}</button></h2>
      <div class="mu-titelliste">${titel.map((t, i) => titelZeile(t, i, { cover: true, album: true, kontext: titel })).join('')}</div></section>` : ''}`);
}

/* ---------------------------------------------------------------- Titelzeilen */

/* Die zuletzt gezeichneten Titel, damit ein Klick weiss, was dazugehört. */
let titelNachId = new Map();
let kontextListe = [];
function merkeTitel(liste) {
  kontextListe = liste || [];
  for (const t of kontextListe) titelNachId.set(t.id, t);
  if (titelNachId.size > 5000) titelNachId = new Map(kontextListe.map(t => [t.id, t]));
}

function titelZeile(t, i, o = {}) {
  const anders = o.interpretWennAnders != null && t.interpret && t.interpret.toLowerCase() !== (o.interpretWennAnders || '').toLowerCase();
  const unter = [o.album ? t.interpret : anders ? t.interpret : '', o.album && t.album ? t.album : ''].filter(Boolean).map(esc).join(' · ');
  return `<div class="mu-zeile" data-mu-zeile="${esc(t.id)}" data-i="${i}" ${o.liste ? 'draggable="true"' : ''}>
    ${o.liste ? `<span class="mu-griff" aria-hidden="true">${svg(IC.griff, 16, 2)}</span>` : ''}
    <button class="mu-zeile-start" data-mu-titel="${esc(t.id)}" data-i="${i}" aria-label="${esc(tr('{titel} abspielen', { titel: t.titel }))}">
      ${o.cover ? `<span class="mu-zeile-cover">${t.cover ? `<img src="${esc(coverUrl(t.cover, 'klein'))}" alt="" loading="lazy" data-weg>` : svg(NOTE, 16, 1.6)}</span>`
        : `<span class="mu-nr tnum">${o.nummer ? (t.track || i + 1) : i + 1}</span>`}
      <span class="mu-zeile-text"><span class="t-body clamp-1">${esc(t.titel)}${t.explizit ? ` <span class="badge" title="explizit">${T.explizit}</span>` : ''}</span>
        ${unter ? `<span class="t-sub c-2 clamp-1">${unter}</span>` : ''}</span>
    </button>
    ${o.jahr ? `<span class="t-sub c-2 tnum mu-spalte-jahr">${t.jahr || ''}</span>` : ''}
    ${herz(t.favorit, 'titel', t.id)}
    <span class="t-sub c-2 tnum mu-dauer">${dauer(t.dauerMs)}</span>
    ${o.liste ? `<button class="btn mu-mehr" data-mu-entfernen="${esc(t.id)}" data-liste="${esc(o.liste)}" aria-label="${tr('Aus der Playlist nehmen')}" title="${tr('Aus der Playlist nehmen')}">${svg(IC.weg, 15, 2)}</button>` : ''}
    <button class="btn mu-mehr" data-mu-menue="${esc(t.id)}" aria-label="${tr('Mehr zu {titel}', { titel: esc(t.titel) })}" title="${tr('Mehr')}">${svg(IC.mehr, 18, 2)}</button>
  </div>`;
}

function markiereLaufenden() {
  const id = Q.liste[Q.pos]?.id;
  for (const z of document.querySelectorAll('.mu-zeile')) {
    const an = z.dataset.muZeile === id;
    z.classList.toggle('mu-zeile--laeuft', an);
    z.classList.toggle('mu-zeile--pause', an && !Q.spielt);
  }
}

/* ---------------------------------------------------------------- Player */

/* Die Warteschlange: liste ist, was der Reihe nach kommt, pos der
 * laufende Titel. „Als Nächstes" schiebt hinter pos ein, „Später" hängt
 * hinten an. Beim Zufall wird der Rest hinter pos gemischt — die alte
 * Reihenfolge bleibt in ordnung, damit „Zufall aus" sie zurückbringt. */
const Q = {
  liste: [], pos: -1, ordnung: null, zufall: false, wiederholen: 'aus', // aus | alle | eins
  spielt: false, offen: false, schlangeOffen: false, konto: null,
  gemeldet: false, a: null, b: null, vorgeladen: null, lautstaerke: 1, umgewandelt: new Set(),
};
let leiste = null, gross = null;

function spielerAufbauen() {
  const audio = () => {
    const el = document.createElement('audio');
    el.preload = 'auto';
    el.setAttribute('playsinline', '');
    el.addEventListener('timeupdate', zeitGeaendert);
    el.addEventListener('ended', zuEnde);
    el.addEventListener('play', () => { if (el === Q.a) { Q.spielt = true; zustandZeigen(); } });
    el.addEventListener('pause', () => { if (el === Q.a) { Q.spielt = false; zustandZeigen(); } });
    el.addEventListener('error', () => { if (el === Q.a) abspielFehler(); });
    el.addEventListener('loadedmetadata', () => { if (el === Q.a) zeitGeaendert(); });
    return el;
  };
  Q.a = audio(); Q.b = audio();
  try { Q.lautstaerke = Math.min(1, Math.max(0, Number(localStorage.getItem('lumen.musik.laut') ?? 1))); } catch {}
  Q.a.volume = Q.b.volume = Q.lautstaerke;

  leiste = document.createElement('div');
  leiste.className = 'mu-mini glass glass--regular';
  leiste.hidden = true;
  leiste.setAttribute('role', 'region');
  leiste.setAttribute('aria-label', tr('Musik-Player'));
  document.body.appendChild(leiste);

  gross = document.createElement('div');
  gross.className = 'mu-gross';
  gross.hidden = true;
  gross.setAttribute('role', 'dialog');
  gross.setAttribute('aria-modal', 'true');
  gross.setAttribute('aria-label', tr('Läuft gerade'));
  document.body.appendChild(gross);
  document.body.appendChild(Q.a); document.body.appendChild(Q.b);

  // Mit Maus oder Finger am Fortschritt ziehen.
  for (const el of [leiste, gross]) {
    el.addEventListener('pointerdown', e => {
      const bar = e.target.closest('[data-mu-scrub]');
      if (!bar) return;
      e.preventDefault();
      const setzen = ev => {
        const r = bar.getBoundingClientRect();
        const anteil = Math.min(1, Math.max(0, (ev.clientX - r.left) / r.width));
        if (Q.a.duration && isFinite(Q.a.duration)) Q.a.currentTime = anteil * Q.a.duration;
        zeitGeaendert();
      };
      setzen(e);
      const weg = () => { removeEventListener('pointermove', setzen); removeEventListener('pointerup', weg); };
      addEventListener('pointermove', setzen); addEventListener('pointerup', weg);
    });
    el.addEventListener('input', e => {
      if (e.target.matches('[data-mu-laut]')) lautSetzen(Number(e.target.value));
    });
  }
  mediaSession();
}

const laeuft = () => Q.liste[Q.pos] || null;

function quelleFuer(t) {
  // Kann der Browser das Format? Sonst gleich die umgewandelte Fassung.
  const kann = t.mime && Q.a.canPlayType(t.mime) !== '';
  const umw = !kann || Q.umgewandelt.has(t.id);
  if (!umw) return mitKey(t.stream);
  // AAC können fast alle; wo nicht (freie Chromium-Fassungen unter Linux),
  // nimmt Lumen Opus.
  const als = Q.a.canPlayType('audio/mp4; codecs="mp4a.40.2"') !== '' ? 'aac' : 'opus';
  return mitKey(`${t.stream}?als=${als}`);
}

function laden(el, t) {
  el.src = quelleFuer(t);
  el.dataset.titel = t.id;
  el.load();
}

async function abspielen(liste, start = 0, zufall = false) {
  if (!liste?.length) return;
  Q.liste = liste.slice();
  Q.ordnung = null;
  Q.zufall = false;
  Q.pos = Math.max(0, Math.min(start, Q.liste.length - 1));
  if (zufall) {
    Q.pos = Math.floor(Math.random() * Q.liste.length);
    zufallAn();
  }
  await titelStarten();
}

async function titelStarten(position = 0) {
  const t = laeuft();
  if (!t) return;
  Q.gemeldet = false;
  Q.vorgeladen = null;
  Q.b.removeAttribute('src');
  if (Q.a.dataset.titel !== t.id || !Q.a.src) laden(Q.a, t);
  if (position) {
    const setzen = () => { try { Q.a.currentTime = position; } catch {} };
    if (Q.a.readyState >= 1) setzen(); else Q.a.addEventListener('loadedmetadata', setzen, { once: true });
  }
  try { await C.api.medienSchluessel(); } catch {}
  try { await Q.a.play(); } catch (e) {
    // Autoplay verweigert (etwa nach dem Neuladen) — dann wartet der
    // Player auf einen Druck auf Abspielen.
    Q.spielt = false;
  }
  zustandZeigen(true);
  speichern();
}

function abspielFehler() {
  const t = laeuft();
  if (!t) return;
  // Direkt ging nicht (altes Safari und FLAC, ein seltenes Format): einmal
  // umgewandelt versuchen, dann aufgeben und weiter.
  if (!Q.umgewandelt.has(t.id)) {
    Q.umgewandelt.add(t.id);
    laden(Q.a, t);
    Q.a.play().catch(() => {});
    return;
  }
  C.toast(tr('„{titel}“ lässt sich nicht abspielen', { titel: t.titel }));
  setTimeout(() => naechster(true), 600);
}

function pausieren() { Q.a.pause(); }
function weiter() {
  if (!laeuft()) return;
  if (!Q.a.src) return titelStarten(Q.gespeichertePos || 0);
  Q.a.play().catch(() => {});
}
function umschalten() { Q.spielt ? pausieren() : weiter(); }

function naechster(automatisch = false) {
  if (!Q.liste.length) return;
  if (Q.wiederholen === 'eins' && automatisch) { Q.a.currentTime = 0; Q.gemeldet = false; Q.a.play().catch(() => {}); return; }
  if (Q.pos + 1 < Q.liste.length) Q.pos++;
  else if (Q.wiederholen === 'alle') Q.pos = 0;
  else { Q.a.pause(); Q.a.currentTime = 0; zustandZeigen(); return; }
  wechselnAufVorgeladen();
}

function vorheriger() {
  // Wie überall: nach drei Sekunden zurück an den Anfang des Titels,
  // davor zum vorigen.
  if (Q.a.currentTime > 3 || Q.pos === 0) { Q.a.currentTime = 0; return; }
  Q.pos--;
  titelStarten();
}

/* Lückenlos, so gut es ohne Web Audio geht: der nächste Titel liegt
 * schon geladen im zweiten <audio>, und beim Ende wird nur getauscht. */
function wechselnAufVorgeladen() {
  const t = laeuft();
  if (t && Q.vorgeladen === t.id && Q.b.dataset.titel === t.id) {
    const alt = Q.a;
    Q.a = Q.b; Q.b = alt;
    alt.pause(); alt.removeAttribute('src'); alt.load();
    Q.vorgeladen = null; Q.gemeldet = false;
    Q.a.volume = Q.lautstaerke;
    Q.a.play().catch(() => {});
    zustandZeigen(true);
    speichern();
    return;
  }
  titelStarten();
}

function zuEnde() { naechster(true); }

function vorladen() {
  const naechsterIndex = Q.pos + 1 < Q.liste.length ? Q.pos + 1 : Q.wiederholen === 'alle' ? 0 : -1;
  const n = Q.liste[naechsterIndex];
  if (!n || Q.wiederholen === 'eins' || Q.vorgeladen === n.id) return;
  Q.vorgeladen = n.id;
  laden(Q.b, n);
}

function zeitGeaendert() {
  const t = laeuft(), a = Q.a;
  if (!t) return;
  const d = isFinite(a.duration) && a.duration ? a.duration : (t.dauerMs || 0) / 1000;
  const p = a.currentTime || 0;
  // Ganz gehört: die Hälfte oder vier Minuten — dann zählt es.
  if (!Q.gemeldet && d && (p >= d * 0.5 || p >= 240)) {
    Q.gemeldet = true;
    C.api.call(`/musik/titel/${encodeURIComponent(t.id)}/gehoert`, { method: 'POST' }).catch(() => {});
  }
  if (d && d - p < 25) vorladen();
  const anteil = d ? Math.min(100, p / d * 100) : 0;
  for (const bar of document.querySelectorAll('[data-mu-scrub] > i')) bar.style.width = anteil + '%';
  for (const el of document.querySelectorAll('[data-mu-zeit]')) el.textContent = dauer(p * 1000);
  for (const el of document.querySelectorAll('[data-mu-rest]')) el.textContent = '−' + dauer(Math.max(0, d - p) * 1000);
  if ('mediaSession' in navigator && d && navigator.mediaSession.setPositionState) {
    try { navigator.mediaSession.setPositionState({ duration: d, position: Math.min(p, d), playbackRate: a.playbackRate || 1 }); } catch {}
  }
  if (Math.floor(p) % 5 === 0) speichernBald();
}

function lautSetzen(v) {
  Q.lautstaerke = Math.min(1, Math.max(0, v));
  Q.a.volume = Q.b.volume = Q.lautstaerke;
  try { localStorage.setItem('lumen.musik.laut', String(Q.lautstaerke)); } catch {}
  for (const el of document.querySelectorAll('[data-mu-laut]')) if (Number(el.value) !== Q.lautstaerke) el.value = Q.lautstaerke;
}

/* ---------------------------------------------------------------- Warteschlange */

function zufallAn() {
  Q.ordnung = Q.liste.slice();
  const t = laeuft();
  const rest = Q.liste.filter((_, i) => i !== Q.pos);
  for (let i = rest.length - 1; i > 0; i--) { const j = Math.floor(Math.random() * (i + 1)); [rest[i], rest[j]] = [rest[j], rest[i]]; }
  Q.liste = t ? [t, ...rest] : rest;
  Q.pos = 0;
  Q.zufall = true;
  Q.vorgeladen = null;
}
function zufallAus() {
  const t = laeuft();
  if (Q.ordnung) {
    // Was inzwischen dazukam, bleibt hinten dran.
    const ids = new Set(Q.ordnung.map(x => x.id));
    Q.liste = [...Q.ordnung, ...Q.liste.filter(x => !ids.has(x.id))];
    Q.pos = Math.max(0, Q.liste.findIndex(x => x.id === t?.id));
  }
  Q.ordnung = null; Q.zufall = false; Q.vorgeladen = null;
}

function einreihen(titel, wie) {
  if (!titel.length) return;
  if (!laeuft()) return abspielen(titel, 0);
  if (wie === 'naechstes') Q.liste.splice(Q.pos + 1, 0, ...titel);
  else Q.liste.push(...titel);
  if (Q.ordnung) Q.ordnung.push(...titel);
  Q.vorgeladen = null;
  C.toast(wie === 'naechstes' ? (titel.length === 1 ? tr('Kommt als Nächstes') : tr('Musik|{n} Titel kommen als Nächstes', { n: titel.length }))
    : (titel.length === 1 ? tr('Kommt später') : tr('Musik|{n} Titel kommen später', { n: titel.length })));
  zustandZeigen();
  speichern();
}

function ausSchlange(i) {
  if (i === Q.pos) return;
  Q.liste.splice(i, 1);
  if (i < Q.pos) Q.pos--;
  Q.vorgeladen = null;
  zustandZeigen();
  speichern();
}

function verschieben(von, nach) {
  if (von === nach || von === Q.pos) return;
  const [x] = Q.liste.splice(von, 1);
  Q.liste.splice(nach, 0, x);
  if (von < Q.pos && nach >= Q.pos) Q.pos--;
  else if (von > Q.pos && nach <= Q.pos) Q.pos++;
  Q.vorgeladen = null;
  zustandZeigen();
  speichern();
}

/* ---------------------------------------------------------------- Merken */

/* Die Warteschlange je Profil in diesem Browser, damit ein Neuladen sie
 * nicht verliert. Nur hier, nie auf dem Server. */
let speicherTimer = null;
function speichernBald() { clearTimeout(speicherTimer); speicherTimer = setTimeout(speichern, 1500); }
function speichern() {
  if (!Q.konto) return;
  const knapp = t => ({ id: t.id, titel: t.titel, interpret: t.interpret, interpretId: t.interpretId, album: t.album,
    albumId: t.albumId, cover: t.cover, stream: t.stream, mime: t.mime, dauerMs: t.dauerMs });
  try {
    localStorage.setItem('lumen.musik.' + Q.konto, JSON.stringify({
      liste: Q.liste.slice(0, 1000).map(knapp), pos: Q.pos, zeit: Q.a.currentTime || 0,
      zufall: Q.zufall, wiederholen: Q.wiederholen }));
  } catch {}
}

function kontoGewechselt(id) {
  // Anderes Profil, andere Musik: anhalten und die Schlange des neuen
  // Profils holen, falls es eine gab.
  Q.a.pause(); Q.b.pause();
  Q.a.removeAttribute('src'); Q.b.removeAttribute('src');
  Q.liste = []; Q.pos = -1; Q.ordnung = null; Q.spielt = false; Q.offen = false; Q.konto = id;
  gross.hidden = true;
  bereicheFuer = null;
  if (id) {
    bereicheLaden().then(() => { const n = document.querySelector('.sidebar'); if (n && bereiche.musik && !n.querySelector('[data-go="musik"]')) C.render(); });
    try {
      const g = JSON.parse(localStorage.getItem('lumen.musik.' + id) || 'null');
      if (g && g.liste?.length) {
        Q.liste = g.liste; Q.pos = Math.min(g.pos || 0, g.liste.length - 1);
        Q.zufall = !!g.zufall; Q.wiederholen = g.wiederholen || 'aus'; Q.gespeichertePos = g.zeit || 0;
      }
    } catch {}
  }
  zustandZeigen(true);
}

/* ---------------------------------------------------------------- Anzeige */

function leisteZeigen() {
  const sichtbar = !!laeuft() && !!C.S.me && !['player', 'profiles', 'unlock', 'setup', 'redeem', 'start'].includes(C.S.view);
  leiste.hidden = !sichtbar;
  document.body.classList.toggle('mu-an', sichtbar);
  const hell = document.documentElement.classList.contains('hell');
  leiste.classList.toggle('mu-hell', hell);
  if (!sichtbar && !gross.hidden) grossZu();
}

function zustandZeigen(neu = false) {
  const t = laeuft();
  if (neu || !leiste.firstChild) leisteMalen();
  else {
    for (const b of document.querySelectorAll('[data-mu-umschalten]')) {
      b.innerHTML = Q.spielt ? C.PAUSE(b.dataset.gr || 22) : C.PLAY(b.dataset.gr || 22);
      b.setAttribute('aria-label', Q.spielt ? tr('Pause') : tr('Abspielen'));
    }
  }
  if (!gross.hidden) grossMalen();
  leisteZeigen();
  markiereLaufenden();
  if ('mediaSession' in navigator) {
    navigator.mediaSession.playbackState = t ? (Q.spielt ? 'playing' : 'paused') : 'none';
    if (neu && t) {
      try {
        navigator.mediaSession.metadata = new MediaMetadata({
          title: t.titel, artist: t.interpret || '', album: t.album || '',
          artwork: t.cover ? [{ src: new URL(coverUrl(t.cover, 'gross'), location.href).href, sizes: '1200x1200', type: 'image/jpeg' },
                              { src: new URL(coverUrl(t.cover, 'mittel'), location.href).href, sizes: '480x480', type: 'image/jpeg' }] : [],
        });
      } catch {}
    }
  }
  // Der Browser-Tab nennt, was läuft — wie bei jedem Musikdienst.
  if (neu) document.title = t && Q.spielt ? `${t.titel} · ${t.interpret || ''} — Lumen` : 'Lumen';
}

function leisteMalen() {
  const t = laeuft();
  if (!t) { leiste.innerHTML = ''; return; }
  leiste.innerHTML = `
    <div class="mu-mini-bar" data-mu-scrub aria-hidden="true"><i></i></div>
    <button class="mu-mini-info" data-mu-aufklappen aria-label="${tr('Player öffnen')}">
      <span class="mu-mini-cover">${t.cover ? `<img src="${esc(coverUrl(t.cover, 'klein'))}" alt="" data-weg>` : svg(NOTE, 18, 1.6)}</span>
      <span class="mu-mini-text"><span class="t-headline clamp-1">${esc(t.titel)}</span>
        <span class="t-sub c-2 clamp-1">${esc(t.interpret || '')}</span></span></button>
    <div class="mu-mini-knoepfe">
      <button class="btn mu-k mu-nur-breit" data-mu-vorher aria-label="${tr('Vorheriger Titel')}">${svg(IC.zurueck, 20, 1.8)}</button>
      <button class="btn mu-k" data-mu-umschalten data-gr="24" aria-label="${Q.spielt ? tr('Pause') : tr('Abspielen')}">${Q.spielt ? C.PAUSE(24) : C.PLAY(24)}</button>
      <button class="btn mu-k" data-mu-naechster aria-label="${tr('Nächster Titel')}">${svg(IC.vor, 20, 1.8)}</button>
      <span class="mu-nur-breit mu-laut">${svg(IC.laut, 17, 1.8)}<input type="range" min="0" max="1" step="0.01" value="${Q.lautstaerke}" data-mu-laut aria-label="${tr('Lautstärke')}"></span>
    </div>`;
}

function grossAuf() {
  if (!laeuft()) return;
  Q.offen = true;
  gross.hidden = false;
  grossMalen();
  document.body.classList.add('mu-gross-auf');
  requestAnimationFrame(() => gross.classList.add('mu-gross--auf'));
  gross.querySelector('[data-mu-zuklappen]')?.focus();
}
function grossZu() {
  Q.offen = false; Q.schlangeOffen = false;
  gross.classList.remove('mu-gross--auf');
  document.body.classList.remove('mu-gross-auf');
  setTimeout(() => { if (!Q.offen) gross.hidden = true; }, 320);
}

function grossMalen() {
  const t = laeuft();
  if (!t) return;
  const d = (t.dauerMs || 0) / 1000;
  const wied = { aus: tr('Wiederholen aus'), alle: tr('Alles wiederholen'), eins: tr('Diesen Titel wiederholen') }[Q.wiederholen];
  const kommt = Q.liste.slice(Q.pos + 1);
  gross.innerHTML = `
    <div class="mu-gross-grund">${t.cover ? `<img src="${esc(coverUrl(t.cover, 'mittel'))}" alt="" data-weg>` : ''}</div>
    <div class="mu-gross-innen">
      <div class="mu-gross-oben">
        <button class="btn btn-round glass glass--clear" data-mu-zuklappen aria-label="${tr('Zuklappen')}">${svg(IC.runter, 22, 2.2)}</button>
        <span style="flex:1"></span>
        <button class="btn btn-round glass glass--clear${Q.schlangeOffen ? ' mu-an-knopf' : ''}" data-mu-schlange aria-pressed="${Q.schlangeOffen}" aria-label="${T.warteschlange}" title="${T.warteschlange}">${svg(IC.schlange, 20, 2)}</button>
      </div>
      <div class="mu-gross-haupt${Q.schlangeOffen ? ' mu-gross-haupt--schlange' : ''}">
        <div class="mu-gross-links">
          <div class="mu-gross-cover${Q.spielt ? '' : ' mu-gross-cover--pause'}">${t.cover ? `<img src="${esc(coverUrl(t.cover, 'gross'))}" alt="" data-weg>` : `<span>${svg(NOTE, 80, 1.2)}</span>`}</div>
          <div class="mu-gross-titel">
            <div style="min-width:0;flex:1"><div class="t-title2 clamp-1">${esc(t.titel)}</div>
              <button class="btn mu-link-hell clamp-1" data-mu-gehzu="interpret" ${t.interpretId ? '' : 'disabled'}>${esc(t.interpret || '')}</button>
              ${t.album ? `<button class="btn mu-link-hell mu-link-leise clamp-1" data-mu-gehzu="album">${esc(t.album)}</button>` : ''}</div>
            ${herz(titelNachId.get(t.id)?.favorit, 'titel', t.id, true)}
          </div>
          <div class="mu-gross-bar" data-mu-scrub><i></i></div>
          <div class="mu-gross-zeiten t-caption tnum"><span data-mu-zeit>0:00</span><span data-mu-rest>−${dauer(d * 1000)}</span></div>
          <div class="mu-gross-knoepfe">
            <button class="btn mu-k mu-k--klein${Q.zufall ? ' mu-k--an' : ''}" data-mu-zufallumschalten aria-pressed="${Q.zufall}" aria-label="${tr('Zufall')}" title="${tr('Zufall')}">${svg(IC.zufall, 20, 2)}</button>
            <button class="btn mu-k" data-mu-vorher aria-label="${tr('Vorheriger Titel')}">${svg(IC.zurueck, 32, 1.6)}</button>
            <button class="btn mu-k mu-k--gross" data-mu-umschalten data-gr="44" aria-label="${Q.spielt ? tr('Pause') : tr('Abspielen')}">${Q.spielt ? C.PAUSE(44) : C.PLAY(44)}</button>
            <button class="btn mu-k" data-mu-naechster aria-label="${tr('Nächster Titel')}">${svg(IC.vor, 32, 1.6)}</button>
            <button class="btn mu-k mu-k--klein${Q.wiederholen !== 'aus' ? ' mu-k--an' : ''}" data-mu-wiederholen aria-label="${wied}" title="${wied}">${svg(IC.wiederholen, 20, 2)}${Q.wiederholen === 'eins' ? '<b class="mu-eins">1</b>' : ''}</button>
          </div>
          <div class="mu-gross-laut">${svg(IC.leise, 16, 1.8)}<input type="range" min="0" max="1" step="0.01" value="${Q.lautstaerke}" data-mu-laut aria-label="${tr('Lautstärke')}">${svg(IC.laut, 18, 1.8)}</div>
        </div>
        ${Q.schlangeOffen ? `<div class="mu-schlange" aria-label="${T.warteschlange}">
          <div class="t-headline" style="margin:4px 4px 10px;display:flex;align-items:center">${T.warteschlange}
            ${Q.zufall ? `<span class="t-caption c-2" style="margin-left:8px">${tr('gemischt')}</span>` : ''}
            <span style="flex:1"></span>${kommt.length ? `<button class="btn mu-link-hell t-sub" data-mu-schlangeleer>${tr('Leeren')}</button>` : ''}</div>
          ${kommt.length ? kommt.map((x, k) => {
            const i = Q.pos + 1 + k;
            return `<div class="mu-schlange-zeile" draggable="true" data-mu-sz="${i}">
              <span class="mu-griff" aria-hidden="true">${svg(IC.griff, 16, 2)}</span>
              <button class="mu-schlange-start" data-mu-springen="${i}">
                <span class="mu-zeile-cover">${x.cover ? `<img src="${esc(coverUrl(x.cover, 'klein'))}" alt="" data-weg>` : svg(NOTE, 14, 1.6)}</span>
                <span class="mu-zeile-text"><span class="t-body clamp-1">${esc(x.titel)}</span><span class="t-sub clamp-1">${esc(x.interpret || '')}</span></span></button>
              <button class="btn mu-k mu-k--klein" data-mu-hoch="${i}" aria-label="${tr('Nach oben')}" ${k === 0 ? 'disabled' : ''}>${svg('<path d="M6 15l6-6 6 6"/>', 16, 2)}</button>
              <button class="btn mu-k mu-k--klein" data-mu-raus="${i}" aria-label="${tr('Aus der Warteschlange')}">${svg(IC.weg, 15, 2)}</button>
            </div>`; }).join('') : `<p class="t-sub" style="opacity:.7;margin:4px">${tr('Danach kommt nichts mehr.')}</p>`}
        </div>` : ''}
      </div>
    </div>`;
  zeitGeaendert();
}

/* ---------------------------------------------------------------- Media Session */

function mediaSession() {
  if (!('mediaSession' in navigator)) return;
  const ms = navigator.mediaSession;
  const h = (a, f) => { try { ms.setActionHandler(a, f); } catch {} };
  h('play', () => weiter());
  h('pause', () => pausieren());
  h('previoustrack', () => vorheriger());
  h('nexttrack', () => naechster());
  h('seekto', e => { if (e.seekTime != null) Q.a.currentTime = e.seekTime; });
  h('seekbackward', e => { Q.a.currentTime = Math.max(0, Q.a.currentTime - (e.seekOffset || 10)); });
  h('seekforward', e => { Q.a.currentTime = Math.min(Q.a.duration || 1e9, Q.a.currentTime + (e.seekOffset || 10)); });
  h('stop', () => pausieren());
}

/* ---------------------------------------------------------------- Menü */

let menue = null;
function menueZu() { menue?.remove(); menue = null; }

function menueAuf(knopf, eintraege) {
  menueZu();
  menue = document.createElement('div');
  menue.className = 'mu-menue glass glass--thick';
  menue.setAttribute('role', 'menu');
  menue.innerHTML = eintraege.map(([wert, text, gefahr]) => wert === '-' ? '<hr>'
    : `<button role="menuitem" data-mu-wahl="${esc(wert)}"${gefahr ? ' class="mu-gefahr"' : ''}>${text}</button>`).join('');
  document.body.appendChild(menue);
  const r = knopf.getBoundingClientRect(), m = menue.getBoundingClientRect();
  let left = Math.min(window.innerWidth - m.width - 10, r.right - m.width);
  let top = r.bottom + 6;
  if (top + m.height > window.innerHeight - 10) top = Math.max(10, r.top - m.height - 6);
  menue.style.left = Math.max(10, left) + 'px';
  menue.style.top = top + 'px';
  menue.querySelector('button')?.focus();
  return new Promise(fertig => { menue._fertig = fertig; });
}

async function listenWahl(knopf, titelIds, album) {
  let listen = [];
  try { listen = (await C.api.call('/musik/listen')).listen.filter(l => l.darfAendern); } catch {}
  const wahl = await menueAuf(knopf, [['neu', tr('＋ Neue Playlist …')], ...(listen.length ? [['-']] : []),
    ...listen.map(l => ['l:' + l.id, esc(l.name)])]);
  if (!wahl) return;
  try {
    const body = album ? { album } : { titel: titelIds };
    if (wahl === 'neu') {
      const name = prompt(tr('Name der neuen Playlist'));
      if (!name || !name.trim()) return;
      const l = await C.api.call('/musik/listen', { method: 'POST', body: { name: name.trim() } });
      await C.api.call(`/musik/listen/${encodeURIComponent(l.id)}/titel`, { method: 'POST', body });
      C.toast(tr('In „{name}“ gelegt', { name: name.trim() }));
    } else {
      const id = wahl.slice(2);
      await C.api.call(`/musik/listen/${encodeURIComponent(id)}/titel`, { method: 'POST', body });
      C.toast(tr('In „{name}“ gelegt', { name: listen.find(l => l.id === id)?.name || tr('die Playlist') }));
    }
  } catch (err) { C.toast(err.message); }
}

async function titelMenue(knopf, t) {
  const wahl = await menueAuf(knopf, [['naechstes', T.alsNaechstes], ['spaeter', T.spaeter], ['liste', T.zurListe], ['-'],
    ...(t.albumId ? [['album', T.zumAlbum]] : []), ...(t.interpretId ? [['interpret', T.zumInterpreten]] : [])]);
  if (wahl === 'naechstes' || wahl === 'spaeter') einreihen([t], wahl);
  else if (wahl === 'liste') listenWahl(knopf, [t.id]);
  else if (wahl === 'album') C.go('musikAlbum', { id: t.albumId });
  else if (wahl === 'interpret') C.go('musikInterpret', { id: t.interpretId });
}

/* ---------------------------------------------------------------- Ereignisse */

async function alleTitelVonInterpret(id) {
  const d = await C.api.call('/musik/interpreten/' + encodeURIComponent(id));
  const titel = [];
  for (const a of [...d.alben].reverse()) {
    try { titel.push(...(await C.api.call('/musik/alben/' + encodeURIComponent(a.id))).titel); } catch {}
  }
  return titel;
}

async function klick(e) {
  const el = e.target instanceof Element ? e.target : null;
  if (!el) return;
  if (menue && !el.closest('.mu-menue')) { menue._fertig?.(null); menueZu(); }
  const hit = sel => el.closest(`[${sel}]`);
  let n;

  if ((n = hit('data-mu-wahl'))) { const f = menue?._fertig; menueZu(); f?.(n.dataset.muWahl); return; }
  if ((n = hit('data-mu-titel'))) {
    const liste = kontextVon(n);
    const i = Number(n.dataset.i) || 0;
    if (laeuft()?.id === liste[i]?.id) return umschalten();
    return abspielen(liste, i);
  }
  if ((n = hit('data-mu-spielen'))) { const liste = kontextListe.slice(); return abspielen(liste, Number(n.dataset.muSpielen) || 0); }
  if (hit('data-mu-zufall')) return abspielen(kontextListe.slice(), 0, true);
  if ((n = hit('data-mu-interpretspielen'))) {
    try { const titel = await alleTitelVonInterpret(n.dataset.muInterpretspielen); merkeTitel(titel); return abspielen(titel, 0, !!n.dataset.zufall); }
    catch (err) { return C.toast(err.message); }
  }
  if (hit('data-mu-allezufall')) {
    try { const d = await C.api.call('/musik/titel?sort=titel&limit=2000'); return abspielen(d.titel, 0, true); }
    catch (err) { return C.toast(err.message); }
  }
  if ((n = hit('data-mu-herz'))) {
    e.preventDefault();
    const [art, id] = n.dataset.muHerz.split(/:(.*)/s);
    const an = n.getAttribute('aria-pressed') !== 'true';
    try {
      await C.api.call(`/musik/favoriten/${encodeURIComponent(art)}/${encodeURIComponent(id)}`, { method: an ? 'PUT' : 'DELETE' });
      for (const b of document.querySelectorAll(`[data-mu-herz="${CSS.escape(n.dataset.muHerz)}"]`)) {
        b.setAttribute('aria-pressed', String(an)); b.classList.toggle('mu-herz--an', an);
      }
      if (art === 'titel' && titelNachId.get(id)) titelNachId.get(id).favorit = an;
    } catch (err) { C.toast(err.message); }
    return;
  }
  if ((n = hit('data-mu-menue'))) {
    const t = titelNachId.get(n.dataset.muMenue);
    if (t) titelMenue(n, t);
    return;
  }
  if ((n = hit('data-mu-albummenue'))) {
    const titel = (C.S.data?.titel || []).slice();
    const wahl = await menueAuf(n, [['naechstes', T.alsNaechstes], ['spaeter', T.spaeter], ['liste', T.zurListe]]);
    if (wahl === 'liste') listenWahl(n, [], n.dataset.muAlbummenue);
    else if (wahl) einreihen(titel, wahl);
    return;
  }
  if ((n = hit('data-mu-entfernen'))) {
    try {
      await C.api.call(`/musik/listen/${encodeURIComponent(n.dataset.liste)}/titel/${encodeURIComponent(n.dataset.muEntfernen)}`, { method: 'DELETE' });
      return C.load();
    } catch (err) { return C.toast(err.message); }
  }
  if ((n = hit('data-mu-sort'))) { C.S.muSort = n.dataset.muSort; return C.load(); }
  if ((n = hit('data-mu-titelsort'))) {
    const v = n.dataset.muTitelsort;
    if (C.S.muTitelSort === v || (!C.S.muTitelSort && v === 'titel')) C.S.muTitelAb = !C.S.muTitelAb;
    else { C.S.muTitelSort = v; C.S.muTitelAb = false; }
    return C.load();
  }
  if (hit('data-mu-mehrtitel')) {
    const d = C.S.data, s = C.S.muTitelSort || 'titel';
    try {
      const mehr = await C.api.call(`/musik/titel?sort=${s}&ab=${C.S.muTitelAb ? 1 : ''}&limit=500&offset=${d.titel.length}`);
      d.titel = [...d.titel, ...mehr.titel]; d.weiter = mehr.weiter;
      const y = document.querySelector('.page')?.scrollTop;
      C.render();
      if (y != null) document.querySelector('.page').scrollTop = y;
    } catch (err) { C.toast(err.message); }
    return;
  }
  if (hit('data-mu-verlaufweg')) {
    if (!confirm(tr('Alles vergessen, was du gehört hast? „Zuletzt gehört“ und „Beliebt“ fangen von vorn an.'))) return;
    try { await C.api.call('/musik/hoerverlauf', { method: 'DELETE' }); C.toast(tr('Hörverlauf gelöscht')); return C.load(); }
    catch (err) { return C.toast(err.message); }
  }
  if (hit('data-mu-umbenennen')) { C.S.muListeUmbenennen = true; C.render(); document.querySelector('#mu-umbenennen input')?.select(); return; }
  if (hit('data-mu-listeweg')) {
    if (!C.S.muListeWeg) { C.S.muListeWeg = true; return C.render(); }
    try { await C.api.call('/musik/listen/' + encodeURIComponent(C.S.id), { method: 'DELETE' }); C.S.muListeWeg = false; return C.go('musikListen', { ersetzen: true }); }
    catch (err) { return C.toast(err.message); }
  }
  if ((n = hit('data-mu-listesw'))) {
    const an = n.getAttribute('aria-checked') !== 'true';
    try { await C.api.call('/musik/listen/' + encodeURIComponent(C.S.id), { method: 'POST', body: { [n.dataset.muListesw]: an } }); return C.load(); }
    catch (err) { return C.toast(err.message); }
  }

  // Player
  if (hit('data-mu-umschalten')) return umschalten();
  if (hit('data-mu-naechster')) return naechster();
  if (hit('data-mu-vorher')) return vorheriger();
  if (hit('data-mu-aufklappen')) return grossAuf();
  if (hit('data-mu-zuklappen')) return grossZu();
  if (hit('data-mu-schlange')) { Q.schlangeOffen = !Q.schlangeOffen; return grossMalen(); }
  if (hit('data-mu-schlangeleer')) { Q.liste = Q.liste.slice(0, Q.pos + 1); if (Q.ordnung) Q.ordnung = Q.liste.slice(); Q.vorgeladen = null; speichern(); return grossMalen(); }
  if ((n = hit('data-mu-springen'))) { Q.pos = Number(n.dataset.muSpringen); return titelStarten(); }
  if ((n = hit('data-mu-raus'))) return ausSchlange(Number(n.dataset.muRaus));
  if ((n = hit('data-mu-hoch'))) { const i = Number(n.dataset.muHoch); return verschieben(i, Math.max(Q.pos + 1, i - 1)); }
  if (hit('data-mu-zufallumschalten')) { Q.zufall ? zufallAus() : zufallAn(); speichern(); return grossMalen(); }
  if (hit('data-mu-wiederholen')) {
    Q.wiederholen = { aus: 'alle', alle: 'eins', eins: 'aus' }[Q.wiederholen];
    Q.vorgeladen = null; speichern(); return grossMalen();
  }
  if ((n = hit('data-mu-gehzu'))) {
    const t = laeuft();
    if (!t) return;
    grossZu();
    return n.dataset.muGehzu === 'album' ? C.go('musikAlbum', { id: t.albumId }) : C.go('musikInterpret', { id: t.interpretId });
  }
}

function kontextVon(knopf) {
  // Der Titel gehört zu der Liste, in der er steht — in der Suche etwa zu
  // den Treffern, nicht zur ganzen Seite.
  const ids = [...(knopf.closest('.mu-titelliste')?.querySelectorAll('[data-mu-zeile]') || [])].map(z => z.dataset.muZeile);
  const liste = ids.map(id => titelNachId.get(id)).filter(Boolean);
  return liste.length ? liste : kontextListe;
}

async function absenden(e) {
  const f = e.target;
  if (f.id === 'mu-listeform') {
    e.preventDefault(); e.stopPropagation();
    const name = f.name.value.trim();
    if (!name) return;
    try { const l = await C.api.call('/musik/listen', { method: 'POST', body: { name } }); C.go('musikListe', { id: l.id }); }
    catch (err) { C.toast(err.message); }
  }
  if (f.id === 'mu-umbenennen') {
    e.preventDefault(); e.stopPropagation();
    try { await C.api.call('/musik/listen/' + encodeURIComponent(C.S.id), { method: 'POST', body: { name: f.name.value } }); }
    catch (err) { C.toast(err.message); }
    C.S.muListeUmbenennen = false;
    C.load();
  }
}

function tipptGerade(e) {
  const z = e.target;
  return z && z.tagName && (z.isContentEditable || ['INPUT', 'TEXTAREA', 'SELECT'].includes(z.tagName)) && z.type !== 'range';
}

function taste(e) {
  if (e.key === 'Escape' && menue) { menue._fertig?.(null); menueZu(); return; }
  if (!laeuft() || tipptGerade(e) || C.S.view === 'player' || document.querySelector('.fo-betrachter:not([hidden])')) return;
  if (e.key === 'Escape' && Q.offen) { e.preventDefault(); return grossZu(); }
  if (e.key === ' ' && !e.target.closest?.('button')) { e.preventDefault(); return umschalten(); }
  if (Q.offen && e.key === 'ArrowRight' && e.shiftKey) { e.preventDefault(); return naechster(); }
  if (Q.offen && e.key === 'ArrowLeft' && e.shiftKey) { e.preventDefault(); return vorheriger(); }
}

/* Ziehen zum Ordnen — in der Warteschlange und in eigenen Listen. */
let gezogen = null;
document.addEventListener('dragstart', e => {
  const z = e.target.closest?.('[data-mu-sz], .mu-zeile[draggable="true"]');
  if (!z) return;
  gezogen = z;
  z.classList.add('mu-zieht');
  e.dataTransfer.effectAllowed = 'move';
  try { e.dataTransfer.setData('text/plain', ''); } catch {}
});
document.addEventListener('dragover', e => {
  if (!gezogen) return;
  const ziel = e.target.closest?.('[data-mu-sz], .mu-zeile[draggable="true"]');
  if (!ziel || ziel === gezogen || ziel.parentNode !== gezogen.parentNode) return;
  e.preventDefault();
  const r = ziel.getBoundingClientRect();
  ziel.parentNode.insertBefore(gezogen, e.clientY > r.top + r.height / 2 ? ziel.nextSibling : ziel);
});
document.addEventListener('dragend', async () => {
  const z = gezogen;
  gezogen = null;
  if (!z) return;
  z.classList.remove('mu-zieht');
  if (z.dataset.muSz != null) {
    const von = Number(z.dataset.muSz);
    const reihe = [...z.parentNode.querySelectorAll('[data-mu-sz]')];
    const nach = Q.pos + 1 + reihe.indexOf(z);
    return verschieben(von, nach);
  }
  const ids = [...z.parentNode.querySelectorAll('.mu-zeile')].map(x => x.dataset.muZeile);
  try { await C.api.call(`/musik/listen/${encodeURIComponent(C.S.id)}/reihenfolge`, { method: 'POST', body: { titel: ids } }); C.load(); }
  catch (err) { C.toast(err.message); }
});

addEventListener('pagehide', speichern);
