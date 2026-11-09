// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package org.lumen.core

/**
 * Fassungen und Teile eines Films — dieselben Regeln wie im
 * Browser (app.js: fassungName, fassungsWahl, startPlayer).
 *
 * Zwei Fälle, die beide mehrere Dateien zu einem Titel bedeuten:
 *
 *  - **Fassungen**: mehrere Dateien mit Teil 1 — derselbe Film in MKV mit
 *    1080p und als AVI mit 400p. „Ansehen" fragt dann zuerst nach der
 *    Qualität. Der Server liefert sie schon geordnet: die bevorzugte vorn,
 *    dann die mit der höchsten Auflösung. Die erste ist darum der Standard.
 *  - **Teile**: Dateien mit Teil 1, 2, 3 … („Apocalypse Now (1979) a.avi",
 *    „b.avi", „c.avi") — ein Film, der nacheinander läuft.
 *
 * Steht hier und nicht in den Oberflächen, damit Telefon und Fernseher
 * dasselbe zeigen und Tests.kt jeden Fall ohne Gerät nachstellen kann.
 */
object Fassungen {

    /** Die Fassungen: jede vorhandene Datei, die als Teil 1 gilt — in der Reihenfolge des Servers. */
    fun liste(dateien: List<MediaFile>): List<MediaFile> =
        dateien.filter { !it.missing && it.partNo == 1 }

    /** Ob „Ansehen" erst nach der Qualität fragen muss. */
    fun mehrere(dateien: List<MediaFile>): Boolean = liste(dateien).size > 1

    /** Die Auflösung, wie man sie kennt: 4K, 1080p, 720p — sonst die Zeilen, etwa „400p". */
    fun aufloesung(hoehe: Int?): String? {
        val h = hoehe ?: 0
        return when {
            h >= 2000 -> "4K"
            h >= 1000 -> "1080p"
            h >= 700 -> "720p"
            h > 0 -> "${h}p"
            else -> null
        }
    }

    /**
     * Die Grösse wie im Browser (de-CH, höchstens eine Nachkommastelle):
     * „7,9 GB", „8 GB" — unter einem GB in MB, mindestens „1 MB".
     */
    fun groesse(bytes: Long): String? {
        if (bytes <= 0) return null
        if (bytes < 1_000_000_000L) return "${maxOf(1L, Math.round(bytes / 1e6))} MB"
        val gb = Math.round(bytes / 1e8) / 10.0
        val text = if (gb == Math.floor(gb)) gb.toLong().toString() else Titelseite.komma(gb)
        return "$text GB"
    }

    /** Eine Fassung in der Auswahl: „1080p · MKV · 7,9 GB". */
    fun name(f: MediaFile): String = listOfNotNull(
        aufloesung(f.height),
        f.container?.takeIf { it.isNotBlank() }?.uppercase(),
        groesse(f.sizeBytes),
    ).joinToString(" · ")

    /**
     * Welche Datei spielt: die gewählte, solange es sie noch gibt — sonst
     * die erste vorhandene, wie bisher. Eine Kennung aus einer älteren
     * Auswahl, die inzwischen fehlt, soll nicht ins Leere führen.
     */
    fun datei(dateien: List<MediaFile>, gewaehlt: String?): MediaFile? =
        gewaehlt?.let { id -> dateien.firstOrNull { it.id == id && !it.missing } }
            ?: dateien.firstOrNull { !it.missing }

    /**
     * Die Teile, die zu [erste] gehören: Teil 1 ist die gewählte Datei
     * (sofern sie Teil 1 ist), dann Teil 2, 3 … der Reihe nach, bis einer
     * fehlt. Wie im Browser: Fehlt Teil 3, läuft der Film bis Teil 2.
     */
    fun teile(dateien: List<MediaFile>, erste: MediaFile): FilmTeile {
        val liste = mutableListOf<MediaFile>()
        var nr = 1
        while (true) {
            val t = if (nr == 1 && erste.partNo == 1) erste
                    else dateien.firstOrNull { !it.missing && it.partNo == nr }
            if (t == null) break
            liste.add(t)
            nr++
        }
        // Eine Datei, die nicht Teil 1 ist und keinen Teil 1 neben sich hat,
        // spielt trotzdem — allein, wie bisher.
        return FilmTeile(liste.ifEmpty { listOf(erste) })
    }
}

/**
 * Ein Film in Teilen. Der gespeicherte Stand zählt über alle Teile: die
 * Position ist die Dauer der Teile davor plus die Stelle im Teil, die
 * Dauer die Summe aller Teile — genau wie im Browser, damit „Weiter bei"
 * auf allen Geräten dasselbe meint.
 *
 * Bei nur einer Datei ist alles wie bisher: Teil 0, nichts davor.
 */
class FilmTeile(val dateien: List<MediaFile>) {

    /** Wo eine Gesamtstelle liegt: in welchem Teil, was davor kommt, und wo darin. */
    data class Stelle(val teil: Int, val vorher: Double, val imTeil: Double)

    val anzahl: Int get() = dateien.size
    val mehrere: Boolean get() = dateien.size > 1

    /** Die Dauer aller Teile zusammen, in Sekunden. */
    val gesamtSekunden: Double get() = dateien.sumOf { it.durationSeconds }

    /** Die Dauer der Teile vor [teil], in Sekunden. */
    fun vorher(teil: Int): Double = dateien.take(teil.coerceIn(0, dateien.size)).sumOf { it.durationSeconds }

    fun istLetzter(teil: Int): Boolean = teil >= dateien.size - 1

    /**
     * Aus einer Gesamtstelle der Teil und die Stelle darin. Ein Stand
     * hinter dem Ende (fünf Sekunden Luft) heisst: von vorn — wie im
     * Browser, damit ein fertig geschauter Film nicht im Leeren beginnt.
     */
    fun stelle(gesamt: Double): Stelle {
        if (!mehrere) return Stelle(0, 0.0, gesamt)
        val summe = gesamtSekunden
        var start = gesamt
        if (summe > 0 && start > summe - 5) start = 0.0
        var teil = 0
        var vorher = 0.0
        while (teil < dateien.size - 1 && start >= vorher + dateien[teil].durationSeconds) {
            vorher += dateien[teil].durationSeconds
            teil++
        }
        return Stelle(teil, vorher, start - vorher)
    }

    /**
     * Die Gesamtstelle, mit der der Teil nach [teil] beginnt — eine halbe
     * Sekunde hinter der Grenze, damit [stelle] sicher im nächsten Teil
     * landet (wie im Browser in amEnde).
     */
    fun weiterAb(teil: Int): Double =
        vorher(teil) + (dateien.getOrNull(teil)?.durationSeconds ?: 0.0) + 0.5

    /** Was im Player unter dem Titel steht: „Teil 2 von 3" — bei einer Datei nichts. */
    fun beschriftung(teil: Int): String? = if (mehrere) Texte.t(Tx.TEIL_X_VON_Y, teil + 1, anzahl) else null
}
