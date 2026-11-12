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
 * der damit bei Emby anfragt und es danach vergisst.
 *
 * Wer die Mediathek verwaltet, kann am Schreibtisch zusätzlich die
 * Zuordnungen (IMDb/TMDb/TVDb) der Filme und Serien übernehmen — das gilt
 * dann für alle, nicht nur für das eigene Profil. */

import { api } from './api.js';
import { U, escH, fenster, meldung, fehlerText, beschaeftigt, amSchreibtisch } from './fenster.js';
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
  // Zuordnungen übernehmen: nur mit Verwaltungsrechten und am Schreibtisch.
  const darfZuordnen = !!U.S?.me?.account?.isAdmin && amSchreibtisch();
  let zuordnungen = false;

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
        ${darfZuordnen ? `<label class="wahl-check" style="margin-top:6px"><input type="checkbox" name="zuordnungen"${zuordnungen ? ' checked' : ''}>
          <span style="flex:1">${tr('Zuordnungen (IMDb/TMDb/TVDb) übernehmen')}
            <span class="ff-hinweis" style="display:block">${tr('Welcher Film und welche Serie zu welchem Eintrag gehört, so wie in {art} — gilt für alle Profile. Lumen findet die Titel über die Dateien.', { art: escH(server.art) })}</span></span></label>` : ''}
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

  // Die Zuordnungen in der Vorschau: Zahlen und zwei Schalter.
  const zuordnungsTeil = z => {
    if (!z) return '';
    const fehlend = z.nichtGefunden ? `<details style="margin-top:6px"><summary class="t-caption c-2" style="cursor:pointer">${tr('{n} nicht in Lumen gefunden', { n: zahl(z.nichtGefunden) })}</summary>
      <div class="fehlend">${(z.fehlend || []).map(escH).join('<br>')}${z.nichtGefunden > (z.fehlend || []).length ? '<br>…' : ''}</div>
      <p class="ff-hinweis">${tr('Gesucht wird über den Dateipfad (Dateiname und Ordner). Liegt die Datei in Lumen anders oder fehlt sie, bleibt der Titel unverändert.')}</p></details>` : '';
    return `<div class="t-sub c-2" style="margin:14px 0 4px">${tr('Zuordnungen (gilt für alle Profile):')}</div>
      ${z.ohneQuelle ? `<div class="fenster-meldung fenster-meldung--info">${z.schluessel === 'abgelehnt'
        ? tr('TMDb lehnt den Schlüssel in Lumen ab (Verwaltung › Poster und Beschreibungen). Lumen speichert die Kennungen trotzdem; Poster und Beschreibungen kommen, sobald ein gültiger Schlüssel eingetragen ist.')
        : tr('In Lumen ist kein TMDb-Schlüssel eingerichtet (Verwaltung › Poster und Beschreibungen). Lumen speichert die Kennungen trotzdem; Poster und Beschreibungen kommen, sobald ein Schlüssel eingetragen ist.')}</div>` : ''}
      <label class="wahl-check"><input type="checkbox" name="zuordnungen" ${z.neu || z.gesperrt ? 'checked' : 'disabled'}>
        <span style="flex:1">${tr('IMDb/TMDb/TVDb übernehmen')}</span>
        <span class="t-caption c-2">${z.neu ? tr('{n} neu', { n: zahl(z.neu) }) : '—'}${z.schonRichtig ? ' · ' + tr('{n} schon richtig', { n: zahl(z.schonRichtig) }) : ''}</span></label>
      ${z.gesperrt ? `<label class="wahl-check"><input type="checkbox" name="ueberschreiben">
        <span style="flex:1">${tr('Auch Titel überschreiben, die in Lumen von Hand zugewiesen sind')}</span>
        <span class="t-caption c-2">${zahl(z.gesperrt)}</span></label>` : ''}
      ${z.ohneKennung ? `<p class="ff-hinweis">${tr('{n} Titel haben in {art} keine Kennung.', { n: zahl(z.ohneKennung), art: escH(server?.art || '') })}</p>` : ''}
      ${fehlend}`;
  };

  const vorschau = st => {
    const v = st.vorschau, a = v.aenderungen || {};
    const z = st.zuordnungen;
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
        ${zuordnungsTeil(z)}
        <div data-meldung hidden></div>
        <div class="fenster-fuss">
          <button type="button" class="btn btn-pill btn-pill--fill" style="height:40px" data-fenster-zu>${tr('Abbrechen')}</button>
          <button class="btn btn-primary" style="height:40px" data-laeuft="${tr('Übernimmt …')}"${v.gesehen + v.angefangen + v.favoriten + v.bewertungen || (z && (z.neu || z.gesperrt)) ? '' : ' disabled'}>${tr('Übernehmen')}</button></div>
      </form>`;
  };

  const ergebnis = st => {
    const e = st.ergebnis || {};
    const zs = st.zuordnungStand;
    fertig = true;
    const teile = [e.gesehen ? tr('{n} als gesehen', { n: zahl(e.gesehen) }) : '', e.angefangen ? tr('{n} zum Weiterschauen', { n: zahl(e.angefangen) }) : '',
      e.favoriten ? tr('{n} Favoriten', { n: zahl(e.favoriten) }) : '', e.bewertungen ? tr('{n} Bewertungen', { n: zahl(e.bewertungen) }) : ''].filter(Boolean);
    const zText = !zs ? '' : zs.laeuft
      ? `<div class="fenster-laden" role="status" aria-live="polite" style="margin-top:10px"><span class="spin"></span>
          ${tr('Übernehme Zuordnungen … {fertig} von {gesamt}', { fertig: zahl(zs.fertig), gesamt: zahl(zs.gesamt) })}</div>
         <p class="ff-hinweis">${tr('Das läuft auch weiter, wenn du das Fenster schliesst. Bilder und Folgen kommen danach im Hintergrund.')}</p>`
      : `<div class="fenster-meldung fenster-meldung--gut" role="status" style="margin-top:10px">${tr('Zuordnungen: {neu} übernommen, {richtig} schon richtig, {hand} von Hand belassen, {fehler} nicht möglich.', {
          neu: zahl(zs.uebernommen), richtig: zahl(zs.schonRichtig), hand: zahl(zs.gesperrt), fehler: zahl(zs.fehler) })}${zs.vorgemerkt
          ? ' ' + (zs.grund === 'abgelehnt'
            ? tr('{n} Kennungen gespeichert — TMDb lehnt den Schlüssel ab; Poster und Beschreibungen kommen mit einem gültigen.', { n: zahl(zs.vorgemerkt) })
            : tr('{n} Kennungen gespeichert — Poster und Beschreibungen kommen, sobald ein TMDb-Schlüssel eingetragen ist.', { n: zahl(zs.vorgemerkt) }))
          : ''}</div>`;
    d.inhalt.innerHTML = `${schritte(4)}
      ${teile.length || !zs ? `<div class="fenster-meldung fenster-meldung--gut" role="status">${teile.length ? tr('Übernommen: {was}.', { was: teile.join(', ') }) : tr('Es gab nichts Neues zu übernehmen.')}</div>` : ''}
      ${zText}
      <p class="ff-hinweis" style="margin-top:10px">${tr('Lumen hat sich bei {server} wieder abgemeldet und nichts davon aufbewahrt.', { server: escH(server?.art || tr('dem Server')) })}</p>
      <div class="fenster-fuss"><button class="btn btn-primary" style="height:40px" data-fenster-zu>${tr('Fertig')}</button></div>`;
    // Während die Zuordnungen laufen: nachfragen.
    if (zs?.laeuft) {
      clearTimeout(lauf);
      lauf = setTimeout(async () => {
        if (!d.isConnected) return;
        try { ergebnis(await api.call('/me/uebernahme')); } catch { /* beim nächsten Öffnen */ }
      }, 1000);
    }
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
        zuordnungen = !!f.zuordnungen?.checked;
        const body = { adresse, benutzer, passwort: f.passwort.value };
        if (zuordnungen) body.zuordnungen = true;
        await beschaeftigt(f.querySelector('button:not([type=button])'), () =>
          api.call('/me/uebernahme', { method: 'POST', body }));
        f.passwort.value = '';
        warten({ phase: 'verbinden' });
        lauf = setTimeout(nachfragen, 400);
      } catch (err) { meldung(d.inhalt, fehlerText(err)); }
    } else if (f.matches('[data-ue3]')) {
      const body = Object.fromEntries(['gesehen', 'angefangen', 'favoriten', 'bewertungen'].map(k => [k, !!f[k]?.checked]));
      if (f.zuordnungen) { body.zuordnungen = !!f.zuordnungen.checked; body.ueberschreiben = !!f.ueberschreiben?.checked; }
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
