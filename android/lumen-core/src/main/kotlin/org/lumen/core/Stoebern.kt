// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package org.lumen.core

/**
 * Sortieren und nach Anfangsbuchstaben springen in den Rastern der Filme
 * und Serien — dieselben vier Ordnungen wie im Browser
 * (app.js: SORTIERUNGEN, BUCHSTABEN). Steht hier, damit Telefon und
 * Fernseher dasselbe anbieten und Tests.kt es ohne Gerät prüfen kann.
 */
enum class Sortierung(val schluessel: String, private val film: Tx, private val serie: Tx) {
    TITEL("title", Tx.SORT_AZ, Tx.SORT_AZ),
    HINZUGEFUEGT("added", Tx.SORT_HINZUGEFUEGT, Tx.SORT_HINZUGEFUEGT),
    NEUESTE("year", Tx.SORT_NEUESTE_FILME, Tx.SORT_NEUESTE_SERIEN),
    AELTESTE("year-asc", Tx.SORT_AELTESTE_FILME, Tx.SORT_AELTESTE_SERIEN);

    /** Die Aufschrift im Raster: bei Serien heisst es „Neueste Serien", nicht „Filme". */
    fun titel(kind: String): String = Texte.t(if (kind == "series") serie else film)

    /** Die Buchstabenleiste gibt es nur bei A–Z — sonst spränge ein Buchstabe ins Leere. */
    val mitLeiste: Boolean get() = this == TITEL
}

/**
 * Die Buchstabenleiste: „#“ für Zahlen und Zeichen, dann A bis Z — mit der
 * Zahl der Titel je Buchstabe aus GET /items/letters. Ein Buchstabe ohne
 * Titel ist da, aber nicht wählbar.
 */
data class Buchstaben(val zahlen: Map<String, Int>) {
    fun anzahl(b: String): Int = zahlen[b] ?: 0
    fun waehlbar(b: String): Boolean = anzahl(b) > 0

    companion object {
        /** Die Raute: alles, was nicht mit einem Buchstaben beginnt. Als Zeichen
         *  geschrieben — tools/schnittstelle_pruefen.py liest sonst den Rest
         *  der Datei als Swift-Rohtext. */
        val RAUTE: String = '#'.toString()

        val ALLE: List<String> = listOf(RAUTE) + ('A'..'Z').map { it.toString() }

        /** {"A":12, …} — alles, was keine Zahl ist, fällt weg. */
        fun from(j: JsonValue?): Buchstaben = Buchstaben(
            (j as? JsonValue.Obj)?.fields
                ?.mapNotNull { (k, v) -> v.int?.let { k.uppercase() to it } }
                ?.toMap() ?: emptyMap())
    }
}
