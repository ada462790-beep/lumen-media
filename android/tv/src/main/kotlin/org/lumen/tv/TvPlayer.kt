// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package org.lumen.tv

import android.content.Context
import android.view.KeyEvent
import android.view.View
import androidx.activity.compose.BackHandler
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.focusable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.focus.onFocusChanged
import androidx.compose.ui.geometry.CornerRadius
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.input.key.Key
import androidx.compose.ui.input.key.KeyEventType
import androidx.compose.ui.input.key.key
import androidx.compose.ui.input.key.onKeyEvent
import androidx.compose.ui.input.key.type
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalView
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.ui.viewinterop.AndroidView
import androidx.compose.ui.window.Dialog
import androidx.compose.ui.window.DialogProperties
import androidx.compose.ui.window.DialogWindowProvider
import androidx.media3.common.C
import androidx.media3.common.MediaItem
import androidx.media3.common.MimeTypes
import androidx.media3.common.Player
import androidx.media3.common.TrackSelectionOverride
import androidx.media3.common.Timeline
import androidx.media3.common.Tracks
import androidx.media3.exoplayer.ExoPlayer
import androidx.media3.exoplayer.hls.HlsManifest
import androidx.media3.ui.CaptionStyleCompat
import androidx.media3.ui.PlayerView
import coil.compose.AsyncImage
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import org.lumen.core.*

/**
 * Der Player auf dem Fernseher — samt dem, was danach kommt.
 *
 * Abgespielt wird mit ExoPlayer, bedient mit der Fernbedienung. Die
 * Steuerung von Media3 kennt das Tastenfeld schon: Richtungstasten zum
 * Spulen, Mitteltaste für Pause. Darüber liegt, was Lumen beisteuert, wie
 * im Player von Apple TV: rechts unten zwei runde Symbole für Untertitel
 * und Ton, dahinter ein dunkles Feld am rechten Rand; „Vorspann
 * überspringen"; „Als Nächstes" mit Bild und Countdown.
 *
 * Der Weg mit dem Steuerkreuz: ist die Steuerung sichtbar, führt „hoch"
 * zu den zwei Symbolen; „runter" oder „zurück" wieder ins Bild. Die
 * Mitteltaste öffnet das Feld, das die Fernbedienung dann für sich hat.
 *
 * Was als Nächstes kommt, entscheidet [AmStueck] in lumen-core, genau wie
 * auf dem Telefon; den Vorspann für eine Serie markiert man im Browser
 * oder am Telefon — der Fernseher hat keinen Entsperr-Dialog dafür.
 */
@Composable
internal fun TvPlayer(
    client: ApiClient,
    start: TvAbspielen,
    prefs: Map<String, String>,
    verwalter: Boolean = false,
    onPref: (String, String) -> Unit,
    onClose: () -> Unit,
) {
    var aktuell by remember { mutableStateOf(start.titel) }
    var index by remember { mutableStateOf(start.index) }
    var naechstes by remember { mutableStateOf<AmStueck.Naechstes?>(null) }
    var gesuchtFuer by remember { mutableStateOf<String?>(null) }
    var abgelehntFuer by remember { mutableStateOf<String?>(null) }
    // „Von vorn" gilt nur für den ersten Titel; danach zählt der Stand.
    var startAb by remember { mutableStateOf(start.ab) }
    // Ein Film in Teilen: wo der nächste Teil beginnt (Gesamtstelle), und
    // ein Zähler, der für jeden Teil einen frischen Player erzwingt.
    var teilAb by remember { mutableStateOf<Double?>(null) }
    var teilLauf by remember { mutableStateOf(0) }
    // Unter „Qualität" mitten im Film eine andere Fassung gewählt: für
    // welchen Titel welche Datei, und Ton und Untertitel der vorigen, um
    // sie nach Sprache zu übernehmen. Ein frischer Player an derselben Stelle.
    var fassungFuer by remember { mutableStateOf<Pair<String, String>?>(null) }
    var uebernahme by remember { mutableStateOf<Pair<List<Track>, Spuren.Wahl>?>(null) }
    var startHinweis by remember { mutableStateOf<String?>(null) }
    // Eines pro Wiedergabe: es merkt sich, welche Titel eine Datei haben.
    val folge = remember { Abspielfolge(client) }
    val scope = rememberCoroutineScope()
    val autoplay = prefs["autoplay"] != "0"

    fun weiter(n: AmStueck.Naechstes) {
        naechstes = null
        startAb = null
        teilAb = null
        uebernahme = null
        startHinweis = null
        if (n.index >= 0) index = n.index
        aktuell = n.titel
    }

    fun suchen(beendet: Item) {
        if (gesuchtFuer == beendet.id || abgelehntFuer == beendet.id) return
        gesuchtFuer = beendet.id
        val ab = index + 1
        scope.launch {
            val gefunden = try {
                withContext(Dispatchers.IO) {
                    if (start.liste.isNotEmpty()) {
                        // Die Liste läuft am Stück — ein ausdrücklicher
                        // Wunsch, darum unabhängig von der Einstellung.
                        folge.naechstesInListe(start.liste, ab)
                    } else if (beendet.kind == "episode" && autoplay) {
                        folge.folgeNach(beendet)
                    } else {
                        null
                    }
                }
            } catch (e: LumenException) {
                null
            }
            if (aktuell.id == beendet.id && abgelehntFuer != beendet.id) naechstes = gefunden
        }
    }

    Box(Modifier.fillMaxSize().background(Color.Black)) {
        // Ein neuer Titel bekommt einen ganz neuen Player samt Zustand.
        key(aktuell.id, teilLauf) {
            TvWiedergabe(
                client, aktuell, prefs, verwalter,
                abSekunden = teilAb ?: if (aktuell.id == start.titel.id) startAb else null,
                // Die gewählte Fassung gilt für ihren Titel, über alle Teile.
                dateiId = fassungFuer?.takeIf { it.first == aktuell.id }?.second
                    ?: if (aktuell.id == start.titel.id) start.dateiId else null,
                uebernahme = uebernahme,
                startHinweis = startHinweis,
                // Oben im Bild: aus welcher Liste und der wievielte Titel.
                unterzeile = if (start.liste.isNotEmpty() && index >= 0)
                    tx(R.string.liste_platz, start.listenName ?: tx(R.string.liste), index + 1, start.liste.size)
                else null,
                naechstes = naechstes,
                onPref = onPref,
                onAbspann = { suchen(it) },
                onEnded = { beendet -> if (naechstes == null) suchen(beendet) },
                // Noch ein Teil übrig: gleich weiter, ohne Countdown.
                onNaechsterTeil = { ab -> teilAb = ab; uebernahme = null; startHinweis = null; teilLauf++ },
                // Eine andere Fassung mitten im Film: an derselben Stelle weiter,
                // mit derselben Ton- und Untertitelsprache — wie im Browser.
                onFassung = { dateiId, ab, spuren, alteWahl, hinweis ->
                    fassungFuer = aktuell.id to dateiId
                    uebernahme = spuren to alteWahl
                    startHinweis = hinweis
                    teilAb = ab
                    teilLauf++
                },
                onNaechstes = { naechstes?.let { weiter(it) } },
                onAbbrechen = { abgelehntFuer = aktuell.id; naechstes = null },
                // „Nächste Folge" oder eine Folge aus dem Folgen-Feld.
                onWechsel = { neu -> naechstes = null; startAb = null; teilAb = null; uebernahme = null; startHinweis = null; aktuell = neu },
                onClose = onClose)
        }
    }
}

/** Welches Feld offen ist. */
private enum class TvFeld { TON, UNTERTITEL, FOLGEN, QUALITAET }

/**
 * Die Bildfläche von Media3 mit einem Griff fürs Steuerkreuz: „hoch" bei
 * sichtbarer Steuerung geht zu den Symbolen von Lumen, statt ins Leere.
 */
@androidx.annotation.OptIn(androidx.media3.common.util.UnstableApi::class)
private class LumenPlayerView(context: Context, private val hoch: () -> Boolean) : PlayerView(context) {
    override fun dispatchKeyEvent(event: KeyEvent): Boolean {
        if (event.action == KeyEvent.ACTION_DOWN && event.keyCode == KeyEvent.KEYCODE_DPAD_UP &&
            isControllerFullyVisible && hoch()) {
            return true
        }
        return super.dispatchKeyEvent(event)
    }
}

@androidx.annotation.OptIn(androidx.media3.common.util.UnstableApi::class)
@Composable
private fun TvWiedergabe(
    client: ApiClient,
    item: Item,
    prefs: Map<String, String>,
    verwalter: Boolean,
    abSekunden: Double?,
    dateiId: String?,
    uebernahme: Pair<List<Track>, Spuren.Wahl>?,
    startHinweis: String?,
    unterzeile: String?,
    naechstes: AmStueck.Naechstes?,
    onPref: (String, String) -> Unit,
    onAbspann: (Item) -> Unit,
    onEnded: (Item) -> Unit,
    onNaechsterTeil: (Double) -> Unit,
    onFassung: (String, Double, List<Track>, Spuren.Wahl, String) -> Unit,
    onNaechstes: () -> Unit,
    onAbbrechen: () -> Unit,
    onWechsel: (Item) -> Unit,
    onClose: () -> Unit,
) {
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    var player by remember { mutableStateOf<ExoPlayer?>(null) }
    var ansicht by remember { mutableStateOf<PlayerView?>(null) }
    // Ob nach einer 401 schon einmal ein neuer Medienschlüssel geholt wurde.
    var schluesselErneuert by remember { mutableStateOf(false) }
    // Wann zuletzt nach einem 410 neu angesetzt wurde — gegen Schleifen.
    var zuletztNeuAngesetzt by remember { mutableStateOf(0L) }
    // Vorschaubilder beim Spulen, und wo gerade gespult wird.
    var trickplay by remember { mutableStateOf<Trickplay?>(null) }
    var spultBei by remember { mutableStateOf<Long?>(null) }
    // „Qualität": die Stufe für dieses Gerät (Original, 1080p · 8 Mbit/s …,
    // Auto), die Fassungen des Titels, welche läuft und welche Standard ist.
    var stufe by remember { mutableStateOf(Qualitaetswahl.lesen(context)) }
    var fassungen by remember { mutableStateOf<List<MediaFile>>(emptyList()) }
    var fassungId by remember { mutableStateOf<String?>(null) }
    var standardId by remember { mutableStateOf<String?>(null) }
    var fehler by remember { mutableStateOf<String?>(null) }
    var detail by remember { mutableStateOf<ItemDetail?>(null) }
    var datei by remember { mutableStateOf<MediaFile?>(null) }
    var plan by remember { mutableStateOf<PlaybackPlan?>(null) }
    var laenge by remember { mutableStateOf(0.0) }
    // Ein Film in Teilen: alle Teile, welcher läuft, was davor kommt. Der
    // Stand zählt über alle Teile — wie im Browser und auf dem Telefon.
    var teile by remember { mutableStateOf<FilmTeile?>(null) }
    var teil by remember { mutableStateOf(0) }
    var vorher by remember { mutableStateOf(0.0) }

    var wahl by remember { mutableStateOf(Spuren.Wahl()) }
    // Die Untertitelspuren, die der Server schon fertig hat. Nur die gehen
    // an den Abspieler: eine Spur, die er erst noch aus der Datei zieht,
    // hielte den Start bis zu 20 Sekunden auf oder käme als JSON an.
    var utBereit by remember { mutableStateOf(emptySet<Int>()) }
    // Die HLS-Sitzung, falls umgewandelt wird — beim Schliessen beenden.
    var hlsSitzung by remember { mutableStateOf<HlsSitzung?>(null) }
    var offen by remember { mutableStateOf<TvFeld?>(null) }
    var steuerungSichtbar by remember { mutableStateOf(true) }
    var leisteFokussiert by remember { mutableStateOf(false) }
    val leisteFokus = remember { FocusRequester() }

    // Das Folgen-Feld: Staffeln der Serie, die gewählte, deren Folgen.
    var staffeln by remember { mutableStateOf<List<Item>?>(null) }
    var folgenStaffel by remember { mutableStateOf(item.parentId) }
    var folgenListe by remember { mutableStateOf<List<Item>?>(null) }
    var kurzHinweis by remember { mutableStateOf<String?>(null) }
    val istFolge = item.kind == "episode"
    // Grösse und Hintergrund der Untertitel, wie in den Einstellungen.
    val utStil = UntertitelStil.aus(prefs)

    var intro by remember { mutableStateOf<Vorspann?>(null) }
    // Wo der Abspann der laufenden Fassung beginnt — aus jeDatei, sonst
    // die Angabe für die erste Datei.
    var abspannMs by remember { mutableStateOf<Long?>(null) }
    var introUebersprungen by remember { mutableStateOf(false) }
    var abspannGemeldet by remember { mutableStateOf(false) }
    var positionMs by remember { mutableStateOf(0L) }
    var beendet by remember { mutableStateOf(false) }

    // Den Haken setzt entweder die Schleife unten oder das Ende — einmal.
    var fertigGemeldet by remember { mutableStateOf(false) }
    val amEnde by rememberUpdatedState(onEnded)
    val imAbspann by rememberUpdatedState(onAbspann)
    val teilZuEnde by rememberUpdatedState(onNaechsterTeil)

    /** Die Gesamtstelle über alle Teile — bei einer Datei einfach die Stelle. */
    fun gesamtStelle(imTeil: Double): Double = vorher + imTeil
    /** Die Dauer, die der Server bekommt: die aller Teile, sonst die der Datei. */
    fun gesamtLaenge(): Double = teile?.takeIf { it.mehrere }?.gesamtSekunden?.takeIf { it > 0 } ?: laenge
    /** Erst nach dem letzten Teil ist der Film gesehen. */
    fun letzterTeil(): Boolean = teile?.istLetzter(teil) ?: true

    fun alsGesehen() {
        if (fertigGemeldet) return
        fertigGemeldet = true
        // Ein eigener Faden statt einer Koroutine: gleich danach kann der
        // nächste Titel beginnen, und der Haken soll trotzdem ankommen.
        Thread { try { client.markFinished(item.id) } catch (e: LumenException) {} }.start()
    }

    // Als Funktion, nicht als Wert: der Abspieler ruft von seinem Zuhörer
    // aus hierher, und der stammt aus der ersten Zusammensetzung — als die
    // Datei noch nicht da war. Gelesen wird darum immer der aktuelle Stand.
    fun spuren(): List<Track> = datei?.tracks ?: emptyList()

    /** Das Geräteprofil mit den Grenzen der gewählten Stufe — für Plan und Strom. */
    fun profil(): DeviceProfile = Qualitaet.profil(stufe, datei?.height)

    fun mediaItem(): MediaItem? {
        val d = datei ?: return null
        val p = plan ?: return null
        val adresse = client.streamUrl(d, p, wahl.ton, profil())
        // Nur die fertigen Untertitelspuren, siehe [utBereit].
        val untertitel = Spuren.text(d.tracks).filter { it.index in utBereit }.map { t ->
            MediaItem.SubtitleConfiguration.Builder(
                android.net.Uri.parse(client.subtitleUrl(d.id, t.index)))
                .setMimeType(MimeTypes.TEXT_VTT)
                .setId("spur-${t.index}")
                .setLanguage(t.language?.takeIf { it.isNotBlank() && it != "und" })
                .setLabel(t.anzeigename)
                .setSelectionFlags(if (wahl.untertitel == t.index) C.SELECTION_FLAG_DEFAULT else 0)
                .build()
        }
        return MediaItem.Builder().setUri(adresse).setSubtitleConfigurations(untertitel).build()
    }

    fun untertitelAnwenden(p: Player) {
        val b = p.trackSelectionParameters.buildUpon().clearOverridesOfType(C.TRACK_TYPE_TEXT)
        val gewuenscht = wahl.untertitel
        if (gewuenscht == null) {
            b.setTrackTypeDisabled(C.TRACK_TYPE_TEXT, true)
        } else {
            b.setTrackTypeDisabled(C.TRACK_TYPE_TEXT, false)
            p.currentTracks.groups.firstOrNull {
                it.type == C.TRACK_TYPE_TEXT && it.getTrackFormat(0).id == "spur-$gewuenscht"
            }?.let { b.addOverride(TrackSelectionOverride(it.mediaTrackGroup, 0)) }
        }
        p.trackSelectionParameters = b.build()
    }

    // Läuft die Datei direkt, stecken alle Tonspuren im Strom; der
    // Abspieler kennt sie der Reihe nach — darum über die Stelle, zur
    // Not über die Sprache. Beim Umpacken hat der Server schon gewählt.
    fun tonAnwenden(p: Player) {
        if (wahl.ton < 0) return
        val alle = spuren()
        val tonSpuren = Spuren.ton(alle)
        val gruppen = p.currentTracks.groups.filter { it.type == C.TRACK_TYPE_AUDIO }
        val stelle = Spuren.tonPosition(alle, wahl.ton)
        val sprache = tonSpuren.firstOrNull { it.index == wahl.ton }?.language
        val ziel = if (gruppen.size == tonSpuren.size && stelle >= 0) gruppen[stelle]
                   else if (sprache != null) gruppen.firstOrNull { it.getTrackFormat(0).language == sprache }
                   else null
        ziel ?: return
        p.trackSelectionParameters = p.trackSelectionParameters.buildUpon()
            .setOverrideForType(TrackSelectionOverride(ziel.mediaTrackGroup, 0)).build()
    }

    fun laden(p: ExoPlayer, abMs: Long) {
        val m = mediaItem() ?: return
        if (abMs > 1000) p.setMediaItem(m, abMs) else p.setMediaItem(m)
        p.prepare()
        p.playWhenReady = true
    }

    /** Neu laden, weil eine Untertitelspur fertig wurde — angehalten bleibt angehalten. */
    fun mitUntertitelNeuLaden(p: ExoPlayer) {
        val spielte = p.playWhenReady
        laden(p, p.currentPosition)
        p.playWhenReady = spielte
    }

    fun tonWaehlen(t: Track) {
        if (wahl.ton == t.index) return
        wahl = wahl.copy(ton = t.index)
        Spuren.prefWertTon(t)?.let { onPref(Spuren.PREF_TON, it) }
        // Der Ton steckt im Strom: neu öffnen, an derselben Stelle.
        player?.let { p -> laden(p, p.currentPosition) }
    }

    fun untertitelWaehlen(t: Track?) {
        wahl = wahl.copy(untertitel = t?.index)
        onPref(Spuren.PREF_UNTERTITEL, Spuren.prefWertUntertitel(t))
        player?.let { untertitelAnwenden(it) }
    }

    fun vorspannUeberspringen() {
        val i = intro ?: return
        introUebersprungen = true
        player?.seekTo(i.bisMs)
    }

    /** Den Stand sichern und zu einer anderen Folge wechseln. */
    fun wechseln(neu: Item) {
        if (neu.id == item.id) return
        player?.let { p ->
            val position = p.currentPosition / 1000.0
            if (position > 0 && laenge > 0) {
                val stelle = gesamtStelle(position)
                val dauer = gesamtLaenge()
                Thread { try { client.saveProgress(item.id, stelle, dauer) }
                         catch (e: LumenException) {} }.start()
            }
        }
        offen = null
        onWechsel(neu)
    }

    /** „Nächste Folge": die Folge danach, über das Staffelende hinweg. */
    fun naechsteFolge() {
        scope.launch {
            val n = try {
                withContext(Dispatchers.IO) { Abspielfolge(client).folgeNach(item) }
            } catch (e: LumenException) {
                null
            }
            if (n == null) {
                kurzHinweis = tx(R.string.das_war_die_letzte_folge)
                delay(2500)
                kurzHinweis = null
            } else {
                wechseln(n.titel)
            }
        }
    }

    fun staffelLaden(id: String) {
        folgenStaffel = id
        folgenListe = null
        scope.launch {
            folgenListe = try {
                AmStueck.folgenOrdnen(withContext(Dispatchers.IO) { client.children(id) })
            } catch (e: LumenException) {
                emptyList()
            }
        }
    }

    fun folgenOeffnen() {
        offen = TvFeld.FOLGEN
        if (staffeln != null) return
        val sid = detail?.seriesId
        scope.launch {
            staffeln = try {
                if (sid == null) emptyList()
                else AmStueck.staffelnOrdnen(withContext(Dispatchers.IO) { client.item(sid) }.seasons)
            } catch (e: LumenException) {
                emptyList()
            }
        }
        item.parentId?.let { staffelLaden(it) } ?: run { folgenListe = emptyList() }
    }

    /** Zurück ins Bild: die Steuerung zeigen und ihr den Fokus geben. */
    fun zumBild() {
        offen = null
        ansicht?.let { v ->
            v.showController()
            v.requestFocus()
        }
    }

    LaunchedEffect(item.id) {
        try {
            val d = withContext(Dispatchers.IO) { client.item(item.id) }
            // Die gewählte Fassung, sonst die erste — und dazu ihre Teile.
            val erste = Fassungen.datei(d.files, dateiId)
            if (erste == null) { fehler = tx(R.string.zu_diesem_titel_gibt_es); return@LaunchedEffect }
            val alle = Fassungen.teile(d.files, erste)
            // Aus der Gesamtstelle der Teil und die Stelle darin.
            val stelle = alle.stelle(abSekunden ?: item.resumeSeconds)
            val f = alle.dateien[stelle.teil]
            // Mit dem Plan ein frischer Medienschlüssel: der Film läuft
            // Stunden, und der Abspieler lädt mit derselben Adresse nach.
            val pl = withContext(Dispatchers.IO) { client.wiedergabeVorbereiten(f.id, Qualitaet.profil(stufe, f.height)) }
            detail = d
            datei = f
            plan = pl
            teile = alle
            teil = stelle.teil
            vorher = stelle.vorher
            fassungen = Fassungen.liste(d.files)
            fassungId = erste.id
            standardId = fassungen.firstOrNull()?.id
            // Der Vorspann liegt im ersten Teil; in Teil 2 wäre die Marke falsch.
            // Es gelten die Marken der Fassung, die wirklich läuft (jeDatei).
            val marken = d.marken(erste.id)
            intro = if (stelle.teil == 0) marken.intro else null
            abspannMs = marken.creditsMs
            wahl = Spuren.vorbelegen(f.tracks, prefs)
            // Nach einem Fassungswechsel: Ton und Untertitel nach Sprache übernehmen.
            uebernahme?.let { (alteSpuren, alteWahl) -> wahl = Spuren.uebernehmen(alteSpuren, alteWahl, f.tracks, wahl) }
            startHinweis?.let { h ->
                scope.launch {
                    kurzHinweis = h
                    delay(2500)
                    if (kurzHinweis == h) kurzHinweis = null
                }
            }
            // Ist die vorgewählte Untertitelspur schon fertig, kommt sie
            // gleich mit; sonst startet der Film ohne, und sie folgt, sobald
            // sie da ist. Kurz gefragt: der Server wartet nur eine Sekunde.
            utBereit = emptySet()
            wahl.untertitel?.let { i ->
                val bereit = withContext(Dispatchers.IO) { oderNull { client.untertitelBereit(f.id, i, kurz = true) } }
                if (bereit == true) utBereit = setOf(i)
            }

            val dauer = if (f.durationSeconds > 0) f.durationSeconds else item.runtimeSeconds
            laenge = dauer
            var ab = stelle.imTeil
            if (dauer > 0 && ab > dauer - 5) ab = 0.0

            player = ExoPlayer.Builder(context).build().apply {
                addListener(object : Player.Listener {
                    override fun onPlayerError(error: androidx.media3.common.PlaybackException) {
                        // 410 nach langer Pause: die Umwandlung wurde
                        // aufgeräumt. Neuer Plan, an derselben Stelle weiter —
                        // ohne Fehlermeldung.
                        val jetzt = System.currentTimeMillis()
                        if (hlsAbgelaufen(error) && jetzt - zuletztNeuAngesetzt > 30_000) {
                            zuletztNeuAngesetzt = jetzt
                            val p = this@apply
                            val stelle = p.currentPosition
                            val dateiJetzt = datei
                            scope.launch {
                                val neuerPlan = dateiJetzt?.let { d ->
                                    withContext(Dispatchers.IO) { oderNull { client.wiedergabeVorbereiten(d.id, profil()) } }
                                }
                                if (neuerPlan != null) { plan = neuerPlan; laden(p, stelle) }
                                else fehler = tx(R.string.der_strom_ist_abgerissen_x, error.errorCodeName)
                            }
                            return
                        }
                        // 401 mitten im Film: Der Server wurde neu gestartet
                        // und kennt den Medienschlüssel nicht mehr. Einmal
                        // einen neuen holen und an derselben Stelle weiter —
                        // erst wenn das nicht hilft, ist es ein Fehler.
                        if (!schluesselErneuert && medienSchluesselAbgewiesen(error)) {
                            schluesselErneuert = true
                            val p = this@apply
                            val stelle = p.currentPosition
                            scope.launch {
                                val erneuert = withContext(Dispatchers.IO) { client.wiedergabeSchluesselErneuern() }
                                if (erneuert) laden(p, stelle)
                                else fehler = tx(R.string.der_strom_ist_abgerissen_x, error.errorCodeName)
                            }
                            return
                        }
                        // 403 mitten im Film: meist ist die Bildschirmzeit des
                        // Kinderprofils um. Der Plan sagt, warum — dessen Satz
                        // zeigen und anhalten.
                        if (zugriffAbgewiesen(error)) {
                            val p = this@apply
                            p.pause()
                            val dateiJetzt = datei
                            scope.launch {
                                val grund = dateiJetzt?.let { d ->
                                    withContext(Dispatchers.IO) {
                                        try { client.plan(d.id, profil()); null } catch (e: LumenException) { e.message }
                                    }
                                }
                                fehler = grund ?: tx(R.string.der_strom_ist_abgerissen_x, error.errorCodeName)
                            }
                            return
                        }
                        fehler = tx(R.string.der_strom_ist_abgerissen_x, error.errorCodeName)
                    }

                    override fun onPlaybackStateChanged(playbackState: Int) {
                        // Läuft wieder: Ein späterer Neustart des Servers
                        // darf wieder einmal neu ansetzen.
                        if (playbackState == Player.STATE_READY) schluesselErneuert = false
                        if (playbackState == Player.STATE_ENDED && !beendet) {
                            beendet = true
                            // Noch ein Teil übrig: der Film ist nicht zu Ende.
                            val t = teile
                            if (t != null && !t.istLetzter(teil)) {
                                teilZuEnde(t.weiterAb(teil))
                                return
                            }
                            alsGesehen()
                            amEnde(item)
                        }
                    }

                    override fun onTracksChanged(tracks: Tracks) {
                        tonAnwenden(this@apply)
                        untertitelAnwenden(this@apply)
                    }

                    // Die Kennung der HLS-Sitzung steht in der Adresse der
                    // Medienliste, die der Abspieler selbst holt.
                    override fun onTimelineChanged(timeline: Timeline, reason: Int) {
                        val d = datei ?: return
                        val liste = (this@apply.currentManifest as? HlsManifest)?.mediaPlaylist ?: return
                        HlsSitzung.kennung(liste.baseUri, d.id)?.let { hlsSitzung = HlsSitzung(d.id, it) }
                    }
                })
                laden(this, (ab * 1000).toLong())
            }
        } catch (e: LumenException) {
            fehler = e.message
        }
    }

    // Die Stelle im Blick: Vorspann, Abspann, Karte — und alle fünf
    // Sekunden den Stand sichern.
    LaunchedEffect(player) {
        val p = player ?: return@LaunchedEffect
        var tick = 0
        // Was zuletzt gesichert wurde — angehalten ändert sich nichts, und
        // dann geht auch nichts an den Server.
        var gesichert = -1.0
        while (true) {
            delay(500)
            tick++
            val pos = p.currentPosition
            positionMs = pos
            val dauerMs = (laenge * 1000).toLong()

            if (Marken.imVorspann(intro, pos) && !introUebersprungen && prefs["skipIntro"] == "1") {
                vorspannUeberspringen()
            }
            // Der Abspann liegt im letzten Teil — vorher kommt kein „Als Nächstes".
            if (!abspannGemeldet && letzterTeil() && Marken.imAbspann(abspannMs, dauerMs, pos)) {
                abspannGemeldet = true
                imAbspann(item)
            }
            if (tick % 10 == 0 && pos > 0 && laenge > 0) {
                val position = gesamtStelle(pos / 1000.0)
                val dauer = gesamtLaenge()
                // Für sich, nicht in der Schleife: Ein langsamer Server
                // hielte sonst Vorspann, Abspann und Stelle mit auf.
                if (kotlin.math.abs(position - gesichert) >= 1.0) {
                    gesichert = position
                    scope.launch(Dispatchers.IO) {
                        try { client.saveProgress(item.id, position, dauer) } catch (e: LumenException) {}
                    }
                }
                if (letzterTeil() && position / dauer >= SessionController.FINISHED_AT) alsGesehen()
            }
        }
    }

    /** Den Stand sichern — in einem eigenen Faden, damit er auch beim Verlassen ankommt. */
    fun standSichern() {
        val p = player ?: return
        val position = p.currentPosition / 1000.0
        if (position > 0 && laenge > 0) {
            val stelle = gesamtStelle(position)
            val dauer = gesamtLaenge()
            Thread { try { client.saveProgress(item.id, stelle, dauer) }
                     catch (e: LumenException) {} }.start()
        }
    }

    /**
     * Eine andere Stufe: dieselbe Datei an derselben Stelle neu holen, mit
     * den neuen Grenzen. Ton und Untertitel bleiben, wie sie sind.
     */
    fun qualitaetWaehlen(id: String) {
        val vorher = profil()
        stufe = id
        Qualitaetswahl.merken(context, id)
        zumBild()
        val d = datei ?: return
        val neu = profil()
        if (neu == vorher) return
        scope.launch {
            try {
                val neuerPlan = withContext(Dispatchers.IO) { client.wiedergabeVorbereiten(d.id, neu) }
                plan = neuerPlan
                player?.let { p ->
                    val spielte = p.playWhenReady
                    laden(p, p.currentPosition)
                    p.playWhenReady = spielte
                }
                kurzHinweis = tx(R.string.pl_qualitaet_x, Qualitaet.name(Qualitaet.wirksam(stufe, d.height)))
            } catch (e: LumenException) {
                kurzHinweis = e.message
            }
            delay(2500)
            kurzHinweis = null
        }
    }

    /**
     * Eine andere Fassung (Datei) desselben Titels: an derselben Stelle
     * weiter, mit derselben Ton- und Untertitelsprache. Der Fortschritt
     * gehört zum Titel, nicht zur Datei — er bleibt.
     */
    fun fassungWechseln(f: MediaFile) {
        if (f.id == fassungId) { zumBild(); return }
        offen = null
        val p = player ?: return
        val stelle = gesamtStelle(p.currentPosition / 1000.0)
        val i = fassungen.indexOfFirst { it.id == f.id }
        val name = Fassungen.namen(fassungen).getOrNull(i) ?: ""
        standSichern()
        onFassung(f.id, stelle, spuren(), wahl, tx(R.string.pl_fassung_x, name))
    }

    /** Verwaltung: die laufende Fassung für alle zum Standard machen. */
    fun alsStandard() {
        val id = fassungId ?: return
        zumBild()
        scope.launch {
            kurzHinweis = try {
                withContext(Dispatchers.IO) { client.fassungAlsStandard(item.id, id) }
                standardId = id
                tx(R.string.pl_gilt_fuer_alle)
            } catch (e: LumenException) {
                e.message
            }
            delay(2500)
            kurzHinweis = null
        }
    }

    /** Den Player freigeben und eine laufende Umwandlung gleich beenden. */
    fun freigeben() {
        player?.release()
        player = null
        hlsSitzung?.let { h ->
            hlsSitzung = null
            h.beendenImHintergrund(client)
        }
    }

    fun beenden() {
        standSichern()
        freigeben()
        onClose()
    }

    // Eine gewählte Untertitelspur, die noch nicht fertig ist: nachfragen,
    // bis der Server sie hat (er wartet je Frage bis zu 20 Sekunden, sagt
    // dann 202 und „in 3 Sekunden wieder"), und dann mit ihr neu laden.
    LaunchedEffect(player, wahl.untertitel) {
        val p = player ?: return@LaunchedEffect
        val d = datei ?: return@LaunchedEffect
        val i = wahl.untertitel ?: return@LaunchedEffect
        if (i in utBereit) return@LaunchedEffect
        repeat(UNTERTITEL_VERSUCHE) {
            when (withContext(Dispatchers.IO) { oderNull { client.untertitelBereit(d.id, i) } }) {
                true -> {
                    utBereit = utBereit + i
                    if (player === p) mitUntertitelNeuLaden(p)
                    return@LaunchedEffect
                }
                false -> delay(3000)
                // Gibt es nicht oder lässt sich nicht lesen: ohne Untertitel.
                null -> return@LaunchedEffect
            }
        }
    }

    // Vorschaubilder fürs Spulen; 202: später noch einmal fragen, 404: keine.
    LaunchedEffect(datei?.id) {
        val f = datei ?: return@LaunchedEffect
        repeat(4) {
            when (val stand = withContext(Dispatchers.IO) { oderNull { client.trickplay(f.id) } }) {
                is TrickplayStand.Bereit -> { trickplay = stand.trickplay; return@LaunchedEffect }
                TrickplayStand.WirdErstellt -> delay(30_000)
                else -> return@LaunchedEffect
            }
        }
    }

    // Die App verlassen (Home-Taste, anderer Eingang, Bildschirmschoner):
    // anhalten und den Stand sichern, statt im Hintergrund weiterzuspielen.
    val besitzer = androidx.lifecycle.compose.LocalLifecycleOwner.current
    DisposableEffect(besitzer, player) {
        val p = player
        val beobachter = androidx.lifecycle.LifecycleEventObserver { _, e ->
            if (e == androidx.lifecycle.Lifecycle.Event.ON_STOP && p != null) {
                p.pause()
                standSichern()
            }
        }
        besitzer.lifecycle.addObserver(beobachter)
        onDispose { besitzer.lifecycle.removeObserver(beobachter) }
    }

    BackHandler {
        when {
            offen != null -> zumBild()
            leisteFokussiert -> zumBild()
            else -> beenden()
        }
    }
    // Verschwindet die Ansicht auf anderem Weg, soll der Stand trotzdem ankommen.
    DisposableEffect(Unit) { onDispose { standSichern(); freigeben() } }

    val zeigeTon = Spuren.ton(spuren()).size > 1
    val zeigeText = Spuren.text(spuren()).isNotEmpty()
    // „Qualität" (Zahnrad) gibt es immer — darum ist immer etwas zu wählen.
    val etwasZuWaehlen = true
    // Die Bildfläche entsteht einmal; ihr Griff fürs Steuerkreuz liest
    // darum den jeweils aktuellen Stand, nicht den vom ersten Aufbau.
    val hochErlaubt by rememberUpdatedState(etwasZuWaehlen && naechstes == null)

    Box(Modifier.fillMaxSize().background(Color.Black)) {
        val p = player
        if (p != null) {
            AndroidView(
                factory = { ctx ->
                    LumenPlayerView(ctx, hoch = {
                        if (hochErlaubt) runCatching { leisteFokus.requestFocus() }.isSuccess
                        else false
                    }).apply {
                        this.player = p
                        useController = true
                        setShowSubtitleButton(false)
                        controllerShowTimeoutMs = 3500
                        setControllerVisibilityListener(
                            PlayerView.ControllerVisibilityListener { sichtbarkeit ->
                                steuerungSichtbar = sichtbarkeit == View.VISIBLE
                            })
                        // Beim Spulen mit links/rechts auf der Zeitleiste: das Vorschaubild.
                        findViewById<androidx.media3.ui.DefaultTimeBar>(androidx.media3.ui.R.id.exo_progress)
                            ?.addListener(object : androidx.media3.ui.TimeBar.OnScrubListener {
                                override fun onScrubStart(timeBar: androidx.media3.ui.TimeBar, position: Long) {
                                    spultBei = position
                                }
                                override fun onScrubMove(timeBar: androidx.media3.ui.TimeBar, position: Long) {
                                    spultBei = position
                                }
                                override fun onScrubStop(timeBar: androidx.media3.ui.TimeBar, position: Long,
                                                         canceled: Boolean) {
                                    spultBei = null
                                }
                            })
                        // Untertitel wie eingestellt — Grösse und Hintergrund.
                        subtitleView?.apply {
                            setApplyEmbeddedFontSizes(false)
                            setFractionalTextSize(utStil.textAnteil)
                            setStyle(
                                if (utStil.hintergrund) CaptionStyleCompat(
                                    android.graphics.Color.WHITE, 0x8C000000.toInt(),
                                    android.graphics.Color.TRANSPARENT, CaptionStyleCompat.EDGE_TYPE_NONE,
                                    android.graphics.Color.BLACK, null)
                                else CaptionStyleCompat(
                                    android.graphics.Color.WHITE, android.graphics.Color.TRANSPARENT,
                                    android.graphics.Color.TRANSPARENT, CaptionStyleCompat.EDGE_TYPE_DROP_SHADOW,
                                    android.graphics.Color.BLACK, null))
                        }
                        ansicht = this
                    }
                },
                update = { view ->
                    view.player = p
                    // Solange „Als Nächstes" steht, nimmt die Steuerung keine
                    // Tasten — sonst wäre „Abbrechen" nicht zu erreichen.
                    view.useController = naechstes == null
                },
                modifier = Modifier.fillMaxSize())
        } else if (fehler == null) {
            Mitte { CircularProgressIndicator(color = Tv.label2) }
        }

        fehler?.let {
            Mitte {
                Column(horizontalAlignment = Alignment.CenterHorizontally) {
                    Text(tx(R.string.das_laesst_sich_nicht_abspielen), fontSize = 26.sp)
                    Spacer(Modifier.height(10.dp))
                    Text(it, color = Tv.label2, fontSize = 17.sp)
                    Spacer(Modifier.height(22.dp))
                    TvButton(tx(R.string.zurueck)) { beenden() }
                }
            }
        }

        // Beim Spulen: das Vorschaubild über der Zeitleiste.
        val tp = trickplay
        val spult = spultBei
        if (tp != null && spult != null) {
            TvTrickplay(tp, { n -> tp.blaetter.getOrNull(n)?.let { client.trickplayBlattUrl(it) } },
                spult / 1000.0, Modifier.align(Alignment.BottomCenter).padding(bottom = 130.dp))
        }

        // Oben links, solange die Steuerung steht: was läuft — wie im Browser.
        if (p != null && steuerungSichtbar && offen == null && fehler == null) {
            Column(Modifier.align(Alignment.TopStart).padding(start = 48.dp, top = 36.dp)
                       .clip(RoundedCornerShape(16.dp)).background(Color(0x8C000000))
                       .padding(horizontal = 20.dp, vertical = 12.dp)) {
                Text(item.title, fontSize = 24.sp, fontWeight = FontWeight.SemiBold, maxLines = 1,
                     overflow = TextOverflow.Ellipsis, modifier = Modifier.widthIn(max = 760.dp))
                // Bei einem Film in Teilen: „Teil 2 von 3".
                val unten = unterzeile ?: teile?.beschriftung(teil) ?: if (istFolge)
                    Titelseite.folgenKennung(item) else ""
                if (unten.isNotEmpty()) Text(unten, fontSize = 17.sp, color = Tv.label2)
            }
        }

        // Rechts unten über der Zeitleiste: Untertitel und Ton.
        if (p != null && etwasZuWaehlen && naechstes == null &&
            (steuerungSichtbar || leisteFokussiert || offen != null)) {
            Row(
                Modifier
                    .align(Alignment.BottomEnd)
                    .padding(end = 48.dp, bottom = 120.dp)
                    .onKeyEvent { e ->
                        if (e.type != KeyEventType.KeyDown) return@onKeyEvent false
                        when (e.key) {
                            Key.DirectionDown, Key.Back -> { zumBild(); true }
                            else -> false
                        }
                    },
                horizontalArrangement = Arrangement.spacedBy(16.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                // „Hoch" landet auf dem ersten Knopf der Leiste.
                val erster = when {
                    istFolge -> "naechste"
                    zeigeText -> "untertitel"
                    zeigeTon -> "ton"
                    else -> "qualitaet"
                }
                fun fokus(name: String) = if (name == erster) Modifier.focusRequester(leisteFokus) else Modifier
                if (istFolge) {
                    TvNaechsteFolgeKnopf(
                        modifier = fokus("naechste"),
                        onFocus = { leisteFokussiert = it },
                        onClick = { naechsteFolge() })
                    TvSpurKnopf(TvFeld.FOLGEN,
                        onFocus = { leisteFokussiert = it },
                        onClick = { folgenOeffnen() })
                }
                if (zeigeText) {
                    TvSpurKnopf(TvFeld.UNTERTITEL,
                        modifier = fokus("untertitel"),
                        onFocus = { leisteFokussiert = it },
                        onClick = { offen = TvFeld.UNTERTITEL })
                }
                if (zeigeTon) {
                    TvSpurKnopf(TvFeld.TON,
                        modifier = fokus("ton"),
                        onFocus = { leisteFokussiert = it },
                        onClick = { offen = TvFeld.TON })
                }
                TvSpurKnopf(TvFeld.QUALITAET,
                    modifier = fokus("qualitaet"),
                    onFocus = { leisteFokussiert = it },
                    onClick = { offen = TvFeld.QUALITAET })
            }
        }

        // „Vorspann überspringen" — nimmt den Fokus, damit die Mitteltaste
        // springt; jede andere Taste gibt ihn ans Bild zurück.
        if (p != null && offen == null && naechstes == null &&
            Marken.imVorspann(intro, positionMs) && !introUebersprungen) {
            TvVorspannKnopf(
                onClick = { vorspannUeberspringen() },
                onWeg = { zumBild() },
                modifier = Modifier.align(Alignment.BottomEnd).padding(end = 48.dp, bottom = 190.dp))
        }

        naechstes?.let { n ->
            key(n.titel.id) {
                val dauerMs = (laenge * 1000).toLong()
                val restImFilm = if (beendet || dauerMs <= 0) 0
                                 else Math.round((dauerMs - positionMs) / 1000.0).toInt().coerceAtLeast(0)
                TvAlsNaechstes(
                    bild = client.frameUrl(n.titel.id),
                    beschriftung = n.beschriftung,
                    restImFilm = restImFilm,
                    mindest = if (abspannGemeldet) Marken.MINDEST_COUNTDOWN
                              else AmStueck.COUNTDOWN_SEKUNDEN,
                    onCancel = { onAbbrechen(); zumBild() },
                    onNow = onNaechstes,
                    modifier = Modifier.align(Alignment.BottomEnd).padding(48.dp))
            }
        }
    }

    kurzHinweis?.let {
        Box(Modifier.fillMaxSize()) {
            Text(it, fontSize = 20.sp,
                 modifier = Modifier.align(Alignment.TopCenter).padding(top = 40.dp)
                     .clip(RoundedCornerShape(16.dp)).background(Color(0xD912121A))
                     .padding(horizontal = 24.dp, vertical = 14.dp))
        }
    }

    if (offen == TvFeld.FOLGEN) {
        TvFolgenfeld(
            staffeln = staffeln ?: emptyList(),
            staffelId = folgenStaffel,
            folgen = folgenListe,
            aktuellId = item.id,
            bild = { client.wideUrl(it) },
            onStaffel = { staffelLaden(it.id) },
            onFolge = { wechseln(it) },
            onClose = { zumBild() })
        return
    }

    offen?.let { feld ->
        val abschnitte = when (feld) {
            TvFeld.FOLGEN -> emptyList()
            // Wie im Browser (qualitaetPanel): oben die Fassungen, darunter
            // die Stufen fürs Umwandeln, für Verwalter „Diese Fassung als Standard".
            TvFeld.QUALITAET -> buildList {
                val standard = standardId ?: fassungen.firstOrNull()?.id
                if (fassungen.size > 1) {
                    val namen = Fassungen.namen(fassungen)
                    add(TvSpurabschnitt(tx(R.string.pl_fassung), fassungen.mapIndexed { i, f ->
                        TvSpureintrag(if (f.id == standard) namen[i] + " · " + tx(R.string.pl_standard) else namen[i],
                            gewaehlt = f.id == fassungId, onClick = { fassungWechseln(f) })
                    }))
                }
                val quelle = datei?.height
                val jetzt = Qualitaet.wirksam(stufe, quelle)
                add(TvSpurabschnitt(tx(R.string.pl_qualitaet), buildList {
                    val herkunft = Fassungen.aufloesung(quelle)
                    add(TvSpureintrag(Qualitaet.name(Qualitaet.ORIGINAL) + (herkunft?.let { " · $it" } ?: ""),
                        gewaehlt = jetzt == Qualitaet.ORIGINAL, onClick = { qualitaetWaehlen(Qualitaet.ORIGINAL) }))
                    Qualitaet.stufenFuer(quelle).forEach { st ->
                        add(TvSpureintrag(Qualitaet.name(st.id), gewaehlt = jetzt == st.id, onClick = { qualitaetWaehlen(st.id) }))
                    }
                    add(TvSpureintrag(Qualitaet.name(Qualitaet.AUTO), gewaehlt = jetzt == Qualitaet.AUTO,
                        onClick = { qualitaetWaehlen(Qualitaet.AUTO) }))
                }))
                if (verwalter && fassungen.size > 1 && fassungId != standard) {
                    add(TvSpurabschnitt(tx(R.string.pl_fuer_alle), listOf(
                        TvSpureintrag(tx(R.string.pl_als_standard), gewaehlt = false, onClick = { alsStandard() }))))
                }
            }
            TvFeld.TON -> listOf(TvSpurabschnitt(tx(R.string.tonspur), Spuren.ton(spuren()).mapIndexed { i, t ->
                TvSpureintrag(t.anzeigename,
                    gewaehlt = wahl.ton == t.index || (wahl.ton < 0 && i == 0),
                    onClick = { tonWaehlen(t) })
            }))
            TvFeld.UNTERTITEL -> listOf(TvSpurabschnitt(tx(R.string.untertitel), buildList {
                add(TvSpureintrag(tx(R.string.aus), gewaehlt = wahl.untertitel == null,
                    onClick = { untertitelWaehlen(null) }))
                Spuren.text(spuren()).forEach { t ->
                    add(TvSpureintrag(t.anzeigename, gewaehlt = wahl.untertitel == t.index,
                        onClick = { untertitelWaehlen(t) }))
                }
            }))
        }
        TvSpurfeld(abschnitte, onClose = { zumBild() })
    }
}

// ---------------------------------------------------------------- Feld und Symbole

private data class TvSpureintrag(val text: String, val gewaehlt: Boolean, val onClick: () -> Unit)
private data class TvSpurabschnitt(val titel: String, val eintraege: List<TvSpureintrag>)

/**
 * Das Feld am rechten Rand — ein eigenes Fenster, damit die Fernbedienung
 * ihm gehört und nicht hinter ihm die Zeitleiste spult. Das Fenster ist
 * durchsichtig und ohne Abdunkeln: das Bild läuft daneben weiter.
 */
@Composable
private fun TvSpurfeld(abschnitte: List<TvSpurabschnitt>, onClose: () -> Unit) {
    Dialog(onDismissRequest = onClose,
           properties = DialogProperties(usePlatformDefaultWidth = false)) {
        val fenster = (LocalView.current.parent as? DialogWindowProvider)?.window
        SideEffect { fenster?.setDimAmount(0f) }

        val anfang = remember { FocusRequester() }
        LaunchedEffect(Unit) {
            delay(100)
            runCatching { anfang.requestFocus() }
        }

        Box(Modifier.fillMaxSize()) {
            Column(
                Modifier
                    .align(Alignment.CenterEnd)
                    .padding(end = 48.dp)
                    .width(420.dp)
                    .clip(RoundedCornerShape(24.dp))
                    .background(Color(0xD912121A))
                    .padding(vertical = 20.dp, horizontal = 14.dp)
                    .verticalScroll(rememberScrollState()),
            ) {
                // Der Fokus steht auf dem gewählten Eintrag — sonst auf dem ersten.
                var fokusVergeben = false
                abschnitte.forEachIndexed { i, a ->
                    if (i > 0) Spacer(Modifier.height(22.dp))
                    Text(a.titel.uppercase(), color = Tv.label3, fontSize = 14.sp,
                         fontWeight = FontWeight.SemiBold, letterSpacing = 1.5.sp,
                         modifier = Modifier.padding(start = 18.dp, bottom = 8.dp))
                    val gewaehlter = a.eintraege.indexOfFirst { it.gewaehlt }
                    a.eintraege.forEachIndexed { j, e ->
                        val nimmFokus = !fokusVergeben && (j == gewaehlter || (gewaehlter < 0 && j == 0))
                        if (nimmFokus) fokusVergeben = true
                        TvSpurZeile(e, if (nimmFokus) Modifier.focusRequester(anfang) else Modifier)
                    }
                }
            }
        }
    }
}

/** Ein Eintrag: fokussiert ein weisser Balken mit schwarzer Schrift, der Haken rechts. */
@Composable
private fun TvSpurZeile(e: TvSpureintrag, modifier: Modifier = Modifier) {
    var fokussiert by remember { mutableStateOf(false) }
    Row(
        modifier
            .fillMaxWidth()
            .onFocusChanged { fokussiert = it.isFocused }
            .clickable(role = Role.Button, onClick = e.onClick)
            .clip(RoundedCornerShape(14.dp))
            .background(if (fokussiert) Color.White else Color.Transparent)
            .padding(horizontal = 18.dp, vertical = 16.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(e.text, fontSize = 20.sp, color = if (fokussiert) Color.Black else Color.White,
             maxLines = 2, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f))
        if (e.gewaehlt) {
            Spacer(Modifier.width(12.dp))
            Text("✓", fontSize = 22.sp, fontWeight = FontWeight.Bold,
                 color = if (fokussiert) Color.Black else Color.White)
        }
    }
}

/** Ein runder Knopf mit Symbol; fokussiert weiss mit schwarzem Zeichen. */
@Composable
private fun TvSpurKnopf(
    feld: TvFeld,
    modifier: Modifier = Modifier,
    onFocus: (Boolean) -> Unit,
    onClick: () -> Unit,
) {
    var fokussiert by remember { mutableStateOf(false) }
    val hintergrund = if (fokussiert) Color.White else Color(0x8C000000)
    val glyph = if (fokussiert) Color.Black else Color.White
    Box(
        modifier
            .size(60.dp)
            .onFocusChanged { fokussiert = it.isFocused; onFocus(it.isFocused) }
            .clickable(role = Role.Button, onClick = onClick)
            .clip(CircleShape)
            .background(hintergrund),
        contentAlignment = Alignment.Center,
    ) {
        when (feld) {
            TvFeld.UNTERTITEL -> TvSprechblase(glyph, Modifier.size(30.dp))
            TvFeld.TON -> TvTonBalken(glyph, Modifier.size(30.dp))
            TvFeld.FOLGEN -> TvFolgenStriche(glyph, Modifier.size(30.dp))
            TvFeld.QUALITAET -> TvZahnrad(glyph, Modifier.size(30.dp))
        }
    }
}

/** Das Symbol für „Qualität": ein Zahnrad mit acht Zähnen — wie im Browser. */
@Composable
private fun TvZahnrad(farbe: Color, modifier: Modifier) {
    Canvas(modifier) {
        val s = size.minDimension
        val mitte = Offset(size.width / 2, size.height / 2)
        val strich = s * 0.1f
        for (i in 0 until 8) {
            val winkel = Math.toRadians(i * 45.0)
            val innen = s * 0.3f
            val aussen = s * 0.46f
            drawLine(farbe,
                Offset(mitte.x + innen * kotlin.math.cos(winkel).toFloat(), mitte.y + innen * kotlin.math.sin(winkel).toFloat()),
                Offset(mitte.x + aussen * kotlin.math.cos(winkel).toFloat(), mitte.y + aussen * kotlin.math.sin(winkel).toFloat()),
                strich * 1.4f, StrokeCap.Round)
        }
        drawCircle(farbe, radius = s * 0.3f, center = mitte, style = Stroke(width = strich))
        drawCircle(farbe, radius = s * 0.1f, center = mitte, style = Stroke(width = strich * 0.8f))
    }
}

/** Das Symbol für die Folgen: drei Zeilen, die unterste kürzer. */
@Composable
private fun TvFolgenStriche(farbe: Color, modifier: Modifier) {
    Canvas(modifier) {
        val s = size.minDimension
        listOf(0.27f to 0.84f, 0.5f to 0.84f, 0.73f to 0.58f).forEach { (y, ende) ->
            drawLine(farbe, Offset(s * 0.16f, s * y), Offset(s * ende, s * y), s * 0.09f, StrokeCap.Round)
        }
    }
}

/** „Nächste Folge": eine Pille mit Dreieck und Strich, fokussiert weiss. */
@Composable
private fun TvNaechsteFolgeKnopf(modifier: Modifier = Modifier, onFocus: (Boolean) -> Unit, onClick: () -> Unit) {
    var fokussiert by remember { mutableStateOf(false) }
    val farbe = if (fokussiert) Color.Black else Color.White
    Row(
        modifier
            .height(60.dp)
            .onFocusChanged { fokussiert = it.isFocused; onFocus(it.isFocused) }
            .clickable(role = Role.Button, onClick = onClick)
            .clip(CircleShape)
            .background(if (fokussiert) Color.White else Color(0x8C000000))
            .padding(horizontal = 24.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Canvas(Modifier.size(20.dp)) {
            val s = size.minDimension
            drawPath(Path().apply {
                moveTo(s * 0.2f, s * 0.2f); lineTo(s * 0.62f, s * 0.5f); lineTo(s * 0.2f, s * 0.8f); close()
            }, farbe)
            drawLine(farbe, Offset(s * 0.76f, s * 0.2f), Offset(s * 0.76f, s * 0.8f), s * 0.1f, StrokeCap.Round)
        }
        Spacer(Modifier.width(10.dp))
        Text(tx(R.string.naechste_folge), fontSize = 20.sp, fontWeight = FontWeight.SemiBold, color = farbe)
    }
}

/**
 * Das Folgen-Feld am rechten Rand, gebaut wie das für Ton und Untertitel:
 * ein eigenes Fenster, das die Fernbedienung für sich hat. Oben die
 * Staffeln als Pillen (links, rechts), darunter die Folgen mit Bild,
 * Haken und Balken; die laufende hell, und auf ihr steht der Fokus.
 */
@Composable
private fun TvFolgenfeld(
    staffeln: List<Item>,
    staffelId: String?,
    folgen: List<Item>?,
    aktuellId: String,
    bild: (Item) -> String?,
    onStaffel: (Item) -> Unit,
    onFolge: (Item) -> Unit,
    onClose: () -> Unit,
) {
    Dialog(onDismissRequest = onClose,
           properties = DialogProperties(usePlatformDefaultWidth = false)) {
        val fenster = (LocalView.current.parent as? DialogWindowProvider)?.window
        SideEffect { fenster?.setDimAmount(0f) }

        val anfang = remember { FocusRequester() }
        val liste = androidx.compose.foundation.lazy.rememberLazyListState()
        val ziel = folgen?.indexOfFirst { it.id == aktuellId }?.takeIf { it >= 0 } ?: 0
        LaunchedEffect(folgen) {
            if (folgen.isNullOrEmpty()) return@LaunchedEffect
            liste.scrollToItem(ziel)
            delay(100)
            runCatching { anfang.requestFocus() }
        }

        Box(Modifier.fillMaxSize()) {
            Column(
                Modifier
                    .align(Alignment.CenterEnd)
                    .padding(end = 48.dp, top = 40.dp, bottom = 40.dp)
                    .width(560.dp)
                    .clip(RoundedCornerShape(24.dp))
                    .background(Color(0xD912121A))
                    .padding(vertical = 20.dp, horizontal = 14.dp),
            ) {
                Text(tx(R.string.folgen), color = Tv.label3, fontSize = 14.sp,
                     fontWeight = FontWeight.SemiBold, letterSpacing = 1.5.sp,
                     modifier = Modifier.padding(start = 18.dp, bottom = 10.dp))
                if (staffeln.size > 1) {
                    androidx.compose.foundation.lazy.LazyRow(
                        contentPadding = PaddingValues(horizontal = 12.dp),
                        horizontalArrangement = Arrangement.spacedBy(8.dp),
                        modifier = Modifier.padding(bottom = 12.dp),
                    ) {
                        items(staffeln.size, key = { staffeln[it].id }) { i ->
                            val st = staffeln[i]
                            TvStaffelPille(AmStueck.staffelName(st), st.id == staffelId) { onStaffel(st) }
                        }
                    }
                }
                when {
                    folgen == null -> Box(Modifier.fillMaxWidth().height(100.dp), Alignment.Center) {
                        CircularProgressIndicator(color = Tv.label2)
                    }
                    folgen.isEmpty() -> Text(tx(R.string.keine_folgen), color = Tv.label3, fontSize = 18.sp,
                                             modifier = Modifier.padding(18.dp))
                    else -> androidx.compose.foundation.lazy.LazyColumn(state = liste) {
                        items(folgen.size, key = { folgen[it].id }) { i ->
                            val f = folgen[i]
                            TvFolgenZeile(f, f.id == aktuellId, bild(f),
                                if (i == ziel) Modifier.focusRequester(anfang) else Modifier) { onFolge(f) }
                        }
                    }
                }
            }
        }
    }
}

@Composable
private fun TvStaffelPille(text: String, gewaehlt: Boolean, onClick: () -> Unit) {
    var fokussiert by remember { mutableStateOf(false) }
    Text(
        text, fontSize = 17.sp,
        fontWeight = if (gewaehlt) FontWeight.SemiBold else FontWeight.Normal,
        color = if (fokussiert) Color.Black else Color.White,
        modifier = Modifier
            .onFocusChanged { fokussiert = it.isFocused }
            .clickable(role = Role.Button, onClick = onClick)
            .clip(CircleShape)
            .background(when {
                fokussiert -> Color.White
                gewaehlt -> Color(0x40FFFFFF)
                else -> Color(0x17FFFFFF)
            })
            .padding(horizontal = 18.dp, vertical = 8.dp))
}

@Composable
private fun TvFolgenZeile(f: Item, aktuell: Boolean, bild: String?, modifier: Modifier, onClick: () -> Unit) {
    var fokussiert by remember { mutableStateOf(false) }
    val vorn = if (fokussiert) Color.Black else Color.White
    val hinten = if (fokussiert) Color(0x99000000) else Tv.label3
    Row(
        modifier
            .fillMaxWidth()
            .onFocusChanged { fokussiert = it.isFocused }
            .clickable(role = Role.Button, onClick = onClick)
            .clip(RoundedCornerShape(14.dp))
            .background(when {
                fokussiert -> Color.White
                aktuell -> Color(0x29FFFFFF)
                else -> Color.Transparent
            })
            .padding(horizontal = 12.dp, vertical = 10.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Box(Modifier.width(150.dp).height(84.dp).clip(RoundedCornerShape(10.dp))
                .background(Color(0xFF1C1C21))) {
            if (bild != null) {
                AsyncImage(model = bild, contentDescription = null, contentScale = ContentScale.Crop,
                           modifier = Modifier.fillMaxSize())
            }
            if (f.progress > 1 && !f.finished) {
                Box(Modifier.align(Alignment.BottomCenter).fillMaxWidth().height(4.dp)
                        .background(Color(0x73000000))) {
                    Box(Modifier.fillMaxWidth((f.progress / 100).coerceIn(0.0, 1.0).toFloat())
                            .fillMaxHeight().background(Color.White))
                }
            }
        }
        Spacer(Modifier.width(16.dp))
        Column(Modifier.weight(1f)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(f.folgenNummer, fontSize = 18.sp, color = hinten, modifier = Modifier.width(30.dp))
                Text(f.title, fontSize = 19.sp, color = vorn, maxLines = 1, overflow = TextOverflow.Ellipsis,
                     fontWeight = if (aktuell) FontWeight.SemiBold else FontWeight.Normal,
                     modifier = Modifier.weight(1f, fill = false))
                if (f.finished) {
                    Spacer(Modifier.width(8.dp))
                    Text("✓", fontSize = 18.sp, fontWeight = FontWeight.Bold, color = vorn)
                }
            }
            Text(if (aktuell) tx(R.string.laeuft) else f.runtimeMs?.takeIf { it > 0 }?.let { Item.duration(it) } ?: "",
                 fontSize = 15.sp, color = if (aktuell && !fokussiert) Tv.accent else hinten,
                 modifier = Modifier.padding(start = 30.dp, top = 2.dp))
        }
    }
}

/** Das Symbol für Untertitel: eine Sprechblase mit zwei Zeilen. */
@Composable
private fun TvSprechblase(farbe: Color, modifier: Modifier) {
    Canvas(modifier) {
        val s = size.minDimension
        val strich = s * 0.085f
        val breite = s * 0.84f
        val hoehe = s * 0.62f
        val links = (size.width - breite) / 2
        val oben = s * 0.08f
        drawRoundRect(farbe, Offset(links, oben), Size(breite, hoehe),
                      CornerRadius(s * 0.16f), style = Stroke(strich))
        val zipfel = Path().apply {
            moveTo(links + breite * 0.22f, oben + hoehe - strich / 2)
            lineTo(links + breite * 0.16f, oben + hoehe + s * 0.2f)
            lineTo(links + breite * 0.42f, oben + hoehe - strich / 2)
        }
        drawPath(zipfel, farbe, style = Stroke(strich, cap = StrokeCap.Round))
        for ((i, laenge) in listOf(0.52f, 0.36f).withIndex()) {
            val y = oben + hoehe * (0.38f + i * 0.26f)
            drawLine(farbe, Offset(links + breite * 0.2f, y),
                     Offset(links + breite * (0.2f + laenge), y), strich, StrokeCap.Round)
        }
    }
}

/** Das Symbol für den Ton: drei Balken wie ein Pegel. */
@Composable
private fun TvTonBalken(farbe: Color, modifier: Modifier) {
    Canvas(modifier) {
        val s = size.minDimension
        val balken = s * 0.16f
        val abstand = s * 0.1f
        val gesamt = 3 * balken + 2 * abstand
        val links = (size.width - gesamt) / 2
        for ((i, anteil) in listOf(0.45f, 0.9f, 0.65f).withIndex()) {
            val h = s * anteil
            drawRoundRect(farbe, Offset(links + i * (balken + abstand), (size.height - h) / 2),
                          Size(balken, h), CornerRadius(balken / 2))
        }
    }
}

/**
 * „Vorspann überspringen": steht, solange die Stelle im Vorspann liegt,
 * und hat den Fokus — die Mitteltaste springt. Jede andere Taste ([onWeg])
 * gibt den Fokus zurück ans Bild, damit Spulen weiter geht.
 */
@Composable
private fun TvVorspannKnopf(onClick: () -> Unit, onWeg: () -> Unit, modifier: Modifier = Modifier) {
    var fokussiert by remember { mutableStateOf(false) }
    val fokus = remember { FocusRequester() }
    LaunchedEffect(Unit) {
        delay(100)
        runCatching { fokus.requestFocus() }
    }
    Row(
        modifier
            .focusRequester(fokus)
            .onKeyEvent { e ->
                if (e.type != KeyEventType.KeyDown) return@onKeyEvent false
                when (e.key) {
                    Key.DirectionCenter, Key.Enter, Key.NumPadEnter -> false
                    else -> { onWeg(); true }
                }
            }
            .onFocusChanged { fokussiert = it.isFocused }
            .clickable(role = Role.Button, onClick = onClick)
            .clip(RoundedCornerShape(30.dp))
            .background(if (fokussiert) Color.White else Color(0xD912121A))
            .padding(horizontal = 26.dp, vertical = 16.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(tx(R.string.vorspann_ueberspringen), fontSize = 20.sp, fontWeight = FontWeight.SemiBold,
             color = if (fokussiert) Color.Black else Color.White)
        Spacer(Modifier.width(10.dp))
        Text("›", fontSize = 24.sp, fontWeight = FontWeight.Bold,
             color = if (fokussiert) Color.Black else Color.White)
    }
}

/**
 * „Als Nächstes" mit Bild und Countdown. [restImFilm] ist die Zeit bis zum
 * Ende des laufenden Titels; der Countdown folgt ihr, mindestens aber
 * [mindest] Sekunden. Der Fokus steht auf „Jetzt"; rechts davon „Abbrechen".
 */
@Composable
private fun TvAlsNaechstes(
    bild: String?,
    beschriftung: String,
    restImFilm: Int,
    mindest: Int,
    onCancel: () -> Unit,
    onNow: () -> Unit,
    modifier: Modifier = Modifier,
) {
    var vergangen by remember { mutableStateOf(0) }
    val jetzt by rememberUpdatedState(onNow)
    val jetztFokus = remember { FocusRequester() }
    val rest = maxOf(restImFilm, mindest - vergangen).coerceAtLeast(0)

    LaunchedEffect(Unit) {
        delay(100)
        runCatching { jetztFokus.requestFocus() }
    }
    LaunchedEffect(Unit) {
        while (true) {
            delay(1000)
            vergangen++
        }
    }
    LaunchedEffect(rest) { if (rest <= 0) jetzt() }

    Row(
        modifier
            .widthIn(max = 700.dp)
            .clip(RoundedCornerShape(20.dp))
            .background(Color(0xE612121A))
            .padding(22.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        if (bild != null) {
            AsyncImage(
                model = bild, contentDescription = null, contentScale = ContentScale.Crop,
                modifier = Modifier.width(192.dp).height(108.dp).clip(RoundedCornerShape(10.dp))
                    .background(Color(0xFF1C1C21)))
            Spacer(Modifier.width(20.dp))
        }
        Column(Modifier.weight(1f)) {
            Text(tx(R.string.als_naechstes_in_x_s, rest), color = Tv.label2, fontSize = 16.sp)
            Text(beschriftung, fontSize = 24.sp, fontWeight = FontWeight.SemiBold,
                 maxLines = 2, overflow = TextOverflow.Ellipsis)
            Spacer(Modifier.height(16.dp))
            Row(horizontalArrangement = Arrangement.spacedBy(14.dp)) {
                TvButton(tx(R.string.jetzt), modifier = Modifier.focusRequester(jetztFokus)) { onNow() }
                TvButton(tx(R.string.abbrechen)) { onCancel() }
            }
        }
    }
}

/**
 * Ob der Abspieler an einer 401 gescheitert ist — dann gilt der
 * Medienschlüssel in der Adresse nicht mehr (meist: der Server wurde neu
 * gestartet). Das lässt sich beheben; jeder andere Fehler nicht so.
 */
@androidx.annotation.OptIn(androidx.media3.common.util.UnstableApi::class)
internal fun medienSchluesselAbgewiesen(error: androidx.media3.common.PlaybackException): Boolean =
    generateSequence<Throwable>(error) { it.cause }.any {
        it is androidx.media3.datasource.HttpDataSource.InvalidResponseCodeException && it.responseCode == 401
    }

/**
 * Ob ein Fehler des Abspielers ein „410 Gone" auf ein HLS-Stück ist: Nach
 * einer langen Pause hat der Server die Umwandlung aufgeräumt.
 */
@androidx.annotation.OptIn(androidx.media3.common.util.UnstableApi::class)
internal fun hlsAbgelaufen(error: androidx.media3.common.PlaybackException): Boolean =
    generateSequence<Throwable>(error) { it.cause }.any {
        it is androidx.media3.datasource.HttpDataSource.InvalidResponseCodeException && it.responseCode == 410
    }

/** Das Vorschaubild beim Spulen: eine Kachel aus einem Trickplay-Blatt, darunter die Zeit. */
@Composable
private fun TvTrickplay(trickplay: Trickplay, blattUrl: (Int) -> String?, sekunden: Double, modifier: Modifier) {
    val kachel = trickplay.kachel(sekunden)
    val context = LocalContext.current
    val blaetter = remember(trickplay) { mutableStateMapOf<Int, androidx.compose.ui.graphics.ImageBitmap>() }
    val nr = kachel?.blatt
    LaunchedEffect(trickplay, nr) {
        if (nr == null || blaetter.containsKey(nr)) return@LaunchedEffect
        val url = blattUrl(nr) ?: return@LaunchedEffect
        val ergebnis = coil.Coil.imageLoader(context).execute(
            coil.request.ImageRequest.Builder(context).data(url).allowHardware(false).build())
        val bild = ((ergebnis as? coil.request.SuccessResult)?.drawable as? android.graphics.drawable.BitmapDrawable)?.bitmap
        if (bild != null) blaetter[nr] = bild.asImageBitmap()
    }
    val breite = 320.dp
    val hoehe = breite * (trickplay.hoehe.toFloat() / trickplay.breite.coerceAtLeast(1))
    Column(modifier, horizontalAlignment = Alignment.CenterHorizontally) {
        val bild = nr?.let { blaetter[it] }
        if (kachel != null && bild != null) {
            androidx.compose.foundation.Canvas(Modifier.width(breite).height(hoehe).clip(RoundedCornerShape(12.dp))) {
                drawImage(bild,
                    srcOffset = androidx.compose.ui.unit.IntOffset(kachel.x, kachel.y),
                    srcSize = androidx.compose.ui.unit.IntSize(kachel.breite, kachel.hoehe),
                    dstSize = androidx.compose.ui.unit.IntSize(size.width.toInt(), size.height.toInt()))
            }
        } else {
            Box(Modifier.width(breite).height(hoehe).clip(RoundedCornerShape(12.dp)).background(Color(0x99000000)))
        }
        Spacer(Modifier.height(6.dp))
        Text(clock(sekunden), fontSize = 18.sp, color = Color.White,
             modifier = Modifier.clip(RoundedCornerShape(8.dp)).background(Color(0x99000000))
                 .padding(horizontal = 10.dp, vertical = 3.dp))
    }
}

/**
 * Ob der Abspieler an einer 403 gescheitert ist — meist ist dann die
 * Bildschirmzeit des Kinderprofils um; der Plan nennt den Grund.
 */
@androidx.annotation.OptIn(androidx.media3.common.util.UnstableApi::class)
internal fun zugriffAbgewiesen(error: androidx.media3.common.PlaybackException): Boolean =
    generateSequence<Throwable>(error) { it.cause }.any {
        it is androidx.media3.datasource.HttpDataSource.InvalidResponseCodeException && it.responseCode == 403
    }
