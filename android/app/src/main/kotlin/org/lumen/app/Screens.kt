// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package org.lumen.app

import org.lumen.texte.Sprachwahl
import android.content.Intent
import android.net.Uri
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.PickVisualMediaRequest
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.ArrowBack
import androidx.compose.material.icons.filled.CheckCircle
import androidx.compose.material.icons.filled.Dns
import androidx.compose.material.icons.filled.Notifications
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material.icons.filled.Star
import androidx.compose.material.icons.filled.Tv
import androidx.compose.material.icons.outlined.StarBorder
import androidx.compose.material.icons.outlined.CheckCircle as CheckCircleOutlined
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import coil.compose.AsyncImage
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import org.lumen.core.*

// ---------------------------------------------------------------- Titelseite

/**
 * Die Titelseite — alles, was der Browser dort hat: Kopfbild, Titel und
 * Originaltitel, die Zeile mit Jahr, Freigabe, Länge, Genres und IMDb, die
 * Technik-Zeile, Abspielen (bei einer Serie die erste offene Folge),
 * „Von vorn", Merken, Zur Liste, Gesehen, die eigene Bewertung, Staffeln
 * mit ihren Folgen, Besetzung, die Filmreihe, ähnliche Titel, die
 * geteilten Bewertungen im Haushalt und die Dateien.
 */
@OptIn(ExperimentalMaterial3Api::class, ExperimentalLayoutApi::class)
@Composable
fun DetailScreen(
    model: LumenViewModel,
    itemId: String,
    onBack: () -> Unit,
    onPlay: (Abspielen) -> Unit,
    onOpen: (Item) -> Unit = {},
    // Ein Tipp auf jemanden aus der Besetzung: alles mit dieser Person.
    onPerson: (String) -> Unit = {},
    // Ein Tipp auf den Namen der Filmreihe: die ganze Reihe.
    onReihe: (String) -> Unit = {},
    // „Heruntergeladen" führt zur Liste der Downloads.
    onDownloads: () -> Unit = {},
) {
    var zurListe by remember(itemId) { mutableStateOf(false) }
    // Ob dieses Gerät die Datei direkt abspielt — für die Technik-Zeile.
    // Null, solange der Plan nicht da ist; dann fehlt nur dieses Abzeichen.
    var direkt by remember(itemId) { mutableStateOf<Boolean?>(null) }
    var detail by remember(itemId) { mutableStateOf<ItemDetail?>(null) }
    // Die Staffeln in der Reihenfolge zum Schauen (Specials ans Ende) und
    // die Folgen der gewählten — die erste Staffel gleich mit, damit die
    // Seite nicht mit einer leeren Liste aufgeht.
    var staffel by remember(itemId) { mutableStateOf<String?>(null) }
    var episodes by remember(itemId) { mutableStateOf<List<Item>?>(null) }
    var loading by remember(itemId) { mutableStateOf(true) }
    var sucht by remember(itemId) { mutableStateOf(false) }
    var hinweis by remember(itemId) { mutableStateOf<String?>(null) }
    // Ob „In welcher Qualität?" offen ist — nur bei mehreren Fassungen.
    var qualitaetOffen by remember(itemId) { mutableStateOf(false) }
    val error by model.error.collectAsStateWithLifecycle()
    var reload by remember { mutableStateOf(0) }
    val scope = rememberCoroutineScope()

    LaunchedEffect(itemId, reload) {
        detail = model.fetch { it.item(itemId) }
        loading = false
        val d = detail ?: return@LaunchedEffect
        if (staffel == null) staffel = AmStueck.staffelnOrdnen(d.seasons).firstOrNull()?.id
        val datei = d.playable
        if (datei != null && direkt == null) {
            direkt = withContext(Dispatchers.IO) {
                oderNull { model.client.istDirekt(datei, model.client.plan(datei.id)) }
            }
        }
    }
    LaunchedEffect(staffel, reload) {
        val st = staffel ?: return@LaunchedEffect
        episodes = AmStueck.folgenOrdnen(model.fetch { it.children(st) } ?: emptyList())
    }

    val d = detail
    if (d == null) {
        Column(Modifier.fillMaxSize()) {
            IconButton(onClick = onBack) { Icon(Icons.Filled.ArrowBack, tx(R.string.zurueck), tint = Color.White) }
            StateOverlay(loading, error, onRetry = { reload++ })
        }
        return
    }
    val item = d.item
    val staffeln = AmStueck.staffelnOrdnen(d.seasons)
    // Mehrere Fassungen desselben Films (MKV in 1080p, AVI in 400p):
    // „Ansehen" fragt dann zuerst nach der Qualität. Teile
    // eines Films (a/b/c) sind keine Fassungen; die laufen nacheinander.
    val fassungen = Fassungen.liste(d.files)

    /** Bei einer Serie: die erste Folge, die noch offen ist — wie im Browser. */
    fun serieAbspielen() {
        if (sucht) return
        sucht = true
        hinweis = null
        scope.launch {
            var beantwortet = false
            val n = model.fetch { c ->
                Abspielfolge(c).naechstesInListe(listOf(item), 0).also { beantwortet = true }
            }
            sucht = false
            if (n != null) onPlay(Abspielen(n.titel))
            else if (beantwortet) hinweis = tx(R.string.in_dieser_serie_liegt_noch)
        }
    }

    fun bewerten(sterne: Int) {
        model.send(onDone = { reload++ }) {
            it.rate(item.id, sterne, shared = Einstellungen.bewertungenTeilen(model.me?.prefs ?: emptyMap()))
        }
    }

    LazyColumn {
        item {
            Box {
                Artwork(
                    model.client.wideUrl(item),
                    item.title,
                    Modifier.fillMaxWidth().height(230.dp),
                    corner = 0.dp)
                Box(Modifier.matchParentSize().background(Brush.verticalGradient(
                    listOf(Color(0x66050507), Color.Transparent, Lumen.background))))
                IconButton(onClick = onBack, modifier = Modifier.statusBarsPadding().padding(6.dp)) {
                    Icon(Icons.Filled.ArrowBack, tx(R.string.zurueck), tint = Color.White)
                }
            }
        }

        item {
            Column(Modifier.padding(16.dp)) {
                Text(item.title, fontSize = 26.sp, fontWeight = FontWeight.Bold)
                item.originalTitle?.takeIf { it != item.title && it.isNotBlank() }?.let {
                    Text(it, color = Lumen.label2, fontSize = 14.sp)
                }
                Spacer(Modifier.height(6.dp))
                Text(Titelseite.metaZeile(item), color = Lumen.label2, fontSize = 14.sp)
                // Auflösung, HDR, Ton, Sprachen, Untertitel, direkt oder umgewandelt.
                val technik = Technik.abzeichen(item, d.playable, direkt)
                if (technik.isNotEmpty()) {
                    Spacer(Modifier.height(12.dp))
                    TechnikZeile(technik)
                }

                Spacer(Modifier.height(16.dp))
                FlowRow(horizontalArrangement = Arrangement.spacedBy(10.dp),
                        verticalArrangement = Arrangement.spacedBy(10.dp)) {
                    if (d.playable != null && fassungen.size > 1) {
                        // Mehrere Qualitäten: erst fragen, welche. „Von vorn"
                        // steht dann bei jeder Fassung in der Auswahl.
                        PrimaryButton("▶  " + Titelseite.abspielText(item) + " · " + txn(R.plurals.qualitaeten_anzahl, fassungen.size)) {
                            qualitaetOffen = !qualitaetOffen
                        }
                    } else if (d.playable != null) {
                        PrimaryButton("▶  " + Titelseite.abspielText(item)) { onPlay(Abspielen(item)) }
                        if (Titelseite.hatStand(item)) {
                            QuietButton(tx(R.string.von_vorn)) { onPlay(Abspielen(item, ab = 0.0)) }
                        }
                    }
                    if (item.kind == "series" && staffeln.isNotEmpty()) {
                        PrimaryButton("▶  " + Titelseite.serienText(item, episodes ?: emptyList()),
                                      enabled = !sucht) { serieAbspielen() }
                    }
                    QuietButton(if (item.onList) tx(R.string.aus_der_merkliste) else tx(R.string.merken)) {
                        model.send(onDone = { reload++ }) {
                            if (item.onList) it.removeFromWatchlist(item.id)
                            else it.addToWatchlist(item.id)
                        }
                    }
                    // Das Herz gibt es für Filme und Serien — so führt der
                    // Browser die Favoriten, getrennt nach beiden.
                    if (item.kind == "movie" || item.kind == "series") {
                        HerzKnopf(item.favorite) {
                            model.send(onDone = { reload++ }) {
                                if (item.favorite) it.removeFavorite(item.id)
                                else it.addFavorite(item.id)
                            }
                        }
                    }
                    // In eine Liste kommen nur Filme und Serien — so will es
                    // der Server, und so ist es auch gemeint.
                    if (item.kind == "movie" || item.kind == "series") {
                        QuietButton(tx(R.string.zur_liste)) { zurListe = true }
                    }
                    // Bei einer Serie heisst das: alle Folgen (der Server
                    // markiert sie); die Staffel geht weiter unten einzeln.
                    QuietButton(when {
                        item.finished && item.kind == "series" -> tx(R.string.serie_nicht_mehr_als_gesehen)
                        item.kind == "series" -> tx(R.string.ganze_serie_als_gesehen)
                        item.finished -> tx(R.string.nicht_mehr_als_gesehen)
                        else -> tx(R.string.als_gesehen_markieren)
                    }) {
                        model.send(onDone = { reload++ }) {
                            if (item.finished) it.clearFinished(item.id) else it.markFinished(item.id)
                        }
                    }
                    // Herunterladen für unterwegs — nur Filme und Folgen.
                    if (d.playable != null && (item.kind == "movie" || item.kind == "episode")) {
                        DownloadKnopf(model, item, onDownloads)
                    }
                    // Auf einen Chromecast schicken — nur, wenn einer da ist.
                    if (d.playable != null) CastKnopf(model)
                }
                if (qualitaetOffen && fassungen.size > 1) {
                    Spacer(Modifier.height(14.dp))
                    QualitaetsWahl(fassungen, Titelseite.hatStand(item),
                        onPlay = { f, vonVorn ->
                            qualitaetOffen = false
                            onPlay(Abspielen(item, ab = if (vonVorn) 0.0 else null, dateiId = f.id))
                        })
                }
                hinweis?.let {
                    Spacer(Modifier.height(8.dp))
                    Text(it, color = Lumen.label2, fontSize = 13.sp)
                }

                item.overview?.takeIf { it.isNotBlank() }?.let {
                    Spacer(Modifier.height(18.dp))
                    Text(it, color = Lumen.label, fontSize = 15.sp, lineHeight = 22.sp)
                }

                Spacer(Modifier.height(22.dp))
                Text(tx(R.string.deine_bewertung), fontWeight = FontWeight.SemiBold)
                Row(verticalAlignment = Alignment.CenterVertically) {
                    (1..5).forEach { n ->
                        IconButton(onClick = { bewerten(n) }) {
                            Icon(
                                if (item.myRating >= n) Icons.Filled.Star
                                else Icons.Outlined.StarBorder,
                                tx(R.string.x_von_5, n),
                                tint = if (item.myRating >= n) Lumen.star else Lumen.label3)
                        }
                    }
                    if (item.myRating > 0) {
                        Spacer(Modifier.width(6.dp))
                        TextButton(onClick = { bewerten(0) }) { Text(tx(R.string.loeschen), color = Lumen.label2) }
                    }
                }
                Text(tx(R.string.nur_fuer_dich_sichtbar), color = Lumen.label3, fontSize = 12.sp)
            }
        }

        // Staffeln als Reiter, darunter die Folgen der gewählten: Bild,
        // Nummer und Name, Länge, Datum, kurze Beschreibung — was gesehen
        // ist, trägt den Haken.
        if (staffeln.isNotEmpty()) {
            item {
                LazyRow(contentPadding = PaddingValues(horizontal = 16.dp),
                        horizontalArrangement = Arrangement.spacedBy(8.dp),
                        modifier = Modifier.padding(top = 6.dp, bottom = 8.dp)) {
                    items(staffeln, key = { it.id }) { st ->
                        FilterChip(
                            selected = staffel == st.id,
                            onClick = { if (staffel != st.id) { staffel = st.id; episodes = null } },
                            label = { Text(AmStueck.staffelName(st)) },
                            colors = FilterChipDefaults.filterChipColors(
                                labelColor = Lumen.label2, selectedContainerColor = Lumen.fill,
                                selectedLabelColor = Lumen.label))
                    }
                }
            }
            val folgen = episodes
            // Die gewählte Staffel auf einmal als gesehen markieren — oder,
            // wenn schon alles gesehen ist, wieder zurücknehmen.
            val gewaehlteStaffel = staffeln.firstOrNull { it.id == staffel }
            val staffelNr = gewaehlteStaffel?.seasonNo ?: folgen?.firstOrNull()?.seasonNo
            if (!folgen.isNullOrEmpty() && staffelNr != null) {
                val alleGesehen = folgen.all { it.finished }
                item {
                    TextButton(onClick = {
                        model.send(onDone = { reload++ }) {
                            if (alleGesehen) it.clearFinished(item.id, staffel = staffelNr)
                            else it.markFinished(item.id, staffel = staffelNr)
                        }
                    }, modifier = Modifier.padding(horizontal = 8.dp)) {
                        Text(if (alleGesehen) tx(R.string.staffel_nicht_mehr_als_gesehen) else tx(R.string.staffel_als_gesehen_markieren),
                             color = Lumen.label2)
                    }
                }
            }
            if (folgen == null) {
                item {
                    Box(Modifier.fillMaxWidth().height(80.dp), Alignment.Center) {
                        CircularProgressIndicator(color = Lumen.label2, strokeWidth = 2.dp,
                                                  modifier = Modifier.size(22.dp))
                    }
                }
            } else if (folgen.isEmpty()) {
                item {
                    Text(tx(R.string.in_dieser_staffel_liegt_noch), color = Lumen.label2,
                         modifier = Modifier.padding(16.dp))
                }
            } else {
                items(folgen, key = { "folge:" + it.id }) { episode ->
                    EpisodeRow(model, episode,
                        onGesehen = {
                            model.send(onDone = { reload++ }) {
                                if (episode.finished) it.clearFinished(episode.id) else it.markFinished(episode.id)
                            }
                        }) { onPlay(Abspielen(episode)) }
                }
            }
            item { Spacer(Modifier.height(18.dp)) }
        }

        // Die Besetzung: ein Tipp führt zu allem mit dieser Person.
        if (d.cast.isNotEmpty()) {
            item {
                Column(Modifier.padding(bottom = 22.dp)) {
                    Text(tx(R.string.besetzung), fontSize = 18.sp, fontWeight = FontWeight.SemiBold,
                         modifier = Modifier.padding(start = 16.dp, bottom = 10.dp))
                    LazyRow(
                        contentPadding = PaddingValues(horizontal = 16.dp),
                        horizontalArrangement = Arrangement.spacedBy(14.dp),
                    ) {
                        items(d.cast.distinctBy { it.name }, key = { it.name }) { p ->
                            BesetzungKarte(model, p) { onPerson(p.name) }
                        }
                    }
                }
            }
        }

        // Die Filmreihe: der Name führt zur ganzen Reihe; der Film, auf
        // dessen Seite man ist, steht hervorgehoben darin.
        d.collection?.let { reihe ->
            item {
                Column(Modifier.padding(bottom = 22.dp)) {
                    Row(Modifier.padding(start = 16.dp, bottom = 10.dp).clickableRow { onReihe(reihe.id) },
                        verticalAlignment = Alignment.CenterVertically) {
                        Text(reihe.name, fontSize = 18.sp, fontWeight = FontWeight.SemiBold)
                        Text("  ›  ${reihe.anzahlText}", fontSize = 14.sp, color = Lumen.label2)
                    }
                    LazyRow(
                        contentPadding = PaddingValues(horizontal = 16.dp),
                        horizontalArrangement = Arrangement.spacedBy(14.dp),
                    ) {
                        items(reihe.items, key = { it.id }) { film ->
                            // Der Film, auf dessen Seite man schon ist, öffnet
                            // sich nicht noch einmal über sich selbst.
                            PosterCard(model, film, { if (it.id != item.id) onOpen(it) },
                                       hervorgehoben = film.id == item.id)
                        }
                    }
                }
            }
        }

        // Ähnliche Titel: gleiche Reihe, geteilte Besetzung, gleiche Genres.
        if (d.similar.isNotEmpty()) {
            item {
                Reihenzeile(model, tx(R.string.aehnliche_titel), d.similar, { if (it.id != item.id) onOpen(it) })
            }
        }

        if (d.sharedRatings.isNotEmpty()) {
            item {
                Column(Modifier.padding(16.dp)) {
                    Text(tx(R.string.im_haushalt), fontWeight = FontWeight.SemiBold)
                    Spacer(Modifier.height(6.dp))
                    d.sharedRatings.forEach { r ->
                        Row(verticalAlignment = Alignment.CenterVertically,
                            modifier = Modifier.padding(vertical = 4.dp)) {
                            Avatar(r.name.take(1), r.avatar, 24.dp)
                            Spacer(Modifier.width(8.dp))
                            Text(r.name, Modifier.weight(1f))
                            Text("★".repeat(r.stars), color = Lumen.star)
                        }
                    }
                }
            }
        }

        // Die Dateien: Name, Auflösung, Codec, HDR, Grösse — wie im Browser.
        if (d.files.isNotEmpty()) {
            item {
                Column(Modifier.padding(horizontal = 16.dp, vertical = 8.dp)) {
                    Text(tx(R.string.dateien), fontWeight = FontWeight.SemiBold)
                    Spacer(Modifier.height(6.dp))
                    d.files.forEach { f ->
                        Column(Modifier.padding(vertical = 4.dp)) {
                            Text(f.fileName, fontSize = 13.sp, color = Lumen.label2, maxLines = 1,
                                 overflow = TextOverflow.Ellipsis,
                                 fontFamily = androidx.compose.ui.text.font.FontFamily.Monospace)
                            Text(Titelseite.dateiZeile(f), fontSize = 12.sp,
                                 color = if (f.missing) Lumen.bad else Lumen.label3)
                        }
                    }
                }
            }
        }

        item { Spacer(Modifier.height(32.dp)) }
    }

    if (zurListe) {
        ZurListeDialog(model, item.id) { zurListe = false }
    }
}

/** Jemand aus der Besetzung: rund, Bild oder Anfangsbuchstabe, Name, Rolle. */
@Composable
private fun BesetzungKarte(model: LumenViewModel, p: Besetzung, onClick: () -> Unit) {
    Column(
        Modifier.width(84.dp).clickableRow(onClick),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Box(
            Modifier.size(72.dp).clip(CircleShape)
                .background(Brush.linearGradient(listOf(Color(0xFF2A2A31), Color(0xFF16161B)))),
            contentAlignment = Alignment.Center,
        ) {
            Text(p.initial, color = Lumen.label2, fontSize = 26.sp, fontWeight = FontWeight.Medium)
            model.client.bildUrl(p.bild)?.let { bild ->
                AsyncImage(model = bild, contentDescription = null,
                           contentScale = ContentScale.Crop, modifier = Modifier.fillMaxSize())
            }
        }
        Spacer(Modifier.height(6.dp))
        Text(p.name, fontSize = 13.sp, maxLines = 1, overflow = TextOverflow.Ellipsis,
             textAlign = TextAlign.Center)
        p.rolle?.takeIf { it.isNotBlank() }?.let {
            Text(it, fontSize = 12.sp, color = Lumen.label2, maxLines = 1,
                 overflow = TextOverflow.Ellipsis, textAlign = TextAlign.Center)
        }
    }
}

/** Eine Folge: Bild mit Balken, Nummer und Name, Haken, Länge und Datum, Beschreibung. */
@Composable
private fun EpisodeRow(model: LumenViewModel, episode: Item, onGesehen: () -> Unit, onPlay: () -> Unit) {
    Row(
        Modifier.fillMaxWidth().clickableRow(onPlay).padding(start = 16.dp, end = 4.dp, top = 8.dp, bottom = 8.dp),
        verticalAlignment = Alignment.Top,
    ) {
        Box {
            Artwork(
                model.client.wideUrl(episode),
                episode.title, Modifier.width(128.dp).height(72.dp), corner = 8.dp)
            if (episode.progress > 1 && !episode.finished) {
                ProgressStripe(episode.progress, Modifier.align(Alignment.BottomCenter))
            }
            Icon(Icons.Filled.PlayArrow, null, tint = Color.White,
                 modifier = Modifier.align(Alignment.Center).size(26.dp))
        }
        Spacer(Modifier.width(12.dp))
        Column(Modifier.weight(1f)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(episode.folgenNummer, fontSize = 14.sp, color = Lumen.label3,
                     modifier = Modifier.padding(end = 8.dp))
                Text(episode.title, fontSize = 14.sp, maxLines = 1, overflow = TextOverflow.Ellipsis,
                     modifier = Modifier.weight(1f, fill = false))
                if (episode.finished) Text("  ✓", fontSize = 13.sp, color = Lumen.label2)
            }
            val zeile = Titelseite.folgenZeile(episode)
            if (zeile.isNotEmpty()) Text(zeile, fontSize = 12.sp, color = Lumen.label3)
            episode.overview?.takeIf { it.isNotBlank() }?.let {
                Text(it, fontSize = 13.sp, color = Lumen.label2, maxLines = 2,
                     overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(top = 2.dp))
            }
        }
        // Eine Folge einzeln als gesehen markieren oder zurücknehmen.
        IconButton(onClick = onGesehen) {
            Icon(if (episode.finished) Icons.Filled.CheckCircle else Icons.Outlined.CheckCircleOutlined,
                 if (episode.finished) tx(R.string.nicht_mehr_als_gesehen) else tx(R.string.als_gesehen_markieren),
                 tint = if (episode.finished) Lumen.label else Lumen.label3)
        }
    }
}

/**
 * „In welcher Qualität?" — jede Fassung als Zeile: „1080p · MKV · 7,9 GB".
 * Die erste ist der Standard (der Server ordnet: bevorzugte, dann höchste
 * Auflösung). Mit Stand gibt es je Fassung auch „Von vorn" — wie im Browser.
 */
@Composable
private fun QualitaetsWahl(
    fassungen: List<MediaFile>,
    mitVonVorn: Boolean,
    onPlay: (MediaFile, Boolean) -> Unit,
) {
    Column(Modifier.fillMaxWidth().clip(androidx.compose.foundation.shape.RoundedCornerShape(14.dp))
               .background(Lumen.fill)) {
        Text(tx(R.string.in_welcher_qualitaet), color = Lumen.label2, fontSize = 13.sp,
             modifier = Modifier.padding(start = 16.dp, top = 12.dp, bottom = 4.dp))
        fassungen.forEachIndexed { i, f ->
            Row(Modifier.fillMaxWidth().clickableRow { onPlay(f, false) }
                    .padding(horizontal = 16.dp, vertical = 10.dp),
                verticalAlignment = Alignment.CenterVertically) {
                Icon(Icons.Filled.PlayArrow, null, tint = Lumen.label, modifier = Modifier.size(20.dp))
                Spacer(Modifier.width(12.dp))
                Text(Fassungen.name(f).ifEmpty { f.fileName }, fontSize = 15.sp, modifier = Modifier.weight(1f))
                if (i == 0) Text(tx(R.string.standard), color = Lumen.label3, fontSize = 12.sp,
                                 modifier = Modifier.padding(end = 8.dp))
                if (mitVonVorn) TextButton(onClick = { onPlay(f, true) }) {
                    Text(tx(R.string.von_vorn), color = Lumen.label2, fontSize = 13.sp)
                }
            }
        }
    }
}

// ---------------------------------------------------------------- Profil

@Composable
fun ProfileScreen(
    model: LumenViewModel,
    onNeuigkeiten: () -> Unit = {},
    onReihen: () -> Unit = {},
    onListen: () -> Unit = {},
    onFavoriten: () -> Unit = {},
    onWuensche: () -> Unit = {},
    onFernseher: () -> Unit = {},
    onSperre: () -> Unit = {},
    onDownloads: () -> Unit = {},
) {
    // Über den Zustand gelesen, nicht über model.me: nach einer Änderung
    // (Profilbild, Untertitel, Schalter) soll die Seite sie gleich zeigen.
    val stage by model.stage.collectAsStateWithLifecycle()
    val me = (stage as? Stage.SignedIn)?.me ?: return
    val stand by model.avatarStand.collectAsStateWithLifecycle()
    var confirmClear by remember { mutableStateOf(false) }
    var ueberallAbmelden by remember { mutableStateOf(false) }
    var sprachwahl by remember { mutableStateOf<String?>(null) }
    // Die eigenen Geräte — nur die eigenen; was andere angemeldet haben,
    // gibt der Server nicht heraus.
    var geraete by remember { mutableStateOf<List<Device>?>(null) }
    LaunchedEffect(Unit) { geraete = model.fetch { it.devices() } ?: emptyList() }
    // Was gerade im eigenen Profil läuft. Der Zähler lädt nach dem Beenden neu.
    var wiedergaben by remember { mutableStateOf<List<EigeneWiedergabe>>(emptyList()) }
    var wiedergabenStand by remember { mutableStateOf(0) }
    LaunchedEffect(wiedergabenStand) {
        wiedergaben = model.fetch { it.meineWiedergaben() } ?: emptyList()
    }
    // Wie viel Bildschirmzeit heute bleibt — nur Kinderprofile haben eine
    // Regel, die anderen fragen gar nicht erst. Beiwerk: ohne Antwort
    // bleibt der Abschnitt weg.
    var bildschirmzeit by remember { mutableStateOf<Bildschirmzeit?>(null) }
    LaunchedEffect(me.account.id, me.account.isKid) {
        bildschirmzeit = if (!me.account.isKid) null
            else withContext(Dispatchers.IO) { oderNull { model.client.bildschirmzeit() } }
    }

    fun pref(key: String, wert: String) {
        model.send(onDone = { model.refreshMe() }) { it.updatePrefs(mapOf(key to wert)) }
    }

    LazyColumn(contentPadding = PaddingValues(16.dp)) {
        item {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Avatar(me.account.initial, me.account.avatar, 52.dp,
                       model.client.profilbildUrl(me.account.id, me.account.avatar, stand))
                Spacer(Modifier.width(12.dp))
                Column(Modifier.weight(1f)) {
                    Text(me.account.name, fontSize = 20.sp, fontWeight = FontWeight.SemiBold)
                    // Wer über eine Gruppe Vollzugriff hat, liest das hier —
                    // verwaltet wird trotzdem nur im Browser.
                    Text(Einstellungen.rolle(me.account), color = Lumen.label2, fontSize = 13.sp)
                }
                QuietButton(tx(R.string.abmelden)) { model.signOut() }
            }
            Spacer(Modifier.height(24.dp))
        }

        bildschirmzeit?.takeIf { it.aktiv }?.let { bz ->
            item {
                SettingGroup(tx(R.string.bildschirmzeit))
                val zeile = bz.nebenzeile
                ListItem(
                    headlineContent = { Text(bz.hauptzeile) },
                    supportingContent = if (zeile != null) {
                        { Text(zeile, color = Lumen.label3) }
                    } else null,
                    colors = ListItemDefaults.colors(containerColor = Lumen.background))
                Spacer(Modifier.height(18.dp))
            }
        }

        // Mehr: was nicht in der unteren Leiste steht — wie im Browser auf
        // dem Telefon unter „Ich".
        item {
            SettingGroup(tx(R.string.mehr))
            LinkZeile(tx(R.string.filmreihen), tx(R.string.von_lumen_zusammengestellt), onReihen)
            LinkZeile(tx(R.string.favoriten), tx(R.string.was_du_mit_dem_herz), onFavoriten)
            LinkZeile(tx(R.string.listen), tx(R.string.eigene_und_geteilte), onListen)
            LinkZeile(tx(R.string.wuensche), tx(R.string.ansehen_und_wuenschen), onWuensche)
            LinkZeile(tx(R.string.downloads), tx(R.string.zum_schauen_ohne_netz), onDownloads)
            Spacer(Modifier.height(18.dp))
        }

        // Den eigenen Schutz ändern — nur, solange der Server das kann.
        if (!model.sperreUnbekannt) {
            item {
                SettingGroup(tx(R.string.profil_schuetzen))
                LinkZeile(
                    when (me.account.lockKind) {
                        "pin" -> tx(R.string.pin_aendern)
                        "password" -> tx(R.string.passwort_aendern)
                        else -> tx(R.string.pin_oder_passwort_festlegen)
                    },
                    Profilschutz.name(me.account.lockKind), onSperre)
                Spacer(Modifier.height(18.dp))
            }
        }

        // Neuigkeiten: neue Folgen deiner Serien, neue Filme deiner Reihen.
        item {
            ListItem(
                headlineContent = { Text(tx(R.string.neuigkeiten)) },
                supportingContent = {
                    Text(if (me.neuigkeiten > 0) tx(R.string.x_ungelesen, me.neuigkeiten) else tx(R.string.alles_gelesen),
                         color = Lumen.label3)
                },
                leadingContent = { Icon(Icons.Filled.Notifications, null, tint = Lumen.label2) },
                trailingContent = {
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        if (me.neuigkeiten > 0) {
                            Badge(containerColor = Color(0xFFFF453A), contentColor = Color.White) {
                                Text(if (me.neuigkeiten > 9) "9+" else me.neuigkeiten.toString())
                            }
                            Spacer(Modifier.width(8.dp))
                        }
                        Text("›", color = Lumen.label3, fontSize = 20.sp)
                    }
                },
                colors = ListItemDefaults.colors(containerColor = Lumen.background),
                modifier = Modifier.clickableRow(onNeuigkeiten))
            Spacer(Modifier.height(18.dp))
        }

        item {
            SettingGroup(tx(R.string.profilbild))
            ProfilbildWahl(model, me)
            Text(tx(R.string.eine_farbe_eine_figur_oder),
                 color = Lumen.label3, fontSize = 12.sp,
                 modifier = Modifier.padding(top = 6.dp, bottom = 22.dp))
        }

        item {
            SettingGroup(tx(R.string.untertitel))
            val stil = UntertitelStil.aus(me.prefs)
            ListItem(
                headlineContent = { Text(tx(R.string.groesse)) },
                trailingContent = {
                    Row(Modifier.clip(CircleShape).background(Lumen.fill).padding(3.dp),
                        horizontalArrangement = Arrangement.spacedBy(2.dp)) {
                        UntertitelStil.GROESSEN.forEach { (wert, name) ->
                            val an = stil.groesse == wert
                            Text(name, fontSize = 14.sp,
                                 color = if (an) Color.Black else Lumen.label,
                                 fontWeight = if (an) FontWeight.SemiBold else FontWeight.Normal,
                                 modifier = Modifier.clip(CircleShape)
                                     .background(if (an) Color.White else Color.Transparent)
                                     .clickableRow {
                                         model.send(onDone = { model.refreshMe() }) {
                                             it.updatePrefs(mapOf(UntertitelStil.PREF_GROESSE to wert))
                                         }
                                     }
                                     .padding(horizontal = 12.dp, vertical = 6.dp))
                        }
                    }
                },
                colors = ListItemDefaults.colors(containerColor = Lumen.background))
            SettingSwitch(tx(R.string.dunkler_hintergrund_hinter_dem_text), stil.hintergrund) { on ->
                model.send(onDone = { model.refreshMe() }) {
                    it.updatePrefs(mapOf(UntertitelStil.PREF_HINTERGRUND to if (on) "1" else "0"))
                }
            }
            Text(tx(R.string.gilt_im_browser_und_in), color = Lumen.label3, fontSize = 12.sp,
                 modifier = Modifier.padding(top = 6.dp, bottom = 22.dp))
        }

        // Keine Verwaltung auf dem Telefon: Sie gibt es nur im Browser und in
        // der Mac-App (Vorgabe des Besitzers). Alles für die eigene Person
        // — Geräte, eigene Wiedergaben, Einstellungen — bleibt hier.

        item {
            // Die Sprache gehört zum Profil: gilt im Browser und in allen
            // Apps. Leer heisst: wie der Server.
            val ctx = LocalContext.current
            SettingGroup(Sprachwahl.titel(ctx))
            LinkZeile(Sprachwahl.titel(ctx),
                      Sprachwahl.wahlText(ctx, me.prefs, model.serverSprachen, model.serverSprache)) {
                sprachwahl = Sprachen.PREF
            }
            Text(Sprachwahl.hinweis(ctx), color = Lumen.label3, fontSize = 12.sp,
                 modifier = Modifier.padding(top = 6.dp, bottom = 22.dp))
        }

        item {
            SettingGroup(tx(R.string.wiedergabe))
            SettingSwitch(tx(R.string.folgen_automatisch_weiter), Einstellungen.autoplay(me.prefs)) { on ->
                pref(Einstellungen.AUTOPLAY, Einstellungen.schalter(on))
            }
            SettingSwitch(tx(R.string.vorspann_ueberspringen), Einstellungen.vorspann(me.prefs)) { on ->
                pref(Einstellungen.VORSPANN, Einstellungen.schalter(on))
            }
            // Die Sprachen, die der Player vorbelegt. Lumen merkt sie sich
            // auch, wenn man im Player eine Spur wählt.
            LinkZeile(tx(R.string.tonsprache), Einstellungen.tonText(me.prefs)) { sprachwahl = Spuren.PREF_TON }
            LinkZeile(tx(R.string.untertitel), Einstellungen.untertitelText(me.prefs)) { sprachwahl = Spuren.PREF_UNTERTITEL }
            SettingRow(tx(R.string.qualitaet_unterwegs), me.limits.remoteQuality)
            Spacer(Modifier.height(22.dp))
        }

        item {
            SettingGroup(tx(R.string.privatsphaere))
            SettingSwitch(tx(R.string.bewertungen_im_haushalt_zeigen), Einstellungen.bewertungenTeilen(me.prefs)) { on ->
                pref(Einstellungen.BEWERTUNGEN_TEILEN, Einstellungen.schalter(on))
            }
            Text(
                tx(R.string.dein_verlauf_und_deine_bewertungen),
                color = Lumen.label3, fontSize = 12.sp,
                modifier = Modifier.padding(top = 6.dp, bottom = 22.dp))
        }

        item {
            SettingGroup(tx(R.string.verlauf))
            ListItem(
                headlineContent = { Text(tx(R.string.verlauf_loeschen_2), color = Lumen.bad) },
                colors = ListItemDefaults.colors(containerColor = Lumen.background),
                modifier = Modifier.clickableRow { confirmClear = true })
            Text(tx(R.string.entfernt_alle_fortschritte_und_haken),
                 color = Lumen.label3, fontSize = 12.sp,
                 modifier = Modifier.padding(top = 6.dp, bottom = 22.dp))
        }

        item {
            SettingGroup(tx(R.string.geraete))
            // Am Fernseher steht ein Code — hier eingeben, und er ist mit
            // diesem Profil angemeldet. Kein Tippen mit der Fernbedienung.
            ListItem(
                headlineContent = { Text(tx(R.string.fernseher_anmelden)) },
                supportingContent = { Text(tx(R.string.mit_dem_code_der_dort), color = Lumen.label3) },
                leadingContent = { Icon(Icons.Filled.Tv, null, tint = Lumen.label2) },
                trailingContent = { Text("›", color = Lumen.label3, fontSize = 20.sp) },
                colors = ListItemDefaults.colors(containerColor = Lumen.background),
                modifier = Modifier.clickableRow(onFernseher))
            val liste = geraete
            if (liste == null) {
                SettingRow(tx(R.string.wird_geladen), "")
            } else {
                liste.forEach { g -> SettingRow(g.name.ifBlank { tx(R.string.ohne_namen) }, g.artText) }
            }
            // Ein vergessener Fernseher bei den Eltern, ein Tablet im Hotel:
            // hier lässt es sich selbst beenden, ohne den Besitzer zu fragen.
            // Nur, wenn gerade etwas läuft.
            if (wiedergaben.isNotEmpty()) {
                Text(tx(R.string.laeuft_gerade_in_deinem_profil), color = Lumen.label2, fontSize = 13.sp,
                     modifier = Modifier.padding(start = 4.dp, top = 12.dp, bottom = 2.dp))
                wiedergaben.forEach { w ->
                    ListItem(
                        headlineContent = { Text(w.geraet) },
                        supportingContent = { Text(w.zeile, color = Lumen.label3) },
                        trailingContent = {
                            TextButton(onClick = {
                                model.send(onDone = { wiedergabenStand++ }) { it.wiedergabeBeenden(w.id) }
                            }) { Text(tx(R.string.beenden), color = Lumen.bad) }
                        },
                        colors = ListItemDefaults.colors(containerColor = Lumen.background))
                }
            }
            ListItem(
                headlineContent = { Text(tx(R.string.ueberall_abmelden_2), color = Lumen.bad) },
                colors = ListItemDefaults.colors(containerColor = Lumen.background),
                modifier = Modifier.clickableRow { ueberallAbmelden = true })
            ListItem(
                headlineContent = { Text(tx(R.string.profil_wechseln)) },
                colors = ListItemDefaults.colors(containerColor = Lumen.background),
                modifier = Modifier.clickableRow { model.signOut() })
            Spacer(Modifier.height(26.dp))
        }

        // Mehrere Server: wechseln, ohne sich abzumelden — wie bei Emby.
        item {
            SettingGroup(tx(R.string.server))
            val aktiv = model.aktiverServer
            ListItem(
                headlineContent = { Text(tx(R.string.server_wechseln)) },
                supportingContent = {
                    Text(aktiv?.let { "${it.name} · ${it.adresseKurz}" } ?: "", color = Lumen.label3,
                         maxLines = 1, overflow = TextOverflow.Ellipsis)
                },
                leadingContent = { Icon(Icons.Filled.Dns, null, tint = Lumen.label2) },
                trailingContent = { Text("›", color = Lumen.label3, fontSize = 20.sp) },
                colors = ListItemDefaults.colors(containerColor = Lumen.background),
                modifier = Modifier.clickableRow { model.serverWahl() })
            Spacer(Modifier.height(26.dp))
        }

        item { SettingGroup(tx(R.string.ueber_lumen)) }

        item {
            val ctx = LocalContext.current
            SettingRow(tx(R.string.fassung), model.serverVersion)
            if (model.license.isNotEmpty()) SettingRow(tx(R.string.lizenz), model.license)
            model.sourceUrl?.let { url ->
                ListItem(
                    headlineContent = { Text(tx(R.string.quelltext)) },
                    trailingContent = { Text("ansehen", color = Lumen.accent, fontSize = 15.sp) },
                    colors = ListItemDefaults.colors(containerColor = Lumen.background),
                    modifier = Modifier.clickableRow {
                        // Schlägt fehl, wenn gar kein Browser da ist — dann
                        // lieber nichts tun als die App abstürzen lassen.
                        runCatching {
                            ctx.startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(url)))
                        }
                    })
            }
            Text(
                tx(R.string.lumen_ist_freie_software_du) +
                    if (model.hasFfmpeg) ""
                    else tx(R.string.ffmpeg_fehlt_auf_dem_server),
                color = Lumen.label3, fontSize = 12.sp,
                modifier = Modifier.padding(top = 8.dp, bottom = 28.dp))
        }
    }

    if (ueberallAbmelden) {
        AlertDialog(
            onDismissRequest = { ueberallAbmelden = false },
            title = { Text(tx(R.string.ueberall_abmelden)) },
            text = { Text(tx(R.string.dein_profil_wird_auf_allen)) },
            confirmButton = {
                TextButton(onClick = {
                    ueberallAbmelden = false
                    model.send(onDone = { model.signOut() }) { it.signOutEverywhere() }
                }) { Text(tx(R.string.abmelden), color = Lumen.bad) }
            },
            dismissButton = {
                TextButton(onClick = { ueberallAbmelden = false }) { Text(tx(R.string.abbrechen)) }
            },
            containerColor = Color(0xFF16161A))
    }

    sprachwahl?.let { key ->
        val ctx = LocalContext.current
        val wahl = when (key) {
            Sprachen.PREF -> Sprachwahl.wahl(ctx, model.serverSprachen, model.serverSprache)
            Spuren.PREF_TON -> Einstellungen.TON_WAHL
            else -> Einstellungen.UNTERTITEL_WAHL
        }
        val aktuell = me.prefs[key]?.trim().orEmpty().ifEmpty { if (key == Spuren.PREF_UNTERTITEL) Spuren.AUS else "" }
        AlertDialog(
            onDismissRequest = { sprachwahl = null },
            title = {
                Text(when (key) {
                    Sprachen.PREF -> Sprachwahl.titel(ctx)
                    Spuren.PREF_TON -> tx(R.string.tonsprache)
                    else -> tx(R.string.untertitel)
                })
            },
            text = {
                Column(Modifier.heightIn(max = 380.dp).verticalScrollable()) {
                    wahl.forEach { (wert, name) ->
                        ListItem(
                            headlineContent = { Text(name) },
                            trailingContent = { if (wert == aktuell) Text("✓", color = Lumen.good) },
                            colors = ListItemDefaults.colors(containerColor = Color.Transparent),
                            modifier = Modifier.clickableRow { sprachwahl = null; pref(key, wert) })
                    }
                }
            },
            confirmButton = { TextButton(onClick = { sprachwahl = null }) { Text(tx(R.string.fertig)) } },
            containerColor = Color(0xFF16161A))
    }

    if (confirmClear) {
        AlertDialog(
            onDismissRequest = { confirmClear = false },
            title = { Text(tx(R.string.verlauf_loeschen)) },
            text = { Text(tx(R.string.alle_fortschritte_und_haken_werden)) },
            confirmButton = {
                TextButton(onClick = {
                    confirmClear = false
                    model.send { it.clearHistory() }
                }) { Text(tx(R.string.loeschen), color = Lumen.bad) }
            },
            dismissButton = {
                TextButton(onClick = { confirmClear = false }) { Text(tx(R.string.abbrechen)) }
            },
            containerColor = Color(0xFF16161A))
    }
}

/**
 * Die Auswahl fürs Profilbild: fünf Farben, die eingebauten Figuren und
 * ein eigenes Bild über den Foto-Auswähler des Systems — der braucht keine
 * Berechtigung für die ganze Galerie, nur für das eine gewählte Bild.
 */
@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun ProfilbildWahl(model: LumenViewModel, me: Me) {
    val ctx = LocalContext.current
    val scope = rememberCoroutineScope()
    val stand by model.avatarStand.collectAsStateWithLifecycle()
    var laedt by remember { mutableStateOf(false) }
    var hinweis by remember { mutableStateOf<String?>(null) }
    val a = me.account

    fun setzen(avatar: String) {
        if (a.avatar == avatar) return
        model.send(onDone = { model.refreshMe() }) { it.setAvatar(avatar) }
    }

    val waehler = rememberLauncherForActivityResult(ActivityResultContracts.PickVisualMedia()) { uri ->
        if (uri == null) return@rememberLauncherForActivityResult
        laedt = true
        hinweis = tx(R.string.bild_wird_hochgeladen)
        scope.launch {
            val typ = ctx.contentResolver.getType(uri) ?: "application/octet-stream"
            val bytes = withContext(Dispatchers.IO) {
                runCatching { ctx.contentResolver.openInputStream(uri)?.use { it.readBytes() } }.getOrNull()
            }
            if (bytes == null) {
                hinweis = tx(R.string.das_bild_liess_sich_nicht)
            } else if (bytes.size > 16 * 1024 * 1024) {
                hinweis = tx(R.string.das_bild_ist_zu_gross)
            } else {
                val neu = model.fetch { it.uploadAvatar(bytes, typ) }
                if (neu != null) {
                    model.avatarGeaendert()
                    model.refreshMe()
                    hinweis = tx(R.string.profilbild_gesetzt)
                } else {
                    hinweis = model.error.value ?: tx(R.string.das_hat_nicht_geklappt)
                }
            }
            laedt = false
        }
    }

    FlowRow(
        Modifier.fillMaxWidth().padding(horizontal = 4.dp, vertical = 8.dp),
        horizontalArrangement = Arrangement.spacedBy(10.dp),
        verticalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        Profilbild.FARBEN.forEach { farbe ->
            ProfilbildKnopf(a.avatar == farbe, tx(R.string.farbe_x, farbe.removePrefix("av-")), { setzen(farbe) }) {
                Avatar(a.initial, farbe, 40.dp)
            }
        }
        Figuren.angeboten(me.figuren).forEach { f ->
            ProfilbildKnopf(a.avatar == "fig-${f.name}", f.name, { setzen("fig-${f.name}") }) {
                Avatar(a.initial, "fig-${f.name}", 40.dp)
            }
        }
        ProfilbildKnopf(Profilbild.istBild(a.avatar), tx(R.string.eigenes_bild_hochladen), {
            if (!laedt) waehler.launch(PickVisualMediaRequest(ActivityResultContracts.PickVisualMedia.ImageOnly))
        }) {
            Box(Modifier.size(40.dp).clip(CircleShape).background(Lumen.fill), Alignment.Center) {
                if (laedt) {
                    CircularProgressIndicator(color = Lumen.label2, strokeWidth = 2.dp,
                                              modifier = Modifier.size(18.dp))
                } else if (Profilbild.istBild(a.avatar)) {
                    AsyncImage(model = model.client.profilbildUrl(a.id, a.avatar, stand),
                               contentDescription = null, contentScale = ContentScale.Crop,
                               modifier = Modifier.fillMaxSize())
                } else {
                    Text("+", color = Lumen.label2, fontSize = 22.sp)
                }
            }
        }
    }
    hinweis?.let { Text(it, color = Lumen.label2, fontSize = 13.sp, modifier = Modifier.padding(start = 4.dp)) }
}

/** Ein Knopf in der Auswahl: rund, der gewählte mit blauem Ring. */
@Composable
private fun ProfilbildKnopf(
    gewaehlt: Boolean,
    beschreibung: String,
    onClick: () -> Unit,
    inhalt: @Composable () -> Unit,
) {
    Box(
        Modifier
            .size(48.dp)
            .clip(CircleShape)
            .border(if (gewaehlt) 2.5.dp else 0.dp,
                    if (gewaehlt) Lumen.accent else Color.Transparent, CircleShape)
            .padding(if (gewaehlt) 4.dp else 2.dp)
            .clickableRow(onClick)
            .semantics { contentDescription = beschreibung },
        contentAlignment = Alignment.Center,
    ) { inhalt() }
}

@Composable
fun SettingSwitch(label: String, checked: Boolean, onChange: (Boolean) -> Unit) {
    ListItem(
        headlineContent = { Text(label) },
        trailingContent = {
            Switch(checked = checked, onCheckedChange = onChange,
                   colors = SwitchDefaults.colors(checkedTrackColor = Lumen.good))
        },
        colors = ListItemDefaults.colors(containerColor = Lumen.background))
}

/** Eine Zeile, die woandershin führt: Beschriftung, Wert, Pfeil. */
@Composable
private fun LinkZeile(label: String, wert: String, onClick: () -> Unit) {
    ListItem(
        headlineContent = { Text(label) },
        trailingContent = {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(wert, color = Lumen.label2, fontSize = 15.sp)
                Text("  ›", color = Lumen.label3, fontSize = 20.sp)
            }
        },
        colors = ListItemDefaults.colors(containerColor = Lumen.background),
        modifier = Modifier.clickableRow(onClick))
}

/** Überschrift über einer Gruppe von Einstellungen. */
@Composable
private fun SettingGroup(title: String) {
    Text(title.uppercase(), color = Lumen.label3, fontSize = 12.sp,
         fontWeight = FontWeight.SemiBold,
         modifier = Modifier.padding(start = 4.dp, bottom = 4.dp))
}

/** Eine Zeile, die nur etwas anzeigt — Beschriftung links, Wert rechts. */
@Composable
private fun SettingRow(label: String, value: String) {
    ListItem(
        headlineContent = { Text(label) },
        trailingContent = { Text(value, color = Lumen.label2, fontSize = 15.sp) },
        colors = ListItemDefaults.colors(containerColor = Lumen.background))
}
