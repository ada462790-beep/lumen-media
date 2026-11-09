// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package org.lumen.core

/**
 * Anmelden mit Code — vor allem für den Fernseher, wo Tippen mit der
 * Fernbedienung eine Qual ist.
 *
 * 1. Der Fernseher holt einen Code ([ApiClient.codeStarten]) und zeigt ihn
 *    gross an: „482 913".
 * 2. Am Handy oder im Browser gibt jemand, der schon angemeldet ist, den
 *    Code ein ([ApiClient.codeFreigeben]) und gibt damit sein eigenes
 *    Profil für dieses Gerät frei.
 * 3. Der Fernseher fragt alle paar Sekunden nach ([ApiClient.codeAbfragen])
 *    und bekommt, sobald freigegeben, ein eigenes Geräte-Token.
 *
 * Abgeholt wird das Token mit [geheim], nicht mit dem Code. Wer den Code am
 * Bildschirm abliest, kann damit nur sein eigenes Profil freigeben.
 */
data class AnmeldeCode(
    val code: String,
    val geheim: String,
    val gueltigSek: Int,
) {
    /** „482 913" — in zwei Dreiergruppen liest man ihn leichter ab. */
    val anzeige: String get() = Anmeldecode.anzeige(code)

    companion object {
        fun from(j: JsonValue?) = AnmeldeCode(
            code = j.requiredText("code"),
            geheim = j.requiredText("geheim"),
            gueltigSek = j["gueltigSek"].int ?: 300,
        )
    }
}

/** Was beim Nachfragen herauskam. */
enum class CodeStand {
    /** Noch niemand hat den Code eingegeben. */
    WARTET,

    /** Abgelaufen oder schon abgeholt — ein neuer Code muss her. */
    ABGELAUFEN,

    /** Freigegeben: das Token ist da, die App ist angemeldet. */
    ANGEMELDET,

    /** Gerade kein Durchkommen — beim nächsten Mal wieder versuchen. */
    FEHLER,
}

object Anmeldecode {
    /** So lange Pause zwischen zwei Nachfragen. */
    const val NACHFRAGEN_MS = 3_000L

    /** So viele Ziffern hat ein Code. */
    const val LAENGE = 6

    /** Nur die Ziffern, höchstens sechs — so tippt es sich am Handy. */
    fun saubern(eingabe: String): String = eingabe.filter { it in '0'..'9' }.take(LAENGE)

    fun vollstaendig(eingabe: String): Boolean = saubern(eingabe).length == LAENGE

    /** „482913" → „482 913". */
    fun anzeige(code: String): String {
        val z = code.filter { it in '0'..'9' }
        return if (z.length == LAENGE) z.substring(0, 3) + " " + z.substring(3) else z
    }

    /** Die verbleibende Zeit als „4:59" — nie weniger als „0:00". */
    fun rest(sekunden: Long): String {
        val s = sekunden.coerceAtLeast(0)
        return "%d:%02d".format(s / 60, s % 60)
    }

    /** Die Zeile unter dem Code. */
    fun restText(sekunden: Long): String =
        if (sekunden <= 0) Texte.t(Tx.CODE_ABGELAUFEN)
        else Texte.t(Tx.CODE_NOCH_GUELTIG, rest(sekunden))

    /** Was der Fernseher dazu sagt. */
    val ERKLAERUNG: String get() = Texte.t(Tx.CODE_ERKLAERUNG)

    /** Was das Handy sagt, wenn es sich selbst so anmeldet. */
    val ERKLAERUNG_HANDY: String get() = Texte.t(Tx.CODE_ERKLAERUNG_HANDY)

    /** Die Meldung nach dem Freigeben. */
    fun angemeldetText(geraet: String): String =
        Texte.t(Tx.GERAET_ANGEMELDET, geraet.ifBlank { Texte.t(Tx.DAS_GERAET) })
}
