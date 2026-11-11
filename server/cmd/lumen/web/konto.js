// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

/* Was ein Konto sehen und tun darf — nachträglich ändern.
 *
 * Bisher ging das nur über die Einladung: Wer Lina mit „bis FSK 12"
 * eingeladen hatte, konnte die Grenze danach nicht mehr auf 6 setzen,
 * keine Bibliothek dazugeben und nichts am Herunterladen ändern — die
 * Schnittstelle konnte es (PUT /admin/accounts/{id}/limits), die
 * Oberfläche nicht. Gilt für Konten ohne Verwaltung; steht ein Konto in
 * einer Gruppe, gelten deren Grenzen, und dieses Fenster sagt das. */

import { api } from './api.js';
import { tr } from './sprache.js';
import { U, escH, fenster, meldung, fehlerText, beschaeftigt } from './fenster.js';
import { altersOptionen, ALTERSSTUFEN, KINDERSTUFEN } from './schutz.js';

const QUALITAETEN = [['480p', '480p'], ['720p', '720p'], ['1080p', '1080p'], ['original', 'Original']];

function formular(konto, bibliotheken, kind) {
  const lim = konto.limits || {};
  const libs = new Set(konto.libraries || []);
  const stufen = kind ? KINDERSTUFEN : ALTERSSTUFEN;
  // Wechselt jemand zum Kinderprofil, passt die alte Grenze oft nicht
  // (ohne Grenze, 18): dann die strengste sinnvolle Vorgabe.
  const alter = stufen.includes(Number(lim.maxAge)) ? lim.maxAge : (kind ? 12 : lim.maxAge);
  return `
    <form autocomplete="off" data-grenzen-form style="display:grid;gap:14px">
      ${konto.groupName ? `<div class="fenster-meldung fenster-meldung--info" role="status">${escH(tr('{name} gehört zur Gruppe „{gruppe}“. Solange gelten deren Bibliotheken und Grenzen; was du hier einstellst, gilt erst ohne Gruppe.', { name: konto.name, gruppe: konto.groupName }))}</div>` : ''}
      <label class="wahl-check"><input type="checkbox" name="kind"${kind ? ' checked' : ''} data-grenzen-kind>
        <span style="flex:1">${tr('Kinderprofil')}</span></label>
      <p class="ff-hinweis" style="margin:-8px 0 0">${tr('Ein Kinderprofil sieht nur Titel mit passender Freigabe – ungeprüfte Titel bleiben verborgen, Herunterladen ist aus.')}</p>
      <div class="ff"><div class="ff-kopf"><label for="gz-alter">${tr('Bis Altersfreigabe')}</label></div>
        <select id="gz-alter" name="maxAge">${altersOptionen(alter, stufen)}</select></div>
      <fieldset class="ff" style="border:0;padding:0;margin:0"><legend class="ff-kopf">${tr('Bibliotheken')}</legend>
        ${bibliotheken.length ? bibliotheken.map(l => `<label class="wahl-check"><input type="checkbox" name="lib" value="${escH(l.id)}"${libs.has(l.id) ? ' checked' : ''}><span>${escH(l.name)}</span></label>`).join('')
          : `<span class="ff-hinweis">${tr('Noch keine Bibliotheken.')}</span>`}</fieldset>
      <div style="display:grid;grid-template-columns:repeat(auto-fit,minmax(140px,1fr));gap:12px">
        <div class="ff"><div class="ff-kopf"><label for="gz-streams">${tr('Gleichzeitig')}</label></div>
          <input id="gz-streams" name="maxStreams" type="number" min="1" max="20" value="${Number(lim.maxStreams) || 2}"></div>
        <div class="ff"><div class="ff-kopf"><label for="gz-qual">${tr('Qualität unterwegs')}</label></div>
          <select id="gz-qual" name="remoteQuality">${QUALITAETEN.map(([v, n]) => `<option value="${v}"${(lim.remoteQuality || '1080p') === v ? ' selected' : ''}>${v === 'original' ? tr('Original') : n}</option>`).join('')}</select></div>
      </div>
      <div data-grenzen-erwachsen${kind ? ' hidden' : ''} style="display:grid;gap:10px">
        <label class="wahl-check"><input type="checkbox" name="allowUnrated"${lim.allowUnrated ? ' checked' : ''}><span>${tr('Ungeprüfte Titel')}</span></label>
        <label class="wahl-check"><input type="checkbox" name="allowDownload"${lim.allowDownload ? ' checked' : ''}><span>${tr('Herunterladen')}</span></label>
        <div class="ff"><div class="ff-kopf"><label for="gz-dl">${tr('Downloads')}</label></div>
          <input id="gz-dl" name="maxDownloads" type="number" min="0" max="500" value="${Number(lim.maxDownloads) || 0}"></div>
      </div>
      <label class="wahl-check"><input type="checkbox" name="allowTranscode"${lim.allowTranscode !== false ? ' checked' : ''}><span>${tr('Umwandeln')}</span></label>
      <div data-meldung hidden></div>
      <div class="fenster-fuss">
        <button class="btn btn-primary" style="height:40px" data-laeuft="${tr('Speichert …')}">${tr('Speichern')}</button></div>
    </form>`;
}

function grenzenAus(form) {
  const fd = new FormData(form);
  const kind = !!fd.get('kind');
  return {
    kinderprofil: kind,
    libraries: fd.getAll('lib').map(String),
    limits: {
      maxAge: Number(fd.get('maxAge')),
      maxStreams: Number(fd.get('maxStreams')) || 2,
      remoteQuality: String(fd.get('remoteQuality') || '1080p'),
      allowTranscode: !!fd.get('allowTranscode'),
      // Kinderprofile: keine ungeprüften Titel, kein Herunterladen (der
      // Server prüft das ebenso).
      allowUnrated: kind ? false : !!fd.get('allowUnrated'),
      allowDownload: kind ? false : !!fd.get('allowDownload'),
      maxDownloads: kind ? 0 : Number(fd.get('maxDownloads')) || 0,
    },
  };
}

/* Das Fenster. nachher() lädt die Kontenliste neu. */
export function grenzenBearbeiten(konto, bibliotheken, nachher) {
  let kind = !!konto.isKid;
  const d = fenster({ titel: tr('Was {name} darf', { name: konto.name }), zu: () => nachher?.() });
  const zeichnen = () => { d.inhalt.innerHTML = formular(konto, bibliotheken, kind); };
  zeichnen();
  d.addEventListener('change', e => {
    if (!e.target.closest('[data-grenzen-kind]')) return;
    // Den Rest des Formulars behalten, nur die Stufen und die
    // Erwachsenen-Schalter umstellen.
    kind = e.target.checked;
    const f = e.target.form;
    const sel = f.maxAge;
    const stufen = kind ? KINDERSTUFEN : ALTERSSTUFEN;
    const wert = stufen.includes(Number(sel.value)) ? Number(sel.value) : 12;
    sel.innerHTML = altersOptionen(wert, stufen);
    f.querySelector('[data-grenzen-erwachsen]').hidden = kind;
  });
  d.addEventListener('submit', async e => {
    e.preventDefault();
    const knopf = e.target.querySelector('.btn-primary');
    const g = grenzenAus(e.target);
    if (!g.libraries.length) { meldung(d, tr('Mindestens eine Bibliothek – sonst sieht {name} nichts.', { name: konto.name })); return; }
    try {
      await beschaeftigt(knopf, () => api.adminSetLimits(konto.id, g.limits, g.libraries, g.kinderprofil));
      d.close();
      U.toast?.(tr('Gespeichert'));
    } catch (err) {
      if (err?.abgebrochen) return;
      meldung(d, fehlerText(err));
    }
  });
}
