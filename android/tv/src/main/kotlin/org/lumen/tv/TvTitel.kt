// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package org.lumen.tv

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.combinedClickable
import androidx.compose.foundation.focusable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.scale
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.focus.onFocusChanged
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.ui.window.Dialog
import androidx.compose.ui.window.DialogProperties
import androidx.compose.animation.core.animateFloatAsState
import coil.compose.AsyncImage
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import org.lumen.core.*

/**
 * Die Titelseite und die Listen auf dem Fernseher.
 *
 * Ein Druck auf einen Titel öffnet seine Seite: dort steht „Abspielen"
 * vorn unter dem Fokus — ein zweiter Druck, und es läuft. Darunter, wie im
 * Browser: die Technik-Zeile, „Von vorn", Merken, Zur Liste, Gesehen,
 * Bewerten; bei einer Serie die Staffeln mit ihren Folgen; die Besetzung
 * (ein Druck führt zu allem mit der Person), die Filmreihe, ähnliche
 * Titel, die geteilten Bewertungen im Haushalt und die Dateien.
 *
 * Wer aus einer Liste kommt, kann den Titel dort herausnehmen oder nach
 * vorn und hinten schieben.
 *
 * Und wie überall: Listen zeigen, was jemand bewusst hineingelegt hat.
 * Nicht, was jemand schaut.
 */

/** Ein Titel, der offen ist — und falls aus einer Liste, welche und wo. */
internal data class TvTitelZiel(
    val item: Item,
    val liste: Liste? = null,
    val index: Int = -1,
)

@Composable
internal fun TvTitel(
    client: ApiClient,
    ziel: TvTitelZiel,
    prefs: Map<String, String>,
    onPlay: (TvAbspielen) -> Unit,
    onOpen: (Item) -> Unit,
    onPerson: (String) -> Unit,
    onReihe: (String) -> Unit,
    onListeGeaendert: () -> Unit,
    onClose: () -> Unit,
) {
    var detail by remember { mutableStateOf<ItemDetail?>(null) }
    var stand by remember { mutableStateOf(0) }
    // Ob dieses Gerät die Datei direkt abspielt — für die Technik-Zeile.
    var direkt by remember { mutableStateOf<Boolean?>(null) }
    var staffel by remember { mutableStateOf<String?>(null) }
    var folgen by remember { mutableStateOf<List<Item>?>(null) }
    // Die Liste, aus der man kommt, und der Platz darin — beides ändert
    // sich, wenn der Titel verschoben wird.
    var liste by remember { mutableStateOf(ziel.liste) }
    var platz by remember { mutableStateOf(ziel.index) }
    var hinweis by remember { mutableStateOf<String?>(null) }
    var sucht by remember { mutableStateOf(false) }
    var zurListe by remember { mutableStateOf(false) }
    var entfernen by remember { mutableStateOf(false) }
    var bewerten by remember { mutableStateOf(false) }
    // „In welcher Qualität?": ob offen, und ab wo es dann läuft — null
    // beim gespeicherten Stand, 0.0 für „Von vorn".
    var qualitaetOffen by remember { mutableStateOf(false) }
    var qualitaetAb by remember { mutableStateOf<Double?>(null) }
    val scope = rememberCoroutineScope()
    val anfang = remember { FocusRequester() }

    LaunchedEffect(stand) {
        try {
            val d = withContext(Dispatchers.IO) { client.item(ziel.item.id) }
            detail = d
            if (staffel == null) staffel = AmStueck.staffelnOrdnen(d.seasons).firstOrNull()?.id
            val datei = d.playable
            if (datei != null && direkt == null) {
                direkt = withContext(Dispatchers.IO) {
                    oderNull { client.istDirekt(datei, client.plan(datei.id)) }
                }
            }
        } catch (e: LumenException) {
            hinweis = e.message
        }
    }
    LaunchedEffect(staffel, stand) {
        val st = staffel ?: return@LaunchedEffect
        folgen = try {
            AmStueck.folgenOrdnen(withContext(Dispatchers.IO) { client.children(st) })
        } catch (e: LumenException) {
            emptyList()
        }
    }

    // Der Fokus steht gleich auf „Abspielen" — das will man hier meistens,
    // und so kostet die Titelseite genau einen Druck mehr als vorher.
    LaunchedEffect(Unit) {
        kotlinx.coroutines.delay(100)
        runCatching { anfang.requestFocus() }
    }

    val d = detail
    val item = d?.item ?: ziel.item
    val l = liste
    val staffeln = AmStueck.staffelnOrdnen(d?.seasons ?: emptyList())
    // Mehrere Fassungen desselben Films (Issue #8): Dann fragt „Abspielen"
    // zuerst nach der Qualität — nur bei einem Film für sich, nicht aus
    // einer Liste heraus (die läuft am Stück, mit der Standard-Fassung).
    val fassungen = Fassungen.liste(d?.files ?: emptyList())
    val qualitaetFragen = l == null && item.kind != "series" && fassungen.size > 1

    // Was „Abspielen" heisst, hängt davon ab, woher man kommt: aus einer
    // Liste läuft die Liste ab hier weiter; eine Serie beginnt bei ihrer
    // ersten offenen Folge; ein Film ist ein Film.
    fun abspielen(ab: Double? = null, dateiId: String? = null) {
        if (sucht) return
        sucht = true
        hinweis = null
        scope.launch {
            try {
                val start = withContext(Dispatchers.IO) {
                    val folge = Abspielfolge(client)
                    when {
                        l != null && platz >= 0 ->
                            folge.naechstesInListe(l.items, platz)
                                ?.let { TvAbspielen(it.titel, l.items, it.index, listenName = l.name) }
                        item.kind == "series" ->
                            folge.naechstesInListe(listOf(item), 0)?.let { TvAbspielen(it.titel) }
                        else -> TvAbspielen(item, ab = ab, dateiId = dateiId)
                    }
                }
                if (start != null) onPlay(start)
                else hinweis = if (item.kind == "series") tx(R.string.alle_folgen_sind_gesehen_oder)
                               else tx(R.string.ab_hier_ist_nichts_was)
            } catch (e: LumenException) {
                hinweis = e.message
            }
            sucht = false
        }
    }

    /** Etwas am eigenen Stand ändern — danach neu laden. */
    fun aendern(block: (ApiClient) -> Unit) {
        scope.launch {
            try {
                withContext(Dispatchers.IO) { block(client) }
                stand++
            } catch (e: LumenException) {
                hinweis = e.message
            }
        }
    }

    /** Den Titel in der Liste verschieben, aus der man kommt. */
    fun verschieben(schritt: Int) {
        val li = l ?: return
        val von = platz
        val nach = von + schritt
        if (von < 0 || nach < 0 || nach >= li.items.size) return
        val neu = li.items.toMutableList()
        val t = neu.removeAt(von)
        neu.add(nach, t)
        liste = li.copy(items = neu)
        platz = nach
        scope.launch {
            try {
                withContext(Dispatchers.IO) { client.orderPlaylist(li.id, neu.map { it.id }) }
                onListeGeaendert()
            } catch (e: LumenException) {
                hinweis = e.message
            }
        }
    }

    Box(Modifier.fillMaxSize()) {
        TvHintergrund(client, item)
        LazyColumn(contentPadding = PaddingValues(start = 56.dp, end = 56.dp, bottom = 56.dp)) {
            item(key = "lumen:kopf") {
                Column(Modifier.fillParentMaxHeight(0.94f).padding(top = 48.dp),
                       verticalArrangement = Arrangement.Bottom) {
                    Text(item.title, fontSize = 52.sp, fontWeight = FontWeight.Bold,
                         maxLines = 2, overflow = TextOverflow.Ellipsis,
                         modifier = Modifier.widthIn(max = 1000.dp))
                    item.originalTitle?.takeIf { it.isNotBlank() && it != item.title }?.let {
                        Text(it, color = Tv.label2, fontSize = 18.sp)
                    }
                    Spacer(Modifier.height(8.dp))
                    Text(
                        buildList {
                            val zeile = Titelseite.metaZeile(item)
                            if (zeile.isNotEmpty()) add(zeile)
                            if (item.kind == "series") add(tx(R.string.serie))
                            if (item.kind != "series" && item.finished) add("gesehen")
                            if (l != null) add(tx(R.string.aus_x_x_von_x, l.name, platz + 1, l.items.size))
                        }.joinToString(" · "),
                        color = Tv.label2, fontSize = 18.sp)
                    // Auflösung, HDR, Ton, Sprachen, Untertitel, direkt oder umgewandelt.
                    val technik = Technik.abzeichen(item, d?.playable, direkt)
                    if (technik.isNotEmpty()) {
                        Spacer(Modifier.height(12.dp))
                        TvTechnikZeile(technik)
                    }
                    item.overview?.takeIf { it.isNotBlank() }?.let {
                        Spacer(Modifier.height(14.dp))
                        Text(it, color = Tv.label2, fontSize = 17.sp, lineHeight = 25.sp,
                             maxLines = 4, overflow = TextOverflow.Ellipsis,
                             modifier = Modifier.widthIn(max = 900.dp))
                    }
                    Spacer(Modifier.height(26.dp))
                    LazyRow(horizontalArrangement = Arrangement.spacedBy(14.dp),
                            contentPadding = PaddingValues(vertical = 6.dp, horizontal = 2.dp),
                            verticalAlignment = Alignment.CenterVertically) {
                        item(key = "abspielen") {
                            TvButton(
                                when {
                                    l != null -> tx(R.string.ab_hier_abspielen)
                                    item.kind == "series" -> Titelseite.serienText(item, folgen ?: emptyList())
                                    else -> Titelseite.abspielText(item)
                                },
                                gross = true,
                                modifier = Modifier.focusRequester(anfang),
                            ) { if (qualitaetFragen) { qualitaetAb = null; qualitaetOffen = true } else abspielen() }
                        }
                        if (l == null && item.kind != "series" && Titelseite.hatStand(item)) {
                            item(key = "vonvorn") {
                                TvButton(tx(R.string.von_vorn)) {
                                    if (qualitaetFragen) { qualitaetAb = 0.0; qualitaetOffen = true } else abspielen(ab = 0.0)
                                }
                            }
                        }
                        item(key = "merken") {
                            TvButton(if (item.onList) tx(R.string.aus_der_merkliste) else tx(R.string.merken)) {
                                aendern { c -> if (item.onList) c.removeFromWatchlist(item.id) else c.addToWatchlist(item.id) }
                            }
                        }
                        // Das Herz gleich neben „Merken" (Issue #10) — für Filme und Serien.
                        if (item.kind == "movie" || item.kind == "series") {
                            item(key = "herz") {
                                TvHerz(item.favorite) {
                                    aendern { c -> if (item.favorite) c.removeFavorite(item.id) else c.addFavorite(item.id) }
                                }
                            }
                        }
                        // In eine Liste kommen Filme und Serien — so will es der Server.
                        if (item.kind == "movie" || item.kind == "series") {
                            item(key = "zurliste") { TvButton(tx(R.string.zur_liste)) { zurListe = true } }
                        }
                        item(key = "gesehen") {
                            // Bei einer Serie: alle Folgen (der Server markiert sie).
                            TvButton(when {
                                item.finished && item.kind == "series" -> tx(R.string.serie_nicht_mehr_als_gesehen)
                                item.kind == "series" -> tx(R.string.ganze_serie_als_gesehen)
                                item.finished -> tx(R.string.nicht_mehr_als_gesehen)
                                else -> tx(R.string.als_gesehen_markieren)
                            }) {
                                aendern { c -> if (item.finished) c.clearFinished(item.id) else c.markFinished(item.id) }
                            }
                        }
                        item(key = "bewerten") {
                            TvButton(if (item.myRating > 0) "★".repeat(item.myRating) + "☆".repeat(5 - item.myRating)
                                     else tx(R.string.bewerten)) { bewerten = true }
                        }
                        if (l != null && l.darfAendern) {
                            item(key = "vor") { TvButton(tx(R.string.nach_vorn)) { verschieben(-1) } }
                            item(key = "zurueck") { TvButton(tx(R.string.nach_hinten)) { verschieben(1) } }
                            item(key = "raus") { TvButton(tx(R.string.aus_der_liste_nehmen)) { entfernen = true } }
                        }
                        item(key = "schliessen") { TvButton(tx(R.string.zurueck)) { onClose() } }
                    }
                    hinweis?.let {
                        Spacer(Modifier.height(12.dp))
                        Text(it, color = Tv.label2, fontSize = 17.sp)
                    }
                    Spacer(Modifier.height(24.dp))
                }
            }

            // Staffeln als Pillen, darunter die Folgen der gewählten — ein
            // Druck spielt die Folge ab.
            if (staffeln.isNotEmpty()) {
                item(key = "lumen:staffeln") {
                    Column(Modifier.padding(bottom = 30.dp)) {
                        Text(tx(R.string.folgen_2), fontSize = 24.sp, fontWeight = FontWeight.SemiBold)
                        Spacer(Modifier.height(12.dp))
                        if (staffeln.size > 1) {
                            LazyRow(horizontalArrangement = Arrangement.spacedBy(10.dp),
                                    contentPadding = PaddingValues(vertical = 6.dp, horizontal = 2.dp)) {
                                items(staffeln, key = { it.id }) { st ->
                                    TvWahlPille(AmStueck.staffelName(st), st.id == staffel) {
                                        if (staffel != st.id) { staffel = st.id; folgen = null }
                                    }
                                }
                            }
                            Spacer(Modifier.height(8.dp))
                        }
                        val f = folgen
                        // Die gewählte Staffel auf einmal — oder zurück, wenn alles gesehen ist.
                        val staffelNr = staffeln.firstOrNull { it.id == staffel }?.seasonNo ?: f?.firstOrNull()?.seasonNo
                        if (!f.isNullOrEmpty() && staffelNr != null) {
                            val alle = f.all { it.finished }
                            Row(verticalAlignment = Alignment.CenterVertically,
                                modifier = Modifier.padding(vertical = 6.dp)) {
                                TvButton(if (alle) tx(R.string.staffel_nicht_mehr_als_gesehen) else tx(R.string.staffel_als_gesehen)) {
                                    aendern { c ->
                                        if (alle) c.clearFinished(item.id, staffel = staffelNr)
                                        else c.markFinished(item.id, staffel = staffelNr)
                                    }
                                }
                                Spacer(Modifier.width(18.dp))
                                Text(tx(R.string.eine_folge_lange_druecken_als),
                                     color = Tv.label3, fontSize = 15.sp)
                            }
                        }
                        when {
                            f == null -> Box(Modifier.height(120.dp), Alignment.CenterStart) {
                                CircularProgressIndicator(color = Tv.label2)
                            }
                            f.isEmpty() -> Text(tx(R.string.in_dieser_staffel_liegt_noch),
                                                color = Tv.label2, fontSize = 18.sp)
                            else -> LazyRow(horizontalArrangement = Arrangement.spacedBy(24.dp),
                                            contentPadding = PaddingValues(vertical = 12.dp, horizontal = 4.dp)) {
                                items(f, key = { it.id }) { folge ->
                                    TvFolgenKarte(client, folge,
                                        onGesehen = {
                                            aendern { c ->
                                                if (folge.finished) c.clearFinished(folge.id) else c.markFinished(folge.id)
                                            }
                                        }) { onPlay(TvAbspielen(folge)) }
                                }
                            }
                        }
                    }
                }
            }

            // Die Besetzung: ein Druck führt zu allem mit dieser Person.
            if (d != null && d.cast.isNotEmpty()) {
                item(key = "lumen:besetzung") {
                    Column(Modifier.padding(bottom = 30.dp)) {
                        Text(tx(R.string.besetzung), fontSize = 24.sp, fontWeight = FontWeight.SemiBold)
                        Spacer(Modifier.height(12.dp))
                        LazyRow(horizontalArrangement = Arrangement.spacedBy(20.dp),
                                contentPadding = PaddingValues(vertical = 6.dp, horizontal = 2.dp)) {
                            items(d.cast.distinctBy { it.name }, key = { it.name }) { p ->
                                TvPersonKachel(client, p.name, p.bild, p.rolle) { onPerson(p.name) }
                            }
                        }
                    }
                }
            }

            // Die Filmreihe: vorn „Ganze Reihe", dann ihre Filme; dieser hervorgehoben.
            d?.collection?.let { reihe ->
                item(key = "lumen:reihe") {
                    Column(Modifier.padding(bottom = 30.dp)) {
                        Text("${reihe.name} · ${reihe.anzahlText}", fontSize = 24.sp, fontWeight = FontWeight.SemiBold)
                        Spacer(Modifier.height(12.dp))
                        LazyRow(horizontalArrangement = Arrangement.spacedBy(28.dp),
                                contentPadding = PaddingValues(vertical = 12.dp, horizontal = 4.dp)) {
                            item(key = "lumen:ganze-reihe") {
                                TvKachel(bild = null, titel = tx(R.string.ganze_reihe), zeile = reihe.anzahlText,
                                         symbol = "›", breite = 180.dp) { onReihe(reihe.id) }
                            }
                            items(reihe.items, key = { it.id }) { film ->
                                TvCard(client, film, onFocus = {},
                                       onClick = { if (film.id != item.id) onOpen(film) },
                                       breite = 180.dp, hervorgehoben = film.id == item.id)
                            }
                        }
                    }
                }
            }

            // Ähnliche Titel: gleiche Reihe, geteilte Besetzung, gleiche Genres.
            if (d != null && d.similar.isNotEmpty()) {
                item(key = "lumen:aehnlich") {
                    TvKartenReihe(client, tx(R.string.aehnliche_titel), d.similar,
                                  onOpen = { if (it.id != item.id) onOpen(it) }, breite = 180.dp)
                }
            }

            if (d != null && d.sharedRatings.isNotEmpty()) {
                item(key = "lumen:haushalt") {
                    Column(Modifier.padding(bottom = 30.dp)) {
                        Text(tx(R.string.im_haushalt), fontSize = 24.sp, fontWeight = FontWeight.SemiBold)
                        Spacer(Modifier.height(8.dp))
                        d.sharedRatings.forEach { r -> TvEinstellung(r.name, wert = "★".repeat(r.stars)) }
                    }
                }
            }

            // Die Dateien: Name, Auflösung, Codec, HDR, Grösse — wie im Browser.
            if (d != null && d.files.isNotEmpty()) {
                item(key = "lumen:dateien") {
                    Column(Modifier.padding(bottom = 30.dp)) {
                        Text(tx(R.string.dateien), fontSize = 24.sp, fontWeight = FontWeight.SemiBold)
                        Spacer(Modifier.height(8.dp))
                        d.files.forEach { f -> TvEinstellung(f.fileName, wert = Titelseite.dateiZeile(f)) }
                    }
                }
            }
        }
    }

    if (zurListe) {
        TvZurListe(client, item.id, onGeaendert = onListeGeaendert) { zurListe = false }
    }

    if (qualitaetOffen) {
        TvQualitaet(fassungen, onClose = { qualitaetOffen = false }) { f ->
            qualitaetOffen = false
            abspielen(ab = qualitaetAb, dateiId = f.id)
        }
    }

    if (bewerten) {
        TvBewerten(item.myRating, onClose = { bewerten = false }) { sterne ->
            bewerten = false
            aendern { c -> c.rate(item.id, sterne, shared = Einstellungen.bewertungenTeilen(prefs)) }
        }
    }

    if (entfernen && l != null) {
        TvFrage(
            titel = tx(R.string.x_aus_x_nehmen, item.title, l.name),
            text = tx(R.string.der_titel_bleibt_in_der),
            ja = tx(R.string.herausnehmen),
            onNein = { entfernen = false },
        ) {
            entfernen = false
            scope.launch {
                try {
                    withContext(Dispatchers.IO) { client.removeFromPlaylist(l.id, item.id) }
                    onListeGeaendert()
                    onClose()
                } catch (e: LumenException) {
                    hinweis = e.message
                }
            }
        }
    }
}

/** Die Technik-Zeile: kleine umrandete Abzeichen; „Direkt abspielbar" grün, „Wird umgewandelt" blau. */
/**
 * Der Herz-Knopf: wie die übrigen Knöpfe, das Herz rot, wenn der Titel zu
 * den Favoriten gehört. Mit Wort daneben — ein Symbol allein ist vom Sofa
 * aus schwer zu deuten.
 */
@Composable
private fun TvHerz(an: Boolean, onClick: () -> Unit) {
    var fokussiert by remember { mutableStateOf(false) }
    Row(
        Modifier
            .onFocusChanged { fokussiert = it.isFocused }
            .clickable(role = Role.Button, onClick = onClick)
            .clip(RoundedCornerShape(28.dp))
            .background(if (fokussiert) Color.White else Color(0x22FFFFFF))
            .padding(horizontal = 24.dp, vertical = 12.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(if (an) "♥" else "♡", fontSize = 20.sp,
             color = if (an) Color(0xFFFF453A) else if (fokussiert) Color.Black else Color.White)
        Spacer(Modifier.width(8.dp))
        Text(if (an) tx(R.string.favorit) else tx(R.string.zu_den_favoriten), fontSize = 18.sp, fontWeight = FontWeight.SemiBold,
             color = if (fokussiert) Color.Black else Color.White, maxLines = 1)
    }
}

@Composable
private fun TvTechnikZeile(abzeichen: List<TechAbzeichen>) {
    Row(horizontalArrangement = Arrangement.spacedBy(10.dp)) {
        abzeichen.forEach { a ->
            val (rand, grund, schrift) = when (a.art) {
                "gut" -> Triple(Color.Transparent, Color(0x2930D158), Tv.good)
                "blau" -> Triple(Color.Transparent, Color(0x2E0A84FF), Color(0xFF4AA3FF))
                else -> Triple(Color(0x33FFFFFF), Color.Transparent, Color.White)
            }
            Text((if (a.untertitel) "▭ " else "") + a.text, color = schrift, fontSize = 15.sp,
                 fontWeight = FontWeight.SemiBold, maxLines = 1,
                 modifier = Modifier.clip(RoundedCornerShape(7.dp)).background(grund)
                     .border(1.dp, rand, RoundedCornerShape(7.dp))
                     .padding(horizontal = 10.dp, vertical = 4.dp))
        }
    }
}

/** Eine Folge als breite Karte: Bild mit Balken, Nummer und Name, Haken, Länge und Datum. */
@OptIn(androidx.compose.foundation.ExperimentalFoundationApi::class)
@Composable
private fun TvFolgenKarte(client: ApiClient, folge: Item, onGesehen: () -> Unit, onClick: () -> Unit) {
    var fokussiert by remember { mutableStateOf(false) }
    val skalierung by animateFloatAsState(if (fokussiert) 1.06f else 1f, label = "fokus")
    Column(
        Modifier
            .width(280.dp)
            .scale(skalierung)
            .onFocusChanged { fokussiert = it.isFocused }
            // Lange drücken schaltet „gesehen" um — ein Druck spielt ab.
            .combinedClickable(role = Role.Button, onLongClick = onGesehen,
                               onLongClickLabel = tx(R.string.gesehen_umschalten), onClick = onClick)
    ) {
        Box(
            Modifier.width(280.dp).height(158.dp).clip(RoundedCornerShape(10.dp))
                .background(Color(0xFF16161B))
                .then(if (fokussiert) Modifier.border(3.dp, Color.White, RoundedCornerShape(10.dp)) else Modifier),
        ) {
            client.wideUrl(folge)?.let {
                AsyncImage(model = it, contentDescription = null, contentScale = ContentScale.Crop,
                           modifier = Modifier.fillMaxSize())
            }
            if (folge.progress > 1 && !folge.finished) {
                Box(Modifier.align(Alignment.BottomCenter).fillMaxWidth().height(4.dp)
                        .background(Color(0x73000000))) {
                    Box(Modifier.fillMaxWidth((folge.progress / 100).coerceIn(0.0, 1.0).toFloat())
                            .fillMaxHeight().background(Color.White))
                }
            }
        }
        Spacer(Modifier.height(10.dp))
        Text("${folge.episodeNo ?: ""}  ${folge.title}" + if (folge.finished) "  ✓" else "",
             fontSize = 17.sp, maxLines = 1, overflow = TextOverflow.Ellipsis,
             fontWeight = if (fokussiert) FontWeight.SemiBold else FontWeight.Normal)
        val zeile = Titelseite.folgenZeile(folge)
        if (zeile.isNotEmpty()) Text(zeile, color = Tv.label2, fontSize = 14.sp)
        if (fokussiert) {
            folge.overview?.takeIf { it.isNotBlank() }?.let {
                Text(it, color = Tv.label2, fontSize = 14.sp, maxLines = 3, overflow = TextOverflow.Ellipsis)
            }
        }
    }
}

/**
 * „In welcher Qualität?" — jede Fassung als Zeile („1080p · MKV · 7,9 GB"),
 * der Fokus auf der ersten: Sie ist der Standard, so wie der Server ordnet.
 */
@Composable
private fun TvQualitaet(fassungen: List<MediaFile>, onClose: () -> Unit, onWahl: (MediaFile) -> Unit) {
    val anfang = remember { FocusRequester() }
    LaunchedEffect(Unit) {
        kotlinx.coroutines.delay(100)
        runCatching { anfang.requestFocus() }
    }
    TvDialogRahmen(onDismiss = onClose) {
        Text(tx(R.string.in_welcher_qualitaet), fontSize = 30.sp, fontWeight = FontWeight.Bold)
        Spacer(Modifier.height(16.dp))
        fassungen.forEachIndexed { i, f ->
            TvZeile(text = Fassungen.name(f).ifEmpty { f.fileName },
                    nebenzeile = if (i == 0) tx(R.string.standard) else null,
                    modifier = if (i == 0) Modifier.focusRequester(anfang) else Modifier) { onWahl(f) }
        }
    }
}

/** Bewerten: fünf bis ein Stern, dazu „Löschen", wenn schon bewertet. Nur für mich sichtbar. */
@Composable
private fun TvBewerten(jetzt: Int, onClose: () -> Unit, onWahl: (Int) -> Unit) {
    val anfang = remember { FocusRequester() }
    LaunchedEffect(Unit) {
        kotlinx.coroutines.delay(100)
        runCatching { anfang.requestFocus() }
    }
    TvDialogRahmen(onDismiss = onClose) {
        Text(tx(R.string.deine_bewertung), fontSize = 30.sp, fontWeight = FontWeight.Bold)
        Spacer(Modifier.height(6.dp))
        Text(tx(R.string.nur_fuer_dich_sichtbar_im),
             color = Tv.label2, fontSize = 16.sp)
        Spacer(Modifier.height(16.dp))
        val start = if (jetzt in 1..5) jetzt else 5
        (5 downTo 1).forEach { n ->
            TvZeile(text = "★".repeat(n) + "☆".repeat(5 - n) + tx(R.string.x_von_5, n),
                    rechts = if (n == jetzt) "✓" else "",
                    modifier = if (n == start) Modifier.focusRequester(anfang) else Modifier) { onWahl(n) }
        }
        if (jetzt > 0) TvZeile(text = tx(R.string.bewertung_loeschen), farbe = Tv.bad) { onWahl(0) }
    }
}

// ---------------------------------------------------------------- Zur Liste

/**
 * In welchen Listen der Titel liegt, mit Haken; ein Druck legt ihn hinein
 * oder nimmt ihn heraus. Angeboten wird nur, worin diese Person schreiben
 * darf — die eigenen Listen und die gemeinsamen.
 */
@Composable
internal fun TvZurListe(
    client: ApiClient,
    itemId: String,
    onGeaendert: () -> Unit,
    onClose: () -> Unit,
) {
    var listen by remember(itemId) { mutableStateOf<List<Liste>?>(null) }
    var fehler by remember { mutableStateOf<String?>(null) }
    var reload by remember { mutableStateOf(0) }
    var neu by remember { mutableStateOf(false) }
    val scope = rememberCoroutineScope()
    val anfang = remember { FocusRequester() }

    LaunchedEffect(itemId, reload) {
        try {
            listen = withContext(Dispatchers.IO) { client.playlists(itemId) }.filter { it.darfAendern }
            fehler = null
        } catch (e: LumenException) {
            fehler = e.message
            listen = emptyList()
        }
    }

    fun umschalten(l: Liste) {
        // Gleich umschalten, damit der Haken nicht auf den Server wartet.
        listen = listen?.map { if (it.id == l.id) it.copy(enthaelt = !l.enthaelt) else it }
        scope.launch {
            try {
                withContext(Dispatchers.IO) {
                    if (l.enthaelt) client.removeFromPlaylist(l.id, itemId)
                    else client.addToPlaylist(l.id, itemId)
                }
                onGeaendert()
            } catch (e: LumenException) {
                fehler = e.message
            }
            reload++
        }
    }

    if (neu) {
        TvNameDialog(tx(R.string.neue_liste), "", tx(R.string.anlegen), onDismiss = { neu = false }) { name ->
            neu = false
            scope.launch {
                try {
                    withContext(Dispatchers.IO) {
                        val angelegt = client.createPlaylist(name)
                        client.addToPlaylist(angelegt.id, itemId)
                    }
                    onGeaendert()
                } catch (e: LumenException) {
                    fehler = e.message
                }
                reload++
            }
        }
        return
    }

    val alle = listen
    LaunchedEffect(alle != null) {
        if (alle != null) {
            kotlinx.coroutines.delay(100)
            runCatching { anfang.requestFocus() }
        }
    }

    TvDialogRahmen(onDismiss = onClose) {
        Text(tx(R.string.zur_liste), fontSize = 30.sp, fontWeight = FontWeight.Bold)
        Spacer(Modifier.height(6.dp))
        Text(tx(R.string.ein_druck_legt_den_titel),
             color = Tv.label2, fontSize = 16.sp)
        Spacer(Modifier.height(20.dp))
        if (alle == null) {
            Box(Modifier.fillMaxWidth().height(80.dp), Alignment.Center) {
                CircularProgressIndicator(color = Tv.label2)
            }
        } else {
            Column(Modifier.heightIn(max = 420.dp).verticalScroll(rememberScrollState())) {
                alle.forEachIndexed { i, l ->
                    TvZeile(
                        text = l.name,
                        nebenzeile = if (l.meine) null else tx(R.string.von_x, l.von.name),
                        rechts = if (l.enthaelt) "✓" else "",
                        modifier = if (i == 0) Modifier.focusRequester(anfang) else Modifier,
                    ) { umschalten(l) }
                }
                TvZeile(text = tx(R.string.neue_liste_2), farbe = Tv.accent,
                        modifier = if (alle.isEmpty()) Modifier.focusRequester(anfang) else Modifier) {
                    neu = true
                }
            }
        }
        fehler?.let {
            Spacer(Modifier.height(10.dp))
            Text(it, color = Tv.bad, fontSize = 16.sp)
        }
        Spacer(Modifier.height(22.dp))
        Row(horizontalArrangement = Arrangement.End, modifier = Modifier.fillMaxWidth()) {
            TvButton(tx(R.string.fertig)) { onClose() }
        }
    }
}

// ---------------------------------------------------------------- Bausteine

/**
 * Ein Dialog, der die Fernbedienung für sich hat. Ein eigenes Fenster
 * statt einer Schicht über der Seite: sonst wandert der Fokus mit den
 * Pfeiltasten hinter den Dialog, und „Zurück" schliesst die falsche Seite.
 */
@Composable
internal fun TvDialogRahmen(onDismiss: () -> Unit, content: @Composable ColumnScope.() -> Unit) {
    Dialog(onDismissRequest = onDismiss,
           properties = DialogProperties(usePlatformDefaultWidth = false)) {
        Column(
            Modifier
                .width(680.dp)
                .clip(RoundedCornerShape(22.dp))
                .background(Color(0xFF16161B))
                .padding(32.dp),
            content = content)
    }
}

/** Nach einem Namen fragen — für eine neue Liste und fürs Umbenennen. */
@Composable
internal fun TvNameDialog(
    titel: String,
    vorher: String,
    knopf: String,
    onDismiss: () -> Unit,
    onConfirm: (String) -> Unit,
) {
    var name by remember { mutableStateOf(vorher) }
    val feld = remember { FocusRequester() }
    LaunchedEffect(Unit) {
        kotlinx.coroutines.delay(100)
        runCatching { feld.requestFocus() }
    }

    TvDialogRahmen(onDismiss = onDismiss) {
        Text(titel, fontSize = 30.sp, fontWeight = FontWeight.Bold)
        Spacer(Modifier.height(20.dp))
        OutlinedTextField(
            value = name,
            onValueChange = { name = it },
            singleLine = true,
            placeholder = { Text(tx(R.string.name)) },
            textStyle = androidx.compose.ui.text.TextStyle(fontSize = 22.sp),
            modifier = Modifier.fillMaxWidth().focusRequester(feld))
        Spacer(Modifier.height(24.dp))
        Row(horizontalArrangement = Arrangement.spacedBy(18.dp)) {
            if (name.isNotBlank()) TvButton(knopf) { onConfirm(name.trim()) }
            TvButton(tx(R.string.abbrechen)) { onDismiss() }
        }
    }
}

/** Eine Frage mit zwei Antworten; der Fokus steht auf „Nein". */
@Composable
internal fun TvFrage(
    titel: String,
    text: String,
    ja: String,
    onNein: () -> Unit,
    onJa: () -> Unit,
) {
    val nein = remember { FocusRequester() }
    LaunchedEffect(Unit) {
        kotlinx.coroutines.delay(100)
        runCatching { nein.requestFocus() }
    }
    TvDialogRahmen(onDismiss = onNein) {
        Text(titel, fontSize = 28.sp, fontWeight = FontWeight.Bold,
             maxLines = 3, overflow = TextOverflow.Ellipsis)
        Spacer(Modifier.height(10.dp))
        Text(text, color = Tv.label2, fontSize = 17.sp)
        Spacer(Modifier.height(24.dp))
        Row(horizontalArrangement = Arrangement.spacedBy(18.dp)) {
            TvButton(tx(R.string.abbrechen), modifier = Modifier.focusRequester(nein)) { onNein() }
            TvButton(ja) { onJa() }
        }
    }
}

/** Eine Zeile in einem Dialog: Text links, ein Haken rechts, fokussierbar. */
@Composable
internal fun TvZeile(
    text: String,
    modifier: Modifier = Modifier,
    nebenzeile: String? = null,
    rechts: String = "",
    farbe: Color = Color.White,
    onClick: () -> Unit,
) {
    var fokussiert by remember { mutableStateOf(false) }
    Row(
        modifier
            .fillMaxWidth()
            .onFocusChanged { fokussiert = it.isFocused }
            .clickable(role = Role.Button, onClick = onClick)
            .clip(RoundedCornerShape(12.dp))
            .background(if (fokussiert) Color(0x33FFFFFF) else Color.Transparent)
            .padding(horizontal = 16.dp, vertical = 14.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Column(Modifier.weight(1f)) {
            Text(text, fontSize = 20.sp, color = farbe, maxLines = 1, overflow = TextOverflow.Ellipsis)
            nebenzeile?.let { Text(it, fontSize = 14.sp, color = Tv.label3) }
        }
        if (rechts.isNotEmpty()) Text(rechts, fontSize = 22.sp, color = Color(0xFF30D158))
    }
}
