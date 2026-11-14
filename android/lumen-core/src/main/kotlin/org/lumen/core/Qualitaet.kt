// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package org.lumen.core

/**
 * „Qualität" im Player — dieselben Stufen wie im Browser (app.js,
 * QUALITAETS_STUFEN, qualitaetParameter): Original, 4K · 20 Mbit/s,
 * 1080p · 8, 720p · 4, 480p · 2, 360p · 1 Mbit/s und Auto.
 *
 * Angeboten wird, was nicht über der Quelle liegt; „Original" und „Auto"
 * gibt es immer. Die Wahl gilt für dieses Gerät (die Apps merken sie
 * lokal), nicht fürs Konto: Am Handy unterwegs will man etwas anderes als
 * am Fernseher.
 *
 * Steht hier und nicht in den Oberflächen, damit Telefon und Fernseher
 * dasselbe zeigen und Tests.kt jeden Fall ohne Gerät nachstellen kann.
 */
object Qualitaet {

    const val ORIGINAL = "original"
    const val AUTO = "auto"

    /** Eine Stufe fürs Umwandeln: Obergrenzen für Höhe und Bitrate. */
    data class Stufe(val id: String, val hoehe: Int, val bitrate: Int)

    val STUFEN = listOf(
        Stufe("2160", 2160, 20_000_000),
        Stufe("1080", 1080, 8_000_000),
        Stufe("720", 720, 4_000_000),
        Stufe("480", 480, 2_000_000),
        Stufe("360", 360, 1_000_000),
    )

    /** „Original" hebt die Grenze des Bildschirms auf — wie im Browser. */
    const val ORIGINAL_HOEHE = 4320

    /** Die Stufen bis zur Quelle. Unbekannte Höhe: so tun, als wäre es 1080p. */
    fun stufenFuer(quellHoehe: Int?): List<Stufe> {
        val h = quellHoehe?.takeIf { it > 0 } ?: 1080
        return STUFEN.filter { it.hoehe <= h }
    }

    /**
     * Was für diese Quelle gilt: die gemerkte Stufe, wenn es sie hier gibt —
     * sonst „Auto" (480p gemerkt, die Datei hat 400p: dann eben die Datei).
     */
    fun wirksam(gemerkt: String?, quellHoehe: Int?): String = when {
        gemerkt == ORIGINAL -> ORIGINAL
        stufenFuer(quellHoehe).any { it.id == gemerkt } -> gemerkt!!
        else -> AUTO
    }

    /**
     * Das Geräteprofil mit den Grenzen der Stufe — für Plan und Strom
     * (`maxHeight`, `maxBitrate`). „Auto" lässt das Profil, wie es ist.
     */
    fun profil(gemerkt: String?, quellHoehe: Int?, basis: DeviceProfile = DeviceProfile.current): DeviceProfile {
        val id = wirksam(gemerkt, quellHoehe)
        if (id == ORIGINAL) return basis.copy(maxHeight = ORIGINAL_HOEHE, maxBitrate = 0)
        val s = STUFEN.firstOrNull { it.id == id } ?: return basis
        return basis.copy(maxHeight = s.hoehe, maxBitrate = s.bitrate)
    }

    /** „Original", „1080p · 8 Mbit/s", „4K · 20 Mbit/s", „Auto". */
    fun name(id: String): String {
        if (id == ORIGINAL) return Texte.t(Tx.QUALITAET_ORIGINAL)
        val s = STUFEN.firstOrNull { it.id == id } ?: return Texte.t(Tx.QUALITAET_AUTO)
        val hoehe = if (s.hoehe == 2160) "4K" else "${s.hoehe}p"
        return hoehe + " · " + Texte.t(Tx.MBIT_S, s.bitrate / 1_000_000)
    }
}
