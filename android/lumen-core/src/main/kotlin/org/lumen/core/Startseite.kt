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
