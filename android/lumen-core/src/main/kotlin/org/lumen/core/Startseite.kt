// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package org.lumen.core

/** Filmreihen auf der Startseite — dieselbe Regel wie im Browser und beim Server (?limit=). */
object Reihen {
    /** Ab so vielen sichtbaren Filmen steht eine Reihe im Band der Startseite. */
    const val STARTSEITE_MINDESTENS = 2

    /** Nur Reihen mit mindestens zwei Filmen, höchstens [anzahl] — auch wenn ein älterer Server alles schickt. */
    fun fuerStartseite(reihen: List<Reihe>, anzahl: Int): List<Reihe> =
        reihen.filter { maxOf(it.anzahl, it.items.size) >= STARTSEITE_MINDESTENS }.take(anzahl)
}

/** Wie die Seite Filmreihen sortiert — wie im Browser (A–Z, zuletzt hinzugefügt, Anzahl Filme). */
enum class ReihenSortierung(private val beschriftung: Tx) {
    NAME(Tx.SORT_NAME), NEU(Tx.SORT_ZULETZT_NEU), ANZAHL(Tx.SORT_ANZAHL_FILME);

    val text: String get() = Texte.t(beschriftung)
}

/** Suchen und sortieren auf der Seite Filmreihen — dieselben Regeln wie reihenAuswahl im Browser. */
object ReihenAuswahl {
    private fun klein(t: String): String =
        java.text.Normalizer.normalize(t, java.text.Normalizer.Form.NFD)
            .replace(Regex("\\p{M}+"), "").lowercase()

    /**
     * Der Name zählt, aber auch die Titel darin — „Matrix Reloaded" findet
     * die Matrix-Reihe. Jedes Wort muss vorkommen; Akzente zählen nicht.
     */
    fun auswahl(liste: List<Reihe>, suche: String, sortierung: ReihenSortierung): List<Reihe> {
        val worte = klein(suche).split(Regex("\\s+")).filter { it.isNotEmpty() }
        val passend = if (worte.isEmpty()) liste else liste.filter { r ->
            val heu = klein((listOf(r.name) + r.items.map { it.title }).joinToString(" "))
            worte.all { heu.contains(it) }
        }
        val sammler = java.text.Collator.getInstance(Texte.locale).apply { strength = java.text.Collator.PRIMARY }
        val nachName = Comparator<Reihe> { a, b -> sammler.compare(a.name, b.name) }
        return when (sortierung) {
            ReihenSortierung.NAME -> passend.sortedWith(nachName)
            ReihenSortierung.NEU -> passend.sortedWith(compareByDescending<Reihe> { it.zuletztNeu }.then(nachName))
            ReihenSortierung.ANZAHL -> passend.sortedWith(compareByDescending<Reihe> { it.anzahl }.then(nachName))
        }
    }
}
