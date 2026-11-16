// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package org.lumen.core

/**
 * Bilder in der Grösse, in der sie gezeigt werden — wie die Web-App mit
 * srcset (server/internal/api/bildgroesse.go).
 *
 * Lokale Poster aus der Sammlung sind oft 2000 × 3000 Punkte und über ein
 * Megabyte gross; eine Kachel am Telefon ist rund 330 Punkte breit. Mit
 * `?w=` rechnet der Server das Bild einmal auf die nächste Stufe herunter
 * und behält es. Das spart unterwegs Daten und macht Raster und Suche
 * flüssiger.
 *
 * Dazu der Stand (`?v=`): Der Server hängt ihn an die Bildadressen, die er
 * in Titeln mitschickt. Ändert sich ein Poster, ändert sich die Adresse —
 * so zeigt der Bildlader das neue sofort und darf das alte sonst behalten.
 */
object Bildgroesse {

    /**
     * Wie breit dieses Gerät ein Poster (Kachel) bzw. ein breites Bild
     * (Hintergrund, Standbild über die ganze Breite) zeigt, in echten
     * Bildpunkten. Die App setzt beides beim Start aus Bildschirm und
     * Dichte; ohne Angabe fragt sie ohne ?w= (der Server begrenzt dann
     * selbst auf 780 bzw. 1920).
     */
    @Volatile var posterBildpunkte: Int? = null
    @Volatile var breitBildpunkte: Int? = null

    /** Beim Start: Kachelbreite und Bildschirmbreite in dp, dazu die Dichte. */
    fun einrichten(kachelDp: Float, bildschirmDp: Float, dichte: Float) {
        posterBildpunkte = (kachelDp * dichte).toInt().takeIf { it > 0 }
        breitBildpunkte = (bildschirmDp * dichte).toInt().takeIf { it > 0 }
    }

    /** Die Breiten, die der Server kennt (bildBreiten). Eine andere Angabe rundet er auf. */
    val STUFEN = listOf(185, 342, 500, 780, 1280, 1920)

    /** Diese Bilder rechnet der Server herunter; Logos (durchsichtig), Standbilder und Vorschau nicht. */
    private fun verkleinerbar(kind: String): Boolean =
        kind == "poster" || kind == "backdrop" || kind == "landscape" || kind == "banner" ||
            kind == "thumb" || kind.startsWith("cast")

    /**
     * Die Breite für `?w=` — die nächste Stufe ab [bildpunkte] (echte
     * Pixel, nicht dp), oder null, wenn es keine braucht: keine Angabe,
     * ein Logo, ein Standbild.
     */
    fun breite(kind: String, bildpunkte: Int?): Int? {
        val w = bildpunkte ?: return null
        if (w <= 0 || !verkleinerbar(kind)) return null
        return STUFEN.firstOrNull { it >= w } ?: STUFEN.last()
    }

    /** Der Stand aus einer Bildadresse des Servers („/api/v1/art/it_1/poster?v=abc") — oder null. */
    fun stand(pfad: String?): String? {
        val q = pfad?.substringAfter('?', "") ?: return null
        if (q.isEmpty()) return null
        return q.split('&').firstOrNull { it.startsWith("v=") }?.removePrefix("v=")?.takeIf { it.isNotBlank() }
            ?.let { java.net.URLDecoder.decode(it, Charsets.UTF_8) }
    }
}

/**
 * Der Schlüssel, unter dem ein Bild im Speicher des Bildladers liegt: die
 * Adresse ohne den Medienschlüssel (?key=). Der wechselt alle paar Stunden
 * — mit ihm im Schlüssel lud die App nach jedem Wechsel jedes Poster neu,
 * obwohl es dasselbe Bild ist. Ob das Bild noch gilt, sagen Stand (?v=)
 * und die Angaben des Servers zur Haltbarkeit.
 */
fun bildSpeicherSchluessel(adresse: String): String {
    val q = adresse.substringAfter('?', "")
    if (q.isEmpty()) return adresse
    val rest = q.split('&').filter { it.isNotEmpty() && !it.startsWith("key=") }
    val basis = adresse.substringBefore('?')
    return if (rest.isEmpty()) basis else basis + "?" + rest.joinToString("&")
}
