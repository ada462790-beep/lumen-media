// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package org.lumen.core

import java.util.Locale

/**
 * Tonspuren und Untertitel: was angeboten wird, wie es heisst und was
 * vorbelegt ist.
 *
 * Alles ohne Android, damit Telefon und Fernseher dieselbe Spur wählen und
 * Tests.kt jeden Fall nachstellen kann. Die Regeln sind die des Browsers
 * (app.js, spurenVorbelegen): Ton in der gemerkten Sprache; Untertitel
 * nur, wenn jemand sie will; erzwungene Untertitel laufen von selbst mit,
 * wenn sie zur Tonsprache gehören — dafür sind sie da.
 */
object Spuren {

    /** Was in den Einstellungen für „keine Untertitel" steht. */
    const val AUS = "aus"

    /** Die Schlüssel in den Prefs (PATCH /me). */
    const val PREF_TON = "tonSprache"
    const val PREF_UNTERTITEL = "untertitel"

    /**
     * Sprachkürzel, wie der Server sie liefert, die ausgeschrieben werden.
     * Die Namen selbst kommen aus der Locale der Oberfläche: „Englisch"
     * auf Deutsch, „English" auf Englisch.
     */
    private val sprachen = setOf(
        "de", "en", "fr", "it", "es", "ja", "ko", "pt", "nl", "ru", "pl", "tr", "sv", "da",
        "zh", "no", "fi", "cs", "hu", "el", "ar", "hi", "th", "uk", "rm",
    )

    /** „Deutsch" zu „de"; ein unbekanntes Kürzel in Grossbuchstaben; ohne: „Unbekannt". */
    fun sprache(code: String?): String {
        val k = code?.trim()?.lowercase() ?: ""
        if (k in sprachen) {
            val name = Locale(k).getDisplayLanguage(Texte.locale)
            if (name.isNotBlank() && name != k) return name.replaceFirstChar { it.titlecase(Texte.locale) }
        }
        return if (k.isEmpty() || k == "und") Texte.t(Tx.SPRACHE_UNBEKANNT) else k.uppercase()
    }

    /**
     * Der Anzeigename einer Spur — derselbe wie im Browser und in der
     * HLS-Liste des Servers: Sprache, dann in Klammern, was dransteht.
     * „Deutsch (erzwungen)", „Englisch (Kommentar, 5.1)", „Deutsch (Datei)".
     */
    fun name(t: Track): String {
        val name = sprache(t.language)
        val zusatz = buildList {
            t.title?.takeIf { it.isNotBlank() && !it.equals(name, ignoreCase = true) }?.let { add(it) }
            if (t.forced) add(Texte.t(Tx.SPUR_ERZWUNGEN))
            if (t.hearingImpaired) add(Texte.t(Tx.SPUR_HOERGESCHAEDIGT))
            if (t.isAudio && t.channels > 2) add("${t.channels - 1}.1")
            if (t.external) add(Texte.t(Tx.SPUR_DATEI))
        }
        return if (zusatz.isEmpty()) name else "$name (${zusatz.joinToString(", ")})"
    }

    /** Die Tonspuren, in der Reihenfolge der Datei. */
    fun ton(tracks: List<Track>): List<Track> = tracks.filter { it.isAudio }.sortedBy { it.index }

    /** Die Untertitel, die sich als Text ausliefern lassen — Bild-Untertitel nicht. */
    fun text(tracks: List<Track>): List<Track> =
        tracks.filter { it.isSubtitle && !it.bitmap }.sortedBy { it.index }

    /** Ob es überhaupt etwas zu wählen gibt: mehr als eine Tonspur oder Untertitel. */
    fun waehlbar(tracks: List<Track>): Boolean = ton(tracks).size > 1 || text(tracks).isNotEmpty()

    /**
     * Was läuft: [ton] ist der Index der Tonspur, -1 für die Vorgabe des
     * Stroms; [untertitel] der Index der Textspur oder null für aus.
     */
    data class Wahl(val ton: Int = -1, val untertitel: Int? = null)

    /** Profil › Tonsprache: „profil" (Standard) oder „original" — wie im Browser. */
    const val PREF_TON_WAHL = "tonWahl"
    const val TON_ORIGINAL = "original"

    /**
     * Die Vorbelegung — dieselbe Regel wie im Browser (spurenVorbelegen):
     *
     * Ton: die gemerkte Sprache, wenn es sie gibt. Ohne gemerkte Sprache
     * die Sprache des Profils ([eigen]) — oder, wer „Originalton" gewählt
     * hat, die als Original markierte (sonst die erste) Spur. Gibt es
     * nichts davon, bleibt die Vorgabe des Stroms.
     *
     * Untertitel:
     *  - Läuft der Ton in einer anderen Sprache als das Profil, kommen
     *    Untertitel in der Sprache des Profils, wenn es welche gibt — sonst
     *    keine. Fremde Untertitel schaltet Lumen nie von selbst ein.
     *  - Läuft er in der Sprache des Profils, gibt es Untertitel nur, wenn
     *    jemand sie in genau dieser Sprache gewählt hat (etwa für
     *    Hörgeschädigte).
     *  - „Aus" bleibt aus. Erzwungene in der Tonsprache laufen immer mit.
     *
     * Vorher galt die zuletzt gewählte Sprache überall: Wer einmal
     * englische Untertitel nahm, bekam sie bei jedem englischen Film.
     */
    fun vorbelegen(tracks: List<Track>, prefs: Map<String, String>,
                   eigen: String = Texte.sprache.take(2).lowercase()): Wahl {
        val tonSpuren = ton(tracks)
        val textSpuren = text(tracks)

        var tonIndex = -1
        var tonWunsch = prefs[PREF_TON]?.trim()?.takeIf { it.isNotEmpty() }
        if (tonWunsch == null && prefs[PREF_TON_WAHL] == TON_ORIGINAL) {
            val original = Regex("""\boriginal|\bOV\b|\bO-?Ton\b""", RegexOption.IGNORE_CASE)
            (tonSpuren.firstOrNull { original.containsMatchIn(it.title ?: "") } ?: tonSpuren.firstOrNull())
                ?.let { tonIndex = it.index }
        } else if (tonWunsch == null) {
            tonWunsch = eigen
        }
        tonWunsch?.let { w -> tonSpuren.firstOrNull { it.language == w }?.let { tonIndex = it.index } }

        val aktuell = tonSpuren.firstOrNull { it.index == tonIndex } ?: tonSpuren.firstOrNull()
        val tonSprache = aktuell?.language?.takeIf { it.isNotBlank() && it != "und" } ?: ""
        fun finden(sprache: String): Track? =
            textSpuren.firstOrNull { it.language == sprache && !it.forced }
                ?: textSpuren.firstOrNull { it.language == sprache }

        val gewaehlt = prefs[PREF_UNTERTITEL]?.trim().orEmpty()
        val untertitel: Track? = when {
            gewaehlt == AUS -> null
            tonSprache.isNotEmpty() && tonSprache != eigen -> finden(eigen)
            gewaehlt.isNotEmpty() && gewaehlt == tonSprache.ifEmpty { eigen } -> finden(gewaehlt)
            else -> null
        }
        return Wahl(tonIndex, untertitel?.index ?: erzwungene(tracks, tonIndex)?.index)
    }

    /**
     * Die erzwungene Untertitelspur zur Tonspur [tonIndex] (-1: die erste
     * Tonspur) — oder null. Erzwungene Untertitel übersetzen, was in einer
     * fremden Sprache gesprochen wird; sie gehören zur Tonsprache.
     */
    fun erzwungene(tracks: List<Track>, tonIndex: Int): Track? {
        val tonSpuren = ton(tracks)
        val aktuell = tonSpuren.firstOrNull { it.index == tonIndex } ?: tonSpuren.firstOrNull()
            ?: return null
        return text(tracks).firstOrNull { it.forced && it.language == aktuell.language }
    }

    /** Welche Stelle unter den Tonspuren der Index hat — für Abspieler, die
     *  Spuren nur der Reihe nach kennen. -1, wenn es die Spur nicht gibt. */
    /**
     * Die Wahl aus der vorigen Fassung auf die Spuren dieser übertragen —
     * nach Sprache (und Kanälen, Merkmalen), nicht nach Nummer. Wie im
     * Browser (app.js, spurenUebernehmen): Gibt es die Sprache hier nicht,
     * bleibt [vorbelegt]. Untertitel „aus" bleibt aus.
     */
    fun uebernehmen(alt: List<Track>, wahl: Wahl, neu: List<Track>, vorbelegt: Wahl): Wahl {
        val altTon = ton(alt).let { t -> t.firstOrNull { it.index == wahl.ton } ?: if (wahl.ton < 0) t.firstOrNull() else null }
        var tonIndex = vorbelegt.ton
        altTon?.language?.takeIf { it.isNotBlank() }?.let { sprache ->
            val kandidaten = ton(neu)
            (kandidaten.firstOrNull { it.language == sprache && it.channels == altTon.channels }
                ?: kandidaten.firstOrNull { it.language == sprache })?.let { tonIndex = it.index }
        }
        val untertitel: Int? = if (wahl.untertitel == null) null else {
            val u = alt.firstOrNull { it.isSubtitle && it.index == wahl.untertitel }
            if (u == null) vorbelegt.untertitel else {
                val passt = { x: Track -> (x.language ?: "") == (u.language ?: "") && x.forced == u.forced }
                val textSpuren = text(neu)
                (textSpuren.firstOrNull { passt(it) && it.hearingImpaired == u.hearingImpaired }
                    ?: textSpuren.firstOrNull(passt))?.index ?: vorbelegt.untertitel
            }
        }
        return Wahl(tonIndex, untertitel)
    }

    fun tonPosition(tracks: List<Track>, tonIndex: Int): Int =
        ton(tracks).indexOfFirst { it.index == tonIndex }

    /** Was in die Prefs kommt, wenn jemand diese Untertitel wählt: die
     *  Sprache, „und" für eine ohne, [AUS] für keine. */
    fun prefWertUntertitel(t: Track?): String =
        if (t == null) AUS else t.language?.takeIf { it.isNotBlank() } ?: "und"

    /** Was in die Prefs kommt, wenn jemand diese Tonspur wählt — oder null,
     *  wenn die Spur keine Sprache hat; dann gibt es nichts zu merken. */
    fun prefWertTon(t: Track?): String? =
        t?.language?.takeIf { it.isNotBlank() && it != "und" }
}

/**
 * Vorspann und Abspann im Player — wann der Knopf steht, wann „Als
 * Nächstes" kommt, wie lange der Countdown läuft. Dieselben Zahlen wie im
 * Browser (vorspannPruefen, abspannPruefen).
 */
object Marken {

    /** Ohne Kapitel beginnt der Abspann so viele Sekunden vor dem Ende. */
    const val ABSPANN_SEKUNDEN = 40

    /** „Als Nächstes" im Abspann zählt mindestens so lange herunter. */
    const val MINDEST_COUNTDOWN = 10

    /** Ein Vorspann muss zwischen einer Sekunde und zehn Minuten lang sein —
     *  dieselbe Grenze zieht der Server. */
    fun vorspannGueltig(vonMs: Long, bisMs: Long): Boolean =
        bisMs > vonMs && bisMs - vonMs <= 10 * 60 * 1000

    /**
     * Ob die Stelle im Vorspann liegt: eine halbe Sekunde nach dem Anfang
     * bis eine Sekunde vor dem Ende — sonst flackert der Knopf an den
     * Rändern, und wer von selbst springt, landet nicht mitten im Sprung.
     */
    fun imVorspann(intro: Vorspann?, positionMs: Long): Boolean {
        if (intro == null) return false
        return positionMs >= intro.vonMs + 500 && positionMs < intro.bisMs - 1000
    }

    /** Ab wann der Abspann läuft: laut Kapitel, sonst 40 s vor dem Ende. 0, wenn unbekannt. */
    fun abspannAb(creditsMs: Long?, dauerMs: Long): Long {
        if (creditsMs != null && creditsMs > 0) return creditsMs
        if (dauerMs <= 0) return 0
        return (dauerMs - ABSPANN_SEKUNDEN * 1000L).coerceAtLeast(0)
    }

    /**
     * Ob „Als Nächstes" jetzt erscheinen soll: die Stelle liegt im Abspann,
     * aber nicht in den letzten zwei Sekunden — da endet der Titel gleich
     * von selbst, und das Ende hat seine eigene Behandlung.
     */
    fun imAbspann(creditsMs: Long?, dauerMs: Long, positionMs: Long): Boolean {
        if (dauerMs <= 0) return false
        val ab = abspannAb(creditsMs, dauerMs)
        return positionMs >= ab && positionMs <= dauerMs - 2000
    }

    /** Der Countdown im Abspann: die Zeit bis zum Ende, mindestens zehn Sekunden. */
    fun countdown(dauerMs: Long, positionMs: Long): Int {
        val rest = Math.round((dauerMs - positionMs) / 1000.0).toInt()
        return rest.coerceAtLeast(MINDEST_COUNTDOWN)
    }
}
