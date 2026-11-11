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
    this.token = t || null;
    if (t) write(TOKEN_KEY, t); else remove(TOKEN_KEY);
    this.mediaKey = null; this.mediaKeyBis = 0;
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
    return this.mediaKey;
  },
  keyParam() { return this.mediaKey ? 'key=' + encodeURIComponent(this.mediaKey) : ''; },

  /* ---------------------------------------------------------- Grundlage */

  async call(path, { method = 'GET', body, raw = false, bestaetigung = '' } = {}) {
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
    try {
      res = await fetch('/api/v1' + path, {
        method, headers,
        body: body === undefined ? undefined : JSON.stringify(body),
      });
    } catch (e) {
      throw new ApiError(tr('Der Server ist nicht erreichbar.'), 0);
    }

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

  async unlock(profileId, secret) {
    const r = await this.call('/unlock', { method: 'POST',
      body: { profileId, secret, deviceName: deviceName(), deviceKind: 'browser' } });
    // Einmaliger Hinweis des Servers (etwa: die Verwaltung hat den PIN
    // zurückgesetzt). app.js zeigt ihn nach dem Anmelden an.
    this.anmeldeHinweis = r.hinweis || null;
    this.setToken(r.token);
    await this.medienSchluessel();
    return r.account;
  },

  // Von unterwegs gibt es keine Profilauswahl: Name und PIN oder Passwort.
  async unlockName(name, secret) {
    const r = await this.call('/unlock', { method: 'POST',
      body: { name, secret, deviceName: deviceName(), deviceKind: 'browser' } });
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
  search(q, limit = 60) {
    return this.call('/search?' + new URLSearchParams({ q, limit }));
  },

  /* ---------------------------------------------------------- mein Stand */

  saveProgress(id, positionMs, durationMs) {
    return this.call(`/items/${encodeURIComponent(id)}/progress`, { method: 'POST',
      body: { positionMs: Math.round(positionMs), durationMs: Math.round(durationMs) } });
  },
  /* Gesehen: bei einer Serie alle Folgen; { staffel: N } nur eine Staffel;
     bei einer Folge { vorherige: true } auch alle davor. */
  markFinished(id, wo = {})  { return this.call(`/items/${encodeURIComponent(id)}/finished${gesehenFrage(wo)}`, { method: 'POST' }); },
  clearFinished(id, wo = {}) { return this.call(`/items/${encodeURIComponent(id)}/finished${gesehenFrage(wo)}`, { method: 'DELETE' }); },
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
  clearHistory()     { return this.call('/me/progress', { method: 'DELETE' }); },
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
  wish(title, year)  { return this.call('/wishes', { method: 'POST', body: { title, year } }); },

  /* ---------------------------------------------------------- Wiedergabe */

  plan(fileId) {
    return this.call(`/stream/${encodeURIComponent(fileId)}/plan?${browserCaps()}`);
  },

  /* Wo ein Strom wirklich beginnen würde. Beim blossen Umpacken ist der
     Einstieg an Schlüsselbilder gebunden — danach richtet sich die Anzeige. */
  seekPoint(fileId, sec, untertitel = -1) {
    const p = new URLSearchParams(browserCaps());
    p.set('t', Math.floor(sec));
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
    if (startSec > 0) p.set('t', Math.floor(startSec));
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
  adminDeleteAccount(id) {
    return this.call(`/admin/accounts/${encodeURIComponent(id)}`, { method: 'DELETE' });
  },
  adminSetLimits(id, limits, libraries, kinderprofil) {
    return this.call(`/admin/accounts/${encodeURIComponent(id)}/limits`, { method: 'PUT', body: { limits, libraries, kinderprofil } });
  },
  /* zusatz: { kinderprofil: true, altersgrenze: 0|6|12|16 } für ein Kind. */
  adminInvite(libraries, limits, days = 7, zusatz = {}) {
    return this.call('/admin/invites', { method: 'POST', body: { libraries, limits, days, ...zusatz } });
  },
  /* PIN oder Passwort eines Haushaltsprofils neu setzen (mit Bestätigung). */
  adminKontoSperre(id, art, neu) {
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
