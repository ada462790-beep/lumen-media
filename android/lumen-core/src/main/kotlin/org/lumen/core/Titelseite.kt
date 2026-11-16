// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package org.lumen.core

/**
 * Was Telefon und Fernseher auf der Titelseite, beim Einlösen einer
 * Einladung und in den Einstellungen gleich sagen sollen — dieselben
 * Regeln wie im Browser (app.js: viewDetail, viewRedeem, viewSettings).
 *
 * Steht hier und nicht in den Oberflächen, damit beide Apps dasselbe
 * anzeigen und Tests.kt jeden Fall ohne Gerät nachstellen kann.
 */
object Titelseite {

    /** Ab so viel gespeichertem Stand heisst der Knopf „Weiter bei …" — und es gibt „Von vorn". */
    const val WEITER_AB_SEKUNDEN = 60.0

    /** „🍅 93 % Rotten Tomatoes" — ab 60 % gilt ein Titel als frisch (wie im Browser). */
    fun tomaten(prozent: Int): String = (if (tomateFrisch(prozent)) "🍅" else "🟢") + " " + Texte.t(Tx.TOMATEN, prozent)

    /** Ab so viel Prozent guter Kritiken ist ein Titel bei Rotten Tomatoes „frisch". */
    const val TOMATE_FRISCH_AB = 60

    fun tomateFrisch(prozent: Int): Boolean = prozent >= TOMATE_FRISCH_AB

    /** Welche Wertung: IMDb, eine Note ohne Quelle (aus einer NFO), Rotten Tomatoes, TMDb. */
    enum class WertungArt { IMDB, NOTE, TOMATE_FRISCH, TOMATE_FAUL, TMDB }

    /**
     * Eine Wertung in der eigenen Reihe unter dem Titel: [wert] gross
     * („7,8", „93 %"), [zusatz] klein (Stimmen, „Rotten Tomatoes"),
     * [beschreibung] für Vorleseprogramme.
     */
    data class Wertung(val art: WertungArt, val wert: String, val zusatz: String?, val beschreibung: String)

    /**
     * Die Wertungen eines Titels in der Reihenfolge des Browsers
     * (wertungsReihe): IMDb, Rotten Tomatoes, TMDb. [neutral]: Die Note
     * stammt aus einer NFO ohne Quelle (bewertungQuelle „nfo") — dann ohne
     * IMDb-Schriftzug, nur „★ 7,4".
     */
    fun wertungen(item: Item, neutral: Boolean = false): List<Wertung> = buildList {
        item.imdbRating?.takeIf { it > 0 }?.let { note ->
            val n = komma(note)
            if (neutral) {
                add(Wertung(WertungArt.NOTE, n, null, Texte.t(Tx.WERTUNG_VON_ZEHN, n)))
            } else {
                val stimmen = item.imdbVotes?.takeIf { it > 0 }?.let { tausender(it) }
                add(Wertung(WertungArt.IMDB, n, stimmen,
                    if (stimmen != null) Texte.t(Tx.IMDB_VON_ZEHN_STIMMEN, n, stimmen) else Texte.t(Tx.IMDB_VON_ZEHN, n)))
            }
        }
        item.rottenTomatoes?.takeIf { it in 0..100 }?.let { p ->
            val frisch = tomateFrisch(p)
            add(Wertung(if (frisch) WertungArt.TOMATE_FRISCH else WertungArt.TOMATE_FAUL, "$p %", "Rotten Tomatoes",
                Texte.t(if (frisch) Tx.TOMATEN_FRISCH else Tx.TOMATEN_FAUL, p)))
        }
        item.tmdbRating?.takeIf { it > 0 }?.let { p ->
            add(Wertung(WertungArt.TMDB, "$p %", null, Texte.t(Tx.TMDB_WERTUNG, p)))
        }
    }

    /**
     * Die ruhige Eckdatenzeile unter den Wertungen: „2019 · ab 12 · 1 Std.
     * 58 Min. · Drama · Thriller" und bei einer Serie der eigene Stand.
     * Die Wertungen stehen eigens darüber ([wertungen]) — vorher suchte man
     * die Note in einer langen grauen Zeile (wie im Browser geändert).
     */
    fun eckdaten(item: Item): String = buildList {
        item.jahresText?.let { add(it) }
        item.ageYears?.let { add(Texte.t(Tx.AB_ALTER, it)) }
        item.runtimeMs?.takeIf { it > 0 }?.let { add(Item.duration(it)) }
        if (item.genres.isNotEmpty()) add(item.genres.joinToString(" · "))
        item.serienStand?.let { add(it) }
    }.joinToString(" · ")

    /**
     * Die Zeile unter dem Titel samt Wertungen in einem Satz: „2019 · ab 12 ·
     * 1 Std. 58 Min. · Drama · IMDb 7,8 (12 345)" — für Stellen ohne Platz
     * für eine eigene Wertungsreihe.
     */
    fun metaZeile(item: Item): String = buildList {
        // Bei Serien der Zeitraum: „seit 2014", „2014–2020".
        item.jahresText?.let { add(it) }
        item.ageYears?.let { add(Texte.t(Tx.AB_ALTER, it)) }
        item.runtimeMs?.takeIf { it > 0 }?.let { add(Item.duration(it)) }
        if (item.genres.isNotEmpty()) add(item.genres.joinToString(" · "))
        item.imdbRating?.takeIf { it > 0 }?.let { note ->
            val stimmen = item.imdbVotes?.takeIf { it > 0 }?.let { " (${tausender(it)})" } ?: ""
            add("IMDb " + komma(note) + stimmen)
        }
        item.rottenTomatoes?.let { add(tomaten(it)) }
        // TMDb neben IMDb und Rotten Tomatoes, wie im Browser (wertungsReihe).
        item.tmdbRating?.let { add(Texte.t(Tx.TMDB_WERTUNG, it)) }
        // Bei einer Serie der eigene Stand — nur der eigene.
        item.serienStand?.let { add(it) }
    }.joinToString(" · ")

    /** Die Note auf einer Kachel: „★ 6,8" — genau eine Nachkommastelle, null ohne Note. */
    fun kachelNote(item: Item): String? = item.imdbRating?.takeIf { it > 0 }?.let { "★ " + komma(it) }

    /** „S1 F3" (en: „S1 E3") — bei einem Special ohne eigene Nummer „Special". */
    fun folgenKennungKurz(folge: Item): String {
        if (folge.specialOhneNummer) return Texte.t(Tx.SPECIAL)
        val s = folge.seasonNo ?: 0
        val f = folge.episodeNo ?: return ""
        return Texte.t(Tx.STAFFEL_FOLGE_KURZ, s, f)
    }

    /**
     * „Staffel 1, Folge 2" — bei einem Special ohne eigene Nummer schlicht
     * „Special" (wie im Browser). Für Player, Downloads und „Als Nächstes".
     */
    fun folgenKennung(folge: Item): String {
        if (folge.specialOhneNummer) return Texte.t(Tx.SPECIAL)
        return listOfNotNull(
            folge.seasonNo?.let { Texte.t(Tx.STAFFEL_N, it) },
            folge.episodeNo?.let { Texte.t(Tx.FOLGE_N, it) },
        ).joinToString(", ")
    }

    /** Ob es lohnt, „Weiter bei …" und „Von vorn" anzubieten. */
    fun hatStand(item: Item): Boolean = item.resumeSeconds > WEITER_AB_SEKUNDEN

    /** „Weiter bei 1:12:04" oder „Ansehen". */
    fun abspielText(item: Item): String =
        if (hatStand(item)) Texte.t(Tx.WEITER_BEI, clock(item.resumeSeconds)) else Texte.t(Tx.ANSEHEN)

    /**
     * Der Knopf einer Serie: „Weiterschauen", sobald dieses Profil eine
     * Folge gesehen oder angefangen hat, sonst „Ansehen".
     */
    fun serienText(serie: Item, folgen: List<Item> = emptyList()): String =
        if (serie.gesehen > 0 || serie.progress > 1 || folgen.any { it.finished || it.progress > 1 })
            Texte.t(Tx.WEITERSCHAUEN) else Texte.t(Tx.ANSEHEN)

    /** Der Knopf einer Folge in der Liste: „Weiter" bei angefangener, sonst „Ansehen". */
    fun folgenKnopf(folge: Item): String =
        if (folge.progress > 1 && !folge.finished) Texte.t(Tx.WEITER) else Texte.t(Tx.ANSEHEN)

    /** Unter dem Namen einer Folge: „48 Min. · 14.4.2019". */
    fun folgenZeile(folge: Item): String = listOfNotNull(
        folge.runtimeMs?.takeIf { it > 0 }?.let { Item.duration(it) },
        datum(folge.premiered),
    ).joinToString(" · ")

    /**
     * Eine Datei, wie die Titelseite sie zeigt: „1080p · hevc · HDR10 ·
     * 4,2 GB" — und „fehlt", wenn sie nicht mehr da ist. Der Name steht
     * daneben; den Pfad zeigt keine App.
     */
    fun dateiZeile(f: MediaFile): String = buildList {
        f.height?.takeIf { it > 0 }?.let { add("${it}p") }
        f.videoCodec?.takeIf { it.isNotBlank() }?.let { add(it) }
        f.hdr?.takeIf { it.isNotBlank() }?.let { add(it) }
        if (f.sizeBytes > 0) add(komma(f.sizeBytes / 1e9) + " GB")
        if (f.missing) add(Texte.t(Tx.DATEI_FEHLT))
        else if (f.defekt != null) add(Texte.t(Tx.DATEI_DEFEKT))
    }.joinToString(" · ")

    /**
     * Warum es kein „Ansehen" gibt, wenn alle vorhandenen Dateien defekt
     * sind — der Browser sagt es statt schwarz zu bleiben. Null, wenn sich
     * etwas abspielen lässt oder keine Datei da ist.
     */
    fun nichtAbspielbar(d: ItemDetail): String? =
        if (d.playable == null && d.files.any { !it.missing && it.defekt != null }) Texte.t(Tx.DATEI_DEFEKT_SATZ) else null

    /**
     * „2019-04-14" wird „14.4.2019" (de), „4/14/2019" (en-US) oder
     * „14/04/2019" (en-GB) — das Muster kommt mit der Sprache. Unlesbares
     * fällt weg.
     */
    fun datum(text: String?): String? {
        val t = text?.trim()?.take(10) ?: return null
        val m = Regex("""^(\d{4})-(\d{2})-(\d{2})$""").find(t) ?: return null
        val (j, mo, d) = m.destructured
        val tag = try {
            java.time.LocalDate.of(j.toInt(), mo.toInt(), d.toInt())
        } catch (e: java.time.DateTimeException) {
            return null
        }
        return java.time.format.DateTimeFormatter.ofPattern(Texte.t(Tx.MUSTER_DATUM), Texte.locale).format(tag)
    }

    /** Eine Zahl mit einer Nachkommastelle: 7,8 auf Deutsch, 7.8 auf Englisch. */
    fun komma(x: Double): String =
        // Erst auf eine Stelle runden wie der Browser (Math.round(x * 10) / 10),
        // dann schreiben: 6.799 → „6,8", 6.75 → „6,8", nie „6,799".
        "%.1f".format(java.util.Locale.ROOT, Math.round(x * 10) / 10.0).replace(".", Texte.t(Tx.DEZIMALZEICHEN))

    /** 12345 wird „12’345" (de-CH), „12.345" (de-DE) oder „12,345" (en). */
    fun tausender(n: Int): String {
        val s = n.toString()
        val trenner = Texte.t(Tx.TAUSENDERZEICHEN)
        val out = StringBuilder()
        s.forEachIndexed { i, c ->
            if (i > 0 && (s.length - i) % 3 == 0) out.append(trenner)
            out.append(c)
        }
        return out.toString()
    }
}

/**
 * Eine Einladung einlösen: was der Browser vorab prüft, bevor er den Code
 * zum Server schickt — sonst wäre ein verbrauchter Code die Strafe für
 * einen zu kurzen PIN.
 */
object Einladung {
    /** Die Arten, sich anzumelden, mit dem Namen auf dem Knopf. */
    val ARTEN: List<Pair<String, String>>
        get() = listOf("password" to Texte.t(Tx.PASSWORT), "pin" to Texte.t(Tx.PIN), "none" to Texte.t(Tx.OHNE))

    /** Nur die Schlüssel, ohne Namen — zum Prüfen. */
    val SCHLUESSEL = listOf("password", "pin", "none")

    /** Leerzeichen aus dem Abtippen weg; Gross- und Kleinschreibung bleibt. */
    fun codeSaubern(code: String): String = code.trim().replace(" ", "")

    /** Null, wenn alles passt — sonst, was nicht stimmt, in einem Satz. */
    fun pruefen(code: String, name: String, lockKind: String, secret: String): String? = when {
        codeSaubern(code).isEmpty() -> Texte.t(Tx.EINLADUNG_OHNE_CODE)
        name.isBlank() -> Texte.t(Tx.WIE_HEISST_DU)
        lockKind !in SCHLUESSEL -> Texte.t(Tx.UNBEKANNTE_ANMELDEART)
        lockKind == "pin" && (secret.length < 4 || !secret.all { it.isDigit() }) ->
            Texte.t(Tx.PIN_ZU_KURZ)
        lockKind == "password" && secret.length < 8 -> Texte.t(Tx.PASSWORT_ZU_KURZ)
        else -> null
    }

    /** Was unter der Eingabe steht. */
    fun hinweis(lockKind: String): String = when (lockKind) {
        "pin" -> Texte.t(Tx.HINWEIS_PIN)
        "password" -> Texte.t(Tx.HINWEIS_PASSWORT)
        else -> Texte.t(Tx.HINWEIS_OFFEN)
    }
}

/**
 * Die Einstellungen, die Telefon und Fernseher gleich anbieten: die
 * Schalter der Wiedergabe und die gemerkten Sprachen für Ton und Untertitel.
 * Die Sprachen merkt sich Lumen auch, wenn man sie im Player wählt — hier
 * lassen sie sich vorab festlegen.
 */
object Einstellungen {
    const val AUTOPLAY = "autoplay"
    const val VORSPANN = "skipIntro"
    const val BEWERTUNGEN_TEILEN = "shareRatings"

    /** „Folgen automatisch weiter" ist an, solange es nicht ausdrücklich aus ist. */
    fun autoplay(prefs: Map<String, String>): Boolean = prefs[AUTOPLAY] != "0"
    fun vorspann(prefs: Map<String, String>): Boolean = prefs[VORSPANN] == "1"
    fun bewertungenTeilen(prefs: Map<String, String>): Boolean = prefs[BEWERTUNGEN_TEILEN] == "1"

    fun schalter(an: Boolean): String = if (an) "1" else "0"

    /** Die Sprachen, die zur Wahl stehen — die häufigsten, in dieser Reihenfolge. */
    private val SPRACHEN = listOf("de", "en", "fr", "it", "es", "pt", "nl", "sv", "da", "no",
                                  "fi", "pl", "cs", "ru", "tr", "ja", "ko", "zh")

    /**
     * Für die Tonspur: „Profilsprache" (leer, Standard), „Originalton" und
     * die Sprachen — wie Profil › Tonsprache im Browser. Ohne gemerkte
     * Sprache läuft der Ton in der Sprache des Profils ([Spuren.vorbelegen]).
     */
    val TON_WAHL: List<Pair<String, String>>
        get() = listOf("" to Texte.t(Tx.PROFILSPRACHE), Spuren.TON_ORIGINAL to Texte.t(Tx.ORIGINALTON)) +
            SPRACHEN.map { it to Spuren.sprache(it) }

    /** Was eine Wahl aus [TON_WAHL] ins Profil schreibt: Profilsprache und Originalton löschen die gemerkte Sprache. */
    fun tonPrefs(wert: String): Map<String, String> = when (wert) {
        "" -> mapOf(Spuren.PREF_TON to "", Spuren.PREF_TON_WAHL to "profil")
        Spuren.TON_ORIGINAL -> mapOf(Spuren.PREF_TON to "", Spuren.PREF_TON_WAHL to Spuren.TON_ORIGINAL)
        else -> mapOf(Spuren.PREF_TON to wert)
    }

    /** Welcher Eintrag aus [TON_WAHL] gerade gilt. */
    fun tonAktuell(prefs: Map<String, String>): String {
        val w = prefs[Spuren.PREF_TON]?.trim().orEmpty()
        return if (w.isEmpty() && prefs[Spuren.PREF_TON_WAHL] == Spuren.TON_ORIGINAL) Spuren.TON_ORIGINAL else w
    }

    /** Für Untertitel: „Aus" und die Sprachen. */
    val UNTERTITEL_WAHL: List<Pair<String, String>>
        get() = listOf(Spuren.AUS to Texte.t(Tx.AUS)) + SPRACHEN.map { it to Spuren.sprache(it) }

    /** Was gerade gilt, als Text — auch eine Sprache, die nicht in der Liste steht. */
    fun tonText(prefs: Map<String, String>): String = when (val w = tonAktuell(prefs)) {
        "" -> Texte.t(Tx.PROFILSPRACHE)
        Spuren.TON_ORIGINAL -> Texte.t(Tx.ORIGINALTON)
        else -> Spuren.sprache(w)
    }

    fun untertitelText(prefs: Map<String, String>): String {
        val w = prefs[Spuren.PREF_UNTERTITEL]?.trim().orEmpty()
        return if (w.isEmpty() || w == Spuren.AUS) Texte.t(Tx.AUS) else Spuren.sprache(w)
    }

    /** Der nächste Eintrag einer Wahl — für den Fernseher, wo ein Druck weiterschaltet. */
    fun naechste(wahl: List<Pair<String, String>>, aktuell: String?): String {
        val i = wahl.indexOfFirst { it.first == (aktuell ?: "") }
        return wahl[(i + 1).mod(wahl.size)].first
    }

    /** Wer man in diesem Haushalt ist — die Zeile unter dem Namen. */
    fun rolle(a: Account): String = when {
        a.isOwner -> Texte.t(Tx.ROLLE_BESITZER)
        a.isAdmin -> Texte.t(Tx.ROLLE_VOLLZUGRIFF) + (a.groupName?.let { " · $it" } ?: "")
        a.isKid -> Texte.t(Tx.ROLLE_KIND)
        a.groupName != null -> a.groupName
        else -> Texte.t(Tx.ROLLE_HAUSHALT)
    }
}
