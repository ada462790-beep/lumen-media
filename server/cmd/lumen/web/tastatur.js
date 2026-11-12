/*
 Lumen — eigener Streaming-Server fürs Heimnetz.
 Copyright (C) 2026 Die Lumen-Autoren

 Veröffentlicht unter der GNU Affero General Public License v3.0.
 Der vollständige Text steht in der Datei LICENSE.
 SPDX-License-Identifier: AGPL-3.0-only
*/

/* Bedienen ohne Maus: Pfeiltasten und Fernbedienung.
 *
 * Ein Fernseher-Browser hat keine Tab-Taste, nur ein Steuerkreuz und
 * „Zurück". Bisher scrollten die Pfeile nur die Seite, der Fokus blieb
 * stehen. Jetzt springt er — wie auf dem Apple TV — zum nächsten Knopf
 * oder zur nächsten Karte in Pfeilrichtung (räumliche Navigation).
 *
 * Dazu:
 *   - Zurück (Backspace, BrowserBack, GoBack, die Zurück-Taste von webOS
 *     und Tizen) führt eine Seite zurück.
 *   - Einfachauswahl (role="radiogroup") mit den Pfeiltasten.
 *   - F6 springt zwischen Inhalt und Navigation.
 *
 * Wo die Pfeiltasten schon etwas tun — Textfelder, Menüs, Schieberegler,
 * der Player — bleibt es dabei. */

const FOKUSSIERBAR = 'a[href], button:not([disabled]), input:not([disabled]):not([type=hidden]), select:not([disabled]), textarea, [tabindex]:not([tabindex="-1"])';

// Ein Fernseher-Browser oder ein Gerät ganz ohne Zeiger: Dort beginnt die
// räumliche Navigation auch dann, wenn der Fokus noch auf der Überschrift steht.
const AUF_TV = /SMART-TV|SmartTV|Tizen|Web0S|webOS|NetCast|HbbTV|BRAVIA|AFT[A-Z]|CrKey|GoogleTV|Android TV|AppleTV|PhilipsTV|VIDAA/i.test(navigator.userAgent || '')
  || (typeof matchMedia === 'function' && matchMedia('(any-pointer: none)').matches);

const ZURUECK_TASTEN = new Set(['BrowserBack', 'GoBack']);
const ZURUECK_CODES = new Set([461, 10009]); // webOS, Tizen

function sichtbar(el) {
  if (!el.isConnected || el.closest('[hidden], [inert], [aria-hidden="true"]')) return false;
  const r = el.getBoundingClientRect();
  if (r.width < 2 || r.height < 2) return false;
  const cs = getComputedStyle(el);
  return cs.visibility !== 'hidden' && cs.display !== 'none' && Number(cs.opacity) > 0.05;
}

// Wo die Pfeile schon eine Aufgabe haben.
function eigenePfeile(el) {
  if (!el || !el.closest) return false;
  if (el.isContentEditable) return true;
  if (el.matches('input:not([type=checkbox]):not([type=radio]):not([type=button]):not([type=submit]), textarea, select')) return true;
  return !!el.closest('[role="slider"], [role="menu"], [role="menubar"], [role="listbox"], [role="radiogroup"], [role="tablist"], [role="grid"], .spuren, [data-bar]');
}

/* Der nächste Kandidat in Pfeilrichtung. Gewichtet wird der Abstand in
   Richtung voll, der quer dazu dreifach — so bleibt man in seiner Reihe,
   statt schräg in die nächste zu springen. */
function naechster(von, richtung, kandidaten) {
  const a = von.getBoundingClientRect();
  const ax = a.left + a.width / 2, ay = a.top + a.height / 2;
  let best = null, bestWert = Infinity;
  for (const el of kandidaten) {
    if (el === von || von.contains(el) || el.contains(von)) continue;
    const b = el.getBoundingClientRect();
    const bx = b.left + b.width / 2, by = b.top + b.height / 2;
    let haupt, quer;
    if (richtung === 'ArrowRight') { if (b.left < a.right - 4 && bx <= ax + 1) continue; haupt = Math.max(0, b.left - a.right); quer = Math.abs(by - ay); }
    else if (richtung === 'ArrowLeft') { if (b.right > a.left + 4 && bx >= ax - 1) continue; haupt = Math.max(0, a.left - b.right); quer = Math.abs(by - ay); }
    else if (richtung === 'ArrowDown') { if (b.top < a.bottom - 4 && by <= ay + 1) continue; haupt = Math.max(0, b.top - a.bottom); quer = Math.abs(bx - ax); }
    else { if (b.bottom > a.top + 4 && by >= ay - 1) continue; haupt = Math.max(0, a.top - b.bottom); quer = Math.abs(bx - ax); }
    // Waagerecht: Überlappen sich die Zeilen, zählt quer kaum.
    const ueberlapp = richtung === 'ArrowRight' || richtung === 'ArrowLeft'
      ? Math.min(a.bottom, b.bottom) - Math.max(a.top, b.top) > 0
      : Math.min(a.right, b.right) - Math.max(a.left, b.left) > 0;
    const wert = haupt + (ueberlapp ? quer * 0.3 : quer * 3 + 40);
    if (wert < bestWert) { bestWert = wert; best = el; }
  }
  return best;
}

function kandidatenIn(wurzel) {
  return [...wurzel.querySelectorAll(FOKUSSIERBAR)].filter(el => !el.closest('.rail-pfeil, .skip-link') && sichtbar(el));
}

/**
 * @param {{app: HTMLElement, imPlayer: () => boolean, zurueck: () => void}} o
 */
export function tastaturEinrichten({ app, imPlayer, zurueck }) {
  document.addEventListener('keydown', e => {
    if (e.defaultPrevented || e.ctrlKey || e.metaKey || e.altKey) return;
    const fokus = document.activeElement instanceof Element ? document.activeElement : null;

    // Einfachauswahl: Pfeile wählen den Nachbarn (wie echte Radios).
    const radio = fokus?.closest('[role="radiogroup"] [role="radio"]');
    if (radio && e.key.startsWith('Arrow')) {
      const alle = [...radio.closest('[role="radiogroup"]').querySelectorAll('[role="radio"]')];
      const i = alle.indexOf(radio);
      const vor = e.key === 'ArrowRight' || e.key === 'ArrowDown';
      const ziel = alle[(i + (vor ? 1 : alle.length - 1)) % alle.length];
      if (ziel) { e.preventDefault(); ziel.click(); requestAnimationFrame(() => document.querySelector(`[role="radio"][aria-checked="true"]`)?.focus()); }
      return;
    }

    const tippt = fokus && (fokus.isContentEditable || fokus.matches('input, textarea, select'));
    const dialog = document.querySelector('dialog[open]');

    // Zurück-Taste der Fernbedienung (und Backspace ausserhalb von Feldern).
    if ((ZURUECK_TASTEN.has(e.key) || ZURUECK_CODES.has(e.keyCode) || (e.key === 'Backspace' && !tippt)) && !dialog) {
      if (tippt && e.key === 'Backspace') return;
      e.preventDefault();
      if (imPlayer()) {
        // Der Player hat sein eigenes „Zurück" auf Esc.
        document.activeElement?.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));
        return;
      }
      zurueck();
      return;
    }

    // F6: zwischen Inhalt und Navigation wechseln.
    if (e.key === 'F6' && !dialog && !imPlayer()) {
      const nav = [...app.querySelectorAll('.sidebar .nav, .tabbar .tab')].find(sichtbar);
      const inNav = fokus?.closest('.sidebar, .tabbar');
      e.preventDefault();
      if (inNav || !nav) {
        const h1 = app.querySelector('main h1') || app.querySelector('main');
        if (h1) { h1.setAttribute('tabindex', '-1'); h1.focus(); }
      } else {
        (app.querySelector('.sidebar .nav[aria-current="page"], .tabbar .tab[aria-current="page"]') || nav).focus();
      }
      return;
    }

    if (!['ArrowUp', 'ArrowDown', 'ArrowLeft', 'ArrowRight'].includes(e.key)) return;
    if (imPlayer() || tippt || eigenePfeile(fokus) || e.shiftKey) return;
    const wurzel = dialog || app;
    const aufElement = fokus && fokus !== document.body && wurzel.contains(fokus) && fokus.matches(FOKUSSIERBAR);
    if (!aufElement && !AUF_TV) return; // am Schreibtisch scrollen die Pfeile weiter die Seite
    const kandidaten = kandidatenIn(wurzel);
    if (!kandidaten.length) return;
    let ziel;
    if (aufElement) ziel = naechster(fokus, e.key, kandidaten);
    else if (e.key === 'ArrowDown' || e.key === 'ArrowRight') {
      // Vom Seitenanfang (Überschrift) aus: das erste Element im Inhalt.
      ziel = kandidaten.find(el => el.closest('main') && el.getBoundingClientRect().bottom > 0) || kandidaten[0];
    }
    if (!ziel) return;
    e.preventDefault();
    ziel.focus({ preventScroll: true });
    ziel.scrollIntoView({ block: 'nearest', inline: 'nearest', behavior: matchMedia('(prefers-reduced-motion: reduce)').matches ? 'auto' : 'smooth' });
  });
}
