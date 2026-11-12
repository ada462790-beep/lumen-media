// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

/* Eigene Sammlungen: von der Verwaltung zusammengestellt, für alle
 * sichtbar — jeder sieht darin nur, was er ohnehin sehen darf.
 *
 * Sie stehen im Bereich „Filmreihen" als zweiter Reiter, nicht als eigener
 * Eintrag in der Seitenleiste: Für die Leute im Haushalt ist beides
 * dasselbe — Filme, die zusammengehören. Zusammenstellen geht nur am
 * Schreibtisch (Verwaltung). */

import { api } from './api.js';
import { U, escH, fenster, ladeHinweis, meldung, fehlerText, beschaeftigt, hochladen, amSchreibtisch } from './fenster.js';
import { tr, texte } from './sprache.js';

export const sammlungenLaden = () => api.call('/sammlungen');
export const sammlungLaden = id => api.call('/sammlungen/' + encodeURIComponent(id));

const verwalten = () => !!U.S?.me?.account?.isAdmin && amSchreibtisch();
const mitSchluessel = url => url + (url.includes('?') ? '&' : '?') + api.bildParam();
const SORT_NAME = texte({ hand: 'von Hand sortiert', jahr: 'nach Jahr', titel: 'nach Titel' });

/* „Filmreihen | Sammlungen" — oben auf beiden Seiten. */
// Wie bei der Merkliste: ein Segment unter dem grossen Titel, das die
// Seite wechselt (darum aria-current).
export function reiter(aktiv) {
  return `<nav class="segment" aria-label="${tr('Filmreihen oder Sammlungen')}">${
    [['collections', tr('Filmreihen')], ['sammlungen', tr('Sammlungen')]].map(([v, n]) =>
      `<button type="button" class="segment-knopf" data-go="${v}"${aktiv === v ? ' aria-current="page"' : ''}>${n}</button>`).join('')}</nav>`;
}

function titelAnzahl(n) { return tr('{n} Titel', { n }); }

export function sammlungKarte(x) {
  const { artOf } = U;
  const bild = x.bild ? `<div class="reihe-blatt reihe-blatt--0"><img class="art-img" src="${escH(mitSchluessel(x.bild))}" alt="" loading="lazy"></div>` : '';
  const blaetter = (x.items || []).slice(0, x.bild ? 2 : 3);
  const hinten = blaetter.map((it, i) => `<div class="reihe-blatt reihe-blatt--${i + (x.bild ? 1 : 0)}">${artOf(it)}</div>`).reverse().join('');
  const leer = !x.bild && !blaetter.length ? `<div class="reihe-blatt reihe-blatt--0"><div class="art-none"><span>${escH(x.name)}</span></div></div>` : '';
  return `<button class="card reihe-karte" data-go="sammlung" data-id="${escH(x.id)}" aria-label="${escH(x.name)}, ${titelAnzahl(x.anzahl)}">
    <div class="reihe-stapel" aria-hidden="true">${hinten}${bild}${leer}</div>
    <div class="t-body clamp-1" style="margin-top:10px">${escH(x.name)}</div>
    <div class="t-sub c-2">${x.anzahl ? titelAnzahl(x.anzahl) : tr('noch leer')}</div>
  </button>`;
}

export function sammlungenSeite() {
  const { S, shell } = U;
  const liste = S.data?.sammlungen || [];
  return shell(`<div class="wrap wrap--flat">
    <h1 class="t-large">${tr('Sammlungen')}</h1>
    ${reiter('sammlungen')}
    <div class="t-sub c-2" style="margin:4px 0 22px">
      ${liste.length ? `${tr('{n} Sammlungen', { n: liste.length })} · ${tr('von Hand zusammengestellt')}` : ''}</div>
    ${verwalten() ? `<div style="margin:-6px 0 24px"><button class="btn btn-pill btn-pill--fill" data-sam-neu>${tr('+ Neue Sammlung')}</button></div>` : ''}
    ${liste.length ? `<div class="reihen-grid">${liste.map(sammlungKarte).join('')}</div>`
      : `<div class="group group--inset" style="max-width:620px"><p class="hint" style="padding:18px 16px;margin:0">
          ${tr('Noch keine Sammlungen.')} ${S.me?.account?.isAdmin
            ? tr('Leg eine an — etwa „Weihnachtsfilme“ oder „Marvel in Reihenfolge“ — und füge auf der Seite eines Films „Zu Sammlung hinzufügen“ dazu.')
            : tr('Die Verwaltung kann hier Filme und Serien zusammenstellen, etwa „Weihnachtsfilme“.')}</p></div>`}
    <div style="height:40px"></div></div>`);
}

export function sammlungSeite() {
  const { S, shell, posterCard, PLAY, svg, I } = U;
  const x = S.data;
  if (!x) return shell(`<div class="wrap">${U.loading()}</div>`);
  const admin = verwalten();
  const vonHand = new Set(x.vonHand || []);
  const naechster = x.items.find(t => !t.finished) || x.items[0];
  const unter = [titelAnzahl(x.anzahl), SORT_NAME[x.sortierung] || '',
    x.regel ? tr('ergänzt durch eine Regel') : '', x.aufStart ? tr('auf der Startseite') : ''].filter(Boolean).join(' · ');
  const handIds = x.items.filter(t => vonHand.has(t.id)).map(t => t.id);

  const karte = t => {
    if (!admin) return posterCard(t);
    const hand = vonHand.has(t.id);
    const i = handIds.indexOf(t.id);
    const knoepfe = hand ? `<div class="sammlung-zeile-knoepfe" style="margin-top:6px">
        ${x.sortierung === 'hand' ? `<button class="btn" data-sam-hoch="${escH(t.id)}" aria-label="${tr('{title} nach vorn', { title: escH(t.title) })}"${i <= 0 ? ' disabled' : ''}>${svg(I.back, 14, 2.2)}</button>
        <button class="btn" data-sam-runter="${escH(t.id)}" aria-label="${tr('{title} nach hinten', { title: escH(t.title) })}"${i >= handIds.length - 1 ? ' disabled' : ''}>${svg(I.chev, 14, 2.2)}</button>` : ''}
        <button class="btn" data-sam-weg="${escH(t.id)}" aria-label="${tr('{title} aus der Sammlung nehmen', { title: escH(t.title) })}" style="color:#ff6961">${svg(I.x, 14, 2.2)}</button></div>`
      : `<div class="durch-regel" style="margin-top:6px">${tr('durch die Regel')}</div>`;
    return `<div>${posterCard(t)}${knoepfe}</div>`;
  };
  const aktionen = [
    naechster ? `<button class="btn btn-primary" data-play="${escH(naechster.id)}">${PLAY(16)} ${tr('Ansehen')}</button>` : '',
    admin ? `<button class="btn btn-pill btn-pill--fill" data-sam-bearbeiten>${tr('Bearbeiten')}</button>` : '',
  ].filter(Boolean).join('');
  return shell(`<div class="wrap wrap--flat">
    <header style="display:flex;gap:24px;align-items:flex-end;flex-wrap:wrap">
      ${x.bild ? `<div style="position:relative;width:140px;aspect-ratio:2/3;border-radius:12px;overflow:hidden;flex:none">
          <img class="art-img" src="${escH(mitSchluessel(x.bild))}" alt=""></div>` : ''}
      <div style="flex:1;min-width:240px">
        <button class="btn btn-pill btn-pill--fill" style="height:32px;font-size:.8125rem;gap:6px" data-back>${svg(I.back, 16, 2)}${tr('Zurück')}</button>
        <h1 class="t-large" style="margin:14px 0 0">${escH(x.name)}</h1>
        <div class="t-sub c-2" style="margin-top:4px">${escH(unter)}</div>
        ${aktionen ? `<div style="display:flex;gap:10px;flex-wrap:wrap;margin-top:18px">${aktionen}</div>` : ''}
      </div>
    </header>
    ${x.beschreibung ? `<p class="t-body c-2" style="max-width:640px;line-height:1.6;margin-top:18px;white-space:pre-line">${escH(x.beschreibung)}</p>` : ''}
    ${x.items.length ? `<div class="grid" style="margin-top:30px">${x.items.map(karte).join('')}</div>`
      : `<div class="group group--inset" style="max-width:620px;margin-top:26px"><p class="hint" style="padding:18px 16px;margin:0">
          ${tr('Noch leer. Auf der Seite eines Films oder einer Serie: „Zu Sammlung hinzufügen“.')}</p></div>`}
    <div style="height:40px"></div></div>`);
}

/* Klicks auf den Sammlungsseiten. true = erledigt. */
export function sammlungKlick(e) {
  const { S } = U;
  const hit = sel => e.target.closest(`[${sel}]`);
  let n;
  if (hit('data-sam-neu')) { sammlungBearbeiten(null); return true; }
  if (hit('data-sam-bearbeiten') && S.data) { sammlungBearbeiten(S.data); return true; }
  if ((n = hit('data-sam-weg'))) {
    api.call(`/admin/sammlungen/${encodeURIComponent(S.id)}/titel/${encodeURIComponent(n.dataset.samWeg)}`, { method: 'DELETE' })
      .then(() => { U.toast(tr('Aus der Sammlung genommen')); U.load(); })
      .catch(err => U.toast(fehlerText(err)));
    return true;
  }
  if ((n = hit('data-sam-hoch')) || (n = hit('data-sam-runter'))) {
    const ids = (S.data?.vonHand || []).slice();
    const id = n.dataset.samHoch || n.dataset.samRunter;
    const i = ids.indexOf(id), j = n.dataset.samHoch ? i - 1 : i + 1;
    if (i < 0 || j < 0 || j >= ids.length) return true;
    [ids[i], ids[j]] = [ids[j], ids[i]];
    api.call(`/admin/sammlungen/${encodeURIComponent(S.id)}/reihenfolge`, { method: 'POST', body: { ids } })
      .then(() => U.load()).catch(err => U.toast(fehlerText(err)));
    return true;
  }
  return false;
}

/* ---------------------------------------------------------------- Fenster */

/* Anlegen (x = null) oder bearbeiten. */
export async function sammlungBearbeiten(x) {
  let geaendert = false, neueID = null;
  const d = fenster({ titel: x ? tr('Sammlung bearbeiten') : tr('Neue Sammlung'), zu: () => {
    if (neueID) U.go('sammlung', { id: neueID });
    else if (geaendert) U.load();
  } });
  d.inhalt.innerHTML = ladeHinweis();
  // Die Genres der Mediathek als Vorschläge für die Regel.
  const genres = await api.call('/items?seite=1&limit=1').then(r => r.genres || []).catch(() => []);
  const r = x?.regel || {};
  d.inhalt.innerHTML = `<form novalidate autocomplete="off">
    <div class="ff"><div class="ff-kopf"><label for="sam-name">${tr('Name')}</label></div>
      <input id="sam-name" name="name" required maxlength="80" value="${escH(x?.name || '')}" placeholder="${tr('z. B. Weihnachtsfilme')}"></div>
    <div class="ff"><div class="ff-kopf"><label for="sam-text">${tr('Beschreibung')}</label></div>
      <textarea id="sam-text" name="beschreibung" maxlength="2000" style="min-height:80px">${escH(x?.beschreibung || '')}</textarea></div>
    <div class="ff-reihe">
      <div class="ff"><div class="ff-kopf"><label for="sam-sort">${tr('Reihenfolge')}</label></div>
        <select id="sam-sort" name="sortierung">
          ${[['hand', tr('Von Hand')], ['jahr', tr('Nach Jahr')], ['titel', tr('Nach Titel')]].map(([v, t]) =>
            `<option value="${v}"${(x?.sortierung || 'hand') === v ? ' selected' : ''}>${t}</option>`).join('')}</select></div>
      <div class="ff" style="justify-content:flex-end"><label class="wahl-check"><input type="checkbox" name="aufStart"${x?.aufStart ? ' checked' : ''}>
        ${tr('Auf der Startseite zeigen')}</label></div>
    </div>
    <details style="margin-top:16px"${x?.regel ? ' open' : ''}>
      <summary class="t-body" style="cursor:pointer">${tr('Regel (optional)')}</summary>
      <p class="ff-hinweis" style="margin:6px 0 0">${tr('Passende Titel kommen von selbst dazu — auch neue, sobald sie in der Mediathek sind.')}</p>
      <div class="ff-reihe">
        <div class="ff"><div class="ff-kopf"><label for="sam-art">${tr('Art')}</label></div>
          <select id="sam-art" name="art">${[['', tr('Filme und Serien')], ['movie', tr('Nur Filme')], ['series', tr('Nur Serien')]].map(([v, t]) =>
            `<option value="${v}"${(r.art || '') === v ? ' selected' : ''}>${t}</option>`).join('')}</select></div>
        <div class="ff"><div class="ff-kopf"><label for="sam-genre">${tr('Genre')}</label></div>
          <input id="sam-genre" name="genre" list="sam-genres" value="${escH(r.genre || '')}" maxlength="40">
          <datalist id="sam-genres">${genres.map(g => `<option value="${escH(g)}">`).join('')}</datalist></div>
        <div class="ff"><div class="ff-kopf"><label for="sam-von">${tr('Jahr von')}</label></div>
          <input id="sam-von" name="jahrVon" type="number" inputmode="numeric" min="1870" max="2100" value="${r.jahrVon || ''}"></div>
        <div class="ff"><div class="ff-kopf"><label for="sam-bis">${tr('Jahr bis')}</label></div>
          <input id="sam-bis" name="jahrBis" type="number" inputmode="numeric" min="1870" max="2100" value="${r.jahrBis || ''}"></div>
      </div>
      <div class="ff"><div class="ff-kopf"><label for="sam-wort">${tr('Stichwort im Titel')}</label></div>
        <input id="sam-wort" name="stichwort" maxlength="80" value="${escH(r.stichwort || '')}" placeholder="${tr('z. B. Weihnacht')}"></div>
    </details>
    ${x ? `<div class="ff" style="margin-top:18px"><div class="ff-kopf"><span>${tr('Bild')}</span></div>
      <div style="display:flex;gap:14px;align-items:center">
        <div class="bild-vorschau bild-vorschau--hoch" style="width:72px" data-sam-bildbox>${x.bild ? `<img src="${escH(mitSchluessel(x.bild))}" alt="">` : ''}</div>
        <div class="bild-knoepfe">
          <span class="btn btn-pill btn-pill--fill datei-wahl">${tr('Hochladen …')}<input type="file" accept="image/jpeg,image/png,image/gif" data-sam-bild aria-label="${tr('Bild der Sammlung hochladen')}"></span>
          <button type="button" class="btn btn-pill btn-pill--fill" data-sam-bild-weg${x.bild ? '' : ' hidden'}>${tr('Eigenes Bild entfernen')}</button>
        </div></div>
      <span class="ff-hinweis">${tr('Ohne eigenes Bild zeigt Lumen die Poster der ersten Titel.')}</span></div>` : ''}
    <div data-meldung hidden></div>
    <div class="fenster-fuss">
      ${x ? `<button type="button" class="btn btn-pill" style="height:40px;color:#ff6961;margin-right:auto" data-sam-loeschen>${tr('Löschen')}</button>` : ''}
      <button type="button" class="btn btn-pill btn-pill--fill" style="height:40px" data-fenster-zu>${tr('Abbrechen')}</button>
      <button type="submit" class="btn btn-primary" style="height:40px" data-laeuft="${tr('Speichert …')}">${x ? tr('Speichern') : tr('Anlegen')}</button>
    </div></form>`;
  const f = d.inhalt.querySelector('form');
  f.name.focus();
  f.addEventListener('submit', async e => {
    e.preventDefault();
    const name = f.name.value.trim();
    if (!name) { meldung(d.inhalt, tr('Die Sammlung braucht einen Namen.')); return; }
    const von = Number(f.jahrVon.value) || 0, bis = Number(f.jahrBis.value) || 0;
    if (von && bis && von > bis) { meldung(d.inhalt, tr('Der Jahrbereich stimmt nicht: „von“ muss vor „bis“ liegen.')); return; }
    const body = { name, beschreibung: f.beschreibung.value, sortierung: f.sortierung.value, aufStart: f.aufStart.checked,
      regel: { art: f.art.value, genre: f.genre.value.trim(), jahrVon: von, jahrBis: bis, stichwort: f.stichwort.value.trim() } };
    try {
      await beschaeftigt(f.querySelector('[type=submit]'), async () => {
        if (x) await api.call('/admin/sammlungen/' + encodeURIComponent(x.id), { method: 'POST', body });
        else neueID = (await api.call('/admin/sammlungen', { method: 'POST', body })).id;
      });
      geaendert = true;
      d.close();
      U.toast(x ? tr('Gespeichert') : tr('„{name}“ angelegt', { name }));
    } catch (err) { meldung(d.inhalt, fehlerText(err)); }
  });
  d.addEventListener('change', async e => {
    const inp = e.target.closest('[data-sam-bild]');
    if (!inp?.files?.[0]) return;
    const datei = inp.files[0];
    if (datei.size > 20 * 1024 * 1024) { meldung(d.inhalt, tr('Das Bild ist zu gross (höchstens 20 MB).')); return; }
    const box = d.inhalt.querySelector('[data-sam-bildbox]');
    box.innerHTML = '<span class="spin"></span>';
    try {
      const neu = await hochladen(`/admin/sammlungen/${encodeURIComponent(x.id)}/bild`, datei);
      box.innerHTML = neu.bild ? `<img src="${escH(mitSchluessel(neu.bild) + '&t=' + Date.now())}" alt="">` : '';
      d.inhalt.querySelector('[data-sam-bild-weg]').hidden = false;
      geaendert = true;
      meldung(d.inhalt, tr('Bild gesetzt.'), 'gut');
    } catch (err) { box.innerHTML = ''; meldung(d.inhalt, fehlerText(err)); }
    inp.value = '';
  });
  d.addEventListener('click', async e => {
    if (e.target.closest('[data-sam-bild-weg]')) {
      try {
        await api.call(`/admin/sammlungen/${encodeURIComponent(x.id)}/bild`, { method: 'DELETE' });
        d.inhalt.querySelector('[data-sam-bildbox]').innerHTML = '';
        e.target.closest('[data-sam-bild-weg]').hidden = true;
        geaendert = true;
      } catch (err) { meldung(d.inhalt, fehlerText(err)); }
    }
    const weg = e.target.closest('[data-sam-loeschen]');
    if (weg) {
      if (weg.dataset.sicher !== '1') {
        weg.dataset.sicher = '1';
        weg.textContent = tr('Wirklich löschen?');
        meldung(d.inhalt, tr('Gelöscht wird nur die Sammlung — kein Film, keine Datei.'), 'info');
        return;
      }
      try {
        await api.call('/admin/sammlungen/' + encodeURIComponent(x.id), { method: 'DELETE' });
        geaendert = false;
        d.close();
        U.toast(tr('Sammlung gelöscht'));
        U.go('sammlungen', { ersetzen: true });
      } catch (err) { meldung(d.inhalt, fehlerText(err)); }
    }
  });
}

/* „Zu Sammlung hinzufügen" auf der Titelseite. */
export async function zuSammlung(itemId, titel) {
  const d = fenster({ titel: tr('Zu Sammlung hinzufügen') });
  const zeichnen = async () => {
    let liste;
    try { liste = await api.call('/admin/sammlungen?item=' + encodeURIComponent(itemId)); }
    catch (err) { d.inhalt.innerHTML = `<div class="fenster-meldung fenster-meldung--fehler" role="alert">${escH(fehlerText(err))}</div>`; return; }
    d.inhalt.innerHTML = `<p class="t-sub c-2" style="margin:0 0 12px">${escH(titel)}</p>
      <div class="wahl" role="group" aria-label="${tr('Sammlungen')}">
        ${liste.length ? liste.map(s => `<button type="button" class="wahl-zeile" data-sam-wahl="${escH(s.id)}" aria-pressed="${s.enthaelt}">
            <span class="haken${s.enthaelt ? ' haken--an' : ''}" aria-hidden="true">${s.enthaelt ? U.svg(U.I.check, 13, 2.8) : ''}</span>
            <span style="flex:1">${escH(s.name)}</span>
            <span class="t-caption c-2">${s.regel && !s.vonHand ? tr('mit Regel') : titelAnzahl(s.vonHand) + (s.regel ? ' · ' + tr('mit Regel') : '')}</span></button>`).join('')
          : `<div class="wahl-leer">${tr('Noch keine Sammlung. Leg unten eine an.')}</div>`}
      </div>
      <form class="ff" style="margin-top:14px" data-sam-neuform autocomplete="off">
        <div class="ff-kopf"><label for="sam-neu">${tr('Neue Sammlung')}</label></div>
        <div style="display:flex;gap:8px"><input id="sam-neu" name="name" maxlength="80" placeholder="${tr('Name')}">
          <button class="btn btn-pill btn-pill--fill" style="height:40px" data-laeuft="${tr('Legt an …')}">${tr('Anlegen')}</button></div>
      </form>
      <div data-meldung hidden></div>`;
  };
  d.inhalt.innerHTML = ladeHinweis();
  await zeichnen();
  d.addEventListener('click', async e => {
    const z = e.target.closest('[data-sam-wahl]');
    if (!z) return;
    const drin = z.getAttribute('aria-pressed') === 'true';
    z.disabled = true;
    try {
      await (drin
        ? api.call(`/admin/sammlungen/${encodeURIComponent(z.dataset.samWahl)}/titel/${encodeURIComponent(itemId)}`, { method: 'DELETE' })
        : api.call(`/admin/sammlungen/${encodeURIComponent(z.dataset.samWahl)}/titel`, { method: 'POST', body: { itemId } }));
      await zeichnen();
      meldung(d.inhalt, drin ? tr('Aus der Sammlung genommen.') : tr('Zur Sammlung hinzugefügt.'), 'gut');
    } catch (err) { z.disabled = false; meldung(d.inhalt, fehlerText(err)); }
  });
  d.addEventListener('submit', async e => {
    e.preventDefault();
    const f = e.target;
    const name = f.name.value.trim();
    if (!name) return;
    try {
      await beschaeftigt(f.querySelector('button'), async () => {
        const s = await api.call('/admin/sammlungen', { method: 'POST', body: { name } });
        await api.call(`/admin/sammlungen/${encodeURIComponent(s.id)}/titel`, { method: 'POST', body: { itemId } });
      });
      await zeichnen();
      meldung(d.inhalt, tr('„{name}“ angelegt und den Titel hinzugefügt.', { name }), 'gut');
    } catch (err) { meldung(d.inhalt, fehlerText(err)); }
  });
}
