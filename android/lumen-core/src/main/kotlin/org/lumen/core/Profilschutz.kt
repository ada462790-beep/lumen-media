// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package org.lumen.core

/**
 * Das eigene Profil schützen: PIN, Passwort oder keines — auf Telefon,
 * Tablet und Fernseher gleich. Die Regeln stehen hier, damit beide
 * Oberflächen dasselbe sagen, bevor der Server gefragt wird.
 */
object Profilschutz {
    /** Die Arten, mit dem Namen auf dem Knopf — dieselben wie beim Einladen. */
    val ARTEN: List<Pair<String, String>> get() = Einladung.ARTEN

    /** Wie die Art heisst, für „Geschützt mit …". */
    fun name(art: String): String = when (art) {
        "pin" -> Texte.t(Tx.PIN)
        "password" -> Texte.t(Tx.PASSWORT)
        else -> Texte.t(Tx.KEIN_SCHUTZ)
    }

    /**
     * Null, wenn sich das so abschicken lässt — sonst, was nicht stimmt, in
     * einem Satz. [bisher] ist die Art, die das Profil gerade hat.
     */
    fun pruefen(bisher: String, alt: String, art: String, neu: String, wiederholt: String): String? = when {
        bisher != "none" && alt.isEmpty() ->
            if (bisher == "pin") Texte.t(Tx.ZUERST_BISHERIGER_PIN)
            else Texte.t(Tx.ZUERST_BISHERIGES_PASSWORT)
        art !in Einladung.SCHLUESSEL -> Texte.t(Tx.UNBEKANNTE_SCHUTZART)
        art == "pin" && (neu.length < 4 || !neu.all { it.isDigit() }) -> Texte.t(Tx.PIN_ZU_KURZ)
        art == "password" && neu.length < 8 -> Texte.t(Tx.PASSWORT_ZU_KURZ)
        art != "none" && neu != wiederholt -> Texte.t(Tx.EINGABEN_UNGLEICH)
        else -> null
    }

    /** Was die App zu einer Ablehnung des Servers sagt — am Code erkannt. */
    fun meldung(e: LumenException): String {
        val s = e as? LumenException.Server
        return when {
            s?.code == "falsch" -> Texte.t(Tx.BISHERIGES_FALSCH)
            s?.code == "verwaltung_braucht_sperre" -> Texte.t(Tx.VOLLZUGRIFF_BRAUCHT_SCHUTZ)
            s?.status == 429 -> e.message ?: Texte.t(Tx.ZU_VIELE_VERSUCHE)
            else -> e.message ?: Texte.t(Tx.FEHLER_ALLGEMEIN)
        }
    }

    /** Ob der Server die Schnittstelle (noch) nicht kennt — dann still ausblenden. */
    fun fehltBeimServer(e: LumenException): Boolean =
        (e as? LumenException.Server)?.status == 404 && (e as? LumenException.Server)?.code == null
}
