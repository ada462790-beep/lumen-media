// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

/* Gemeinsames für die Fenster von Handbearbeitung, Sammlungen, Übernahme
 * und Untertiteln.
 *
 * Die Hauptansicht (app.js) zeichnet bei jeder Änderung die ganze Seite
 * neu. Ein Formular mit zwanzig Feldern, Schlössern und Bildern wäre dort
 * mühsam: Jede Eingabe müsste in den Zustand und zurück. Darum leben diese
 * Formulare in einem eigenen <dialog>: Es liegt über der Seite, behält
 * seine Eingaben, Esc schliesst es, und der Fokus bleibt darin.
 *
 * U ist die Brücke zu app.js — die Hilfen von dort (esc, svg, toast …)
 * trägt app.js beim Start ein, damit es sie nur einmal gibt. */

import { api, ApiError } from './api.js';
import { tr } from './sprache.js';

export const U = {};

// Die Gestaltung der Fenster liegt in einer eigenen Datei; geladen wird
// sie einmal, sobald dieses Modul da ist.
(() => {
  if (document.querySelector('link[data-funktionen]')) return;
  const l = document.createElement('link');
  l.rel = 'stylesheet';
  l.href = '/funktionen.css';
  l.dataset.funktionen = '';
  document.head.appendChild(l);
})();

export const escH = s => String(s ?? '').replace(/[\u202A-\u202E\u2066-\u2069]/g, '').replace(/[&<>"']/g,
  c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));

/* Verwaltung nur am Schreibtisch: mit Maus oder Trackpad, nicht auf einem
   Touch-Gerät. Dieselbe Frage, die app.js für die Vorschau stellt. */
export const amSchreibtisch = () => matchMedia('(hover: hover) and (pointer: fine)').matches;

/* fenster öffnet einen Dialog. Zurück kommt das Element; inhalt(el) füllt
   es (und darf es später neu füllen). Beim Schliessen: zu(). */
export function fenster({ titel, klasse = '', breit = false, zu }) {
  const d = document.createElement('dialog');
  d.className = `lumen fenster${breit ? ' fenster--breit' : ''} ${klasse}`.trim();
  d.setAttribute('aria-labelledby', 'fenster-t-' + (fenster.n = (fenster.n || 0) + 1));
  if (document.documentElement.classList.contains('hell')) d.classList.add('light');
  d.innerHTML = `<div class="fenster-kopf">
      <h2 class="fenster-titel" id="fenster-t-${fenster.n}">${escH(titel)}</h2>
      <button type="button" class="btn btn-round sm fenster-zu" data-fenster-zu aria-label="${tr('Schliessen')}">
        <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" aria-hidden="true"><path d="M6 6l12 12M18 6L6 18"/></svg></button>
    </div>
    <div class="fenster-inhalt"></div>`;
  document.body.appendChild(d);
  d.addEventListener('click', e => {
    if (e.target.closest('[data-fenster-zu]')) d.close();
  });
  d.addEventListener('close', () => { d.remove(); if (zu) zu(); });
  d.showModal();
  d.inhalt = d.querySelector('.fenster-inhalt');
  d.titelSetzen = t => { d.querySelector('.fenster-titel').textContent = t; };
  return d;
}

export const ladeHinweis = text => `<div class="fenster-laden" role="status"><span class="spin"></span>${escH(text || tr('Einen Moment …'))}</div>`;

/* Fehler im Fenster: als Zeile mit Rolle „alert", damit ein Bildschirm-
   leser sie vorliest. Ein Toast wäre hinter dem Dialog unsichtbar. */
export function meldung(el, text, art = 'fehler') {
  const box = el.querySelector('[data-meldung]');
  if (!box) return;
  box.className = `fenster-meldung fenster-meldung--${art}`;
  box.textContent = text || '';
  box.hidden = !text;
  if (text) box.setAttribute('role', art === 'fehler' ? 'alert' : 'status');
}

export function fehlerText(err) {
  if (err instanceof ApiError) {
    if (err.status === 423 || err.sperre) return tr('Die Verwaltung ist gesperrt. Erst unter Verwaltung entsperren, dann geht das.');
    return err.message;
  }
  return tr('Unerwarteter Fehler.');
}

/* Ein Knopf, der während der Arbeit nicht noch einmal gedrückt werden
   kann und zeigt, dass etwas passiert. */
export async function beschaeftigt(knopf, arbeit) {
  if (!knopf) return arbeit();
  const vorher = knopf.innerHTML;
  knopf.disabled = true;
  knopf.setAttribute('aria-busy', 'true');
  knopf.innerHTML = `<span class="spin spin--s"></span>${knopf.dataset.laeuft ? escH(knopf.dataset.laeuft) : vorher}`;
  try { return await arbeit(); }
  finally {
    if (knopf.isConnected) {
      knopf.disabled = false;
      knopf.removeAttribute('aria-busy');
      knopf.innerHTML = vorher;
    }
  }
}

/* Hochladen mit Anmeldung (und, für die Verwaltung, dem Token der
   Verwaltungssperre). api.call kann nur JSON. */
export async function hochladen(pfad, datei) {
  const headers = { Authorization: 'Bearer ' + api.token, 'Content-Type': datei.type || 'application/octet-stream' };
  if (pfad.startsWith('/admin/')) {
    try { const t = sessionStorage.getItem('lumen.verwaltung'); if (t) headers['X-Lumen-Admin'] = t; } catch {}
  }
  let res;
  try { res = await fetch('/api/v1' + pfad, { method: 'POST', headers, body: datei }); }
  catch { throw new ApiError(tr('Der Server ist nicht erreichbar.'), 0); }
  if (!res.ok) {
    let msg = tr('Das hat nicht geklappt.'), body = {};
    try { body = await res.json(); msg = body.error || msg; } catch {}
    const e = new ApiError(msg, res.status);
    if (res.status === 423) e.sperre = body.sperre || true;
    throw e;
  }
  const text = await res.text();
  return text ? JSON.parse(text) : null;
}

/* Ein Bild neu laden, nachdem es ersetzt wurde: Der Browser hält Bilder
   einen Tag lang; so holt er das neue sofort. */
export function bildAuffrischen(url) {
  if (!url) return;
  try { fetch(url + (url.includes('?') ? '&' : '?') + api.keyParam(), { cache: 'reload' }).catch(() => {}); } catch {}
}
