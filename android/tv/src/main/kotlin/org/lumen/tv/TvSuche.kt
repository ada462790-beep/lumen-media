// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package org.lumen.tv

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.focusable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.focus.onFocusChanged
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import org.lumen.core.*

/**
 * Die Suche am Fernseher — dieselbe wie im Browser: der Server deutet die
 * Eingabe („Komödie 90er", „Filme mit Tom Hanks"), darüber stehen die
 * passenden Personen und Genres, dann die Treffer. Findet sich nichts,
 * lässt sich der Titel wünschen.
 *
 * Getippt wird mit der Bildschirmtastatur des Fernsehers; gesucht wird,
 * sobald das Tippen kurz ruht.
 */
@Composable
internal fun TvSuche(
    client: ApiClient,
    eingabe: String,
    onEingabe: (String) -> Unit,
    onOpen: (Item) -> Unit,
    onPerson: (String) -> Unit,
    onGenre: (String) -> Unit,
    onReihe: (Reihe) -> Unit = {},
    onSammlung: (Sammlung) -> Unit = {},
) {
    var ergebnis by remember { mutableStateOf<Suchergebnis?>(null) }
    var sucht by remember { mutableStateOf(false) }
    var fehler by remember { mutableStateOf<String?>(null) }
    var gewuenscht by remember { mutableStateOf<String?>(null) }
    var rueckmeldung by remember { mutableStateOf<String?>(null) }
    val feld = remember { FocusRequester() }
    val scope = rememberCoroutineScope()

    LaunchedEffect(Unit) {
        delay(150)
        runCatching { feld.requestFocus() }
    }

    // Erst suchen, wenn das Tippen kurz ruht — sonst eine Anfrage je Taste.
    LaunchedEffect(eingabe) {
        val q = eingabe.trim()
        if (q.isEmpty()) { ergebnis = null; sucht = false; return@LaunchedEffect }
        delay(300)
        sucht = true
        try {
            ergebnis = withContext(Dispatchers.IO) { client.search(q) }
            fehler = null
        } catch (e: LumenException) {
            fehler = e.message
        }
        sucht = false
    }

    val q = eingabe.trim()
    LazyColumn(contentPadding = PaddingValues(start = 56.dp, end = 56.dp, top = 16.dp, bottom = 48.dp)) {
        item(key = "lumen:feld") {
            Column(Modifier.padding(bottom = 24.dp)) {
                OutlinedTextField(
                    value = eingabe,
                    onValueChange = { onEingabe(it); gewuenscht = null },
                    singleLine = true,
                    placeholder = { Text(tx(R.string.titel_schauspieler_genre_jahr)) },
                    textStyle = TextStyle(fontSize = 22.sp),
                    modifier = Modifier.width(720.dp).focusRequester(feld))
                Spacer(Modifier.height(10.dp))
                when {
                    q.isEmpty() -> Text(tx(R.string.tippe_los_auch_komoedie_2019),
                                        color = Tv.label2, fontSize = 18.sp)
                    sucht && ergebnis == null -> Text(tx(R.string.sucht), color = Tv.label2, fontSize = 18.sp)
                    fehler != null -> Text(fehler!!, color = Tv.bad, fontSize = 18.sp)
                }
            }
        }
        val e = ergebnis
        if (q.isNotEmpty() && e != null) {
            // Filmreihen und Sammlungen, deren Name passt — „Harry Potter"
            // findet die Reihe, nicht nur die einzelnen Filme.
            if (e.hatReihenOderSammlungen) {
                item(key = "lumen:reihen") {
                    Column(Modifier.padding(bottom = 28.dp)) {
                        Text(tx(R.string.filmreihen_und_sammlungen), fontSize = 22.sp, fontWeight = FontWeight.SemiBold)
                        Spacer(Modifier.height(12.dp))
                        LazyRow(horizontalArrangement = Arrangement.spacedBy(30.dp),
                                contentPadding = PaddingValues(vertical = 14.dp, horizontal = 4.dp)) {
                            items(e.reihen, key = { "reihe:" + it.id }) { r ->
                                TvKachel(client.reiheBildUrl(r), r.name, r.anzahlText, breite = 160.dp) { onReihe(r) }
                            }
                            items(e.sammlungen, key = { "sammlung:" + it.id }) { x ->
                                TvKachel(client.sammlungBildUrl(x), x.name, x.anzahlText, breite = 160.dp) { onSammlung(x) }
                            }
                        }
                    }
                }
            }
            if (e.personen.isNotEmpty()) {
                item(key = "lumen:personen") {
                    Column(Modifier.padding(bottom = 28.dp)) {
                        Text(tx(R.string.personen), fontSize = 22.sp, fontWeight = FontWeight.SemiBold)
                        Spacer(Modifier.height(12.dp))
                        LazyRow(horizontalArrangement = Arrangement.spacedBy(20.dp),
                                contentPadding = PaddingValues(vertical = 6.dp, horizontal = 2.dp)) {
                            items(e.personen, key = { it.name }) { p ->
                                TvPersonKachel(client, p.name, p.bild, p.anzahlText) { onPerson(p.name) }
                            }
                        }
                    }
                }
            }
            if (e.genres.isNotEmpty()) {
                item(key = "lumen:genres") {
                    LazyRow(horizontalArrangement = Arrangement.spacedBy(10.dp),
                            contentPadding = PaddingValues(vertical = 6.dp, horizontal = 2.dp),
                            modifier = Modifier.padding(bottom = 24.dp)) {
                        items(e.genres, key = { it }) { g -> TvWahlPille(g, false) { onGenre(g) } }
                    }
                }
            }
            if (e.items.isNotEmpty()) {
                item(key = "lumen:treffer") {
                    Text(e.trefferZeile, color = Tv.label2, fontSize = 18.sp,
                         modifier = Modifier.padding(bottom = 12.dp))
                }
                // Je fünf nebeneinander — ein Raster in einer Liste geht in
                // Compose nicht, Zeilen aus Karten schon.
                val zeilen = e.items.chunked(5)
                items(zeilen.size, key = { "zeile:" + zeilen[it].first().id }) { i ->
                    Row(horizontalArrangement = Arrangement.spacedBy(30.dp),
                        modifier = Modifier.padding(vertical = 14.dp, horizontal = 4.dp)) {
                        zeilen[i].forEach { t ->
                            TvCard(client, t, onFocus = {}, onClick = { onOpen(t) }, breite = 160.dp)
                        }
                    }
                }
            } else if (!e.leer) {
                item(key = "lumen:gedeutet") {
                    val d = e.deutung
                    Text(if (d != null && d.gedeutet) tx(R.string.kein_titel_passt_genau_gesucht, d.beschreibung)
                         else tx(R.string.kein_titel_passt_genau_aber),
                         color = Tv.label2, fontSize = 20.sp)
                }
            } else {
                // Nichts gefunden: dann lässt sich der Titel wünschen.
                item(key = "lumen:wunsch") {
                    Column {
                        Text(tx(R.string.nichts_gefunden_fuer_x, q), fontSize = 24.sp)
                        Spacer(Modifier.height(8.dp))
                        Text(tx(R.string.du_kannst_dir_den_titel),
                             color = Tv.label2, fontSize = 18.sp)
                        Spacer(Modifier.height(20.dp))
                        val meldung = rueckmeldung
                        if (gewuenscht == q && meldung != null) {
                            // Neu, schon eigen oder angeschlossen — wie im Browser.
                            Text(meldung, color = Tv.good, fontSize = 18.sp)
                        } else {
                            TvButton(tx(R.string.x_wuenschen, q)) {
                                scope.launch {
                                    try {
                                        val w = withContext(Dispatchers.IO) { client.wish(q) }
                                        rueckmeldung = w.rueckmeldung
                                        gewuenscht = q
                                    } catch (ex: LumenException) {
                                        fehler = ex.message
                                    }
                                }
                            }
                        }
                    }
                }
            }
        }
    }
}

// ---------------------------------------------------------------- Wünsche

/**
 * Die Wünsche: was ich mir gewünscht habe, und wie es steht — dazu ein
 * Feld, um mir etwas zu wünschen. Annehmen und Ablehnen gehören zur
 * Verwaltung, und die gibt es nur im Browser.
 */
@Composable
internal fun TvWuensche(client: ApiClient) {
    var wuensche by remember { mutableStateOf<List<Wish>?>(null) }
    var eingabe by remember { mutableStateOf("") }
    var hinweis by remember { mutableStateOf<String?>(null) }
    var fehler by remember { mutableStateOf<String?>(null) }
    var stand by remember { mutableStateOf(0) }
    // Löschen oder austreten: erst nach einer Rückfrage (Fokus auf „Abbrechen").
    var weg by remember { mutableStateOf<Wish?>(null) }
    val scope = rememberCoroutineScope()

    LaunchedEffect(stand) {
        try {
            wuensche = withContext(Dispatchers.IO) { client.wishes() }
        } catch (e: LumenException) {
            fehler = e.message
        }
    }

    fun wuenschen() {
        val t = eingabe.trim()
        if (t.isEmpty()) return
        scope.launch {
            try {
                val w = withContext(Dispatchers.IO) { client.wish(t) }
                eingabe = ""
                hinweis = w.rueckmeldung
                fehler = null
                stand++
            } catch (e: LumenException) {
                fehler = e.message
            }
        }
    }

    LazyColumn(contentPadding = PaddingValues(start = 56.dp, end = 56.dp, top = 16.dp, bottom = 48.dp)) {
        item {
            Column(Modifier.padding(bottom = 24.dp)) {
                TvKopf(tx(R.string.wuensche), tx(R.string.was_du_dir_gewuenscht_hast))
                Spacer(Modifier.height(20.dp))
                Row(verticalAlignment = Alignment.CenterVertically) {
                    OutlinedTextField(
                        value = eingabe,
                        onValueChange = { eingabe = it; hinweis = null },
                        singleLine = true,
                        placeholder = { Text(tx(R.string.titel_wuenschen)) },
                        textStyle = TextStyle(fontSize = 20.sp),
                        modifier = Modifier.width(560.dp))
                    Spacer(Modifier.width(18.dp))
                    TvButton(tx(R.string.wuenschen)) { wuenschen() }
                }
                Spacer(Modifier.height(8.dp))
                Text(tx(R.string.der_besitzer_sieht_den_titel),
                     color = Tv.label3, fontSize = 15.sp)
                hinweis?.let { Text(it, color = Tv.good, fontSize = 17.sp) }
                fehler?.let { Text(it, color = Tv.bad, fontSize = 17.sp) }
            }
        }
        val liste = wuensche
        when {
            liste == null -> item { TvLadeZeile() }
            liste.isEmpty() -> item { Text(tx(R.string.noch_keine_wuensche), color = Tv.label2, fontSize = 20.sp) }
            else -> items(liste, key = { it.id }) { w -> TvWunschZeile(w) { weg = w } }
        }
    }

    weg?.let { w ->
        val austreten = w.wegArt == WunschWeg.AUSTRETEN
        TvFrage(
            titel = tx(if (austreten) R.string.nicht_mehr_wuenschen_frage else R.string.wunsch_loeschen_frage),
            text = if (austreten) tx(R.string.wunsch_austreten_text, w.title) else tx(R.string.wunsch_loeschen_text, w.title),
            ja = tx(if (austreten) R.string.nicht_mehr_wuenschen else R.string.loeschen),
            onNein = { weg = null },
        ) {
            weg = null
            hinweis = null
            scope.launch {
                try {
                    withContext(Dispatchers.IO) { client.wunschLoeschen(w.id) }
                    fehler = null
                } catch (e: LumenException) {
                    // 409 nicht_offen: Der Satz des Servers sagt, warum.
                    fehler = e.message
                }
                stand++
            }
        }
    }
}

/** Ein Wunsch: Titel, von wem, rechts der Stand. Fokussierbar, damit sich die Liste blättern lässt. */
@Composable
private fun TvWunschZeile(w: Wish, onWeg: () -> Unit) {
    var fokussiert by remember { mutableStateOf(false) }
    val art = w.wegArt
    Row(
        Modifier
            .widthIn(max = 980.dp)
            .fillMaxWidth()
            .onFocusChanged { fokussiert = it.isFocused }
            // Mit OK: den eigenen offenen Wunsch löschen oder einen
            // angeschlossenen verlassen. Sonst nur fokussierbar zum Blättern.
            .then(if (art != null) Modifier.clickable(role = Role.Button, onClick = onWeg) else Modifier.focusable())
            .clip(RoundedCornerShape(14.dp))
            .background(if (fokussiert) Color(0x33FFFFFF) else Color.Transparent)
            .padding(horizontal = 20.dp, vertical = 14.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Column(Modifier.weight(1f)) {
            Text(w.titelZeile, fontSize = 22.sp, maxLines = 1, overflow = TextOverflow.Ellipsis)
            Text(w.vonText, color = Tv.label2, fontSize = 16.sp)
        }
        val (grund, schrift) = when (w.state) {
            "working" -> Color(0x290A84FF) to Color(0xFF4AA3FF)
            "done" -> Color(0x2930D158) to Color(0xFF30D158)
            "declined" -> Color(0x24FF453A) to Color(0xFFFF6961)
            else -> Color(0x3D787880) to Tv.label2
        }
        if (fokussiert && art != null) {
            Text(tx(if (art == WunschWeg.AUSTRETEN) R.string.nicht_mehr_wuenschen else R.string.loeschen),
                 color = Tv.label2, fontSize = 15.sp, modifier = Modifier.padding(end = 14.dp))
        }
        Text(w.stateLabel, color = schrift, fontSize = 15.sp, fontWeight = FontWeight.SemiBold,
             modifier = Modifier.clip(CircleShape).background(grund)
                 .padding(horizontal = 14.dp, vertical = 5.dp))
    }
}
