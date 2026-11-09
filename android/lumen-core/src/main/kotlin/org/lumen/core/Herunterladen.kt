// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package org.lumen.core

/**
 * Was GET /items/{id}/download/info sagt: ob diese Person den Titel
 * herunterladen darf, und welche Dateien es gibt — bei mehreren Fassungen
 * mehrere, mit Grösse und Bildhöhe, damit man wählen kann.
 */
data class DownloadInfo(val erlaubt: Boolean, val dateien: List<DownloadDatei>) {
    companion object {
        fun from(j: JsonValue?) = DownloadInfo(
            erlaubt = j["erlaubt"].bool ?: false,
            dateien = j["dateien"].list.mapNotNull { DownloadDatei.from(it) },
        )
    }
}

data class DownloadDatei(
    val fileId: String,
    val dateiname: String,
    /** In Bytes; 0, wenn der Server es nicht weiss. */
    val groesse: Long,
    /** Bildhöhe in Pixeln; 0, wenn unbekannt. */
    val hoehe: Int,
) {
    /** „1080p · 4,2 GB" — für die Wahl zwischen Fassungen. */
    val beschriftung: String
        get() = listOfNotNull(
            hoehe.takeIf { it > 0 }?.let { "${it}p" },
            groesse.takeIf { it > 0 }?.let { Herunterladen.groesse(it) },
        ).joinToString(" · ").ifEmpty { dateiname }

    companion object {
        fun from(j: JsonValue?): DownloadDatei? {
            val id = j["fileId"].text ?: return null
            return DownloadDatei(
                fileId = id,
                dateiname = j["dateiname"].text ?: id,
                groesse = j["groesse"].long ?: 0L,
                hoehe = j["hoehe"].int ?: 0,
            )
        }
    }
}

/**
 * Was ein heruntergeladener Titel über sich weiss — ohne Server. Liegt
 * neben der Datei als kleine JSON-Datei, damit die Liste der Downloads auch
 * im Flugzeug steht und der Stand später zum Server kann.
 */
data class Heruntergeladen(
    val itemId: String,
    val fileId: String,
    val titel: String,
    /** „Staffel 1, Folge 3" oder Jahr — die zweite Zeile in der Liste. */
    val unterzeile: String,
    val dateiname: String,
    val groesse: Long,
    /** fertig, laedt, fehler */
    val zustand: String,
    val geladen: Long = 0,
    val fehler: String? = null,
    /** Der Stand beim Offline-Schauen, in Sekunden. */
    val stelle: Double = 0.0,
    val dauer: Double = 0.0,
    /** Ob [stelle] seit dem letzten Abgleich verändert wurde. */
    val stelleOffen: Boolean = false,
    /** Wer heruntergeladen hat — Downloads gehören zu einem Profil. */
    val kontoId: String = "",
    val serverId: String = "",
) {
    val fertig: Boolean get() = zustand == FERTIG

    /** Wie weit, in Prozent; 0, solange die Grösse unbekannt ist. */
    val prozent: Int get() = if (groesse > 0) ((geladen * 100) / groesse).toInt().coerceIn(0, 100) else 0

    fun alsJson(): String = JsonWriter.obj(
        "itemId" to itemId, "fileId" to fileId, "titel" to titel, "unterzeile" to unterzeile,
        "dateiname" to dateiname, "groesse" to groesse, "zustand" to zustand, "geladen" to geladen,
        "fehler" to fehler, "stelle" to stelle, "dauer" to dauer, "stelleOffen" to stelleOffen,
        "kontoId" to kontoId, "serverId" to serverId)

    companion object {
        const val FERTIG = "fertig"
        const val LAEDT = "laedt"
        const val FEHLER = "fehler"

        fun aus(text: String): Heruntergeladen? = try {
            val j = Json.parse(text)
            Heruntergeladen(
                itemId = j.requiredText("itemId"),
                fileId = j.requiredText("fileId"),
                titel = j["titel"].text ?: "",
                unterzeile = j["unterzeile"].text ?: "",
                dateiname = j["dateiname"].text ?: "",
                groesse = j["groesse"].long ?: 0L,
                zustand = j["zustand"].text ?: FEHLER,
                geladen = j["geladen"].long ?: 0L,
                fehler = j["fehler"].text,
                stelle = j["stelle"].double ?: 0.0,
                dauer = j["dauer"].double ?: 0.0,
                stelleOffen = j["stelleOffen"].bool ?: false,
                kontoId = j["kontoId"].text ?: "",
                serverId = j["serverId"].text ?: "",
            )
        } catch (e: JsonException) {
            null
        }
    }
}

/** Kleine Rechnungen rund ums Herunterladen — ohne Android prüfbar. */
object Herunterladen {
    /**
     * Die ganze Grösse der Datei. Bei einer Fortsetzung (206) steht sie in
     * „Content-Range: bytes 100-999/1000" hinter dem Schrägstrich; sonst
     * ist es die Länge der Antwort plus das, was schon da war.
     */
    fun gesamtGroesse(contentRange: String?, laenge: Long, schon: Long): Long {
        val hinten = contentRange?.substringAfterLast('/', "")?.trim()
        hinten?.toLongOrNull()?.let { if (it > 0) return it }
        return if (laenge > 0) laenge + schon else 0L
    }

    /** „850 MB", „4,2 GB" — mit dem Dezimalzeichen der Sprache. */
    fun groesse(bytes: Long): String {
        val mb = bytes / 1_000_000.0
        return if (mb < 1000) "${Math.round(mb)} MB"
        else Titelseite.komma(mb / 1000) + " GB"
    }

    /**
     * Ein Dateiname, der auf jedem Dateisystem geht: ohne Pfadtrenner und
     * Sonderzeichen, nicht zu lang. Die Endung bleibt.
     */
    fun sichererName(name: String): String {
        val sauber = name.replace(Regex("[^A-Za-z0-9._ -]"), "_").trim().trimStart('.')
        val endung = sauber.substringAfterLast('.', "").take(5)
        val stamm = sauber.substringBeforeLast('.').take(80).ifEmpty { "film" }
        return if (endung.isEmpty()) stamm else "$stamm.$endung"
    }

    /** Die Meldung zu einem abgelehnten Download — vom Code des Servers. */
    fun meldung(e: LumenException): String = when ((e as? LumenException.Server)?.code) {
        "kein_download" -> Texte.t(Tx.DOWNLOAD_NICHT_FREIGEGEBEN)
        "zu_viele_downloads" -> Texte.t(Tx.DOWNLOAD_ZU_VIELE)
        else -> e.message ?: Texte.t(Tx.DOWNLOAD_FEHLER)
    }
}
