-- Lumen — eigener Streaming-Server fürs Heimnetz.
-- Copyright (C) 2026 Die Lumen-Autoren
--
-- Veröffentlicht unter der GNU Affero General Public License v3.0.
-- Der vollständige Text steht in der Datei LICENSE.
-- SPDX-License-Identifier: AGPL-3.0-only

-- Lumen — Datenbankschema
--
-- Zwei Grundsätze stecken schon hier drin:
--
-- 1. Fortschritt und Bewertungen hängen immer an einem Profil und werden
--    ausschliesslich über die Abfragen in playback.go gelesen. Dort nimmt
--    jede Abfrage die Profil-Kennung aus dem Token entgegen — es gibt keine
--    Abfrage, die über mehrere Profile hinweg zählt oder sortiert.
-- 2. Es gibt bewusst KEINE Tabelle für einen Wiedergabe-Verlauf mit Zeitpunkt.
--    Was jemand wann und wie lange geschaut hat, wird nicht aufbewahrt.
--    Gespeichert wird nur, wo jemand stehen geblieben ist, damit er weiter-
--    schauen kann — und das gehört diesem Profil allein.

PRAGMA journal_mode = WAL;
PRAGMA foreign_keys = ON;

-- ---------------------------------------------------------------- Konten

CREATE TABLE IF NOT EXISTS account (
  id            TEXT PRIMARY KEY,
  name          TEXT NOT NULL,
  avatar        TEXT NOT NULL DEFAULT 'av-a',   -- Farbe oder Figur aus der Mediathek
  is_owner      INTEGER NOT NULL DEFAULT 0,
  is_kid        INTEGER NOT NULL DEFAULT 0,
  lock_kind     TEXT NOT NULL DEFAULT 'none',   -- none | pin | password
  secret_hash   BLOB,                           -- PBKDF2-SHA256
  secret_salt   BLOB,
  created_at    INTEGER NOT NULL,
  disabled      INTEGER NOT NULL DEFAULT 0,
  expires_at    INTEGER                         -- NULL = läuft nie ab
);

CREATE UNIQUE INDEX IF NOT EXISTS account_one_owner
  ON account (is_owner) WHERE is_owner = 1;

-- Was ein Konto darf. Der Besitzer legt das fest; es sagt nichts darüber aus,
-- was die Person tatsächlich schaut.
CREATE TABLE IF NOT EXISTS grant_library (
  account_id    TEXT NOT NULL REFERENCES account(id) ON DELETE CASCADE,
  library_id    TEXT NOT NULL REFERENCES library(id) ON DELETE CASCADE,
  PRIMARY KEY (account_id, library_id)
);

CREATE TABLE IF NOT EXISTS account_limit (
  account_id      TEXT PRIMARY KEY REFERENCES account(id) ON DELETE CASCADE,
  max_age         INTEGER NOT NULL DEFAULT 18,   -- Altersfreigabe in Jahren
  allow_unrated   INTEGER NOT NULL DEFAULT 0,
  max_streams     INTEGER NOT NULL DEFAULT 2,
  allow_transcode INTEGER NOT NULL DEFAULT 1,
  allow_download  INTEGER NOT NULL DEFAULT 1,
  max_downloads   INTEGER NOT NULL DEFAULT 20,
  remote_quality  TEXT NOT NULL DEFAULT '1080p'
);

CREATE TABLE IF NOT EXISTS token (
  hash          BLOB PRIMARY KEY,               -- SHA-256 des Tokens, nie das Token selbst
  account_id    TEXT NOT NULL REFERENCES account(id) ON DELETE CASCADE,
  device_name   TEXT NOT NULL,
  device_kind   TEXT NOT NULL,                  -- tv | phone | tablet | desktop | browser
  created_at    INTEGER NOT NULL,
  last_seen_at  INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS token_by_account ON token (account_id);

CREATE TABLE IF NOT EXISTS invite (
  code          TEXT PRIMARY KEY,
  created_by    TEXT NOT NULL REFERENCES account(id) ON DELETE CASCADE,
  expires_at    INTEGER NOT NULL,
  used_at       INTEGER,
  preset        TEXT NOT NULL                   -- JSON: Bibliotheken und Rechte
);

-- ---------------------------------------------------------------- Mediathek

CREATE TABLE IF NOT EXISTS library (
  id            TEXT PRIMARY KEY,
  name          TEXT NOT NULL,
  kind          TEXT NOT NULL,                  -- movie | series
  path          TEXT NOT NULL,
  language      TEXT NOT NULL DEFAULT 'de',
  scanned_at    INTEGER
);

CREATE TABLE IF NOT EXISTS item (
  id            TEXT PRIMARY KEY,
  library_id    TEXT NOT NULL REFERENCES library(id) ON DELETE CASCADE,
  kind          TEXT NOT NULL,                  -- movie | series | season | episode
  parent_id     TEXT REFERENCES item(id) ON DELETE CASCADE,
  title         TEXT NOT NULL,
  sort_title    TEXT NOT NULL,
  original_title TEXT,
  year          INTEGER,
  season_no     INTEGER,
  episode_no    INTEGER,
  overview      TEXT,
  age_rating    TEXT,
  age_years     INTEGER,                        -- für die Altersgrenze, NULL = ungeprüft
  runtime_ms    INTEGER,
  premiered     TEXT,
  tmdb_id       TEXT,
  tvdb_id       TEXT,
  imdb_id       TEXT,
  imdb_rating   REAL,
  imdb_votes    INTEGER,
  added_at      INTEGER NOT NULL,
  updated_at    INTEGER NOT NULL,
  match_state   TEXT NOT NULL DEFAULT 'pending' -- pending | matched | unmatched | manual
);
CREATE INDEX IF NOT EXISTS item_by_library ON item (library_id, kind);
CREATE INDEX IF NOT EXISTS item_by_parent  ON item (parent_id, season_no, episode_no);
CREATE INDEX IF NOT EXISTS item_by_sort    ON item (sort_title);

-- Suchindex als gewöhnliche Tabelle statt FTS5: eine Mediathek zu Hause hat
-- ein paar tausend Titel, da reicht LIKE auf einen vorbereiteten Text. Das
-- erspart dem Server eine Erweiterung, die beim Bauen eigens eingeschaltet
-- werden müsste — und damit eine Fehlerquelle beim Ausliefern.
CREATE TABLE IF NOT EXISTS item_search (
  item_id  TEXT PRIMARY KEY REFERENCES item(id) ON DELETE CASCADE,
  haystack TEXT NOT NULL      -- Titel, Originaltitel, Regie, Besetzung, alles klein
);
CREATE INDEX IF NOT EXISTS item_search_hay ON item_search (haystack);

CREATE TABLE IF NOT EXISTS genre (
  item_id       TEXT NOT NULL REFERENCES item(id) ON DELETE CASCADE,
  name          TEXT NOT NULL,
  PRIMARY KEY (item_id, name)
);

CREATE TABLE IF NOT EXISTS person (
  item_id       TEXT NOT NULL REFERENCES item(id) ON DELETE CASCADE,
  name          TEXT NOT NULL,
  role          TEXT NOT NULL,                  -- actor | director | writer
  character     TEXT,
  ord           INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS person_by_item ON person (item_id, role, ord);

CREATE TABLE IF NOT EXISTS artwork (
  item_id       TEXT NOT NULL REFERENCES item(id) ON DELETE CASCADE,
  kind          TEXT NOT NULL,                  -- poster | backdrop | logo | thumb
  path          TEXT NOT NULL,                  -- Datei im Zwischenspeicher
  source_url    TEXT,
  blurhash      TEXT,
  PRIMARY KEY (item_id, kind)
);

-- Eine Datei auf der Platte. Ein Titel kann mehrere haben (4K und 1080p,
-- Mehrteiler, Director's Cut) — daraus speist sich die Doppel-Erkennung.
CREATE TABLE IF NOT EXISTS media_file (
  id            TEXT PRIMARY KEY,
  item_id       TEXT NOT NULL REFERENCES item(id) ON DELETE CASCADE,
  path          TEXT NOT NULL UNIQUE,
  size_bytes    INTEGER NOT NULL,
  mtime         INTEGER NOT NULL,
  part_no       INTEGER NOT NULL DEFAULT 1,
  edition       TEXT,
  container     TEXT,
  duration_ms   INTEGER,
  bitrate       INTEGER,
  width         INTEGER,
  height        INTEGER,
  video_codec   TEXT,
  hdr           TEXT,
  probed_at     INTEGER,
  missing_since INTEGER                          -- gesetzt, wenn die Datei weg ist
);
CREATE INDEX IF NOT EXISTS file_by_item ON media_file (item_id, part_no);

CREATE TABLE IF NOT EXISTS media_track (
  file_id       TEXT NOT NULL REFERENCES media_file(id) ON DELETE CASCADE,
  idx           INTEGER NOT NULL,
  kind          TEXT NOT NULL,                  -- audio | subtitle
  codec         TEXT,
  language      TEXT,
  title         TEXT,
  channels      INTEGER,
  forced        INTEGER NOT NULL DEFAULT 0,
  hearing_impaired INTEGER NOT NULL DEFAULT 0,
  external_path TEXT,                            -- für .srt neben der Datei
  PRIMARY KEY (file_id, idx)
);

CREATE TABLE IF NOT EXISTS chapter (
  file_id       TEXT NOT NULL REFERENCES media_file(id) ON DELETE CASCADE,
  idx           INTEGER NOT NULL,
  start_ms      INTEGER NOT NULL,
  end_ms        INTEGER NOT NULL,
  title         TEXT,
  kind          TEXT,                            -- intro | credits | normal
  PRIMARY KEY (file_id, idx)
);

CREATE TABLE IF NOT EXISTS collection (
  id            TEXT PRIMARY KEY,
  name          TEXT NOT NULL,
  overview      TEXT,
  tmdb_id       TEXT
);
CREATE TABLE IF NOT EXISTS collection_item (
  collection_id TEXT NOT NULL REFERENCES collection(id) ON DELETE CASCADE,
  item_id       TEXT NOT NULL REFERENCES item(id) ON DELETE CASCADE,
  ord           INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (collection_id, item_id)
);

CREATE TABLE IF NOT EXISTS trash (
  item_id       TEXT PRIMARY KEY,
  title         TEXT NOT NULL,
  deleted_at    INTEGER NOT NULL,
  purge_after   INTEGER NOT NULL,
  size_bytes    INTEGER NOT NULL,
  paths         TEXT NOT NULL                    -- JSON
);

-- ------------------------------------------------- Was jedem Profil gehört
--
-- Ab hier wird es privat. Jede Zeile gehört genau einem Konto. Es gibt in
-- playback.go keine Abfrage ohne account_id in der WHERE-Klausel, und keine
-- mit GROUP BY über mehrere Konten. Der Besitzer ist davon nicht ausgenommen.

CREATE TABLE IF NOT EXISTS progress (
  account_id    TEXT NOT NULL REFERENCES account(id) ON DELETE CASCADE,
  item_id       TEXT NOT NULL REFERENCES item(id) ON DELETE CASCADE,
  position_ms   INTEGER NOT NULL,
  duration_ms   INTEGER NOT NULL,
  finished      INTEGER NOT NULL DEFAULT 0,
  -- Keine Uhrzeit: nur eine laufende Nummer je Profil, damit
  -- „Weiterschauen" das zuletzt Angefangene vorn zeigt. Sie zählt nur
  -- weiter, wenn jemand zu einem anderen Titel wechselt — nicht mit der
  -- Zeit, die geschaut wird.
  reihenfolge   INTEGER NOT NULL,
  PRIMARY KEY (account_id, item_id)
);
-- Der Index progress_resume entsteht erst nach dem Umbau (store.go,
-- nachUmbauIndizes): Bei alten Datenbanken heisst die Spalte hier noch
-- updated_at.

-- Die laufende Nummer je Profil und der Titel, an dem sie zuletzt
-- weitergezählt hat.
CREATE TABLE IF NOT EXISTS progress_reihe (
  account_id    TEXT PRIMARY KEY REFERENCES account(id) ON DELETE CASCADE,
  reihe         INTEGER NOT NULL DEFAULT 0,
  letzter       TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS rating (
  account_id    TEXT NOT NULL REFERENCES account(id) ON DELETE CASCADE,
  item_id       TEXT NOT NULL REFERENCES item(id) ON DELETE CASCADE,
  stars         INTEGER NOT NULL,                -- 1..5
  shared        INTEGER NOT NULL DEFAULT 0,      -- nur wenn die Person es will
  updated_at    INTEGER NOT NULL,
  PRIMARY KEY (account_id, item_id)
);

-- Angaben zu Personen aus der Besetzung, zwischengespeichert.
CREATE TABLE IF NOT EXISTS person_info (
  name          TEXT PRIMARY KEY,
  daten         TEXT NOT NULL,
  geholt        INTEGER NOT NULL
);

-- Favoriten: was jemandem besonders gefallen hat. Wie die
-- Merkliste privat — nur das eigene Profil sieht es.
CREATE TABLE IF NOT EXISTS favorit (
  account_id    TEXT NOT NULL REFERENCES account(id) ON DELETE CASCADE,
  item_id       TEXT NOT NULL REFERENCES item(id) ON DELETE CASCADE,
  added_at      INTEGER NOT NULL,
  PRIMARY KEY (account_id, item_id)
);

CREATE TABLE IF NOT EXISTS watchlist (
  account_id    TEXT NOT NULL REFERENCES account(id) ON DELETE CASCADE,
  item_id       TEXT NOT NULL REFERENCES item(id) ON DELETE CASCADE,
  added_at      INTEGER NOT NULL,
  PRIMARY KEY (account_id, item_id)
);

CREATE TABLE IF NOT EXISTS playlist (
  id            TEXT PRIMARY KEY,
  account_id    TEXT NOT NULL REFERENCES account(id) ON DELETE CASCADE,
  name          TEXT NOT NULL,
  shared        INTEGER NOT NULL DEFAULT 0,      -- im Haushalt sichtbar
  collaborative INTEGER NOT NULL DEFAULT 0,      -- andere dürfen mitschreiben
  created_at    INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS playlist_item (
  playlist_id   TEXT NOT NULL REFERENCES playlist(id) ON DELETE CASCADE,
  item_id       TEXT NOT NULL REFERENCES item(id) ON DELETE CASCADE,
  ord           INTEGER NOT NULL,
  added_by      TEXT NOT NULL REFERENCES account(id) ON DELETE CASCADE,
  PRIMARY KEY (playlist_id, item_id)
);

CREATE TABLE IF NOT EXISTS prefs (
  account_id    TEXT NOT NULL REFERENCES account(id) ON DELETE CASCADE,
  key           TEXT NOT NULL,
  value         TEXT NOT NULL,
  PRIMARY KEY (account_id, key)
);

-- ---------------------------------------------------------------- Wünsche
-- Der Besitzer sieht Titel und Absender — mehr nicht.

CREATE TABLE IF NOT EXISTS wish (
  id            TEXT PRIMARY KEY,
  account_id    TEXT NOT NULL REFERENCES account(id) ON DELETE CASCADE,
  title         TEXT NOT NULL,
  year          INTEGER,
  tmdb_id       TEXT,
  state         TEXT NOT NULL DEFAULT 'open',    -- open | working | done | declined
  note          TEXT,
  created_at    INTEGER NOT NULL,
  updated_at    INTEGER NOT NULL
);

-- ---------------------------------------------------------------- Mitteilungen

CREATE TABLE IF NOT EXISTS notice (
  id            TEXT PRIMARY KEY,
  account_id    TEXT REFERENCES account(id) ON DELETE CASCADE, -- NULL = an den Besitzer
  kind          TEXT NOT NULL,
  title         TEXT NOT NULL,
  body          TEXT NOT NULL,
  item_id       TEXT REFERENCES item(id) ON DELETE SET NULL,
  created_at    INTEGER NOT NULL,
  read_at       INTEGER
);
CREATE INDEX IF NOT EXISTS notice_inbox ON notice (account_id, created_at DESC);

-- ---------------------------------------------------------------- Server

CREATE TABLE IF NOT EXISTS setting (
  key           TEXT PRIMARY KEY,
  value         TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS log_line (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  at            INTEGER NOT NULL,
  level         TEXT NOT NULL,                   -- info | warn | error
  area          TEXT NOT NULL,
  message       TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS log_recent ON log_line (at DESC);
