// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package org.lumen.app

import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.grid.GridCells
import androidx.compose.foundation.lazy.grid.GridItemSpan
import androidx.compose.foundation.lazy.grid.LazyVerticalGrid
import androidx.compose.foundation.lazy.grid.items
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.ArrowBack
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.KeyboardArrowDown
import androidx.compose.material.icons.filled.KeyboardArrowUp
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import org.lumen.core.*

/**
 * Filmreihen und Listen.
 *
 * Reihen stellt der Server zusammen; hier wird nur gezeigt. Listen stellt
 * jede Person selbst zusammen und teilt sie, wenn sie will. Was andere
 * schauen, kommt auf keiner dieser Seiten vor — nur, was jemand bewusst in
 * eine Liste gelegt hat.
 */

// ---------------------------------------------------------------- Filmreihen

@Composable
fun ReihenScreen(
    model: LumenViewModel,
    // Eine eigene Sammlung — sie stehen über den Reihen, wenn es welche gibt.
    onSammlung: (Sammlung) -> Unit = {},
    onOpen: (Reihe) -> Unit,
) {
    var reihen by remember { mutableStateOf<List<Reihe>>(emptyList()) }
    var sammlungen by remember { mutableStateOf<List<Sammlung>>(emptyList()) }
    var loading by remember { mutableStateOf(true) }
    val error by model.error.collectAsStateWithLifecycle()

    LaunchedEffect(Unit) {
        reihen = model.fetch { it.collections() } ?: emptyList()
        loading = false
    }
    // Beiwerk: ohne Sammlungen (älterer Server) bleibt es bei den Reihen.
    LaunchedEffect(Unit) {
        sammlungen = withContext(Dispatchers.IO) { oderNull { model.client.sammlungen() } } ?: emptyList()
    }

    LazyVerticalGrid(
        columns = GridCells.Adaptive(Lumen.posterWidth),
        contentPadding = PaddingValues(16.dp),
        horizontalArrangement = Arrangement.spacedBy(14.dp),
        verticalArrangement = Arrangement.spacedBy(18.dp),
    ) {
        if (sammlungen.isNotEmpty()) {
            item(span = { GridItemSpan(maxLineSpan) }, key = "lumen:sammlungen") {
                Abschnitt(tx(R.string.sammlungen))
            }
            items(sammlungen, key = { "sammlung:" + it.id }) { x -> SammlungKarte(model, x) { onSammlung(x) } }
            item(span = { GridItemSpan(maxLineSpan) }, key = "lumen:reihen") {
                if (reihen.isNotEmpty()) {
                    Abschnitt(tx(R.string.filmreihen))
                }
            }
        }
        item(span = { GridItemSpan(maxLineSpan) }) {
            if (reihen.isNotEmpty()) {
                Text(tx(R.string.reihen_zeile, Texte.n(TxN.REIHEN_ANZAHL, reihen.size)),
                     color = Lumen.label2, fontSize = 14.sp)
            }
        }
        items(reihen, key = { it.id }) { reihe -> ReiheKarte(model, reihe) { onOpen(reihe) } }
    }
    StateOverlay(loading && reihen.isEmpty(), error,
        reihen.isEmpty() && sammlungen.isEmpty() && !loading,
        tx(R.string.noch_keine_reihen_lumen_erkennt))
}

/** Eine eigene Sammlung als Karte: ihr eigenes Bild, sonst das des ersten Titels. */
@Composable
fun SammlungKarte(model: LumenViewModel, sammlung: Sammlung, onClick: () -> Unit) {
    Column(Modifier.width(Lumen.posterWidth).clickableRow(onClick)) {
        Artwork(
            model.client.sammlungBildUrl(sammlung), sammlung.name,
            Modifier.width(Lumen.posterWidth).height(Lumen.posterHeight))
        Spacer(Modifier.height(7.dp))
        Text(sammlung.name, fontSize = 14.sp, maxLines = 1, overflow = TextOverflow.Ellipsis)
        Text(sammlung.anzahlText, fontSize = 12.sp, color = Lumen.label2, maxLines = 1)
    }
}

/**
 * Eine eigene Sammlung: Name, Anzahl, Beschreibung, darunter die Titel in
 * der Reihenfolge, die die Verwaltung gewählt hat. Zusammenstellen geht
 * nur im Browser.
 */
@Composable
fun SammlungScreen(
    model: LumenViewModel,
    sammlungId: String,
    onBack: () -> Unit,
    onOpen: (Item) -> Unit,
) {
    var sammlung by remember(sammlungId) { mutableStateOf<Sammlung?>(null) }
    var loading by remember(sammlungId) { mutableStateOf(true) }
    var reload by remember { mutableStateOf(0) }
    val error by model.error.collectAsStateWithLifecycle()

    LaunchedEffect(sammlungId, reload) {
        sammlung = model.fetch { it.sammlung(sammlungId) }
        loading = false
    }

    val x = sammlung
    if (x == null) {
        Column(Modifier.fillMaxSize()) {
            ZurueckZeile(onBack)
            StateOverlay(loading, error, onRetry = { reload++ })
        }
        return
    }

    LazyVerticalGrid(
        columns = GridCells.Adaptive(Lumen.posterWidth),
        contentPadding = PaddingValues(16.dp),
        horizontalArrangement = Arrangement.spacedBy(14.dp),
        verticalArrangement = Arrangement.spacedBy(18.dp),
    ) {
        item(span = { GridItemSpan(maxLineSpan) }) {
            Column {
                ZurueckZeile(onBack)
                Text(x.name, fontSize = 24.sp, fontWeight = FontWeight.Bold)
                Text(x.anzahlText, color = Lumen.label2, fontSize = 14.sp)
                x.beschreibung?.let {
                    Spacer(Modifier.height(10.dp))
                    Text(it, color = Lumen.label, fontSize = 15.sp, lineHeight = 22.sp)
                }
            }
        }
        items(x.items, key = { it.id }) { titel -> PosterCard(model, titel, onOpen) }
    }
}

/** Eine Filmreihe als Karte: ihr eigenes Poster, sonst das des ersten Films. */
@Composable
fun ReiheKarte(model: LumenViewModel, reihe: Reihe, onClick: () -> Unit) {
    Column(Modifier.width(Lumen.posterWidth).clickableRow(onClick)) {
        Artwork(
            model.client.reiheBildUrl(reihe), reihe.name,
            Modifier.width(Lumen.posterWidth).height(Lumen.posterHeight))
        Spacer(Modifier.height(7.dp))
        Text(reihe.name, fontSize = 14.sp, maxLines = 1, overflow = TextOverflow.Ellipsis)
        Text(reihe.anzahlText, fontSize = 12.sp, color = Lumen.label2, maxLines = 1)
    }
}

@Composable
fun ReiheScreen(
    model: LumenViewModel,
    reiheId: String,
    onBack: () -> Unit,
    onOpen: (Item) -> Unit,
    // „Ansehen" oder „Weiter mit …": der erste Film, der noch nicht gesehen ist.
    onPlay: (Item) -> Unit = {},
) {
    var reihe by remember(reiheId) { mutableStateOf<Reihe?>(null) }
    var loading by remember(reiheId) { mutableStateOf(true) }
    var reload by remember { mutableStateOf(0) }
    val error by model.error.collectAsStateWithLifecycle()

    LaunchedEffect(reiheId, reload) {
        reihe = model.fetch { it.collection(reiheId) }
        loading = false
    }

    val r = reihe
    if (r == null) {
        Column(Modifier.fillMaxSize()) {
            ZurueckZeile(onBack)
            StateOverlay(loading, error, onRetry = { reload++ })
        }
        return
    }

    Box(Modifier.fillMaxSize()) {
        LazyVerticalGrid(
            columns = GridCells.Adaptive(Lumen.posterWidth),
            contentPadding = PaddingValues(16.dp),
            horizontalArrangement = Arrangement.spacedBy(14.dp),
            verticalArrangement = Arrangement.spacedBy(18.dp),
        ) {
            item(span = { GridItemSpan(maxLineSpan) }) {
                Column {
                    // Mit eigenem Hintergrund der Reihe wird daraus ein Kopf
                    // wie bei einem Film; ohne bleibt es die schlichte Überschrift.
                    if (r.backdrop != null) {
                        Box {
                            Artwork(model.client.reiheBildUrl(r, breit = true), r.name,
                                    Modifier.fillMaxWidth().aspectRatio(16f / 9f))
                            ZurueckZeile(onBack)
                        }
                        Spacer(Modifier.height(12.dp))
                    } else {
                        ZurueckZeile(onBack)
                    }
                    Text(r.name, fontSize = 24.sp, fontWeight = FontWeight.Bold)
                    Text(r.unterzeile, color = Lumen.label2, fontSize = 14.sp)
                    r.naechster?.let { n ->
                        Spacer(Modifier.height(14.dp))
                        PrimaryButton("▶  " + r.abspielText) { onPlay(n) }
                    }
                    r.overview?.takeIf { it.isNotBlank() }?.let {
                        Spacer(Modifier.height(10.dp))
                        Text(it, color = Lumen.label, fontSize = 15.sp, lineHeight = 22.sp)
                    }
                }
            }
            // In der Reihenfolge, die der Server vorgibt — die der Reihe.
            items(r.items, key = { it.id }) { film -> PosterCard(model, film, onOpen) }
        }
    }
}

// ---------------------------------------------------------------- Listen

@Composable
fun ListenScreen(model: LumenViewModel, onOpen: (String) -> Unit) {
    var listen by remember { mutableStateOf<List<Liste>>(emptyList()) }
    var loading by remember { mutableStateOf(true) }
    var neu by remember { mutableStateOf(false) }
    val error by model.error.collectAsStateWithLifecycle()
    val scope = rememberCoroutineScope()

    LaunchedEffect(Unit) {
        listen = model.fetch { it.playlists() } ?: emptyList()
        loading = false
    }

    val meine = listen.filter { it.meine }
    val geteilte = listen.filter { !it.meine }

    LazyColumn(contentPadding = PaddingValues(16.dp)) {
        item {
            Text(tx(R.string.eigene_zusammenstellungen_fuer_dich_oder),
                 color = Lumen.label2, fontSize = 14.sp)
            Spacer(Modifier.height(12.dp))
            QuietButton(tx(R.string.neue_liste_2)) { neu = true }
            Spacer(Modifier.height(18.dp))
        }
        if (meine.isNotEmpty()) {
            item { Abschnitt(tx(R.string.meine_listen)) }
            items(meine, key = { it.id }) { l -> ListenZeile(model, l) { onOpen(l.id) } }
        }
        if (geteilte.isNotEmpty()) {
            item {
                Spacer(Modifier.height(18.dp))
                Abschnitt(tx(R.string.im_haushalt_geteilt))
            }
            items(geteilte, key = { it.id }) { l -> ListenZeile(model, l) { onOpen(l.id) } }
        }
        item {
            Text(tx(R.string.eine_liste_sieht_nur_wer),
                 color = Lumen.label3, fontSize = 12.sp, modifier = Modifier.padding(top = 26.dp))
        }
    }
    StateOverlay(loading && listen.isEmpty(), error, false)
    if (!loading && listen.isEmpty() && error == null) {
        Box(Modifier.fillMaxSize().padding(32.dp), Alignment.Center) {
            Text(tx(R.string.noch_keine_listen_leg_eine),
                 color = Lumen.label2)
        }
    }

    if (neu) {
        NameDialog(tx(R.string.neue_liste_2), "", tx(R.string.anlegen), onDismiss = { neu = false }) { name ->
            neu = false
            scope.launch {
                // Gleich hinein in die neue Liste — dort geht es weiter.
                val angelegt = model.fetch { it.createPlaylist(name) }
                if (angelegt != null) onOpen(angelegt.id)
            }
        }
    }
}

@Composable
private fun ListenZeile(model: LumenViewModel, liste: Liste, onClick: () -> Unit) {
    Row(
        Modifier.fillMaxWidth().padding(vertical = 6.dp).clickableRow(onClick),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        val erster = liste.vorschau.firstOrNull()
        Artwork(erster?.let { model.client.posterUrl(it) }, liste.name,
                Modifier.width(56.dp).height(84.dp), corner = 8.dp)
        Spacer(Modifier.width(12.dp))
        Column(Modifier.weight(1f)) {
            Text(liste.name, fontSize = 16.sp, maxLines = 1, overflow = TextOverflow.Ellipsis)
            Text(
                buildList {
                    add(liste.anzahlText)
                    if (!liste.meine) add(tx(R.string.von_x, liste.von.name))
                    else if (liste.gemeinsam) add(tx(R.string.liste_gemeinsam_zeile))
                    else if (liste.geteilt) add(tx(R.string.im_haushalt_sichtbar))
                }.joinToString(" · "),
                fontSize = 13.sp, color = Lumen.label2, maxLines = 1)
        }
        if (!liste.meine) Avatar(liste.von.initial, liste.von.avatar, 26.dp,
                                 model.profilbildUrl(liste.von.id, liste.von.avatar))
    }
}

@Composable
fun ListeScreen(
    model: LumenViewModel,
    listeId: String,
    onBack: () -> Unit,
    onOpen: (Item) -> Unit,
    // Name der Liste, alle Einträge und was davon zuerst läuft.
    onPlay: (String, List<Item>, AmStueck.Naechstes) -> Unit,
) {
    var liste by remember(listeId) { mutableStateOf<Liste?>(null) }
    var sucht by remember { mutableStateOf(false) }
    var nichtsSpielbar by remember(listeId) { mutableStateOf(false) }
    val scope = rememberCoroutineScope()
    var loading by remember(listeId) { mutableStateOf(true) }
    var reload by remember { mutableStateOf(0) }
    var umbenennen by remember { mutableStateOf(false) }
    var loeschen by remember { mutableStateOf(false) }
    val error by model.error.collectAsStateWithLifecycle()

    LaunchedEffect(listeId, reload) {
        liste = model.fetch { it.playlist(listeId) }
        loading = false
    }

    val l = liste
    if (l == null) {
        Column(Modifier.fillMaxSize()) {
            ZurueckZeile(onBack)
            StateOverlay(loading, error, onRetry = { reload++ })
        }
        return
    }

    /** Einen Titel um [schritt] verschieben — sofort sichtbar, dann zum Server. */
    fun verschieben(index: Int, schritt: Int) {
        val ziel = index + schritt
        if (ziel < 0 || ziel >= l.items.size) return
        val neu = l.items.toMutableList()
        val titel = neu.removeAt(index)
        neu.add(ziel, titel)
        liste = l.copy(items = neu)
        model.send(onDone = { reload++ }) { it.orderPlaylist(l.id, neu.map { t -> t.id }) }
    }

    // Abspielen spielt die ganze Liste am Stück, von oben nach unten. Eine
    // Serie steht dabei für ihre erste Folge, die noch offen ist. Was das
    // erste Abspielbare ist, weiss erst der Server — darum wird gefragt,
    // bevor der Player aufgeht.
    fun abspielen() {
        if (sucht) return
        sucht = true
        nichtsSpielbar = false
        scope.launch {
            val eintraege = l.items
            // Nur wenn der Server geantwortet hat, heisst null wirklich
            // „nichts abspielbar" — sonst steht der Fehler schon da.
            var beantwortet = false
            val erstes = model.fetch { c ->
                Abspielfolge(c).naechstesInListe(eintraege, 0).also { beantwortet = true }
            }
            sucht = false
            if (erstes != null) onPlay(l.name, eintraege, erstes)
            else if (beantwortet) nichtsSpielbar = true
        }
    }

    LazyColumn(contentPadding = PaddingValues(bottom = 32.dp)) {
        item {
            Column(Modifier.padding(horizontal = 16.dp)) {
                ZurueckZeile(onBack)
                Text(l.name, fontSize = 24.sp, fontWeight = FontWeight.Bold)
                Text(
                    if (l.meine) l.anzahlText else tx(R.string.x_von_x_2, l.anzahlText, l.von.name),
                    color = Lumen.label2, fontSize = 14.sp)
                Spacer(Modifier.height(16.dp))
                Row(horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                    if (l.items.isNotEmpty()) {
                        PrimaryButton(tx(R.string.abspielen), enabled = !sucht) { abspielen() }
                    }
                    if (l.meine) QuietButton(tx(R.string.umbenennen)) { umbenennen = true }
                }
                if (nichtsSpielbar) {
                    Text(tx(R.string.in_dieser_liste_ist_nichts),
                         color = Lumen.label2, fontSize = 13.sp,
                         modifier = Modifier.padding(top = 8.dp))
                }
            }
        }

        // Freigabe und Löschen nur für die Person, die die Liste angelegt hat.
        if (l.meine) {
            item {
                Spacer(Modifier.height(10.dp))
                SettingSwitch(tx(R.string.im_haushalt_zeigen), l.geteilt) { an ->
                    liste = l.copy(geteilt = an, gemeinsam = l.gemeinsam && an)
                    model.send(onDone = { reload++ }) {
                        // Wer die Freigabe zurücknimmt, nimmt auch das
                        // Mitschreiben zurück — sonst bliebe es stehen und
                        // käme beim nächsten Teilen unbemerkt wieder.
                        if (an) it.updatePlaylist(l.id, geteilt = true)
                        else it.updatePlaylist(l.id, geteilt = false, gemeinsam = false)
                    }
                }
                SettingSwitch(tx(R.string.alle_duerfen_mitschreiben), l.gemeinsam) { an ->
                    // Mitschreiben heisst auch: sichtbar. Das regelt der Server.
                    liste = l.copy(gemeinsam = an, geteilt = l.geteilt || an)
                    model.send(onDone = { reload++ }) { it.updatePlaylist(l.id, gemeinsam = an) }
                }
                Text(
                    tx(R.string.geteilt_sehen_die_anderen_im),
                    color = Lumen.label3, fontSize = 12.sp,
                    modifier = Modifier.padding(horizontal = 16.dp, vertical = 6.dp))
            }
        }

        if (l.items.isEmpty()) {
            item {
                Text(
                    if (l.darfAendern) tx(R.string.noch_leer_auf_der_seite)
                    else tx(R.string.diese_liste_ist_noch_leer),
                    color = Lumen.label2,
                    modifier = Modifier.padding(16.dp))
            }
        }

        items(l.items.size, key = { l.items[it].id }) { index ->
            val titel = l.items[index]
            Row(
                Modifier.fillMaxWidth().clickableRow { onOpen(titel) }
                    .padding(horizontal = 16.dp, vertical = 6.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Artwork(model.client.posterUrl(titel), titel.title,
                        Modifier.width(56.dp).height(84.dp), corner = 8.dp)
                Spacer(Modifier.width(12.dp))
                Column(Modifier.weight(1f)) {
                    Text(titel.title, fontSize = 15.sp, maxLines = 1,
                         overflow = TextOverflow.Ellipsis)
                    titel.jahresText?.let {
                        Text(it, fontSize = 13.sp, color = Lumen.label2)
                    }
                    // Wer etwas hineingelegt hat, zählt nur, wo mehrere
                    // mitschreiben — in der eigenen Liste war es ohnehin man selbst.
                    if (l.gemeinsam || !l.meine) {
                        titel.hinzugefuegtVon?.let {
                            Text(tx(R.string.von_x, it), fontSize = 12.sp, color = Lumen.label3)
                        }
                    }
                }
                if (l.darfAendern) {
                    IconButton(onClick = { verschieben(index, -1) }, enabled = index > 0) {
                        Icon(Icons.Filled.KeyboardArrowUp, tx(R.string.nach_oben),
                             tint = if (index > 0) Lumen.label2 else Lumen.label3)
                    }
                    IconButton(onClick = { verschieben(index, 1) },
                               enabled = index < l.items.size - 1) {
                        Icon(Icons.Filled.KeyboardArrowDown, tx(R.string.nach_unten),
                             tint = if (index < l.items.size - 1) Lumen.label2 else Lumen.label3)
                    }
                    IconButton(onClick = {
                        liste = l.copy(items = l.items.filter { it.id != titel.id },
                                       anzahl = (l.anzahl - 1).coerceAtLeast(0))
                        model.send(onDone = { reload++ }) {
                            it.removeFromPlaylist(l.id, titel.id)
                        }
                    }) {
                        Icon(Icons.Filled.Close, tx(R.string.aus_der_liste_nehmen), tint = Lumen.label2)
                    }
                }
            }
        }

        if (l.meine) {
            item {
                Spacer(Modifier.height(22.dp))
                ListItem(
                    headlineContent = { Text(tx(R.string.liste_loeschen), color = Lumen.bad) },
                    colors = ListItemDefaults.colors(containerColor = Lumen.background),
                    modifier = Modifier.clickableRow { loeschen = true })
            }
        }
    }

    if (umbenennen) {
        NameDialog(tx(R.string.liste_umbenennen), l.name, tx(R.string.sichern),
                   onDismiss = { umbenennen = false }) { name ->
            umbenennen = false
            liste = l.copy(name = name)
            model.send(onDone = { reload++ }) { it.updatePlaylist(l.id, name = name) }
        }
    }

    if (loeschen) {
        AlertDialog(
            onDismissRequest = { loeschen = false },
            title = { Text(tx(R.string.x_loeschen, l.name)) },
            text = {
                Text(if (l.geteilt) tx(R.string.die_liste_verschwindet_auch_bei)
                     else tx(R.string.die_filme_selbst_bleiben))
            },
            confirmButton = {
                TextButton(onClick = {
                    loeschen = false
                    model.send(onDone = onBack) { it.deletePlaylist(l.id) }
                }) { Text(tx(R.string.loeschen), color = Lumen.bad) }
            },
            dismissButton = {
                TextButton(onClick = { loeschen = false }) { Text(tx(R.string.abbrechen)) }
            },
            containerColor = Color(0xFF16161A))
    }
}

// ---------------------------------------------------------------- Zur Liste

/**
 * Von der Titelseite aus: in welchen Listen der Titel liegt, mit einem
 * Haken. Ein Tipp legt ihn hinein oder nimmt ihn heraus. Angeboten werden
 * nur Listen, in die diese Person schreiben darf.
 */
@Composable
fun ZurListeDialog(model: LumenViewModel, itemId: String, onDismiss: () -> Unit) {
    var listen by remember(itemId) { mutableStateOf<List<Liste>?>(null) }
    var reload by remember { mutableStateOf(0) }
    var neu by remember { mutableStateOf(false) }

    LaunchedEffect(itemId, reload) {
        listen = (model.fetch { it.playlists(itemId) } ?: emptyList()).filter { it.darfAendern }
    }

    if (neu) {
        NameDialog(tx(R.string.neue_liste_2), "", tx(R.string.anlegen), onDismiss = { neu = false }) { name ->
            neu = false
            model.send(onDone = { reload++ }) {
                val angelegt = it.createPlaylist(name)
                it.addToPlaylist(angelegt.id, itemId)
            }
        }
        return
    }

    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(tx(R.string.zur_liste)) },
        text = {
            val alle = listen
            if (alle == null) {
                Box(Modifier.fillMaxWidth().height(80.dp), Alignment.Center) {
                    CircularProgressIndicator(color = Lumen.label2)
                }
            } else {
                // Eine einfache Spalte statt LazyColumn: im Dialog misst
                // Compose den Inhalt vorab aus, und das kann LazyColumn nicht.
                // Es sind ohnehin nur eine Handvoll Listen.
                Column(Modifier.heightIn(max = 360.dp).verticalScrollable()) {
                    alle.forEach { l ->
                        ListItem(
                            headlineContent = { Text(l.name) },
                            // Ohne Zeile statt mit leerer: sonst wird jede
                            // eigene Liste grundlos doppelt so hoch.
                            supportingContent = if (l.meine) null else {
                                { Text(tx(R.string.von_x, l.von.name), color = Lumen.label3) }
                            },
                            trailingContent = {
                                if (l.enthaelt) Text("✓", color = Lumen.good, fontSize = 18.sp)
                            },
                            colors = ListItemDefaults.colors(containerColor = Color.Transparent),
                            modifier = Modifier.clickableRow {
                                // Gleich umschalten, damit der Haken nicht auf
                                // den Server warten muss; danach neu laden.
                                listen = alle.map {
                                    if (it.id == l.id) it.copy(enthaelt = !l.enthaelt) else it
                                }
                                model.send(onDone = { reload++ }) {
                                    if (l.enthaelt) it.removeFromPlaylist(l.id, itemId)
                                    else it.addToPlaylist(l.id, itemId)
                                }
                            })
                    }
                    ListItem(
                        headlineContent = { Text(tx(R.string.neue_liste), color = Lumen.accent) },
                        colors = ListItemDefaults.colors(containerColor = Color.Transparent),
                        modifier = Modifier.clickableRow { neu = true })
                }
            }
        },
        confirmButton = {
            TextButton(onClick = onDismiss) { Text(tx(R.string.fertig)) }
        },
        containerColor = Color(0xFF16161A))
}

// ---------------------------------------------------------------- Bausteine

/** Nach einem Namen fragen — für eine neue Liste und fürs Umbenennen. */
@Composable
private fun NameDialog(
    titel: String,
    vorher: String,
    knopf: String,
    onDismiss: () -> Unit,
    onConfirm: (String) -> Unit,
) {
    var name by remember { mutableStateOf(vorher) }
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(titel) },
        text = { Field(tx(R.string.name), name, { name = it }) },
        confirmButton = {
            TextButton(onClick = { onConfirm(name.trim()) }, enabled = name.isNotBlank()) {
                Text(knopf)
            }
        },
        dismissButton = {
            TextButton(onClick = onDismiss) { Text(tx(R.string.abbrechen)) }
        },
        containerColor = Color(0xFF16161A))
}

@Composable
private fun ZurueckZeile(onBack: () -> Unit) {
    IconButton(onClick = onBack) {
        Icon(Icons.Filled.ArrowBack, tx(R.string.zurueck), tint = Color.White)
    }
}

@Composable
private fun Abschnitt(titel: String) {
    Text(titel, fontSize = 18.sp, fontWeight = FontWeight.SemiBold,
         modifier = Modifier.padding(bottom = 6.dp))
}
