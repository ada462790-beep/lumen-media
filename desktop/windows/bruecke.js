// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

// Die Brücke zwischen Seite und App. WebView2 legt dieses Skript in jede
// Seite, bevor deren eigene Skripte laufen — in die eingebaute Seite und
// in die Web-App des gewählten Servers. Fremde Seiten kommen gar nicht
// ins Fenster (App.Wohin), also auch nicht an die Brücke.
//
// Die Web-App erkennt die Hülle an window.lumenDesktop (und am Zusatz
// „LumenDesktop/<Fassung>" im User-Agent) und zeigt dann „Server
// wechseln". Mehr braucht sie nicht zu wissen.

(() => {
  'use strict';
  const wv = window.chrome && window.chrome.webview;
  if (!wv || window.lumenDesktop || window.top !== window) return;

  let nr = 0;
  const offen = new Map();
  // Antworten kommen über ExecuteScript, nicht über postMessage: WebView2
  // schickt jede Nachricht als Echo zurück, das wäre nicht zu unterscheiden.
  Object.defineProperty(window, '__lumenAntwort', {
    value: a => {
      const p = offen.get(a && a.id);
      if (!p) return;
      offen.delete(a.id);
      if (a.ok) p.ja(a.daten);
      else p.nein(Object.assign(new Error(a.fehler || 'unbekannt'), { code: a.fehler || 'unbekannt' }));
    },
  });
  const frage = (art, daten) => new Promise((ja, nein) => {
    const id = ++nr;
    offen.set(id, { ja, nein });
    wv.postMessage(JSON.stringify({ id, art, daten: daten || {} }));
  });

  // ---------------------------------------------------------- Update-Hinweis
  //
  // Eine kleine Pille unten rechts, nur wenn es eine neuere Fassung gibt.
  // Im Schatten-DOM, damit sich Seite und Pille nicht in die Quere kommen.
  const DE = (navigator.language || 'de').toLowerCase().startsWith('de');
  const T = DE ? {
    da: 'Lumen {v} ist da.', laden: 'Laden', laedt: 'Lumen {v} wird geladen …',
    bereit: 'Lumen {v} ist bereit.', installieren: 'Installieren', fehler: 'Das Laden hat nicht geklappt.',
    erneut: 'Erneut', seite: 'Auf GitHub', spaeter: 'Später',
  } : {
    da: 'Lumen {v} is available.', laden: 'Download', laedt: 'Downloading Lumen {v} …',
    bereit: 'Lumen {v} is ready.', installieren: 'Install', fehler: 'The download didn’t work.',
    erneut: 'Retry', seite: 'On GitHub', spaeter: 'Later',
  };
  let pille = null;
  let stand = null;

  function zeigeUpdate(s) {
    stand = s;
    if (!document.documentElement) return;
    if (!s || !s.verfuegbar) { if (pille) { pille.remove(); pille = null; } return; }
    if (!pille) {
      pille = document.createElement('lumen-desktop-update');
      const wurzel = pille.attachShadow({ mode: 'closed' });
      pille._wurzel = wurzel;
      document.documentElement.appendChild(pille);
    }
    const w = pille._wurzel;
    const v = String(s.version || '');
    const satz = (s.phase === 'laedt' ? T.laedt : s.phase === 'bereit' ? T.bereit : s.phase === 'fehler' ? T.fehler : T.da).replace('{v}', v);
    w.innerHTML = `<style>
      :host { all:initial; position:fixed; right:20px; bottom:20px; z-index:2147483647; }
      .p { display:flex; align-items:center; gap:10px; padding:8px 8px 8px 16px; border-radius:999px;
           font:500 13px/1.2 -apple-system,"Segoe UI Variable Text","Segoe UI",system-ui,sans-serif; letter-spacing:-.01em;
           color:rgba(255,255,255,.94); background:rgba(30,30,34,.82);
           -webkit-backdrop-filter:blur(30px) saturate(160%); backdrop-filter:blur(30px) saturate(160%);
           box-shadow:inset 0 1px 0 rgba(255,255,255,.12), inset 0 0 0 .5px rgba(255,255,255,.08), 0 10px 30px rgba(0,0,0,.4) }
      button { all:unset; cursor:pointer; height:28px; padding:0 12px; border-radius:999px; font-weight:600; font-size:12px;
               background:rgba(255,255,255,.12); color:inherit; display:inline-flex; align-items:center }
      button.haupt { background:#fff; color:#000 }
      button:focus-visible { outline:2px solid #fff; outline-offset:2px }
      button.zu { width:28px; padding:0; justify-content:center; background:transparent; color:rgba(235,235,245,.6) }
      button.zu:hover { background:rgba(255,255,255,.1) }
      .spin { width:12px; height:12px; border-radius:50%; border:2px solid rgba(255,255,255,.15); border-top-color:rgba(255,255,255,.7);
              animation:d .8s linear infinite }
      @keyframes d { to { transform:rotate(360deg) } }
    </style>
    <div class="p" role="status"><span></span></div>`;
    const box = w.querySelector('.p');
    box.firstElementChild.textContent = satz;
    const knopf = (text, klasse, art) => {
      const b = document.createElement('button');
      b.textContent = text;
      if (klasse) b.className = klasse;
      b.addEventListener('click', art);
      box.appendChild(b);
      return b;
    };
    if (s.phase === 'laedt') {
      const sp = document.createElement('span'); sp.className = 'spin'; box.appendChild(sp);
      return;
    }
    if (s.phase === 'bereit') knopf(T.installieren, 'haupt', () => frage('updateStarten'));
    else if (s.phase === 'fehler') {
      knopf(T.erneut, 'haupt', () => frage('updateLaden'));
      if (s.seite) knopf(T.seite, '', () => frage('extern', { adresse: s.seite }));
    } else knopf(T.laden, 'haupt', () => frage('updateLaden'));
    const zu = knopf('', 'zu', () => { frage('updateSpaeter'); zeigeUpdate(null); });
    zu.setAttribute('aria-label', T.spaeter);
    zu.title = T.spaeter;
    zu.innerHTML = '<svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round"><path d="M6 6l12 12M18 6L6 18"/></svg>';
  }

  Object.defineProperty(window, 'lumenDesktop', {
    value: Object.freeze({
      version: '__LUMEN_VERSION__',
      plattform: 'windows',
      // Zeigt die Serverauswahl der App (wie Strg+Umschalt+S).
      serverWechseln: () => frage('serverWechseln'),
      // Für die eingebaute Seite; die App prüft selbst, wer fragt.
      _frage: frage,
      _update: zeigeUpdate,
    }),
  });

  const nachLaden = () => frage('updateStand').then(s => { if (s && s.verfuegbar) zeigeUpdate(s); }).catch(() => {});
  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', nachLaden, { once: true });
  else nachLaden();
  // Eine Seite, die ihr <html> neu aufbaut, verliert die Pille — wieder anhängen.
  document.addEventListener('DOMContentLoaded', () => { if (stand && stand.verfuegbar && pille && !pille.isConnected) document.documentElement.appendChild(pille); });
})();
