// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package org.lumen.core

/** Eine Sprache, wie der Server sie anbietet: Kennung („en-GB") und Name. */
data class Sprache(val id: String, val name: String)

/**
 * Welche Sprachen die Apps können und wie sie aus den Antworten des
 * Servers gelesen werden.
 *
 * GET /hello nennt die Vorgabe des Servers („sprache") und die Liste
 * („sprachen"), GET /me die Sprache, die für das Profil gilt. Die eigene
 * Wahl steht in den Prefs unter [PREF] — leer heisst: wie der Server.
 * Ein älterer Server schickt nichts davon; dann bleibt die App bei der
 * zuletzt gültigen Sprache, anfangs der des Geräts.
 */
object Sprachen {
    /** Der Schlüssel in den Prefs (PATCH /me). */
    const val PREF = "sprache"

    /** Was die Apps übersetzt mitbringen — in dieser Reihenfolge zur Wahl. */
    val UNTERSTUETZT = listOf("de-CH", "de-DE", "de-AT", "en-US", "en-GB")

    /**
     * Die Namen in der eigenen Sprache, damit jeder seine findet — auch
     * wenn die Oberfläche gerade eine andere spricht.
     */
    val NAMEN = mapOf(
        "de-CH" to "Deutsch (Schweiz)",
        "de-DE" to "Deutsch (Deutschland)",
        "de-AT" to "Deutsch (Österreich)",
        "en-US" to "English (United States)",
        "en-GB" to "English (United Kingdom)",
    )

    /**
     * Eine Kennung auf eine der unterstützten bringen: „de_ch", „DE-ch"
     * werden „de-CH". Was die Apps nicht können, ergibt null.
     */
    fun normalisieren(tag: String?): String? {
        val t = tag?.trim()?.replace('_', '-') ?: return null
        if (t.isEmpty()) return null
        return UNTERSTUETZT.firstOrNull { it.equals(t, ignoreCase = true) }
    }

    /** Die Sprache aus einer Antwort (/hello oder /me) — oder null. */
    fun wirksam(j: JsonValue?): String? = j["sprache"].text?.trim()?.takeIf { it.isNotEmpty() }

    /** Die Liste aus /hello; fehlt sie, die fünf, die die Apps kennen. */
    fun liste(j: JsonValue?): List<Sprache> =
        j["sprachen"].list.mapNotNull { e ->
            val id = e["id"].text?.trim().orEmpty()
            if (id.isEmpty()) null else Sprache(id, e["name"].text?.trim()?.takeIf { it.isNotEmpty() } ?: NAMEN[id] ?: id)
        }

    /** Die Wahl für die Einstellungen: was der Server kennt und die Apps können. */
    fun wahl(vomServer: List<Sprache>): List<Sprache> {
        val bekannt = vomServer.filter { normalisieren(it.id) != null }
        return bekannt.ifEmpty { UNTERSTUETZT.map { Sprache(it, NAMEN.getValue(it)) } }
    }
}
