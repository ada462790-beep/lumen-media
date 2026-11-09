// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package org.lumen.app

import android.content.Context
import android.net.Uri
import android.view.ContextThemeWrapper
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.*
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Cast
import androidx.compose.material.icons.filled.Pause
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.ui.viewinterop.AndroidView
import androidx.mediarouter.app.MediaRouteButton
import com.google.android.gms.cast.CastMediaControlIntent
import com.google.android.gms.cast.MediaInfo
import com.google.android.gms.cast.MediaLoadRequestData
import com.google.android.gms.cast.MediaMetadata
import com.google.android.gms.cast.MediaSeekOptions
import com.google.android.gms.cast.MediaStatus
import com.google.android.gms.cast.MediaTrack
import com.google.android.gms.cast.framework.CastButtonFactory
import com.google.android.gms.cast.framework.CastContext
import com.google.android.gms.cast.framework.CastOptions
import com.google.android.gms.cast.framework.CastSession
import com.google.android.gms.cast.framework.CastState
import com.google.android.gms.cast.framework.CastStateListener
import com.google.android.gms.cast.framework.OptionsProvider
import com.google.android.gms.cast.framework.SessionManagerListener
import com.google.android.gms.cast.framework.SessionProvider
import com.google.android.gms.cast.framework.media.RemoteMediaClient
import com.google.android.gms.common.images.WebImage
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.delay
import kotlinx.coroutines.withContext
import org.lumen.core.*
import java.net.Inet4Address
import java.net.InetAddress

/**
 * Die Einstellungen fürs Cast-SDK: der Standard-Empfänger von Google. Ein
 * eigener Empfänger bräuchte eine Anmeldung bei Google und einen Server im
 * Internet — für einen Heimserver genau das Falsche.
 */
class LumenCastOptions : OptionsProvider {
    override fun getCastOptions(context: Context): CastOptions =
        CastOptions.Builder()
            .setReceiverApplicationId(CastMediaControlIntent.DEFAULT_MEDIA_RECEIVER_APPLICATION_ID)
            .build()

    override fun getAdditionalSessionProviders(context: Context): List<SessionProvider>? = null
}

/**
 * Alles rund um den Chromecast an einer Stelle.
 *
 * Wichtig: Der Chromecast holt den Film selbst beim Server. Er bekommt
 * dafür eine Adresse mit dem Medienschlüssel — nie das Token — und mit der
 * IP des Servers im Heimnetz (siehe [CastAdresse]). Den Stand meldet das
 * Telefon, solange es verbunden ist, wie bei jeder anderen Wiedergabe.
 */
internal object CastHilfe {

    /** Der Cast-Zusammenhang — null, wenn es keine Google-Dienste gibt (etwa auf einem Gerät ohne Play Store). */
    fun kontext(context: Context): CastContext? = try {
        @Suppress("DEPRECATION")
        CastContext.getSharedInstance(context.applicationContext)
    } catch (e: Exception) {
        // Ohne Play-Dienste wirft das SDK sehr verschiedene Ausnahmen.
        // Dann gibt es eben kein Cast; Koroutinen gibt es hier keine.
        null
    }

    fun sitzung(context: Context): CastSession? =
        kontext(context)?.sessionManager?.currentCastSession?.takeIf { it.isConnected }

    /** Was gerade auf dem Chromecast läuft — für die Meldung des Stands. */
    private var laufend: Laufend? = null

    private class Laufend(
        val itemId: String,
        val vorher: Double,
        val gesamt: Double,
        val letzterTeil: Boolean,
        val client: RemoteMediaClient,
        val hoerer: RemoteMediaClient.ProgressListener,
    )

    /** Den Stand nicht mehr melden — beim nächsten Titel oder am Ende. */
    fun meldenBeenden() {
        laufend?.let { it.client.removeProgressListener(it.hoerer) }
        laufend = null
    }

    /**
     * Den Titel auf den verbundenen Chromecast schicken. Gibt null zurück,
     * wenn es klappt — sonst, warum nicht, in einem Satz.
     */
    suspend fun abspielen(context: Context, model: LumenViewModel, start: Abspielen): String? {
        val session = sitzung(context) ?: return tx(R.string.kein_chromecast_verbunden)
        val rmc = session.remoteMediaClient ?: return tx(R.string.der_chromecast_antwortet_nicht)
        val client = model.client
        val basis = model.castBasis() ?: return CastAdresse.NICHT_IM_NETZ
        if (basis is CastAdresse.Ergebnis.Nicht) return basis.grund
        val ziel = (basis as CastAdresse.Ergebnis.Im).basis

        // Datei, Teil und Plan — für den Chromecast, nicht für das Telefon.
        val vorbereitet = model.abfragen { c ->
            val d = c.item(start.titel.id)
            val erste = Fassungen.datei(d.files, start.dateiId) ?: return@abfragen null
            val alle = Fassungen.teile(d.files, erste)
            // Der Stand frisch vom Server: eben noch lief der Film vielleicht hier.
            val stelle = alle.stelle(start.ab ?: d.item.resumeSeconds)
            val f = alle.dateien[stelle.teil]
            val plan = c.plan(f.id, DeviceProfile.chromecast)
            // Ein frischer Schlüssel: der Film läuft Stunden.
            c.wiedergabeSchluesselErneuern()
            Vorbereitet(d, f, plan, stelle, alle)
        }
        val v = vorbereitet.wert ?: return vorbereitet.fehler ?: tx(R.string.zu_diesem_titel_gibt_es)
        val wahl = Spuren.vorbelegen(v.datei.tracks, model.me?.prefs ?: emptyMap())
        val direkt = client.istDirekt(v.datei, v.plan, DeviceProfile.chromecast)
        val adresse = CastAdresse.umschreiben(
            client.streamUrl(v.datei, v.plan, wahl.ton, DeviceProfile.chromecast), client.baseUrl, ziel)

        val titel = v.detail.item
        val meta = MediaMetadata(if (titel.kind == "episode") MediaMetadata.MEDIA_TYPE_TV_SHOW
                                 else MediaMetadata.MEDIA_TYPE_MOVIE).apply {
            putString(MediaMetadata.KEY_TITLE, titel.title)
            client.wideUrl(titel)?.let { addImage(WebImage(Uri.parse(CastAdresse.umschreiben(it, client.baseUrl, ziel)))) }
        }
        // Untertitel als WebVTT — der Chromecast lädt sie selbst.
        val texte = Spuren.text(v.datei.tracks)
        val spuren = texte.map { t ->
            MediaTrack.Builder(t.index.toLong() + 1, MediaTrack.TYPE_TEXT)
                .setContentId(CastAdresse.umschreiben(client.subtitleUrl(v.datei.id, t.index), client.baseUrl, ziel))
                .setContentType("text/vtt")
                .setSubtype(MediaTrack.SUBTYPE_SUBTITLES)
                .setName(t.anzeigename)
                .setLanguage(t.language?.takeIf { it.isNotBlank() && it != "und" })
                .build()
        }
        val info = MediaInfo.Builder(adresse)
            .setStreamType(MediaInfo.STREAM_TYPE_BUFFERED)
            .setContentType(CastAdresse.inhaltsTyp(direkt, v.datei.container))
            .setMetadata(meta)
            .setMediaTracks(spuren)
            .build()
        var ab = v.stelle.imTeil
        val dauer = if (v.datei.durationSeconds > 0) v.datei.durationSeconds else titel.runtimeSeconds
        if (dauer > 0 && ab > dauer - 5) ab = 0.0
        val anfrage = MediaLoadRequestData.Builder()
            .setMediaInfo(info)
            .setAutoplay(true)
            .setCurrentTime((ab * 1000).toLong())
            .apply { wahl.untertitel?.let { setActiveTrackIds(longArrayOf(it.toLong() + 1)) } }
            .build()

        meldenBeenden()
        rmc.load(anfrage)

        // Den Stand melden, solange der Chromecast spielt — alle zehn Sekunden.
        val vorher = v.stelle.vorher
        val gesamt = v.teile.takeIf { it.mehrere }?.gesamtSekunden?.takeIf { it > 0 } ?: dauer
        val letzter = v.teile.istLetzter(v.stelle.teil)
        var gesehenGemeldet = false
        val hoerer = RemoteMediaClient.ProgressListener { fortschritt, laenge ->
            if (fortschritt <= 0 || laenge <= 0) return@ProgressListener
            val stelle = vorher + fortschritt / 1000.0
            val ganz = if (gesamt > 0) gesamt else laenge / 1000.0
            model.send { it.saveProgress(titel.id, stelle, ganz) }
            if (letzter && !gesehenGemeldet && stelle / ganz >= SessionController.FINISHED_AT) {
                gesehenGemeldet = true
                model.send { it.markFinished(titel.id) }
            }
        }
        rmc.addProgressListener(hoerer, 10_000)
        laufend = Laufend(titel.id, vorher, gesamt, letzter, rmc, hoerer)
        return null
    }

    private class Vorbereitet(
        val detail: ItemDetail,
        val datei: MediaFile,
        val plan: PlaybackPlan,
        val stelle: FilmTeile.Stelle,
        val teile: FilmTeile,
    )

    /**
     * Die Adresse, unter der der Chromecast den Server erreicht — einmal je
     * Server ermittelt (ein Name wird dafür aufgelöst, also im Hintergrund).
     */
    suspend fun basisErmitteln(serverUrl: String, vonUnterwegs: Boolean): CastAdresse.Ergebnis =
        withContext(Dispatchers.IO) {
            CastAdresse.fuer(serverUrl, vonUnterwegs) { name ->
                try {
                    InetAddress.getAllByName(name).firstOrNull { it is Inet4Address }?.hostAddress
                } catch (e: java.io.IOException) {
                    null
                }
            }
        }
}

/**
 * Der Cast-Knopf — auf der Titelseite und im Player. Steht nur da, wenn
 * ein Chromecast im Netz ist. Ist der Server von dort nicht erreichbar
 * (Fernzugriff über den Vermittler), steht er ausgegraut da und sagt beim
 * Antippen, warum.
 */
@Composable
fun CastKnopf(model: LumenViewModel, modifier: Modifier = Modifier) {
    val context = LocalContext.current
    val cast = remember { CastHilfe.kontext(context) } ?: return
    var zustand by remember { mutableStateOf(cast.castState) }
    var hinweis by remember { mutableStateOf(false) }
    DisposableEffect(cast) {
        val hoerer = CastStateListener { zustand = it }
        cast.addCastStateListener(hoerer)
        onDispose { cast.removeCastStateListener(hoerer) }
    }
    LaunchedEffect(model.client.baseUrl) { model.castBasisErmitteln() }
    if (zustand == CastState.NO_DEVICES_AVAILABLE) return

    when (val basis = model.castAdresse) {
        is CastAdresse.Ergebnis.Nicht -> {
            IconButton(onClick = { hinweis = true }, modifier = modifier) {
                Icon(Icons.Filled.Cast, tx(R.string.chromecast_hier_nicht_verfuegbar), tint = Lumen.label3)
            }
            if (hinweis) {
                AlertDialog(
                    onDismissRequest = { hinweis = false },
                    title = { Text(tx(R.string.chromecast_nicht_verfuegbar)) },
                    text = { Text(basis.grund) },
                    confirmButton = { TextButton(onClick = { hinweis = false }) { Text(tx(R.string.verstanden)) } },
                )
            }
        }
        is CastAdresse.Ergebnis.Im -> AndroidView(
            factory = { ctx ->
                // Der Knopf will ein AppCompat-Thema — das der App ist keines.
                MediaRouteButton(ContextThemeWrapper(ctx, androidx.appcompat.R.style.Theme_AppCompat)).also {
                    CastButtonFactory.setUpMediaRouteButton(ctx.applicationContext, it)
                }
            },
            modifier = modifier.size(48.dp))
        null -> Unit
    }
}

/**
 * Beobachtet, ob ein Chromecast verbunden ist — für das Gerüst, das dann
 * statt des Players die Fernbedienung zeigt. Endet die Sitzung, während
 * auf dem Chromecast etwas lief, ist auch hier Schluss.
 */
@Composable
fun CastBeobachten(model: LumenViewModel) {
    val context = LocalContext.current
    val cast = remember { CastHilfe.kontext(context) } ?: return
    DisposableEffect(cast) {
        model.castVerbunden = cast.sessionManager.currentCastSession?.isConnected == true
        val hoerer = object : SessionManagerListener<CastSession> {
            override fun onSessionStarted(session: CastSession, sessionId: String) { model.castVerbunden = true }
            override fun onSessionResumed(session: CastSession, wasSuspended: Boolean) { model.castVerbunden = true }
            override fun onSessionEnded(session: CastSession, error: Int) { beendet() }
            override fun onSessionSuspended(session: CastSession, reason: Int) { }
            override fun onSessionStarting(session: CastSession) { }
            override fun onSessionStartFailed(session: CastSession, error: Int) { }
            override fun onSessionEnding(session: CastSession) { }
            override fun onSessionResuming(session: CastSession, sessionId: String) { }
            override fun onSessionResumeFailed(session: CastSession, error: Int) { }

            private fun beendet() {
                model.castVerbunden = false
                CastHilfe.meldenBeenden()
                // Lief etwas auf dem Fernseher, ist es jetzt vorbei — nicht
                // stillschweigend auf dem Telefon weiterspielen.
                if (model.aufCast) { model.aufCast = false; model.abspielen = null }
            }
        }
        cast.sessionManager.addSessionManagerListener(hoerer, CastSession::class.java)
        onDispose { cast.sessionManager.removeSessionManagerListener(hoerer, CastSession::class.java) }
    }
}

/**
 * Die Fernbedienung, solange ein Titel auf dem Chromecast läuft: was
 * läuft, auf welchem Gerät, Pause, Spulen und „Beenden". Lässt sich der
 * Titel nicht schicken, steht hier warum — mit „Auf diesem Gerät abspielen".
 */
@Composable
fun CastWiedergabe(model: LumenViewModel, start: Abspielen, onHier: () -> Unit, onClose: () -> Unit) {
    val context = LocalContext.current
    var fehler by remember { mutableStateOf<String?>(null) }
    var geladen by remember { mutableStateOf(false) }
    var spielt by remember { mutableStateOf(false) }
    var stelle by remember { mutableStateOf(0L) }
    var laenge by remember { mutableStateOf(0L) }
    var ziehen by remember { mutableStateOf<Float?>(null) }
    val geraet = remember { CastHilfe.sitzung(context)?.castDevice?.friendlyName ?: tx(R.string.chromecast) }

    LaunchedEffect(start) {
        model.aufCast = true
        fehler = CastHilfe.abspielen(context, model, start)
        if (fehler != null) model.aufCast = false
        geladen = true
    }
    // Den Zustand vom Chromecast ablesen — einmal pro Sekunde reicht.
    LaunchedEffect(geladen) {
        while (geladen && fehler == null) {
            val rmc = CastHilfe.sitzung(context)?.remoteMediaClient
            if (rmc != null) {
                spielt = rmc.isPlaying || rmc.isBuffering
                stelle = rmc.approximateStreamPosition
                laenge = rmc.streamDuration
                val st = rmc.mediaStatus
                if (st != null && st.playerState == MediaStatus.PLAYER_STATE_IDLE &&
                    st.idleReason == MediaStatus.IDLE_REASON_FINISHED) {
                    CastHilfe.meldenBeenden()
                    model.aufCast = false
                    onClose()
                    return@LaunchedEffect
                }
            }
            delay(1000)
        }
    }
    androidx.activity.compose.BackHandler { onClose() }

    Column(Modifier.fillMaxSize().background(Lumen.background).padding(28.dp),
           verticalArrangement = Arrangement.Center,
           horizontalAlignment = Alignment.CenterHorizontally) {
        Icon(Icons.Filled.Cast, null, tint = Lumen.label2, modifier = Modifier.size(48.dp))
        Spacer(Modifier.height(16.dp))
        Text(start.titel.title, fontSize = 24.sp, fontWeight = FontWeight.Bold, textAlign = TextAlign.Center)
        Spacer(Modifier.height(6.dp))
        val f = fehler
        when {
            f != null -> {
                Text(f, color = Lumen.label2, textAlign = TextAlign.Center)
                Spacer(Modifier.height(20.dp))
                PrimaryButton(tx(R.string.auf_diesem_geraet_abspielen), onClick = onHier)
                Spacer(Modifier.height(10.dp))
                QuietButton(tx(R.string.zurueck), onClick = onClose)
            }
            !geladen -> CircularProgressIndicator(color = Lumen.label2)
            else -> {
                Text(tx(R.string.laeuft_auf_x, geraet), color = Lumen.label2)
                Spacer(Modifier.height(24.dp))
                if (laenge > 0) {
                    Slider(
                        value = ziehen ?: (stelle.toFloat() / laenge).coerceIn(0f, 1f),
                        onValueChange = { ziehen = it },
                        onValueChangeFinished = {
                            val ziel = ((ziehen ?: 0f) * laenge).toLong()
                            CastHilfe.sitzung(context)?.remoteMediaClient
                                ?.seek(MediaSeekOptions.Builder().setPosition(ziel).build())
                            stelle = ziel
                            ziehen = null
                        },
                        colors = SliderDefaults.colors(thumbColor = Color.White, activeTrackColor = Color.White),
                        modifier = Modifier.widthIn(max = 520.dp))
                    Row(Modifier.widthIn(max = 520.dp).fillMaxWidth()) {
                        Text(clock((((ziehen ?: -1f).takeIf { it >= 0 }?.times(laenge))?.toLong() ?: stelle) / 1000.0),
                             color = Lumen.label2, fontSize = 13.sp)
                        Spacer(Modifier.weight(1f))
                        Text(clock(laenge / 1000.0), color = Lumen.label2, fontSize = 13.sp)
                    }
                }
                Spacer(Modifier.height(16.dp))
                IconButton(onClick = {
                    val rmc = CastHilfe.sitzung(context)?.remoteMediaClient
                    if (spielt) rmc?.pause() else rmc?.play()
                    spielt = !spielt
                }, modifier = Modifier.size(72.dp)) {
                    Icon(if (spielt) Icons.Filled.Pause else Icons.Filled.PlayArrow,
                         if (spielt) tx(R.string.pause) else tx(R.string.abspielen), tint = Color.White, modifier = Modifier.size(56.dp))
                }
                Spacer(Modifier.height(16.dp))
                QuietButton(tx(R.string.beenden)) {
                    CastHilfe.sitzung(context)?.remoteMediaClient?.stop()
                    CastHilfe.meldenBeenden()
                    model.aufCast = false
                    onClose()
                }
                Spacer(Modifier.height(8.dp))
                CastKnopf(model)
            }
        }
    }
}
