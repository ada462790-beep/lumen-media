// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

/* Profile schützen: PIN und Passwort ändern, Kinderprofile, Einladungen.
 *
 * Eigene Datei, damit app.js nicht noch länger wird. Hier stehen die
 * Dialoge und Bausteine; app.js ruft sie auf und hält den Zustand.
 *
 * Alle Texte sind ganze Sätze (mit Platzhaltern wie {name}), damit sie
 * sich später übersetzen lassen, ohne Satzteile zusammenzusetzen.
 */

import { tr, texte } from './sprache.js';

const esc = s => String(s ?? '').replace(/[&<>"]/g,
  c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' })[c]);

/* Die Texte an einer Stelle. {x} wird ersetzt. texte() übersetzt beim
   Lesen – so gilt ein Wechsel der Sprache auch für diese Tabelle. */
export const T = texte({
  ohneGrenze: 'ohne Grenze',
  bisFsk: 'bis FSK {n}',
  kinderprofil: 'Kinderprofil',
  kinderprofilBis: 'Kinderprofil · bis FSK {n}',
  schutzPin: 'Geschützt mit einer PIN.',
  schutzPasswort: 'Geschützt mit einem Passwort.',
  schutzOhne: 'Ohne Schutz – jeder im Haushalt kann dein Profil öffnen.',
  schutzOhneKurz: 'ohne Schutz',
  schutzPinKurz: 'mit PIN',
  schutzPasswortKurz: 'mit Passwort',
  falschHilfePin: 'Falls du sie vergessen hast, kann {name} sie in der Verwaltung zurücksetzen.',
  falschHilfePasswort: 'Falls du es vergessen hast, kann {name} es in der Verwaltung zurücksetzen.',
  verwaltungBrauchtSchutz: 'Bevor du die Verwaltung öffnest, schütze dein Profil mit einer PIN oder einem Passwort. Sonst könnte jeder im Haushalt mit einem Klick hinein.',
  unterwegsFrage: 'Diese Anmeldung kommt von unterwegs, nicht aus deinem Heimnetz. Gib den Code nur frei, wenn „{geraet}“ wirklich dein Gerät ist – niemand von Lumen fragt dich je nach so einem Code.',
});

// t: früher eigene Ersetzung, jetzt die Übersetzung selbst (sprache.js).
export { tr as t };

/* Altersgrenzen: -1 heisst „ohne Grenze", 0 ist „nur FSK 0". */
export const ALTERSSTUFEN = [-1, 0, 6, 12, 16, 18];
export const KINDERSTUFEN = [0, 6, 12, 16];

export function altersName(n) {
  return n < 0 ? T.ohneGrenze : tr(T.bisFsk, { n });
}

export function altersOptionen(wert, stufen = ALTERSSTUFEN) {
  return stufen.map(n => `<option value="${n}"${Number(wert) === n ? ' selected' : ''}>${esc(altersName(n))}</option>`).join('');
}

/* Wie ein Profil geschützt ist — kurz für Listen, lang für „Ich". */
export function schutzText(art, kurz = false) {
  if (art === 'pin') return kurz ? T.schutzPinKurz : T.schutzPin;
  if (art === 'password') return kurz ? T.schutzPasswortKurz : T.schutzPasswort;
  return kurz ? T.schutzOhneKurz : T.schutzOhne;
}

/* Die Meldung bei falschem PIN: was nicht stimmt, und wer helfen kann. */
export function falschMeldung(err) {
  let msg = err.message || '';
  if (err.hilfe) msg += ' ' + tr(err.art === 'password' ? T.falschHilfePasswort : T.falschHilfePin, { name: err.hilfe });
  return msg;
}

/* ---------------------------------------------------------------- Dialoge */

function dialogRahmen(titelId) {
  const d = document.createElement('dialog');
  d.className = 'lumen glass glass--thick dialog-karte';
  if (document.documentElement.classList.contains('hell')) d.classList.add('light');
  d.setAttribute('aria-labelledby', titelId);
  // Wie der Bestätigungsdialog in app.js: Schrift, Lage und Grösse
  // ausdrücklich, weil ein <dialog> sie nicht von der Seite erbt.
  d.style.cssText = 'position:fixed;inset:0;margin:auto;height:fit-content;max-height:calc(100vh - 32px);' +
    'border:0;border-radius:22px;padding:22px;width:min(380px,calc(100vw - 32px));' +
    'box-sizing:border-box;overflow:auto;color:var(--label)';
  return d;
}

const FELD = 'width:100%;box-sizing:border-box;height:44px;border-radius:12px;border:0;outline:none;' +
  'background:var(--fill);color:var(--label);padding:0 14px;font:inherit;font-size:1rem';

/* Eine Frage mit Ja/Abbrechen. Gibt true zurück, wenn bestätigt. */
export function frage(titel, text, ja) {
  return new Promise(fertig => {
    const d = dialogRahmen('frage-t');
    d.innerHTML = `<form method="dialog" style="display:flex;flex-direction:column;gap:12px">
      <div id="frage-t" class="t-title" style="font-size:1.1875rem;font-weight:700">${esc(titel)}</div>
      <div class="t-body c-2">${esc(text)}</div>
      <div style="display:flex;gap:10px;justify-content:flex-end">
        <button type="button" class="btn btn-pill" value="ab">${tr('Abbrechen')}</button>
        <button class="btn btn-pill btn-pill--fill" value="ok">${esc(ja)}</button></div></form>`;
    document.body.appendChild(d);
    let ok = false;
    d.querySelector('button[value="ab"]').addEventListener('click', () => d.close());
    d.querySelector('form').addEventListener('submit', () => { ok = true; });
    d.addEventListener('close', () => { d.remove(); fertig(ok); });
    d.showModal();
  });
}

/* Eine Mitteilung mit nur einem Knopf – für Hinweise, bei denen es
   nichts zu entscheiden gibt. */
export function mitteilung(titel, text) {
  return new Promise(fertig => {
    const d = dialogRahmen('mitteilung-t');
    d.setAttribute('role', 'alertdialog');
    d.innerHTML = `<form method="dialog" style="display:flex;flex-direction:column;gap:12px">
      <div id="mitteilung-t" class="t-title" style="font-size:1.1875rem;font-weight:700">${esc(titel)}</div>
      <div class="t-body c-2">${esc(text)}</div>
      <div style="display:flex;justify-content:flex-end">
        <button class="btn btn-pill btn-pill--fill" value="ok">${tr('OK')}</button></div></form>`;
    document.body.appendChild(d);
    d.addEventListener('close', () => { d.remove(); fertig(); });
    d.showModal();
  });
}

/* PIN oder Passwort setzen.
 *   mitAlt:    erst den bisherigen Code fragen (eigenes Profil)
 *   altArt:    womit das Profil bisher geschützt ist
 *   ohneErlaubt: „Ohne Schutz" anbieten (nicht für Verwaltungsprofile)
 *   warnung:   was das Speichern sonst noch bewirkt – hervorgehoben, weil
 *              es nicht rückgängig zu machen ist
 *   senden(werte) → Promise; wirft einen Fehler, bleibt der Dialog offen
 *   und zeigt die Meldung. */
export function schutzDialog({ titel, hinweis = '', warnung = '', mitAlt = false, altArt = 'none', ohneErlaubt = true, senden }) {
  return new Promise(fertig => {
    const d = dialogRahmen('schutz-t');
    const arten = [['pin', 'PIN'], ['password', tr('Passwort')]];
    if (ohneErlaubt) arten.push(['none', tr('Ohne Schutz')]);
    let art = altArt === 'password' ? 'password' : 'pin';
    const zeichnen = fehler => {
      d.innerHTML = `<form method="dialog" style="display:flex;flex-direction:column;gap:12px">
        <div id="schutz-t" class="t-title" style="font-size:1.1875rem;font-weight:700">${esc(titel)}</div>
        ${hinweis ? `<div class="t-caption c-2">${esc(hinweis)}</div>` : ''}
        ${warnung ? `<div class="t-caption" role="note" style="color:var(--red,#ff453a);font-weight:600">${esc(warnung)}</div>` : ''}
        ${mitAlt && altArt !== 'none' ? `<label class="t-caption c-2">${altArt === 'password' ? tr('Bisheriges Passwort') : tr('Bisherige PIN')}
          <input type="password" name="alt" autocomplete="current-password" required style="${FELD};margin-top:6px"
            inputmode="${altArt === 'pin' ? 'numeric' : 'text'}"></label>` : ''}
        <div role="tablist" style="display:inline-flex;padding:3px;border-radius:999px;background:var(--fill);gap:2px;align-self:flex-start">
          ${arten.map(([v, n]) => `<button type="button" class="tab-pill" role="tab" aria-selected="${art === v}" data-art="${v}"
            style="height:30px;font-size:.875rem">${n}</button>`).join('')}</div>
        ${art === 'none' ? `<div class="t-caption c-2">${esc(T.schutzOhne)}</div>` : `
          <label class="t-caption c-2">${art === 'pin' ? tr('Neue PIN – mindestens vier Ziffern') : tr('Neues Passwort – mindestens acht Zeichen')}
            <input type="password" name="neu" autocomplete="new-password" required minlength="${art === 'pin' ? 4 : 8}"
              ${art === 'pin' ? 'inputmode="numeric" pattern="[0-9]*"' : ''} style="${FELD};margin-top:6px"></label>
          <label class="t-caption c-2">${tr('Noch einmal')}
            <input type="password" name="neu2" autocomplete="new-password" required style="${FELD};margin-top:6px"
              ${art === 'pin' ? 'inputmode="numeric"' : ''}></label>`}
        <div class="err" role="alert">${esc(fehler || '')}</div>
        <div style="display:flex;gap:10px;justify-content:flex-end">
          <button type="button" class="btn btn-pill" value="ab">${tr('Abbrechen')}</button>
          <button class="btn btn-pill btn-pill--fill" value="ok">${tr('Speichern')}</button></div></form>`;
      d.querySelector('button[value="ab"]').addEventListener('click', () => { d.close(); });
      d.querySelectorAll('[data-art]').forEach(b => b.addEventListener('click', () => {
        art = b.dataset.art; zeichnen('');
      }));
      d.querySelector('form').addEventListener('submit', async e => {
        e.preventDefault();
        const f = e.target;
        const neu = f.neu ? f.neu.value : '';
        if (f.neu2 && f.neu2.value !== neu) return zeichnen(tr('Die beiden Eingaben stimmen nicht überein.'));
        try {
          await senden({ alt: f.alt ? f.alt.value : '', art, neu });
          fertig(true); d.close();
        } catch (err) {
          // Am Merkmal, nicht am Text erkennen: der Text ist übersetzt.
          if (err?.abgebrochen) return;
          zeichnen(err?.message || tr('Das hat nicht geklappt.'));
        }
      });
      const erstes = d.querySelector('input');
      if (erstes) erstes.focus();
    };
    zeichnen('');
    document.body.appendChild(d);
    d.addEventListener('close', () => { d.remove(); fertig(false); });
    d.showModal();
    const erstes = d.querySelector('input');
    if (erstes) erstes.focus();
  });
}

/* ---------------------------------------------------------------- Einladen */

/* Das Formular in „Konten und Rechte": Erwachsener oder Kind, bei Kindern
   die Altersgrenze, und welche Bibliotheken. Früher gab ein Knopf ohne
   Rückfrage alles frei und legte immer ein Erwachsenenkonto an. */
export function einladungsFormular(bibliotheken, kind) {
  return `<form id="inviteform" style="display:grid;gap:14px">
    <div role="radiogroup" aria-label="${tr('Art des Profils')}" style="display:flex;gap:18px;flex-wrap:wrap">
      <label class="t-body" style="display:flex;gap:8px;align-items:center"><input type="radio" name="art" value="erwachsen"${kind ? '' : ' checked'} data-einladungsart>${tr('Erwachsenes Profil')}</label>
      <label class="t-body" style="display:flex;gap:8px;align-items:center"><input type="radio" name="art" value="kind"${kind ? ' checked' : ''} data-einladungsart>${esc(T.kinderprofil)}</label>
    </div>
    <div class="field" data-einladung-alter${kind ? '' : ' hidden'}><label for="ialter">${tr('Altersgrenze')}</label>
      <select id="ialter" name="alter">${altersOptionen(12, KINDERSTUFEN)}</select>
      <span class="hint">${tr('Ein Kinderprofil sieht nur Titel mit passender Freigabe – ungeprüfte Titel bleiben verborgen, Herunterladen ist aus.')}</span></div>
    <div class="field"><label>${tr('Bibliotheken')}</label>
      ${bibliotheken.length ? bibliotheken.map(l => `<label class="row" style="min-height:36px;padding:0;cursor:pointer">
        <input type="checkbox" name="lib" value="${esc(l.id)}" checked style="margin-right:10px"><span class="t-body">${esc(l.name)}</span></label>`).join('')
        : `<span class="hint">${tr('Noch keine Bibliotheken.')}</span>`}</div>
    <div><button class="btn btn-primary" type="submit">${tr('Einladungslink erzeugen')}</button></div>
  </form>`;
}

// „2IH44II3V7DNNZNY" → „2IH4-4II3-V7DN-NZNY": in Vierergruppen liest
// und tippt man ihn am Fernseher leichter. Der Server nimmt ihn mit und
// ohne Bindestriche.
const codeGruppen = code => String(code).match(/.{1,4}/g)?.join('-') || code;

export function einladungsBox(url, code) {
  return `
    <div class="field" style="margin-top:14px">
      <label for="einladungslink">${tr('Diesen Link weitergeben')}</label>
      <div style="display:flex;gap:8px">
        <input id="einladungslink" class="mono" readonly style="flex:1;min-width:0" value="${esc(url)}" data-auswahl>
        <button class="btn btn-pill btn-pill--fill" style="flex:none" data-kopieren="${esc(url)}">${tr('Kopieren')}</button>
      </div>
    </div>
    <div class="field" style="margin-top:10px">
      <label>${tr('Oder nur der Code')}</label>
      <div style="display:flex;gap:8px;align-items:center">
        <span class="mono t-title3" style="flex:1;letter-spacing:.08em">${esc(codeGruppen(code))}</span>
        <button class="btn btn-pill btn-pill--fill" style="flex:none" data-kopieren="${esc(code)}">${tr('Kopieren')}</button>
      </div>
      <span class="hint">${tr('In der App auf dem Fernseher ist der Code einfacher als der Link.')}</span>
    </div>`;
}
