// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package org.lumen.core

import java.time.Instant
import java.time.OffsetDateTime
import java.time.ZoneId
import java.time.format.DateTimeFormatter

/**
 * Was Telefon und Fernseher gleich anzeigen sollen und sich ohne Android
 * entscheiden lässt: die Abzeichen auf den Karten, die Technik-Zeile der
 * Titelseite, die Darstellung der Untertitel, die Profilbilder und wann
 * eine Neuigkeit neu ist. Dieselben Regeln wie im Browser (app.js), damit
 * eine Karte überall dasselbe sagt.
 */

// ---------------------------------------------------------------- Abzeichen

object Abzeichen {
    /**
     * Links oben: „Neu" oder „3 neue Folgen". Eine ganz neue Serie ist
     * „Neu", nicht „12 neue Folgen" — so hält es auch der Browser.
     */
    fun links(it: Item): String? = when {
        it.neu -> Texte.t(Tx.NEU)
        it.neueFolgen >= 1 -> Texte.n(TxN.NEUE_FOLGEN, it.neueFolgen)
        else -> null
    }

    /** Rechts oben: 4K und die HDR-Art. HD und SD sind kein Abzeichen wert. */
    fun rechts(it: Item): List<String> = listOfNotNull(
        if (it.qualitaet == "4K") "4K" else null,
        hdrName(it.hdr),
    )

    /** „DV" heisst Dolby Vision; alles andere steht, wie der Server es nennt. */
    fun hdrName(hdr: String?): String? = when {
        hdr.isNullOrBlank() -> null
        hdr == "DV" -> "Dolby Vision"
        else -> hdr
    }
}

// ---------------------------------------------------------------- Technik

/**
 * Ein Abzeichen der Technik-Zeile. [art] färbt es: „gut" grün (direkt
 * abspielbar), „blau" (wird umgewandelt), sonst nur umrandet. [untertitel]
 * bekommt das Sprechblasen-Symbol davor.
 */
data class TechAbzeichen(val text: String, val art: String = "normal", val untertitel: Boolean = false)

object Technik {
    /**
     * Die Zeile unter dem Titel: Auflösung, HDR, Ton (5.1, Stereo), die
     * Tonsprachen, Untertitel samt Sprachen, und — wenn der Plan schon da
     * ist — ob dieses Gerät die Datei direkt abspielt.
     *
     * [direkt] ist null, solange der Plan fehlt; dann bleibt das letzte
     * Abzeichen weg statt etwas zu raten.
     */
    fun abzeichen(item: Item, datei: MediaFile?, direkt: Boolean?): List<TechAbzeichen> {
        if (datei == null && item.qualitaet.isNullOrBlank()) return emptyList()
        val spuren = datei?.tracks ?: emptyList()
        val ton = spuren.filter { it.isAudio }
        val text = spuren.filter { it.isSubtitle && !it.bitmap }
        val kanaele = (ton.maxOfOrNull { it.channels } ?: 0).takeIf { it > 0 } ?: item.kanaele
        val sprachen = sprachenVon(ton)
        val utSprachen = sprachenVon(text)

        val out = mutableListOf<TechAbzeichen>()
        item.qualitaet?.takeIf { it.isNotBlank() }?.let { out += TechAbzeichen(it) }
        Abzeichen.hdrName(item.hdr ?: datei?.hdr)?.let { out += TechAbzeichen(it) }
        tonName(kanaele)?.let { out += TechAbzeichen(it) }
        if (sprachen.isNotEmpty()) out += TechAbzeichen(sprachen.joinToString(" · "))
        if (text.isNotEmpty()) {
            out += TechAbzeichen(
                if (utSprachen.isEmpty()) Texte.t(Tx.UNTERTITEL)
                else Texte.t(Tx.UNTERTITEL_SPRACHEN, utSprachen.joinToString(" · ")),
                untertitel = true)
        }
        when (direkt) {
            true -> out += TechAbzeichen(Texte.t(Tx.DIREKT_ABSPIELBAR), "gut")
            false -> out += TechAbzeichen(Texte.t(Tx.WIRD_UMGEWANDELT), "blau")
            null -> Unit
        }
        return out
    }

    /** 6 Kanäle sind 5.1, 8 sind 7.1, 2 Stereo; Mono und Unbekanntes nichts. */
    fun tonName(kanaele: Int): String? = when {
        kanaele > 2 -> "${kanaele - 1}.1"
        kanaele == 2 -> Texte.t(Tx.STEREO)
        else -> null
    }

    /** „DE · EN" — Sprachen in Grossbuchstaben, ohne „und", ohne Doppelte. */
    private fun sprachenVon(spuren: List<Track>): List<String> =
        spuren.mapNotNull { it.language?.trim()?.uppercase()?.takeIf { l -> l.isNotEmpty() && l != "UND" } }
            .distinct()
}

// ---------------------------------------------------------------- Untertitel

/**
 * Wie Untertitel aussehen: drei Grössen, mit oder ohne dunklen Hintergrund.
 * Steht in den Prefs des Kontos („utGroesse", „utHintergrund") und gilt im
 * Browser wie in den Apps.
 */
data class UntertitelStil(val groesse: String, val hintergrund: Boolean) {
    /**
     * Der Anteil an der Bildhöhe für Media3 (SubtitleView.setFractionalTextSize).
     * Media3 nimmt von sich aus 0,0533; klein und gross liegen im selben
     * Verhältnis darum wie im Browser.
     */
    val textAnteil: Float
        get() = GRUNDGROESSE * when (groesse) {
            "klein" -> 0.8f
            "gross" -> 1.4f
            else -> 1f
        }

    companion object {
        const val PREF_GROESSE = "utGroesse"
        const val PREF_HINTERGRUND = "utHintergrund"
        const val GRUNDGROESSE = 0.0533f
        /** Die Grössen mit ihrem Namen in der Sprache der Oberfläche. */
        val GROESSEN: List<Pair<String, String>>
            get() = listOf("klein" to Texte.t(Tx.GROESSE_KLEIN), "normal" to Texte.t(Tx.GROESSE_NORMAL),
                           "gross" to Texte.t(Tx.GROESSE_GROSS))
        private val SCHLUESSEL = listOf("klein", "normal", "gross")

        fun aus(prefs: Map<String, String>): UntertitelStil = UntertitelStil(
            groesse = prefs[PREF_GROESSE]?.takeIf { g -> g in SCHLUESSEL } ?: "normal",
            hintergrund = prefs[PREF_HINTERGRUND] != "0",
        )
    }
}

// ---------------------------------------------------------------- Profilbilder

/**
 * Ein Profilbild ist eine Farbe („av-a"), eine eingebaute Figur
 * („fig-fuchs") oder ein eigenes Bild („bild"), das der Server unter
 * /avatar/{konto} ausliefert.
 */
object Profilbild {
    val FARBEN = listOf("av-a", "av-b", "av-c", "av-d", "av-k")

    fun istBild(avatar: String?): Boolean = avatar == "bild"

    /** Die Figur zu einem Avatar-Schlüssel — oder null, wenn es keine ist. */
    fun figur(avatar: String?): Figur? {
        if (avatar == null || !avatar.startsWith("fig-")) return null
        return Figuren.alle[avatar.removePrefix("fig-")]
    }
}

/**
 * Eine Form der Zeichnung in einem 24 × 24-Feld: SVG-Pfaddaten, weiss
 * gezogen mit 1,6 Strichstärke; [gefuellt] auch ausgemalt (die Augen).
 */
data class Figurform(val pfad: String, val gefuellt: Boolean = false)

/** Eine eingebaute Figur: ihr Name, die Hintergrundfarbe (ARGB) und die Striche. */
data class Figur(val name: String, val farbe: Long, val formen: List<Figurform>)

/**
 * Die zwölf eingebauten Figuren — dieselben Zeichnungen wie im Browser,
 * eigene, keine fremden Marken. Kreise, Ellipsen und Rechtecke stehen hier
 * als Pfade, damit eine einzige Zeichenroutine genügt.
 */
object Figuren {
    private fun z(x: Double): String =
        if (x == Math.floor(x)) x.toLong().toString() else x.toString()

    /** Ein Kreis als Pfad: zwei Halbbögen. */
    fun kreis(cx: Double, cy: Double, r: Double): String = ellipse(cx, cy, r, r)

    fun ellipse(cx: Double, cy: Double, rx: Double, ry: Double): String =
        "M${z(cx - rx)} ${z(cy)}a${z(rx)} ${z(ry)} 0 1 0 ${z(2 * rx)} 0" +
            "a${z(rx)} ${z(ry)} 0 1 0 ${z(-2 * rx)} 0z"

    /** Ein Rechteck mit runden Ecken. */
    fun rechteck(x: Double, y: Double, b: Double, h: Double, r: Double): String =
        "M${z(x + r)} ${z(y)}h${z(b - 2 * r)}a${z(r)} ${z(r)} 0 0 1 ${z(r)} ${z(r)}" +
            "v${z(h - 2 * r)}a${z(r)} ${z(r)} 0 0 1 ${z(-r)} ${z(r)}" +
            "h${z(-(b - 2 * r))}a${z(r)} ${z(r)} 0 0 1 ${z(-r)} ${z(-r)}" +
            "v${z(-(h - 2 * r))}a${z(r)} ${z(r)} 0 0 1 ${z(r)} ${z(-r)}z"

    private fun f(d: String) = Figurform(d)
    private fun punkt(cx: Double, cy: Double) = Figurform(kreis(cx, cy, 0.8), gefuellt = true)

    val reihenfolge: List<Figur> = listOf(
        Figur("fuchs", 0xFFF59E5B, listOf(
            f("M6 7l3 4h6l3-4-2 9c-1 2-3 3-4 3s-3-1-4-3z"), punkt(9.5, 13.0), punkt(14.5, 13.0))),
        Figur("eule", 0xFF8B7BD1, listOf(
            f(kreis(12.0, 13.0, 7.0)), f(kreis(9.5, 12.0, 2.0)), f(kreis(14.5, 12.0, 2.0)),
            f("M12 14l-1 2h2z M7 7l2 2M17 7l-2 2"))),
        Figur("katze", 0xFFE07AA0, listOf(
            f("M6 6l2 4h8l2-4v9a6 6 0 0 1-12 0z"), f("M9 13h1M14 13h1M10 17c1 1 3 1 4 0"))),
        Figur("roboter", 0xFF6FB0E0, listOf(
            f(rechteck(6.0, 8.0, 12.0, 10.0, 2.0)), f(kreis(9.5, 12.5, 1.0)), f(kreis(14.5, 12.5, 1.0)),
            f("M12 4v4M9 16h6"))),
        Figur("rakete", 0xFFEF6B6B, listOf(
            f("M12 3c3 3 4 7 3 11l-3 3-3-3c-1-4 0-8 3-11z"), f("M9 14l-3 2 1-5M15 14l3 2-1-5M12 17v3"))),
        Figur("stern", 0xFFF2C84B, listOf(
            f("M12 4l2.4 5 5.6.8-4 3.9.9 5.5-4.9-2.6-4.9 2.6.9-5.5-4-3.9 5.6-.8z"))),
        Figur("wal", 0xFF4FA3C7, listOf(
            f("M4 13c2-5 9-6 14-3l2-3v7c-3 4-9 5-14 2z"), f("M15 12h.01M4 13l-1 3"))),
        Figur("blume", 0xFF7FCF8B, listOf(
            f(kreis(12.0, 10.0, 2.5)), f(kreis(12.0, 5.0, 2.0)), f(kreis(17.0, 10.0, 2.0)),
            f(kreis(7.0, 10.0, 2.0)), f(kreis(12.0, 15.0, 2.0)), f("M12 17v4"))),
        Figur("pilz", 0xFFD9775B, listOf(
            f("M4 12c0-5 4-8 8-8s8 3 8 8z"), f("M9 12v6a3 3 0 0 0 6 0v-6"))),
        Figur("geist", 0xFFB5B5C8, listOf(
            f("M6 20V11a6 6 0 0 1 12 0v9l-2-2-2 2-2-2-2 2-2-2z"), punkt(9.5, 11.0), punkt(14.5, 11.0))),
        Figur("drache", 0xFF5FB49C, listOf(
            f("M5 15c2-6 7-9 13-8l-2 3 3 2-4 1c0 4-4 6-8 5z"), f("M8 18l-2 3M12 18l0 3"))),
        Figur("pinguin", 0xFF5C6B8A, listOf(
            f(ellipse(12.0, 13.0, 5.0, 7.0)), f("M10 12l2 2 2-2M7 12l-2 4M17 12l2 4"))),
    )

    val alle: Map<String, Figur> = reihenfolge.associateBy { it.name }

    /**
     * Die Figuren, die dieser Server annimmt, in seiner Reihenfolge. Ein
     * Name, zu dem die App keine Zeichnung hat, fällt weg; schickt ein
     * älterer Server gar keine Liste, gelten alle eigenen.
     */
    fun angeboten(vomServer: List<String>): List<Figur> =
        if (vomServer.isEmpty()) reihenfolge else vomServer.mapNotNull { alle[it] }
}

// ---------------------------------------------------------------- Zeit

/** Zeitpunkte, wie der Server sie schickt (RFC 3339, aus Go). */
object Zeitpunkt {
    fun ms(text: String?): Long? {
        if (text.isNullOrBlank()) return null
        return try {
            OffsetDateTime.parse(text).toInstant().toEpochMilli()
        } catch (e: Exception) {
            null
        }
    }

    /** Ob [a] nach [b] liegt; ohne [b] ist alles danach. */
    fun nach(a: String?, b: String?): Boolean {
        val x = ms(a) ?: return false
        val y = ms(b) ?: return true
        return x > y
    }

    /**
     * „29. Okt." in de-CH, „Oct 29" in en-US, „29 Oct" in en-GB — das
     * Muster kommt mit der Sprache, die Monatsnamen aus der Locale (in
     * de-AT darum „Jän.").
     */
    fun kurz(text: String?, zone: ZoneId = ZoneId.systemDefault()): String {
        val m = ms(text) ?: return ""
        val d = Instant.ofEpochMilli(m).atZone(zone)
        return DateTimeFormatter.ofPattern(Texte.t(Tx.MUSTER_TAG_MONAT), Texte.locale).format(d)
    }
}
