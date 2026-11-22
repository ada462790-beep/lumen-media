// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

// Die eingebaute Seite: Server hinzufügen, wählen, entfernen. Sie spricht
// über die Brücke (bruecke.js) mit der App; die prüft die Adresse (aus Go,
// nicht aus der Seite — ein Lumen-Server erlaubt fremden Seiten keine
// Abfragen, und das soll so bleiben) und merkt sich die Liste in
// %APPDATA%\Lumen\server.json.

'use strict';

const DE = (navigator.language || 'de').toLowerCase().startsWith('de');
document.documentElement.lang = DE ? 'de' : 'en';

// Zwei Sprachen: Deutsch (Schweizer Schreibweise, wie die Web-App für
// de-CH) und Englisch für alles andere. Die Texte meiden ß-Wörter, damit
// sie auch in Deutschland und Österreich stimmen.
const T = DE ? {
  untertitelNeu: 'Verbinde dich mit deinem Lumen-Server.',
  untertitelListe: 'Wähle einen Server.',
  listeTitel: 'Deine Server',
  neu: 'Server hinzufügen',
  formTitel: 'Server hinzufügen',
  adresse: 'Adresse',
  hinweis: 'Die Adresse, unter der Lumen im Browser läuft – etwa 192.168.10.10:8097 oder lumen.example.org. Ohne Anschluss versucht Lumen 8097.',
  verbinden: 'Verbinden',
  abbrechen: 'Abbrechen',
  suche: 'Suche Lumen …',
  gefunden: 'Gefunden: {name}',
  entfernen: '{name} entfernen',
  entfernenFrage: 'Entfernen?',
  entfernenJa: 'Entfernen',
  aktiv: 'zuletzt gewählt',
  erneut: 'Erneut versuchen',
  fuss: 'Lumen für Windows {v} · Server wechseln: Strg+Umschalt+S',
  fehler: {
    adresse_ungueltig: 'Das sieht nicht nach einer Adresse aus. Beispiel: 192.168.10.10:8097',
    nicht_erreichbar: 'Unter dieser Adresse antwortet nichts. Läuft der Server, und ist dieser Rechner im selben Netz?',
    kein_lumen: 'Dort antwortet etwas, aber kein Lumen-Server.',
    zertifikat: 'Das Zertifikat des Servers ist nicht gültig. Versuche es mit http:// statt https:// oder richte ein gültiges Zertifikat ein.',
    unbekannt: 'Das hat nicht geklappt. Versuche es noch einmal.',
  },
  ladenFehler: {
    nicht_erreichbar: '{name} ist gerade nicht erreichbar.',
    zertifikat: 'Das Zertifikat von {name} ist nicht gültig.',
  },
} : {
  untertitelNeu: 'Connect to your Lumen server.',
  untertitelListe: 'Choose a server.',
  listeTitel: 'Your Servers',
  neu: 'Add Server',
  formTitel: 'Add Server',
  adresse: 'Address',
  hinweis: 'The address you use for Lumen in the browser – for example 192.168.10.10:8097 or lumen.example.org. Without a port, Lumen tries 8097.',
  verbinden: 'Connect',
  abbrechen: 'Cancel',
  suche: 'Looking for Lumen …',
  gefunden: 'Found: {name}',
  entfernen: 'Remove {name}',
  entfernenFrage: 'Remove?',
  entfernenJa: 'Remove',
  aktiv: 'last used',
  erneut: 'Try Again',
  fuss: 'Lumen for Windows {v} · Switch server: Ctrl+Shift+S',
  fehler: {
    adresse_ungueltig: 'That doesn’t look like an address. Example: 192.168.10.10:8097',
    nicht_erreichbar: 'Nothing answers at this address. Is the server running, and is this computer on the same network?',
    kein_lumen: 'Something answers there, but it isn’t a Lumen server.',
    zertifikat: 'The server’s certificate isn’t valid. Try http:// instead of https:// or set up a valid certificate.',
    unbekannt: 'That didn’t work. Please try again.',
  },
  ladenFehler: {
    nicht_erreichbar: '{name} can’t be reached right now.',
    zertifikat: 'The certificate of {name} isn’t valid.',
  },
};
const fuellen = (s, w) => s.replace(/\{(\w+)\}/g, (_, k) => (w[k] ?? ''));

const $ = id => document.getElementById(id);
const frage = (art, daten) => {
  const d = window.lumenDesktop;
  if (!d) return Promise.reject(Object.assign(new Error('ohne App'), { code: 'unbekannt' }));
  return d._frage(art, daten);
};

const S = { server: [], aktiv: '', version: '', modus: 'liste', fragtWeg: '' };
const params = new URLSearchParams(location.search);

const ICON_WEG = '<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" aria-hidden="true"><path d="M6 6l12 12M18 6L6 18"/></svg>';
const ICON_PFEIL = '<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" class="pfeil"><path d="M9 5l7 7-7 7"/></svg>';

function zeichnen() {
  const mitListe = S.server.length > 0;
  if (!mitListe) S.modus = 'form';
  $('untertitel').textContent = mitListe && S.modus === 'liste' ? T.untertitelListe : T.untertitelNeu;
  $('liste-block').hidden = S.modus !== 'liste';
  $('form-block').hidden = S.modus !== 'form';
  $('abbrechen').hidden = !mitListe;
  $('fuss').textContent = fuellen(T.fuss, { v: S.version || '' });

  const liste = $('liste');
  liste.replaceChildren();
  for (const s of S.server) {
    const zeile = document.createElement('div');
    zeile.className = 'zeile';
    zeile.setAttribute('role', 'listitem');
    if (S.fragtWeg === s.id) {
      const text = document.createElement('div');
      text.className = 'zeile-haupt';
      text.style.cursor = 'default';
      text.innerHTML = '<span class="zeile-text"><span class="t-headline"></span><span class="t-caption c-2"></span></span>';
      text.querySelector('.t-headline').textContent = s.name;
      text.querySelector('.t-caption').textContent = T.entfernenFrage;
      const frageBox = document.createElement('div');
      frageBox.className = 'frage';
      const nein = knopf(T.abbrechen, 'btn btn-pill btn-pill--fill', () => { S.fragtWeg = ''; zeichnen(); fokus(`[data-weg="${s.id}"]`); });
      const ja = knopf(T.entfernenJa, 'btn btn-pill btn-rot', () => entfernen(s.id));
      frageBox.append(nein, ja);
      zeile.append(text, frageBox);
      liste.append(zeile);
      setTimeout(() => nein.focus(), 0);
      continue;
    }
    const haupt = document.createElement('button');
    haupt.type = 'button';
    haupt.className = 'zeile-haupt';
    haupt.dataset.waehlen = s.id;
    haupt.innerHTML = `<span class="punkt${s.id === S.aktiv ? ' punkt--an' : ''}" aria-hidden="true"></span>
      <span class="zeile-text"><span class="t-headline"></span><span class="t-caption c-2"></span></span>`;
    haupt.querySelector('.t-headline').textContent = s.name;
    haupt.querySelector('.t-caption').textContent = s.adresse.replace(/^http:\/\//, '') + (s.id === S.aktiv ? ' · ' + T.aktiv : '');
    haupt.insertAdjacentHTML('beforeend', ICON_PFEIL);
    haupt.addEventListener('click', () => waehlen(s.id));
    const weg = knopf('', 'btn weg', () => { S.fragtWeg = s.id; zeichnen(); });
    weg.innerHTML = ICON_WEG;
    weg.dataset.weg = s.id;
    weg.setAttribute('aria-label', fuellen(T.entfernen, { name: s.name }));
    weg.title = weg.getAttribute('aria-label');
    zeile.append(haupt, weg);
    liste.append(zeile);
  }
}

function knopf(text, klasse, aktion) {
  const b = document.createElement('button');
  b.type = 'button';
  b.className = klasse;
  b.textContent = text;
  b.addEventListener('click', aktion);
  return b;
}
const fokus = sel => setTimeout(() => document.querySelector(sel)?.focus(), 0);

function meldung(text, mitErneut) {
  $('meldung').hidden = !text;
  $('meldung-text').textContent = text || '';
  $('erneut').hidden = !mitErneut;
}

function status(text, art) {
  const st = $('status');
  st.className = 'status' + (art ? ' status--' + art : '');
  st.replaceChildren();
  if (!text) return;
  if (art === 'suche') {
    const s = document.createElement('span');
    s.className = 'spin';
    s.setAttribute('aria-hidden', 'true');
    st.append(s);
  }
  st.append(document.createTextNode(text));
}

async function laden() {
  try {
    const d = await frage('liste');
    S.server = d.server || [];
    S.aktiv = d.aktiv || '';
    S.version = d.version || '';
  } catch { /* ohne Liste bleibt das Formular */ }
  zeichnen();
}

async function waehlen(id) {
  meldung('');
  try { await frage('waehlen', { id }); } catch (e) { meldung(T.fehler[e.code] || T.fehler.unbekannt); }
}

async function entfernen(id) {
  S.fragtWeg = '';
  try {
    const d = await frage('entfernen', { id });
    S.server = d.server || [];
    S.aktiv = d.aktiv || '';
  } catch { /* Liste bleibt */ }
  if (params.get('id') === id) meldung('');
  zeichnen();
  if (S.modus === 'liste') fokus('#neu'); else fokus('#adresse');
}

async function verbinden(ev) {
  ev.preventDefault();
  const feld = $('adresse');
  const adresse = feld.value.trim();
  feld.removeAttribute('aria-invalid');
  if (!adresse) { feld.focus(); return; }
  $('verbinden').disabled = true;
  status(T.suche, 'suche');
  try {
    const s = await frage('hinzufuegen', { adresse });
    status(fuellen(T.gefunden, { name: s.name }), 'ok');
    // Den Namen einen Moment zeigen, dann die Web-App laden.
    setTimeout(() => waehlen(s.id), 700);
  } catch (e) {
    $('verbinden').disabled = false;
    feld.setAttribute('aria-invalid', 'true');
    status(T.fehler[e.code] || T.fehler.unbekannt, 'fehler');
    feld.focus();
  }
}

function start() {
  $('liste-titel').textContent = T.listeTitel;
  $('neu').textContent = T.neu;
  $('form-titel').textContent = T.formTitel;
  $('adresse-label').textContent = T.adresse;
  $('hinweis').textContent = T.hinweis;
  $('verbinden').textContent = T.verbinden;
  $('abbrechen').textContent = T.abbrechen;
  $('erneut').textContent = T.erneut;

  $('neu').addEventListener('click', () => { S.modus = 'form'; meldung(''); status(''); zeichnen(); fokus('#adresse'); });
  $('abbrechen').addEventListener('click', () => { S.modus = 'liste'; status(''); zeichnen(); fokus('#neu'); });
  $('form').addEventListener('submit', verbinden);
  $('erneut').addEventListener('click', () => { const id = params.get('id'); if (id) waehlen(id); });

  laden().then(() => {
    // Kam die Seite, weil ein Server nicht lud? Dann sagen, welcher.
    const art = params.get('fehler'), id = params.get('id');
    const s = S.server.find(x => x.id === id);
    if (art && s) {
      const satz = T.ladenFehler[art] || T.ladenFehler.nicht_erreichbar;
      meldung(fuellen(satz, { name: s.name }), true);
      fokus('#erneut');
    } else if (S.modus === 'form') {
      fokus('#adresse');
    }
  });
}

start();
