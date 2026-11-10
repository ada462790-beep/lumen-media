// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package org.lumen.app

import android.view.View
import androidx.activity.compose.BackHandler
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.*
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.ui.viewinterop.AndroidView
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
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import org.lumen.core.*

/**
 * Was abgespielt werden soll: ein Titel, und falls er aus einer Liste
 * kommt, die ganze Liste samt Platz darin — damit es danach weitergeht.
 */
data class Abspielen(
    val titel: Item,
    val liste: List<Item> = emptyList(),
    val index: Int = -1,
    // Ab wo der erste Titel läuft, in Sekunden — 0.0 für „Von vorn". Null
    // heisst: beim gespeicherten Stand.
    val ab: Double? = null,
    // Der Name der Liste, die am Stück läuft — für die Zeile oben im Bild.
    val listenName: String? = null,
    // Welche Fassung des ersten Titels läuft — gewählt unter
    // „In welcher Qualität?". Null heisst: die erste, wie der Server sie ordnet.
    val dateiId: String? = null,
)

/**
 * Der Player.
 *
 * Darunter steckt ExoPlayer. Der versteht HLS von Haus aus — und weil der
 * Server die vollständige Abspielliste vorab ausliefert, funktioniert damit
 * auch das Springen in einen Film, der gerade erst umgewandelt wird.
 *
 * Die Bedienung übernimmt [PlayerView]: Zeitleiste, Spulen, Pause. Lumen
 * legt Vollbild quer, Bild im Bild, Tastatur und Gamepad dazu, und eine
 * MediaSession, damit Kopfhörertasten und das Bild-im-Bild-Fenster den Film
 * steuern. Im Hintergrund spielt Lumen bewusst nicht weiter: wer die App
 * verlässt, hält den Film an (siehe [BeimVerlassenAnhalten]). Darüber liegt,
 * was Lumen selbst beisteuert: Ton und Untertitel, „Vorspann überspringen",
 * „Als Nächstes" (Spurmenue.kt) und die Vorschaubilder beim Spulen.
 *
 * Was als Nächstes kommt, entscheidet [AmStueck] in lumen-core; hier wird
 * es nur angezeigt. In einer Liste ist es der nächste Eintrag, bei einer
 * Folge die nächste Folge — Letzteres nur, wenn „Folgen automatisch
 * weiter" eingeschaltet ist. Gesucht wird schon im Abspann, damit die Karte
 * dort steht und bis zum Ende herunterzählt.
 */
@Composable
fun PlayerScreen(model: LumenViewModel, start: Abspielen, onClose: () -> Unit) {
    var aktuell by remember { mutableStateOf(start.titel) }
    var index by remember { mutableStateOf(start.index) }
    var naechstes by remember { mutableStateOf<AmStueck.Naechstes?>(null) }
    // Für welchen Titel schon gesucht wurde — und für welchen jemand
    // „Abbrechen" gedrückt hat. Dann bleibt es am Ende still.
    var gesuchtFuer by remember { mutableStateOf<String?>(null) }
    var abgelehntFuer by remember { mutableStateOf<String?>(null) }
    // „Von vorn" gilt nur für den ersten Titel; danach zählt der Stand.
    var startAb by remember { mutableStateOf(start.ab) }
    // Ein Film in Teilen: Wo der nächste Teil beginnt (Gesamtstelle über
    // alle Teile), und ein Zähler, der für jeden Teil einen frischen Player
    // erzwingt — wie im Browser, der dafür startPlayer neu aufruft.
    var teilAb by remember { mutableStateOf<Double?>(null) }
    var teilLauf by remember { mutableStateOf(0) }
    // Eines pro Wiedergabe: es merkt sich, welche Titel eine Datei haben.
    val folge = remember { Abspielfolge(model.client) }
    val scope = rememberCoroutineScope()

    fun weiter(n: AmStueck.Naechstes) {
        naechstes = null
        startAb = null
        teilAb = null
        if (n.index >= 0) index = n.index
        aktuell = n.titel
    }

    fun suchen(beendet: Item) {
        if (gesuchtFuer == beendet.id || abgelehntFuer == beendet.id) return
        gesuchtFuer = beendet.id
        val ab = index + 1
        scope.launch {
            val gefunden = if (start.liste.isNotEmpty()) {
                // Die Liste läuft am Stück — ein ausdrücklicher Wunsch,
                // darum unabhängig von der Einstellung für Folgen.
                model.fetch { folge.naechstesInListe(start.liste, ab) }
            } else if (beendet.kind == "episode" && model.me?.prefs?.get("autoplay") != "0") {
                model.fetch { folge.folgeNach(beendet) }
            } else {
                null
            }
            // Inzwischen könnte jemand abgebrochen oder weitergeschaltet haben.
            if (aktuell.id == beendet.id && abgelehntFuer != beendet.id) naechstes = gefunden
        }
    }

    // Vollbild und quer gilt für die ganze Wiedergabe, über Folgen hinweg.
    Vollbild()

    Box(Modifier.fillMaxSize().background(Color.Black)) {
        // Ein neuer Titel bekommt einen ganz neuen Player samt Zustand —
        // so bleibt nichts vom vorigen hängen, weder Stand noch Haken.
        key(aktuell.id, teilLauf) {
            Wiedergabe(
                model, aktuell,
                abSekunden = teilAb ?: if (aktuell.id == start.titel.id) startAb else null,
                // Die gewählte Fassung gilt nur für den Titel, für den sie
                // gewählt wurde — auch über seine Teile hinweg.
                dateiId = if (aktuell.id == start.titel.id) start.dateiId else null,
                // Oben im Bild: aus welcher Liste und der wievielte Titel —
                // wie im Browser „Filmabend · 2 von 5".
                unterzeile = if (start.liste.isNotEmpty() && index >= 0)
                    tx(R.string.liste_platz, start.listenName ?: tx(R.string.liste), index + 1, start.liste.size)
                else null,
                naechstes = naechstes,
                onAbspann = { suchen(it) },
                onEnded = { beendet ->
                    // Steht „Als Nächstes" schon da, zählt die Karte selbst
                    // zu Ende — sonst jetzt nachsehen.
                    if (naechstes == null) suchen(beendet)
                },
                // Noch ein Teil übrig: gleich weiter, ohne Countdown — es
                // ist derselbe Film.
                onNaechsterTeil = { ab -> teilAb = ab; teilLauf++ },
                onNaechstes = { naechstes?.let { weiter(it) } },
                onAbbrechen = { abgelehntFuer = aktuell.id; naechstes = null },
                // „Nächste Folge" oder eine Folge aus dem Folgen-Feld.
                onWechsel = { neu -> naechstes = null; startAb = null; teilAb = null; aktuell = neu },
                onClose = onClose,
            )
        }
    }
}

/** Welches Feld über dem Bild offen ist — oder keines. */
private enum class Offen { TON, UNTERTITEL, FOLGEN }

@androidx.annotation.OptIn(androidx.media3.common.util.UnstableApi::class)
@Composable
private fun Wiedergabe(
    model: LumenViewModel,
    item: Item,
    abSekunden: Double?,
    dateiId: String?,
    unterzeile: String?,
    naechstes: AmStueck.Naechstes?,
    onAbspann: (Item) -> Unit,
    onEnded: (Item) -> Unit,
    onNaechsterTeil: (Double) -> Unit,
    onNaechstes: () -> Unit,
    onAbbrechen: () -> Unit,
    onWechsel: (Item) -> Unit,
    onClose: () -> Unit,
) {
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    var player by remember { mutableStateOf<ExoPlayer?>(null) }
    var fehler by remember { mutableStateOf<String?>(null) }
    var detail by remember { mutableStateOf<ItemDetail?>(null) }
    var datei by remember { mutableStateOf<MediaFile?>(null) }
    var plan by remember { mutableStateOf<PlaybackPlan?>(null) }
    var laenge by remember { mutableStateOf(0.0) }
    // Ein Film in Teilen: alle Teile, welcher läuft, und was davor kommt.
    // Der Stand geht als Gesamtstelle an den Server — vorher + Stelle im
    // Teil, Dauer aller Teile —, damit „Weiter bei" überall dasselbe meint.
    var teile by remember { mutableStateOf<FilmTeile?>(null) }
    var teil by remember { mutableStateOf(0) }
    var vorher by remember { mutableStateOf(0.0) }
    var umgewandelt by remember { mutableStateOf(false) }
    // Ob nach einer 401 schon einmal ein neuer Medienschlüssel geholt wurde.
    var schluesselErneuert by remember { mutableStateOf(false) }
    // Wann zuletzt nach einem 410 neu angesetzt wurde — damit ein Server,
    // der immer 410 sagt, nicht in eine Schleife führt.
    var zuletztNeuAngesetzt by remember { mutableStateOf(0L) }
    // Die MediaSession: Kopfhörertasten, Bild-im-Bild-Steuerung.
    var sitzung by remember { mutableStateOf<androidx.media3.session.MediaSession?>(null) }
    // Vorschaubilder fürs Spulen, und wo gerade gespult wird (null: gar nicht).
    var trickplay by remember { mutableStateOf<Trickplay?>(null) }
    var spultBei by remember { mutableStateOf<Long?>(null) }

    // Ton und Untertitel: was gewählt ist, und ob das Feld dazu offen ist.
    var wahl by remember { mutableStateOf(Spuren.Wahl()) }
    // Die Untertitelspuren, die der Server schon fertig hat. Nur die gehen
    // an den Abspieler: eine Spur, die er erst noch aus der Datei zieht,
    // hielte den Start bis zu 20 Sekunden auf oder käme als JSON an.
    var utBereit by remember { mutableStateOf(emptySet<Int>()) }
    // Die HLS-Sitzung, falls umgewandelt wird — beim Schliessen beenden.
    var hlsSitzung by remember { mutableStateOf<HlsSitzung?>(null) }
    var offen by remember { mutableStateOf<Offen?>(null) }
    var steuerungSichtbar by remember { mutableStateOf(true) }

    // Das Folgen-Feld: die Staffeln der Serie, welche gewählt ist, und
    // deren Folgen — geladen beim ersten Öffnen.
    var staffeln by remember { mutableStateOf<List<Item>?>(null) }
    var folgenStaffel by remember { mutableStateOf(item.parentId) }
    var folgenListe by remember { mutableStateOf<List<Item>?>(null) }
    var kurzHinweis by remember { mutableStateOf<String?>(null) }
    val istFolge = item.kind == "episode"

    // Wie die Untertitel aussehen: Grösse und Hintergrund aus den Prefs.
    val utStil = UntertitelStil.aus(model.me?.prefs ?: emptyMap())

    // Vorspann und Abspann.
    var intro by remember { mutableStateOf<Vorspann?>(null) }
    var introUebersprungen by remember { mutableStateOf(false) }
    var abspannGemeldet by remember { mutableStateOf(false) }
    var positionMs by remember { mutableStateOf(0L) }
    var beendet by remember { mutableStateOf(false) }

    // Gehört dem Titel, nicht der Schleife: den Haken setzt entweder die
    // Schleife unten oder das Ende — aber nur einmal.
    var fertigGemeldet by remember { mutableStateOf(false) }
    val amEnde by rememberUpdatedState(onEnded)
    val imAbspann by rememberUpdatedState(onAbspann)
    val teilZuEnde by rememberUpdatedState(onNaechsterTeil)

    /** Die Gesamtstelle über alle Teile — bei einer Datei einfach die Stelle. */
    fun gesamtStelle(imTeil: Double): Double = vorher + imTeil
    /** Die Dauer, die der Server bekommt: die aller Teile, sonst die der Datei. */
    fun gesamtLaenge(): Double = teile?.takeIf { it.mehrere }?.gesamtSekunden?.takeIf { it > 0 } ?: laenge
    /** Ob dies der letzte (oder einzige) Teil ist — erst danach ist der Film gesehen. */
    fun letzterTeil(): Boolean = teile?.istLetzter(teil) ?: true

    fun alsGesehen() {
        if (fertigGemeldet) return
        fertigGemeldet = true
        model.send { it.markFinished(item.id) }
    }

    // Als Funktion, nicht als Wert: der Abspieler ruft von seinem Zuhörer
    // aus hierher, und der stammt aus der ersten Zusammensetzung — als die
    // Datei noch nicht da war. Gelesen wird darum immer der aktuelle Stand.
    fun spuren(): List<Track> = datei?.tracks ?: emptyList()

    /**
     * Das MediaItem: der Strom mit der gewünschten Tonspur, und die
     * fertigen Text-Untertitel seitlich dazu. Die gewählte Spur bekommt den
     * Vorrang; erkannt wird sie später an ihrer Kennung „spur-N".
     */
    fun mediaItem(): MediaItem? {
        val d = datei ?: return null
        val p = plan ?: return null
        val adresse = model.client.streamUrl(d, p, wahl.ton)
        val untertitel = Spuren.text(d.tracks).filter { it.index in utBereit }.map { t ->
            MediaItem.SubtitleConfiguration.Builder(
                android.net.Uri.parse(model.client.subtitleUrl(d.id, t.index)))
                .setMimeType(MimeTypes.TEXT_VTT)
                .setId("spur-${t.index}")
                .setLanguage(t.language?.takeIf { it.isNotBlank() && it != "und" })
                .setLabel(t.anzeigename)
                .setSelectionFlags(if (wahl.untertitel == t.index) C.SELECTION_FLAG_DEFAULT else 0)
                .build()
        }
        return MediaItem.Builder().setUri(adresse).setSubtitleConfigurations(untertitel).build()
    }

    /** Die Untertitel im Abspieler auf die Wahl bringen — oder ganz aus. */
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

    /**
     * Läuft die Datei direkt, stecken alle Tonspuren im Strom, und der
     * Abspieler muss die gewählte nehmen. Er kennt die Spuren der Reihe
     * nach, nicht unter der Nummer des Servers — darum über die Stelle,
     * zur Not über die Sprache. Beim Umpacken hat der Server schon gewählt.
     */
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

    /** Den Strom (neu) laden, ab [abMs] — beim Start und beim Wechsel der Tonspur. */
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

    fun merken(key: String, wert: String) {
        model.send(onDone = { model.refreshMe() }) { it.updatePrefs(mapOf(key to wert)) }
    }

    fun tonWaehlen(t: Track) {
        if (wahl.ton == t.index) return
        wahl = wahl.copy(ton = t.index)
        Spuren.prefWertTon(t)?.let { merken(Spuren.PREF_TON, it) }
        // Der Ton steckt im Strom: neu öffnen, an derselben Stelle.
        player?.let { p -> laden(p, p.currentPosition) }
    }

    fun untertitelWaehlen(t: Track?) {
        wahl = wahl.copy(untertitel = t?.index)
        merken(Spuren.PREF_UNTERTITEL, Spuren.prefWertUntertitel(t))
        player?.let { untertitelAnwenden(it) }
    }

    fun vorspannUeberspringen() {
        val i = intro ?: return
        introUebersprungen = true
        player?.seekTo(i.bisMs)
    }

    /**
     * Den Stand an den Server — beim Wechseln, Schliessen, Verlassen der
     * App und wenn die Ansicht verschwindet. Über model.send: das läuft im
     * ViewModel weiter, auch wenn diese Ansicht schon weg ist.
     */
    fun standSichern() {
        val p = player ?: return
        val position = p.currentPosition / 1000.0
        if (position > 0 && laenge > 0) {
            val stelle = gesamtStelle(position)
            val dauer = gesamtLaenge()
            model.send { it.saveProgress(item.id, stelle, dauer) }
        }
    }

    /** Den Stand sichern und zu einer anderen Folge wechseln. */
    fun wechseln(neu: Item) {
        if (neu.id == item.id) return
        standSichern()
        offen = null
        onWechsel(neu)
    }

    /** „Nächste Folge": die Folge danach, über das Staffelende hinweg. */
    fun naechsteFolge() {
        scope.launch {
            val n = model.fetch { Abspielfolge(it).folgeNach(item) }
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
            folgenListe = AmStueck.folgenOrdnen(model.fetch { it.children(id) } ?: emptyList())
        }
    }

    fun folgenOeffnen() {
        offen = Offen.FOLGEN
        if (staffeln != null) return
        val sid = detail?.seriesId
        scope.launch {
            staffeln = if (sid == null) emptyList()
                       else AmStueck.staffelnOrdnen(model.fetch { it.item(sid) }?.seasons ?: emptyList())
        }
        item.parentId?.let { staffelLaden(it) } ?: run { folgenListe = emptyList() }
    }

    // Vorbereiten: Datei holen, Plan erfragen, Spuren vorbelegen, Adresse bauen.
    LaunchedEffect(item.id) {
        val d = model.fetch { it.item(item.id) }
        // Die gewählte Fassung, sonst die erste — und dazu ihre Teile.
        val erste = d?.let { Fassungen.datei(it.files, dateiId) }
        if (d == null || erste == null) {
            fehler = tx(R.string.zu_diesem_titel_gibt_es)
            return@LaunchedEffect
        }
        val alle = Fassungen.teile(d.files, erste)
        // Aus der Gesamtstelle der Teil und die Stelle darin.
        val stelle = alle.stelle(abSekunden ?: item.resumeSeconds)
        val f = alle.dateien[stelle.teil]
        // Mit dem Plan ein frischer Medienschlüssel: der Film läuft Stunden,
        // und der Abspieler lädt die ganze Zeit mit derselben Adresse nach.
        val pl = model.fetch { it.wiedergabeVorbereiten(f.id) }
        if (pl == null) {
            fehler = tx(R.string.der_server_konnte_die_wiedergabe)
            return@LaunchedEffect
        }
        detail = d
        datei = f
        plan = pl
        teile = alle
        teil = stelle.teil
        vorher = stelle.vorher
        // Der Vorspann liegt im ersten Teil; in Teil 2 wäre die Marke falsch.
        intro = if (stelle.teil == 0) d.intro else null
        umgewandelt = !pl.direct
        wahl = Spuren.vorbelegen(f.tracks, model.me?.prefs ?: emptyMap())

        // Ist die vorgewählte Untertitelspur schon fertig, kommt sie gleich
        // mit; sonst startet der Film ohne, und sie folgt, sobald sie da ist.
        // Kurz gefragt: der Server wartet dafür nur eine Sekunde.
        utBereit = emptySet()
        wahl.untertitel?.let { i ->
            val bereit = withContext(Dispatchers.IO) { oderNull { model.client.untertitelBereit(f.id, i, kurz = true) } }
            if (bereit == true) utBereit = setOf(i)
        }

        val dauer = if (f.durationSeconds > 0) f.durationSeconds else item.runtimeSeconds
        laenge = dauer

        // Ein Stand hinter dem Ende passt nicht mehr zur Datei — etwa weil
        // sie durch eine kürzere Fassung ersetzt wurde. Dann von vorn.
        var ab = stelle.imTeil
        if (dauer > 0 && ab > dauer - 5) ab = 0.0

        // Gebaut und zugewiesen ohne Unterbrechung dazwischen: Früher lag
        // dazwischen ein withContext — wurde die Ansicht genau dann
        // geschlossen, blieb ein Player übrig, den niemand mehr freigab.
        // LaunchedEffect läuft ohnehin auf dem Hauptfaden.
        player = ExoPlayer.Builder(context).build().apply {
                addListener(object : Player.Listener {
                    override fun onPlayerError(error: androidx.media3.common.PlaybackException) {
                        // 410 nach langer Pause: Der Server hat die
                        // Umwandlung aufgeräumt. Kein Fehler für die Person —
                        // ein neuer Plan, und an derselben Stelle weiter.
                        val jetzt = System.currentTimeMillis()
                        if (hlsAbgelaufen(error) && jetzt - zuletztNeuAngesetzt > 30_000) {
                            zuletztNeuAngesetzt = jetzt
                            val p = this@apply
                            val stelle = p.currentPosition
                            val dateiJetzt = datei
                            scope.launch {
                                val neuerPlan = dateiJetzt?.let { d -> model.abfragen { it.wiedergabeVorbereiten(d.id) }.wert }
                                if (neuerPlan != null) {
                                    plan = neuerPlan
                                    laden(p, stelle)
                                } else {
                                    fehler = tx(R.string.der_strom_ist_abgerissen_x, error.errorCodeName)
                                }
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
                                val erneuert = withContext(Dispatchers.IO) { model.client.wiedergabeSchluesselErneuern() }
                                if (erneuert) laden(p, stelle)
                                else fehler = tx(R.string.der_strom_ist_abgerissen_x, error.errorCodeName)
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

                    // Erst jetzt kennt der Abspieler seine Spuren — also
                    // erst jetzt lässt sich sagen, welche laufen sollen.
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
        sitzung = player?.let { p ->
            androidx.media3.session.MediaSession.Builder(context, p)
                .setId("lumen-" + item.id + "-" + System.nanoTime())
                .build()
        }
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
            when (withContext(Dispatchers.IO) { oderNull { model.client.untertitelBereit(d.id, i) } }) {
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

    // Vorschaubilder fürs Spulen. 202 heisst „wird gerade erstellt" — dann
    // ein paarmal später nachfragen; 404 heisst: gibt es nicht.
    LaunchedEffect(datei?.id) {
        val f = datei ?: return@LaunchedEffect
        repeat(4) {
            when (val stand = model.abfragen { it.trickplay(f.id) }.wert) {
                is TrickplayStand.Bereit -> { trickplay = stand.trickplay; return@LaunchedEffect }
                TrickplayStand.WirdErstellt -> delay(30_000)
                else -> return@LaunchedEffect
            }
        }
    }

    // Die Stelle im Blick behalten: für Vorspann, Abspann und die Karte —
    // und alle fünf Sekunden den Stand sichern.
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

            if (Marken.imVorspann(intro, pos) && !introUebersprungen &&
                model.me?.prefs?.get("skipIntro") == "1") {
                vorspannUeberspringen()
            }
            // Der Abspann liegt im letzten Teil — vorher kommt kein „Als Nächstes".
            if (!abspannGemeldet && letzterTeil() && Marken.imAbspann(detail?.creditsMs, dauerMs, pos)) {
                abspannGemeldet = true
                imAbspann(item)
            }

            if (tick % 10 == 0 && pos > 0 && laenge > 0) {
                val position = gesamtStelle(pos / 1000.0)
                val dauer = gesamtLaenge()
                if (kotlin.math.abs(position - gesichert) >= 1.0) {
                    gesichert = position
                    model.send { it.saveProgress(item.id, position, dauer) }
                }
                if (letzterTeil() && position / dauer >= SessionController.FINISHED_AT) alsGesehen()
            }
        }
    }

    fun freigeben() {
        sitzung?.release()
        sitzung = null
        player?.release()
        player = null
        // Die Umwandlung gleich beenden, statt sie zwei Minuten weiterlaufen
        // zu lassen. Ohne Warten, und Fehler zählen nicht.
        hlsSitzung?.let { h ->
            hlsSitzung = null
            h.beendenImHintergrund(model.client)
        }
    }

    fun beenden() {
        standSichern()
        freigeben()
        onClose()
    }

    BackHandler {
        if (offen != null) offen = null else beenden()
    }
    // Verschwindet die Ansicht auf anderem Weg (abgemeldet, neue Folge),
    // soll der Stand trotzdem ankommen — und nichts weiterlaufen.
    DisposableEffect(Unit) { onDispose { standSichern(); freigeben() } }
    // Die App verlassen: anhalten und den Stand sichern.
    BeimVerlassenAnhalten(player) { standSichern() }
    // Läuft etwas, darf es ins Bild-im-Bild-Fenster; dort keine eigene Steuerung.
    val imFenster = imBildImBild(aktiv = player != null && fehler == null)
    Tasten(player)

    /** Die Abschnitte des offenen Felds. */
    fun abschnitte(feld: Offen): List<Spurabschnitt> = when (feld) {
        Offen.FOLGEN -> emptyList()
        Offen.TON -> buildList {
            val ton = Spuren.ton(spuren())
            if (ton.size > 1) {
                add(Spurabschnitt(tx(R.string.tonspur), ton.mapIndexed { i, t ->
                    Spureintrag(t.anzeigename,
                        gewaehlt = wahl.ton == t.index || (wahl.ton < 0 && i == 0),
                        onClick = { tonWaehlen(t) })
                }))
            }
            // Den Vorspann für eine Serie markieren gehört zur Verwaltung —
            // und die gibt es nur im Browser und in der Mac-App.
        }
        Offen.UNTERTITEL -> {
            val text = Spuren.text(spuren())
            listOf(Spurabschnitt(tx(R.string.untertitel), buildList {
                add(Spureintrag(tx(R.string.aus), gewaehlt = wahl.untertitel == null,
                    onClick = { untertitelWaehlen(null) }))
                text.forEach { t ->
                    add(Spureintrag(t.anzeigename, gewaehlt = wahl.untertitel == t.index,
                        onClick = { untertitelWaehlen(t) }))
                }
            }))
        }
    }

    // ------------------------------------------------------------ Bild

    Box(Modifier.fillMaxSize().background(Color.Black)) {
        val p = player
        if (p != null) {
            AndroidView(
                factory = { ctx ->
                    PlayerView(ctx).apply {
                        this.player = p
                        useController = true
                        setShowNextButton(false)
                        setShowPreviousButton(false)
                        setShowSubtitleButton(false)
                        controllerShowTimeoutMs = 2800
                        setControllerVisibilityListener(
                            PlayerView.ControllerVisibilityListener { sichtbarkeit ->
                                steuerungSichtbar = sichtbarkeit == View.VISIBLE
                            })
                        // Beim Spulen über die Zeitleiste: wo man landen würde.
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
                        // Untertitel wie eingestellt: Grösse und Hintergrund
                        // aus den Prefs, eingebettete Schriftgrössen nicht.
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
                    }
                },
                update = { view ->
                    view.player = p
                    // Im kleinen Fenster keine Steuerung — die bringt das System.
                    view.useController = !imFenster
                },
                modifier = Modifier.fillMaxSize(),
            )
        } else if (fehler == null) {
            Column(
                Modifier.align(Alignment.Center),
                horizontalAlignment = Alignment.CenterHorizontally,
            ) {
                CircularProgressIndicator(color = Lumen.label2)
                Spacer(Modifier.height(12.dp))
                Text(item.title, color = Lumen.label2)
            }
        }

        fehler?.let {
            Column(
                Modifier.align(Alignment.Center).padding(28.dp),
                horizontalAlignment = Alignment.CenterHorizontally,
            ) {
                Text(tx(R.string.das_laesst_sich_nicht_abspielen), fontSize = 18.sp)
                Spacer(Modifier.height(8.dp))
                Text(it, color = Lumen.label2)
                Spacer(Modifier.height(18.dp))
                QuietButton(tx(R.string.zurueck)) { beenden() }
            }
        }

        // Beim Spulen: das Vorschaubild über der Zeitleiste.
        val tp = trickplay
        val spult = spultBei
        if (tp != null && spult != null && !imFenster) {
            TrickplayVorschau(tp, { n -> tp.blaetter.getOrNull(n)?.let { model.client.trickplayBlattUrl(it) } },
                spult / 1000.0, Modifier.align(Alignment.BottomCenter).padding(bottom = 96.dp))
        }

        // Oben links, solange die Steuerung steht: was läuft.
        if (p != null && steuerungSichtbar && offen == null && fehler == null && !imFenster) {
            Column(Modifier.align(Alignment.TopStart).padding(start = 18.dp, top = 16.dp, end = 120.dp)
                       .background(Color(0x66000000), androidx.compose.foundation.shape.RoundedCornerShape(12.dp))
                       .padding(horizontal = 12.dp, vertical = 8.dp)) {
                Text(item.title, fontSize = 16.sp, maxLines = 1,
                     overflow = androidx.compose.ui.text.style.TextOverflow.Ellipsis)
                // Bei einem Film in Teilen: „Teil 2 von 3".
                val unten = unterzeile ?: teile?.beschriftung(teil) ?: if (istFolge)
                    listOfNotNull(item.seasonNo?.let { tx(R.string.staffel_x, it) }, item.episodeNo?.let { tx(R.string.folge_x, it) })
                        .joinToString(", ") else ""
                if (unten.isNotEmpty()) Text(unten, fontSize = 12.sp, color = Lumen.label2)
            }
        }

        // Auf den Chromecast: das Gerüst übernimmt, sobald er verbunden ist.
        if (p != null && steuerungSichtbar && offen == null && !imFenster) {
            CastKnopf(model, Modifier.align(Alignment.TopEnd).padding(top = 34.dp, end = 8.dp))
        }

        if (umgewandelt && fehler == null && !imFenster) {
            Text(
                tx(R.string.wird_umgewandelt),
                color = Lumen.accent, fontSize = 12.sp,
                modifier = Modifier.align(Alignment.TopEnd).padding(14.dp),
            )
        }

        // Rechts unten über der Zeitleiste: Untertitel und Ton — nur, wenn
        // es etwas zu wählen gibt (oder der Vorspann zu markieren ist).
        val zeigeTon = Spuren.ton(spuren()).size > 1
        val zeigeText = Spuren.text(spuren()).isNotEmpty()
        if (p != null && (steuerungSichtbar || offen != null) && (zeigeTon || zeigeText || istFolge) &&
            naechstes == null && !imFenster) {
            Row(
                Modifier.align(Alignment.BottomEnd).padding(end = 16.dp, bottom = 92.dp),
                horizontalArrangement = Arrangement.spacedBy(10.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                // Bei einer Folge: weiter zur nächsten und das Folgen-Feld.
                if (istFolge) {
                    NaechsteFolgeKnopf { naechsteFolge() }
                    SpurKnopf(Spurfeld.FOLGEN, offen == Offen.FOLGEN) {
                        if (offen == Offen.FOLGEN) offen = null else folgenOeffnen()
                    }
                }
                if (zeigeText) {
                    SpurKnopf(Spurfeld.UNTERTITEL, offen == Offen.UNTERTITEL) {
                        offen = if (offen == Offen.UNTERTITEL) null else Offen.UNTERTITEL
                    }
                }
                if (zeigeTon) {
                    SpurKnopf(Spurfeld.TON, offen == Offen.TON) {
                        offen = if (offen == Offen.TON) null else Offen.TON
                    }
                }
            }
        }

        // „Vorspann überspringen", solange die Stelle im Vorspann liegt.
        if (p != null && offen == null && naechstes == null &&
            Marken.imVorspann(intro, positionMs) && !introUebersprungen) {
            VorspannKnopf(
                onClick = { vorspannUeberspringen() },
                modifier = Modifier.align(Alignment.BottomEnd).padding(end = 16.dp, bottom = 150.dp))
        }

        naechstes?.let { n ->
            key(n.titel.id) {
                val dauerMs = (laenge * 1000).toLong()
                val restImFilm = if (beendet || dauerMs <= 0) 0
                                 else Math.round((dauerMs - positionMs) / 1000.0).toInt().coerceAtLeast(0)
                AlsNaechstes(
                    bild = model.client.frameUrl(n.titel.id),
                    beschriftung = n.beschriftung,
                    restImFilm = restImFilm,
                    // Im Abspann erschienen: mindestens zehn Sekunden; erst
                    // nach dem Ende gefunden: fünf, wie bisher.
                    mindest = if (abspannGemeldet) Marken.MINDEST_COUNTDOWN
                              else AmStueck.COUNTDOWN_SEKUNDEN,
                    onCancel = onAbbrechen,
                    onNow = onNaechstes,
                    modifier = Modifier.align(Alignment.BottomEnd).padding(20.dp))
            }
        }

        kurzHinweis?.let {
            Text(it, fontSize = 15.sp,
                 modifier = Modifier.align(Alignment.TopCenter).padding(top = 28.dp)
                     .background(Color(0xD912121A), androidx.compose.foundation.shape.RoundedCornerShape(14.dp))
                     .padding(horizontal = 16.dp, vertical = 10.dp))
        }

        when (val feld = offen) {
            null -> Unit
            Offen.FOLGEN -> FolgenfeldAnsicht(
                staffeln = staffeln ?: emptyList(),
                staffelId = folgenStaffel,
                folgen = folgenListe,
                aktuellId = item.id,
                bild = { model.client.wideUrl(it) },
                onStaffel = { staffelLaden(it.id) },
                onFolge = { wechseln(it) },
                onClose = { offen = null })
            else -> SpurfeldAnsicht(abschnitte(feld), onClose = { offen = null })
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
