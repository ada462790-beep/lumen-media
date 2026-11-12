// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

/* Untertitel suchen und holen (OpenSubtitles), und die Einstellungen dazu
 * in der Verwaltung.
 *
 * Das Fenster ist eigenständig, damit der Player es nur aufrufen muss: Er
 * reicht Titel und Datei herein und bekommt nach dem Holen die neue
 * Spurenliste zurück (nachher). */

import { api } from './api.js';
import { U, escH, fenster, ladeHinweis, meldung, fehlerText, beschaeftigt } from './fenster.js';
import { tr, sprachName, sprache } from './sprache.js';

/* Ob OpenSubtitles eingerichtet ist — einmal gefragt, dann gemerkt. */
export const utStand = { verfuegbar: null, sprachen: ['de', 'en'] };
let utFrage = null;
export function utStandLaden(neu = false) {
  if (!api.signedIn) return Promise.resolve(utStand);
  if (utFrage && !neu) return utFrage;
  utFrage = api.call('/untertitel/verfuegbar')
    .then(r => { utStand.verfuegbar = !!r.verfuegbar; utStand.sprachen = r.sprachen || utStand.sprachen; return utStand; })
    .catch(() => { utFrage = null; return utStand; });
  return utFrage;
}

// Die Sprachen, die OpenSubtitles anbietet; Namen aus Intl (sprache.js).
const SPRACHEN = ['de', 'en', 'fr', 'it', 'es', 'pt', 'nl', 'pl', 'tr', 'sv', 'da', 'ru', 'ja'];

/* Suchen. opts: { itemId, fileId, titel, nachher(tracks, neuIndex) } */
export async function untertitelSuchen(opts) {
  const d = fenster({ titel: tr('Untertitel suchen'), breit: true, klasse: 'fenster--ut' });
  d.inhalt.innerHTML = ladeHinweis();
  await utStandLaden();
  let gewaehlt = (utStand.sprachen || ['de']).slice(0, 3);
  let fileId = opts.fileId || '';

  d.inhalt.innerHTML = `<p class="t-sub c-2" style="margin:0">${escH(opts.titel || '')}</p>
    <form data-ut-form autocomplete="off">
      <div class="ff"><div class="ff-kopf"><span>${tr('Sprachen')}</span></div>
        <div class="pillen" role="group" aria-label="${tr('Sprachen')}">${SPRACHEN.map(l =>
          `<button type="button" class="tab-pill" data-ut-sprache="${l}" aria-pressed="${gewaehlt.includes(l)}">${escH(sprachName(l))}</button>`).join('')}</div></div>
      <div class="ff"><div class="ff-kopf"><label for="ut-name">${tr('Suchbegriff (optional)')}</label></div>
        <div style="display:flex;gap:8px"><input id="ut-name" name="name" maxlength="120" placeholder="${tr('Leer lassen: Lumen sucht über die Datei und den Titel')}">
          <button class="btn btn-primary" style="height:40px" data-laeuft="${tr('Sucht …')}">${tr('Suchen')}</button></div></div>
    </form>
    <div data-meldung hidden></div>
    <div data-ut-treffer style="margin-top:16px"></div>`;

  const suchen = async () => {
    const box = d.inhalt.querySelector('[data-ut-treffer]');
    if (!gewaehlt.length) { meldung(d.inhalt, tr('Bitte mindestens eine Sprache wählen.')); return; }
    meldung(d.inhalt, '');
    box.innerHTML = ladeHinweis(tr('Suche bei OpenSubtitles …'));
    const q = new URLSearchParams({ sprachen: gewaehlt.join(',') });
    if (fileId) q.set('datei', fileId);
    const name = d.inhalt.querySelector('[name=name]').value.trim();
    if (name) q.set('name', name);
    try {
      const r = await api.call(`/items/${encodeURIComponent(opts.itemId)}/untertitel/suche?${q}`);
      fileId = r.datei || fileId;
      box.innerHTML = trefferListe(r.treffer || [], r.hash);
    } catch (err) {
      box.innerHTML = '';
      meldung(d.inhalt, fehlerText(err));
    }
  };

  d.addEventListener('click', async e => {
    const p = e.target.closest('[data-ut-sprache]');
    if (p) {
      const l = p.dataset.utSprache;
      gewaehlt = gewaehlt.includes(l) ? gewaehlt.filter(x => x !== l) : [...gewaehlt, l].slice(-5);
      d.inhalt.querySelectorAll('[data-ut-sprache]').forEach(b => b.setAttribute('aria-pressed', String(gewaehlt.includes(b.dataset.utSprache))));
      return;
    }
    const h = e.target.closest('[data-ut-holen]');
    if (!h) return;
    const t = JSON.parse(h.dataset.utHolen);
    meldung(d.inhalt, '');
    try {
      let r;
      await beschaeftigt(h, async () => {
        r = await api.call(`/items/${encodeURIComponent(opts.itemId)}/untertitel`, { method: 'POST',
          body: { datei: fileId, dateiId: t.dateiId, sprache: t.sprache, hoergeschaedigt: !!t.hoergeschaedigt, erzwungen: !!t.erzwungen } });
      });
      h.textContent = tr('Geholt');
      h.disabled = true;
      meldung(d.inhalt, `${tr('Gespeichert — die Untertitel ({sprache}) sind jetzt als Spur da.', { sprache: sprachName(t.sprache) })}${
        r.rest >= 0 ? ` ${tr('Heute noch {rest} Downloads bei OpenSubtitles.', { rest: r.rest })}` : ''}`, 'gut');
      opts.nachher?.(r.tracks || [], r.neu);
    } catch (err) { meldung(d.inhalt, fehlerText(err)); }
  });
  d.addEventListener('submit', async e => {
    e.preventDefault();
    await beschaeftigt(d.inhalt.querySelector('[data-ut-form] .btn-primary'), suchen);
  });
  suchen();
}

function trefferListe(treffer, mitHash) {
  if (!treffer.length) {
    return `<div class="wahl"><div class="wahl-leer">${tr('Nichts gefunden. Eine andere Sprache wählen oder einen Suchbegriff eingeben — etwa den Originaltitel.')}</div></div>`;
  }
  return `<div class="wahl">${treffer.map(t => {
    const abz = [
      t.hashTreffer ? `<span class="abz-klein abz-klein--gut" title="${tr('Passt genau zu dieser Datei')}">${tr('passt genau')}</span>` : '',
      t.maschinell ? `<span class="abz-klein abz-klein--warn" title="${tr('Maschinell übersetzt')}">${tr('maschinell')}</span>` : '',
      t.hoergeschaedigt ? `<span class="abz-klein">${tr('für Hörgeschädigte')}</span>` : '',
      t.erzwungen ? `<span class="abz-klein">${tr('nur fremdsprachige Stellen')}</span>` : '',
    ].filter(Boolean).join(' ');
    return `<div class="wahl-zeile" style="cursor:default">
      <span class="abz-klein" style="min-width:28px;justify-content:center">${escH((t.sprache || '').toUpperCase())}</span>
      <span style="flex:1;min-width:0">
        <span class="t-body clamp-1" style="display:block">${escH(t.release || t.dateiname || t.titel || tr('Untertitel'))}</span>
        <span class="t-caption c-2" style="display:flex;gap:6px;flex-wrap:wrap;align-items:center;margin-top:3px">${abz}
          <span>${tr('{n} Downloads', { n: (t.downloads || 0).toLocaleString(sprache()) })}</span></span>
      </span>
      <button type="button" class="btn btn-pill btn-pill--fill" style="height:32px;font-size:.8125rem;flex:none" data-laeuft="${tr('Holt …')}"
        data-ut-holen="${escH(JSON.stringify({ dateiId: t.dateiId, sprache: t.sprache, hoergeschaedigt: t.hoergeschaedigt, erzwungen: t.erzwungen }))}">${tr('Holen')}</button>
    </div>`;
  }).join('')}</div>
  ${mitHash ? '' : `<p class="ff-hinweis" style="margin-top:8px">${tr('Die Datei ist zu klein für einen genauen Abgleich — die Treffer gelten für den Titel, nicht für genau diese Fassung.')}</p>`}`;
}

/* ---------------------------------------------------------------- Verwaltung */

export async function untertitelVerwaltung() {
  const d = fenster({ titel: tr('Untertitel von OpenSubtitles') });
  d.inhalt.innerHTML = ladeHinweis();
  let st;
  try { st = await api.call('/admin/untertitel'); }
  catch (err) { d.inhalt.innerHTML = `<div class="fenster-meldung fenster-meldung--fehler" role="alert">${escH(fehlerText(err))}</div>`; return; }
  d.inhalt.innerHTML = `<form autocomplete="off" data-uta>
    <p class="t-sub c-2" style="margin:0;line-height:1.5">${tr('Damit alle im Haushalt im Player und auf der Titelseite Untertitel suchen können. Gilt für alle Profile — niemand braucht ein eigenes Konto.')}</p>
    <div class="ff"><div class="ff-kopf"><label for="uta-key">${tr('API-Schlüssel')}</label></div>
      <input id="uta-key" name="apiKey" class="mono" autocomplete="off" placeholder="${st.configured ? tr('•••••••• — hinterlegt') : tr('Schlüssel einfügen')}">
      <span class="ff-hinweis">${tr('Kostenlos auf {seite} unter „API Consumers“ anlegen. Der Schlüssel verlässt den Server nie.', {
        seite: '<a href="https://www.opensubtitles.com/consumers" target="_blank" rel="noopener noreferrer">opensubtitles.com</a>' })}</span></div>
    <div class="ff-reihe">
      <div class="ff"><div class="ff-kopf"><label for="uta-user">${tr('Benutzerkonto (optional)')}</label></div>
        <input id="uta-user" name="benutzer" autocomplete="off" value="${escH(st.benutzer || '')}"></div>
      <div class="ff"><div class="ff-kopf"><label for="uta-pw">${tr('Passwort')}</label></div>
        <input id="uta-pw" name="passwort" type="password" autocomplete="new-password" placeholder="${st.passwort ? tr('•••••••• — hinterlegt') : ''}"></div>
    </div>
    <span class="ff-hinweis">${tr('Ohne Konto erlaubt OpenSubtitles nur wenige Downloads am Tag, mit kostenlosem Konto etwa 20.')}</span>
    <h3 class="t-body" style="margin:22px 0 0;font-weight:600">${tr('Fehlende Untertitel von selbst holen')}</h3>
    <label class="wahl-check"><input type="checkbox" name="auto"${st.auto ? ' checked' : ''}> ${tr('Einschalten')}</label>
    <div class="ff-reihe">
      <div class="ff" style="margin-top:4px"><div class="ff-kopf"><label for="uta-spr">${tr('Sprachen')}</label></div>
        <input id="uta-spr" name="sprachen" value="${escH((st.sprachen || []).join(', '))}" placeholder="de, en"></div>
      <div class="ff" style="margin-top:4px"><div class="ff-kopf"><label for="uta-lim">${tr('Höchstens pro Tag')}</label></div>
        <input id="uta-lim" name="tageslimit" type="number" min="0" max="1000" value="${st.tageslimit}"></div>
    </div>
    <span class="ff-hinweis">${tr('Läuft im Hintergrund, nie während etwas umgewandelt wird, und nimmt nur, was sicher passt. Heute automatisch geholt: {heuteGeholt}.', { heuteGeholt: st.heuteGeholt })}</span>
    <div data-meldung hidden></div>
    <div class="fenster-fuss">
      <button type="button" class="btn btn-pill btn-pill--fill" style="height:40px" data-fenster-zu>${tr('Schliessen')}</button>
      <button class="btn btn-primary" style="height:40px" data-laeuft="${tr('Prüft …')}">${tr('Speichern und prüfen')}</button></div>
  </form>`;
  // Ändern darf nur der Besitzer (api/routen_funktionen.go); Mitverwalter
  // sehen die Einstellungen nur, ohne den Benutzernamen des Kontos.
  if (st.darfAendern === false) {
    const form = d.inhalt.querySelector('form');
    form.querySelectorAll('input').forEach(i => { i.disabled = true; });
    form.querySelector('.btn-primary')?.remove();
    const hinweis = document.createElement('p');
    hinweis.className = 'ff-hinweis';
    hinweis.textContent = tr('Diese Einstellungen ändert nur der Besitzer des Servers.');
    form.querySelector('.fenster-fuss').before(hinweis);
    return;
  }
  d.addEventListener('submit', async e => {
    e.preventDefault();
    const f = e.target;
    const sprachen = f.sprachen.value.split(/[,\s]+/).map(s => s.trim().toLowerCase()).filter(Boolean);
    if (sprachen.some(s => !/^[a-z]{2,3}(-[a-z]{2})?$/.test(s))) { meldung(d.inhalt, tr('Sprachen bitte als Kürzel, z. B. de, en, fr.')); return; }
    const body = { auto: f.auto.checked, sprachen, tageslimit: Number(f.tageslimit.value) || 0, benutzer: f.benutzer.value.trim() };
    if (f.apiKey.value.trim()) body.apiKey = f.apiKey.value.trim();
    if (f.passwort.value) body.passwort = f.passwort.value;
    body.pruefen = !!(body.apiKey || st.configured);
    try {
      let r;
      await beschaeftigt(f.querySelector('.btn-primary'), async () => { r = await api.call('/admin/untertitel', { method: 'PUT', body }); });
      f.apiKey.value = ''; f.passwort.value = '';
      st = r;
      if (r.configured) {
        f.apiKey.placeholder = tr('•••••••• — hinterlegt');
        if (r.passwort) f.passwort.placeholder = tr('•••••••• — hinterlegt');
      }
      meldung(d.inhalt, r.geprueft
        ? (r.rest >= 0 ? tr('Gespeichert. OpenSubtitles antwortet — heute noch {rest} Downloads.', { rest: r.rest }) : tr('Gespeichert. OpenSubtitles antwortet.'))
        : tr('Gespeichert.'), 'gut');
      utStandLaden(true);
    } catch (err) { meldung(d.inhalt, fehlerText(err)); }
  });
}
