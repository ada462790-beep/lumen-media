// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package org.lumen.core

/**
 * Was dieses Gerät abspielen kann — und wie es heisst.
 *
 * Der Server entscheidet daraufhin, ob er die Datei direkt schickt oder
 * umwandelt. Lieber einmal zu viel umgewandelt als ein schwarzes Bild.
 *
 * Auf Android fragt [AndroidDeviceProfile] den Mediencodec-Katalog ab und
 * setzt das Ergebnis über [install]. Ohne Android — in Tests oder auf der
 * Kommandozeile — bleibt es bei einer vorsichtigen Annahme.
 */
data class DeviceProfile(
    val name: String,
    val kind: String,          // tv | phone | tablet
    val codecs: List<String>,
    val containers: List<String>,
    val audio: List<String>,
    val maxHeight: Int,
    val hdr: Boolean,
    // Obergrenze für die Bitrate, in Bit/s — gesetzt von der Stufe unter
    // „Qualität" im Player ([Qualitaet.profil]). 0: keine eigene Grenze.
    val maxBitrate: Int = 0,
) {
    fun query(): Map<String, String> = buildMap {
        put("codecs", codecs.joinToString(","))
        put("containers", containers.joinToString(","))
        put("audio", audio.joinToString(","))
        put("maxHeight", maxHeight.toString())
        put("hdr", if (hdr) "1" else "0")
        if (maxBitrate > 0) put("maxBitrate", maxBitrate.toString())
    }

    companion object {
        /** Was praktisch jedes Android-Gerät seit Jahren kann. */
        val fallback = DeviceProfile(
            name = "Android", kind = "phone",
            codecs = listOf("h264"),
            containers = listOf("mp4", "hls"),
            audio = listOf("aac"),
            maxHeight = 1080, hdr = false)

        /**
         * Ein Chromecast mit dem Standard-Empfänger von Google: H.264 bis
         * 1080p, AAC und MP3, MP4 oder HLS. Neuere Geräte können mehr —
         * aber welches gerade dran ist, verrät die Sitzung nicht verlässlich,
         * und ein schwarzes Bild auf dem Fernseher ist schlimmer als einmal
         * zu viel umgewandelt.
         */
        val chromecast = DeviceProfile(
            name = "Chromecast", kind = "tv",
            codecs = listOf("h264"),
            containers = listOf("mp4", "hls"),
            audio = listOf("aac", "mp3"),
            maxHeight = 1080, hdr = false)

        @Volatile
        var current: DeviceProfile = fallback
            private set

        fun install(profile: DeviceProfile) { current = profile }

        /**
         * Ohne Umwandlung geht nur, was der Abspieler unmittelbar öffnet.
         * MKV kann ExoPlayer zwar lesen — aber ob der *Decoder* im Gerät den
         * Inhalt kann, entscheidet der Server anhand der Codec-Liste. Hier
         * geht es allein um den Behälter.
         */
        fun playsDirectly(container: String?): Boolean =
            container?.lowercase() in setOf("mp4", "m4v", "mov", "mkv", "webm")
    }
}
