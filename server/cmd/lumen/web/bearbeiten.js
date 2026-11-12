// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

/* Metadaten von Hand bearbeiten (Verwaltung, nur am Schreibtisch).
 *
 * Geschickt wird nur, was sich wirklich geändert hat: Der Server sperrt
 * jedes geschickte Feld gegen „Metadaten neu laden" und NFO-Dateien. Wer
 * nur die Beschreibung korrigiert, soll nicht nebenbei den Titel
 * festnageln. Ein Schloss am Feld zeigt die Sperre; ein Klick darauf hebt
 * sie auf. */

import { api } from './api.js';
import { U, escH, fenster, ladeHinweis, meldung, fehlerText, beschaeftigt, hochladen, bildAuffrischen } from './fenster.js';
import { tr, texte } from './sprache.js';

// Eine Funktion: Beschriftungen in der Sprache, die gerade gilt.
const felder = () => ({
  title:         { label: tr('Titel'), art: 'text', pflicht: true },
  originalTitle: { label: tr('Originaltitel'), art: 'text' },
  sortTitle:     { label: tr('Sortiertitel'), art: 'text', hinweis: tr('Wie der Titel im Alphabet einsortiert wird, z. B. „Herr der Ringe 1“. Leer = nach dem Titel.') },
  year:          { label: tr('Jahr'), art: 'zahl', min: 1870, max: 2100 },
  premiered:     { label: tr('Erscheinungsdatum'), art: 'datum' },
  ageRating:     { label: tr('Altersfreigabe'), art: 'text', liste: ['0', '6', '12', '16', '18'],
                   hinweis: tr('Zum Beispiel 12, FSK 16 oder PG-13. Gilt für die Altersgrenzen der Profile.') },
  seasonNo:      { label: tr('Staffelnummer'), art: 'zahl', min: 0, max: 9999, hinweis: tr('0 steht für „Specials“.') },
  episodeNo:     { label: tr('Folgennummer'), art: 'zahl', min: 0, max: 99999 },
  overview:      { label: tr('Beschreibung'), art: 'lang' },
  genres:        { label: tr('Genres'), art: 'marken' },
  cast:          { label: tr('Besetzung'), art: 'leute' },
});
// In dieser Reihenfolge stehen die Felder im Fenster.
const REIHENFOLGE = ['title', 'originalTitle', 'sortTitle', 'year', 'premiered', 'ageRating', 'seasonNo', 'episodeNo', 'overview', 'genres', 'cast'];
const ART_NAME = texte({ movie: 'Film', series: 'Serie', season: 'Staffel', episode: 'Folge' });

const schlossSvg = '<svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round" aria-hidden="true"><path d="M8 10V7a4 4 0 0 1 8 0v3"/><rect x="6" y="10" width="12" height="10" rx="2.5"/></svg>';
const xSvg = '<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" aria-hidden="true"><path d="M6 6l12 12M18 6L6 18"/></svg>';

const pfad = id => `/admin/items/${encodeURIComponent(id)}`;

/* Öffnet den Editor. nachher() läuft nach dem Schliessen, wenn sich etwas
   geändert hat — die Titelseite lädt sich dann neu. */
export async function bearbeitenOeffnen(itemId, nachher) {
  let geaendert = false;
  const d = fenster({ titel: tr('Bearbeiten'), breit: true, zu: () => { if (geaendert && nachher) nachher(); } });
  d.inhalt.innerHTML = ladeHinweis(tr('Metadaten werden geladen …'));
  let stand;
  try {
    // Die Dateien dazu: Verwalter sehen hier den vollen Pfad — so findet
    // man heraus, woher eine falsch erkannte Folge stammt, ohne auf die NAS
    // zu müssen.
    const [m, t] = await Promise.all([api.call(pfad(itemId) + '/metadata'), api.item(itemId).catch(() => null)]);
    stand = m;
    stand.dateien = (t && t.files || []).map(f => f.path).filter(Boolean);
  }
  catch (err) {
    d.inhalt.innerHTML = `<div class="fenster-meldung fenster-meldung--fehler" role="alert">${escH(fehlerText(err))}</div>`;
    return;
  }
  const zeichnen = () => {
    d.titelSetzen(tr('{art} bearbeiten', { art: ART_NAME[stand.item.kind] || tr('Titel') }));
    d.inhalt.innerHTML = formular(stand);
    verdrahten();
  };

  // Was jemand gerade eingibt, gegen den geladenen Stand.
  const wertAus = (feld) => {
    const f = felder()[feld];
    if (f.art === 'marken') return [...d.inhalt.querySelectorAll('[data-marke]')].map(x => x.dataset.marke);
    if (f.art === 'leute') return [...d.inhalt.querySelectorAll('.leute-zeile')].map(z => ({
      name: z.querySelector('[data-name]').value.trim(), rolle: z.querySelector('[data-rolle]').value.trim() })).filter(p => p.name);
    const el = d.inhalt.querySelector(`[name="${feld}"]`);
    if (!el) return undefined;
    if (f.art === 'zahl') return el.value.trim() === '' ? 0 : Number(el.value);
    return el.value.trim();
  };
  const anfang = (feld) => {
    if (feld === 'cast') return (stand.cast || []).map(p => ({ name: p.name, rolle: p.rolle || '' }));
    const v = stand.item[feld];
    if (felder()[feld].art === 'zahl') return v || 0;
    if (felder()[feld].art === 'marken') return v || [];
    return v ?? '';
  };
  const gleich = (a, b) => JSON.stringify(a) === JSON.stringify(b);
  const aenderungen = () => {
    const out = {};
    for (const feld of stand.felder) {
      if (!felder()[feld]) continue;
      const v = wertAus(feld);
      if (v !== undefined && !gleich(v, anfang(feld))) out[feld] = v;
    }
    return out;
  };
  const markieren = () => {
    const a = aenderungen();
    for (const feld of stand.felder) {
      d.inhalt.querySelector(`[data-feld="${feld}"]`)?.classList.toggle('ff--geaendert', feld in a);
    }
    const n = Object.keys(a).length;
    const knopf = d.inhalt.querySelector('[data-speichern]');
    if (knopf) knopf.textContent = n ? tr('{n} Änderungen speichern', { n }) : tr('Speichern');
  };

  function verdrahten() {
    const f = d.inhalt.querySelector('form');
    f.addEventListener('input', markieren);
    f.addEventListener('submit', async e => {
      e.preventDefault();
      const a = aenderungen();
      if (!Object.keys(a).length) { d.close(); return; }
      if ('title' in a && !a.title) { meldung(d.inhalt, tr('Der Titel darf nicht leer sein.')); return; }
      meldung(d.inhalt, '');
      try {
        await beschaeftigt(d.inhalt.querySelector('[data-speichern]'), async () => {
          stand = await api.call(pfad(itemId) + '/metadata', { method: 'PATCH', body: a });
        });
        geaendert = true;
        d.close();
        U.toast?.(tr('Gespeichert — diese Felder bleiben jetzt so'));
      } catch (err) { meldung(d.inhalt, fehlerText(err)); }
    });
    // Genres: Eingabe mit Enter oder Komma wird zur Marke.
    const genreFeld = d.inhalt.querySelector('[data-genre-eingabe]');
    genreFeld?.addEventListener('keydown', e => {
      if (e.key === 'Enter' || e.key === ',') {
        e.preventDefault();
        markeHinzu(genreFeld.value);
      } else if (e.key === 'Backspace' && !genreFeld.value) {
        const letzte = [...d.inhalt.querySelectorAll('[data-marke]')].pop();
        if (letzte) { letzte.remove(); markieren(); }
      }
    });
    genreFeld?.addEventListener('change', () => { if (genreFeld.value.trim()) markeHinzu(genreFeld.value); });
  }
  function markeHinzu(roh) {
    const feld = d.inhalt.querySelector('[data-genre-eingabe]');
    const name = roh.replace(/,/g, '').trim();
    feld.value = '';
    if (!name) return;
    const schon = [...d.inhalt.querySelectorAll('[data-marke]')].some(x => x.dataset.marke.toLowerCase() === name.toLowerCase());
    if (!schon) feld.insertAdjacentHTML('beforebegin', marke(name));
    markieren();
  }

  d.addEventListener('click', async e => {
    let n;
    if ((n = e.target.closest('[data-marke-weg]'))) { n.closest('[data-marke]').remove(); markieren(); return; }
    if (e.target.closest('[data-person-hinzu]')) {
      d.inhalt.querySelector('.leute').insertAdjacentHTML('beforeend', person({ name: '', rolle: '' }));
      d.inhalt.querySelector('.leute-zeile:last-child [data-name]')?.focus();
      return;
    }
    if ((n = e.target.closest('[data-person-weg]'))) { n.closest('.leute-zeile').remove(); markieren(); return; }
    if ((n = e.target.closest('[data-schloss]'))) {
      const feld = n.dataset.schloss;
      n.setAttribute('aria-busy', 'true');
      try {
        // Eingaben behalten: nur das Schloss verschwindet.
        stand = await api.call(`${pfad(itemId)}/lock/${encodeURIComponent(feld)}`, { method: 'DELETE' });
        geaendert = true;
        n.remove();
        meldung(d.inhalt, tr('Die Sperre ist aufgehoben. Beim nächsten „Metadaten neu laden“ darf die Quelle das Feld wieder füllen.'), 'info');
      } catch (err) { n.removeAttribute('aria-busy'); meldung(d.inhalt, fehlerText(err)); }
      return;
    }
    if ((n = e.target.closest('[data-bild-url]'))) {
      const art = n.dataset.bildUrl;
      const zeile = d.inhalt.querySelector(`[data-url-zeile="${art}"]`);
      zeile.hidden = !zeile.hidden;
      if (!zeile.hidden) zeile.querySelector('input').focus();
      return;
    }
    if ((n = e.target.closest('[data-bild-laden]'))) {
      const art = n.dataset.bildLaden;
      const url = d.inhalt.querySelector(`[data-url-zeile="${art}"] input`).value.trim();
      if (!/^https?:\/\//i.test(url)) { meldung(d.inhalt, tr('Bitte eine Adresse mit http:// oder https:// angeben.')); return; }
      await bildSetzen(art, () => api.call(`${pfad(itemId)}/artwork/${art}`, { method: 'POST', body: { url } }), n);
    }
  });
  d.addEventListener('change', async e => {
    const inp = e.target.closest('input[type=file][data-bild-datei]');
    if (!inp || !inp.files?.[0]) return;
    const datei = inp.files[0];
    if (datei.size > 20 * 1024 * 1024) { meldung(d.inhalt, tr('Das Bild ist zu gross (höchstens 20 MB).')); inp.value = ''; return; }
    if (!/^image\/(jpeg|png|gif)$/.test(datei.type)) { meldung(d.inhalt, tr('Nur JPEG, PNG oder GIF.')); inp.value = ''; return; }
    await bildSetzen(inp.dataset.bildDatei, () => hochladen(`${pfad(itemId)}/artwork/${inp.dataset.bildDatei}`, datei), inp.closest('.datei-wahl'));
    inp.value = '';
  });

  async function bildSetzen(art, laden, knopf) {
    meldung(d.inhalt, '');
    const box = d.inhalt.querySelector(`[data-bildbox="${art}"]`);
    try {
      box?.setAttribute('aria-busy', 'true');
      if (box) box.innerHTML = '<span class="spin"></span>';
      await beschaeftigt(knopf?.tagName === 'BUTTON' ? knopf : null, async () => { stand = await laden(); });
      geaendert = true;
      const url = art === 'poster' ? stand.poster : stand.backdrop;
      bildAuffrischen(url);
      // Eingaben im Formular stehen lassen, nur die Bilder neu zeichnen.
      const alt = d.inhalt.querySelector('[data-bilder]');
      if (alt) alt.outerHTML = bilder(stand);
      meldung(d.inhalt, art === 'poster' ? tr('Bild gesetzt und gesperrt.') : tr('Hintergrund gesetzt und gesperrt.'), 'gut');
    } catch (err) {
      const alt = d.inhalt.querySelector('[data-bilder]');
      if (alt) alt.outerHTML = bilder(stand);
      meldung(d.inhalt, fehlerText(err));
    }
  }

  zeichnen();
}

/* ---------------------------------------------------------------- Zeichnen */

const schloss = (stand, feld) => (stand.locks || []).includes(feld)
  ? `<button type="button" class="schloss" data-schloss="${feld}" title="${tr('Gesperrt — klicken hebt die Sperre auf')}"
      aria-label="${escH(tr('{feld}: gesperrt. Sperre aufheben', { feld: felder()[feld]?.label || feld }))}">${schlossSvg}${tr('gesperrt')}</button>` : '';

const marke = name => `<span class="marke" data-marke="${escH(name)}">${escH(name)}
  <button type="button" data-marke-weg aria-label="${escH(tr('{name} entfernen', { name }))}">${xSvg}</button></span>`;

const person = p => `<div class="leute-zeile">
  <input data-name value="${escH(p.name)}" placeholder="${tr('Name')}" aria-label="${tr('Name')}" maxlength="120">
  <input data-rolle value="${escH(p.rolle || '')}" placeholder="${tr('Rolle (optional)')}" aria-label="${tr('Rolle')}" maxlength="120">
  <button type="button" data-person-weg aria-label="${tr('Person entfernen')}">${xSvg}</button></div>`;

function bildFeld(stand, art) {
  const url = art === 'poster' ? stand.poster : stand.backdrop;
  const hoch = art === 'poster';
  const name = art === 'poster' ? (stand.item.kind === 'episode' ? tr('Vorschaubild') : tr('Poster')) : tr('Hintergrund');
  const src = url ? url + (url.includes('?') ? '&' : '?') + api.keyParam() : '';
  return `<div class="bild-feld">
    <div class="ff-kopf"><span style="flex:1">${name}</span>${schloss(stand, art)}</div>
    <div class="bild-vorschau ${hoch && stand.item.kind !== 'episode' ? 'bild-vorschau--hoch' : 'bild-vorschau--quer'}" data-bildbox="${art}">
      ${src ? `<img src="${escH(src)}" alt="">` : tr('kein Bild')}</div>
    <div class="bild-knoepfe">
      <span class="btn btn-pill btn-pill--fill datei-wahl">${tr('Hochladen …')}
        <input type="file" accept="image/jpeg,image/png,image/gif" data-bild-datei="${art}" aria-label="${tr('{name} hochladen', { name })}"></span>
      <button type="button" class="btn btn-pill btn-pill--fill" data-bild-url="${art}">${tr('Von Adresse …')}</button>
    </div>
    <div class="ff" data-url-zeile="${art}" hidden style="margin-top:0">
      <div style="display:flex;gap:6px"><input type="url" placeholder="${tr('https://…')}" aria-label="${tr('Adresse des Bildes')}">
        <button type="button" class="btn btn-pill btn-pill--fill" style="height:40px" data-bild-laden="${art}" data-laeuft="${tr('Lädt …')}">${tr('Laden')}</button></div>
    </div>
  </div>`;
}

function bilder(stand) {
  const mitPoster = stand.felder.includes('poster'), mitHg = stand.felder.includes('backdrop');
  if (!mitPoster && !mitHg) return '<div data-bilder></div>';
  return `<div class="bilder${mitHg ? '' : ' bilder--breit'}" data-bilder>
    ${mitPoster ? bildFeld(stand, 'poster') : ''}
    ${mitHg ? bildFeld(stand, 'backdrop') : ''}
  </div>`;
}

function feldHtml(stand, feld) {
  const f = felder()[feld];
  if (!f) return '';
  const id = 'bf-' + feld;
  const wert = stand.item[feld];
  const kopf = `<div class="ff-kopf"><label for="${id}">${f.label}</label>${schloss(stand, feld)}</div>`;
  const hinweis = f.hinweis ? `<span class="ff-hinweis">${f.hinweis}</span>` : '';
  let eingabe;
  switch (f.art) {
    case 'zahl':
      eingabe = `<input id="${id}" name="${feld}" type="number" inputmode="numeric" min="${f.min}" max="${f.max}" value="${wert || wert === 0 && feld !== 'year' ? escH(wert) : ''}">`;
      break;
    case 'datum':
      eingabe = `<input id="${id}" name="${feld}" type="date" value="${escH(wert || '')}">`;
      break;
    case 'lang':
      eingabe = `<textarea id="${id}" name="${feld}" maxlength="10000">${escH(wert || '')}</textarea>`;
      break;
    case 'marken':
      eingabe = `<div class="marken">${(wert || []).map(marke).join('')}
        <input id="${id}" data-genre-eingabe list="bf-genreliste" placeholder="${tr('Genre hinzufügen …')}" maxlength="40" autocomplete="off"></div>
        <datalist id="bf-genreliste">${(stand.genreVorschlaege || []).map(g => `<option value="${escH(g)}">`).join('')}</datalist>`;
      return `<div class="ff" data-feld="${feld}">${kopf}${eingabe}<span class="ff-hinweis">${tr('Mit Enter oder Komma hinzufügen.')}</span></div>`;
    case 'leute':
      return `<div class="ff" data-feld="${feld}"><div class="ff-kopf"><span style="flex:1">${f.label}</span>${schloss(stand, feld)}</div>
        <div class="leute">${(stand.cast || []).map(person).join('')}</div>
        <div><button type="button" class="btn btn-pill btn-pill--fill" style="height:30px;font-size:.8125rem" data-person-hinzu>${tr('+ Person')}</button></div></div>`;
    default:
      eingabe = `<input id="${id}" name="${feld}" value="${escH(wert || '')}" maxlength="300"${f.pflicht ? ' required' : ''}${
        f.liste ? ` list="${id}-liste"` : ''}${feld === 'sortTitle' ? ` placeholder="${escH(stand.item.title)}"` : ''}>${
        f.liste ? `<datalist id="${id}-liste">${f.liste.map(x => `<option value="${x}">`).join('')}</datalist>` : ''}`;
  }
  return `<div class="ff" data-feld="${feld}">${kopf}${eingabe}${hinweis}</div>`;
}

function formular(stand) {
  const it = stand.item;
  const kurz = REIHENFOLGE.filter(f => stand.felder.includes(f) && ['year', 'premiered', 'ageRating', 'seasonNo', 'episodeNo'].includes(f));
  const rest = REIHENFOLGE.filter(f => stand.felder.includes(f) && !kurz.includes(f));
  const oben = rest.filter(f => ['title', 'originalTitle', 'sortTitle'].includes(f));
  const unten = rest.filter(f => !oben.includes(f));
  return `<form novalidate autocomplete="off">
    ${it.matchState === 'unmatched' || it.matchState === 'pending'
      ? `<div class="fenster-meldung fenster-meldung--info">${it.matchState === 'unmatched'
          ? tr('TMDb kennt diesen Titel nicht. Was du hier einträgst, bleibt so — der Titel steht danach nicht mehr unter „nicht erkannt“.')
          : tr('Für diesen Titel wurden noch keine Metadaten geholt. Titel oder Jahr von Hand zu setzen heisst: Lumen sucht nicht mehr selbst.')}</div>` : ''}
    <p class="ff-hinweis" style="margin:4px 0 0;font-size:.8125rem">${tr('Was du änderst, wird gesperrt: „Metadaten neu laden“ und NFO-Dateien überschreiben es nicht mehr. Ein Klick auf das Schloss hebt die Sperre wieder auf.')}</p>
    ${(stand.dateien || []).length ? `<div class="ff-hinweis" style="margin:8px 0 0;font-size:.75rem">${tr('Datei')}:
      ${stand.dateien.map(x => `<div class="mono" style="overflow-wrap:anywhere">${escH(x)}</div>`).join('')}</div>` : ''}
    ${bilder(stand)}
    ${oben.map(f => feldHtml(stand, f)).join('')}
    ${kurz.length ? `<div class="ff-reihe">${kurz.map(f => feldHtml(stand, f)).join('')}</div>` : ''}
    ${unten.map(f => feldHtml(stand, f)).join('')}
    <div data-meldung hidden></div>
    <div class="fenster-fuss">
      <button type="button" class="btn btn-pill btn-pill--fill" style="height:40px" data-fenster-zu>${tr('Abbrechen')}</button>
      <button type="submit" class="btn btn-primary" style="height:40px" data-speichern data-laeuft="${tr('Speichert …')}">${tr('Speichern')}</button>
    </div>
  </form>`;
}
