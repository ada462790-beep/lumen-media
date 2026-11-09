// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package org.lumen.tv

import android.content.Context
import coil.ImageLoader
import okhttp3.Interceptor
import okhttp3.OkHttpClient
import okhttp3.Response
import org.lumen.core.ApiClient

/**
 * Der Bildlader des Fernsehers für alle Poster, Standbilder und Profilbilder.
 *
 * Die Bildadressen tragen den Medienschlüssel (?key=…). Der lebt beim
 * Server nur im Arbeitsspeicher: Nach einem Neustart des Servers — oder
 * wenn der Fernseher eine Nacht im Standby war — antwortet er auf jeden
 * Schlüssel mit 401. Dann holt dieser Lader einmal einen neuen und
 * versucht dasselbe Bild noch einmal, statt eine Wand leerer Kacheln zu
 * zeigen. Einmal, nicht öfter: Hilft der neue Schlüssel nicht, liegt es
 * nicht am Schlüssel.
 *
 * [client] liefert den Client der App, sobald es einen gibt — gefragt wird
 * erst beim ersten 401, nicht beim Bauen des Laders.
 */
internal fun lumenBildlader(context: Context, client: () -> ApiClient?): ImageLoader =
    ImageLoader.Builder(context)
        .okHttpClient {
            OkHttpClient.Builder()
                .addInterceptor(SchluesselErneuern(client))
                .build()
        }
        .build()

private class SchluesselErneuern(private val client: () -> ApiClient?) : Interceptor {
    override fun intercept(chain: Interceptor.Chain): Response {
        val anfrage = chain.request()
        val antwort = chain.proceed(anfrage)
        if (antwort.code != 401) return antwort
        val c = client() ?: return antwort
        // Läuft schon auf einem Hintergrundfaden des Laders — hier darf
        // blockiert werden.
        val neu = c.nachAbgelaufenemSchluessel(anfrage.url.toString()) ?: return antwort
        antwort.close()
        return chain.proceed(anfrage.newBuilder().url(neu).build())
    }
}
