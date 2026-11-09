// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package org.lumen.core

/**
 * Die Vorschaubilder fürs Spulen: Blätter (JPEG) mit [spalten] × [zeilen]
 * Kacheln, jede [breite] × [hoehe] Pixel, eine alle [intervall].
 *
 * Wer am Telefon über die Zeitleiste fährt oder am Fernseher spult, sieht
 * so, wo er landet — statt blind zu springen und neu laden zu lassen.
 */
data class Trickplay(
    val intervall: Double,
    val breite: Int,
    val hoehe: Int,
    val spalten: Int,
    val zeilen: Int,
    val bilder: Int,
    val blaetter: List<String>,
) {
    /**
     * Der Abstand in Sekunden. Der Vertrag nennt keine Einheit; Werte ab
     * 100 sind sicher Millisekunden (niemand macht ein Bild alle zwei
     * Minuten), kleinere Sekunden — so liest es auch die Apple-App.
     */
    val intervallSekunden: Double get() = if (intervall >= 100) intervall / 1000 else intervall

    /** Ob sich damit etwas zeigen lässt. */
    val brauchbar: Boolean
        get() = intervallSekunden > 0 && breite > 0 && hoehe > 0 && spalten > 0 && zeilen > 0 &&
            bilder > 0 && blaetter.isNotEmpty()

    /** Wo das Bild für eine Stelle liegt: welches Blatt, und darin die Kachel in Pixeln. */
    data class Kachel(val blatt: Int, val x: Int, val y: Int, val breite: Int, val hoehe: Int)

    /** Die Kachel für [sekunden] — null, wenn es keine gibt. */
    fun kachel(sekunden: Double): Kachel? {
        if (!brauchbar || sekunden.isNaN() || sekunden.isInfinite()) return null
        val nr = (sekunden / intervallSekunden).toInt().coerceIn(0, bilder - 1)
        val jeBlatt = spalten * zeilen
        val blatt = nr / jeBlatt
        if (blatt >= blaetter.size) return null
        val imBlatt = nr % jeBlatt
        return Kachel(blatt, (imBlatt % spalten) * breite, (imBlatt / spalten) * hoehe, breite, hoehe)
    }

    companion object {
        fun from(j: JsonValue?) = Trickplay(
            intervall = j["intervall"].double ?: 0.0,
            breite = j["breite"].int ?: 0,
            hoehe = j["hoehe"].int ?: 0,
            spalten = j["spalten"].int ?: 0,
            zeilen = j["zeilen"].int ?: 0,
            bilder = j["bilder"].int ?: 0,
            blaetter = j["blaetter"].strings(),
        )
    }
}

/** Was der Server zu den Vorschaubildern einer Datei sagt. */
sealed interface TrickplayStand {
    data class Bereit(val trickplay: Trickplay) : TrickplayStand
    /** 202: wird gerade erstellt — später noch einmal fragen. */
    data object WirdErstellt : TrickplayStand
    /** 404: gibt es nicht. */
    data object Keins : TrickplayStand
}
