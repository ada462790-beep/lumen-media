// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package org.lumen.core

/**
 * Die Datenmodelle, gelesen aus dem, was der Server schickt.
 *
 * Jedes `from`-Verfahren steht direkt neben seiner Klasse — so sieht man an
 * einer Stelle, welches Feld woher kommt. Pflichtfelder gehen über
 * [required]; fehlt eines, ist das kein Flüchtigkeitsfehler, sondern heisst,
 * dass App und Server nicht zusammenpassen, und genau das meldet der Fehler.
 */

data class Account(
    val id: String,
    val name: String,
    val avatar: String,
    val isOwner: Boolean,
    val isKid: Boolean,
    val lockKind: String,
    val disabled: Boolean,
    // Darf in die Verwaltung: der Besitzer immer, sonst über eine Gruppe
    // mit „Verwaltung". Ein älterer Server kennt das Feld noch
    // nicht — dann gilt, was vorher galt: nur der Besitzer.
    val isAdmin: Boolean = isOwner,
    // Womit diese Person ihre Verwaltung schützt: none, pin, password,
    // totp. Nur für sie selbst von Belang.
    val adminLock: String = "none",
    // Die Gruppe, in der das Konto steckt — falls eine.
    val groupId: String? = null,
    val groupName: String? = null,
) {
    val initial: String get() = name.take(1).uppercase()
    val needsCode: Boolean get() = lockKind != "none"

    /** Ob die Verwaltung eine Sperre hat, die erst geöffnet werden muss. */
    val hatVerwaltungssperre: Boolean get() = adminLock != "none" && adminLock.isNotEmpty()

    companion object {
        fun from(j: JsonValue?): Account {
            val isOwner = j["isOwner"].bool ?: false
            return Account(
                id = j.requiredText("id"),
                name = j.requiredText("name"),
                avatar = j["avatar"].text ?: "av-a",
                isOwner = isOwner,
                isKid = j["isKid"].bool ?: false,
                lockKind = j["lockKind"].text ?: "none",
                disabled = j["disabled"].bool ?: false,
                isAdmin = j["isAdmin"].bool ?: isOwner,
                adminLock = j["adminLock"].text ?: "none",
                groupId = j["groupId"].text,
                groupName = j["groupName"].text,
            )
        }
    }
}

/** Was vor der Anmeldung sichtbar ist: Name, Bild, ob ein Code nötig ist. */
data class ProfileCard(
    val id: String,
    val name: String,
    val avatar: String,
    val lockKind: String,
    val isKid: Boolean,
) {
    val initial: String get() = name.take(1).uppercase()
    val needsCode: Boolean get() = lockKind != "none"

    companion object {
        fun from(j: JsonValue?) = ProfileCard(
            id = j.requiredText("id"),
            name = j.requiredText("name"),
            avatar = j["avatar"].text ?: "av-a",
            lockKind = j["lockKind"].text ?: "none",
            isKid = j["isKid"].bool ?: false,
        )
    }
}

data class Limits(
    val maxAge: Int,
    val allowUnrated: Boolean,
    val maxStreams: Int,
    val allowTranscode: Boolean,
    val allowDownload: Boolean,
    val maxDownloads: Int,
    val remoteQuality: String,
) {
    companion object {
        fun from(j: JsonValue?) = Limits(
            maxAge = j["maxAge"].int ?: 18,
            allowUnrated = j["allowUnrated"].bool ?: true,
            maxStreams = j["maxStreams"].int ?: 2,
            allowTranscode = j["allowTranscode"].bool ?: true,
            allowDownload = j["allowDownload"].bool ?: true,
            maxDownloads = j["maxDownloads"].int ?: 20,
            remoteQuality = j["remoteQuality"].text ?: "1080p",
        )
    }
}

data class Me(
    val account: Account,
    val limits: Limits,
    val prefs: Map<String, String>,
    val libraries: List<String>,
    // Wie viele eigene Neuigkeiten noch ungelesen sind — für den Punkt an
    // der Navigation. Zählt nur, was dieses Konto selbst betrifft.
    val neuigkeiten: Int = 0,
    // Die Namen der eingebauten Figuren, die der Server als Profilbild
    // annimmt. Die Zeichnungen dazu liegen in der App (Profilbild.kt).
    val figuren: List<String> = emptyList(),
    // Die Sprache, die für dieses Profil gilt — die eigene Wahl oder die
    // Vorgabe des Servers. Null bei einem Server, der noch keine kennt.
    val sprache: String? = null,
) {
    companion object {
        fun from(j: JsonValue?) = Me(
            account = Account.from(j.required("account")),
            limits = Limits.from(j["limits"]),
            prefs = j["prefs"].map(),
            libraries = j["libraries"].strings(),
            neuigkeiten = j["neuigkeiten"].int ?: 0,
            figuren = j["figuren"].strings(),
            sprache = Sprachen.wirksam(j),
        )
    }
}

data class Hello(
    val server: String,
    val version: String,
    val setUp: Boolean,
    val ffmpeg: Boolean,
    // Lumen steht unter der AGPL. Deren §13 verlangt, dass wer den Dienst
    // über das Netz benutzt, an den Quelltext kommt. Ein älterer Server
    // schickt die zwei Felder noch nicht — darum nullbar.
    val license: String? = null,
    val sourceUrl: String? = null,
    // Wie der Besitzer den Server nennt („Mein NAS") — der Vorschlag für
    // die Serverliste. Leer oder fehlend, solange keiner gesetzt ist.
    val name: String? = null,
    // Die Sprache, die der Server vorgibt („de-CH"), und welche er kennt.
    // Ein älterer Server schickt beides nicht.
    val sprache: String? = null,
    val sprachen: List<Sprache> = emptyList(),
) {
    companion object {
        fun from(j: JsonValue?) = Hello(
            server = j["server"].text ?: "Lumen",
            version = j["version"].text ?: "?",
            setUp = j["setUp"].bool ?: false,
            ffmpeg = j["ffmpeg"].bool ?: false,
            license = j["license"].text,
            sourceUrl = j["sourceUrl"].text,
            name = j["name"].text?.trim()?.takeIf { it.isNotEmpty() },
            sprache = Sprachen.wirksam(j),
            sprachen = Sprachen.liste(j),
        )
    }
}

// ---------------------------------------------------------------- Mediathek

data class Item(
    val id: String,
    val libraryId: String,
    val kind: String,
    val parentId: String?,
    val title: String,
    val originalTitle: String?,
    val year: Int?,
    val seasonNo: Int?,
    val episodeNo: Int?,
    val overview: String?,
    val ageYears: Int?,
    val runtimeMs: Long?,
    val imdbRating: Double?,
    val imdbVotes: Int?,
    val genres: List<String>,
    /** Rotten Tomatoes: Anteil guter Kritiken in Prozent, wenn bekannt. */
    val rottenTomatoes: Int? = null,
    /** TMDb: Nutzerwertung in Prozent (0–100) und wie viele abgestimmt haben. */
    val tmdbRating: Int? = null,
    val tmdbVotes: Int? = null,
    /** Der Server kann ein Special als solches kennzeichnen (siehe [specialOhneNummer]). */
    val special: Boolean = false,
    // Was dem angemeldeten Profil gehört — und nur ihm.
    val progress: Double,
    val finished: Boolean,
    val myRating: Int,
    val onList: Boolean,
    val resumeMs: Long,
    val poster: String?,
    val backdrop: String?,
    // Ein Standbild aus dem Film selbst. Der Server setzt es nur, wenn
    // Poster oder Hintergrund fehlen — als Ersatz, damit nicht bloss der
    // Titel auf grauem Grund steht. Hinten und mit Vorgabe, damit alle
    // bisherigen Aufrufe weiter übersetzen.
    val frame: String? = null,
    // Nur in einer Liste gesetzt: wer den Titel hineingelegt hat. Das ist
    // Inhalt der Liste, die ihr Besitzer freiwillig teilt — kein Verlauf.
    val hinzugefuegtVon: String? = null,
    // Nur bei Serien: wie viele Folgen dieses Profil gesehen hat, von wie
    // vielen es sehen darf. Zählt nur die eigenen Folgen — was andere im
    // Haushalt geschaut haben, steht hier nicht und kommt auch nicht vom Server.
    val gesehen: Int = 0,
    val folgen: Int = 0,
    // Für die Abzeichen: in den letzten sieben Tagen dazugekommen, wie
    // viele Folgen neu sind, beste Auflösung (SD, HD, 4K), HDR-Art (HDR10,
    // HLG, DV) und die meisten Tonkanäle. Alles aus der Datei, nichts aus
    // dem Stand anderer.
    val neu: Boolean = false,
    val neueFolgen: Int = 0,
    val qualitaet: String? = null,
    val hdr: String? = null,
    val kanaele: Int = 0,
    // Erstausstrahlung einer Folge, wie die Metadaten-Quelle sie nennt
    // („2019-04-14"). Steht in der Folgenliste der Titelseite.
    val premiered: String? = null,
    // Nur bei Serien: das Jahr der letzten Staffel und ob noch neue kommen.
    // Fehlt laeuftNoch, weiss der Server es nicht — dann steht
    // nur das Startjahr da, wie bisher.
    val endYear: Int? = null,
    val laeuftNoch: Boolean? = null,
    // Das Herz: nur das eigene, wie die Merkliste.
    val favorite: Boolean = false,
) {
    /**
     * Ein Special ohne eigene Nummer („S08E00 - The Last Watch"): Der Server
     * zählt es ab 1000 in Staffel 0 (store.SpecialOhneNummer) — die Nummer
     * ist selbst vergeben, angezeigt wird „Special" statt „Folge 1001".
     * Echte S00Exx (unter 1000) behalten ihre Nummer. Wie im Browser.
     */
    val specialOhneNummer: Boolean
        get() = kind == "episode" && (seasonNo ?: 0) == 0 &&
            (episodeNo?.let { it >= SPECIAL_OHNE_NUMMER_AB } ?: special)

    /** Die Nummer einer Folge in Listen — leer bei Specials ohne Nummer. */
    val folgenNummer: String get() = if (specialOhneNummer) "" else episodeNo?.toString() ?: ""

    /**
     * Das Jahr, wie es überall unter einem Titel steht — bei Serien der
     * Zeitraum: „seit 2014" für eine, die noch läuft, „2014–2020" für eine
     * abgeschlossene. Dieselbe Regel wie jahre() im Browser (app.js), damit
     * Telefon, Fernseher und Browser dasselbe sagen. Null ohne Jahr.
     */
    val jahresText: String?
        get() {
            val start = year?.takeIf { it > 0 } ?: return null
            val laeuft = laeuftNoch
            if (kind != "series" || laeuft == null) return start.toString()
            if (laeuft) return Texte.t(Tx.SEIT_JAHR, start)
            val ende = endYear?.takeIf { it > 0 && it != start } ?: return start.toString()
            return "$start–$ende"
        }

    /** „Neu" oder „3 neue Folgen" — links oben auf der Karte. */
    val abzeichenLinks: String? get() = Abzeichen.links(this)

    /** „4K", „HDR10", „Dolby Vision" — rechts oben auf der Karte. */
    val abzeichenRechts: List<String> get() = Abzeichen.rechts(this)

    val resumeSeconds: Double get() = resumeMs / 1000.0
    val runtimeSeconds: Double get() = (runtimeMs ?: 0L) / 1000.0

    /**
     * Der Stand einer Serie als Satz: „4 von 10 Folgen", „alle 10 Folgen
     * gesehen" — oder null, wenn es keine Serie ist oder noch nichts
     * gesehen wurde. Dann steht auf der Karte, was sonst dort steht.
     */
    val serienStand: String?
        get() {
            if (kind != "series" || folgen <= 0 || gesehen <= 0) return null
            return if (gesehen >= folgen) Texte.n(TxN.SERIE_ALLE_GESEHEN, folgen)
                   else Texte.t(Tx.SERIE_X_VON_Y, gesehen, folgen)
        }

    /** Welches Bild als Poster taugt: das Poster, sonst das Standbild. */
    val posterKind: String
        get() = if (poster == null && frame != null) "frame" else "poster"

    /** Welches Bild in die Breite taugt: Hintergrund, Standbild, Poster. */
    val wideKind: String
        get() = when {
            backdrop != null -> "backdrop"
            // Bei Folgen ist das Poster das breite Standbild von TMDb.
            kind == "episode" && poster != null -> "poster"
            frame != null -> "frame"
            else -> "poster"
        }

    /** „2019 · 1 Std. 58 Min. · ab 16 · Drama" */
    val subtitleLine: String
        get() = buildList {
            jahresText?.let { add(it) }
            runtimeMs?.takeIf { it > 0 }?.let { add(duration(it)) }
            ageYears?.let { add(Texte.t(Tx.AB_ALTER, it)) }
            if (genres.isNotEmpty()) add(genres.take(2).joinToString(" · "))
        }.joinToString(" · ")

    companion object {
        /** Ab dieser Nummer ist eine Folge in Staffel 0 ein Special ohne eigene Nummer. */
        const val SPECIAL_OHNE_NUMMER_AB = 1000

        fun from(j: JsonValue?) = Item(
            id = j.requiredText("id"),
            libraryId = j["libraryId"].text ?: "",
            kind = j["kind"].text ?: "movie",
            parentId = j["parentId"].text,
            title = j.requiredText("title"),
            originalTitle = j["originalTitle"].text,
            year = j["year"].int,
            seasonNo = j["seasonNo"].int,
            episodeNo = j["episodeNo"].int,
            overview = j["overview"].text,
            ageYears = j["ageYears"].int,
            runtimeMs = j["runtimeMs"].long,
            imdbRating = j["imdbRating"].double,
            imdbVotes = j["imdbVotes"].int,
            rottenTomatoes = j["rottenTomatoes"].int,
            tmdbRating = j["tmdbRating"].int?.takeIf { it > 0 },
            tmdbVotes = j["tmdbVotes"].int,
            special = j["special"].bool ?: false,
            genres = j["genres"].strings(),
            progress = j["progress"].double ?: 0.0,
            finished = j["finished"].bool ?: false,
            myRating = j["myRating"].int ?: 0,
            onList = j["onList"].bool ?: false,
            resumeMs = j["resumeMs"].long ?: 0L,
            poster = j["poster"].text,
            backdrop = j["backdrop"].text,
            frame = j["frame"].text,
            hinzugefuegtVon = j["hinzugefuegtVon"].text,
            gesehen = j["gesehen"].int ?: 0,
            folgen = j["folgen"].int ?: 0,
            neu = j["neu"].bool ?: false,
            neueFolgen = j["neueFolgen"].int ?: 0,
            qualitaet = j["qualitaet"].text,
            hdr = j["hdr"].text,
            kanaele = j["kanaele"].int ?: 0,
            premiered = j["premiered"].text,
            endYear = j["endYear"].int,
            laeuftNoch = j["laeuftNoch"].bool,
            favorite = j["favorite"].bool ?: false,
        )

        /** „1 Std. 58 Min." oder „48 Min." */
        fun duration(ms: Long): String {
            val minutes = Math.round(ms / 60000.0).toInt()
            return if (minutes >= 60) Texte.t(Tx.DAUER_STD_MIN, minutes / 60, minutes % 60)
            else Texte.t(Tx.DAUER_MIN, minutes)
        }
    }
}

data class MediaFile(
    val id: String,
    val itemId: String,
    val path: String,
    val sizeBytes: Long,
    val container: String?,
    val durationMs: Long,
    val width: Int?,
    val height: Int?,
    val videoCodec: String?,
    val hdr: String?,
    val missing: Boolean,
    // Spuren und Kapitel — gefüllt, sobald der Server die Datei ausgemessen
    // hat. Eine ältere Fassung schickt sie nicht; dann sind die Listen leer
    // und der Player bietet schlicht nichts zum Wählen an.
    val tracks: List<Track> = emptyList(),
    val chapters: List<Chapter> = emptyList(),
    // Welcher Teil eines Films die Datei ist („… a.avi", „… b.avi"): 1, 2, 3 …
    // Mehrere Dateien mit Teil 1 sind Fassungen desselben Films.
    // Ein älterer Server schickt das Feld nicht — dann ist jede Datei Teil 1.
    val partNo: Int = 1,
    // Die Fassung, die der Besitzer für den Titel als Standard gewählt hat.
    val preferred: Boolean = false,
    // Defekt: Der Server konnte die Datei nicht ausmessen — „leer",
    // „gesperrt" oder „kaputt". Sie lässt sich nicht abspielen (422
    // datei_defekt) und wird darum weder als Fassung noch als Standard
    // angeboten. Ein älterer Server schickt das Feld nicht.
    val defekt: String? = null,
    // Die Edition aus dem Dateinamen („Director's Cut") — für den Namen
    // der Fassung im Player.
    val edition: String? = null,
) {
    val durationSeconds: Double get() = durationMs / 1000.0
    /** Ob die Datei abspielbar ist: vorhanden und nicht defekt. */
    val nutzbar: Boolean get() = !missing && defekt == null
    val fileName: String get() = path.substringAfterLast('/').substringAfterLast('\\')

    companion object {
        fun from(j: JsonValue?) = MediaFile(
            id = j.requiredText("id"),
            itemId = j["itemId"].text ?: "",
            path = j["path"].text ?: "",
            sizeBytes = j["sizeBytes"].long ?: 0L,
            container = j["container"].text,
            durationMs = j["durationMs"].long ?: 0L,
            width = j["width"].int,
            height = j["height"].int,
            videoCodec = j["videoCodec"].text,
            hdr = j["hdr"].text,
            missing = j["missing"].bool ?: false,
            tracks = j["tracks"].list.map { Track.from(it) },
            chapters = j["chapters"].list.map { Chapter.from(it) },
            partNo = j["partNo"].int?.takeIf { it > 0 } ?: 1,
            preferred = j["preferred"].bool ?: false,
            defekt = j["defekt"].text?.takeIf { it.isNotBlank() },
            edition = j["edition"].text?.takeIf { it.isNotBlank() },
        )
    }
}

// ---------------------------------------------------------------- Spuren

/**
 * Eine Spur in der Datei: Ton oder Untertitel.
 *
 * [index] ist die Nummer, unter der der Server die Spur kennt — die geht
 * als `?spur=` an den Strom und steht im Pfad der Untertitel. Untertitel
 * als eigene Datei neben dem Film ([external]) haben Nummern ab 1000.
 * [bitmap] sind Bild-Untertitel (PGS, DVD): die gibt es nicht als Text,
 * darum bietet die App sie nicht an.
 */
data class Track(
    val index: Int,
    val kind: String,
    val codec: String?,
    val language: String?,
    val title: String?,
    val channels: Int,
    val forced: Boolean,
    val hearingImpaired: Boolean,
    val external: Boolean,
    val bitmap: Boolean,
) {
    val isAudio: Boolean get() = kind == "audio"
    val isSubtitle: Boolean get() = kind == "subtitle"

    /** „Deutsch (erzwungen)", „Englisch (5.1)" — siehe [Spuren.name]. */
    val anzeigename: String get() = Spuren.name(this)

    companion object {
        fun from(j: JsonValue?) = Track(
            index = j["index"].int ?: 0,
            kind = j["kind"].text ?: "audio",
            codec = j["codec"].text,
            language = j["language"].text,
            title = j["title"].text,
            channels = j["channels"].int ?: 0,
            forced = j["forced"].bool ?: false,
            hearingImpaired = j["hearingImpaired"].bool ?: false,
            external = j["external"].bool ?: false,
            bitmap = j["bitmap"].bool ?: false,
        )
    }
}

/** Ein Kapitel der Datei; [kind] ist intro, credits oder normal. */
data class Chapter(
    val index: Int,
    val startMs: Long,
    val endMs: Long,
    val title: String?,
    val kind: String,
) {
    companion object {
        fun from(j: JsonValue?) = Chapter(
            index = j["index"].int ?: 0,
            startMs = j["startMs"].long ?: 0L,
            endMs = j["endMs"].long ?: 0L,
            title = j["title"].text,
            kind = j["kind"].text ?: "normal",
        )
    }
}

/**
 * Wo der Vorspann einer Folge liegt. [quelle] sagt, woher der Server das
 * weiss: „kapitel" aus der Datei selbst, „serie" von einer Markierung, die
 * jemand mit Zugang zur Verwaltung für die ganze Serie gesetzt hat.
 */
data class Vorspann(val vonMs: Long, val bisMs: Long, val quelle: String) {
    // quelle: kapitel | serie | folge | automatisch — gelesen wird jede,
    // auch eine, die erst ein neuerer Server kennt.
    val vonSerie: Boolean get() = quelle == "serie"

    companion object {
        fun from(j: JsonValue?) = Vorspann(
            vonMs = j["vonMs"].long ?: 0L,
            bisMs = j["bisMs"].long ?: 0L,
            quelle = j["quelle"].text ?: "serie",
        )
    }
}

data class ItemDetail(
    val item: Item,
    val files: List<MediaFile>,
    val seasons: List<Item>,
    val sharedRatings: List<SharedRating>,
    // Die Filmreihe, in der dieser Film steht — nur bei Filmen, und nur,
    // wenn die Person mindestens zwei Filme daraus sehen darf.
    val collection: Reihe? = null,
    // Bei Folgen und Filmen: wo der Vorspann liegt, wo der Abspann beginnt
    // — soweit der Server es weiss. Bei einer Folge dazu die Serie, an der
    // ein Verwalter den Vorspann markieren kann.
    val intro: Vorspann? = null,
    val creditsMs: Long? = null,
    val seriesId: String? = null,
    // Wer mitspielt — Bilder über die eigene Bilderroute des Servers.
    val cast: List<Besetzung> = emptyList(),
    // „Ähnliche Titel": gleiche Reihe, geteilte Besetzung, gleiche Genres —
    // nur bei Filmen und Serien, nur was diese Person sehen darf.
    val similar: List<Item> = emptyList(),
    // Vorspann und Abspann je Fassung, nach der Kennung ihrer (ersten)
    // Datei — nur, wenn es mehr als eine gibt. [intro] und [creditsMs]
    // gelten für die erste vorhandene Datei.
    val jeDatei: Map<String, VorspannDatei> = emptyMap(),
) {
    /** Die Datei, die sich abspielen lässt — fehlende übersprungen. */
    // Was „Ansehen" spielt: die erste Fassung (Teil 1, vorhanden, nicht
    // defekt), sonst irgendeine abspielbare Datei — wie im Browser.
    val playable: MediaFile? get() = files.firstOrNull { it.nutzbar && it.partNo == 1 } ?: files.firstOrNull { it.nutzbar }

    /**
     * Vorspann und Abspann der Datei, die wirklich läuft: aus [jeDatei],
     * wenn der Server sie für diese Datei nennt, sonst die Angaben oben.
     */
    fun marken(fileId: String): VorspannDatei =
        jeDatei[fileId] ?: VorspannDatei(intro, creditsMs)

    companion object {
        fun from(j: JsonValue?) = ItemDetail(
            item = Item.from(j.required("item")),
            files = j["files"].list.map { MediaFile.from(it) },
            seasons = j["seasons"].list.map { Item.from(it) },
            sharedRatings = j["sharedRatings"].list.map { SharedRating.from(it) },
            collection = j["collection"]?.let { Reihe.from(it) },
            intro = j["intro"]?.let { Vorspann.from(it) },
            creditsMs = j["creditsMs"].long?.takeIf { it > 0 },
            seriesId = j["seriesId"].text,
            cast = j["cast"].list.map { Besetzung.from(it) },
            similar = j["similar"].list.map { Item.from(it) },
            jeDatei = (j["jeDatei"] as? JsonValue.Obj)?.fields
                ?.mapValues { (_, v) -> VorspannDatei.from(v) } ?: emptyMap(),
        )
    }
}

/** Vorspann und Abspann einer bestimmten Datei ([ItemDetail.jeDatei]). */
data class VorspannDatei(val intro: Vorspann?, val creditsMs: Long?) {
    companion object {
        fun from(j: JsonValue?) = VorspannDatei(
            intro = j["intro"]?.let { Vorspann.from(it) },
            creditsMs = j["creditsMs"].long?.takeIf { it > 0 },
        )
    }
}

/** Eine Person aus der Besetzung: Name, Rolle, Bildpfad beim Server. */
data class Besetzung(val name: String, val rolle: String?, val bild: String?) {
    val initial: String get() = name.take(1).uppercase()

    companion object {
        fun from(j: JsonValue?) = Besetzung(
            name = j["name"].text ?: "",
            rolle = j["rolle"].text,
            bild = j["bild"].text,
        )
    }
}

// ---------------------------------------------------------------- Person

/** Woher die Angaben einer Person stammen: „TMDb", „Wikipedia". */
data class Quelle(val name: String, val url: String) {
    /**
     * Die Adresse, wenn sie als Link taugt — nur http und https. Die
     * Quellen kommen aus der Datenbank des Servers; ein „javascript:" oder
     * „intent:" darf darüber nicht auf das Gerät gelangen. Wie nurHttp()
     * im Browser.
     */
    val sichereUrl: String?
        get() = try {
            val u = java.net.URI(url.trim())
            val schema = u.scheme?.lowercase()
            if ((schema == "http" || schema == "https") && !u.host.isNullOrBlank()) u.toString() else null
        } catch (e: Exception) {
            null
        }

    companion object {
        fun from(j: JsonValue?) = Quelle(
            name = j["name"].text ?: "",
            url = j["url"].text ?: "",
        )
    }
}

/**
 * Die Personenseite — GET /persons/{name}: Foto, Lebensdaten,
 * Biografie und Quellen. Kennt der Server die Person nicht, gibt es keine;
 * dann zeigt die Seite nur, worin sie mitspielt.
 */
data class PersonInfo(
    val name: String,
    val bio: String?,
    val bioSprache: String?,
    val geboren: String?,
    val gestorben: String?,
    val geburtsort: String?,
    val quellen: List<Quelle>,
    val bild: String?,
) {
    /** „geboren 23.12.1968 in Queens · gestorben 1.2.2020" — oder null. */
    val lebensZeile: String?
        get() = listOfNotNull(
            Titelseite.datum(geboren)?.let { d ->
                geburtsort?.takeIf { it.isNotBlank() }?.let { Texte.t(Tx.GEBOREN_IN, d, it) }
                    ?: Texte.t(Tx.GEBOREN, d)
            },
            Titelseite.datum(gestorben)?.let { Texte.t(Tx.GESTORBEN, it) },
        ).joinToString(" · ").ifEmpty { null }

    /** „Quelle: TMDb, Wikipedia" — oder null ohne Quellen. */
    val quellenZeile: String?
        get() = quellen.map { it.name }.filter { it.isNotBlank() }
            .takeIf { it.isNotEmpty() }?.let { Texte.t(Tx.QUELLE, it.joinToString(", ")) }

    /**
     * Nur auf Englisch da: die Fassung in der Sprache der Oberfläche fehlt
     * bei TMDb und Wikipedia. Wer Lumen auf Englisch nutzt, braucht den
     * Hinweis nicht.
     */
    val nurEnglisch: Boolean
        get() = !bio.isNullOrBlank() && !bioSprache.isNullOrBlank() &&
            bioSprache != Texte.locale.language && Texte.locale.language != "en"

    companion object {
        val NUR_ENGLISCH: String get() = Texte.t(Tx.NUR_ENGLISCH)

        fun from(j: JsonValue?) = PersonInfo(
            name = j["name"].text ?: "",
            bio = j["bio"].text?.trim()?.takeIf { it.isNotEmpty() },
            bioSprache = j["bioSprache"].text,
            geboren = j["geboren"].text,
            gestorben = j["gestorben"].text,
            geburtsort = j["geburtsort"].text,
            quellen = j["quellen"].list.map { Quelle.from(it) },
            bild = j["bild"].text,
        )
    }
}

// ---------------------------------------------------------------- Suche

/** Eine Person aus der Besetzung, die zur Suche passt — mit der Zahl der
 *  Titel, in denen sie vorkommt, und einem Bild, falls der Server eines hat. */
data class PersonTreffer(
    val name: String,
    val anzahl: Int,
    val itemId: String?,
    val bild: String?,
) {
    val initial: String get() = name.take(1).uppercase()

    /** „1 Titel" oder „7 Titel" */
    val anzahlText: String get() = Texte.n(TxN.TITEL_ANZAHL, anzahl)

    companion object {
        fun from(j: JsonValue?) = PersonTreffer(
            name = j.requiredText("name"),
            anzahl = j["anzahl"].int ?: 0,
            itemId = j["itemId"].text,
            bild = j["bild"].text,
        )
    }
}

/**
 * Wie der Server die Eingabe gedeutet hat: „Komödie 90er" wird zu Genre
 * Komödie und den Jahren 1990 bis 1999, „Filme mit Tom Hanks" zu einer
 * Person. [text] ist, was übrig blieb und als Titel gesucht wurde.
 */
data class Deutung(
    val text: String?,
    val genre: String?,
    val person: String?,
    val jahrVon: Int?,
    val jahrBis: Int?,
) {
    /** Ob der Server mehr herausgelesen hat als blossen Text. */
    val gedeutet: Boolean get() = person != null || genre != null || jahrVon != null

    /** „mit Tom Hanks · Komödie · 1990–1999 · „Bucht"" — in dieser Reihenfolge. */
    val beschreibung: String
        get() = buildList {
            person?.let { add(Texte.t(Tx.MIT_PERSON, it)) }
            genre?.let { add(it) }
            jahrVon?.let { von ->
                add(if (jahrBis == null || jahrBis == von) von.toString() else "$von–$jahrBis")
            }
            text?.takeIf { it.isNotBlank() }?.let { add(Texte.t(Tx.ZITAT, it)) }
        }.joinToString(" · ")

    companion object {
        fun from(j: JsonValue?) = Deutung(
            text = j["text"].text,
            genre = j["genre"].text,
            person = j["person"].text,
            jahrVon = j["jahrVon"].int,
            jahrBis = j["jahrBis"].int,
        )
    }
}

/** Was GET /search zurückgibt: Titel, Personen, Genres und die Deutung. */
data class Suchergebnis(
    val items: List<Item>,
    val personen: List<PersonTreffer>,
    val genres: List<String>,
    val deutung: Deutung?,
    // Filmreihen und eigene Sammlungen, deren Name passt — „Harry Potter"
    // findet die Reihe, nicht nur die Filme. Fehlen bei älteren Servern.
    val reihen: List<Reihe> = emptyList(),
    val sammlungen: List<Sammlung> = emptyList(),
) {
    val leer: Boolean
        get() = items.isEmpty() && personen.isEmpty() && genres.isEmpty() &&
            reihen.isEmpty() && sammlungen.isEmpty()

    /** Gibt es Filmreihen oder Sammlungen für die Zeile ganz oben? */
    val hatReihenOderSammlungen: Boolean get() = reihen.isNotEmpty() || sammlungen.isNotEmpty()

    /** „12 Treffer · mit Tom Hanks · 1990–1999" — die Zeile über den Titeln. */
    val trefferZeile: String
        get() {
            val treffer = Texte.n(TxN.TREFFER, items.size)
            val d = deutung
            return if (d != null && d.gedeutet) "$treffer · ${d.beschreibung}" else treffer
        }

    companion object {
        fun from(j: JsonValue?) = Suchergebnis(
            items = j["items"].list.map { Item.from(it) },
            personen = j["personen"].list.map { PersonTreffer.from(it) },
            genres = j["genres"].strings(),
            deutung = j["deutung"]?.let { Deutung.from(it) },
            // Beiwerk: eine Reihe ohne Kennung soll nicht die ganze Suche
            // scheitern lassen — sie fällt weg.
            reihen = j["reihen"].list.mapNotNull { r -> oderNullJson { Reihe.from(r) } },
            sammlungen = j["sammlungen"].list.mapNotNull { x -> oderNullJson { Sammlung.from(x) } },
        )
    }
}

// ---------------------------------------------------------------- Filmreihen

/**
 * Eine Filmreihe: Filme, die zusammengehören, in ihrer Reihenfolge.
 *
 * [anzahl] zählt, was diese Person sehen darf — nicht, was in der Reihe
 * steht. Sonst hiesse es „5 Filme" über einer Reihe, die drei zeigt.
 */
data class Reihe(
    val id: String,
    val name: String,
    val overview: String?,
    val source: String,
    val hidden: Boolean,
    val anzahl: Int,
    val items: List<Item>,
    // Die Bilder der Reihe selbst (von TMDb) als Pfad beim Server — oder
    // null; dann steht das Poster des ersten Films für die Reihe.
    val poster: String? = null,
    val backdrop: String? = null,
) {
    /** „1 Film" oder „3 Filme" */
    val anzahlText: String get() = Texte.n(TxN.FILME_ANZAHL, anzahl)

    /** Wie viele Filme der Reihe dieses Profil schon gesehen hat — nur die eigenen. */
    val gesehen: Int get() = items.count { it.finished }

    /** Womit es weitergeht: der erste Film, der noch nicht gesehen ist, sonst der erste. */
    val naechster: Item? get() = items.firstOrNull { !it.finished } ?: items.firstOrNull()

    /** „Ansehen" oder „Weiter mit Teil 2" — wie im Browser. */
    val abspielText: String
        get() {
            val n = naechster ?: return Texte.t(Tx.ANSEHEN)
            return if (gesehen in 1 until anzahl) Texte.t(Tx.WEITER_MIT, n.title) else Texte.t(Tx.ANSEHEN)
        }

    /** „3 Filme · 1 gesehen · zusammengestellt nach TMDb" */
    val unterzeile: String
        get() = buildList {
            add(anzahlText)
            if (gesehen > 0) add(Texte.t(Tx.N_GESEHEN, gesehen))
            // Woher die Reihe stammt — wie im Browser (app.js, reiheHerkunft).
            add(when (source) {
                "tmdb" -> Texte.t(Tx.REIHE_TMDB)
                "nfo" -> Texte.t(Tx.REIHE_NFO)
                "emby" -> Texte.t(Tx.REIHE_EMBY)
                else -> Texte.t(Tx.REIHE_LUMEN)
            })
        }.joinToString(" · ")

    companion object {
        fun from(j: JsonValue?) = Reihe(
            id = j.requiredText("id"),
            name = j.requiredText("name"),
            overview = j["overview"].text,
            source = j["source"].text ?: "auto",
            hidden = j["hidden"].bool ?: false,
            anzahl = j["anzahl"].int ?: 0,
            items = j["items"].list.map { Item.from(it) },
            poster = j["poster"].text?.takeIf { it.isNotBlank() },
            backdrop = j["backdrop"].text?.takeIf { it.isNotBlank() },
        )
    }
}

// ---------------------------------------------------------------- Sammlungen

/**
 * Eine eigene Sammlung (GET /sammlungen, /sammlungen/{id}): von der
 * Verwaltung zusammengestellt — „Weihnachtsfilme", „Marvel in
 * Reihenfolge". Jedes Profil sieht darin nur, was es ohnehin sehen darf.
 *
 * In der Übersicht und in der Suche stehen nur die ersten Titel in
 * [items] (fürs Bild), [anzahl] zählt alle. [bild] ist ein eigenes Bild als
 * Pfad beim Server; fehlt es, steht das Poster des ersten Titels da.
 */
data class Sammlung(
    val id: String,
    val name: String,
    val beschreibung: String?,
    val bild: String?,
    val anzahl: Int,
    val items: List<Item>,
) {
    /** „1 Titel" oder „5 Titel" — Filme und Serien gemischt. */
    val anzahlText: String get() = Texte.n(TxN.TITEL_ANZAHL, anzahl)

    companion object {
        fun from(j: JsonValue?): Sammlung {
            val items = j["items"].list.map { Item.from(it) }
            return Sammlung(
                id = j.requiredText("id"),
                name = j.requiredText("name"),
                beschreibung = j["beschreibung"].text?.takeIf { it.isNotBlank() },
                bild = j["bild"].text?.takeIf { it.isNotBlank() },
                anzahl = j["anzahl"].int ?: items.size,
                items = items,
            )
        }
    }
}

// ---------------------------------------------------------------- Bildschirmzeit

/**
 * Wie es um die Bildschirmzeit des eigenen Profils steht
 * (GET /me/bildschirmzeit). [aktiv] nur bei Kinderprofilen mit Regel; die
 * Grenze zieht der Server, die Apps zeigen nur, wie viel bleibt.
 */
data class Bildschirmzeit(
    val aktiv: Boolean,
    val erlaubt: Boolean,
    val genutztMinuten: Int,
    val budgetMinuten: Int?,
    val restMinuten: Int?,
    val von: String?,
    val bis: String?,
    val meldung: String?,
) {
    /**
     * „Heute noch 25 Min.", „Heute ohne Grenze" oder der Satz des Servers,
     * wenn für heute Schluss ist — wie im Browser.
     */
    val hauptzeile: String
        get() = when {
            !erlaubt -> meldung ?: Texte.t(Tx.BZ_SCHLUSS)
            restMinuten != null -> Texte.t(Tx.BZ_HEUTE_NOCH, restMinuten)
            else -> Texte.t(Tx.BZ_OHNE_GRENZE)
        }

    /** „35 von 60 Min. gebraucht · heute 16:00 bis 19:00" — oder null. */
    val nebenzeile: String?
        get() = buildList {
            budgetMinuten?.let { add(Texte.t(Tx.BZ_GEBRAUCHT, genutztMinuten, it)) }
            if (von != null && bis != null) add(Texte.t(Tx.BZ_FENSTER, von, bis))
        }.joinToString(" · ").ifEmpty { null }

    companion object {
        fun from(j: JsonValue?) = Bildschirmzeit(
            aktiv = j["aktiv"].bool ?: false,
            erlaubt = j["erlaubt"].bool ?: false,
            genutztMinuten = j["genutztMinuten"].int ?: 0,
            budgetMinuten = j["budgetMinuten"].int?.takeIf { it > 0 },
            restMinuten = j["restMinuten"].int,
            von = j["von"].text?.takeIf { it.isNotBlank() },
            bis = j["bis"].text?.takeIf { it.isNotBlank() },
            meldung = j["meldung"].text?.takeIf { it.isNotBlank() },
        )
    }
}

// ---------------------------------------------------------------- Listen

/** Wer eine Liste angelegt hat — Name und Bild, mehr nicht. */
data class ListenPerson(val id: String, val name: String, val avatar: String) {
    val initial: String get() = name.take(1).uppercase()

    companion object {
        fun from(j: JsonValue?) = ListenPerson(
            id = j["id"].text ?: "",
            name = j["name"].text ?: "",
            avatar = j["avatar"].text ?: "av-a",
        )
    }
}

/**
 * Eine Liste, die jemand selbst zusammenstellt.
 *
 * Geteilt wird sie nur, wenn ihr Besitzer das ausdrücklich will. Was die
 * anderen schauen, steht hier nirgends — nur, was in der Liste liegt.
 *
 * [enthaelt] gibt es nur bei der Abfrage zu einem Titel, [vorschau] nur in
 * der Übersicht, [items] nur bei einer einzelnen Liste.
 */
data class Liste(
    val id: String,
    val name: String,
    val von: ListenPerson,
    val meine: Boolean,
    val geteilt: Boolean,
    val gemeinsam: Boolean,
    val darfAendern: Boolean,
    val anzahl: Int,
    val erstelltAm: String,
    val enthaelt: Boolean,
    val vorschau: List<Item>,
    val items: List<Item>,
) {
    /** „1 Titel" oder „4 Titel" */
    val anzahlText: String get() = Texte.n(TxN.TITEL_ANZAHL, anzahl)

    companion object {
        fun from(j: JsonValue?) = Liste(
            id = j.requiredText("id"),
            name = j.requiredText("name"),
            von = ListenPerson.from(j["von"]),
            meine = j["meine"].bool ?: false,
            geteilt = j["geteilt"].bool ?: false,
            gemeinsam = j["gemeinsam"].bool ?: false,
            darfAendern = j["darfAendern"].bool ?: false,
            anzahl = j["anzahl"].int ?: 0,
            erstelltAm = j["erstelltAm"].text ?: "",
            enthaelt = j["enthaelt"].bool ?: false,
            vorschau = j["vorschau"].list.map { Item.from(it) },
            items = j["items"].list.map { Item.from(it) },
        )
    }
}

data class SharedRating(val name: String, val avatar: String, val stars: Int) {
    companion object {
        fun from(j: JsonValue?) = SharedRating(
            name = j["name"].text ?: "",
            avatar = j["avatar"].text ?: "av-a",
            stars = j["stars"].int ?: 0,
        )
    }
}

data class HomeRow(val key: String, val title: String, val wide: Boolean, val items: List<Item>) {
    companion object {
        fun from(j: JsonValue?) = HomeRow(
            key = j["key"].text ?: "",
            title = j["title"].text ?: "",
            wide = j["wide"].bool ?: false,
            items = j["items"].list.map { Item.from(it) },
        )
    }
}

/**
 * Eine Seite eines Rasters — GET /items?seite=1: die Titel, wie viele es
 * insgesamt sind, ob es weitergeht, und die Genres für die Filterknöpfe.
 * Die Genres gelten für alle Titel der Art, nicht nur die gefilterten —
 * sonst verschwänden die anderen Knöpfe, sobald einer gedrückt ist.
 */
data class Seite(
    val items: List<Item>,
    val gesamt: Int,
    val offset: Int,
    val weiter: Boolean,
    val genres: List<String>,
    /** Bei sort=random der Samen dieser Mischung — fürs Nachladen. */
    val seed: Long? = null,
) {
    /** Die nächste Seite hinten anhängen; was schon da ist, kommt nicht doppelt. */
    fun mit(naechste: Seite): Seite {
        val schon = items.mapTo(HashSet()) { it.id }
        return copy(
            items = items + naechste.items.filter { it.id !in schon },
            gesamt = naechste.gesamt,
            weiter = naechste.weiter,
            genres = naechste.genres.ifEmpty { genres },
        )
    }

    /** Ab wo die nächste Seite beginnt. */
    val naechsterOffset: Int get() = offset + items.size

    /** „124 Titel" oder „12 Titel · Komödie" */
    fun zeile(genre: String?): String =
        Texte.n(TxN.TITEL_ANZAHL, gesamt) + if (genre.isNullOrBlank()) "" else " · $genre"

    companion object {
        fun from(j: JsonValue?) = Seite(
            items = j["items"].list.map { Item.from(it) },
            gesamt = j["gesamt"].int ?: 0,
            offset = j["offset"].int ?: 0,
            weiter = j["weiter"].bool ?: false,
            genres = j["genres"].strings(),
            seed = j["seed"].long,
        )
    }
}

/**
 * Ein Gerät, auf dem dieses Konto angemeldet ist — GET /me/devices. Nur
 * die eigenen; was andere wo angemeldet haben, kommt nicht vom Server.
 */
data class Device(
    val name: String,
    val kind: String,
    val createdAt: String,
    val lastSeen: String,
) {
    /** „Fernseher", „Handy", „Tablet", „Browser" — sonst, wie der Server es nennt. */
    val artText: String
        get() = when (kind) {
            "tv" -> Texte.t(Tx.GERAET_TV)
            "phone" -> Texte.t(Tx.GERAET_HANDY)
            "tablet" -> Texte.t(Tx.GERAET_TABLET)
            "browser", "web" -> Texte.t(Tx.GERAET_BROWSER)
            "mac" -> "Mac"
            "" -> Texte.t(Tx.GERAET)
            else -> kind
        }

    companion object {
        fun from(j: JsonValue?) = Device(
            name = j["name"].text ?: "",
            kind = j["kind"].text ?: "",
            createdAt = j["createdAt"].text ?: "",
            lastSeen = j["lastSeen"].text ?: "",
        )
    }
}

/**
 * Eine laufende Wiedergabe im eigenen Profil, wie „Ich → Geräte" sie zeigt:
 * auf welchem Gerät, ob direkt oder umgewandelt, und die technische Last
 * („4K hevc → 1080p h264"). Kein Titel — auch nicht für sich selbst: Der
 * Server gibt ihn hier nicht heraus, und so steht auf einem geteilten
 * Fernseher nicht, was jemand schaut.
 */
data class EigeneWiedergabe(
    val id: String,
    val deviceName: String,
    val deviceKind: String,
    val mode: String,
    val detail: String,
) {
    val wirdUmgewandelt: Boolean get() = mode == "transcode"

    /** „direkt · 1080p h264" oder „wird umgewandelt · 4K hevc → 1080p h264". */
    val zeile: String
        get() {
            val art = if (wirdUmgewandelt) Texte.t(Tx.WIEDERGABE_UMGEWANDELT) else Texte.t(Tx.WIEDERGABE_DIREKT)
            return if (detail.isBlank()) art else "$art · $detail"
        }

    /** Der Gerätename, zur Not die Art des Geräts. */
    val geraet: String
        get() = deviceName.ifBlank { Device(name = "", kind = deviceKind, createdAt = "", lastSeen = "").artText }

    companion object {
        fun from(j: JsonValue?) = EigeneWiedergabe(
            id = j.requiredText("id"),
            deviceName = j["deviceName"].text ?: "",
            deviceKind = j["deviceKind"].text ?: "",
            mode = j["mode"].text ?: "direct",
            detail = j["detail"].text ?: "",
        )
    }
}

/**
 * Der Medienschlüssel, wie GET /me/media-key ihn ausgibt. [gueltigSek] ist
 * die Restzeit beim Ausgeben; [gueltigBis] steht nur der Vollständigkeit
 * halber dabei — gerechnet wird mit der Dauer, siehe [ApiClient.medienSchluessel].
 */
data class Medienschluessel(val key: String, val gueltigBis: String, val gueltigSek: Long) {
    companion object {
        fun from(j: JsonValue?) = Medienschluessel(
            key = j.requiredText("key"),
            gueltigBis = j["gueltigBis"].text ?: "",
            // Ohne Angabe die 6 Stunden, die der Server einem frischen gibt.
            gueltigSek = j["gueltigSek"].long ?: (6 * 60 * 60L),
        )
    }
}

// ---------------------------------------------------------------- Neuigkeiten

/**
 * Eine Neuigkeit: neue Folgen einer Serie, die ich schaue oder mir gemerkt
 * habe ([art] „folgen"), ein neuer Film in einer Reihe, aus der ich schon
 * etwas gesehen habe („reihe"), oder was diese Woche dazukam („neu").
 * Der Server stellt sie allein aus dem eigenen Stand zusammen — nie aus
 * dem der anderen. [at] ist ein Zeitpunkt nach RFC 3339.
 */
data class Neuigkeit(
    val art: String,
    val itemId: String?,
    val titel: String,
    val text: String,
    val at: String,
    val anzahl: Int,
    // Bei „{n} neue Titel" die Titel selbst (jüngste zuerst, höchstens so
    // viele, wie der Server mitschickt) — damit die Zeile aufklappen kann,
    // statt zur Startseite zu springen. Ein älterer Server schickt nichts.
    val titelListe: List<Item> = emptyList(),
) {
    /** Ob die Zeile aufklappt (kein einzelner Titel, aber eine Liste) — wie im Browser. */
    val klappbar: Boolean get() = itemId == null && titelListe.isNotEmpty()

    /** Wie viele neue Titel nicht in der Liste stehen („und 4 weitere …"). */
    val weitere: Int get() = maxOf(0, anzahl - titelListe.size)

    companion object {
        fun from(j: JsonValue?) = Neuigkeit(
            art = j["art"].text ?: "neu",
            itemId = j["itemId"].text?.takeIf { it.isNotEmpty() },
            titel = j["titel"].text ?: "",
            text = j["text"].text ?: "",
            at = j["at"].text ?: "",
            anzahl = j["anzahl"].int ?: 0,
            titelListe = j["titelListe"].list.mapNotNull { runCatching { Item.from(it) }.getOrNull() },
        )
    }
}

/** Was GET /news zurückgibt: die Einträge und bis wann sie gelesen sind. */
data class Neuigkeiten(
    val eintraege: List<Neuigkeit>,
    val ungelesen: Int,
    val gelesenBis: String?,
) {
    /** Ob ein Eintrag nach dem letzten Öffnen dazukam. */
    fun istNeu(n: Neuigkeit): Boolean = Zeitpunkt.nach(n.at, gelesenBis)

    companion object {
        fun from(j: JsonValue?) = Neuigkeiten(
            eintraege = j["eintraege"].list.map { Neuigkeit.from(it) },
            ungelesen = j["ungelesen"].int ?: 0,
            gelesenBis = j["gelesenBis"].text,
        )
    }
}

// ---------------------------------------------------------------- Wiedergabe

data class PlaybackPlan(
    val direct: Boolean,
    val reason: String?,
    val videoCodec: String,
    val height: Int,
    val bitrate: Int,
) {
    companion object {
        fun from(j: JsonValue?) = PlaybackPlan(
            direct = j["direct"].bool ?: false,
            reason = j["reason"].text,
            videoCodec = j["videoCodec"].text ?: "h264",
            height = j["height"].int ?: 0,
            bitrate = j["bitrate"].int ?: 0,
        )
    }
}

// ---------------------------------------------------------------- Wünsche

data class Wish(
    val id: String,
    val by: String,
    val avatar: String,
    val title: String,
    val year: Int?,
    val state: String,
    val mine: Boolean,
    // Dieses Profil hat sich einem fremden Wunsch angeschlossen; von wem
    // er ist, steht dann nicht da.
    val dabei: Boolean = false,
    // Nur in der Antwort aufs Wünschen: den Wunsch gab es schon (eigen
    // oder angeschlossen) — es ist kein neuer entstanden.
    val vorhanden: Boolean = false,
) {
    /**
     * Was nach dem Wünschen gemeldet wird — wie im Browser (app.js,
     * wunschVerhalten.gewuenscht): angeschlossen, schon eigen oder neu.
     */
    val rueckmeldung: String
        get() = when {
            dabei -> Texte.t(Tx.WUNSCH_SCHON_DABEI)
            vorhanden -> Texte.t(Tx.WUNSCH_SCHON_EIGEN)
            else -> Texte.t(Tx.WUNSCH_GESCHICKT)
        }

    /** Was sich mit dem Wunsch tun lässt: den eigenen offenen löschen, einen angeschlossenen verlassen. */
    val wegArt: WunschWeg?
        get() = when {
            dabei -> WunschWeg.AUSTRETEN
            mine && state == "open" -> WunschWeg.LOESCHEN
            else -> null
        }

    val stateLabel: String
        get() = when (state) {
            "open" -> Texte.t(Tx.WUNSCH_OFFEN)
            "working" -> Texte.t(Tx.WUNSCH_IN_ARBEIT)
            "done" -> Texte.t(Tx.WUNSCH_ERFUELLT)
            "declined" -> Texte.t(Tx.WUNSCH_ABGELEHNT)
            else -> state
        }

    /** „von Mara" oder „von Alex (du)" */
    val vonText: String get() = if (mine) Texte.t(Tx.VON_X_DU, by) else Texte.t(Tx.VON_X, by)

    /** „Dune (2021)" */
    val titelZeile: String get() = title + (year?.takeIf { it > 0 }?.let { " ($it)" } ?: "")

    companion object {
        fun from(j: JsonValue?) = Wish(
            id = j.requiredText("id"),
            by = j["by"].text ?: "",
            avatar = j["avatar"].text ?: "av-a",
            title = j.requiredText("title"),
            year = j["year"].int,
            state = j["state"].text ?: "open",
            mine = j["mine"].bool ?: false,
            dabei = j["dabei"].bool ?: false,
            vorhanden = j["vorhanden"].bool ?: false,
        )
    }
}

/** Löschen (eigener offener Wunsch) oder austreten (angeschlossen) — beides `DELETE /wishes/{id}`. */
enum class WunschWeg { LOESCHEN, AUSTRETEN }
