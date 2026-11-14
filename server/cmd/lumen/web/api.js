// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

/* Die Verbindung zum Server.
 *
 * Alles, was die Oberfläche vom Server weiss, geht durch diese Datei.
 * Das Token liegt im Browser dieses Geräts — nicht in einem Cookie, damit
 * es nicht bei jeder Anfrage mitgeschickt wird, und nicht im Verlauf.
 */

import { tr, sprache } from './sprache.js';

const TOKEN_KEY = 'lumen.token';
// Der Bildschlüssel liegt neben dem Token: Mit ihm stehen Bilder nach dem
// Neuladen unter derselben Adresse da, und der Browser nimmt sie aus seinem
// Zwischenspeicher, statt sie unterwegs jedes Mal neu zu laden.
const BILD_KEY = 'lumen.bildschluessel';
// Stände, die noch nicht beim Server sind (Verbindung weg), je Gerät.
const AUSGANG_KEY = 'lumen.ausgang';

// Wie lange eine Anfrage höchstens dauern darf. Unterwegs hängt eine halb
// tote Verbindung sonst endlos (kein Fehler, keine Antwort).
const FRIST_LESEN = 20000;
const FRIST_SCHREIBEN = 60000;

function gesehenFrage(wo) {
  const q = new URLSearchParams();
  if (wo.staffel != null) q.set('staffel', String(wo.staffel));
  if (wo.vorherige) q.set('vorherige', '1');
  const t = q.toString();
  return t ? '?' + t : '';
}

export const api = {
  token: read(TOKEN_KEY),

  get signedIn() { return !!this.token; },

  setToken(t) {
    // Was noch an Ständen aussteht, gehört zum bisherigen Profil: jetzt
    // noch mit dessen Token losschicken, danach nicht mehr.
    if (this.token && this.token !== t) ausgangAbgeben(this.token);
    this.token = t || null;
    if (t) write(TOKEN_KEY, t); else remove(TOKEN_KEY);
    this.mediaKey = null; this.mediaKeyBis = 0;
    this.bildKey = null; remove(BILD_KEY);
  },

  /* Der Medienschlüssel steht in den Adressen von Bildern, Videos und
   * Untertiteln — statt des Tokens, das nur im Kopf mitgeht. Er lebt nur
   * im Arbeitsspeicher und wird rechtzeitig erneuert. */
  mediaKey: null,
  mediaKeyBis: 0,
  async medienSchluessel(erzwingen = false) {
    if (!this.token) return null;
    if (!erzwingen && this.mediaKey && this.mediaKeyBis - Date.now() > 60 * 60 * 1000) return this.mediaKey;
    const r = await this.call('/me/media-key');
    this.mediaKey = r.key;
    this.mediaKeyBis = Date.now() + (r.gueltigSek || 0) * 1000;
    if (r.bildKey) {
      this.bildKey = r.bildKey;
      write(BILD_KEY, JSON.stringify({ k: r.bildKey, bis: Date.now() + (r.bildGueltigSek || 0) * 1000, t: kurz(this.token) }));
    }
    return this.mediaKey;
  },
  keyParam() { return this.mediaKey ? 'key=' + encodeURIComponent(this.mediaKey) : ''; },
  /* Der Bildschlüssel für Poster, Hintergründe, Standbilder, Profil- und
     Personenbilder. Ältere Server kennen ihn nicht: dann der Medienschlüssel. */
  bildKey: null,
  bildParam() {
    const k = this.bildKey || this.mediaKey;
    return k ? 'key=' + encodeURIComponent(k) : '';
  },
  istBild(url) {
    return /^\/api\/v1\/(art|avatar)\//.test(url) || /^\/api\/v1\/persons\/[^?]*\/bild/.test(url);
  },

  /* ---------------------------------------------------------- Grundlage */

  async call(path, { method = 'GET', body, raw = false, bestaetigung = '', signal = null } = {}) {
    const headers = {};
    // Rechte ändern (Gruppen, Einschränkungen) verlangt den Code noch einmal.
    if (bestaetigung) headers['X-Lumen-Bestaetigung'] = bestaetigung;
    if (this.token) headers.Authorization = 'Bearer ' + this.token;
    // Die Verwaltungssperre: nach dem Entsperren gibt Lumen ein Token,
    // das nur für diesen Browser und eine Viertelstunde ohne Verwaltung
    // gilt. sessionStorage, nicht localStorage: mit dem Tab ist es weg.
    if (path.startsWith('/admin/')) {
      try { const t = sessionStorage.getItem('lumen.verwaltung'); if (t) headers['X-Lumen-Admin'] = t; } catch {}
    }
    if (body !== undefined) headers['Content-Type'] = 'application/json';
    // Die Sprache, die die Oberfläche gerade zeigt: Vor der Anmeldung
    // antwortet der Server damit in derselben Sprache (danach gilt die
    // des Profils).
    headers['Accept-Language'] = sprache();

    let res;
    // Lesen darf einmal wiederholt werden, wenn die Verbindung kurz weg
    // war; Schreiben nicht (es käme womöglich doppelt an).
    const lesen = method === 'GET';
    for (let versuch = 0; ; versuch++) {
      try {
        res = await fetch('/api/v1' + path, {
          method, headers,
          body: body === undefined ? undefined : JSON.stringify(body),
          signal: mitAbbruch(frist(lesen ? FRIST_LESEN : FRIST_SCHREIBEN), signal),
        });
        break;
      } catch (e) {
        // Selbst abgebrochen (eine neuere Suche ist unterwegs): kein
        // zweiter Versuch und keine Fehlermeldung.
        if (signal?.aborted) { const a = new ApiError(tr('Abgebrochen.'), 0); a.abgebrochen = true; throw a; }
        if (lesen && versuch === 0 && navigator.onLine !== false) {
          await new Promise(r => setTimeout(r, 1500));
          continue;
        }
        throw new ApiError(navigator.onLine === false
          ? tr('Du bist offline. Sobald die Verbindung wieder da ist, geht es weiter.')
          : tr('Der Server ist nicht erreichbar.'), 0);
      }
    }
    // Die Verbindung steht: liegen gebliebene Stände gleich nachreichen.
    if (ausgangOffen) setTimeout(() => this.standNachreichen(), 0);

    if (res.status === 401) {
      // Ein falscher PIN oder ein falsches Passwort ist auch 401 — aber
      // keine abgelaufene Anmeldung. Der Server sagt es mit code „falsch";
      // dann bleibt das Token, und die Meldung sagt, was nicht stimmt.
      let info = {};
      try { info = await res.clone().json(); } catch {}
      if (info.code === 'falsch') {
        const err = new ApiError(info.error || tr('Das stimmt nicht. Bitte noch einmal.'), 401);
        err.code = 'falsch'; err.art = info.art || ''; err.hilfe = info.hilfe || '';
        throw err;
      }
      // Die Anmeldung gilt nicht mehr — zurück zur Profilauswahl.
      this.setToken(null);
      throw new ApiError(tr('Die Anmeldung ist abgelaufen.'), 401);
    }
    if (res.status === 204) return null;

    // Der Server will eine Bestätigung: nachfragen und noch
    // einmal schicken. Bei „stimmt nicht" erneut fragen, abbrechen geht immer.
    if ((res.status === 428 || res.status === 403) && this.bestaetigen) {
      let info = {};
      try { info = await res.clone().json(); } catch {}
      if (info.code === 'bestaetigen') {
        const code = await this.bestaetigen(res.status === 403 ? tr('Das stimmt nicht. Noch einmal:') : '');
        if (!code) { const e = new ApiError(tr('Abgebrochen.'), 0); e.abgebrochen = true; throw e; }
        return this.call(path, { method, body, raw, bestaetigung: code });
      }
    }

    if (!res.ok) {
      let msg = tr('Das hat nicht geklappt.'), body = {};
      try { body = await res.json(); msg = body.error || msg; } catch {}
      const err = new ApiError(msg, res.status);
      // Manche Antworten tragen mehr als den Text — etwa, wie viele
      // Wiedergaben gerade laufen, damit die Oberfläche nachfragen kann.
      if (body && typeof body.wiedergaben === 'number') err.wiedergaben = body.wiedergaben;
      if (res.status === 423 && body && body.sperre) err.sperre = body.sperre;
      if (body && body.code) err.code = body.code;
      // Nach einem Zurücksetzen: womit das Profil geschützt wird (pin/password).
      if (body && typeof body.art === 'string' && !err.art) err.art = body.art;
      throw err;
    }
    if (raw) return res;
    const text = await res.text();
    return text ? JSON.parse(text) : null;
  },

  /* ---------------------------------------------------------- vor der Anmeldung */

  hello()    { return this.call('/hello'); },
  profiles() { return this.call('/profiles'); },

  async setup(name, password, deviceName, sprache = '') {
    const r = await this.call('/setup', { method: 'POST',
      body: { name, password, deviceName, deviceKind: 'browser', sprache } });
    this.setToken(r.token);
    await this.medienSchluessel();
    return r.account;
  },

  /* neu: { neu, neuArt } – nach einem Zurücksetzen durch die Verwaltung
     legt die Person hier gleich ihren eigenen Schutz fest (Fehlercode
     „neue_pin_noetig" ohne). */
  async unlock(profileId, secret, neu = {}) {
    const r = await this.call('/unlock', { method: 'POST',
      body: { profileId, secret, ...neu, deviceName: deviceName(), deviceKind: 'browser' } });
    // Einmaliger Hinweis des Servers (etwa: die Verwaltung hat den PIN
    // zurückgesetzt). app.js zeigt ihn nach dem Anmelden an.
    this.anmeldeHinweis = r.hinweis || null;
    this.setToken(r.token);
    await this.medienSchluessel();
    return r.account;
  },

  // Von unterwegs gibt es keine Profilauswahl: Name und PIN oder Passwort.
  async unlockName(name, secret, neu = {}) {
    const r = await this.call('/unlock', { method: 'POST',
      body: { name, secret, ...neu, deviceName: deviceName(), deviceKind: 'browser' } });
    // Einmaliger Hinweis des Servers (etwa: die Verwaltung hat den PIN
    // zurückgesetzt). app.js zeigt ihn nach dem Anmelden an.
    this.anmeldeHinweis = r.hinweis || null;
    this.setToken(r.token);
    await this.medienSchluessel();
    return r.account;
  },

  async redeemInvite(code, name, lockKind, secret) {
    const r = await this.call('/invite/redeem', { method: 'POST',
      body: { code, name, lockKind, secret, deviceName: deviceName(), deviceKind: 'browser' } });
    this.setToken(r.token);
    await this.medienSchluessel();
    return r.account;
  },

  signOut() { this.setToken(null); },

  /* ---------------------------------------------------------- Mediathek */

  me()       { return this.call('/me'); },
  home()     { return this.call('/home'); },
  item(id)   { return this.call('/items/' + encodeURIComponent(id)); },
  children(id) { return this.call('/items/' + encodeURIComponent(id) + '/children'); },

  items({ kind, q, genre, person, sort, library, limit = 120, offset = 0 } = {}) {
    const p = new URLSearchParams();
    if (kind) p.set('kind', kind);
    if (q) p.set('q', q);
    if (genre) p.set('genre', genre);
    if (person) p.set('person', person);
    if (sort) p.set('sort', sort);
    if (library) p.set('library', library);
    p.set('limit', limit); p.set('offset', offset);
    return this.call('/items?' + p);
  },
  /* Eine Seite eines Rasters: Titel, Gesamtzahl, ob es weitergeht, und
     die Genres für die Filterknöpfe. */
  seite({ kind, genre, sort = 'title', dir, seed, buchstabe, liste, limit = 60, offset = 0 } = {}) {
    const p = new URLSearchParams({ seite: '1', limit, offset });
    if (kind) p.set('kind', kind);
    if (genre) p.set('genre', genre);
    if (sort) p.set('sort', sort);
    // Richtung (asc|desc) und, bei „Zufällig", der Samen der ersten Seite.
    if (dir) p.set('dir', dir);
    if (seed) p.set('seed', seed);
    if (buchstabe) p.set('buchstabe', buchstabe);
    if (liste) p.set('liste', liste);
    return this.call('/items?' + p);
  },
  buchstaben(kind, genre) {
    const p = new URLSearchParams({ kind });
    if (genre) p.set('genre', genre);
    return this.call('/items/letters?' + p);
  },
  addFavorite(id)    { return this.call('/favorites/' + encodeURIComponent(id), { method: 'PUT' }); },
  removeFavorite(id) { return this.call('/favorites/' + encodeURIComponent(id), { method: 'DELETE' }); },

  /* Die Suche: Titel, Personen, Genres — und wie die Eingabe gedeutet wurde. */
  search(q, limit = 60, signal = null) {
    return this.call('/search?' + new URLSearchParams({ q, limit }), { signal });
  },

  /* ---------------------------------------------------------- mein Stand */

  /* Direkt gemeldet und angekommen: Was für diesen Titel noch im Ausgang
     lag, ist damit überholt. */
  async saveProgress(id, positionMs, durationMs) {
    const r = await this.call(`/items/${encodeURIComponent(id)}/progress`, { method: 'POST',
      body: { positionMs: Math.round(positionMs), durationMs: Math.round(durationMs) } });
    this.ausgangVergessen(id);
    return r;
  },

  /* Den Stand melden, ohne dass er verlorengeht.
   *
   * Unterwegs bricht die Verbindung oft weg, während der Film aus dem
   * Puffer weiterläuft. Früher ging jeder Stand, der in dieser Zeit nicht
   * ankam, verloren — beim nächsten Mal begann der Film zu früh. Jetzt
   * liegt der jeweils letzte Stand je Titel im Browser (localStorage, je
   * Profil), bis der Server ihn hat, und wird nachgereicht: mit wachsendem
   * Abstand, sobald das Netz zurück ist, beim Wiederkommen in den Tab und
   * beim nächsten Start. fertig: als gesehen melden statt der Stelle. */
  async standMelden(itemId, positionMs, durationMs, fertig = false) {
    this.standVormerken(itemId, positionMs, durationMs, fertig);
    await this.standNachreichen();
  },
  standVormerken(itemId, positionMs, durationMs, fertig = false) {
    if (!this.token || !itemId) return;
    ausgangSetzen(this.token, itemId, { p: Math.round(positionMs), d: Math.round(durationMs), f: !!fertig, at: Date.now() });
  },

  /* Schickt, was ansteht. keepalive: auch beim Schliessen des Tabs. */
  async standNachreichen({ keepalive = false } = {}) {
    if (!this.token) return;
    const tok = this.token;
    const offen = ausgangLesen(tok);
    if (!Object.keys(offen).length) { ausgangOffen = false; return; }
    if (keepalive) {
      // Die Seite geht gleich weg: alles sofort, ohne auf Antworten zu
      // warten. Was ankommt, räumt sich selbst weg.
      for (const [id, e] of Object.entries(offen)) {
        standSenden(tok, id, e, true).then(r => { if (r !== 'nochmal') ausgangEntfernen(tok, id, e.at); });
      }
      return;
    }
    if (ausgangLaeuft) { ausgangNochmal = true; return; }
    ausgangLaeuft = true;
    ausgangNochmal = false;
    let fehler = false;
    try {
      for (const [id, e] of Object.entries(offen)) {
        const r = await standSenden(tok, id, e, false);
        if (r === 'nochmal') { fehler = true; break; }
        // Angekommen (oder vom Server endgültig abgelehnt): weg damit —
        // ausser es kam inzwischen ein neuerer Stand dazu.
        ausgangEntfernen(tok, id, e.at);
      }
    } finally {
      ausgangLaeuft = false;
    }
    if (fehler) { ausgangSpaeter(() => this.standNachreichen()); return; }
    ausgangPause = 0;
    clearTimeout(ausgangUhr);
    // Während des Schickens kam ein neuerer Stand dazu: gleich hinterher.
    if (ausgangNochmal || Object.keys(ausgangLesen(tok)).length) {
      if (this.token === tok && ausgangNochmal) setTimeout(() => this.standNachreichen(), 0);
      ausgangOffen = true;
    } else {
      ausgangOffen = false;
    }
  },
  /* Gesehen: bei einer Serie alle Folgen; { staffel: N } nur eine Staffel;
     bei einer Folge { vorherige: true } auch alle davor. */
  /* Haken von Hand: Ein Stand im Ausgang für denselben Titel ist danach
     veraltet und fiele sonst später über die Entscheidung her. */
  async markFinished(id, wo = {})  {
    const r = await this.call(`/items/${encodeURIComponent(id)}/finished${gesehenFrage(wo)}`, { method: 'POST' });
    this.ausgangVergessen(id);
    return r;
  },
  async clearFinished(id, wo = {}) {
    const r = await this.call(`/items/${encodeURIComponent(id)}/finished${gesehenFrage(wo)}`, { method: 'DELETE' });
    this.ausgangVergessen(id);
    return r;
  },
  /* Ausgang leeren: für einen Titel (id) oder ganz (ohne). */
  ausgangVergessen(id) {
    if (!this.token) return;
    if (id === undefined) { ausgangSchreiben(this.token, {}); return; }
    ausgangEntfernen(this.token, id);
    bekannteStaende.delete(kurz(this.token) + ':' + id);
  },
  /* Welche Folge „Weiterschauen" auf einer Serienseite startet. */
  weiterInSerie(id) { return this.call(`/items/${encodeURIComponent(id)}/weiterschauen`); },
  /* Vorschaubilder für die Zeitleiste. Antwortet mit dem Aufbau, mit
     { code: 'wird_erstellt' } (202), oder 404, wenn es keine gibt. */
  trickplay(fileId) { return this.call(`/stream/${encodeURIComponent(fileId)}/trickplay`); },
  rate(id, stars, shared = false) {
    return this.call(`/items/${encodeURIComponent(id)}/rating`, { method: 'PUT', body: { stars, shared } });
  },
  addToList(id)      { return this.call('/watchlist/' + encodeURIComponent(id), { method: 'PUT' }); },
  removeFromList(id) { return this.call('/watchlist/' + encodeURIComponent(id), { method: 'DELETE' }); },
  /* Verlauf löschen: auch was noch im Ausgang wartet — sonst käme es
     nachgereicht gleich wieder. */
  async clearHistory() {
    this.ausgangVergessen();
    const r = await this.call('/me/progress', { method: 'DELETE' });
    this.ausgangVergessen();
    bekannteStaende.clear();
    return r;
  },
  updateMe(patch)    { return this.call('/me', { method: 'PATCH', body: patch }); },
  /* Eigenes Profilbild hochladen — roh, der Server verkleinert. */
  async uploadAvatar(file) {
    const res = await fetch('/api/v1/me/avatar', { method: 'POST', headers: { Authorization: 'Bearer ' + this.token, 'Content-Type': file.type || 'application/octet-stream' }, body: file });
    if (!res.ok) { let msg = tr('Das hat nicht geklappt.'); try { msg = (await res.json()).error || msg; } catch {} throw new ApiError(msg, res.status); }
    return res.json();
  },
  news()      { return this.call('/news'); },
  newsSeen()  { return this.call('/news/seen', { method: 'POST' }); },
  devices()          { return this.call('/me/devices'); },
  myPlaybacks()      { return this.call('/me/playbacks'); },
  stopMyPlayback(id) { return this.call('/me/playbacks/' + encodeURIComponent(id), { method: 'DELETE' }); },
  signOutEverywhere(){ return this.call('/me/sign-out-everywhere', { method: 'POST' }); },

  /* ---------------------------------------------------------- Wünsche */

  wishes()           { return this.call('/wishes'); },
  // mehr: art, tmdbId, imdbId, originaltitel aus einem Vorschlag (wuensche.js).
  wish(title, year, mehr = {}) { return this.call('/wishes', { method: 'POST', body: { title, year, ...mehr } }); },
  wishSuggestions(q) { return this.call('/wishes/vorschlaege?q=' + encodeURIComponent(q)); },

  /* ---------------------------------------------------------- Wiedergabe */

  plan(fileId) {
    return this.call(`/stream/${encodeURIComponent(fileId)}/plan?${browserCaps()}`);
  },

  /* Wo ein Strom wirklich beginnen würde. Beim blossen Umpacken ist der
     Einstieg an Schlüsselbilder gebunden — danach richtet sich die Anzeige. */
  seekPoint(fileId, sec, untertitel = -1) {
    const p = new URLSearchParams(browserCaps());
    p.set('t', sekunden(sec));
    // Ein eingebrannter Bild-Untertitel heisst: neu gerechnet, nicht nur
    // umgepackt — und damit ein anderer Einstieg. Der Server muss es wissen.
    p.set('untertitel', String(untertitel));
    return this.call(`/stream/${encodeURIComponent(fileId)}/seek?${p}`);
  },

  /* Die Adresse des Stroms. Der Browser bekommt ein Format, das er von sich
     aus abspielt — gesprungen wird, indem der Strom an der neuen Stelle neu
     beginnt. Wo er dann wirklich steht, sagt der Server in X-Lumen-Start. */
  streamURL(fileId, startSec = 0, spur = -1, { untertitel = -1, lauf = '' } = {}) {
    const p = new URLSearchParams(browserCaps());
    if (startSec > 0) p.set('t', sekunden(startSec));
    // Die gewählte Tonspur — ohne Angabe nimmt der Server die erste.
    if (spur >= 0) p.set('spur', String(spur));
    // Bild-Untertitel (PGS, DVD) rechnet der Server ins Bild. Immer
    // ausdrücklich: ohne Angabe brennte er erzwungene von selbst ein, und
    // die Spurwahl im Player wüsste nichts davon.
    p.set('untertitel', String(untertitel));
    // Die Kennung dieses Players: Endet der Strom zu früh, fragt die App
    // unter browserGrund nach, warum (das <video> sieht keinen Status).
    if (lauf) p.set('lauf', lauf);
    if (this.mediaKey) p.set('key', this.mediaKey);
    return `/api/v1/stream/${encodeURIComponent(fileId)}/browser.mp4?${p}`;
  },
  /* Warum der Strom dieses Players endete: laeuft, wiedergabe_abgelaufen,
     wiedergabe_beendet, wiedergabe_ersetzt, fehler (mit error), unbekannt. */
  browserGrund(fileId, lauf) {
    return this.call(`/stream/${encodeURIComponent(fileId)}/browser/grund?${new URLSearchParams({ lauf })}`);
  },
  // Herunterladen: ob es erlaubt ist und welche Dateien es gibt.
  downloadInfo(itemId) { return this.call(`/items/${encodeURIComponent(itemId)}/download/info`); },
  downloadURL(fileId) {
    return `/api/v1/stream/${encodeURIComponent(fileId)}/download?${this.keyParam()}`;
  },
  subtitleURL(fileId, idx) {
    return `/api/v1/stream/${encodeURIComponent(fileId)}/subtitles/${encodeURIComponent(idx)}.vtt?${this.keyParam()}`;
  },
  adminSetIntro(seriesId, vonMs, bisMs) {
    return this.call(`/admin/items/${encodeURIComponent(seriesId)}/intro`, { method: 'PUT', body: { vonMs, bisMs } });
  },
  adminClearIntro(seriesId) {
    return this.call(`/admin/items/${encodeURIComponent(seriesId)}/intro`, { method: 'DELETE' });
  },
  /* „Kein Vorspann" an einer Serie oder Folge: auch Kapitel und Erkennung nicht. */
  adminKeinIntro(id) {
    return this.call(`/admin/items/${encodeURIComponent(id)}/intro`, { method: 'PUT', body: { keiner: true } });
  },
  /* Abspann von Hand: Beginn an einer Folge, oder „kein Abspann" (Folge oder Serie). */
  adminSetAbspann(folgeId, abMs) {
    return this.call(`/admin/items/${encodeURIComponent(folgeId)}/abspann`, { method: 'PUT', body: { abMs } });
  },
  adminKeinAbspann(id) {
    return this.call(`/admin/items/${encodeURIComponent(id)}/abspann`, { method: 'PUT', body: { keiner: true } });
  },
  adminClearAbspann(id) {
    return this.call(`/admin/items/${encodeURIComponent(id)}/abspann`, { method: 'DELETE' });
  },

  artURL(itemId, kind = 'poster') {
    if (!this.mediaKey) return '';
    return `/api/v1/art/${encodeURIComponent(itemId)}/${encodeURIComponent(kind)}?${this.keyParam()}`;
  },

  /* ---------------------------------------------------------- Verwaltung */

  adminOverview()  { return this.call('/admin/overview'); },
  adminRemote()    { return this.call('/admin/remote'); },
  adminLibraries() { return this.call('/admin/libraries'); },
  adminAddLibrary(name, kind, path, language = 'de') {
    return this.call('/admin/libraries', { method: 'POST',
      body: { name, kind, path, language } });
  },
  adminScan(id)    { return this.call(`/admin/libraries/${encodeURIComponent(id)}/scan`, { method: 'POST' }); },
  adminSetLibrary(id, patch) {
    return this.call(`/admin/libraries/${encodeURIComponent(id)}`, { method: 'POST', body: patch });
  },
  adminMetadataReload(library = '') {
    return this.call('/admin/metadata/reload' + (library ? '?library=' + encodeURIComponent(library) : ''),
      { method: 'POST' });
  },
  adminItemReload(id) {
    return this.call(`/admin/items/${encodeURIComponent(id)}/metadata/reload`, { method: 'POST' });
  },
  adminItemSearch(id, q, year) {
    const p = new URLSearchParams();
    if (q) p.set('q', q);
    if (year) p.set('year', year);
    return this.call(`/admin/items/${encodeURIComponent(id)}/metadata/search?` + p);
  },
  /* Kennung ohne TMDb-Schlüssel speichern (vormerken). */
  adminItemKennung(id, eingabe) {
    return this.call(`/admin/items/${encodeURIComponent(id)}/metadata/kennung`,
      { method: 'POST', body: { eingabe } });
  },
  adminItemAssign(id, sourceId) {
    return this.call(`/admin/items/${encodeURIComponent(id)}/metadata/assign`,
      { method: 'POST', body: { sourceId } });
  },
  adminDeleteLibrary(id) {
    return this.call(`/admin/libraries/${encodeURIComponent(id)}`, { method: 'DELETE' });
  },
  /* Ordner anklicken statt den Pfad blind eintippen. */
  adminBrowse(pfad = '') {
    return this.call('/admin/browse' + (pfad ? '?pfad=' + encodeURIComponent(pfad) : ''));
  },
  adminWishes()    { return this.call('/admin/wishes'); },

  /* ------------------------------------------------- Verwaltungssperre */
  adminLock()           { return this.call('/admin/lock'); },
  adminUnlock(code)     { return this.call('/admin/unlock', { method: 'POST', body: { code } }); },
  adminRelock()         { return this.call('/admin/relock', { method: 'POST' }); },
  adminLockTOTP()       { return this.call('/admin/lock/totp', { method: 'POST' }); },
  adminSetLock(b)       { return this.call('/admin/lock', { method: 'PUT', body: b }); },
  verwaltungsToken(t) {
    try { if (t) sessionStorage.setItem('lumen.verwaltung', t); else sessionStorage.removeItem('lumen.verwaltung'); } catch {}
  },

  /* ------------------------------------------------------------- Gruppen */
  adminGroups()          { return this.call('/admin/groups'); },
  adminCreateGroup(g)    { return this.call('/admin/groups', { method: 'POST', body: g }); },
  adminUpdateGroup(id, g){ return this.call('/admin/groups/' + encodeURIComponent(id), { method: 'PUT', body: g }); },
  adminDeleteGroup(id)   { return this.call('/admin/groups/' + encodeURIComponent(id), { method: 'DELETE' }); },
  adminSetGroup(acc, groupId) {
    return this.call(`/admin/accounts/${encodeURIComponent(acc)}/group`, { method: 'PUT', body: { groupId } });
  },

  /* ------------------------------------------------------------- Updates */
  adminUpdate()          { return this.call('/admin/update'); },
  adminUpdateCheck()     { return this.call('/admin/update/check', { method: 'POST' }); },
  adminUpdateSettings(e) { return this.call('/admin/update/settings', { method: 'POST', body: e }); },
  adminUpdateRun(trotzdem) {
    return this.call('/admin/update/run' + (trotzdem ? '?trotzdem=1' : ''), { method: 'POST' });
  },
  // Im Container über den Aktualisierer; verlangt den Code (bestaetigt).
  adminUpdateContainer(trotzdem) {
    return this.call('/admin/update/container' + (trotzdem ? '?trotzdem=1' : ''), { method: 'POST' });
  },

  /* ---------------------------------------------------------- Filmreihen */
  collections()         { return this.call('/collections'); },
  collection(id)        { return this.call('/collections/' + encodeURIComponent(id)); },
  adminCollections()    { return this.call('/admin/collections'); },
  adminSetCollection(id, patch) {
    return this.call('/admin/collections/' + encodeURIComponent(id), { method: 'POST', body: patch });
  },

  /* ---------------------------------------------------------- Listen */
  playlists(itemId = '') {
    return this.call('/playlists' + (itemId ? '?item=' + encodeURIComponent(itemId) : ''));
  },
  playlist(id)          { return this.call('/playlists/' + encodeURIComponent(id)); },
  createPlaylist(name)  { return this.call('/playlists', { method: 'POST', body: { name } }); },
  updatePlaylist(id, patch) {
    return this.call('/playlists/' + encodeURIComponent(id), { method: 'POST', body: patch });
  },
  deletePlaylist(id) {
    return this.call('/playlists/' + encodeURIComponent(id), { method: 'DELETE' });
  },
  addToPlaylist(id, itemId) {
    return this.call(`/playlists/${encodeURIComponent(id)}/items`, { method: 'POST', body: { itemId } });
  },
  removeFromPlaylist(id, itemId) {
    return this.call(`/playlists/${encodeURIComponent(id)}/items/${encodeURIComponent(itemId)}`,
      { method: 'DELETE' });
  },
  orderPlaylist(id, itemIds) {
    return this.call(`/playlists/${encodeURIComponent(id)}/order`, { method: 'POST', body: { itemIds } });
  },
  adminSessions()  { return this.call('/admin/sessions'); },
  adminBackups()   { return this.call('/admin/backups'); },
  // Umwandeln: Hardware-Beschleunigung, Vorschaubilder, Untertitel.
  adminUmwandlung() { return this.call('/admin/umwandlung'); },
  adminUmwandlungSetzen(e) { return this.call('/admin/umwandlung', { method: 'PUT', body: e }); },
  adminUmwandlungPruefen() { return this.call('/admin/umwandlung/pruefen', { method: 'POST' }); },
  adminBackupNow() { return this.call('/admin/backups', { method: 'POST' }); },
  adminRestore(nr) { return this.call('/admin/backups/' + encodeURIComponent(nr) + '/restore', { method: 'POST' }); },
  adminStopSession(id) { return this.call('/admin/sessions/' + encodeURIComponent(id), { method: 'DELETE' }); },
  adminLog(level)  { return this.call('/admin/log?limit=200' + (level && level !== 'all' ? '&level=' + level : '')); },
  adminCleanup()   { return this.call('/admin/cleanup'); },
  adminAccounts()  { return this.call('/admin/accounts'); },
  /* Bildschirmzeit (bildschirmzeit.js): der eigene Stand, und für die
     Verwaltung Regel und „heute einmalig mehr". */
  meineBildschirmzeit() { return this.call('/me/bildschirmzeit'); },
  adminBildschirmzeit(id, regel) {
    return this.call(`/admin/accounts/${encodeURIComponent(id)}/bildschirmzeit`, { method: 'PUT', body: regel });
  },
  adminBildschirmzeitGeschenk(id, minuten) {
    return this.call(`/admin/accounts/${encodeURIComponent(id)}/bildschirmzeit/geschenk`, { method: 'POST', body: { minuten } });
  },
  /* Nimmt zurück, was heute dazugegeben wurde. */
  adminBildschirmzeitGeschenkZurueck(id) {
    return this.call(`/admin/accounts/${encodeURIComponent(id)}/bildschirmzeit/geschenk`, { method: 'DELETE' });
  },
  adminDeleteAccount(id) {
    return this.call(`/admin/accounts/${encodeURIComponent(id)}`, { method: 'DELETE' });
  },
  /* gast: true/false – gehört nicht zum Haushalt (undefined: unverändert). */
  adminSetLimits(id, limits, libraries, kinderprofil, gast) {
    return this.call(`/admin/accounts/${encodeURIComponent(id)}/limits`, { method: 'PUT', body: { limits, libraries, kinderprofil, gast } });
  },
  /* zusatz: { kinderprofil: true, altersgrenze: 0|6|12|16 } für ein Kind. */
  adminInvite(libraries, limits, days = 7, zusatz = {}) {
    return this.call('/admin/invites', { method: 'POST', body: { libraries, limits, days, ...zusatz } });
  },
  /* Offene Einladungen (ohne Code) und Zurückziehen. */
  adminInvites() { return this.call('/admin/invites'); },
  adminInviteZurueckziehen(kennung) { return this.call('/admin/invites/' + encodeURIComponent(kennung), { method: 'DELETE' }); },
  /* Schutz eines Haushaltsprofils zurücksetzen (mit Bestätigung). Ohne
     „neu" vergibt der Server einen Einmal-Code → { einmalCode, art }. */
  adminKontoSperre(id, art, neu = '') {
    return this.call(`/admin/accounts/${encodeURIComponent(id)}/sperre`, { method: 'PUT', body: { art, neu } });
  },
  /* Das eigene Profil schützen: mit dem bisherigen Code. */
  meineSperre(alt, art, neu) {
    return this.call('/me/sperre', { method: 'PUT', body: { alt, art, neu } });
  },
  adminWishState(id, state, note = '') {
    return this.call('/admin/wishes/' + encodeURIComponent(id), { method: 'PUT', body: { state, note } });
  },
  adminMetadata()  { return this.call('/admin/metadata'); },
  adminSetMetadata(patch) { return this.call('/admin/metadata', { method: 'PUT', body: patch }); },
  adminResolve(library) {
    return this.call('/admin/metadata/resolve' + (library ? '?library=' + library : ''),
      { method: 'POST' });
  },
};

/* canPlayAnything sagt, ob dieser Browser überhaupt Video abspielen kann.
   Ohne das sähe der Nutzer nur ein schwarzes Bild und wüsste nicht, warum. */
export function canPlayAnything() {
  const v = document.createElement('video');
  return v.canPlayType('video/mp4; codecs="avc1.42E01E"') !== '' ||
         v.canPlayType('video/webm; codecs="vp9"') !== '' ||
         v.canPlayType('video/webm; codecs="vp8"') !== '';
}

// Der gemerkte Bildschlüssel gilt nur für dasselbe Token und nicht bis
// zur letzten Minute (dann holt medienSchluessel() ohnehin einen neuen).
try {
  const b = JSON.parse(read(BILD_KEY) || 'null');
  if (b && b.k && b.t === kurz(api.token) && b.bis - Date.now() > 3600 * 1000) api.bildKey = b.k;
} catch {}

// Liegen gebliebene Stände nachreichen: sobald das Netz zurück ist, beim
// Zurückkommen in den Tab, und kurz nach dem Start.
addEventListener('online', () => api.standNachreichen());
document.addEventListener('visibilitychange', () => {
  if (document.visibilityState === 'visible') api.standNachreichen();
});
setTimeout(() => api.standNachreichen(), 3000);

export class ApiError extends Error {
  constructor(message, status) { super(message); this.status = status; }
}

/* ------------------------------------------------------------- Gerät */

/* Was dieser Browser kann. Bewusst knapp gehalten: lieber einmal mehr
   umrechnen lassen als ein schwarzes Bild zeigen. */
function browserCaps() {
  const v = document.createElement('video');
  const can = t => v.canPlayType(t) !== '';

  // Gefragt, nicht angenommen. Nicht jeder Browser bringt H.264 und AAC
  // mit — manche Linux-Ausgaben von Chromium und Firefox lassen die
  // lizenzpflichtigen Decoder weg. Behauptet die App sie trotzdem, schickt
  // der Server etwas, das nur als schwarzes Bild ankommt.
  const codecs = [];
  if (can('video/mp4; codecs="avc1.42E01E"')) codecs.push('h264');
  if (can('video/mp4; codecs="hvc1.1.6.L93.B0"')) codecs.push('hevc');
  if (can('video/mp4; codecs="av01.0.05M.08"')) codecs.push('av1');
  if (can('video/webm; codecs="vp9"')) codecs.push('vp9');
  if (can('video/webm; codecs="vp8"')) codecs.push('vp8');

  const containers = [];
  if (can('video/mp4; codecs="avc1.42E01E"') || can('audio/mp4; codecs="mp4a.40.2"')) containers.push('mp4');
  if (can('video/webm; codecs="vp9"') || can('video/webm; codecs="vp8"')) containers.push('webm');

  const audio = [];
  if (can('audio/mp4; codecs="mp4a.40.2"')) audio.push('aac');
  if (can('audio/webm; codecs="opus"')) audio.push('opus');
  if (can('audio/mpeg')) audio.push('mp3');

  const p = new URLSearchParams();
  p.set('codecs', codecs.join(','));
  p.set('containers', containers.join(','));
  p.set('audio', audio.join(','));
  // Mehr als der Bildschirm hergibt, bringt nichts — spart Leitung und
  // erspart dem Server unnötige Arbeit.
  p.set('maxHeight', String(Math.min(2160, Math.round(screen.height * (devicePixelRatio || 1)))));
  if (isHomeNetwork()) p.set('home', '1');
  return p.toString();
}

/* Im Heimnetz bremst nichts. Erkannt am Namen oder an der Adresse — das ist
   eine Vermutung, keine Garantie, und sie schadet höchstens der Qualität. */
function isHomeNetwork() {
  const h = location.hostname;
  return h === 'localhost' || h.endsWith('.local') || h.endsWith('.lan') ||
    /^10\./.test(h) || /^192\.168\./.test(h) ||
    /^172\.(1[6-9]|2\d|3[01])\./.test(h);
}

function deviceName() {
  const ua = navigator.userAgent;
  const browser = /Edg\//.test(ua) ? 'Edge' : /Chrome\//.test(ua) ? 'Chrome'
    : /Safari\//.test(ua) ? 'Safari' : /Firefox\//.test(ua) ? 'Firefox' : tr('Browser');
  const os = /Macintosh/.test(ua) ? 'Mac' : /Windows/.test(ua) ? 'Windows'
    : /iPhone/.test(ua) ? 'iPhone' : /iPad/.test(ua) ? 'iPad'
    : /Android/.test(ua) ? 'Android' : /Linux/.test(ua) ? 'Linux' : '';
  return os ? `${browser} · ${os}` : browser;
}

/* Browser-Speicher kann in privaten Fenstern ganz fehlen oder beim Zugriff
   eine Ausnahme werfen — darum alles einpacken. */
function read(k)   { try { return localStorage.getItem(k); } catch { return null; } }
function write(k, v) { try { localStorage.setItem(k, v); } catch {} }
function remove(k) { try { localStorage.removeItem(k); } catch {} }

// Eine Stelle im Film für die Adresse: auf Millisekunden, ohne Exponent.
// (Ganze Sekunden setzten einen Ton- oder Untertitelwechsel bis zu einer
// Sekunde zu früh an.)
function sekunden(s) {
  return String(Math.max(0, Math.round((Number(s) || 0) * 1000) / 1000));
}

/* ---------------------------------------------------------- Netz

   frist: ein Signal, das nach ms abbricht. AbortSignal.timeout gibt es
   in älteren Safari nicht. */
/* Die Frist und ein eigener Abbruch (AbortController) zusammen: was
   zuerst kommt, beendet die Anfrage. */
function mitAbbruch(fristSignal, eigenes) {
  if (!eigenes) return fristSignal;
  try { if (AbortSignal.any) return AbortSignal.any([fristSignal, eigenes]); } catch {}
  const c = new AbortController();
  const ab = () => c.abort();
  if (fristSignal.aborted || eigenes.aborted) c.abort();
  fristSignal.addEventListener('abort', ab, { once: true });
  eigenes.addEventListener('abort', ab, { once: true });
  return c.signal;
}

function frist(ms) {
  try { if (AbortSignal.timeout) return AbortSignal.timeout(ms); } catch {}
  const c = new AbortController();
  setTimeout(() => c.abort(), ms);
  return c.signal;
}

/* ---------------------------------------------------------- Stand-Ausgang

   { <kurz(token)>: { <itemId>: { p, d, f, at, b } } } — je Profil getrennt
   (ein Profil schickt nie die Stände eines anderen). Nur der letzte Stand
   je Titel; was älter als sechs Stunden ist, verfällt — ein Stand von vor
   Tagen überschriebe sonst, was inzwischen auf einem anderen Gerät
   geschaut wurde.

   b: der Marker des Servers für diesen Titel, wie dieses Gerät ihn zuletzt
   bekam (Kopf X-Lumen-Stand, store/fortschritt_stand.go). Wird ein Stand
   später nachgereicht, geht b als „basis" mit: Hat seither ein anderes
   Gerät weitergeschaut oder jemand den Verlauf gelöscht, passt der Marker
   nicht mehr, und der Server verwirft den alten Stand. at ist nur die Uhr
   dieses Browsers; der Server bekommt und speichert keine Uhrzeit. */
const AUSGANG_HOECHSTENS = 6 * 3600 * 1000;
// Ein Stand, der so jung ist, geht als gewöhnliche Meldung (ohne basis):
// Er ist der neueste, den es gibt.
const AUSGANG_FRISCH = 30 * 1000;
// Die zuletzt bekannten Marker je Profil und Titel, solange die Seite offen ist.
const bekannteStaende = new Map();
let ausgangLaeuft = false;
let ausgangNochmal = false;
let ausgangOffen = true; // beim Start einmal nachsehen
let ausgangPause = 0;
let ausgangUhr = 0;

function kurz(token) {
  // FNV-1a: nur ein Unterscheidungsmerkmal, kein Geheimnis.
  let h = 0x811c9dc5;
  for (let i = 0; i < (token || '').length; i++) { h ^= token.charCodeAt(i); h = Math.imul(h, 0x01000193); }
  return (h >>> 0).toString(36);
}
function ausgangAlles() {
  try { return JSON.parse(read(AUSGANG_KEY) || '{}') || {}; } catch { return {}; }
}
function ausgangLesen(token) {
  const alles = ausgangAlles();
  const meins = alles[kurz(token)] || {};
  const jetzt = Date.now();
  for (const [id, e] of Object.entries(meins)) if (!e || jetzt - e.at > AUSGANG_HOECHSTENS) delete meins[id];
  return meins;
}
function ausgangSchreiben(token, meins) {
  const alles = ausgangAlles();
  if (Object.keys(meins).length) alles[kurz(token)] = meins; else delete alles[kurz(token)];
  if (Object.keys(alles).length) write(AUSGANG_KEY, JSON.stringify(alles)); else remove(AUSGANG_KEY);
}
function ausgangSetzen(token, id, e) {
  const meins = ausgangLesen(token);
  const b = bekannteStaende.get(kurz(token) + ':' + id) ?? meins[id]?.b;
  if (b !== undefined) e.b = b;
  meins[id] = e;
  ausgangSchreiben(token, meins);
  ausgangOffen = true;
}
function ausgangEntfernen(token, id, at) {
  const meins = ausgangLesen(token);
  if (meins[id] && (at === undefined || meins[id].at === at)) { delete meins[id]; ausgangSchreiben(token, meins); }
}
function ausgangSpaeter(f) {
  clearTimeout(ausgangUhr);
  // 5 s, 10 s, 20 s … höchstens alle fünf Minuten.
  ausgangPause = Math.min(300000, ausgangPause ? ausgangPause * 2 : 5000);
  ausgangUhr = setTimeout(f, ausgangPause);
}
// Ein Profil wird verlassen: letzter Versuch mit seinem Token, dann weg.
function ausgangAbgeben(token) {
  const meins = ausgangLesen(token);
  for (const [id, e] of Object.entries(meins)) standSenden(token, id, e, true);
  ausgangSchreiben(token, {});
}
/* 'ok' | 'weg' (endgültig abgelehnt, etwa Titel gelöscht) | 'nochmal'.
   Ein Stand, der nicht mehr frisch ist, geht mit seiner basis — passt sie
   beim Server nicht mehr, verwirft er ihn (X-Lumen-Veraltet), und er gilt
   hier als erledigt. */
async function standSenden(token, id, e, keepalive) {
  const spaet = e.b !== undefined && Date.now() - e.at > AUSGANG_FRISCH;
  const basis = spaet ? (e.f ? `?basis=${encodeURIComponent(e.b)}` : '') : '';
  const rumpf = { positionMs: e.p, durationMs: e.d };
  if (spaet) rumpf.basis = e.b;
  try {
    const res = await fetch(`/api/v1/items/${encodeURIComponent(id)}/${e.f ? 'finished' + basis : 'progress'}`, {
      method: 'POST', keepalive,
      body: e.f ? undefined : JSON.stringify(rumpf),
      headers: { 'Content-Type': 'application/json', Authorization: 'Bearer ' + token },
      signal: keepalive ? undefined : frist(FRIST_SCHREIBEN),
    });
    if (res.ok) {
      const stand = res.headers.get('X-Lumen-Stand');
      if (stand !== null) bekannteStaende.set(kurz(token) + ':' + id, stand);
      return 'ok';
    }
    // Server überlastet oder kurz weg (Proxy): später nochmal.
    if (res.status >= 500 || res.status === 408 || res.status === 429) return 'nochmal';
    return 'weg';
  } catch {
    // keepalive: der Browser schickt es noch, wenn er kann — ob es ankam,
    // erfährt niemand. Liegen lassen; der nächste Start reicht es nach.
    return 'nochmal';
  }
}
