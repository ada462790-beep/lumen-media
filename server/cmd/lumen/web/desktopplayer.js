// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

/* Der eigene Player von Lumen für Windows (desktop/windows): mpv.
 *
 * In der Windows-App spielt nicht das <video> der Seite, sondern mpv im
 * Prozess der App — die Originaldatei direkt vom Server (MKV, HEVC, 10 Bit,
 * HDR, AC3/DTS/TrueHD, PGS- und ASS-Untertitel), und der Ton gehört dem
 * Programm Lumen (Mischer wie Logi MIXLINE erkennen es). Das Bild liegt in
 * einem Fenster unter der Seite; die Seite wird im Player durchsichtig und
 * zeichnet darüber ihre gewohnten Bedienelemente.
 *
 * Damit app.js dafür kaum etwas wissen muss, bekommt das (leere) <video>
 * des Players die Eigenschaften, die app.js liest und setzt — currentTime,
 * paused, volume, play(), pause() … —, und sie gehen an mpv. Was mpv meldet,
 * kommt als die gewohnten Ereignisse (timeupdate, pause, ended …) zurück.
 * Nur was mpv anders macht als der Browser (Sprung ohne neuen Strom, Ton-
 * und Untertitelwahl ohne Neuladen, kein Einbrennen), fragt app.js über
 * mpvAktiv() ab.
 *
 * Ohne libmpv (Datei fehlt) meldet die App keinen Player — dann bleibt alles
 * beim Browser-Player. Scheitert der Start, schaltet sich mpv für diese
 * Sitzung ab, und der Film läuft im Browser-Player. */

const brueckeDa = () => window.lumenDesktop?.player?.art === 'mpv' && typeof window.lumenDesktop.player.abspielen === 'function';
let abgeschaltet = false;

/* Ob Filme an mpv gehen. */
export const mpvDa = () => !abgeschaltet && brueckeDa();
/* Nach einem gescheiterten Start: für diese Sitzung der Browser-Player. */
export function mpvAbschalten() { abgeschaltet = true; }

/* Was mpv dem Server als abspielbar meldet — für /plan und den Strom. Die
   Liste kommt von der App (mpvFaehigkeiten in desktop/windows/player.go). */
export function mpvFaehigkeiten(qualitaet = null, heim = false) {
  const p = new URLSearchParams(window.lumenDesktop?.player?.faehigkeiten || '');
  if (qualitaet?.maxHeight > 0) p.set('maxHeight', String(qualitaet.maxHeight));
  if (qualitaet?.maxBitrate > 0) p.set('maxBitrate', String(qualitaet.maxBitrate));
  if (heim) p.set('home', '1');
  return p.toString();
}

export const mpvBefehl = (art, wert, mehr) =>
  brueckeDa() ? window.lumenDesktop.player.befehl(art, wert, mehr).catch(() => {}) : Promise.resolve();

/* ---------------------------------------------------------------- Tonausgabe
 *
 * Die Geräte nennt mpv selbst (audio-device-list, WASAPI) — anders als im
 * Browser-Player braucht es dafür keine Erlaubnis. Die Wahl gilt für dieses
 * Gerät, wie die Tonausgabe der Web-App (tonausgabe.js), aber getrennt:
 * Die Kennungen von mpv und Chromium haben nichts miteinander zu tun. */
const TON_SCHLUESSEL = 'lumen.mpvTonausgabe';
export const mpvGeraete = { liste: [], geladen: false };
export function mpvTonausgabe() {
  try {
    const w = JSON.parse(localStorage.getItem(TON_SCHLUESSEL) || 'null');
    return w && typeof w.name === 'string' && w.name ? w : null;
  } catch { return null; }
}
export function mpvTonausgabeWaehlen(name) {
  const g = mpvGeraete.liste.find(x => x.name === name);
  try {
    if (name && name !== 'auto') localStorage.setItem(TON_SCHLUESSEL, JSON.stringify({ name, beschreibung: g?.description || '' }));
    else localStorage.removeItem(TON_SCHLUESSEL);
  } catch {}
  mpvBefehl('tonausgabe', name && name !== 'auto' ? name : '');
  return g || null;
}

/* ---------------------------------------------------------------- Das <video>

   Ein Ereignis von mpv geht an genau einen Player: den, der zuletzt
   angebunden wurde. */
let aktuell = null;

if (brueckeDa()) {
  window.lumenDesktop.player.beiEreignis(e => {
    if (e?.art === 'geraete' && Array.isArray(e.liste)) {
      mpvGeraete.liste = e.liste.filter(g => g && g.name && g.name !== 'auto');
      mpvGeraete.geladen = true;
    }
    if (aktuell) aktuell(e);
  });
}

/* mpvAnbinden macht aus dem <video> v eine Fernbedienung für mpv.
   hooks: { fehler(e), taste(art), geschlossen(), geraete() }. Gibt eine
   Funktion zurück, die die Bindung löst. */
export function mpvAnbinden(v, hooks = {}) {
  const st = { pos: 0, dauer: NaN, paused: true, ended: false, volume: 1, muted: false, rate: 1, src: '', springt: false };
  const feuern = typ => v.dispatchEvent(new Event(typ));
  const def = (name, get, set) => Object.defineProperty(v, name, { configurable: true, get, set });
  def('currentTime', () => st.pos, t => {
    t = Number(t);
    if (!Number.isFinite(t)) return;
    st.pos = Math.max(0, t); st.ended = false;
    mpvBefehl('springen', st.pos);
  });
  def('duration', () => st.dauer);
  def('paused', () => st.paused);
  def('ended', () => st.ended);
  def('seeking', () => st.springt);
  def('volume', () => st.volume, x => {
    x = Math.max(0, Math.min(1, Number(x) || 0));
    if (x === st.volume) return;
    st.volume = x; mpvBefehl('lautstaerke', x); feuern('volumechange');
  });
  def('muted', () => st.muted, x => {
    x = !!x;
    if (x === st.muted) return;
    st.muted = x; mpvBefehl('stumm', x); feuern('volumechange');
  });
  def('playbackRate', () => st.rate, x => {
    x = Number(x) || 1;
    if (x === st.rate) return;
    st.rate = x; mpvBefehl('tempo', x); feuern('ratechange');
  });
  def('defaultPlaybackRate', () => st.rate, () => {});
  // src setzt app.js im mpv-Betrieb nicht; entfernen und load() heisst
  // „diese Wiedergabe ist vorbei" (Fassung, nächste Folge, Schliessen).
  def('src', () => st.src, () => {});
  Object.defineProperty(v, 'play', { configurable: true, value: () => { st.ended = false; mpvBefehl('pause', false); return Promise.resolve(); } });
  Object.defineProperty(v, 'pause', { configurable: true, value: () => { mpvBefehl('pause', true); } });
  Object.defineProperty(v, 'load', { configurable: true, value: () => { if (!st.src) { mpvBefehl('stopp'); } } });
  Object.defineProperty(v, 'removeAttribute', {
    configurable: true,
    value: n => { if (n === 'src') st.src = ''; else HTMLElement.prototype.removeAttribute.call(v, n); },
  });
  Object.defineProperty(v, 'getAttribute', {
    configurable: true,
    value: n => (n === 'src' ? (st.src || null) : HTMLElement.prototype.getAttribute.call(v, n)),
  });
  // Bild im Bild und die Tonausgabe des Browsers gibt es hier nicht.
  Object.defineProperty(v, 'requestPictureInPicture', { configurable: true, value: undefined });
  Object.defineProperty(v, 'setSinkId', { configurable: true, value: undefined });

  const ereignis = e => {
    switch (e?.art) {
      case 'geladen':
        st.src = 'mpv';
        if (Number(e.dauer) > 0) st.dauer = Number(e.dauer);
        st.paused = !!e.pausiert;
        feuern('loadedmetadata'); feuern('canplay');
        if (!st.paused) { feuern('play'); feuern('playing'); }
        break;
      case 'bereit':
        feuern('canplay');
        break;
      case 'zeit':
        st.pos = Number(e.pos) || 0;
        if (Number(e.dauer) > 0) st.dauer = Number(e.dauer);
        feuern('timeupdate');
        break;
      case 'dauer':
        if (Number(e.dauer) > 0) { st.dauer = Number(e.dauer); feuern('durationchange'); }
        break;
      case 'pause':
        if (Number.isFinite(e.pos)) st.pos = e.pos;
        if (!!e.wert === st.paused) break;
        st.paused = !!e.wert;
        if (st.paused) feuern('pause'); else { st.ended = false; feuern('play'); feuern('playing'); }
        break;
      case 'laedt':
        st.springt = !!e.wert;
        feuern(e.wert ? 'waiting' : 'canplay');
        break;
      case 'gesprungen':
        if (Number.isFinite(e.pos)) st.pos = e.pos;
        feuern('seeked');
        break;
      case 'ende':
        if (st.ended) break;
        st.ended = true; st.paused = true;
        if (Number.isFinite(e.pos)) st.pos = e.pos;
        feuern('ended');
        break;
      case 'lautstaerke':
        if (Math.abs((Number(e.wert) || 0) - st.volume) > 0.004) { st.volume = Math.max(0, Math.min(1, Number(e.wert) || 0)); feuern('volumechange'); }
        break;
      case 'stumm':
        if (!!e.wert !== st.muted) { st.muted = !!e.wert; feuern('volumechange'); }
        break;
      case 'tempo':
        if (Number(e.wert) > 0 && Number(e.wert) !== st.rate) { st.rate = Number(e.wert); feuern('ratechange'); }
        break;
      case 'geraete':
        hooks.geraete?.();
        break;
      case 'fehler':
        st.paused = true;
        hooks.fehler?.(e);
        break;
      case 'taste':
        hooks.taste?.(e.taste);
        break;
      case 'geschlossen':
        hooks.geschlossen?.();
        break;
    }
  };
  aktuell = ereignis;
  return {
    zustand: st,
    // Lautstärke und Stummschaltung, wie sie der Auftrag mitbringt.
    vorbelegen(volume, muted, rate) { st.volume = volume; st.muted = muted; st.rate = rate || 1; },
    // Vor einem neuen Laden (Sprung in HLS, andere Qualität): wieder am Anfang.
    neu(ab) { st.src = ''; st.pos = ab || 0; st.ended = false; },
    loesen() { if (aktuell === ereignis) aktuell = null; },
  };
}
