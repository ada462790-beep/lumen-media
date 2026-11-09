// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package org.lumen.app

import androidx.annotation.PluralsRes
import androidx.annotation.StringRes
import org.lumen.texte.Sprachwahl

/**
 * Ein Text aus strings.xml in der Sprache, die gerade gilt.
 *
 * Bewusst eine einzige Funktion statt stringResource in Compose und
 * getString anderswo: dieselben Texte stehen in Composables, in
 * Rückrufen, im ViewModel und in Benachrichtigungen. Wechselt die
 * Sprache, baut sich die Activity ohnehin neu auf (Sprachwahl.anwenden),
 * und alles wird neu gezeichnet.
 */
fun tx(@StringRes id: Int, vararg args: Any?): String {
    val res = checkNotNull(Sprachwahl.ressourcen) { "Sprachwahl.einrichten fehlt" } // sprache: fest
    return if (args.isEmpty()) res.getString(id) else res.getString(id, *args)
}

/** Ein Text mit Mehrzahl; ohne weitere Argumente ist [anzahl] das erste. */
fun txn(@PluralsRes id: Int, anzahl: Int, vararg args: Any?): String {
    val res = checkNotNull(Sprachwahl.ressourcen) { "Sprachwahl.einrichten fehlt" } // sprache: fest
    return if (args.isEmpty()) res.getQuantityString(id, anzahl, anzahl)
    else res.getQuantityString(id, anzahl, *args)
}
