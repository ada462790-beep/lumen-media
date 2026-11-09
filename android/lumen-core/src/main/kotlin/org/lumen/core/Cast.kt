// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package org.lumen.core

import java.net.URI

/**
 * Auf einen Chromecast schicken: Wie kommt der Chromecast an den Strom?
 *
 * Der Chromecast holt den Film selbst beim Server — das Telefon ist nur
 * die Fernbedienung. Er braucht dafür eine Adresse, die *er* erreicht:
 *  - eine IP-Adresse im Heimnetz (ein Chromecast kennt keine Namen wie
 *    „nas.local" — die löst das Telefon vorher auf),
 *  - kein festgenageltes Zertifikat: das kann der Chromecast nicht prüfen,
 *    und der Vermittler für unterwegs steht ohnehin ausserhalb des Netzes.
 * Geht beides nicht, bleibt Cast aus — mit einem Satz, warum.
 */
object CastAdresse {

    /** Wohin der Chromecast soll — oder warum es nicht geht. */
    sealed interface Ergebnis {
        /** Die Basisadresse, die der Chromecast erreicht: „http://192.168.1.20:8097". */
        data class Im(val basis: String) : Ergebnis
        data class Nicht(val grund: String) : Ergebnis
    }

    val UNTERWEGS: String get() = Texte.t(Tx.CAST_UNTERWEGS)

    val NICHT_IM_NETZ: String get() = Texte.t(Tx.CAST_NICHT_IM_NETZ)

    /**
     * Aus der Adresse des Servers die für den Chromecast machen.
     * [vonUnterwegs] heisst: über den Vermittler mit festgenageltem
     * Zertifikat. [aufloesen] macht aus einem Namen eine IPv4-Adresse
     * (auf Android: InetAddress, im Hintergrund) — null, wenn das nicht geht.
     */
    fun fuer(serverUrl: String, vonUnterwegs: Boolean, aufloesen: (String) -> String?): Ergebnis {
        if (vonUnterwegs) return Ergebnis.Nicht(UNTERWEGS)
        val uri = try { URI(serverUrl) } catch (e: Exception) { return Ergebnis.Nicht(NICHT_IM_NETZ) }
        val host = uri.host?.trim('[', ']') ?: return Ergebnis.Nicht(NICHT_IM_NETZ)
        val ip = if (istIPv4(host)) host else aufloesen(host) ?: return Ergebnis.Nicht(NICHT_IM_NETZ)
        if (!imHeimnetz(ip)) return Ergebnis.Nicht(NICHT_IM_NETZ)
        val schema = uri.scheme ?: "http"
        // Mit https bleibt der Name: das Zertifikat gilt für ihn, nicht für
        // die IP. Ein richtiger DNS-Name löst auch der Chromecast auf.
        if (schema == "https") return Ergebnis.Im(serverUrl.trimEnd('/'))
        val port = if (uri.port > 0) ":${uri.port}" else ""
        return Ergebnis.Im("$schema://$ip$port")
    }

    /** Eine Adresse dieses Servers auf die für den Chromecast umschreiben. */
    fun umschreiben(adresse: String, serverUrl: String, basis: String): String =
        if (adresse.startsWith(serverUrl.trimEnd('/'))) basis + adresse.substring(serverUrl.trimEnd('/').length)
        else adresse

    fun istIPv4(s: String): Boolean {
        val teile = s.split('.')
        return teile.size == 4 && teile.all { t -> t.toIntOrNull()?.let { it in 0..255 } == true && t.isNotEmpty() }
    }

    /** 10/8, 172.16/12, 192.168/16 — und 169.254/16 für Netze ohne Router. */
    fun imHeimnetz(ipv4: String): Boolean {
        if (!istIPv4(ipv4)) return false
        val (a, b) = ipv4.split('.').map { it.toInt() }
        return a == 10 || (a == 172 && b in 16..31) || (a == 192 && b == 168) || (a == 169 && b == 254)
    }

    /** Der MIME-Typ, den der Chromecast für die Adresse braucht. */
    fun inhaltsTyp(direkt: Boolean, container: String?): String = when {
        !direkt -> "application/x-mpegURL"
        container?.lowercase() == "webm" -> "video/webm"
        else -> "video/mp4"
    }
}
