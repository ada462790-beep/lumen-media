// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package org.lumen.core

/**
 * Sortieren und nach Anfangsbuchstaben springen in den Rastern der Filme
 * und Serien — dieselben Ordnungen wie im Browser (app.js: SORTIERUNGEN,
 * BUCHSTABEN). Der Schlüssel geht als `sort` an den Server, die Richtung
 * als `dir`. Steht hier, damit Telefon und Fernseher dasselbe anbieten
 * und Tests.kt es ohne Gerät prüfen kann.
 */
enum class Sortierung(val schluessel: String, private val text: Tx, val vorgabeAbsteigend: Boolean) {
    TITEL("title", Tx.SORT_TITEL, false),
    HINZUGEFUEGT("added", Tx.SORT_HINZUGEFUEGT, true),
    ERSCHIENEN("premiered", Tx.SORT_ERSCHIENEN, true),
    BEWERTUNG("rating", Tx.SORT_BEWERTUNG, true),
    LAUFZEIT("runtime", Tx.SORT_LAUFZEIT, false),
    ZULETZT_GESEHEN("lastwatched", Tx.SORT_ZULETZT_GESEHEN, true),
    UNGESEHEN("unwatched", Tx.SORT_UNGESEHEN, false),
    ZUFALL("random", Tx.SORT_ZUFALL, false);

    /** Die Aufschrift: „Titel", „Hinzugefügt", „Erscheinungsdatum" … */
    fun titel(kind: String): String = Texte.t(text)

    /** Die Buchstabenleiste gibt es nur nach Titel — sonst spränge ein Buchstabe ins Leere. */
    val mitLeiste: Boolean get() = this == TITEL

    /** „title:asc" — so steht die Wahl im Profil, wie bei Browser und Apple. */
    fun wert(absteigend: Boolean): String = schluessel + if (absteigend) ":desc" else ":asc"

    companion object {
        /** Die Sortierungen dieser Art — die Laufzeit nur bei Filmen. */
        fun faelle(kind: String): List<Sortierung> =
            if (kind == "movie") entries else entries.filter { it != LAUFZEIT }

        /** Der Schlüssel in den Einstellungen des Profils: sort.movie, sort.series. */
        fun prefSchluessel(kind: String): String = "sort.$kind"

        /** Liest „added:desc"; Unbekanntes gibt null. Ohne Richtung die übliche. */
        fun aus(wert: String?, kind: String): Pair<Sortierung, Boolean>? {
            if (wert.isNullOrBlank()) return null
            val teile = wert.split(":", limit = 2)
            val art = faelle(kind).firstOrNull { it.schluessel == teile[0] } ?: return null
            val ab = when (teile.getOrNull(1)) {
                "asc" -> false
                "desc" -> true
                else -> art.vorgabeAbsteigend
            }
            return art to ab
        }

        /** „Aufsteigend" oder „Absteigend" — für den Knopf der Richtung. */
        fun richtung(absteigend: Boolean): String =
            Texte.t(if (absteigend) Tx.SORT_ABSTEIGEND else Tx.SORT_AUFSTEIGEND)

        /** „Neu mischen" — der Knopf bei „Zufällig". */
        val neuMischen: String get() = Texte.t(Tx.SORT_NEU_MISCHEN)
    }
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

        /** Die Buchstaben in der Reihenfolge des Rasters: absteigend von Z
         *  nach A, die Raute (Zahlen) dann am Ende. */
        fun reihe(absteigend: Boolean): List<String> =
            if (absteigend) ALLE.drop(1).reversed() + RAUTE else ALLE

        /** {"A":12, …} — alles, was keine Zahl ist, fällt weg. */
        fun from(j: JsonValue?): Buchstaben = Buchstaben(
            (j as? JsonValue.Obj)?.fields
                ?.mapNotNull { (k, v) -> v.int?.let { k.uppercase() to it } }
                ?.toMap() ?: emptyMap())
    }
}
