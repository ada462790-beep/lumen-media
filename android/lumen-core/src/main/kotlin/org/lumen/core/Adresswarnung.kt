// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package org.lumen.core

import java.net.URI

/**
 * Unverschlüsselt ins Internet? Im Heimnetz ist http üblich und harmlos —
 * die Apps erlauben es darum für jede Adresse (network_security_config).
 * Zeigt eine http-Adresse aber nach draussen, liefen PIN, Passwort und
 * Token im Klartext durchs Netz. Dann soll die Eingabe das sagen.
 *
 * Nur ein Hinweis, keine Sperre: Wer einen eigenen Namen im Heimnetz hat
 * („nas.fritz.box"), soll trotzdem verbinden können.
 */
object Adresswarnung {

    /** Wahr, wenn [eingabe] über http zu einer Adresse ausserhalb des Heimnetzes führt. */
    fun unverschluesseltAusserhalb(eingabe: String): Boolean {
        val url = ApiClient.normalizeAddress(eingabe) ?: return false
        val uri = try { URI(url) } catch (e: Exception) { return false }
        if (uri.scheme?.lowercase() != "http") return false
        val host = uri.host?.trim('[', ']')?.lowercase()?.trimEnd('.') ?: return false
        return !imHeimnetz(host)
    }

    /**
     * localhost, Namen ohne Punkt („nas"), .local, .lan, .home.arpa,
     * 10/8, 172.16/12, 192.168/16, 169.254/16, 127/8, 100.64/10 (Tailscale
     * und Co.) und die entsprechenden IPv6-Bereiche.
     */
    fun imHeimnetz(host: String): Boolean {
        if (host == "localhost" || (!host.contains('.') && !host.contains(':'))) return true
        if (host.endsWith(".local") || host.endsWith(".lan") || host.endsWith(".home.arpa") ||
            host.endsWith(".localhost")) return true
        if (CastAdresse.istIPv4(host)) {
            val (a, b) = host.split('.').map { it.toInt() }
            return CastAdresse.imHeimnetz(host) || a == 127 || (a == 100 && b in 64..127)
        }
        if (host.contains(':')) {
            // IPv6: Schleife, eigene Netze (fc00::/7), Verbindungsadressen (fe80::/10).
            return host == "::1" || host.startsWith("fc") || host.startsWith("fd") ||
                host.startsWith("fe8") || host.startsWith("fe9") || host.startsWith("fea") || host.startsWith("feb")
        }
        return false
    }
}
