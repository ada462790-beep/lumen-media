// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

/* Umzug von Emby oder Jellyfin: den eigenen Stand übernehmen.
 *
 * Für jedes Profil selbst, auf jedem Gerät (auch dem Handy). Vier
 * Schritte: Adresse → Anmeldung → Vorschau → fertig. Die Verwaltung sieht
 * davon nichts; das Passwort geht nur einmal an den eigenen Lumen-Server,
 * der damit bei Emby anfragt und es danach vergisst. */

import { api } from './api.js';
import { U, escH, fenster, meldung, fehlerText, beschaeftigt } from './fenster.js';
import { tr, sprache } from './sprache.js';

// Zahlen in der Schreibweise der Sprache (1’234 / 1.234 / 1,234).
const zahl = n => (n || 0).toLocaleString(sprache());

function schritte(n) {
  return `<div class="schritte" aria-hidden="true">${[1, 2, 3, 4].map(i => `<span class="${i <= n ? 'an' : ''}"></span>`).join('')}</div>`;
}

export function uebernahmeOeffnen() {
  let fertig = false;
  let lauf = null; // Zeitgeber des Nachfragens
  const d = fenster({ titel: tr('Von Emby oder Jellyfin übernehmen'), zu: () => {
    clearTimeout(lauf);
    // Vorschau verwerfen — sie liegt sonst eine halbe Stunde im Speicher.
    api.call('/me/uebernahme', { method: 'DELETE' }).catch(() => {});
    if (fertig) U.load?.();
  } });
  let server = null, adresse = '';

  const schritt1 = () => {
    d.inhalt.innerHTML = `${schritte(1)}
      <p class="t-body c-2" style="margin:6px 0 0;line-height:1.5">${tr('Lumen holt aus deinem bisherigen Server, was du gesehen hast, wo du stehen geblieben bist, deine Favoriten und Bewertungen — {nur}.', { nur: `<b>${tr('nur für dein Profil')}</b>` })}</p>
      <form autocomplete="off" data-ue1>
        <div class="ff"><div class="ff-kopf"><label for="ue-adr">${tr('Adresse von Emby oder Jellyfin')}</label></div>
          <input id="ue-adr" name="adresse" type="url" inputmode="url" required value="${escH(adresse)}" placeholder="${tr('http://192.168.1.20:…')}">
          <span class="ff-hinweis">${tr('So, wie du sie im Browser aufrufst — mit http:// und Port. Im Heimnetz ist das in Ordnung.')}</span></div>
        <div data-meldung hidden></div>
        <div class="fenster-fuss"><button class="btn btn-primary" style="height:40px" data-laeuft="${tr('Verbinde …')}">${tr('Weiter')}</button></div>
      </form>`;
    d.inhalt.querySelector('input').focus();
  };

  const schritt2 = () => {
    // Der Server nennt vor der Anmeldung nur die Art (Emby/Jellyfin) –
    // keinen Namen, keine Fassung, keine Benutzerliste (api/uebernahme.go).
    d.inhalt.innerHTML = `${schritte(2)}
      <div class="fenster-meldung fenster-meldung--info" style="margin-top:4px">${escH(server.art)}</div>
      <form autocomplete="off" data-ue2>
        <div class="ff"><div class="ff-kopf"><label for="ue-name">${tr('Dein Benutzername bei {art}', { art: escH(server.art) })}</label></div>
          <input id="ue-name" name="benutzer" required autocomplete="off"></div>
        <div class="ff"><div class="ff-kopf"><label for="ue-pw">${tr('Passwort')}</label></div>
          <input id="ue-pw" name="passwort" type="password" required autocomplete="off">
          <span class="ff-hinweis">${tr('Wird nur für diese Anmeldung gebraucht und nirgends gespeichert. Ein Konto ohne Passwort lässt sich nicht übernehmen – setze bei {art} zuerst eines.', { art: escH(server.art) })}</span></div>
        <div data-meldung hidden></div>
        <div class="fenster-fuss">
          <button type="button" class="btn btn-pill btn-pill--fill" style="height:40px" data-ue-zurueck>${tr('Zurück')}</button>
          <button class="btn btn-primary" style="height:40px" data-laeuft="${tr('Meldet an …')}">${tr('Stand holen')}</button></div>
      </form>`;
    d.inhalt.querySelector('#ue-name').focus();
  };

  const warten = st => {
    d.inhalt.innerHTML = `${schritte(3)}
      <div class="fenster-laden" role="status" aria-live="polite"><span class="spin"></span>
        ${st?.phase === 'laden' ? `${tr('Lese deinen Stand …')} ${st.gelesen ? `(${tr('{n} Titel', { n: zahl(st.gelesen) })})` : ''}` : tr('Verbinde mit {server} …', { server: escH(server?.art || tr('dem Server')) })}</div>
      <p class="ff-hinweis">${tr('Bei grossen Mediatheken kann das eine Minute dauern.')}</p>`;
  };

  const vorschau = st => {
    const v = st.vorschau, a = v.aenderungen || {};
    const kachel = (n, text) => `<div class="zahl"><b>${zahl(n)}</b><span>${text}</span></div>`;
    const wahl = (name, text, n, neu) => `<label class="wahl-check"><input type="checkbox" name="${name}" ${n ? 'checked' : 'disabled'}>
      <span style="flex:1">${text}</span><span class="t-caption c-2">${neu ? tr('{n} neu', { n: zahl(neu) }) : n ? tr('schon in Lumen') : '—'}</span></label>`;
    d.inhalt.innerHTML = `${schritte(3)}
      <div class="zahlen">
        ${kachel(v.gesehen, tr('gesehen'))}${kachel(v.angefangen, tr('angefangen'))}${kachel(v.favoriten, tr('Favoriten'))}
        ${kachel(v.bewertungen, tr('Bewertungen'))}${kachel(v.nichtGefunden, tr('nicht gefunden'))}
      </div>
      <form data-ue3 style="margin-top:16px">
        <div class="t-sub c-2" style="margin-bottom:4px">${tr('Übernehmen:')}</div>
        ${wahl('gesehen', tr('Gesehen'), v.gesehen, a.gesehen)}
        ${wahl('angefangen', tr('Wo du stehen geblieben bist'), v.angefangen, a.angefangen)}
        ${wahl('favoriten', tr('Favoriten'), v.favoriten, a.favoriten)}
        ${wahl('bewertungen', tr('Bewertungen'), v.bewertungen, a.bewertungen)}
        <p class="ff-hinweis" style="margin-top:8px">${tr('Was in Lumen schon steht, bleibt: Gesehenes wird nicht zurückgenommen, eine weitere Stelle nicht zurückgesetzt, eine Bewertung nicht überschrieben.')}${a.unveraendert ? ' ' + tr('{n} Einträge sind schon so in Lumen.', { n: zahl(a.unveraendert) }) : ''}</p>
        ${v.nichtGefunden ? `<details style="margin-top:10px"><summary class="t-sub" style="cursor:pointer">${tr('{n} nicht in Lumen gefunden', { n: zahl(v.nichtGefunden) })}</summary>
          <div class="fehlend">${(v.fehlend || []).map(escH).join('<br>')}${v.nichtGefunden > (v.fehlend || []).length ? '<br>…' : ''}</div>
          <p class="ff-hinweis">${tr('Diese Titel gibt es in Lumen nicht — oder nicht in den Bibliotheken, die dein Profil sehen darf.')}</p></details>` : ''}
        <div data-meldung hidden></div>
        <div class="fenster-fuss">
          <button type="button" class="btn btn-pill btn-pill--fill" style="height:40px" data-fenster-zu>${tr('Abbrechen')}</button>
          <button class="btn btn-primary" style="height:40px" data-laeuft="${tr('Übernimmt …')}"${v.gesehen + v.angefangen + v.favoriten + v.bewertungen ? '' : ' disabled'}>${tr('Übernehmen')}</button></div>
      </form>`;
  };

  const ergebnis = st => {
    const e = st.ergebnis || {};
    fertig = true;
    const teile = [e.gesehen ? tr('{n} als gesehen', { n: zahl(e.gesehen) }) : '', e.angefangen ? tr('{n} zum Weiterschauen', { n: zahl(e.angefangen) }) : '',
      e.favoriten ? tr('{n} Favoriten', { n: zahl(e.favoriten) }) : '', e.bewertungen ? tr('{n} Bewertungen', { n: zahl(e.bewertungen) }) : ''].filter(Boolean);
    d.inhalt.innerHTML = `${schritte(4)}
      <div class="fenster-meldung fenster-meldung--gut" role="status">${teile.length ? tr('Übernommen: {was}.', { was: teile.join(', ') }) : tr('Es gab nichts Neues zu übernehmen.')}</div>
      <p class="ff-hinweis" style="margin-top:10px">${tr('Lumen hat sich bei {server} wieder abgemeldet und nichts davon aufbewahrt.', { server: escH(server?.art || tr('dem Server')) })}</p>
      <div class="fenster-fuss"><button class="btn btn-primary" style="height:40px" data-fenster-zu>${tr('Fertig')}</button></div>`;
  };

  const nachfragen = async () => {
    let st;
    try { st = await api.call('/me/uebernahme'); }
    catch (err) { schritt2(); meldung(d.inhalt, fehlerText(err)); return; }
    if (!d.isConnected) return;
    if (st.phase === 'verbinden' || st.phase === 'laden') { warten(st); lauf = setTimeout(nachfragen, 700); return; }
    if (st.phase === 'fehler') { schritt2(); meldung(d.inhalt, st.meldung || tr('Das hat nicht geklappt.')); return; }
    if (st.phase === 'bereit') { vorschau(st); return; }
    if (st.phase === 'fertig') { ergebnis(st); return; }
    schritt1();
  };

  d.addEventListener('submit', async e => {
    e.preventDefault();
    const f = e.target;
    meldung(d.inhalt, '');
    if (f.matches('[data-ue1]')) {
      adresse = f.adresse.value.trim();
      if (!/^https?:\/\//i.test(adresse)) { meldung(d.inhalt, tr('Bitte mit http:// oder https:// beginnen.')); return; }
      try {
        await beschaeftigt(f.querySelector('button'), async () => {
          server = await api.call('/me/uebernahme/server', { method: 'POST', body: { adresse } });
        });
        schritt2();
      } catch (err) { meldung(d.inhalt, fehlerText(err)); }
    } else if (f.matches('[data-ue2]')) {
      const benutzer = f.benutzer.value.trim();
      if (!benutzer) { meldung(d.inhalt, tr('Bitte den Benutzernamen angeben.')); return; }
      try {
        await beschaeftigt(f.querySelector('button:not([type=button])'), () =>
          api.call('/me/uebernahme', { method: 'POST', body: { adresse, benutzer, passwort: f.passwort.value } }));
        f.passwort.value = '';
        warten({ phase: 'verbinden' });
        lauf = setTimeout(nachfragen, 400);
      } catch (err) { meldung(d.inhalt, fehlerText(err)); }
    } else if (f.matches('[data-ue3]')) {
      const body = Object.fromEntries(['gesehen', 'angefangen', 'favoriten', 'bewertungen'].map(k => [k, !!f[k]?.checked]));
      if (!Object.values(body).some(Boolean)) { meldung(d.inhalt, tr('Bitte mindestens eines auswählen.')); return; }
      try {
        let st;
        await beschaeftigt(f.querySelector('button:not([type=button])'), async () => {
          st = await api.call('/me/uebernahme/anwenden', { method: 'POST', body });
        });
        ergebnis(st);
      } catch (err) { meldung(d.inhalt, fehlerText(err)); }
    }
  });
  d.addEventListener('click', e => { if (e.target.closest('[data-ue-zurueck]')) schritt1(); });

  schritt1();
}
