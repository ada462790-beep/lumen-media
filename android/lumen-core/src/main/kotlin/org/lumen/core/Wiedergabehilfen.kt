// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package org.lumen.core

/**
 * Eine laufende HLS-Sitzung des Servers. Der Abspieler holt die
 * Abspielliste selbst; die Kennung steht in der Adresse der Medienliste
 * (`…/stream/{datei}/hls/{sitzung}/media.m3u8`). Wie HLSSitzung auf Apple.
 */
data class HlsSitzung(val fileId: String, val kennung: String) {
    companion object {
        /**
         * Die Kennung aus einer Adresse — null, wenn sie nicht zu dieser
         * Datei gehört oder nur die Hauptliste ist.
         */
        fun kennung(adresse: String, fileId: String): String? {
            val pfad = adresse.substringBefore('?').substringBefore('#')
            val teile = pfad.split('/').filter { it.isNotEmpty() }
            val i = teile.indexOf("stream")
            if (i < 0 || i + 3 >= teile.size) return null
            if (teile[i + 1] != fileId || teile[i + 2] != "hls") return null
            val kennung = teile[i + 3]
            if (kennung.isEmpty() || kennung.endsWith(".m3u8")) return null
            return kennung
        }
    }

    /**
     * Die Sitzung beim Server beenden — ohne zu warten, in einem eigenen
     * Faden: Der Player ist dann schon weg, und die Ansicht vielleicht auch.
     * Fehler zählen nicht; spätestens nach zwei Minuten räumt der Server
     * selbst auf.
     */
    fun beendenImHintergrund(client: ApiClient) {
        val t = Thread {
            try { client.hlsBeenden(fileId, kennung) } catch (e: LumenException) { /* räumt der Server selbst auf */ }
        }
        t.isDaemon = true
        t.name = "lumen-hls-beenden"
        t.start()
    }
}

/**
 * Wie oft ein Player nach einer Untertitelspur fragt, die der Server noch
 * aus der Datei zieht. Je Frage wartet der Server bis zu 20 Sekunden, dann
 * kommen 3 Sekunden Pause — zusammen gut zehn Minuten.
 */
const val UNTERTITEL_VERSUCHE = 30
