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

> **Step-by-step guide (PDF, German):** [Lumen-Installationsanleitung.pdf](docs/Lumen-Installationsanleitung.pdf)

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

Updating: `sudo docker compose pull && sudo docker compose up -d`, or with
one click in the admin pages — see [One-click updates](#one-click-updates).
Lumen always
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
a one-time code: at the next sign-in that person sets their own PIN (or
password, if the profile uses one), entering it twice. A one-time code is
valid for 72 hours; after that an admin has to issue a new one. App versions
older than this release cannot finish this step — use the web app or update
the app first.
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

## One-click updates

Inside a container Lumen can't replace itself — the whole image is
swapped. A small companion does that: the **updater**
([Watchtower](https://github.com/nicholas-fedor/watchtower), the
maintained fork by Nick Fedor, pinned to `nickfedor/watchtower:1.24.0`).
Once it is set up, *Admin › Updates* shows an **Update Now** button, and
*Update automatically* (night window) works in a container too.

When you press it, Lumen asks first if someone is watching, asks for your
PIN or password again, writes the database cleanly to disk, logs the
update and tells the updater "now". The updater pulls the new image, stops
Lumen (30 seconds to shut down cleanly), starts it with the new image and
removes the old one. The page shows "Updating" and reports back once the
new version is running. In the night window the same happens, but only
when nobody is watching.

**Security:** only the updater gets Docker access (`/var/run/docker.sock`),
Lumen does not. The updater listens only on the project's internal
network (no published port) and accepts only requests carrying the secret
token, so all Lumen can tell it is "update now". It only touches
containers labelled `com.centurylinklabs.watchtower.enable=true` — i.e.
Lumen. The token never appears in API responses, logs or error messages,
and the address can only be set in `docker-compose.yml`, not in the UI.

Settings on the `lumen` service: `LUMEN_AKTUALISIERER_URL` (e.g.
`http://aktualisierer:8080`) and `LUMEN_AKTUALISIERER_TOKEN` (same value
as the updater's `WATCHTOWER_HTTP_API_TOKEN`). The
[`docker-compose.yml`](docker-compose.yml) in this repository has the
lines for the `lumen` service; the updater itself is commented out because
it gets access to Docker — switch it on deliberately. To enable it (or with
an older file), on a Synology:

1. **Create the token** — one line in a `.env` file next to
   `docker-compose.yml`. Without SSH: *Control Panel › Task Scheduler ›
   Create › Scheduled Task › User-defined script*, user **root**, with this
   script, then select the task and *Run* it once (delete it afterwards):

   ```sh
   grep -qs LUMEN_AKTUALISIERER_TOKEN /volume1/docker/lumen/.env || \
     echo "LUMEN_AKTUALISIERER_TOKEN=$(openssl rand -hex 32)" >> /volume1/docker/lumen/.env
   ```

   Any random 64-character string of letters and digits works too. If you
   prefer no `.env`, put the token directly in both places below instead of
   `${LUMEN_AKTUALISIERER_TOKEN:-}`.
2. **Edit the YAML:** *Container Manager › Project › lumen*, *Stop*, then
   edit the *YAML configuration*. Add to the `lumen` service (`labels` at
   the same level as `restart`, the two lines under `environment`):

   ```yaml
       labels:
         - com.centurylinklabs.watchtower.enable=true
       environment:
         # … existing lines, plus:
         - LUMEN_AKTUALISIERER_URL=http://aktualisierer:8080
         - LUMEN_AKTUALISIERER_TOKEN=${LUMEN_AKTUALISIERER_TOKEN:-}
   ```

   and a second service under `services:`, at the same level as `lumen:`:

   ```yaml
     aktualisierer:
       image: nickfedor/watchtower:1.24.0
       container_name: lumen-aktualisierer
       restart: unless-stopped
       volumes:
         - /var/run/docker.sock:/var/run/docker.sock
       environment:
         - TZ=Europe/Zurich
         - WATCHTOWER_HTTP_API_ENDPOINTS=update
         - WATCHTOWER_HTTP_API_TOKEN=${LUMEN_AKTUALISIERER_TOKEN:-}
         - WATCHTOWER_LABEL_ENABLE=true
         - WATCHTOWER_CLEANUP=true
         - WATCHTOWER_TIMEOUT=30s
         - WATCHTOWER_NO_STARTUP_MESSAGE=true
   ```
3. **Save, then *Action › Build*.** The button appears in *Admin › Updates*
   as soon as a newer version is available (*Check Now*).

The updater does nothing on its own: with its HTTP API on, its own
schedule is off, and Lumen's night window decides — only Lumen knows
whether someone is watching. Its version is pinned; bump it now and then.
Your Synology may report that container "lumen" stopped unexpectedly
during an update — that's the updater and harmless. If something goes
wrong, the page says why (token rejected, updater not reachable, no newer
image yet); details are in the log of container `lumen-aktualisierer`.

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

### Licenses and third-party software

Everything not written by the Lumen authors is listed with version, license
and source in [THIRD_PARTY.md](THIRD_PARTY.md) (German:
[DRITTSOFTWARE.md](DRITTSOFTWARE.md)). In short:

- Built into the binary: only `go-sqlite3` (MIT, bundling public-domain
  SQLite), `go-qrcode` (MIT) and the Go standard library (BSD-3-Clause).
  Their license texts ship with the release archive and the Docker image.
- FFmpeg is only executed, not linked, and keeps its own license (in the
  image: the Alpine package, GPL). The Alpine packages in an image and where
  to get their source are listed inside it under `/usr/share/licenses/lumen/`.
- The updater (Watchtower, Apache-2.0) is **not part of Lumen**: whoever
  enables it pulls it themselves from its publisher's Docker Hub account.
- Posters and descriptions come from TMDB, using your own key. This product
  uses the TMDB API but is not endorsed or certified by TMDB. Ratings come
  via OMDb (CC BY-NC 4.0), some biographies from Wikipedia (CC BY-SA 4.0).
  The app shows this under **Settings › Data sources and third-party
  software**.
