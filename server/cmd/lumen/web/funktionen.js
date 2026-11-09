// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

/* Die Andockstellen für Handbearbeitung, Sammlungen, Übernahme und
 * Untertitel in app.js — hier gebündelt, damit app.js nur kurze Aufrufe
 * braucht und die eigentliche Arbeit in den eigenen Dateien bleibt. */

import { U, escH, amSchreibtisch } from './fenster.js';
import { bearbeitenOeffnen } from './bearbeiten.js';
import { sammlungKlick, zuSammlung } from './sammlungen.js';
import { uebernahmeOeffnen } from './uebernahme.js';
import { untertitelSuchen, untertitelVerwaltung, utStand, utStandLaden } from './untertitel.js';
import { tr } from './sprache.js';

export { U } from './fenster.js';
export { sammlungenLaden, sammlungLaden, sammlungenSeite, sammlungSeite, reiter as reihenReiter } from './sammlungen.js';
export { utStand, utStandLaden } from './untertitel.js';

const verwalten = () => !!U.S?.me?.account?.isAdmin && amSchreibtisch();
const klein = 'class="btn btn-pill" style="height:30px;font-size:13px"';

/* Titelseite: weitere Knöpfe in der Zeile „Verwaltung:". */
export function detailVerwaltung(it, imMenue = false) {
  if (!verwalten()) return '';
  // Im „Verwaltung"-Menü der Titelseite als Menüpunkte, sonst als Knöpfe.
  const art = imMenue ? 'role="menuitem" tabindex="-1" class="mehr-eintrag"' : klein;
  return `<button ${art} data-fk-bearbeiten="${escH(it.id)}">${tr('Bearbeiten')}</button>
    ${it.kind === 'movie' || it.kind === 'series' ? `<button ${art} data-fk-sammlung="${escH(it.id)}">${tr('Zu Sammlung hinzufügen')}</button>` : ''}`;
}

/* Titelseite: „Untertitel suchen" — für alle, wenn eingerichtet. */
export function detailUntertitel(it, files) {
  const datei = (files || []).find(f => !f.missing);
  if (!datei || it.kind !== 'movie') return '';
  if (utStand.verfuegbar === null) {
    // Noch nicht gefragt: fragen und die Seite danach einmal nachzeichnen.
    utStandLaden().then(s => { if (s.verfuegbar && U.S?.view === 'detail') U.render(); });
    return '';
  }
  if (!utStand.verfuegbar) return '';
  return `<button class="btn btn-pill btn-pill--fill" data-fk-untertitel="${escH(it.id)}" data-datei="${escH(datei.id)}">
    ${U.svg(U.I.untertitel, 15, 1.8)} ${tr('Untertitel suchen')}</button>`;
}

/* Bei Serien: Staffel und Folgen bearbeiten. */
export function staffelBearbeiten(staffelId) {
  return verwalten() && staffelId
    ? `<button ${klein} data-fk-bearbeiten="${escH(staffelId)}" style="height:30px;font-size:13px;margin-left:8px">${tr('Staffel bearbeiten')}</button>` : '';
}
export function folgeBearbeiten(folge) {
  return verwalten()
    ? `<button class="btn btn-round sm" style="width:30px;height:30px;flex:none;background:var(--fill);color:var(--label-2)" data-fk-bearbeiten="${escH(folge.id)}"
        aria-label="${tr('Folge {episodeNo} bearbeiten', { episodeNo: escH(folge.episodeNo) })}" title="${tr('Bearbeiten')}">${U.svg('<path d="M4 20h4L19 9l-4-4L4 16z"/><path d="M14 6l4 4"/>', 14, 1.8)}</button>` : '';
}

/* Ich-Bereich: Umzug von Emby/Jellyfin. */
export function einstellungenUmzug() {
  return `<button class="btn row" style="width:100%;justify-content:flex-start;min-height:52px;gap:12px" data-fk-uebernahme>
      <span style="flex:1;text-align:left"><span class="t-body">${tr('Von Emby oder Jellyfin übernehmen')}</span>
      <span class="t-caption c-2" style="display:block">${tr('Gesehen, Weiterschauen, Favoriten, Bewertungen')}</span></span>
      ${U.svg(U.I.chev, 14, 2.4)}</button>`;
}

/* Verwaltung › Metadaten: OpenSubtitles. */
export function metadatenUntertitel() {
  if (!amSchreibtisch()) return '';
  return `<button type="button" class="btn row" style="width:100%;justify-content:flex-start;min-height:52px" data-fk-ut-verwaltung>
      <span style="flex:1;text-align:left"><span class="t-body">${tr('OpenSubtitles einrichten')}</span>
      <span class="t-caption c-2" style="display:block">${tr('Schlüssel, Konto, fehlende Untertitel von selbst holen')}</span></span>
      ${U.svg(U.I.chev, 14, 2.4)}</button>`;
}

/* Player: Eintrag im Untertitel-Menü. */
export function playerUntertitelEintrag(p) {
  if (!utStand.verfuegbar || (p.kind !== 'movie' && p.kind !== 'episode')) return '';
  return `<button class="spur" data-fk-player-untertitel role="menuitem"><span class="spur-name">${tr('Untertitel suchen …')}</span></button>`;
}

/* Klicks. true = erledigt; app.js macht dann nichts mehr. nachSpuren
   bekommt im Player die neue Spurenliste. */
export function funktionenKlick(e, { nachSpuren } = {}) {
  const hit = sel => e.target.closest(`[${sel}]`);
  let n;
  if ((n = hit('data-fk-bearbeiten'))) {
    bearbeitenOeffnen(n.dataset.fkBearbeiten, () => U.load());
    return true;
  }
  if ((n = hit('data-fk-sammlung'))) {
    zuSammlung(n.dataset.fkSammlung, U.S?.data?.item?.title || '');
    return true;
  }
  if ((n = hit('data-fk-untertitel'))) {
    const it = U.S?.data?.item;
    untertitelSuchen({ itemId: n.dataset.fkUntertitel, fileId: n.dataset.datei, titel: it?.title,
      nachher: () => { U.load(); } });
    return true;
  }
  if (hit('data-fk-player-untertitel')) {
    const p = U.S?.playing;
    if (!p) return true;
    // Das Video hält an, solange gesucht wird.
    try { document.getElementById('v')?.pause(); } catch {}
    untertitelSuchen({ itemId: p.itemId, fileId: p.fileId, titel: [p.title, p.sub].filter(Boolean).join(' · '),
      nachher: (tracks, neu) => nachSpuren?.(tracks, neu) });
    return true;
  }
  if (hit('data-fk-uebernahme')) { uebernahmeOeffnen(); return true; }
  if (hit('data-fk-ut-verwaltung')) { untertitelVerwaltung(); return true; }
  return sammlungKlick(e);
}
