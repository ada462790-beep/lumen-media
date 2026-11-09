// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package org.lumen.app

import org.lumen.texte.Sprachwahl
import android.app.Application
import android.content.Context
import android.content.SharedPreferences
import android.content.pm.PackageManager
import android.media.MediaCodecList
import android.os.Build
import android.util.DisplayMetrics
import android.view.WindowManager
import androidx.security.crypto.EncryptedSharedPreferences
import androidx.security.crypto.MasterKey
import coil.ImageLoader
import coil.ImageLoaderFactory
import org.lumen.core.ApiClient
import org.lumen.core.DeviceProfile
import org.lumen.core.MemorySchluesselablage
import org.lumen.core.Schluesselablage
import org.lumen.core.Serverliste

class LumenApplication : Application(), ImageLoaderFactory {
    /** Die gespeicherten Server — und zugleich Adresse und Token des gewählten. */
    lateinit var store: Serverliste
        private set

    /**
     * Ein Hinweis, falls die Anmeldungen nicht wie gewohnt verschlüsselt
     * gespeichert werden konnten — die Oberfläche zeigt ihn einmal an.
     * Null heisst: alles in Ordnung.
     */
    @Volatile var speicherHinweis: String? = null

    /**
     * Der Client der laufenden Sitzung — gesetzt von der Activity. Der
     * Bildlader fragt ihn nach einem neuen Medienschlüssel, wenn ein Bild
     * mit 401 zurückkommt.
     */
    @Volatile var client: ApiClient? = null

    override fun newImageLoader(): ImageLoader = lumenBildlader(this) { client }

    override fun onConfigurationChanged(newConfig: android.content.res.Configuration) {
        super.onConfigurationChanged(newConfig)
        Sprachwahl.konfigurationGeaendert(this)
    }

    override fun onCreate() {
        super.onCreate()
        // Zuerst die Sprache: alles danach, das Text braucht, soll ihn
        // schon in der richtigen bekommen.
        Sprachwahl.einrichten(this)
        val sicher = sichereEinstellungen(this)
        speicherHinweis = sicher.hinweis
        val prefs = sicher.prefs
        store = if (prefs != null) {
            // Was eine frühere Fassung im Notfall unverschlüsselt in
            // „lumen" abgelegt hat, wandert hier in die verschlüsselte
            // Ablage und wird dort gelöscht.
            Serverliste(PrefsAblage(prefs),
                        alt = PrefsAblage(getSharedPreferences("lumen", Context.MODE_PRIVATE)))
        } else {
            Serverliste(MemorySchluesselablage())
        }
        DeviceProfile.install(detectDeviceProfile(this))
    }
}

/** Die verschlüsselte Ablage — oder keine, und was die Person wissen muss. */
internal class SichereAblage(val prefs: SharedPreferences?, val hinweis: String?)

/**
 * Wo die Serverliste und die Tokens liegen.
 *
 * Ein Token ist der Schlüssel zu einem Server. In gewöhnlichen
 * Einstellungen stünde es im Klartext in einer Datei — darum verschlüsselt,
 * mit einem Schlüssel aus dem Keystore des Geräts.
 *
 * Früher fiel Lumen beim kleinsten Fehler still auf gewöhnliche
 * Einstellungen zurück, und die Tokens lagen unbemerkt im Klartext. Jetzt:
 *  1. Lässt sich die Datei nicht mehr entschlüsseln — meist, weil sie auf
 *     ein anderes Gerät kam, der Schlüssel im Keystore aber nicht —, wird
 *     sie samt Schlüssel verworfen und neu angelegt. Die Anmeldungen sind
 *     dann weg, und das sagt die App auch.
 *  2. Geht selbst das nicht, merkt sich Lumen die Anmeldung nur im
 *     Arbeitsspeicher. Unbequem (nach einem Neustart neu anmelden), aber
 *     nie ein Token im Klartext — und auch das sagt die App.
 */
internal fun sichereEinstellungen(context: Context): SichereAblage {
    fun oeffnen(): SharedPreferences {
        val key = MasterKey.Builder(context)
            .setKeyScheme(MasterKey.KeyScheme.AES256_GCM)
            .build()
        return EncryptedSharedPreferences.create(
            context, SICHERE_DATEI, key,
            EncryptedSharedPreferences.PrefKeyEncryptionScheme.AES256_SIV,
            EncryptedSharedPreferences.PrefValueEncryptionScheme.AES256_GCM)
    }
    // Breit gefangen, mit Absicht: Tink und der Keystore werfen je nach
    // Gerät GeneralSecurityException, IOException, aber auch
    // SecurityException oder ProviderException. Koroutinen gibt es hier
    // keine, ein Abbruch kann also nicht verschluckt werden.
    try {
        return SichereAblage(oeffnen(), null)
    } catch (e: Exception) {
        // Unten: verwerfen und neu anlegen.
    }
    return try {
        context.deleteSharedPreferences(SICHERE_DATEI)
        java.security.KeyStore.getInstance("AndroidKeyStore").apply { load(null) }
            .deleteEntry(MasterKey.DEFAULT_MASTER_KEY_ALIAS)
        SichereAblage(oeffnen(), NEU_ANGELEGT)
    } catch (e: Exception) {
        SichereAblage(null, NUR_IM_SPEICHER)
    }
}

private const val SICHERE_DATEI = "lumen-secure"

private val NEU_ANGELEGT: String get() =
    tx(R.string.deine_gespeicherten_anmeldungen_liessen)

private val NUR_IM_SPEICHER: String get() =
    tx(R.string.dieses_geraet_kann_deine_anmeldung)

/** Die Schlüssel-Wert-Ablage des Kerns über Android-Einstellungen. */
private class PrefsAblage(private val prefs: SharedPreferences) : Schluesselablage {
    override fun lesen(schluessel: String): String? = prefs.getString(schluessel, null)

    // commit statt apply: die Liste wird gleich danach wieder gelesen, und
    // ein Token, das erst später auf der Platte landet, wäre nach einem
    // Absturz verloren. Aufgerufen wird das ohnehin im Hintergrund.
    override fun schreiben(schluessel: String, wert: String?) {
        prefs.edit().apply {
            if (wert == null) remove(schluessel) else putString(schluessel, wert)
        }.commit()
    }
}

/**
 * Was dieses Gerät wirklich abspielen kann.
 *
 * Gefragt wird der Codec-Katalog des Geräts, nicht geraten. Ein billiges
 * Fernsehgerät ohne HEVC bekäme sonst 4K-Material und zeigte nichts.
 */
private fun detectDeviceProfile(context: Context): DeviceProfile {
    val codecs = mutableListOf("h264")   // kann wirklich jedes Android-Gerät
    try {
        val list = MediaCodecList(MediaCodecList.REGULAR_CODECS)
        val typen = list.codecInfos
            .filter { !it.isEncoder }
            .flatMap { it.supportedTypes.asList() }
            .map { it.lowercase() }
            .toSet()
        if ("video/hevc" in typen) codecs.add("hevc")
        if ("video/av01" in typen) codecs.add("av1")
        if ("video/x-vnd.on2.vp9" in typen) codecs.add("vp9")
    } catch (e: Exception) {
        // Bleibt bei H.264 — das ist nie falsch, nur manchmal unnötig sparsam.
    }

    val tv = context.packageManager.hasSystemFeature(PackageManager.FEATURE_LEANBACK)
    val metrics = DisplayMetrics().also { m ->
        @Suppress("DEPRECATION")
        (context.getSystemService(Context.WINDOW_SERVICE) as WindowManager)
            .defaultDisplay.getRealMetrics(m)
    }
    val seite = maxOf(metrics.widthPixels, metrics.heightPixels)
    val kurz = minOf(metrics.widthPixels, metrics.heightPixels)

    // Mehr als der Bildschirm hergibt, bringt nichts — es kostet nur
    // Leitung und lässt den Server unnötig rechnen. Ein Fernseher bekommt
    // 4K, auch wenn Android die Fläche kleiner meldet.
    val maxHeight = when {
        tv -> 2160
        kurz >= 1440 -> 2160
        else -> 1080
    }

    val tablet = !tv && seite >= 1600 && kurz >= 1000

    return DeviceProfile(
        name = geraeteName(tv),
        kind = if (tv) "tv" else if (tablet) "tablet" else "phone",
        codecs = codecs,
        // ExoPlayer liest Matroska und WebM; ob der *Inhalt* läuft,
        // entscheidet die Codec-Liste oben.
        containers = listOf("mp4", "mkv", "webm", "hls"),
        audio = listOf("aac", "mp3", "opus", "flac"),
        maxHeight = maxHeight,
        // Die Abfrage auf Android 7 stand hier einmal — bei minSdk 26
        // ist sie immer wahr und sagt dem Lesenden nur etwas Falsches.
        hdr = tv || kurz >= 1440,
    )
}

private fun geraeteName(tv: Boolean): String {
    val marke = Build.MANUFACTURER.replaceFirstChar { it.uppercase() }
    val modell = Build.MODEL
    val name = if (modell.startsWith(marke, ignoreCase = true)) modell else "$marke $modell"
    return name.ifBlank { if (tv) "Android TV" else "Android" }
}
