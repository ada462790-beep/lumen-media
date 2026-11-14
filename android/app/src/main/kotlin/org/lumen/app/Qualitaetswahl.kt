// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package org.lumen.app

import android.content.Context
import org.lumen.core.Qualitaet

/**
 * Die gewählte Stufe unter „Qualität" im Player — für dieses Gerät, nicht
 * fürs Konto (wie im Browser in localStorage). Kein Geheimnis, darum in
 * den gewöhnlichen Einstellungen der App.
 */
internal object Qualitaetswahl {
    private const val DATEI = "lumen-geraet"
    private const val SCHLUESSEL = "qualitaet"

    fun lesen(context: Context): String =
        context.getSharedPreferences(DATEI, Context.MODE_PRIVATE).getString(SCHLUESSEL, null) ?: Qualitaet.AUTO

    fun merken(context: Context, id: String) {
        context.getSharedPreferences(DATEI, Context.MODE_PRIVATE).edit().putString(SCHLUESSEL, id).apply()
    }
}
