// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

/* Bildschirmzeit für Kinderprofile.
 *
 * Durchgesetzt wird sie auf dem Server (api/bildschirmzeit.go) — die
 * Oberfläche zeigt nur:
 *   - in der Verwaltung (Konten): die Regel bearbeiten, „heute genutzt:
 *     ungefähr 45 von 90 Min." (der Server rundet auf Viertelstunden ab,
 *     damit sich aus der Liste nicht ablesen lässt, wann das Kind schaut)
 *     und „heute einmalig 30 Min. mehr";
 *   - dem Kind: unter „Ich", wie viel heute noch geht; beim Schauen einen
 *     leisen Hinweis, wenn es weniger als zehn Minuten sind; und eine
 *     freundliche Seite über dem Player, wenn die Zeit um ist.
 * Was jemand in diesen Minuten geschaut hat, steht nirgends. */

import { api } from './api.js';
import { tr } from './sprache.js';
import { U, escH, fenster, meldung, fehlerText, beschaeftigt } from './fenster.js';

/* ---------------------------------------------------------- Verwaltung */

/* Die Zeile unter dem Namen eines Kinderprofils in der Kontenliste. */
export function bzKontoText(a) {
  const b = a?.bildschirmzeit;
  if (!b || !b.regel?.aktiv) return '';
  return b.budgetMinuten
    ? tr('Bildschirmzeit heute: ungefähr {genutzt} von {budget} Min.', { genutzt: b.genutztMinuten, budget: b.budgetMinuten })
    : tr('Bildschirmzeit heute: ungefähr {genutzt} Min.', { genutzt: b.genutztMinuten });
}

function tagesart(name, titel, r) {
  const ganz = !r.von;
  return `<fieldset class="bz-art" data-art="${name}">
    <legend class="t-body" style="font-weight:600">${escH(titel)}</legend>
    <label class="wahl-check"><input type="checkbox" name="${name}-ganz"${ganz ? ' checked' : ''} data-bz-ganz>
      <span>${tr('Den ganzen Tag')}</span></label>
    <div class="bz-zeiten"${ganz ? ' hidden' : ''}>
      <div class="ff"><div class="ff-kopf"><label for="bz-${name}-von">${tr('Ab')}</label></div>
        <input id="bz-${name}-von" name="${name}-von" type="time" value="${escH(r.von || '07:00')}"></div>
      <div class="ff"><div class="ff-kopf"><label for="bz-${name}-bis">${tr('Bis')}</label></div>
        <input id="bz-${name}-bis" name="${name}-bis" type="time" value="${escH(r.bis === '24:00' ? '00:00' : r.bis || '19:30')}"></div>
    </div>
    <div class="ff"><div class="ff-kopf"><label for="bz-${name}-min">${tr('Höchstens Minuten am Tag')}</label></div>
      <input id="bz-${name}-min" name="${name}-min" type="number" min="0" max="1440" step="5" value="${Number(r.minuten) || 0}">
      <span class="ff-hinweis">${tr('0 heisst: ohne Tagesgrenze.')}</span></div>
  </fieldset>`;
}

/* Ein Zeitfeld kennt kein „24:00": Bis Mitternacht steht dort als 00:00
   und geht als 24:00 zurück — so bleibt die Regel beim erneuten Speichern
   genau dieselbe (früher wurde daraus 23:59). */
const bisAus = v => (v === '00:00' ? '24:00' : v);

function regelAus(form) {
  const fd = new FormData(form);
  const art = name => fd.get(name + '-ganz')
    ? { von: '', bis: '', minuten: Number(fd.get(name + '-min')) || 0 }
    : { von: String(fd.get(name + '-von') || ''), bis: bisAus(String(fd.get(name + '-bis') || '')), minuten: Number(fd.get(name + '-min')) || 0 };
  return { aktiv: !!fd.get('aktiv'), schultage: art('schultage'), wochenende: art('wochenende') };
}

function standText(b) {
  if (!b) return '';
  if (!b.regel?.aktiv) return tr('Ausgeschaltet — es gilt keine Bildschirmzeit.');
  return b.budgetMinuten
    ? tr('Heute genutzt: ungefähr {genutzt} von {budget} Min. (in Viertelstunden abgerundet)', { genutzt: b.genutztMinuten, budget: b.budgetMinuten })
    : tr('Heute genutzt: ungefähr {genutzt} Min. (in Viertelstunden abgerundet)', { genutzt: b.genutztMinuten });
}

/* Das Fenster zum Einstellen. nachher() lädt die Kontenliste neu. */
export function bzBearbeiten(konto, nachher) {
  let b = konto.bildschirmzeit;
  const d = fenster({ titel: tr('Bildschirmzeit für {name}', { name: konto.name }), zu: () => nachher?.() });
  const zeichnen = () => {
    const r = b.regel;
    d.inhalt.innerHTML = `
      <form autocomplete="off" data-bz-form>
        <label class="wahl-check"><input type="checkbox" name="aktiv"${r.aktiv ? ' checked' : ''}>
          <span style="flex:1">${tr('Bildschirmzeit einschalten')}</span></label>
        <p class="ff-hinweis" style="margin:4px 0 12px">${tr('Gezählt wird nur, solange ein Video läuft. Zwei Geräte gleichzeitig zählen einmal. Es gilt die Uhr des Servers; um Mitternacht beginnt ein neuer Tag — ausser mitten in einem Zeitfenster über Mitternacht, das zählt bis zu seinem Ende zum Vortag. Die Verwaltung sieht die Minuten nur ungefähr, in Viertelstunden. Mit einer Tagesgrenze gibt es keine Downloads.')}</p>
        ${tagesart('schultage', tr('Schultage (Montag bis Freitag)'), r.schultage)}
        ${tagesart('wochenende', tr('Wochenende (Samstag und Sonntag)'), r.wochenende)}
        <p class="ff-hinweis">${tr('Liegt „Bis" vor „Ab", geht die Zeit über Mitternacht.')}</p>
        <div class="fenster-meldung fenster-meldung--info" role="status" data-bz-stand>${escH(standText(b))}</div>
        <div data-meldung hidden></div>
        <div class="fenster-fuss">
          ${r.aktiv && b.budgetMinuten ? `<button type="button" class="btn btn-pill btn-pill--fill" style="height:40px" data-bz-mehr data-laeuft="${tr('Einen Moment …')}">${tr('Heute einmalig 30 Min. mehr')}</button>` : ''}
          <button class="btn btn-primary" style="height:40px" data-laeuft="${tr('Speichert …')}">${tr('Speichern')}</button></div>
      </form>`;
  };
  zeichnen();
  d.addEventListener('change', e => {
    const ganz = e.target.closest('[data-bz-ganz]');
    if (ganz) ganz.closest('fieldset').querySelector('.bz-zeiten').hidden = ganz.checked;
  });
  d.addEventListener('click', async e => {
    const mehr = e.target.closest('[data-bz-mehr]');
    if (!mehr) return;
    try {
      b = await beschaeftigt(mehr, () => api.adminBildschirmzeitGeschenk(konto.id, 30));
      konto.bildschirmzeit = b;
      zeichnen();
      meldung(d, tr('{name} hat heute 30 Minuten mehr.', { name: konto.name }), 'gut');
    } catch (err) { meldung(d, fehlerText(err)); }
  });
  d.addEventListener('submit', async e => {
    e.preventDefault();
    const knopf = e.target.querySelector('.btn-primary');
    try {
      b = await beschaeftigt(knopf, () => api.adminBildschirmzeit(konto.id, regelAus(e.target)));
      konto.bildschirmzeit = b;
      d.close();
      U.toast?.(tr('Bildschirmzeit gespeichert'));
    } catch (err) {
      if (err?.abgebrochen) return;
      meldung(d, fehlerText(err));
    }
  });
}

/* ---------------------------------------------------------- Kind */

let stand = null;

/* Den Stand des eigenen Profils holen (null bei Fehlern). */
export async function bzLaden() {
  try { stand = await api.meineBildschirmzeit(); } catch { stand = null; }
  return stand;
}

export const bzStand = () => stand;

/* Zeilen für „Ich": nur, wenn für dieses Profil Bildschirmzeit gilt. */
export function bzIchZeile(s = stand) {
  if (!s?.aktiv) return '';
  let haupt;
  if (!s.erlaubt) haupt = s.meldung || tr('Für heute ist Schluss.');
  else if (s.restMinuten != null) haupt = tr('Heute noch {n} Min.', { n: s.restMinuten });
  else haupt = tr('Heute ohne Grenze');
  const teile = [];
  if (s.budgetMinuten) teile.push(tr('{genutzt} von {budget} Min. gebraucht', { genutzt: s.genutztMinuten, budget: s.budgetMinuten }));
  if (s.von) teile.push(tr('heute {von} bis {bis}', { von: s.von, bis: s.bis }));
  return `<div class="row" style="min-height:54px"><span style="flex:1">
      <span class="t-body">${escH(haupt)}</span>
      ${teile.length ? `<span class="t-caption c-2" style="display:block">${escH(teile.join(' · '))}</span>` : ''}</span></div>`;
}

/* Beim Schauen: jede Minute nachsehen, einmal leise sagen, wenn es knapp
   wird, und amEnde(meldung) rufen, wenn die Zeit um ist. Die eigentliche
   Grenze zieht der Server. */
let zeitgeber = null;
let gewarnt = 0;
export function bzBeimSchauen(laeuft, amEnde) {
  clearInterval(zeitgeber);
  gewarnt = 0;
  if (!stand?.aktiv) return;
  const pruefen = async () => {
    if (!laeuft()) { clearInterval(zeitgeber); return; }
    const s = await bzLaden();
    // Zeit um: Der Server hat die Wiedergabe schon beendet. Hat der
    // Browser aber weit vorgeladen, liefe der Film aus dem Puffer weiter —
    // darum hier anhalten und die Meldung zeigen.
    if (s?.aktiv && !s.erlaubt && laeuft()) { clearInterval(zeitgeber); amEnde?.(s.meldung); return; }
    const rest = s?.aktiv && s.erlaubt ? s.restMinuten : null;
    if (rest == null) return;
    // Bei zehn und bei zwei Minuten je einmal.
    const stufe = rest <= 2 ? 2 : rest <= 10 ? 1 : 0;
    if (stufe > gewarnt) {
      gewarnt = stufe;
      U.toast?.(tr('Noch {n} Minuten Bildschirmzeit.', { n: rest }));
    }
  };
  pruefen();
  zeitgeber = setInterval(pruefen, 60 * 1000);
}

/* Die Seite, wenn die Zeit um ist — über allem, mit einem Knopf zurück. */
export function bzSchluss(text) {
  document.querySelector('.bz-schluss')?.remove();
  const el = document.createElement('div');
  el.className = 'bz-schluss';
  el.setAttribute('role', 'alertdialog');
  el.setAttribute('aria-modal', 'true');
  el.setAttribute('aria-labelledby', 'bz-schluss-t');
  el.innerHTML = `<div class="bz-schluss-karte">
      <svg width="56" height="56" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
        <path d="M21 12.8A9 9 0 1 1 11.2 3a7 7 0 0 0 9.8 9.8z"/></svg>
      <h2 id="bz-schluss-t">${tr('Bildschirmzeit')}</h2>
      <p>${escH(text || tr('Für heute ist Schluss.'))}</p>
      <button class="btn btn-primary" style="height:46px;min-width:160px" data-bz-ok>${tr('Okay')}</button>
    </div>`;
  document.body.appendChild(el);
  const knopf = el.querySelector('[data-bz-ok]');
  knopf.addEventListener('click', () => el.remove());
  el.addEventListener('keydown', e => { if (e.key === 'Escape') el.remove(); });
  knopf.focus();
}
