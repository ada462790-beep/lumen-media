// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package org.lumen.core

import java.security.MessageDigest
import java.security.cert.CertificateException
import java.security.cert.X509Certificate
import javax.net.ssl.HostnameVerifier
import javax.net.ssl.SSLContext
import javax.net.ssl.SSLSocketFactory
import javax.net.ssl.X509TrustManager

/**
 * Das Zertifikat des eigenen Servers festnageln.
 *
 * Von unterwegs geht alles verschlüsselt — sonst könnte der Vermittler
 * mitlesen. Nur hat ein Heimserver selten einen Namen, für den eine
 * öffentliche Stelle ein Zertifikat ausstellt: er heisst „nas" und steht
 * hinter einer Adresse, die niemandem gehört. Lumen stellt sich deshalb
 * selbst eines aus.
 *
 * Für die App heisst das: die übliche Prüfung schlägt fehl. Die Antwort
 * darauf ist nicht, die Prüfung abzuschalten — dann wäre jede Verbindung
 * angreifbar, und die ganze Verschlüsselung Theater. Die Antwort ist, das
 * eine erwartete Zertifikat zu merken und nur dieses zu akzeptieren.
 *
 * Das ist strenger als das Übliche, nicht lockerer: ein öffentliches
 * Zertifikat akzeptiert jedes, das irgendeine der hundert eingebauten
 * Stellen unterschrieben hat. Hier gilt genau eines.
 *
 * Den Fingerabdruck zeigt der Server in der Verwaltung unter „Von aussen
 * erreichbar". Er wird einmal verglichen und dann gespeichert.
 */
object Pinnen {

    /** SHA-256 eines Zertifikats als Hex — so, wie der Server ihn anzeigt. */
    fun fingerabdruck(cert: X509Certificate): String =
        MessageDigest.getInstance("SHA-256").digest(cert.encoded)
            .joinToString("") { "%02x".format(it) }

    /** Doppelpunkte und Leerzeichen weg, alles klein — damit sich ein
     *  abgetippter Fingerabdruck mit einem kopierten vergleichen lässt. */
    fun saubern(roh: String?): String? =
        roh?.replace(":", "")?.replace(" ", "")?.trim()?.lowercase()
            ?.takeIf { it.length == 64 && it.all { c -> c in "0123456789abcdef" } }

    /**
     * Was eine Verbindung braucht, um nur ein bestimmtes Zertifikat
     * gelten zu lassen.
     *
     * Beides gehört zusammen und wird nur zusammen ausgegeben: die
     * Fabrik, die das Zertifikat prüft, und die Namensprüfung, die
     * danach nichts mehr zu prüfen hat. Getrennt wäre das gefährlich —
     * eine Namensprüfung, die alles durchwinkt, ohne eine Fabrik, die
     * wirklich pinnt, wäre schlimmer als gar kein TLS.
     */
    class Pruefer internal constructor(
        val fabrik: SSLSocketFactory,
        val namenspruefung: HostnameVerifier,
    )

    /**
     * Eine Prüfung, die nur dieses eine Zertifikat gelten lässt.
     *
     * Ist [erwartet] leer oder unbrauchbar, kommt `null` zurück — dann
     * gilt die übliche Prüfung gegen die Wurzelzertifikate des Geräts.
     * Stillschweigend alles durchwinken tut diese Funktion nie.
     */
    fun pruefer(erwartet: String?): Pruefer? {
        val soll = saubern(erwartet) ?: return null

        val vertrauen = object : X509TrustManager {
            override fun checkServerTrusted(kette: Array<X509Certificate>?, typ: String?) {
                val erstes = kette?.firstOrNull()
                    ?: throw CertificateException(Texte.t(Tx.ZERTIFIKAT_FEHLT))
                val ist = fingerabdruck(erstes)
                if (!gleichLang(soll, ist)) {
                    throw AnderesZertifikat()
                }
            }

            // Als Client stellt Lumen nie ein eigenes Zertifikat vor.
            override fun checkClientTrusted(kette: Array<X509Certificate>?, typ: String?) =
                throw CertificateException("hier nicht vorgesehen") // sprache: fest — nie zu sehen

            override fun getAcceptedIssuers(): Array<X509Certificate> = emptyArray()
        }

        val ctx = SSLContext.getInstance("TLS")
        ctx.init(null, arrayOf(vertrauen), java.security.SecureRandom())

        // Die Namensprüfung entfällt — nicht aus Nachlässigkeit, sondern
        // weil sie hier nichts beitragen kann: ein Heimserver heisst
        // „nas" und ist von unterwegs unter der Adresse des Vermittlers
        // erreichbar. Diesen Namen kann sein Zertifikat gar nicht
        // enthalten. Der Fingerabdruck beweist die Gegenstelle genauer,
        // als ein Name es je könnte — er gilt für genau dieses eine
        // Zertifikat und kein anderes.
        //
        // Diese Zeile ist nur deshalb unbedenklich, weil sie untrennbar
        // mit der Prüfung oben ausgeliefert wird. Wer sie je von dort
        // löst, nimmt der Verbindung ihre Sicherheit.
        return Pruefer(ctx.socketFactory, HostnameVerifier { _, _ -> true })
    }

    /**
     * Vergleich in gleichbleibender Zeit.
     *
     * Ein gewöhnlicher Vergleich bricht beim ersten abweichenden Zeichen
     * ab. Wer die Zeit misst, kann daraus Zeichen für Zeichen erraten,
     * was erwartet wird. Bei einem Fingerabdruck ist das weniger kritisch
     * als bei einem Geheimnis — aber es kostet nichts, es richtig zu
     * machen.
     */
    fun gleichLang(a: String, b: String): Boolean {
        if (a.length != b.length) return false
        var unterschied = 0
        for (i in a.indices) unterschied = unterschied or (a[i].code xor b[i].code)
        return unterschied == 0
    }
}

/**
 * Das Zertifikat passt nicht zum gespeicherten Fingerabdruck. Eine eigene
 * Art, damit ApiClient den Fall am Typ erkennt und nicht am Wortlaut —
 * der hängt an der Sprache.
 */
class AnderesZertifikat : CertificateException(Texte.t(Tx.ZERTIFIKAT_ANDERS))
