-- Lumen — eigener Streaming-Server fürs Heimnetz.
-- Copyright (C) 2026 Die Lumen-Autoren
--
-- Veröffentlicht unter der GNU Affero General Public License v3.0.
-- Der vollständige Text steht in der Datei LICENSE.
-- SPDX-License-Identifier: AGPL-3.0-only
-- Alter Stand aus dem Verlauf, für den Upgrade-Test (upgrade_test.go).

-- Datenbank wie Lumen v0.1.0 (git 4fbe29e) sie anlegte, Stand 0.
-- Erzeugt aus schema.sql und nachruesten() dieser Fassung, mit ein paar Zeilen Inhalt.
BEGIN TRANSACTION;
CREATE TABLE account (
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
INSERT INTO "account" VALUES('acc_lea','Lea','av-a',1,0,'none',NULL,NULL,1700000000,0,NULL);
INSERT INTO "account" VALUES('acc_ben','Ben','av-a',0,0,'none',NULL,NULL,1700000000,0,NULL);
CREATE TABLE account_limit (
  account_id      TEXT PRIMARY KEY REFERENCES account(id) ON DELETE CASCADE,
  max_age         INTEGER NOT NULL DEFAULT 18,   -- Altersfreigabe in Jahren
  allow_unrated   INTEGER NOT NULL DEFAULT 0,
  max_streams     INTEGER NOT NULL DEFAULT 2,
  allow_transcode INTEGER NOT NULL DEFAULT 1,
  allow_download  INTEGER NOT NULL DEFAULT 1,
  max_downloads   INTEGER NOT NULL DEFAULT 20,
  remote_quality  TEXT NOT NULL DEFAULT '1080p'
);
CREATE TABLE artwork (
  item_id       TEXT NOT NULL REFERENCES item(id) ON DELETE CASCADE,
  kind          TEXT NOT NULL,                  -- poster | backdrop | logo | thumb
  path          TEXT NOT NULL,                  -- Datei im Zwischenspeicher
  source_url    TEXT,
  blurhash      TEXT,
  PRIMARY KEY (item_id, kind)
);
CREATE TABLE chapter (
  file_id       TEXT NOT NULL REFERENCES media_file(id) ON DELETE CASCADE,
  idx           INTEGER NOT NULL,
  start_ms      INTEGER NOT NULL,
  end_ms        INTEGER NOT NULL,
  title         TEXT,
  kind          TEXT,                            -- intro | credits | normal
  PRIMARY KEY (file_id, idx)
);
CREATE TABLE collection (
  id            TEXT PRIMARY KEY,
  name          TEXT NOT NULL,
  overview      TEXT,
  tmdb_id       TEXT
);
CREATE TABLE collection_item (
  collection_id TEXT NOT NULL REFERENCES collection(id) ON DELETE CASCADE,
  item_id       TEXT NOT NULL REFERENCES item(id) ON DELETE CASCADE,
  ord           INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (collection_id, item_id)
);
CREATE TABLE genre (
  item_id       TEXT NOT NULL REFERENCES item(id) ON DELETE CASCADE,
  name          TEXT NOT NULL,
  PRIMARY KEY (item_id, name)
);
CREATE TABLE grant_library (
  account_id    TEXT NOT NULL REFERENCES account(id) ON DELETE CASCADE,
  library_id    TEXT NOT NULL REFERENCES library(id) ON DELETE CASCADE,
  PRIMARY KEY (account_id, library_id)
);
CREATE TABLE invite (
  code          TEXT PRIMARY KEY,
  created_by    TEXT NOT NULL REFERENCES account(id) ON DELETE CASCADE,
  expires_at    INTEGER NOT NULL,
  used_at       INTEGER,
  preset        TEXT NOT NULL                   -- JSON: Bibliotheken und Rechte
);
CREATE TABLE item (
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
INSERT INTO "item" VALUES('it_f0','lib_f','movie',NULL,'Nordlicht','nordlicht',NULL,2025,NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL,1700000000,1700000000,'pending');
INSERT INTO "item" VALUES('it_f1','lib_f','movie',NULL,'Ärger im Paradies','ärger im paradies',NULL,2001,NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL,1700000001,1700000000,'pending');
INSERT INTO "item" VALUES('it_f2','lib_f','movie',NULL,'Die Bucht','die bucht',NULL,2019,NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL,1700000002,1700000000,'pending');
INSERT INTO "item" VALUES('it_s','lib_s','series',NULL,'Fallout','fallout',NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL,1700000000,1700000000,'matched');
INSERT INTO "item" VALUES('it_st','lib_s','season','it_s','Staffel 1','staffel 1',NULL,NULL,1,NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL,1700000000,1700000000,'matched');
INSERT INTO "item" VALUES('it_e1','lib_s','episode','it_st','Folge 1','folge 1',NULL,NULL,1,1,NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL,1700000101,1700000000,'pending');
INSERT INTO "item" VALUES('it_e2','lib_s','episode','it_st','Folge 2','folge 2',NULL,NULL,1,2,NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL,1700000102,1700000000,'pending');
CREATE TABLE item_search (
  item_id  TEXT PRIMARY KEY REFERENCES item(id) ON DELETE CASCADE,
  haystack TEXT NOT NULL      -- Titel, Originaltitel, Regie, Besetzung, alles klein
);
INSERT INTO "item_search" VALUES('it_f0','nordlicht');
INSERT INTO "item_search" VALUES('it_f1','ärger im paradies');
INSERT INTO "item_search" VALUES('it_f2','die bucht');
CREATE TABLE library (
  id            TEXT PRIMARY KEY,
  name          TEXT NOT NULL,
  kind          TEXT NOT NULL,                  -- movie | series
  path          TEXT NOT NULL,
  language      TEXT NOT NULL DEFAULT 'de',
  scanned_at    INTEGER
);
INSERT INTO "library" VALUES('lib_f','Filme','movie','/medien/filme','de',NULL);
INSERT INTO "library" VALUES('lib_s','Serien','series','/medien/serien','de',NULL);
CREATE TABLE log_line (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  at            INTEGER NOT NULL,
  level         TEXT NOT NULL,                   -- info | warn | error
  area          TEXT NOT NULL,
  message       TEXT NOT NULL
);
CREATE TABLE media_file (
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
INSERT INTO "media_file" VALUES('f_f0','it_f0','/medien/filme/Nordlicht (2025)/Nordlicht.mkv',1000,1600000000,1,NULL,NULL,5400000,NULL,NULL,NULL,NULL,NULL,NULL,NULL);
INSERT INTO "media_file" VALUES('f_f1','it_f1','/medien/filme/Ärger im Paradies (2001)/Ärger im Paradies.mkv',1001,1600000000,1,NULL,NULL,5400000,NULL,NULL,NULL,NULL,NULL,NULL,NULL);
INSERT INTO "media_file" VALUES('f_f2','it_f2','/medien/filme/Die Bucht (2019)/Die Bucht.mkv',1002,1600000000,1,NULL,NULL,5400000,NULL,NULL,NULL,NULL,NULL,NULL,NULL);
INSERT INTO "media_file" VALUES('f_e1','it_e1','/medien/serien/Fallout/Season 01/Fallout S01E01.mkv',500,1600000000,1,NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL);
INSERT INTO "media_file" VALUES('f_e2','it_e2','/medien/serien/Fallout/Season 01/Fallout S01E02.mkv',500,1600000000,1,NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL);
CREATE TABLE media_track (
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
CREATE TABLE notice (
  id            TEXT PRIMARY KEY,
  account_id    TEXT REFERENCES account(id) ON DELETE CASCADE, -- NULL = an den Besitzer
  kind          TEXT NOT NULL,
  title         TEXT NOT NULL,
  body          TEXT NOT NULL,
  item_id       TEXT REFERENCES item(id) ON DELETE SET NULL,
  created_at    INTEGER NOT NULL,
  read_at       INTEGER
);
CREATE TABLE person (
  item_id       TEXT NOT NULL REFERENCES item(id) ON DELETE CASCADE,
  name          TEXT NOT NULL,
  role          TEXT NOT NULL,                  -- actor | director | writer
  character     TEXT,
  ord           INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE playlist (
  id            TEXT PRIMARY KEY,
  account_id    TEXT NOT NULL REFERENCES account(id) ON DELETE CASCADE,
  name          TEXT NOT NULL,
  shared        INTEGER NOT NULL DEFAULT 0,      -- im Haushalt sichtbar
  collaborative INTEGER NOT NULL DEFAULT 0,      -- andere dürfen mitschreiben
  created_at    INTEGER NOT NULL
);
CREATE TABLE playlist_item (
  playlist_id   TEXT NOT NULL REFERENCES playlist(id) ON DELETE CASCADE,
  item_id       TEXT NOT NULL REFERENCES item(id) ON DELETE CASCADE,
  ord           INTEGER NOT NULL,
  added_by      TEXT NOT NULL REFERENCES account(id) ON DELETE CASCADE,
  PRIMARY KEY (playlist_id, item_id)
);
CREATE TABLE prefs (
  account_id    TEXT NOT NULL REFERENCES account(id) ON DELETE CASCADE,
  key           TEXT NOT NULL,
  value         TEXT NOT NULL,
  PRIMARY KEY (account_id, key)
);
CREATE TABLE progress (
  account_id    TEXT NOT NULL REFERENCES account(id) ON DELETE CASCADE,
  item_id       TEXT NOT NULL REFERENCES item(id) ON DELETE CASCADE,
  position_ms   INTEGER NOT NULL,
  duration_ms   INTEGER NOT NULL,
  finished      INTEGER NOT NULL DEFAULT 0,
  updated_at    INTEGER NOT NULL,
  PRIMARY KEY (account_id, item_id)
);
INSERT INTO "progress" VALUES('acc_lea','it_f0',60000,5400000,0,1700001000);
INSERT INTO "progress" VALUES('acc_lea','it_e1',60000,5400000,0,1700002000);
INSERT INTO "progress" VALUES('acc_ben','it_f1',60000,5400000,0,1700003000);
CREATE TABLE rating (
  account_id    TEXT NOT NULL REFERENCES account(id) ON DELETE CASCADE,
  item_id       TEXT NOT NULL REFERENCES item(id) ON DELETE CASCADE,
  stars         INTEGER NOT NULL,                -- 1..5
  shared        INTEGER NOT NULL DEFAULT 0,      -- nur wenn die Person es will
  updated_at    INTEGER NOT NULL,
  PRIMARY KEY (account_id, item_id)
);
CREATE TABLE setting (
  key           TEXT PRIMARY KEY,
  value         TEXT NOT NULL
);
CREATE TABLE token (
  hash          BLOB PRIMARY KEY,               -- SHA-256 des Tokens, nie das Token selbst
  account_id    TEXT NOT NULL REFERENCES account(id) ON DELETE CASCADE,
  device_name   TEXT NOT NULL,
  device_kind   TEXT NOT NULL,                  -- tv | phone | tablet | desktop | browser
  created_at    INTEGER NOT NULL,
  last_seen_at  INTEGER NOT NULL
);
CREATE TABLE trash (
  item_id       TEXT PRIMARY KEY,
  title         TEXT NOT NULL,
  deleted_at    INTEGER NOT NULL,
  purge_after   INTEGER NOT NULL,
  size_bytes    INTEGER NOT NULL,
  paths         TEXT NOT NULL                    -- JSON
);
CREATE TABLE watchlist (
  account_id    TEXT NOT NULL REFERENCES account(id) ON DELETE CASCADE,
  item_id       TEXT NOT NULL REFERENCES item(id) ON DELETE CASCADE,
  added_at      INTEGER NOT NULL,
  PRIMARY KEY (account_id, item_id)
);
INSERT INTO "watchlist" VALUES('acc_ben','it_f2',1700000000);
CREATE TABLE wish (
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
CREATE UNIQUE INDEX account_one_owner
  ON account (is_owner) WHERE is_owner = 1;
CREATE INDEX token_by_account ON token (account_id);
CREATE INDEX item_by_library ON item (library_id, kind);
CREATE INDEX item_by_parent  ON item (parent_id, season_no, episode_no);
CREATE INDEX item_by_sort    ON item (sort_title);
CREATE INDEX item_search_hay ON item_search (haystack);
CREATE INDEX person_by_item ON person (item_id, role, ord);
CREATE INDEX file_by_item ON media_file (item_id, part_no);
CREATE INDEX progress_resume ON progress (account_id, updated_at DESC);
CREATE INDEX notice_inbox ON notice (account_id, created_at DESC);
CREATE INDEX log_recent ON log_line (at DESC);
DELETE FROM "sqlite_sequence";
COMMIT;
PRAGMA user_version = 0;
