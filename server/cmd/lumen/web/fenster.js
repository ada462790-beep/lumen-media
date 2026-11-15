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
import { tr, sprache } from './sprache.js';

export const U = {};

// Die Gestaltung der Fenster liegt in einer eigenen Datei; geladen wird
// sie einmal, sobald dieses Modul da ist.
(() => {
  if (document.querySelector('link[data-funktionen]')) return;
  const l = document.createElement('link');
  l.rel = 'stylesheet';
  // Die Fassung setzt der Server beim Ausliefern ein (web.go).
  l.href = '/funktionen.css?v=__LUMEN_FASSUNG__';
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
   eine Weile; so holt er das neue sofort. Dieselbe Adresse wie im <img>
   (mit dem Bildschlüssel), sonst frischte das einen anderen Eintrag auf. */
export function bildAuffrischen(url) {
  if (!url) return;
  try { fetch(url + (url.includes('?') ? '&' : '?') + api.bildParam(), { cache: 'reload' }).catch(() => {}); } catch {}
}

/* ---- Eigene Bedienelemente im Stil der App ------------------------------
   Statt der Systemoptik (Uhrzeitfeld mit AM/PM, Zahlenfeld mit winzigen
   Pfeilen, Kalender in Browserfarben) dieselben ruhigen Felder wie überall
   sonst. Darunter stecken echte Eingabefelder — Formulare lesen sie wie
   vorher, Tastatur und Bildschirmleser bleiben erhalten. */

const zwei = n => String(n).padStart(2, '0');

/* Zahlenfeld mit − und +. Die Knöpfe sind für Maus und Finger; mit der
   Tastatur gehen die Pfeiltasten im Feld selbst (darum tabindex -1). */
export function zahlFeld(eingabe) {
  return `<span class="zahl-feld">
    <button type="button" class="zahl-knopf" data-zahl-schritt="-1" tabindex="-1" aria-label="${tr('Weniger')}">−</button>
    ${eingabe}
    <button type="button" class="zahl-knopf" data-zahl-schritt="1" tabindex="-1" aria-label="${tr('Mehr')}">+</button></span>`;
}

/* 24 oder 12 Stunden — wie in der eingestellten Sprache und Region üblich. */
const zwoelfStunden = () => {
  try { return new Intl.DateTimeFormat(sprache(), { hour: 'numeric' }).resolvedOptions().hourCycle?.startsWith('h1'); }
  catch { return false; }
};

/* Uhrzeit als zwei Auswahllisten (Stunde, Minute in Viertelstunden). Ein
   verstecktes Feld trägt den Wert „HH:MM" für das Formular und meldet
   jede Änderung (change) wie das alte Uhrzeitfeld. */
export function zeitWahl({ id = '', name = '', wert = '', label = '', attrs = '', versteckt = false, klasse = '' }) {
  const [h0, m0] = (/^\d{1,2}:\d{2}$/.test(wert) ? wert : '00:00').split(':').map(Number);
  const h12 = zwoelfStunden();
  const stunde = h => h12 ? new Date(2000, 0, 1, h).toLocaleTimeString(sprache(), { hour: 'numeric' }) : zwei(h);
  const minuten = [0, 15, 30, 45];
  if (!minuten.includes(m0)) { minuten.push(m0); minuten.sort((a, b) => a - b); }
  const stOpt = Array.from({ length: 24 }, (_, h) => `<option value="${h}"${h === h0 ? ' selected' : ''}>${stunde(h)}</option>`).join('');
  const miOpt = minuten.map(m => `<option value="${m}"${m === m0 ? ' selected' : ''}>${zwei(m)}</option>`).join('');
  const k = klasse ? ` class="${escH(klasse)}"` : '';
  return `<span class="zeit-wahl" role="group"${label ? ` aria-label="${escH(label)}"` : ''}${versteckt ? ' style="display:none"' : ''}>
    <input type="hidden"${name ? ` name="${escH(name)}"` : ''} value="${zwei(h0)}:${zwei(m0)}" ${attrs}>
    <select data-zeit-teil="h"${k}${id ? ` id="${escH(id)}"` : ''} aria-label="${tr('Stunde')}">${stOpt}</select>
    <span class="zeit-trenner" aria-hidden="true">:</span>
    <select data-zeit-teil="m"${k} aria-label="${tr('Minute')}">${miOpt}</select></span>`;
}

/* Datum als Textfeld im Format der Region (TT.MM.JJJJ, MM/DD/YYYY …).
   Gespeichert wird weiter ISO (JJJJ-MM-TT) im versteckten Feld. */
const datumsTeile = () => {
  try {
    return new Intl.DateTimeFormat(sprache(), { year: 'numeric', month: '2-digit', day: '2-digit' })
      .formatToParts(new Date(2000, 10, 22)).map(t => t.type === 'literal' ? { type: 'literal', value: t.value } : { type: t.type });
  } catch { return [{ type: 'day' }, { type: 'literal', value: '.' }, { type: 'month' }, { type: 'literal', value: '.' }, { type: 'year' }]; }
};
export function datumsMuster() {
  const n = { day: tr('Datum|TT'), month: tr('Datum|MM'), year: tr('Datum|JJJJ') };
  return datumsTeile().map(t => t.type === 'literal' ? t.value : n[t.type]).join('');
}
export function datumAnzeigen(iso) {
  const m = /^(\d{4})-(\d{2})-(\d{2})/.exec(iso || '');
  if (!m) return '';
  const w = { year: m[1], month: m[2], day: m[3] };
  return datumsTeile().map(t => t.type === 'literal' ? t.value : w[t.type]).join('');
}
/* Liest, was getippt ist; '' bei leer, null bei ungültig. */
export function datumLesen(text) {
  const t = String(text || '').trim();
  if (!t) return '';
  if (/^\d{4}-\d{2}-\d{2}$/.test(t)) return t;
  const zahlen = t.split(/\D+/).filter(Boolean);
  const reihe = datumsTeile().filter(x => x.type !== 'literal').map(x => x.type);
  if (zahlen.length !== 3) return null;
  const w = {};
  reihe.forEach((r, i) => { w[r] = Number(zahlen[i]); });
  if (w.year < 100) w.year += w.year < 50 ? 2000 : 1900;
  const d = new Date(Date.UTC(w.year, w.month - 1, w.day));
  if (!w.year || d.getUTCMonth() !== w.month - 1 || d.getUTCDate() !== w.day) return null;
  return `${String(w.year).padStart(4, '0')}-${zwei(w.month)}-${zwei(w.day)}`;
}
export function datumFeld({ id = '', name = '', wert = '' }) {
  return `<span class="datum-feld">
    <input type="hidden" name="${escH(name)}" value="${escH(wert || '')}">
    <input type="text" data-datum-text${id ? ` id="${escH(id)}"` : ''} inputmode="numeric" autocomplete="off"
      placeholder="${escH(datumsMuster())}" value="${escH(datumAnzeigen(wert))}"></span>`;
}

/* Ein Mal für alles, was diese Felder brauchen (auch in Dialogen, die am
   body hängen). Im Einfangen, damit der versteckte Wert stimmt, bevor
   andere Zuhörer das Formular lesen. */
(() => {
  if (typeof document === 'undefined' || !document.addEventListener) return;
  const melden = el => el.dispatchEvent(new Event('change', { bubbles: true }));
  document.addEventListener('change', e => {
    const t = e.target;
    if (t?.matches?.('[data-zeit-teil]')) {
      const huelle = t.closest('.zeit-wahl');
      const h = huelle.querySelector('[data-zeit-teil="h"]').value, m = huelle.querySelector('[data-zeit-teil="m"]').value;
      const feld = huelle.querySelector('input[type=hidden]');
      feld.value = `${zwei(h)}:${zwei(m)}`;
      queueMicrotask(() => melden(feld));
    }
  }, true);
  const datumPruefen = t => {
    const iso = datumLesen(t.value);
    const feld = t.closest('.datum-feld')?.querySelector('input[type=hidden]');
    if (!feld) return;
    if (iso === null) {
      t.setCustomValidity(tr('Bitte ein Datum wie {muster} eingeben.', { muster: datumsMuster() }));
      t.setAttribute('aria-invalid', 'true');
      return;
    }
    t.setCustomValidity('');
    t.removeAttribute('aria-invalid');
    if (feld.value !== iso) { feld.value = iso; melden(feld); }
  };
  document.addEventListener('input', e => {
    const t = e.target;
    if (t?.matches?.('[data-datum-text]')) datumPruefen(t);
    // Textfelder wachsen mit dem Text, statt einen Ziehgriff zu zeigen.
    if (t?.tagName === 'TEXTAREA') hoeheAnpassen(t);
    // Eine Fehlermeldung unter dem Feld verschwindet, sobald es stimmt.
    if (t?.getAttribute?.('aria-invalid') === 'true' && t.checkValidity?.()) feldFehler(t, '');
  }, true);
  document.addEventListener('focusout', e => {
    const t = e.target;
    if (!t?.matches?.('[data-datum-text]')) return;
    datumPruefen(t);
    const iso = datumLesen(t.value);
    if (iso) t.value = datumAnzeigen(iso);
  }, true);
  /* Statt der Blase des Browsers („Please fill out this field.", in der
     Sprache des Browsers) eine eigene, übersetzte Zeile unter dem Feld.
     Der Browser hält das Formular weiter an — nur die Blase entfällt. */
  document.addEventListener('invalid', e => {
    const el = e.target;
    if (!el?.form && !el?.closest?.('.lumen')) return;
    e.preventDefault();
    feldFehler(el, pruefText(el));
    const erstes = el.form?.querySelector(':invalid:not(fieldset)');
    if (!erstes || erstes === el) el.focus();
  }, true);
  document.addEventListener('click', e => {
    const k = e.target?.closest?.('[data-zahl-schritt]');
    if (!k) return;
    const feld = k.parentElement.querySelector('input[type=number]');
    if (!feld || feld.disabled) return;
    if (feld.value === '') feld.value = feld.min || '0';
    else if (k.dataset.zahlSchritt === '1') feld.stepUp(); else feld.stepDown();
    feld.dispatchEvent(new Event('input', { bubbles: true }));
    melden(feld);
  });
})();

/* Höhe eines Textfelds nach seinem Inhalt (bis höchstens 60 % des Bildes). */
export function hoeheAnpassen(t) {
  if (!t || CSS.supports?.('field-sizing', 'content')) return;
  t.style.height = 'auto';
  t.style.height = Math.min(t.scrollHeight + 2, innerHeight * 0.6) + 'px';
}

/* Der Text zu einem ungültigen Feld, in der Sprache der Oberfläche. */
function pruefText(el) {
  const v = el.validity || {};
  if (v.customError) return el.validationMessage;
  if (v.valueMissing) return el.type === 'checkbox' ? tr('Bitte einschalten, um fortzufahren.') : tr('Bitte ausfüllen.');
  if (v.tooShort) return tr('Mindestens {n} Zeichen.', { n: el.minLength });
  if (v.tooLong) return tr('Höchstens {n} Zeichen.', { n: el.maxLength });
  if (v.typeMismatch) return el.type === 'email' ? tr('Bitte eine gültige E-Mail-Adresse eingeben.') : el.type === 'url'
    ? tr('Bitte eine vollständige Adresse eingeben, etwa https://…') : tr('Dieser Wert passt nicht.');
  if (v.rangeUnderflow) return tr('Mindestens {n}.', { n: el.min });
  if (v.rangeOverflow) return tr('Höchstens {n}.', { n: el.max });
  if (v.stepMismatch || v.badInput) return el.type === 'number' ? tr('Bitte eine ganze Zahl eingeben.') : tr('Dieser Wert passt nicht.');
  return el.title || tr('Dieser Wert passt nicht.');
}

/* Fehlerzeile unter einem Feld zeigen ('' = entfernen). */
export function feldFehler(el, text) {
  if (!el) return;
  const anker = el.closest('.zahl-feld, .zeit-wahl, .datum-feld, .marken') || el;
  const id = (el.id || el.name || 'feld') + '-fehler';
  let zeile = anker.parentElement?.querySelector(`:scope > .feld-fehler[data-fuer="${CSS.escape(id)}"]`);
  if (!text) {
    zeile?.remove();
    el.removeAttribute('aria-invalid');
    return;
  }
  if (!zeile) {
    zeile = document.createElement('span');
    zeile.className = 'feld-fehler';
    zeile.dataset.fuer = id;
    zeile.id = id + '-' + (feldFehler.n = (feldFehler.n || 0) + 1);
    zeile.setAttribute('role', 'alert');
    anker.insertAdjacentElement('afterend', zeile);
  }
  zeile.textContent = text;
  el.setAttribute('aria-invalid', 'true');
  const ids = new Set((el.getAttribute('aria-describedby') || '').split(/\s+/).filter(Boolean));
  ids.add(zeile.id);
  el.setAttribute('aria-describedby', [...ids].join(' '));
}
