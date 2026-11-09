// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package org.lumen.app

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.grid.GridCells
import androidx.compose.foundation.lazy.grid.GridItemSpan
import androidx.compose.foundation.lazy.grid.LazyVerticalGrid
import androidx.compose.foundation.lazy.grid.items
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.ArrowBack
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.Search
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import coil.compose.AsyncImage
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.delay
import kotlinx.coroutines.withContext
import org.lumen.core.*

/**
 * Die Suche — und was dahinter liegt.
 *
 * Der Server deutet die Eingabe: „Komödie 90er" wird zu Genre und
 * Jahrzehnt, „Filme mit Tom Hanks" zu einer Person. Darum stehen über den
 * Titeln die Personen (rund, mit Bild oder Anfangsbuchstabe) und die
 * passenden Genres, und über dem Raster eine Zeile, die sagt, was gesucht
 * wurde: „12 Treffer · mit Tom Hanks · 1990–1999".
 *
 * Hinter einer Person liegt „alles mit …", hinter einem Genre das Raster
 * der Filme damit. Beides sind Abfragen an /items — kein Verlauf, nichts
 * über andere.
 */

val SUCH_PLATZHALTER: String get() = tx(R.string.titel_schauspieler_genre_jahr)

/**
 * Die Zeile, die wie ein Suchfeld aussieht und die Suche öffnet. Steht auf
 * der Startseite und über den Rastern: ein Feld pro Reiter mit eigener
 * Eingabe wäre dreimal dasselbe, und die Tastatur ginge jedes Mal auf.
 */
@Composable
fun SuchLeiste(onClick: () -> Unit, modifier: Modifier = Modifier) {
    Row(
        modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(12.dp))
            .background(Lumen.fill)
            .clickableRow(onClick)
            .padding(horizontal = 14.dp, vertical = 12.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Icon(Icons.Filled.Search, null, tint = Lumen.label3, modifier = Modifier.size(20.dp))
        Spacer(Modifier.width(10.dp))
        Text(SUCH_PLATZHALTER, color = Lumen.label3, fontSize = 15.sp)
    }
}

@Composable
fun SuchScreen(
    model: LumenViewModel,
    onBack: () -> Unit,
    onOpen: (Item) -> Unit,
    onPerson: (String) -> Unit,
    onGenre: (String) -> Unit,
) {
    var eingabe by remember { mutableStateOf("") }
    var ergebnis by remember { mutableStateOf<Suchergebnis?>(null) }
    var sucht by remember { mutableStateOf(false) }
    val error by model.error.collectAsStateWithLifecycle()
    val feld = remember { FocusRequester() }

    // Die Tastatur gleich auf — man kommt hierher, um zu tippen.
    LaunchedEffect(Unit) {
        delay(80)
        runCatching { feld.requestFocus() }
    }

    // Erst suchen, wenn das Tippen kurz ruht — sonst eine Anfrage je Taste.
    LaunchedEffect(eingabe) {
        val q = eingabe.trim()
        if (q.isEmpty()) {
            ergebnis = null
            sucht = false
            return@LaunchedEffect
        }
        delay(250)
        sucht = true
        ergebnis = model.fetch { it.search(q) }
        sucht = false
    }

    Column(Modifier.fillMaxSize()) {
        Row(
            Modifier.fillMaxWidth().padding(start = 4.dp, end = 16.dp, top = 8.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            IconButton(onClick = onBack) {
                Icon(Icons.Filled.ArrowBack, tx(R.string.zurueck), tint = Color.White)
            }
            OutlinedTextField(
                value = eingabe,
                onValueChange = { eingabe = it },
                singleLine = true,
                placeholder = { Text(SUCH_PLATZHALTER, color = Lumen.label3) },
                leadingIcon = { Icon(Icons.Filled.Search, null, tint = Lumen.label3) },
                trailingIcon = {
                    if (eingabe.isNotEmpty()) {
                        IconButton(onClick = { eingabe = "" }) {
                            Icon(Icons.Filled.Close, tx(R.string.leeren), tint = Lumen.label2)
                        }
                    }
                },
                keyboardOptions = KeyboardOptions(imeAction = ImeAction.Search),
                keyboardActions = KeyboardActions(onSearch = { /* läuft ohnehin beim Tippen */ }),
                shape = RoundedCornerShape(12.dp),
                colors = OutlinedTextFieldDefaults.colors(
                    focusedContainerColor = Lumen.fill,
                    unfocusedContainerColor = Lumen.fill,
                    focusedBorderColor = Lumen.accent,
                    unfocusedBorderColor = Color.Transparent,
                ),
                modifier = Modifier.weight(1f).focusRequester(feld),
            )
        }

        val q = eingabe.trim()
        val e = ergebnis
        when {
            q.isEmpty() -> Box(Modifier.fillMaxSize().padding(32.dp), Alignment.Center) {
                Text(
                    tx(R.string.tippe_los_auch_komoedie_2019),
                    color = Lumen.label2, textAlign = TextAlign.Center)
            }
            e == null -> StateOverlay(sucht, error)
            else -> Suchergebnisse(model, q, e, onOpen, onPerson, onGenre)
        }
    }
}

@Composable
private fun Suchergebnisse(
    model: LumenViewModel,
    q: String,
    e: Suchergebnis,
    onOpen: (Item) -> Unit,
    onPerson: (String) -> Unit,
    onGenre: (String) -> Unit,
) {
    LazyVerticalGrid(
        columns = GridCells.Adaptive(Lumen.posterWidth),
        contentPadding = PaddingValues(16.dp),
        horizontalArrangement = Arrangement.spacedBy(14.dp),
        verticalArrangement = Arrangement.spacedBy(18.dp),
    ) {
        if (e.personen.isNotEmpty()) {
            item(span = { GridItemSpan(maxLineSpan) }) {
                Column {
                    Text(tx(R.string.personen), fontSize = 18.sp, fontWeight = FontWeight.SemiBold,
                         modifier = Modifier.padding(bottom = 10.dp))
                    LazyRow(horizontalArrangement = Arrangement.spacedBy(16.dp)) {
                        items(e.personen, key = { it.name }) { p ->
                            PersonKarte(model, p) { onPerson(p.name) }
                        }
                    }
                }
            }
        }
        if (e.genres.isNotEmpty()) {
            item(span = { GridItemSpan(maxLineSpan) }) {
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp),
                    modifier = Modifier.horizontalScrollable()) {
                    e.genres.forEach { g ->
                        GenreChip(g) { onGenre(g) }
                    }
                }
            }
        }
        if (e.items.isNotEmpty()) {
            item(span = { GridItemSpan(maxLineSpan) }) {
                Text(e.trefferZeile, color = Lumen.label2, fontSize = 14.sp)
            }
            items(e.items, key = { it.id }) { item -> PosterCard(model, item, onOpen) }
        } else if (e.personen.isNotEmpty() || e.genres.isNotEmpty()) {
            item(span = { GridItemSpan(maxLineSpan) }) {
                val d = e.deutung
                Text(
                    if (d != null && d.gedeutet) tx(R.string.kein_titel_passt_genau_gesucht, d.beschreibung)
                    else tx(R.string.kein_titel_passt_genau_aber),
                    color = Lumen.label2, fontSize = 15.sp,
                    modifier = Modifier.padding(top = 8.dp))
            }
        } else {
            // Nichts da: dann lässt sich der Titel wünschen — wie im Browser.
            item(span = { GridItemSpan(maxLineSpan) }) { Wuenschen(model, q) }
        }
        item(span = { GridItemSpan(maxLineSpan) }) { Spacer(Modifier.height(24.dp)) }
    }
}

/** „Nichts gefunden" — mit dem Angebot, sich den Titel zu wünschen. */
@Composable
private fun Wuenschen(model: LumenViewModel, q: String) {
    var geschickt by remember(q) { mutableStateOf(false) }
    Column(Modifier.padding(top = 8.dp)) {
        Text(tx(R.string.nichts_gefunden_fuer_x, q), fontSize = 16.sp)
        Spacer(Modifier.height(6.dp))
        Text(tx(R.string.du_kannst_dir_den_titel),
             color = Lumen.label2, fontSize = 14.sp)
        Spacer(Modifier.height(14.dp))
        if (geschickt) {
            Text(tx(R.string.wunsch_an_den_besitzer_geschickt), color = Lumen.good, fontSize = 14.sp)
        } else {
            PrimaryButton(tx(R.string.x_wuenschen, q)) {
                model.send(onDone = { geschickt = model.error.value == null }) { it.wish(q) }
            }
        }
    }
}

/** Eine Person: rund, Bild oder Anfangsbuchstabe, Name, „7 Titel". */
@Composable
private fun PersonKarte(model: LumenViewModel, p: PersonTreffer, onClick: () -> Unit) {
    Column(
        Modifier.width(88.dp).clickableRow(onClick),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Box(
            Modifier.size(72.dp).clip(CircleShape)
                .background(Brush.linearGradient(listOf(Color(0xFF2A2A31), Color(0xFF16161B)))),
            contentAlignment = Alignment.Center,
        ) {
            Text(p.initial, color = Lumen.label2, fontSize = 26.sp, fontWeight = FontWeight.Medium)
            val bild = model.client.bildUrl(p.bild)
            if (bild != null) {
                AsyncImage(model = bild, contentDescription = null,
                           contentScale = ContentScale.Crop, modifier = Modifier.fillMaxSize())
            }
        }
        Spacer(Modifier.height(6.dp))
        Text(p.name, fontSize = 13.sp, maxLines = 1, overflow = TextOverflow.Ellipsis,
             textAlign = TextAlign.Center)
        Text(p.anzahlText, fontSize = 12.sp, color = Lumen.label2)
    }
}

@Composable
private fun GenreChip(genre: String, onClick: () -> Unit) {
    Text(
        genre,
        fontSize = 14.sp,
        modifier = Modifier
            .clip(CircleShape)
            .background(Lumen.fill)
            .clickableRow(onClick)
            .padding(horizontal = 14.dp, vertical = 8.dp),
    )
}

/**
 * Die Personenseite (Issue #9): Foto, Lebensdaten, Biografie und Quellen —
 * darunter, worin die Person in der eigenen Mediathek mitspielt. Kennt der
 * Server die Person nicht (oder antwortet er nicht), bleiben nur die Titel.
 */
@Composable
fun PersonScreen(model: LumenViewModel, name: String, onBack: () -> Unit, onOpen: (Item) -> Unit) {
    var items by remember(name) { mutableStateOf<List<Item>>(emptyList()) }
    var info by remember(name) { mutableStateOf<PersonInfo?>(null) }
    var loading by remember(name) { mutableStateOf(true) }
    val error by model.error.collectAsStateWithLifecycle()

    LaunchedEffect(name) {
        items = model.fetch { it.items(person = name, limit = 200) } ?: emptyList()
        loading = false
    }
    // Die Angaben sind Beiwerk: ohne sie steht die Seite trotzdem.
    LaunchedEffect(name) {
        info = withContext(Dispatchers.IO) {
            oderNull { model.client.person(name) }
        }
    }

    Raster(
        titel = name,
        zeile = txn(R.plurals.titel_in_mediathek, items.size),
        items = items, model = model, onBack = onBack, onOpen = onOpen,
        kopf = { info?.let { p -> PersonKopf(model, p) } })
    StateOverlay(loading && items.isEmpty(), error, items.isEmpty() && !loading,
        tx(R.string.nichts_dabei_was_du_sehen))
}

/** Foto, Lebensdaten, Biografie (eingeklappt auf sechs Zeilen) und die Quellen als Links. */
@Composable
private fun PersonKopf(model: LumenViewModel, p: PersonInfo) {
    var offen by remember(p.name) { mutableStateOf(false) }
    // Ob die Biografie länger ist als die sechs Zeilen — erst dann „Mehr lesen".
    var zuLang by remember(p.name) { mutableStateOf(false) }
    val links = androidx.compose.ui.platform.LocalUriHandler.current

    Column(Modifier.padding(bottom = 10.dp)) {
        Row(verticalAlignment = Alignment.Top) {
            val bild = model.client.personBildUrl(p)
            if (bild != null) {
                Artwork(bild, p.name, Modifier.width(96.dp).height(128.dp), corner = 12.dp)
                Spacer(Modifier.width(14.dp))
            }
            Column(Modifier.weight(1f)) {
                p.lebensZeile?.let { Text(it, color = Lumen.label2, fontSize = 14.sp) }
            }
        }
        p.bio?.let { bio ->
            Spacer(Modifier.height(12.dp))
            Text(bio, color = Lumen.label, fontSize = 15.sp, lineHeight = 22.sp,
                 maxLines = if (offen) Int.MAX_VALUE else 6, overflow = TextOverflow.Ellipsis,
                 onTextLayout = { if (!offen) zuLang = it.hasVisualOverflow })
            if (zuLang || offen) {
                Text(if (offen) tx(R.string.weniger) else tx(R.string.mehr_lesen), color = Lumen.accent, fontSize = 15.sp,
                     modifier = Modifier.clickableRow { offen = !offen }.padding(vertical = 6.dp))
            }
        }
        val quellen = p.quellen.filter { it.name.isNotBlank() }
        if (quellen.isNotEmpty() || p.nurEnglisch) {
            Spacer(Modifier.height(8.dp))
            Row(verticalAlignment = Alignment.CenterVertically,
                modifier = Modifier.horizontalScrollable()) {
                if (quellen.isNotEmpty()) Text(tx(R.string.quelle), color = Lumen.label3, fontSize = 12.sp)
                quellen.forEachIndexed { i, q ->
                    val url = q.sichereUrl
                    // Nur http(s) wird zum Link — alles andere bleibt Text.
                    Text(q.name, fontSize = 12.sp,
                         color = if (url != null) Lumen.accent else Lumen.label3,
                         modifier = if (url != null) Modifier.clickableRow {
                             runCatching { links.openUri(url) }
                         } else Modifier)
                    if (i < quellen.size - 1) Text(", ", color = Lumen.label3, fontSize = 12.sp)
                }
                if (p.nurEnglisch) {
                    Text((if (quellen.isNotEmpty()) " · " else "") + PersonInfo.NUR_ENGLISCH,
                         color = Lumen.label3, fontSize = 12.sp)
                }
            }
        }
    }
}

/** Die Filme eines Genres — die Seite hinter einem Genre-Chip. */
@Composable
fun GenreScreen(model: LumenViewModel, genre: String, onBack: () -> Unit, onOpen: (Item) -> Unit) {
    var items by remember(genre) { mutableStateOf<List<Item>>(emptyList()) }
    var loading by remember(genre) { mutableStateOf(true) }
    val error by model.error.collectAsStateWithLifecycle()

    LaunchedEffect(genre) {
        items = model.fetch { it.items(kind = "movie", genre = genre, sort = "title", limit = 200) }
            ?: emptyList()
        loading = false
    }

    Raster(
        titel = genre,
        zeile = Texte.n(TxN.FILME_ANZAHL, items.size),
        items = items, model = model, onBack = onBack, onOpen = onOpen)
    StateOverlay(loading && items.isEmpty(), error, items.isEmpty() && !loading,
        tx(R.string.keine_filme_mit_diesem_genre))
}

@Composable
private fun Raster(
    titel: String,
    zeile: String,
    items: List<Item>,
    model: LumenViewModel,
    onBack: () -> Unit,
    onOpen: (Item) -> Unit,
    // Was zwischen Überschrift und Titeln steht — bei einer Person ihre Angaben.
    kopf: @Composable () -> Unit = {},
) {
    LazyVerticalGrid(
        columns = GridCells.Adaptive(Lumen.posterWidth),
        contentPadding = PaddingValues(16.dp),
        horizontalArrangement = Arrangement.spacedBy(14.dp),
        verticalArrangement = Arrangement.spacedBy(18.dp),
    ) {
        item(span = { GridItemSpan(maxLineSpan) }) {
            Column {
                IconButton(onClick = onBack) {
                    Icon(Icons.Filled.ArrowBack, tx(R.string.zurueck), tint = Color.White)
                }
                Text(titel, fontSize = 24.sp, fontWeight = FontWeight.Bold)
                Spacer(Modifier.height(8.dp))
                kopf()
                Text(zeile, color = Lumen.label2, fontSize = 14.sp)
                Spacer(Modifier.height(6.dp))
            }
        }
        items(items, key = { it.id }) { item -> PosterCard(model, item, onOpen) }
    }
}
