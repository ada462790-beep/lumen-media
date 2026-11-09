// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package org.lumen.tv

import androidx.annotation.StringRes
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.focusable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.grid.GridCells
import androidx.compose.foundation.lazy.grid.GridItemSpan
import androidx.compose.foundation.lazy.grid.LazyVerticalGrid
import androidx.compose.foundation.lazy.grid.items
import androidx.compose.foundation.lazy.grid.rememberLazyGridState
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.draw.clip
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.focus.onFocusChanged
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import org.lumen.core.*

/**
 * Die Bereiche — dieselben wie im Browser, nur ohne Verwaltung: die gibt
 * es nur im Browser auf dem Rechner.
 *
 * Oben in der Navigation stehen nur die sechs mit [oben]: elf Pillen
 * nebeneinander waren mit der Fernbedienung eine lange Wanderung, und die
 * hinteren rollten aus dem Bild. Der Rest liegt unter „Ich".
 */
internal enum class TvBereich(@StringRes private val titelId: Int, val oben: Boolean) {
    HOME(R.string.startseite, true),
    FILME(R.string.filme, true),
    SERIEN(R.string.serien, true),
    MERKLISTE(R.string.merkliste, true),
    SUCHE(R.string.suche, true),
    PROFIL(R.string.ich, true),
    FAVORITEN(R.string.favoriten, false),
    REIHEN(R.string.filmreihen, false),
    LISTEN(R.string.listen, false),
    NEUIGKEITEN(R.string.neuigkeiten, false),
    WUENSCHE(R.string.wuensche, false);

    /** Der Name in der Sprache, die gerade gilt — ein Enum entsteht nur einmal. */
    val titel: String get() = tx(titelId)
}

/** Eine Seite, die über einem Bereich aufgeht — „Zurück" schliesst sie wieder. */
internal sealed interface TvSeite {
    data class Titel(val ziel: TvTitelZiel) : TvSeite
    data class ReiheOffen(val id: String) : TvSeite
    data class ListeOffen(val id: String) : TvSeite
    /** Alles mit einer Person aus der Besetzung. */
    data class Person(val name: String) : TvSeite
    /** Die Filme eines Genres — aus der Suche heraus. */
    data class Genre(val name: String) : TvSeite
}

// ---------------------------------------------------------------- Rückkehr

/**
 * Welche Karte zuletzt geöffnet wurde. Kommt man mit „Zurück" von ihrer
 * Seite, holt genau diese Karte den Fokus — nicht die Navigation oben, wie
 * früher: dann musste man sich jedes Mal wieder nach unten durchdrücken.
 */
internal class TvRueckkehr {
    var id: String? by mutableStateOf(null)
}

internal val LocalTvRueckkehr = staticCompositionLocalOf { TvRueckkehr() }

/**
 * Für eine Karte: ist sie die zuletzt geöffnete, bekommt sie den Fokus,
 * sobald sie wieder da ist. Gibt den Modifier dafür zurück.
 */
@Composable
internal fun Modifier.rueckkehrFokus(id: String): Modifier {
    val rueck = LocalTvRueckkehr.current
    if (rueck.id != id) return this
    val fokus = remember { FocusRequester() }
    LaunchedEffect(id) {
        kotlinx.coroutines.delay(80)
        if (runCatching { fokus.requestFocus() }.isSuccess) rueck.id = null
    }
    return this.focusRequester(fokus)
}

// ---------------------------------------------------------------- Navigation

/**
 * Die Navigation oben: die Bereiche als Pillen, der gewählte hell, an den
 * Neuigkeiten der rote Zähler, ganz rechts das eigene Profilbild. Der Fokus
 * steht beim Öffnen auf dem gewählten Bereich; „runter" führt in die Seite.
 */
@Composable
internal fun TvNavLeiste(
    bereich: TvBereich,
    ungelesen: Int,
    ich: Me,
    client: ApiClient,
    onWahl: (TvBereich) -> Unit,
) {
    val gewaehlt = remember { FocusRequester() }
    val rueck = LocalTvRueckkehr.current
    LaunchedEffect(Unit) {
        kotlinx.coroutines.delay(120)
        // Wartet eine Karte auf den Fokus, bekommt sie ihn; findet sie sich
        // nicht (etwa weil sie nicht mehr in der Liste steht), doch hierher.
        if (rueck.id != null) {
            kotlinx.coroutines.delay(600)
            if (rueck.id == null) return@LaunchedEffect
            rueck.id = null
        }
        runCatching { gewaehlt.requestFocus() }
    }
    // Eine gewöhnliche Zeile statt einer faulen Liste: alle Punkte sind
    // da, der gewählte lässt sich immer fokussieren, und wandert der Fokus
    // nach rechts hinaus, rollt die Zeile von selbst mit.
    Row(
        Modifier.fillMaxWidth().horizontalScroll(rememberScrollState())
            .padding(start = 48.dp, end = 48.dp, top = 22.dp, bottom = 6.dp),
        horizontalArrangement = Arrangement.spacedBy(4.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        // Ein Bereich unter „Ich" lässt „Ich" leuchten.
        val oben = if (bereich.oben) bereich else TvBereich.PROFIL
        TvBereich.entries.filter { it.oben }.forEach { b ->
            TvNavPunkt(
                text = b.titel,
                aktiv = b == oben,
                // Die Neuigkeiten liegen unter „Ich" — dort steht ihr Zähler.
                zahl = if (b == TvBereich.PROFIL) ungelesen else 0,
                modifier = if (b == oben) Modifier.focusRequester(gewaehlt) else Modifier,
                // Schon beim Hinwandern wechseln wäre am Fernseher zu
                // unruhig — jede Seite lädt. Erst die Mitteltaste wählt.
                onClick = { onWahl(b) },
            ) {
                if (b == TvBereich.PROFIL) {
                    TvAvatar(ich.account.initial, ich.account.avatar, 26.dp,
                             client.profilbildUrl(ich.account.id, ich.account.avatar))
                    Spacer(Modifier.width(8.dp))
                }
            }
        }
    }
}

@Composable
private fun TvNavPunkt(
    text: String,
    aktiv: Boolean,
    zahl: Int,
    modifier: Modifier,
    onClick: () -> Unit,
    vorn: @Composable RowScope.() -> Unit,
) {
    var fokussiert by remember { mutableStateOf(false) }
    Row(
        modifier
            .onFocusChanged { fokussiert = it.isFocused }
            .clickable(role = Role.Button, onClick = onClick)
            .clip(RoundedCornerShape(22.dp))
            .background(when {
                fokussiert -> Color.White
                aktiv -> Color(0x33FFFFFF)
                else -> Color.Transparent
            })
            .padding(horizontal = 13.dp, vertical = 8.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        vorn()
        Text(text, fontSize = 16.sp,
             fontWeight = if (aktiv) FontWeight.SemiBold else FontWeight.Normal,
             color = if (fokussiert) Color.Black else if (aktiv) Color.White else Tv.label2)
        if (zahl > 0) {
            Spacer(Modifier.width(8.dp))
            Box(Modifier.heightIn(min = 20.dp).widthIn(min = 20.dp)
                    .clip(RoundedCornerShape(10.dp)).background(Color(0xFFFF453A))
                    .padding(horizontal = 5.dp),
                contentAlignment = Alignment.Center) {
                Text(if (zahl > 9) "9+" else zahl.toString(), fontSize = 12.sp,
                     fontWeight = FontWeight.Bold, color = Color.White)
            }
        }
    }
}

// ---------------------------------------------------------------- Bausteine

/** Eine Pille zum Wählen: ein Genre, eine Staffel, eine Art. Fokussiert weiss. */
@Composable
internal fun TvWahlPille(text: String, gewaehlt: Boolean, modifier: Modifier = Modifier, onClick: () -> Unit) {
    var fokussiert by remember { mutableStateOf(false) }
    Text(
        text, fontSize = 17.sp,
        fontWeight = if (gewaehlt) FontWeight.SemiBold else FontWeight.Normal,
        color = if (fokussiert) Color.Black else Color.White,
        maxLines = 1,
        modifier = modifier
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

/** Eine Überschrift mit Zeile darunter — oben in jedem Bereich. */
@Composable
internal fun TvKopf(titel: String, zeile: String?) {
    Column {
        Text(titel, fontSize = 40.sp, fontWeight = FontWeight.Bold)
        if (!zeile.isNullOrBlank()) {
            Spacer(Modifier.height(4.dp))
            Text(zeile, color = Tv.label2, fontSize = 18.sp)
        }
    }
}

/** Die Breite der Karten im Raster — kleiner als auf Home, damit mehr nebeneinander passen. */
private val RASTER_KARTE: Dp = 170.dp

// ---------------------------------------------------------------- Raster

/**
 * Was über einem Raster gewählt ist: Genre, Sortierung und — nur bei A–Z —
 * der Anfangsbuchstabe. Steht im Gerüst, damit es bleibt, wenn
 * man von einem Titel zurückkommt.
 */
internal data class TvRasterWahl(
    val genre: String? = null,
    val sortierung: Sortierung = Sortierung.TITEL,
    val buchstabe: String? = null,
)

/**
 * Filme oder Serien als Raster, wie im Browser: oben „Alle" und die Genres,
 * darunter die Sortierung und bei A–Z die Buchstabenleiste, daneben die
 * Zahl der Titel, und beim Herunterblättern kommt die nächste Seite dazu.
 * [fest] heisst: das Genre ist vorgegeben (aus der Suche) — dann ohne
 * Knöpfe darüber.
 */
@Composable
internal fun TvRaster(
    client: ApiClient,
    kind: String,
    wahl: TvRasterWahl,
    onWahl: (TvRasterWahl) -> Unit,
    onOpen: (Item) -> Unit,
    fest: Boolean = false,
    // Was zuletzt geladen war, je Wahl — vom Gerüst gehalten, damit das
    // Raster nach einem Titel nicht leer neu lädt und oben steht.
    zwischenspeicher: MutableMap<String, Seite>? = null,
) {
    val genre = wahl.genre
    val cacheSchluessel = "$kind|$genre|${wahl.sortierung.name}|${wahl.buchstabe}|$fest"
    var seite by remember(kind, wahl) { mutableStateOf(zwischenspeicher?.get(cacheSchluessel)) }
    var genres by remember(kind) { mutableStateOf<List<String>>(emptyList()) }
    var buchstaben by remember(kind, genre) { mutableStateOf(Buchstaben(emptyMap())) }
    var fehler by remember(kind, wahl) { mutableStateOf<String?>(null) }
    var mehrLaedt by remember(kind, wahl) { mutableStateOf(false) }
    val raster = rememberLazyGridState()
    val scope = rememberCoroutineScope()
    val buchstabe = wahl.buchstabe.takeIf { wahl.sortierung.mitLeiste && !fest }

    var versuch by remember(kind, wahl) { mutableStateOf(0) }
    LaunchedEffect(kind, wahl, versuch) {
        seite?.let { schon -> if (schon.genres.isNotEmpty()) genres = schon.genres; return@LaunchedEffect }
        try {
            val s = withContext(Dispatchers.IO) {
                client.seite(kind, genre, sort = wahl.sortierung.schluessel, buchstabe = buchstabe)
            }
            seite = s
            zwischenspeicher?.put(cacheSchluessel, s)
            if (s.genres.isNotEmpty()) genres = s.genres
            fehler = null
        } catch (e: LumenException) {
            fehler = e.message
        }
    }
    // Die Leiste ist Beiwerk: fehlt sie, bleibt das Raster trotzdem.
    LaunchedEffect(kind, genre, fest) {
        if (fest) return@LaunchedEffect
        buchstaben = withContext(Dispatchers.IO) {
            oderNull { client.buchstaben(kind, genre) }
        } ?: Buchstaben(emptyMap())
    }

    fun mehr() {
        val s = seite ?: return
        if (!s.weiter || mehrLaedt) return
        mehrLaedt = true
        scope.launch {
            try {
                val n = withContext(Dispatchers.IO) {
                    client.seite(kind, genre, sort = wahl.sortierung.schluessel,
                                 offset = s.naechsterOffset, buchstabe = buchstabe)
                }
                if (seite === s) {
                    val zusammen = s.mit(n)
                    seite = zusammen
                    zwischenspeicher?.put(cacheSchluessel, zusammen)
                }
            } catch (e: LumenException) {
                fehler = e.message
            }
            mehrLaedt = false
        }
    }

    // Steht der Fokus nah am Ende, kommt die nächste Seite.
    val amEnde by remember {
        derivedStateOf {
            val info = raster.layoutInfo
            val letzter = info.visibleItemsInfo.lastOrNull()?.index ?: 0
            info.totalItemsCount > 0 && letzter >= info.totalItemsCount - 10
        }
    }
    LaunchedEffect(amEnde, seite) { if (amEnde) mehr() }

    val titel = if (fest) (genre ?: "") else if (kind == "movie") tx(R.string.filme) else tx(R.string.serien)
    TvGitter(raster) {
        item(span = { GridItemSpan(maxLineSpan) }, key = "lumen:kopf") {
            Column(Modifier.padding(bottom = 8.dp)) {
                TvKopf(titel, seite?.zeile(if (fest) null else genre))
                if (!fest && genres.isNotEmpty()) {
                    Spacer(Modifier.height(16.dp))
                    LazyRow(horizontalArrangement = Arrangement.spacedBy(10.dp),
                            contentPadding = PaddingValues(vertical = 6.dp, horizontal = 2.dp)) {
                        // Ein anderes Genre: der Buchstabe gilt dort womöglich nicht.
                        item(key = "lumen:alle") {
                            TvWahlPille(tx(R.string.alle), genre == null) { onWahl(wahl.copy(genre = null, buchstabe = null)) }
                        }
                        items(genres, key = { it }) { g ->
                            TvWahlPille(g, genre == g) { onWahl(wahl.copy(genre = g, buchstabe = null)) }
                        }
                    }
                }
                if (!fest) {
                    Spacer(Modifier.height(6.dp))
                    LazyRow(horizontalArrangement = Arrangement.spacedBy(10.dp),
                            contentPadding = PaddingValues(vertical = 6.dp, horizontal = 2.dp)) {
                        items(Sortierung.entries, key = { it.schluessel }) { so ->
                            TvWahlPille(so.titel(kind), wahl.sortierung == so) {
                                onWahl(wahl.copy(sortierung = so, buchstabe = null))
                            }
                        }
                    }
                }
                if (!fest && wahl.sortierung.mitLeiste) {
                    Spacer(Modifier.height(4.dp))
                    TvBuchstabenLeiste(buchstaben, wahl.buchstabe) { onWahl(wahl.copy(buchstabe = it)) }
                }
                fehler?.let {
                    Spacer(Modifier.height(10.dp))
                    Text(it, color = Tv.bad, fontSize = 17.sp)
                    if (seite == null) {
                        Spacer(Modifier.height(14.dp))
                        TvButton(tx(R.string.nochmal_versuchen)) { fehler = null; versuch++ }
                    }
                }
            }
        }
        val s = seite
        if (s == null && fehler == null) {
            item(span = { GridItemSpan(maxLineSpan) }) { TvLadeZeile() }
        } else if (s != null && s.items.isEmpty()) {
            item(span = { GridItemSpan(maxLineSpan) }) {
                Text(if (buchstabe != null) tx(R.string.unter_diesem_buchstaben_steht_nichts) else tx(R.string.hier_ist_noch_nichts),
                     color = Tv.label2, fontSize = 20.sp)
            }
        } else if (s != null) {
            items(s.items, key = { it.id }) { t ->
                TvCard(client, t, onFocus = {}, onClick = { onOpen(t) }, breite = RASTER_KARTE)
            }
            if (s.weiter) item(span = { GridItemSpan(maxLineSpan) }, key = "lumen:mehr") { TvLadeZeile() }
        }
    }
}

/**
 * „Alle“, „#“ und A bis Z als eine Zeile, die man mit links und rechts
 * abfährt. Buchstaben ohne Titel stehen grau da und lassen sich nicht
 * fokussieren — der Fokus springt über sie hinweg, statt ins Leere zu führen.
 */
@Composable
private fun TvBuchstabenLeiste(buchstaben: Buchstaben, gewaehlt: String?, onWahl: (String?) -> Unit) {
    LazyRow(horizontalArrangement = Arrangement.spacedBy(4.dp),
            contentPadding = PaddingValues(vertical = 6.dp, horizontal = 2.dp)) {
        item(key = "lumen:alle") { TvBuchstabe(tx(R.string.alle), gewaehlt == null, true) { onWahl(null) } }
        items(Buchstaben.ALLE, key = { it }) { b ->
            TvBuchstabe(b, gewaehlt == b, buchstaben.waehlbar(b)) { onWahl(b) }
        }
    }
}

@Composable
private fun TvBuchstabe(text: String, gewaehlt: Boolean, waehlbar: Boolean, onClick: () -> Unit) {
    var fokussiert by remember { mutableStateOf(false) }
    Box(
        Modifier
            .onFocusChanged { fokussiert = it.isFocused }
            .clickable(enabled = waehlbar, role = Role.Button, onClick = onClick)
            .heightIn(min = 40.dp).widthIn(min = 40.dp)
            .clip(CircleShape)
            .background(when {
                fokussiert -> Color.White
                gewaehlt -> Color(0x40FFFFFF)
                else -> Color.Transparent
            })
            .padding(horizontal = 10.dp),
        contentAlignment = Alignment.Center,
    ) {
        Text(text, fontSize = 17.sp,
             fontWeight = if (gewaehlt) FontWeight.SemiBold else FontWeight.Normal,
             color = when {
                 fokussiert -> Color.Black
                 waehlbar -> Color.White
                 else -> Color(0x33FFFFFF)
             })
    }
}

/** Das Raster selbst: Karten nebeneinander, Platz für die Vergrösserung im Fokus. */
@Composable
internal fun TvGitter(
    zustand: androidx.compose.foundation.lazy.grid.LazyGridState = rememberLazyGridState(),
    breite: Dp = RASTER_KARTE,
    inhalt: androidx.compose.foundation.lazy.grid.LazyGridScope.() -> Unit,
) {
    LazyVerticalGrid(
        columns = GridCells.Adaptive(breite + 24.dp),
        state = zustand,
        contentPadding = PaddingValues(start = 56.dp, end = 56.dp, top = 16.dp, bottom = 48.dp),
        horizontalArrangement = Arrangement.spacedBy(12.dp),
        verticalArrangement = Arrangement.spacedBy(26.dp),
        modifier = Modifier.fillMaxSize(),
        content = inhalt,
    )
}

@Composable
internal fun TvLadeZeile() {
    Box(Modifier.fillMaxWidth().padding(24.dp), Alignment.Center) {
        CircularProgressIndicator(color = Tv.label2)
    }
}

// ---------------------------------------------------------------- Merkliste

/**
 * Die Merkliste — und daneben „Gesehen": was ich zu Ende geschaut habe,
 * zuletzt Beendetes zuerst. Wie im Browser und am Telefon. Nur ich sehe beides.
 */
@Composable
internal fun TvMerkliste(client: ApiClient, gesehen: Boolean, onGesehen: (Boolean) -> Unit,
                         onOpen: (Item) -> Unit, onFilme: () -> Unit) {
    val liste = if (gesehen) "gesehen" else "merkliste"
    var titel by remember(liste) { mutableStateOf<List<Item>?>(null) }
    var fehler by remember(liste) { mutableStateOf<String?>(null) }
    var versuch by remember(liste) { mutableStateOf(0) }

    LaunchedEffect(liste, versuch) {
        try {
            // Der Server filtert und ordnet.
            titel = withContext(Dispatchers.IO) { client.listeGanz(liste) }.items
            fehler = null
        } catch (e: LumenException) {
            fehler = e.message
        }
    }

    TvGitter {
        item(span = { GridItemSpan(maxLineSpan) }, key = "lumen:kopf") {
            Column(Modifier.padding(bottom = 8.dp)) {
                TvKopf(if (gesehen) tx(R.string.gesehen) else tx(R.string.merkliste),
                       if (gesehen) tx(R.string.fertig_geschaut_zuletzt_beendetes_zuerst)
                       else tx(R.string.nur_du_siehst_diese_liste_2))
                Spacer(Modifier.height(14.dp))
                Row(horizontalArrangement = Arrangement.spacedBy(10.dp),
                    modifier = Modifier.padding(vertical = 6.dp, horizontal = 2.dp)) {
                    TvWahlPille(tx(R.string.merkliste), !gesehen) { onGesehen(false) }
                    TvWahlPille(tx(R.string.gesehen), gesehen) { onGesehen(true) }
                }
                fehler?.let {
                    Text(it, color = Tv.bad, fontSize = 17.sp)
                    if (titel == null) {
                        Spacer(Modifier.height(14.dp))
                        TvButton(tx(R.string.nochmal_versuchen)) { fehler = null; versuch++ }
                    }
                }
            }
        }
        val t = titel
        when {
            t == null && fehler == null -> item(span = { GridItemSpan(maxLineSpan) }) { TvLadeZeile() }
            t != null && t.isEmpty() -> item(span = { GridItemSpan(maxLineSpan) }) {
                Column {
                    if (gesehen) {
                        Text(tx(R.string.noch_nichts_zu_ende_geschaut), color = Tv.label2, fontSize = 20.sp)
                    } else {
                        Text(tx(R.string.noch_nichts_gemerkt_auf_einer),
                             color = Tv.label2, fontSize = 20.sp)
                        Spacer(Modifier.height(20.dp))
                        TvButton(tx(R.string.filme_ansehen)) { onFilme() }
                    }
                }
            }
            t != null -> items(t, key = { it.id }) { x ->
                TvCard(client, x, onFocus = {}, onClick = { onOpen(x) }, breite = RASTER_KARTE)
            }
        }
    }
}

// ---------------------------------------------------------------- Favoriten

/**
 * Die Favoriten: was mit dem Herz markiert ist, Filme und
 * Serien getrennt wie im Browser. Nur ich sehe sie.
 */
@Composable
internal fun TvFavoriten(client: ApiClient, serien: Boolean, onSerien: (Boolean) -> Unit,
                         onOpen: (Item) -> Unit) {
    val kind = if (serien) "series" else "movie"
    var titel by remember(kind) { mutableStateOf<List<Item>?>(null) }
    var fehler by remember(kind) { mutableStateOf<String?>(null) }

    LaunchedEffect(kind) {
        try {
            titel = withContext(Dispatchers.IO) { client.listeGanz("favoriten", kind) }.items
            fehler = null
        } catch (e: LumenException) {
            fehler = e.message
        }
    }

    TvGitter {
        item(span = { GridItemSpan(maxLineSpan) }, key = "lumen:kopf") {
            Column(Modifier.padding(bottom = 8.dp)) {
                TvKopf(tx(R.string.favoriten), tx(R.string.nur_du_siehst_deine_favoriten))
                Spacer(Modifier.height(14.dp))
                Row(horizontalArrangement = Arrangement.spacedBy(10.dp),
                    modifier = Modifier.padding(vertical = 6.dp, horizontal = 2.dp)) {
                    TvWahlPille(tx(R.string.filme), !serien) { onSerien(false) }
                    TvWahlPille(tx(R.string.serien), serien) { onSerien(true) }
                }
                fehler?.let { Text(it, color = Tv.bad, fontSize = 17.sp) }
            }
        }
        val t = titel
        when {
            t == null && fehler == null -> item(span = { GridItemSpan(maxLineSpan) }) { TvLadeZeile() }
            t != null && t.isEmpty() -> item(span = { GridItemSpan(maxLineSpan) }) {
                Text(if (serien) tx(R.string.noch_keine_lieblingsserien_auf_einer)
                     else tx(R.string.noch_keine_lieblingsfilme_auf_einer),
                     color = Tv.label2, fontSize = 20.sp)
            }
            t != null -> items(t, key = { it.id }) { x ->
                TvCard(client, x, onFocus = {}, onClick = { onOpen(x) }, breite = RASTER_KARTE)
            }
        }
    }
}

// ---------------------------------------------------------------- Filmreihen

/** Alle Filmreihen, die Lumen zusammengestellt hat. */
@Composable
internal fun TvFilmreihen(client: ApiClient, onOpen: (Reihe) -> Unit) {
    var reihen by remember { mutableStateOf<List<Reihe>?>(null) }
    var fehler by remember { mutableStateOf<String?>(null) }

    LaunchedEffect(Unit) {
        try {
            reihen = withContext(Dispatchers.IO) { client.collections() }
        } catch (e: LumenException) {
            fehler = e.message
        }
    }

    TvGitter {
        item(span = { GridItemSpan(maxLineSpan) }) {
            val r = reihen
            Column(Modifier.padding(bottom = 8.dp)) {
                TvKopf(tx(R.string.filmreihen), if (r.isNullOrEmpty()) null
                    else tx(R.string.reihen_zeile, Texte.n(TxN.REIHEN_ANZAHL, r.size)))
                fehler?.let { Text(it, color = Tv.bad, fontSize = 17.sp) }
            }
        }
        val r = reihen
        when {
            r == null && fehler == null -> item(span = { GridItemSpan(maxLineSpan) }) { TvLadeZeile() }
            r != null && r.isEmpty() -> item(span = { GridItemSpan(maxLineSpan) }) {
                Text(tx(R.string.noch_keine_reihen_lumen_erkennt),
                     color = Tv.label2, fontSize = 20.sp)
            }
            r != null -> items(r, key = { it.id }) { reihe ->
                TvKachel(client.reiheBildUrl(reihe), reihe.name, reihe.anzahlText,
                         breite = RASTER_KARTE) { onOpen(reihe) }
            }
        }
    }
}

// ---------------------------------------------------------------- Listen

/** Alle Listen: die eigenen, dann die im Haushalt geteilten — und „Neue Liste". */
@Composable
internal fun TvListen(client: ApiClient, stand: Int, onOpen: (String) -> Unit, onGeaendert: () -> Unit) {
    var listen by remember { mutableStateOf<List<Liste>?>(null) }
    var fehler by remember { mutableStateOf<String?>(null) }
    var neu by remember { mutableStateOf(false) }
    val scope = rememberCoroutineScope()

    LaunchedEffect(stand) {
        try {
            listen = withContext(Dispatchers.IO) { client.playlists() }.sortedBy { !it.meine }
        } catch (e: LumenException) {
            fehler = e.message
        }
    }

    if (neu) {
        TvNameDialog(tx(R.string.neue_liste), "", tx(R.string.anlegen), onDismiss = { neu = false }) { name ->
            neu = false
            scope.launch {
                try {
                    val angelegt = withContext(Dispatchers.IO) { client.createPlaylist(name) }
                    onGeaendert()
                    onOpen(angelegt.id)
                } catch (e: LumenException) {
                    fehler = e.message
                }
            }
        }
    }

    TvGitter {
        item(span = { GridItemSpan(maxLineSpan) }) {
            Column(Modifier.padding(bottom = 8.dp)) {
                TvKopf(tx(R.string.listen), tx(R.string.eigene_zusammenstellungen_fuer_dich_oder))
                Spacer(Modifier.height(6.dp))
                Text(tx(R.string.eine_liste_sieht_nur_wer),
                     color = Tv.label3, fontSize = 15.sp)
                fehler?.let { Text(it, color = Tv.bad, fontSize = 17.sp) }
            }
        }
        item(key = "lumen:neue-liste") {
            TvKachel(null, tx(R.string.neue_liste), tx(R.string.anlegen), symbol = "+", breite = RASTER_KARTE) { neu = true }
        }
        val l = listen
        if (l == null && fehler == null) {
            item(span = { GridItemSpan(maxLineSpan) }) { TvLadeZeile() }
        } else if (l != null) {
            items(l, key = { it.id }) { liste ->
                TvKachel(
                    liste.vorschau.firstOrNull()?.let { client.posterUrl(it) }, liste.name,
                    if (liste.meine) liste.anzahlText else tx(R.string.x_von_x, liste.anzahlText, liste.von.name),
                    breite = RASTER_KARTE) { onOpen(liste.id) }
            }
        }
    }
}

// ---------------------------------------------------------------- Person

/**
 * Die Personenseite: Foto, Lebensdaten, Biografie (eingeklappt)
 * und Quellen — darunter, worin die Person in der Mediathek mitspielt.
 * Kennt der Server die Person nicht, bleiben nur die Titel. Die Quellen
 * stehen als Text da: einen Browser für Links gibt es am Fernseher nicht.
 */
@Composable
internal fun TvPersonSeite(client: ApiClient, name: String, onOpen: (Item) -> Unit) {
    var titel by remember(name) { mutableStateOf<List<Item>?>(null) }
    var info by remember(name) { mutableStateOf<PersonInfo?>(null) }
    var fehler by remember(name) { mutableStateOf<String?>(null) }

    LaunchedEffect(name) {
        try {
            titel = withContext(Dispatchers.IO) { client.items(person = name, limit = 200) }
        } catch (e: LumenException) {
            fehler = e.message
        }
    }
    // Die Angaben sind Beiwerk: ohne sie steht die Seite trotzdem.
    LaunchedEffect(name) {
        info = withContext(Dispatchers.IO) { oderNull { client.person(name) } }
    }

    TvGitter {
        item(span = { GridItemSpan(maxLineSpan) }, key = "lumen:kopf") {
            val t = titel
            Column(Modifier.padding(bottom = 8.dp)) {
                info?.let { p -> TvPersonKopf(client, p) }
                TvKopf(name, t?.let { txn(R.plurals.titel_in_mediathek, it.size) })
                fehler?.let { Text(it, color = Tv.bad, fontSize = 17.sp) }
            }
        }
        val t = titel
        when {
            t == null && fehler == null -> item(span = { GridItemSpan(maxLineSpan) }) { TvLadeZeile() }
            t != null && t.isEmpty() -> item(span = { GridItemSpan(maxLineSpan) }) {
                Text(tx(R.string.nichts_dabei_was_du_sehen), color = Tv.label2, fontSize = 20.sp)
            }
            t != null -> items(t, key = { it.id }) { x ->
                TvCard(client, x, onFocus = {}, onClick = { onOpen(x) }, breite = RASTER_KARTE)
            }
        }
    }
}

/** Foto links, daneben Lebensdaten, Biografie mit „Mehr lesen" und die Quellen. */
@Composable
private fun TvPersonKopf(client: ApiClient, p: PersonInfo) {
    var offen by remember(p.name) { mutableStateOf(false) }
    var zuLang by remember(p.name) { mutableStateOf(false) }
    Row(Modifier.padding(bottom = 18.dp), verticalAlignment = Alignment.Top) {
        client.personBildUrl(p)?.let { url ->
            coil.compose.AsyncImage(model = url, contentDescription = null,
                contentScale = androidx.compose.ui.layout.ContentScale.Crop,
                modifier = Modifier.width(170.dp).height(226.dp)
                    .clip(RoundedCornerShape(16.dp)).background(Color(0xFF2A2A31)))
            Spacer(Modifier.width(28.dp))
        }
        Column(Modifier.widthIn(max = 980.dp)) {
            p.lebensZeile?.let { Text(it, color = Tv.label2, fontSize = 18.sp) }
            p.bio?.let { bio ->
                Spacer(Modifier.height(10.dp))
                Text(bio, color = Tv.label2, fontSize = 17.sp, lineHeight = 25.sp,
                     maxLines = if (offen) Int.MAX_VALUE else 6,
                     overflow = androidx.compose.ui.text.style.TextOverflow.Ellipsis,
                     onTextLayout = { if (!offen) zuLang = it.hasVisualOverflow })
                if (zuLang || offen) {
                    Spacer(Modifier.height(10.dp))
                    TvButton(if (offen) tx(R.string.weniger) else tx(R.string.mehr_lesen)) { offen = !offen }
                }
            }
            val unten = listOfNotNull(p.quellenZeile, PersonInfo.NUR_ENGLISCH.takeIf { p.nurEnglisch })
            if (unten.isNotEmpty()) {
                Spacer(Modifier.height(10.dp))
                Text(unten.joinToString(" · "), color = Tv.label3, fontSize = 15.sp)
            }
        }
    }
}

/** Eine Person als runde Kachel: Bild oder Anfangsbuchstabe, Name, darunter Rolle oder Anzahl. */
@Composable
internal fun TvPersonKachel(client: ApiClient, name: String, bild: String?, zeile: String?,
                            onClick: () -> Unit) {
    var fokussiert by remember { mutableStateOf(false) }
    Column(
        Modifier
            .width(130.dp)
            .onFocusChanged { fokussiert = it.isFocused }
            .clickable(role = Role.Button, onClick = onClick),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Box(
            Modifier.size(110.dp).clip(CircleShape)
                .background(if (fokussiert) Color.White else Color(0xFF2A2A31)),
            contentAlignment = Alignment.Center,
        ) {
            Text(name.take(1).uppercase(), fontSize = 38.sp,
                 color = if (fokussiert) Color.Black else Tv.label2)
            client.bildUrl(bild)?.let { url ->
                coil.compose.AsyncImage(model = url, contentDescription = null,
                    contentScale = androidx.compose.ui.layout.ContentScale.Crop,
                    modifier = Modifier.fillMaxSize().padding(if (fokussiert) 4.dp else 0.dp).clip(CircleShape))
            }
        }
        Spacer(Modifier.height(8.dp))
        Text(name, fontSize = 15.sp, maxLines = 1,
             overflow = androidx.compose.ui.text.style.TextOverflow.Ellipsis,
             fontWeight = if (fokussiert) FontWeight.SemiBold else FontWeight.Normal)
        zeile?.takeIf { it.isNotBlank() }?.let {
            Text(it, fontSize = 13.sp, color = Tv.label2, maxLines = 1,
                 overflow = androidx.compose.ui.text.style.TextOverflow.Ellipsis)
        }
    }
}
