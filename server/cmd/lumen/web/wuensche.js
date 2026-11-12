// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

/* Wünschen mit Vorschlägen.
 *
 * Wer einen Namen tippt, sieht darunter, was TMDb dazu kennt — Poster,
 * Titel, Jahr, Film oder Serie — und wählt einen Treffer aus. Dann kennt
 * der Wunsch TMDb- und IMDb-Kennung und erfüllt sich von selbst, sobald
 * der Titel in der Bibliothek steht. Wer nur den Namen wünschen will,
 * drückt Enter oder „Nur den Namen wünschen".
 *
 * Das Feld ist eine Combobox nach WAI-ARIA: Der Fokus bleibt im Feld, die
 * Pfeiltasten wandern durch die Liste (aria-activedescendant), Enter
 * wählt, Escape schliesst. Ohne TMDb-Schlüssel bleibt es ein Textfeld.
 *
 * Die Wunschliste selbst zeichnet app.js; von hier kommen Poster und
 * Verweise einer Zeile (wunschBild, wunschVerweise). */

import { api } from './api.js';
import { tr } from './sprache.js';
import { escH } from './fenster.js';

const WARTEN_MS = 300;

const mitSchluessel = url => url && api.mediaKey
  ? url + (url.includes('?') ? '&' : '?') + api.keyParam() : url;

const artName = art => art === 'series' ? tr('Serie') : tr('Film');

/* Das Feld. wert: vorbelegt (aus der Suche); symbol: das Plus von app.js. */
export function wunschFeld({ id = 'wunsch', wert = '', symbol = '', knopf = tr('Wünschen') } = {}) {
  const liste = `${id}-liste`;
  return `<form id="${escH(id)}" class="wunschfeld" data-wunschfeld autocomplete="off" novalidate>
    <div class="sfield wunschfeld-eingabe">
      ${symbol}<input name="title" type="text" role="combobox" aria-autocomplete="list" aria-expanded="false"
        aria-controls="${escH(liste)}" aria-haspopup="listbox" enterkeyhint="send" autocapitalize="sentences"
        value="${escH(wert)}" placeholder="${escH(tr('Titel wünschen …'))}" aria-label="${escH(tr('Titel wünschen'))}"
        maxlength="200"${wert ? ' data-wunsch-sofort' : ''}>
      <button class="btn btn-pill btn-pill--fill wunschfeld-knopf" type="submit">${escH(knopf)}</button>
    </div>
    <div class="wunschfeld-liste popover glass" id="${escH(liste)}" role="listbox"
      aria-label="${escH(tr('Vorschläge von TMDb'))}" hidden></div>
    <p class="sr-only" aria-live="polite" data-wunsch-status></p>
  </form>`;
}

/* Kleines Poster einer Zeile der Wunschliste (oder ein leerer Platz). */
export function wunschBild(w) {
  return w.poster
    ? `<span class="wunsch-bild"><img src="${escH(mitSchluessel(w.poster))}" alt="" loading="lazy"></span>`
    : `<span class="wunsch-bild wunsch-bild--leer" aria-hidden="true"></span>`;
}

/* „2021 · Film" */
export function wunschZusatz(w) {
  const teile = [];
  if (w.year) teile.push(String(w.year));
  if (w.art) teile.push(artName(w.art));
  if (w.originaltitel) teile.push(w.originaltitel);
  return teile.map(escH).join(' · ');
}

/* IMDb, TMDb und — sobald da — Ansehen. */
export function wunschVerweise(w, { extern = true } = {}) {
  const v = [];
  if (w.itemId) {
    v.push(`<button type="button" class="btn btn-pill btn-pill--fill wunsch-ansehen" data-wunsch-ansehen="${escH(w.itemId)}">${escH(tr('Ansehen'))}</button>`);
  }
  if (extern && w.imdbId && /^tt\d+$/.test(w.imdbId)) {
    v.push(`<a class="wunsch-link" href="https://www.imdb.com/title/${escH(w.imdbId)}/" target="_blank" rel="noopener noreferrer">IMDb</a>`); // sprache: fest – Name
  }
  if (extern && w.tmdbId && /^\d+$/.test(w.tmdbId) && w.art) {
    const pfad = w.art === 'series' ? 'tv' : 'movie';
    v.push(`<a class="wunsch-link" href="https://www.themoviedb.org/${pfad}/${escH(w.tmdbId)}" target="_blank" rel="noopener noreferrer">TMDb</a>`); // sprache: fest – Name
  }
  return v.length ? `<span class="wunsch-verweise">${v.join('')}</span>` : '';
}

/* ---------------------------------------------------------- Verhalten */

function stand(form) {
  if (!form._wunsch) form._wunsch = { liste: [], aktiv: -1, timer: 0, nr: 0, ohne: false, text: '' };
  return form._wunsch;
}

function zu(form) {
  const st = stand(form), feld = form.title, liste = form.querySelector('[role="listbox"]');
  st.aktiv = -1;
  if (liste) { liste.hidden = true; liste.innerHTML = ''; }
  feld?.setAttribute('aria-expanded', 'false');
  feld?.removeAttribute('aria-activedescendant');
}

function zeichnen(form) {
  const st = stand(form), feld = form.title, liste = form.querySelector('[role="listbox"]');
  const name = feld.value.trim();
  if (!liste) return;
  if (!st.liste.length && !name) return zu(form);
  const id = liste.id;
  const zeilen = st.liste.map((v, i) => {
    const marke = v.schonDa
      ? `<span class="wv-marke wv-marke--da">${escH(tr('Schon da — ansehen'))}</span>`
      : v.schonGewuenscht ? `<span class="wv-marke">${escH(tr('Schon gewünscht'))}</span>` : '';
    const zusatz = [v.jahr ? String(v.jahr) : '', artName(v.art), v.originaltitel || ''].filter(Boolean).map(escH).join(' · ');
    return `<div class="wv" role="option" id="${id}-${i}" aria-selected="false" data-wv="${i}">
      <span class="wv-bild">${v.poster ? `<img src="${escH(mitSchluessel(v.poster))}" alt="" loading="lazy">` : ''}</span>
      <span class="wv-text"><span class="wv-titel">${escH(v.titel)}</span><span class="wv-zusatz">${zusatz}</span></span>
      ${marke}</div>`;
  });
  // Immer da: mit dem Namen allein wünschen.
  zeilen.push(`<div class="wv wv--name" role="option" id="${id}-name" aria-selected="false" data-wv="name">
    <span class="wv-text"><span class="wv-titel">${escH(tr('Nur den Namen wünschen'))}</span>
    <span class="wv-zusatz">„${escH(name)}“</span></span></div>`);
  liste.innerHTML = zeilen.join('');
  liste.hidden = false;
  feld.setAttribute('aria-expanded', 'true');
  st.aktiv = Math.min(st.aktiv, st.liste.length);
  markieren(form);
  const status = form.querySelector('[data-wunsch-status]');
  if (status) status.textContent = st.liste.length
    ? tr('{n} Vorschläge – mit den Pfeiltasten auswählen', { n: st.liste.length })
    : tr('Keine Vorschläge – Enter wünscht den Namen');
}

function markieren(form) {
  const st = stand(form), feld = form.title;
  const optionen = [...form.querySelectorAll('[role="option"]')];
  optionen.forEach((o, i) => o.setAttribute('aria-selected', i === st.aktiv ? 'true' : 'false'));
  const aktiv = optionen[st.aktiv];
  if (aktiv) {
    feld.setAttribute('aria-activedescendant', aktiv.id);
    aktiv.scrollIntoView({ block: 'nearest' });
  } else {
    feld.removeAttribute('aria-activedescendant');
  }
}

async function holen(form) {
  const st = stand(form), feld = form.title;
  const text = feld.value.trim();
  if (st.ohne) return;
  if (text.length < 2) { st.liste = []; st.text = text; return text ? zeichnen(form) : zu(form); }
  const nr = ++st.nr;
  let antwort;
  try {
    antwort = await api.wishSuggestions(text);
  } catch (err) {
    // Gebremst oder nicht erreichbar: die alten Vorschläge bleiben, der
    // Name lässt sich trotzdem wünschen.
    if (nr === st.nr && document.activeElement === feld) zeichnen(form);
    return;
  }
  if (nr !== st.nr || !form.isConnected) return; // inzwischen weitergetippt
  if (antwort?.ohneSchluessel) { st.ohne = true; return zu(form); }
  st.liste = antwort?.vorschlaege || [];
  st.text = text;
  st.aktiv = -1;
  if (document.activeElement === feld) zeichnen(form);
}

/* Einmal einrichten — über Ereignisse am Wurzelelement, weil app.js die
 * Seiten bei jedem Zeichnen neu aufbaut.
 *   gewuenscht(wunsch, schonGewuenscht): nach dem Abschicken
 *   ansehen(itemId): zur Titelseite
 *   fehler(meldung) */
export function wunschFeldEinrichten(wurzel, { gewuenscht, ansehen, fehler }) {
  async function abschicken(form, v) {
    if (form.dataset.laeuft) return;
    const feld = form.title;
    const name = feld.value.trim();
    if (!v && !name) return feld.focus();
    if (v?.schonDa && v.itemId) { zu(form); return ansehen(v.itemId); }
    form.dataset.laeuft = '1';
    form.setAttribute('aria-busy', 'true');
    zu(form);
    try {
      const w = v
        ? await api.wish(v.titel, v.jahr || 0, { art: v.art, tmdbId: v.tmdbId, imdbId: v.imdbId || '', originaltitel: v.originaltitel || '' })
        : await api.wish(name, 0);
      feld.value = '';
      stand(form).liste = [];
      await gewuenscht(w, !!(v?.schonGewuenscht || w?.dabei));
    } catch (err) {
      fehler(err.message);
    } finally {
      delete form.dataset.laeuft;
      form.removeAttribute('aria-busy');
    }
  }

  function waehlen(form, schluessel) {
    const st = stand(form);
    if (schluessel === 'name' || schluessel === st.liste.length) return abschicken(form, null);
    const v = st.liste[Number(schluessel)];
    if (v) return abschicken(form, v);
  }

  wurzel.addEventListener('input', e => {
    const feld = e.target;
    if (!(feld instanceof HTMLInputElement) || !feld.matches('[data-wunschfeld] input[role="combobox"]')) return;
    const form = feld.form, st = stand(form);
    clearTimeout(st.timer);
    // Die Zeile „Nur den Namen" folgt dem Tippen sofort, die Vorschläge
    // nach einer kurzen Pause.
    if (st.liste.length || feld.value.trim()) zeichnen(form); else zu(form);
    st.timer = setTimeout(() => holen(form), WARTEN_MS);
  });

  wurzel.addEventListener('keydown', e => {
    const feld = e.target;
    if (!(feld instanceof HTMLInputElement) || !feld.matches('[data-wunschfeld] input[role="combobox"]')) return;
    const form = feld.form, st = stand(form);
    const offen = feld.getAttribute('aria-expanded') === 'true';
    const anzahl = form.querySelectorAll('[role="option"]').length;
    if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      e.preventDefault();
      if (!offen) { zeichnen(form); if (!feld.value.trim()) return; }
      const n = form.querySelectorAll('[role="option"]').length;
      if (!n) return;
      st.aktiv = e.key === 'ArrowDown' ? (st.aktiv + 1) % n : (st.aktiv <= 0 ? n - 1 : st.aktiv - 1);
      markieren(form);
    } else if (e.key === 'Enter' && offen && st.aktiv >= 0 && st.aktiv < anzahl) {
      e.preventDefault();
      const o = form.querySelectorAll('[role="option"]')[st.aktiv];
      waehlen(form, o?.dataset.wv);
    } else if (e.key === 'Escape' && offen) {
      // Erst die Liste zu; ein zweites Escape gibt das Feld frei.
      e.preventDefault();
      e.stopPropagation();
      zu(form);
    } else if (e.key === 'Tab') {
      zu(form);
    }
  });

  // Ein Klick in die Liste nimmt dem Feld den Fokus nicht.
  wurzel.addEventListener('mousedown', e => {
    if (e.target instanceof Element && e.target.closest('[data-wunschfeld] [role="listbox"]')) e.preventDefault();
  });

  wurzel.addEventListener('click', e => {
    if (!(e.target instanceof Element)) return;
    const o = e.target.closest('[data-wunschfeld] [role="option"]');
    if (o) { e.preventDefault(); e.stopPropagation(); return waehlen(o.closest('form'), o.dataset.wv); }
    const ansehenKnopf = e.target.closest('[data-wunsch-ansehen]');
    if (ansehenKnopf) { e.preventDefault(); e.stopPropagation(); return ansehen(ansehenKnopf.dataset.wunschAnsehen); }
  }, true);

  wurzel.addEventListener('focusout', e => {
    const form = e.target instanceof Element ? e.target.closest('[data-wunschfeld]') : null;
    if (!form) return;
    setTimeout(() => { if (!form.contains(document.activeElement)) zu(form); }, 0);
  });

  wurzel.addEventListener('focusin', e => {
    const feld = e.target;
    if (!(feld instanceof HTMLInputElement) || !feld.matches('[data-wunschfeld] input[role="combobox"]')) return;
    const st = stand(feld.form);
    if (feld.value.trim() && (st.text === feld.value.trim())) zeichnen(feld.form);
  });

  // Enter ohne gewählten Vorschlag, der Knopf „Wünschen": der Name allein.
  wurzel.addEventListener('submit', e => {
    const form = e.target;
    if (!(form instanceof HTMLFormElement) || !form.matches('[data-wunschfeld]')) return;
    e.preventDefault();
    e.stopImmediatePropagation();
    abschicken(form, null);
  }, true);

  return {
    /* Nach dem Zeichnen: ein vorbelegtes Feld (aus der Suche) fragt gleich. */
    nachZeichnen() {
      const feld = wurzel.querySelector('[data-wunschfeld] input[data-wunsch-sofort]');
      if (!feld || feld.dataset.gefragt) return;
      feld.dataset.gefragt = '1';
      feld.focus();
      feld.setSelectionRange(feld.value.length, feld.value.length);
      holen(feld.form);
    },
  };
}
