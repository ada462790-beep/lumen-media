// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package org.lumen.tv

import android.content.Context
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.animation.core.tween
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.focusable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.draw.clip
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.focus.onFocusChanged
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.StrokeJoin
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.graphics.drawscope.scale
import androidx.compose.ui.graphics.drawscope.translate
import androidx.compose.ui.graphics.vector.PathParser
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.ui.viewinterop.AndroidView
import androidx.media3.common.MediaItem
import androidx.media3.common.PlaybackException
import androidx.media3.common.Player
import androidx.media3.exoplayer.ExoPlayer
import androidx.media3.ui.AspectRatioFrameLayout
import android.view.TextureView
import androidx.media3.common.VideoSize
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import org.lumen.core.*

/**
 * Was der Fernseher vom Browser übernimmt: Abzeichen auf den Karten,
 * Profilbilder mit den eingebauten Figuren, die Neuigkeiten und die
 * bewegte Vorschau im Hintergrund der Startseite.
 */

// ---------------------------------------------------------------- Abzeichen

/** „Neu" oder „3 neue Folgen" links, 4K und HDR rechts — gross genug für drei Meter. */
@Composable
internal fun TvKartenAbzeichen(item: Item, modifier: Modifier = Modifier) {
    val links = item.abzeichenLinks
    val rechts = item.abzeichenRechts
    if (links == null && rechts.isEmpty()) return
    Row(
        modifier.fillMaxWidth().padding(10.dp),
        horizontalArrangement = Arrangement.SpaceBetween,
        verticalAlignment = Alignment.Top,
    ) {
        if (links != null) TvPille(links, Color(0xFFFF453A)) else Spacer(Modifier.width(1.dp))
        Column(horizontalAlignment = Alignment.End, verticalArrangement = Arrangement.spacedBy(5.dp)) {
            rechts.forEach { TvPille(it, Color(0x99000000)) }
        }
    }
}

@Composable
private fun TvPille(text: String, farbe: Color) {
    Text(text, color = Color.White, fontSize = 13.sp, fontWeight = FontWeight.Bold, maxLines = 1,
         modifier = Modifier.clip(RoundedCornerShape(6.dp)).background(farbe)
             .padding(horizontal = 8.dp, vertical = 3.dp))
}

// ---------------------------------------------------------------- Figuren

/** Eine eingebaute Figur, weiss gezogen — die Pfade kommen aus lumen-core. */
@Composable
internal fun TvFigur(figur: Figur, modifier: Modifier = Modifier) {
    val pfade = remember(figur.name) {
        figur.formen.map { it to PathParser().parsePathString(it.pfad).toPath() }
    }
    Canvas(modifier) {
        val f = size.minDimension / 24f
        translate((size.width - 24f * f) / 2, (size.height - 24f * f) / 2) {
            scale(f, f, pivot = Offset.Zero) {
                pfade.forEach { (form, pfad) ->
                    if (form.gefuellt) drawPath(pfad, Color.White)
                    drawPath(pfad, Color.White,
                             style = Stroke(1.6f, cap = StrokeCap.Round, join = StrokeJoin.Round))
                }
            }
        }
    }
}

// ---------------------------------------------------------------- Bewegte Vorschau

/**
 * Ein einziger Abspieler für die Vorschau, wiederverwendet für jede Karte:
 * stumm, in Schleife, erst beim ersten Gebrauch angelegt. Einen pro Karte
 * hielte kein Fernseher aus.
 */
internal class TvVorschauSpieler(private val context: Context) {
    private var spieler: ExoPlayer? = null

    /** Für welchen Titel das erste Bild schon da ist — erst dann wird eingeblendet. */
    var bereitFuer by mutableStateOf<String?>(null)
        private set
    private var laeuftFuer: String? = null

    /** Breite durch Höhe des Schnipsels — für das Beschneiden aufs Bild. */
    var seitenverhaeltnis by mutableStateOf(0f)
        private set

    @androidx.annotation.OptIn(androidx.media3.common.util.UnstableApi::class)
    fun spieler(): ExoPlayer = spieler ?: ExoPlayer.Builder(context).build().also { p ->
        p.volume = 0f
        p.repeatMode = Player.REPEAT_MODE_ONE
        p.addListener(object : Player.Listener {
            override fun onRenderedFirstFrame() { bereitFuer = laeuftFuer }
            override fun onVideoSizeChanged(videoSize: VideoSize) {
                if (videoSize.height > 0) {
                    seitenverhaeltnis = videoSize.width * videoSize.pixelWidthHeightRatio / videoSize.height
                }
            }
            // Keine Vorschau (kein FFmpeg, 404): einfach das Bild stehen lassen.
            override fun onPlayerError(error: PlaybackException) { bereitFuer = null }
        })
        spieler = p
    }

    fun spielen(itemId: String, adresse: String) {
        val p = spieler()
        laeuftFuer = itemId
        bereitFuer = null
        p.setMediaItem(MediaItem.fromUri(adresse))
        p.prepare()
        p.playWhenReady = true
    }

    fun flaecheLoesen(flaeche: TextureView) { spieler?.clearVideoTextureView(flaeche) }

    fun anhalten() {
        laeuftFuer = null
        bereitFuer = null
        spieler?.let { it.stop(); it.clearMediaItems() }
    }

    fun freigeben() {
        anhalten()
        spieler?.release()
        spieler = null
    }
}

/**
 * Die Vorschau über dem Hintergrundbild: steht der Fokus eine Sekunde auf
 * einer Karte, läuft ihr Schnipsel. Wechselt der Fokus, hört er sofort
 * auf; bis das erste Bild da ist, bleibt das Standbild sichtbar.
 *
 * Eine TextureView statt der SurfaceView von PlayerView: nur sie lässt
 * sich durchsichtig machen und einblenden. Die SurfaceView stanzte ein
 * Loch ins Bild, ob sichtbar oder nicht — und ohne Fläche zum Zeichnen
 * käme nie das erste Bild, auf das das Einblenden wartet.
 */
@androidx.annotation.OptIn(androidx.media3.common.util.UnstableApi::class)
@Composable
internal fun TvVorschauBild(vorschau: TvVorschauSpieler, client: ApiClient, item: Item?) {
    LaunchedEffect(item?.id) {
        vorschau.anhalten()
        val it = item ?: return@LaunchedEffect
        if (it.kind != "movie" && it.kind != "series") return@LaunchedEffect
        delay(1000)
        client.vorschauUrl(it.id)?.let { adresse -> vorschau.spielen(it.id, adresse) }
    }
    if (item == null) return
    val sichtbar = vorschau.bereitFuer == item.id
    val deckkraft by animateFloatAsState(if (sichtbar) 1f else 0f, tween(450), label = "vorschau")
    val verhaeltnis = vorschau.seitenverhaeltnis
    AndroidView(
        factory = { ctx ->
            AspectRatioFrameLayout(ctx).apply {
                resizeMode = AspectRatioFrameLayout.RESIZE_MODE_ZOOM
                // Der Fokus gehört den Karten, nicht dem Bild dahinter.
                isFocusable = false
                descendantFocusability = android.view.ViewGroup.FOCUS_BLOCK_DESCENDANTS
                val flaeche = TextureView(ctx)
                addView(flaeche, android.widget.FrameLayout.LayoutParams(
                    android.view.ViewGroup.LayoutParams.MATCH_PARENT,
                    android.view.ViewGroup.LayoutParams.MATCH_PARENT))
                vorschau.spieler().setVideoTextureView(flaeche)
            }
        },
        update = { rahmen ->
            rahmen.alpha = deckkraft
            if (verhaeltnis > 0f) rahmen.setAspectRatio(verhaeltnis)
        },
        onRelease = { rahmen ->
            (rahmen.getChildAt(0) as? TextureView)?.let { vorschau.flaecheLoesen(it) }
        },
        modifier = Modifier.fillMaxSize(),
    )
}

// ---------------------------------------------------------------- Neuigkeiten

/**
 * Die eigenen Neuigkeiten: neue Folgen meiner Serien, neue Filme meiner
 * Reihen, was diese Woche dazukam. Allein aus dem eigenen Stand — und
 * genau wie im Browser gilt die Liste als gelesen, sobald sie offen war.
 */
@Composable
internal fun TvNeuigkeiten(
    client: ApiClient,
    onGelesen: () -> Unit,
    onOpen: (Item) -> Unit,
    onHome: () -> Unit,
) {
    var daten by remember { mutableStateOf<Neuigkeiten?>(null) }
    var fehler by remember { mutableStateOf<String?>(null) }
    val scope = rememberCoroutineScope()
    val anfang = remember { FocusRequester() }

    LaunchedEffect(Unit) {
        try {
            val d = withContext(Dispatchers.IO) { client.news() }
            daten = d
            if (d.ungelesen > 0) {
                withContext(Dispatchers.IO) { oderNull { client.newsSeen() } }
                onGelesen()
            }
        } catch (e: LumenException) {
            fehler = e.message
        }
    }

    val d = daten
    if (d == null) {
        TvLaden(fehler)
        return
    }

    LaunchedEffect(d) {
        delay(100)
        runCatching { anfang.requestFocus() }
    }

    fun oeffnen(n: Neuigkeit) {
        val id = n.itemId ?: return onHome()
        scope.launch {
            try {
                onOpen(withContext(Dispatchers.IO) { client.item(id) }.item)
            } catch (e: LumenException) {
                fehler = e.message
            }
        }
    }

    Column(Modifier.fillMaxSize().padding(start = 56.dp, top = 48.dp, end = 56.dp)) {
        Text(tx(R.string.neuigkeiten), fontSize = 46.sp, fontWeight = FontWeight.Bold)
        Spacer(Modifier.height(8.dp))
        Text(tx(R.string.neue_folgen_deiner_serien_neue),
             color = Tv.label2, fontSize = 18.sp)
        fehler?.let {
            Spacer(Modifier.height(8.dp))
            Text(it, color = Tv.bad, fontSize = 16.sp)
        }
        Spacer(Modifier.height(28.dp))
        if (d.eintraege.isEmpty()) {
            Text(tx(R.string.nichts_neues_lumen_meldet_sich),
                 color = Tv.label2, fontSize = 20.sp)
            Spacer(Modifier.height(24.dp))
            TvButton(tx(R.string.zurueck), modifier = Modifier.focusRequester(anfang)) { onHome() }
            return@Column
        }
        LazyColumn(Modifier.widthIn(max = 980.dp), contentPadding = PaddingValues(bottom = 48.dp)) {
            itemsIndexed(d.eintraege) { i, n ->
                TvNeuigkeitZeile(n, d.istNeu(n),
                    if (i == 0) Modifier.focusRequester(anfang) else Modifier) { oeffnen(n) }
            }
        }
    }
}

@Composable
private fun TvNeuigkeitZeile(n: Neuigkeit, neu: Boolean, modifier: Modifier, onClick: () -> Unit) {
    var fokussiert by remember { mutableStateOf(false) }
    Row(
        modifier
            .fillMaxWidth()
            .onFocusChanged { fokussiert = it.isFocused }
            .clickable(role = Role.Button, onClick = onClick)
            .clip(RoundedCornerShape(14.dp))
            .background(if (fokussiert) Color.White else Color.Transparent)
            .padding(horizontal = 20.dp, vertical = 14.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        val vorn = if (fokussiert) Color.Black else Color.White
        val hinten = if (fokussiert) Color(0x99000000) else Tv.label2
        Box(Modifier.size(48.dp).clip(RoundedCornerShape(14.dp))
                .background(if (neu) Color(0x2E0A84FF) else Color(0x17FFFFFF)),
            contentAlignment = Alignment.Center) {
            Text(when (n.art) { "folgen" -> "▤"; "reihe" -> "❐"; else -> "★" },
                 fontSize = 20.sp, color = if (neu) Color(0xFF4AA3FF) else hinten)
        }
        Spacer(Modifier.width(18.dp))
        Column(Modifier.weight(1f)) {
            Text(n.titel, fontSize = 22.sp, color = vorn, maxLines = 1, overflow = TextOverflow.Ellipsis,
                 fontWeight = if (neu) FontWeight.SemiBold else FontWeight.Normal)
            Text(listOf(n.text, Zeitpunkt.kurz(n.at)).filter { it.isNotBlank() }.joinToString(" · "),
                 fontSize = 16.sp, color = hinten, maxLines = 1, overflow = TextOverflow.Ellipsis)
        }
        if (neu) {
            Box(Modifier.padding(start = 12.dp).size(12.dp).clip(CircleShape).background(Color(0xFFFF453A)))
        }
    }
}
