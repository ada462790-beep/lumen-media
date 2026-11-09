// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package org.lumen.app

import android.app.Activity
import android.app.PictureInPictureParams
import android.content.Context
import android.content.ContextWrapper
import android.content.pm.ActivityInfo
import android.graphics.drawable.BitmapDrawable
import android.util.Rational
import android.view.KeyEvent
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.ImageBitmap
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.IntOffset
import androidx.compose.ui.unit.IntSize
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.core.app.PictureInPictureModeChangedInfo
import androidx.core.util.Consumer
import androidx.core.view.WindowCompat
import androidx.core.view.WindowInsetsCompat
import androidx.core.view.WindowInsetsControllerCompat
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.LifecycleEventObserver
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.media3.common.PlaybackException
import androidx.media3.common.Player
import coil.imageLoader
import coil.request.ImageRequest
import coil.request.SuccessResult
import org.lumen.core.Trickplay
import org.lumen.core.clock

/** Die Activity hinter einem Context — Compose reicht oft nur einen Wrapper. */
internal fun Context.activity(): Activity? {
    var c: Context? = this
    while (c is ContextWrapper) {
        if (c is Activity) return c
        c = c.baseContext
    }
    return null
}

/**
 * Vollbild fürs Abspielen: Statusleiste und Navigation weg (sie kommen mit
 * einem Wisch vom Rand kurz zurück), und das Bild quer — gedreht wird mit
 * dem Sensor, also in beide Richtungen. Beim Verlassen kommt alles wieder,
 * wie es war.
 */
@Composable
internal fun Vollbild() {
    val activity = LocalContext.current.activity() ?: return
    DisposableEffect(activity) {
        val fenster = activity.window
        val vorher = activity.requestedOrientation
        activity.requestedOrientation = ActivityInfo.SCREEN_ORIENTATION_SENSOR_LANDSCAPE
        val steuerung = WindowCompat.getInsetsController(fenster, fenster.decorView)
        steuerung.systemBarsBehavior = WindowInsetsControllerCompat.BEHAVIOR_SHOW_TRANSIENT_BARS_BY_SWIPE
        steuerung.hide(WindowInsetsCompat.Type.systemBars())
        // Solange der Film läuft, geht der Bildschirm nicht aus.
        fenster.addFlags(android.view.WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)
        onDispose {
            steuerung.show(WindowInsetsCompat.Type.systemBars())
            fenster.clearFlags(android.view.WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)
            activity.requestedOrientation = vorher
        }
    }
}

/**
 * Was beim Verlassen der App mit der Wiedergabe geschieht.
 *
 * Bewusst so: Lumen spielt nicht im Hintergrund weiter. Ein Film ohne Bild
 * ist selten gewollt, und ein vergessener Strom kostet Akku, Datenvolumen
 * und — wenn umgewandelt wird — die Rechenzeit des Servers. Also: Geht die
 * App in den Hintergrund (ON_STOP), hält der Film an, und der Stand wird
 * gesichert. Im Bild-im-Bild-Fenster bleibt die App sichtbar (nur ON_PAUSE)
 * und spielt weiter; schliesst man das Fenster, folgt ON_STOP.
 */
@Composable
internal fun BeimVerlassenAnhalten(player: Player?, onAngehalten: () -> Unit) {
    val besitzer = LocalLifecycleOwner.current
    val angehalten by rememberUpdatedState(onAngehalten)
    DisposableEffect(besitzer, player) {
        val beobachter = LifecycleEventObserver { _, ereignis ->
            if (ereignis == Lifecycle.Event.ON_STOP && player != null) {
                player.pause()
                angehalten()
            }
        }
        besitzer.lifecycle.addObserver(beobachter)
        onDispose { besitzer.lifecycle.removeObserver(beobachter) }
    }
}

/**
 * Bild im Bild: Wer während des Films zum Startbildschirm wechselt, sieht
 * ihn in einem kleinen Fenster weiter. Gibt zurück, ob die App gerade in
 * diesem Fenster läuft — dann gehört die eigene Steuerung weg.
 */
@Composable
internal fun imBildImBild(aktiv: Boolean): Boolean {
    val activity = LocalContext.current.activity() as? MainActivity
    var imFenster by remember { mutableStateOf(activity?.isInPictureInPictureMode == true) }
    DisposableEffect(activity, aktiv) {
        if (activity == null) return@DisposableEffect onDispose { }
        activity.bildImBild = aktiv
        val hoerer = Consumer<PictureInPictureModeChangedInfo> { imFenster = it.isInPictureInPictureMode }
        activity.addOnPictureInPictureModeChangedListener(hoerer)
        onDispose {
            activity.bildImBild = false
            activity.removeOnPictureInPictureModeChangedListener(hoerer)
        }
    }
    return imFenster
}

/** Die Einstellungen fürs Fenster: 16:9, wie fast jeder Film. */
internal fun bildImBildParameter(): PictureInPictureParams =
    PictureInPictureParams.Builder().setAspectRatio(Rational(16, 9)).build()

/**
 * Tastatur und Gamepad: Leertaste, Enter oder A hält an und spielt weiter,
 * links/rechts und die Schultertasten springen zehn Sekunden. Lautstärke
 * bleibt beim System — die Tasten werden nie geschluckt.
 */
@Composable
internal fun Tasten(player: Player?, sprungMs: Long = 10_000) {
    val activity = LocalContext.current.activity() as? MainActivity ?: return
    DisposableEffect(activity, player) {
        val p = player
        activity.tastenGriff = if (p == null) null else griff@{ e ->
            if (e.action != KeyEvent.ACTION_DOWN) return@griff false
            when (e.keyCode) {
                KeyEvent.KEYCODE_SPACE, KeyEvent.KEYCODE_BUTTON_A, KeyEvent.KEYCODE_MEDIA_PLAY_PAUSE,
                KeyEvent.KEYCODE_HEADSETHOOK -> {
                    if (p.isPlaying) p.pause() else p.play(); true
                }
                KeyEvent.KEYCODE_MEDIA_PLAY -> { p.play(); true }
                KeyEvent.KEYCODE_MEDIA_PAUSE -> { p.pause(); true }
                KeyEvent.KEYCODE_DPAD_LEFT, KeyEvent.KEYCODE_BUTTON_L1, KeyEvent.KEYCODE_MEDIA_REWIND -> {
                    p.seekTo((p.currentPosition - sprungMs).coerceAtLeast(0)); true
                }
                KeyEvent.KEYCODE_DPAD_RIGHT, KeyEvent.KEYCODE_BUTTON_R1, KeyEvent.KEYCODE_MEDIA_FAST_FORWARD -> {
                    p.seekTo(p.currentPosition + sprungMs); true
                }
                else -> false
            }
        }
        onDispose { activity.tastenGriff = null }
    }
}

/**
 * Ob ein Fehler des Abspielers ein „410 Gone" auf ein HLS-Stück ist: Nach
 * einer langen Pause hat der Server die Umwandlung aufgeräumt. Das ist kein
 * Fehler für die Person — die Wiedergabe setzt an derselben Stelle mit
 * einem neuen Plan neu an.
 */
@androidx.annotation.OptIn(androidx.media3.common.util.UnstableApi::class)
internal fun hlsAbgelaufen(error: PlaybackException): Boolean =
    generateSequence<Throwable>(error) { it.cause }.any {
        it is androidx.media3.datasource.HttpDataSource.InvalidResponseCodeException && it.responseCode == 410
    }

/**
 * Das Vorschaubild beim Spulen: eine Kachel aus einem Trickplay-Blatt,
 * darunter die Zeit. Die Blätter lädt Coil — mit dem Bildlader der App,
 * der nach einer 401 selbst einen neuen Medienschlüssel holt.
 */
@Composable
internal fun TrickplayVorschau(
    trickplay: Trickplay,
    blattUrl: (Int) -> String?,
    sekunden: Double,
    modifier: Modifier = Modifier,
    breite: Dp = 176.dp,
) {
    val kachel = trickplay.kachel(sekunden)
    val context = LocalContext.current
    val blaetter = remember(trickplay) { mutableStateMapOf<Int, ImageBitmap>() }
    val nr = kachel?.blatt
    LaunchedEffect(trickplay, nr) {
        if (nr == null || blaetter.containsKey(nr)) return@LaunchedEffect
        val url = blattUrl(nr) ?: return@LaunchedEffect
        val ergebnis = context.imageLoader.execute(
            ImageRequest.Builder(context).data(url).allowHardware(false).build())
        val bild = ((ergebnis as? SuccessResult)?.drawable as? BitmapDrawable)?.bitmap
        if (bild != null) blaetter[nr] = bild.asImageBitmap()
    }
    val hoehe = breite * (trickplay.hoehe.toFloat() / trickplay.breite.coerceAtLeast(1))
    Column(modifier, horizontalAlignment = Alignment.CenterHorizontally) {
        val bild = nr?.let { blaetter[it] }
        if (kachel != null && bild != null) {
            Canvas(Modifier.width(breite).height(hoehe).clip(RoundedCornerShape(8.dp))) {
                drawImage(bild,
                    srcOffset = IntOffset(kachel.x, kachel.y),
                    srcSize = IntSize(kachel.breite, kachel.hoehe),
                    dstSize = IntSize(size.width.toInt(), size.height.toInt()))
            }
        } else {
            Box(Modifier.width(breite).height(hoehe).clip(RoundedCornerShape(8.dp))
                    .background(Color(0x99000000)))
        }
        Spacer(Modifier.height(4.dp))
        Text(clock(sekunden), fontSize = 13.sp, color = Color.White,
             modifier = Modifier.background(Color(0x99000000), RoundedCornerShape(6.dp))
                 .padding(horizontal = 8.dp, vertical = 2.dp))
    }
}
