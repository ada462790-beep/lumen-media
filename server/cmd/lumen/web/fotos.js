// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

/* Fotos — Mediathek nach Jahren, Monaten und Tagen, Alben (Ordner) und
 * ein Betrachter im Vollbild mit Wischen, Zoom und Diashow.
 *
 * Der Betrachter hängt am <body>, nicht in #app: so übersteht er ein
 * Neuzeichnen, und die Seite darunter behält ihre Stelle.
 */

import { bereich } from './musik.js';
import { tr, texte, sprache, zahl } from './sprache.js';

let C = null;

const T = texte({
  fotos: 'Fotos', mediathek: 'Mediathek', alben: 'Alben', jahre: 'Jahre', monate: 'Monate', tage: 'Tage',
  alle: 'Alle Fotos', diashow: 'Diashow', laden: 'Herunterladen', info: 'Info', leer: 'Noch keine Fotos.',
});
// Monatsnamen aus Intl: de-AT sagt „Jänner“, Englisch „January“.
const monat = m => new Date(Date.UTC(2000, m - 1, 15)).toLocaleDateString(sprache(), { month: 'long', timeZone: 'UTC' });
const FOTO_ANSICHTEN = ['fotos', 'fotoAlben', 'fotoAlbum'];

const IC = {
  foto: '<rect x="3" y="5" width="18" height="14" rx="3"/><circle cx="9" cy="10" r="1.8"/><path d="M21 16l-5-5-8 8"/>',
  links: '<path d="M15 5l-7 7 7 7"/>', rechts: '<path d="M9 5l7 7-7 7"/>',
  info: '<circle cx="12" cy="12" r="9"/><path d="M12 11v6M12 7.5v.01"/>',
  laden: '<path d="M12 4v11M7 10l5 5 5-5M5 20h14"/>',
  play: '<path d="M8 5v14l11-7z"/>', pause: '<path d="M8 5h3v14H8zM13 5h3v14h-3z"/>',
  zoom: '<circle cx="11" cy="11" r="6.5"/><path d="M16 16l4 4M8.5 11h5M11 8.5v5"/>',
  video: '<path d="M4 7h11v10H4zM15 10l5-3v10l-5-3"/>',
};

const esc = s => C.esc(s);
const svg = (d, s, w) => C.svg(d, s, w);
const mitKey = url => {
  if (!url) return '';
  const k = C.api.keyParam();
  return k ? url + (url.includes('?') ? '&' : '?') + k : url;
};

export function einrichten(ctx) {
  C = ctx;
  betrachterAufbauen();
  document.addEventListener('click', klick);
  document.addEventListener('keydown', taste);
}

export function nav() {
  return bereich().fotos ? [['fotos', IC.foto, T.fotos, FOTO_ANSICHTEN]] : [];
}
export function ichZeile(linkRow) {
  return bereich().fotos ? linkRow(T.fotos, tr('Mediathek und Alben'), 'fotos') : '';
}

/* Die Verwaltung: ein Schalter für den Aufnahmeort. */
export function adminAbschnitt(d) {
  if (!d) return '';
  return `<section style="margin-top:26px"><div class="section-h">${tr('Fotos')}</div>
    <div class="group group--inset">
      <div class="row" style="min-height:54px"><span style="flex:1"><span class="t-body">${tr('Aufnahmeort anzeigen')}</span>
        <span class="t-caption c-2" style="display:block">${tr('Viele Fotos tragen den Ort, an dem sie entstanden sind. Voreingestellt bleibt er auf dem Server.')}</span>
        ${d.darfAendern === false ? `<span class="t-caption c-2" style="display:block">${tr('Diese Einstellung ändert nur der Besitzer des Servers.')}</span>` : ''}</span>
        <button class="switch" role="switch" aria-checked="${!!d.fotoOrtZeigen}" ${d.darfAendern === false ? 'disabled' : 'data-fo-ortschalter'} aria-label="${tr('Aufnahmeort anzeigen')}"><i></i></button></div>
    </div></section>`;
}

/* ---------------------------------------------------------------- Laden */

const SEITE = 240;

export const ansichten = {
  fotos: {
    laden: async () => {
      artAusAdresse();
      const art = C.S.foArt || 'tage';
      artInAdresse();
      const u = await C.api.call('/fotos/uebersicht');
      if (art === 'jahre' || art === 'monate') return { ...u, art };
      const f = C.S.foFilter || {};
      const q = new URLSearchParams({ limit: SEITE });
      if (f.jahr) q.set('jahr', f.jahr);
      if (f.monat) q.set('monat', f.monat);
      const l = await C.api.call('/fotos/liste?' + q);
      return { ...u, art, liste: l.fotos, gesamtListe: l.gesamt, weiter: l.weiter };
    },
    zeichnen: () => vMediathek(),
    nachher: () => nachladenEinrichten(),
  },
  fotoAlben: { laden: () => C.api.call('/fotos/alben'), zeichnen: () => vAlben() },
  fotoAlbum: {
    laden: async () => {
      const [a, l] = await Promise.all([C.api.call('/fotos/alben/' + encodeURIComponent(C.S.id)),
        C.api.call(`/fotos/liste?album=${encodeURIComponent(C.S.id)}&limit=${SEITE}`)]);
      return { album: a, liste: l.fotos, gesamtListe: l.gesamt, weiter: l.weiter };
    },
    zeichnen: () => vAlbum(),
    nachher: () => nachladenEinrichten(),
  },
};

export function nachRender() {
  const a = ansichten[C.S.view];
  if (a?.nachher) a.nachher();
}

/* Jahre, Monate, Tage und der Filter darauf stehen in der Adresse
   (#/fotos?art=monate&jahr=2024) – so führen F5 und ein Lesezeichen
   genau dorthin zurück. Ein neuer Eintrag im Verlauf ist ein Wechsel
   hier nicht; die Adresse wird nur nachgeführt. */
const ARTEN = ['jahre', 'monate', 'tage'];
function adressTeile() {
  const m = /^#\/fotos\?(.*)$/.exec(location.hash || '');
  return m ? new URLSearchParams(m[1]) : null;
}
function artAusAdresse() {
  const p = adressTeile();
  if (!p) return;
  const art = p.get('art');
  if (!ARTEN.includes(art)) return;
  const zahlOder = (k, min, max) => { const n = Number(p.get(k)); return Number.isInteger(n) && n >= min && n <= max ? n : undefined; };
  const f = { jahr: zahlOder('jahr', 1800, 3000), monat: zahlOder('monat', 1, 12) };
  if (!f.jahr) delete f.jahr;
  if (!f.monat || !f.jahr) delete f.monat;
  C.S.foArt = art;
  C.S.foFilter = art === 'jahre' ? {} : f;
}
function artInAdresse() {
  if (C.S.view !== 'fotos') return;
  const p = new URLSearchParams();
  const art = C.S.foArt || 'tage';
  const f = C.S.foFilter || {};
  if (art !== 'tage' || f.jahr) p.set('art', art);
  if (f.jahr) p.set('jahr', f.jahr);
  if (f.monat) p.set('monat', f.monat);
  const ziel = '#/fotos' + (p.size ? '?' + p : '');
  if (location.hash !== ziel) try { history.replaceState(history.state, '', ziel); } catch {}
}

function artWechseln() { artInAdresse(); return C.load(); }

/* ---------------------------------------------------------------- Ansichten */

function kopf(aktiv) {
  return `<div class="fo-kopf">
    <h1 class="t-large" style="margin:0">${T.fotos}</h1>
    <nav class="mu-reiter" aria-label="${T.fotos}" style="margin:0">
      <button class="tab-pill" aria-pressed="${aktiv === 'fotos'}" data-go="fotos">${T.mediathek}</button>
      <button class="tab-pill" aria-pressed="${aktiv !== 'fotos'}" data-go="fotoAlben">${T.alben}</button></nav></div>`;
}

const kachel = (f, i, klasse = '') => `
  <button class="fo-kachel ${klasse}" data-fo-oeffnen="${i}" aria-label="${f.art === 'video' ? tr('Video') : tr('Foto')} vom ${esc(datumLang(f.aufnahme))}">
    <img src="${esc(mitKey(f.klein))}" alt="" loading="lazy" decoding="async" data-weg>
    ${f.art === 'video' ? `<span class="fo-videozeit">${svg(IC.video, 12, 2)} ${dauer(f.dauerMs)}</span>` : ''}</button>`;

function dauer(ms) { const s = Math.round((ms || 0) / 1000); return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')}`; }
// Die Zeit steht als UTC in der Datenbank, gemeint ist aber die Uhrzeit auf
// der Kamera — darum hier auch als UTC lesen, nicht in die Zone umrechnen.
const zeitAus = iso => new Date(iso);
const datumLang = iso => zeitAus(iso).toLocaleDateString(sprache(), { timeZone: 'UTC', weekday: 'long', day: 'numeric', month: 'long', year: 'numeric' });
const uhrzeit = iso => zeitAus(iso).toLocaleTimeString(sprache(), { timeZone: 'UTC', hour: '2-digit', minute: '2-digit' });
const tagSchluessel = iso => iso.slice(0, 10);

function vMediathek() {
  const d = C.S.data || {}, art = d.art || 'tage';
  if (!d.gesamt) return C.shell(`<div class="wrap wrap--flat fo-seite">${kopf('fotos')}
    <div class="mu-leer">${svg(IC.foto, 44, 1.4)}<p class="t-body">${T.leer}</p>
    <p class="hint">${tr('Die Verwaltung legt unter „Bibliotheken“ eine Bibliothek der Art „Fotos“ an.')}</p></div></div>`);
  const f = C.S.foFilter || {};
  const filterText = f.monat ? `${monat(f.monat)} ${f.jahr}` : f.jahr ? String(f.jahr) : '';
  const segment = `<div class="mu-segment fo-segment" role="group" aria-label="${tr('Ansicht')}">${[['jahre', T.jahre], ['monate', T.monate], ['tage', T.tage], ['alle', T.alle]].map(([v, n]) =>
    `<button class="tab-pill" aria-pressed="${art === v}" data-fo-art="${v}">${n}</button>`).join('')}</div>`;
  let inhalt = '';
  if (art === 'jahre') {
    inhalt = `<div class="fo-jahre">${(d.jahre || []).map(j => `
      <button class="fo-jahr" data-fo-jahr="${j.jahr}"><img src="${esc(mitKey(`/api/v1/medien/fotos/${encodeURIComponent(j.titelbild)}/bild?g=gross`))}" alt="" loading="lazy" data-weg>
        <span class="fo-jahr-text"><span class="t-large">${j.jahr}</span><span class="t-sub">${tr('{n} Objekte', { n: j.anzahl })}</span></span></button>`).join('')}</div>`;
  } else if (art === 'monate') {
    const monate = (d.monate || []).filter(m => !f.jahr || m.jahr === f.jahr);
    inhalt = `<div class="fo-monate">${monate.map(m => `
      <button class="fo-monat" data-fo-monat="${m.jahr}-${m.monat}">
        <span class="fo-monat-bilder fo-monat-bilder--${Math.min(4, m.titelbilder.length)}">${m.titelbilder.slice(0, 4).map(id =>
          `<img src="${esc(mitKey(`/api/v1/medien/fotos/${encodeURIComponent(id)}/bild?g=klein`))}" alt="" loading="lazy" data-weg>`).join('')}</span>
        <span class="t-headline" style="margin-top:8px;display:block">${monat(m.monat)} ${m.jahr}</span>
        <span class="t-sub c-2">${tr('{n} Objekte', { n: m.anzahl })}</span></button>`).join('')}</div>`;
  } else {
    inhalt = raster(d.liste || [], art === 'tage');
  }
  return C.shell(`<div class="wrap wrap--flat fo-seite">${kopf('fotos')}
    <div class="fo-leiste">${segment}<span style="flex:1"></span>
      ${filterText && (art === 'tage' || art === 'alle' || art === 'monate') ? `<button class="btn btn-pill btn-pill--fill" data-fo-filterweg>${esc(filterText)} ${svg(C.I.x, 13, 2.4)}</button>` : ''}
      ${(art === 'tage' || art === 'alle') && (d.liste || []).length ? `<button class="btn btn-pill btn-pill--fill" data-fo-diashow>${svg(IC.play, 13, 2)} ${T.diashow}</button>` : ''}</div>
    ${inhalt}
    ${d.weiter ? '<div class="fo-mehr" data-fo-mehr><span class="spin spin--s"></span></div>' : ''}
    <div style="height:40px"></div></div>`);
}

function raster(fotos, nachTagen) {
  if (!fotos.length) return `<p class="hint">${T.leer}</p>`;
  if (!nachTagen) return `<div class="fo-raster fo-raster--dicht">${fotos.map((f, i) => kachel(f, i)).join('')}</div>`;
  const gruppen = [];
  fotos.forEach((f, i) => {
    const k = tagSchluessel(f.aufnahme);
    if (!gruppen.length || gruppen[gruppen.length - 1].k !== k) gruppen.push({ k, f, teile: [] });
    gruppen[gruppen.length - 1].teile.push(kachel(f, i));
  });
  return gruppen.map(g => `<section class="fo-tag"><h2 class="fo-tag-kopf t-headline">${esc(datumLang(g.f.aufnahme))}</h2>
    <div class="fo-raster">${g.teile.join('')}</div></section>`).join('');
}

function vAlben() {
  const alben = C.S.data?.alben || [];
  return C.shell(`<div class="wrap wrap--flat fo-seite">${kopf('fotoAlben')}
    ${alben.length ? `<div class="fo-alben">${alben.map(a => `
      <button class="card fo-album" data-go="fotoAlbum" data-id="${esc(a.id)}" aria-label="${esc(a.name)}">
        <span class="fo-album-bild thumb">${a.bild ? `<img src="${esc(mitKey(a.bild))}" alt="" loading="lazy" data-weg>` : ''}</span>
        <span class="t-body clamp-1" style="display:block;margin-top:8px">${esc(a.name)}</span>
        <span class="t-sub c-2">${a.anzahl}</span></button>`).join('')}</div>`
      : `<p class="hint">${tr('Jeder Ordner mit Fotos wird zu einem Album.')}</p>`}
    <div style="height:40px"></div></div>`);
}

function vAlbum() {
  const d = C.S.data;
  if (!d) return C.shell(`<div class="wrap">${C.loading()}</div>`);
  const a = d.album;
  const von = a.von ? new Date(a.von * 1000) : null, bis = a.bis ? new Date(a.bis * 1000) : null;
  const opt = { timeZone: 'UTC', day: 'numeric', month: 'long', year: 'numeric' };
  const zeitraum = von ? (von.toDateString() === bis.toDateString() ? von.toLocaleDateString(sprache(), opt)
    : `${von.toLocaleDateString(sprache(), opt)} – ${bis.toLocaleDateString(sprache(), opt)}`) : '';
  return C.shell(`<div class="wrap wrap--flat fo-seite">
    <button class="btn btn-pill btn-pill--fill mu-zurueck" data-back>${svg(C.I.back, 16, 2)}${tr('Zurück')}</button>
    <h1 class="t-large" style="margin:14px 0 2px">${esc(a.name)}</h1>
    <div class="fo-leiste"><span class="t-sub c-2">${tr('{n} Objekte', { n: a.anzahl })}${zeitraum ? ' · ' + esc(zeitraum) : ''}${a.pfad.includes('/') ? ' · ' + esc(a.pfad) : ''}</span>
      <span style="flex:1"></span><button class="btn btn-pill btn-pill--fill" data-fo-diashow>${svg(IC.play, 13, 2)} ${T.diashow}</button></div>
    ${raster(d.liste || [], false)}
    ${d.weiter ? '<div class="fo-mehr" data-fo-mehr><span class="spin spin--s"></span></div>' : ''}
    <div style="height:40px"></div></div>`);
}

/* Weitere Fotos, sobald man ans Ende kommt. */
let beobachter = null, laedtNach = false;
function nachladenEinrichten() {
  beobachter?.disconnect();
  const marke = document.querySelector('[data-fo-mehr]');
  if (!marke) return;
  beobachter = new IntersectionObserver(async ein => {
    if (!ein.some(x => x.isIntersecting) || laedtNach) return;
    laedtNach = true;
    try {
      const d = C.S.data, q = new URLSearchParams({ limit: SEITE, offset: d.liste.length });
      if (C.S.view === 'fotoAlbum') q.set('album', C.S.id);
      else {
        const f = C.S.foFilter || {};
        if (f.jahr) q.set('jahr', f.jahr);
        if (f.monat) q.set('monat', f.monat);
      }
      const l = await C.api.call('/fotos/liste?' + q);
      d.liste = [...d.liste, ...l.fotos]; d.weiter = l.weiter;
      const seite = document.querySelector('.page'), y = seite?.scrollTop;
      C.render();
      if (seite && y != null) document.querySelector('.page').scrollTop = y;
    } catch {}
    laedtNach = false;
  }, { root: document.querySelector('.page'), rootMargin: '900px' });
  beobachter.observe(marke);
}

/* ---------------------------------------------------------------- Betrachter */

const B = { liste: [], i: 0, zoom: 1, x: 0, y: 0, info: false, diashow: null, tempo: 4, detail: null };
let bx = null;

function betrachterAufbauen() {
  bx = document.createElement('div');
  bx.className = 'fo-betrachter';
  bx.hidden = true;
  bx.setAttribute('role', 'dialog');
  bx.setAttribute('aria-modal', 'true');
  bx.setAttribute('aria-label', tr('Foto'));
  document.body.appendChild(bx);

  // Wischen, Ziehen beim Zoomen, Zwei-Finger-Zoom.
  const finger = new Map();
  let start = null, abstand0 = 0, zoom0 = 1, bewegt = false;
  bx.addEventListener('pointerdown', e => {
    if (!e.target.closest('.fo-buehne') || e.target.closest('button')) return;
    // Über einem Video bleibt die Steuerleiste unten dem Video.
    const v = e.target.closest('video');
    if (v && e.clientY > v.getBoundingClientRect().bottom - 70) return;
    finger.set(e.pointerId, { x: e.clientX, y: e.clientY });
    bx.setPointerCapture?.(e.pointerId);
    bewegt = false;
    if (finger.size === 1) start = { x: e.clientX, y: e.clientY, bx: B.x, by: B.y, t: Date.now() };
    if (finger.size === 2) { const [a, b] = [...finger.values()]; abstand0 = Math.hypot(a.x - b.x, a.y - b.y); zoom0 = B.zoom; }
  });
  bx.addEventListener('pointermove', e => {
    if (!finger.has(e.pointerId)) return;
    finger.set(e.pointerId, { x: e.clientX, y: e.clientY });
    if (finger.size === 2) {
      const [a, b] = [...finger.values()];
      B.zoom = Math.min(5, Math.max(1, zoom0 * Math.hypot(a.x - b.x, a.y - b.y) / (abstand0 || 1)));
      if (B.zoom === 1) { B.x = B.y = 0; }
      bewegt = true;
      return bildSetzen();
    }
    if (!start) return;
    const dx = e.clientX - start.x, dy = e.clientY - start.y;
    if (Math.abs(dx) + Math.abs(dy) > 6) bewegt = true;
    if (B.zoom > 1) { B.x = start.bx + dx; B.y = start.by + dy; return bildSetzen(); }
    const bild = bx.querySelector('.fo-bild');
    if (bild) { bild.style.transition = 'none'; bild.style.transform = `translate(${dx}px, ${Math.max(0, dy)}px)`; bild.style.opacity = String(1 - Math.max(0, dy) / 600); }
  });
  const loslassen = e => {
    if (!finger.has(e.pointerId)) return;
    finger.delete(e.pointerId);
    if (finger.size || !start) return;
    const dx = e.clientX - start.x, dy = e.clientY - start.y, schnell = Date.now() - start.t < 350;
    start = null;
    if (B.zoom > 1) return;
    const bild = bx.querySelector('.fo-bild');
    if (bild) { bild.style.transition = ''; bild.style.transform = ''; bild.style.opacity = ''; }
    if (!bewegt) return;
    if (dy > 120 && Math.abs(dy) > Math.abs(dx)) return zu();
    if (Math.abs(dx) > (schnell ? 40 : 110)) blaettern(dx < 0 ? 1 : -1);
  };
  bx.addEventListener('pointerup', loslassen);
  bx.addEventListener('pointercancel', loslassen);
  bx.addEventListener('dblclick', e => {
    if (!e.target.closest('.fo-bild') || e.target.closest('video')) return;
    zoomen(B.zoom > 1 ? 1 : 2.5, e);
  });
  bx.addEventListener('wheel', e => {
    if (!e.target.closest('.fo-buehne')) return;
    e.preventDefault();
    zoomen(Math.min(5, Math.max(1, B.zoom * (e.deltaY < 0 ? 1.15 : 1 / 1.15))), e);
  }, { passive: false });
}

function zoomen(z, e) {
  const alt = B.zoom;
  B.zoom = z;
  if (z === 1) { B.x = B.y = 0; }
  else if (e) {
    // Zur Stelle unter dem Zeiger hin zoomen.
    const r = bx.querySelector('.fo-buehne').getBoundingClientRect();
    const cx = e.clientX - r.left - r.width / 2, cy = e.clientY - r.top - r.height / 2;
    B.x = cx - (cx - B.x) * (z / alt); B.y = cy - (cy - B.y) * (z / alt);
  }
  bildSetzen();
}

function bildSetzen() {
  const img = bx.querySelector('.fo-bild img');
  if (img) {
    img.style.transform = `translate(${B.x}px, ${B.y}px) scale(${B.zoom})`;
    img.style.cursor = B.zoom > 1 ? 'grab' : 'zoom-in';
  }
}

function oeffnen(liste, i) {
  B.liste = liste; B.i = i; B.zoom = 1; B.x = B.y = 0;
  bx.hidden = false;
  document.body.classList.add('fo-offen');
  malen();
  bx.querySelector('[data-fo-zu]')?.focus();
}

function zu() {
  diashowAus();
  bx.querySelector('video')?.pause();
  bx.hidden = true;
  bx.innerHTML = '';
  document.body.classList.remove('fo-offen');
  // Zur Kachel des zuletzt gezeigten Fotos — wer blättert, landet dort.
  document.querySelector(`[data-fo-oeffnen="${B.i}"]`)?.scrollIntoView({ block: 'nearest' });
}

function blaettern(r) {
  const n = B.i + r;
  if (n < 0 || n >= B.liste.length) {
    if (B.diashow && n >= B.liste.length) { B.i = 0; return malen(true); }
    return;
  }
  B.i = n; B.zoom = 1; B.x = B.y = 0;
  malen(true);
}

const kannHevc = () => document.createElement('video').canPlayType('video/mp4; codecs="hvc1"') !== '';

function malen(weich = false) {
  const f = B.liste[B.i];
  if (!f) return zu();
  const vor = B.liste[B.i + 1];
  const video = f.art === 'video'
    ? `<video class="fo-video" controls playsinline autoplay src="${esc(mitKey(f.video + (f.videocodec === 'hevc' && !kannHevc() ? '?als=h264' : '')))}" poster="${esc(mitKey(f.gross))}"></video>` : '';
  const ort = f.breitengrad != null && f.laengengrad != null;
  bx.innerHTML = `
    <div class="fo-oben">
      <button class="btn btn-round glass glass--clear" data-fo-zu aria-label="${tr('Schliessen')}">${svg(C.I.x, 20, 2.2)}</button>
      <div class="fo-titel"><div class="t-headline">${esc(datumLang(f.aufnahme))}</div><div class="t-caption">${esc(uhrzeit(f.aufnahme))} · ${tr('{nr} von {n}', { nr: B.i + 1, n: B.liste.length })}</div></div>
      <span style="flex:1"></span>
      ${B.diashow ? `<div class="mu-segment fo-tempo" role="group" aria-label="${tr('Tempo')}">${[2, 4, 7].map(s =>
        `<button class="tab-pill" aria-pressed="${B.tempo === s}" data-fo-tempo="${s}">${s} s</button>`).join('')}</div>` : ''}
      <button class="btn btn-round glass glass--clear" data-fo-diashow aria-pressed="${!!B.diashow}" aria-label="${B.diashow ? tr('Diashow anhalten') : T.diashow}" title="${T.diashow}">${svg(B.diashow ? IC.pause : IC.play, 18, 2)}</button>
      ${f.art === 'bild' ? `<button class="btn btn-round glass glass--clear fo-nur-breit" data-fo-zoom aria-label="${tr('Zoomen')}" title="${tr('Zoomen (Doppelklick)')}">${svg(IC.zoom, 19, 2)}</button>` : ''}
      <button class="btn btn-round glass glass--clear" data-fo-info aria-pressed="${B.info}" aria-label="${T.info}" title="${T.info}">${svg(IC.info, 19, 2)}</button>
      ${f.original ? `<a class="btn btn-round glass glass--clear" href="${esc(mitKey(f.original))}" download aria-label="${T.laden}" title="${T.laden}">${svg(IC.laden, 19, 2)}</a>` : ''}
    </div>
    <div class="fo-buehne">
      ${B.i > 0 ? `<button class="fo-pfeil fo-pfeil--l" data-fo-blaettern="-1" aria-label="${tr('Vorheriges')}">${svg(IC.links, 26, 2.2)}</button>` : ''}
      <div class="fo-bild${weich ? ' fo-bild--weich' : ''}">${video || `<img src="${esc(mitKey(f.gross))}" alt="" draggable="false">`}</div>
      ${B.i < B.liste.length - 1 ? `<button class="fo-pfeil fo-pfeil--r" data-fo-blaettern="1" aria-label="${tr('Nächstes')}">${svg(IC.rechts, 26, 2.2)}</button>` : ''}
    </div>
    ${B.info ? `<aside class="fo-info glass glass--thick">
      <div class="t-headline">${esc(datumLang(f.aufnahme))}, ${esc(uhrzeit(f.aufnahme))}</div>
      <div class="t-caption c-2">${f.datumQuelle === 'exif' || f.datumQuelle === 'video' ? tr('Aufnahmezeit') : f.datumQuelle === 'name' ? tr('Datum aus dem Dateinamen') : tr('Datum der Datei')}</div>
      ${f.kamera ? `<div class="t-body" style="margin-top:12px">${esc(f.kamera)}</div>` : ''}
      <div class="t-sub c-2" style="margin-top:6px">${[f.breite && f.hoehe ? `${f.breite} × ${f.hoehe}` : '', f.format?.toUpperCase(),
        f.groesse ? (f.groesse < 1048576 ? Math.max(1, Math.round(f.groesse / 1024)) + ' KB' : zahl(f.groesse / 1048576, { maximumFractionDigits: 1 }) + ' MB') : '', f.art === 'video' ? dauer(f.dauerMs) : ''].filter(Boolean).map(esc).join(' · ')}</div>
      ${ort ? `<div class="t-sub" style="margin-top:12px">${f.breitengrad.toFixed(4)}, ${f.laengengrad.toFixed(4)}</div>` : ''}
    </aside>` : ''}
    ${vor && vor.art === 'bild' ? `<link rel="prefetch" href="${esc(mitKey(vor.gross))}">` : ''}`;
  bildSetzen();
}

function diashowAn() {
  diashowAus();
  B.diashow = setInterval(() => blaettern(1), B.tempo * 1000);
  malen();
}
function diashowAus() { if (B.diashow) clearInterval(B.diashow); B.diashow = null; }

/* ---------------------------------------------------------------- Ereignisse */

async function klick(e) {
  const el = e.target instanceof Element ? e.target : null;
  if (!el) return;
  const hit = sel => el.closest(`[${sel}]`);
  let n;
  if ((n = hit('data-fo-oeffnen'))) return oeffnen(C.S.data?.liste || [], Number(n.dataset.foOeffnen));
  if ((n = hit('data-fo-art'))) {
    C.S.foArt = n.dataset.foArt;
    if (C.S.foArt === 'jahre') C.S.foFilter = {};
    return artWechseln();
  }
  if ((n = hit('data-fo-jahr'))) { C.S.foArt = 'monate'; C.S.foFilter = { jahr: Number(n.dataset.foJahr) }; return artWechseln(); }
  if ((n = hit('data-fo-monat'))) {
    const [j, m] = n.dataset.foMonat.split('-').map(Number);
    C.S.foArt = 'tage'; C.S.foFilter = { jahr: j, monat: m }; return artWechseln();
  }
  if (hit('data-fo-filterweg')) { C.S.foFilter = {}; return artWechseln(); }
  if ((n = hit('data-fo-ortschalter'))) {
    const an = n.getAttribute('aria-checked') !== 'true';
    try {
      await C.api.call('/admin/mediathek', { method: 'PUT', body: { fotoOrtZeigen: an } });
      n.setAttribute('aria-checked', String(an));
      C.toast(an ? tr('Der Aufnahmeort wird angezeigt') : tr('Der Aufnahmeort bleibt auf dem Server'));
    } catch (err) { C.toast(err.message); }
    return;
  }
  if (bx.hidden) {
    if (hit('data-fo-diashow') && (C.S.data?.liste || []).length) { oeffnen(C.S.data.liste, 0); return diashowAn(); }
    return;
  }
  // Im Betrachter
  if (hit('data-fo-zu')) return zu();
  if ((n = hit('data-fo-blaettern'))) return blaettern(Number(n.dataset.foBlaettern));
  if (hit('data-fo-info')) { B.info = !B.info; return malen(); }
  if (hit('data-fo-zoom')) return zoomen(B.zoom > 1 ? 1 : 2.5);
  if (hit('data-fo-diashow')) return B.diashow ? (diashowAus(), malen()) : diashowAn();
  if ((n = hit('data-fo-tempo'))) { B.tempo = Number(n.dataset.foTempo); return diashowAn(); }
}

function taste(e) {
  if (bx.hidden) return;
  if (e.target?.tagName === 'INPUT') return;
  const k = e.key;
  if (k === 'Escape') { e.preventDefault(); return B.zoom > 1 ? zoomen(1) : zu(); }
  if (k === 'ArrowRight') { e.preventDefault(); return blaettern(1); }
  if (k === 'ArrowLeft') { e.preventDefault(); return blaettern(-1); }
  if (k === ' ' && !e.target.closest?.('video')) { e.preventDefault(); return B.diashow ? (diashowAus(), malen()) : diashowAn(); }
  if (k === 'i') { B.info = !B.info; return malen(); }
  if (k === '+' || k === '=') return zoomen(Math.min(5, B.zoom * 1.5));
  if (k === '-') return zoomen(Math.max(1, B.zoom / 1.5));
}
