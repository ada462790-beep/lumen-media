# Lumen

*[Deutsche, ausführliche Fassung](README.md)* — the German README is the
complete documentation; this page is a short English summary.

Lumen is your own streaming service at home. Movies and shows from your
own disks, on every device in the house — calm, tidy, with an interface
that looks like Apple built it. Lumen is free software (AGPL-3.0), runs as
a single Docker container on your NAS and needs no account, no
subscription and no cloud.

Lumen is deliberately a server for **movies and TV shows** only — there
are no music or photo libraries. Music and photo libraries from older
versions are removed on the first start of a newer version (files on disk
stay untouched; a backup `lumen.db.vor-umbau` is written first).

**The promise: the admin never sees who watches what.** You run the server
and decide who belongs and what they may see. But when someone watches
something, for how long, and what — that stays with them. Lumen keeps no
watch history, the admin pages don't show it, and there is no query for it.
Tests make sure it stays that way. What is stored and what isn't is listed
(in German) in [DATENSCHUTZ.md](DATENSCHUTZ.md).

## Features

- **One server, every device:** browser, iPhone, iPad, Mac, Apple TV,
  Android, Android TV and Fire TV.
- **Understands your library:** movies, shows, collections, several
  versions of one movie, movies split into parts, NFO files and local
  artwork. Metadata from TMDb, IMDb and Rotten Tomatoes ratings via OMDb.
- **Plays everything:** direct play when the device can handle the file,
  otherwise transcoding — with hardware acceleration (VAAPI, Quick Sync,
  NVENC) if available. Audio tracks, subtitles (including download from
  OpenSubtitles), skip intro, continue watching, trickplay thumbnails,
  downloads for offline viewing.
- **For every person:** own profiles, watchlist, ratings, shared and
  collaborative lists, kids' profiles with age limits, invitations by link.
- **Remote access without opening ports:** through a small relay that
  cannot see inside — Lumen encrypts end to end, the key stays at home.
- **Moving from Emby or Jellyfin:** each profile brings over its own
  watched state, progress and favorites.
- **Languages:** German (Switzerland, Germany, Austria) and English (US, UK).

## Install with Docker

The image is public: **`ghcr.io/ada462790-beep/lumen-media`** (amd64 and
arm64). No registry login, no token.

1. Create a data folder, e.g. `/volume1/docker/lumen/daten` on a Synology
   or `/srv/lumen/daten` on Linux.
2. Copy [`docker-compose.yml`](docker-compose.yml) into the folder above it
   and adjust:
   - the data folder (`source:` under `volumes`),
   - your media folders, e.g. `/volume1/video:/volume1/video:rw`
     (`:rw` lets Lumen merge movies split into parts; it never deletes
     anything),
   - `PUID`/`PGID` — the user that may read your media (Synology: usually
     `1026`/`100`, Linux: usually `1000`/`1000`),
   - `TZ`, your time zone.
3. Start it:

   ```bash
   sudo docker compose up -d
   ```

   On a Synology: *Container Manager › Project › Create*, paste the file.
4. Open `http://<your-nas>:8097`, create the owner account and add your
   libraries.

Updating: `sudo docker compose pull && sudo docker compose up -d` (or the
optional Watchtower service in the compose file, at night). Lumen always
listens on port **8097** — never forward it in your router; use the relay
or Tailscale/WireGuard for remote access.

**Already running the old image** `ghcr.io/ada462790-beep/lumen:latest`?
Replace that line in your `docker-compose.yml` with
`ghcr.io/ada462790-beep/lumen-media:latest` and recreate the container.
Your data stays where it is. The relay image moved the same way, to
`ghcr.io/ada462790-beep/lumen-media-relay`.

Without Docker: download the package for your platform from the
[releases](https://github.com/ada462790-beep/lumen-media/releases), unpack
it and run `./lumen` (FFmpeg should be installed). The admin pages check
for new versions and install signed updates — no GitHub token required.

**Forgot the owner PIN?** Reset it over SSH on the server:
`sudo docker exec -it lumen lumen zuruecksetzen` lists the profiles,
`... zuruecksetzen --konto NAME` sets a new PIN (`--art password` for a
password, `--verwaltung` also clears the admin PIN). The owner keeps all
history; resetting someone else's profile this way deletes their history,
watchlist and ratings, just like in the app, and the new PIN only works as
a one-time code: at the next sign-in that person sets their own PIN.
On a Synology this also works **without SSH**: create a user-defined
script task as **root** in *Control Panel › Task Scheduler* with
`printf '2580\n2580\n' | /usr/local/bin/docker exec -i lumen lumen zuruecksetzen --konto NAME --verwaltung > /volume1/docker/lumen/zuruecksetzen.txt 2>&1`
(the new PIN twice), run it once, check the text file, then delete the
task and the file — the script contains the new PIN.

The admin pages are **desktop only** by design (browser or Mac app); on
phones and tablets Lumen is for watching. Open invitations can be listed
and revoked under *Admin › Accounts*. Arrow keys and a TV remote's D-pad
move focus between cards and buttons, Backspace/Back goes back, F6 jumps
between content and navigation; text follows the browser/system text size.

## Contributing

Bug reports and feature requests go to the
[issues](https://github.com/ada462790-beep/lumen-media/issues). Please
report security vulnerabilities privately, see [SECURITY.md](SECURITY.md).
Contributions are welcome — see [CONTRIBUTING.md](CONTRIBUTING.md) and the
[Contributor License Agreement](CLA.md); the
[Code of Conduct](CODE_OF_CONDUCT.md) applies. Code comments and
identifiers in Lumen are German; English issues and pull requests are fine.

## License

AGPL-3.0-only, see [LICENSE](LICENSE). Copyright (C) 2026 Die
Lumen-Autoren (the Lumen authors). If you run a modified version for
others over a network, you owe them the source — set `LUMEN_SOURCE_URL` to
where your version lives.
