// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package org.lumen.app

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.ArrowBack
import androidx.compose.material.icons.filled.Collections
import androidx.compose.material.icons.filled.Movie
import androidx.compose.material.icons.filled.Tv
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.rotate
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import org.lumen.core.Neuigkeit
import org.lumen.core.Neuigkeiten
import org.lumen.core.Zeitpunkt

/**
 * Die Neuigkeiten: neue Folgen deiner Serien, neue Filme deiner Reihen,
 * neu Dazugekommenes — dieselben Einträge wie im Browser.
 *
 * Alles kommt aus dem eigenen Stand. Niemand sonst sieht diese Liste, und
 * sie verrät nichts darüber, was andere im Haushalt schauen.
 *
 * Öffnen heisst gelesen — aber erst, nachdem die Liste da ist, damit die
 * neuen Einträge hier noch als neu markiert stehen.
 */
@Composable
fun NeuigkeitenScreen(
    model: LumenViewModel,
    onBack: () -> Unit,
    onOpen: (String) -> Unit,
    onHome: () -> Unit,
) {
    var daten by remember { mutableStateOf<Neuigkeiten?>(null) }
    var loading by remember { mutableStateOf(true) }
    var reload by remember { mutableStateOf(0) }
    // Welcher „{n} neue Titel"-Eintrag aufgeklappt ist.
    var auf by remember { mutableStateOf<Int?>(null) }
    val error by model.error.collectAsStateWithLifecycle()

    LaunchedEffect(reload) {
        val d = model.fetch { it.news() }
        daten = d
        loading = false
        if (d != null && d.ungelesen > 0) {
            model.send(onDone = { model.refreshMe() }) { it.newsSeen() }
        }
    }

    val d = daten
    LazyColumn(contentPadding = PaddingValues(16.dp)) {
        item {
            IconButton(onClick = onBack) {
                Icon(Icons.Filled.ArrowBack, tx(R.string.zurueck), tint = Color.White)
            }
            Text(tx(R.string.neuigkeiten), fontSize = 28.sp, fontWeight = FontWeight.Bold)
            Text(tx(R.string.neue_folgen_deiner_serien_neue),
                 color = Lumen.label2, fontSize = 14.sp,
                 modifier = Modifier.padding(top = 4.dp, bottom = 18.dp))
        }
        if (d != null) {
            if (d.eintraege.isEmpty()) {
                item {
                    Text(tx(R.string.nichts_neues_lumen_meldet_sich),
                         color = Lumen.label2, fontSize = 15.sp)
                }
            }
            itemsIndexed(d.eintraege) { i, n ->
                val offen = n.klappbar && auf == i
                NeuigkeitZeile(n, d.istNeu(n), offen) {
                    val id = n.itemId
                    when {
                        id != null -> onOpen(id)
                        // „{n} neue Titel": aufklappen statt zur Startseite — wie im Browser.
                        n.klappbar -> auf = if (offen) null else i
                        else -> onHome()
                    }
                }
                if (offen) NeueTitel(model, n, onOpen)
            }
        }
    }
    if (d == null) StateOverlay(loading, error, onRetry = { reload++ })
}

/** Die neuen Titel eines aufgeklappten Eintrags: Poster zum Öffnen, darunter, was fehlt. */
@Composable
private fun NeueTitel(model: LumenViewModel, n: Neuigkeit, onOpen: (String) -> Unit) {
    Column(Modifier.padding(start = 4.dp, bottom = 12.dp)) {
        LazyRow(horizontalArrangement = Arrangement.spacedBy(12.dp),
                contentPadding = PaddingValues(vertical = 6.dp)) {
            items(n.titelListe, key = { it.id }) { t -> PosterCard(model, t, onOpen = { onOpen(it.id) }) }
        }
        if (n.weitere > 0) {
            Text(tx(R.string.neu_und_weitere, n.weitere), color = Lumen.label2, fontSize = 13.sp,
                 modifier = Modifier.padding(top = 4.dp))
        }
    }
}

@Composable
private fun NeuigkeitZeile(n: Neuigkeit, neu: Boolean, offen: Boolean, onClick: () -> Unit) {
    Row(
        Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(12.dp))
            .clickableRow(onClick)
            .padding(vertical = 10.dp, horizontal = 4.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Box(
            Modifier.size(40.dp).clip(RoundedCornerShape(12.dp))
                .background(if (neu) Color(0x2E0A84FF) else Lumen.fill),
            contentAlignment = Alignment.Center,
        ) {
            Icon(
                when (n.art) {
                    "folgen" -> Icons.Filled.Tv
                    "reihe" -> Icons.Filled.Collections
                    else -> Icons.Filled.Movie
                },
                null,
                tint = if (neu) Color(0xFF4AA3FF) else Lumen.label2,
                modifier = Modifier.size(20.dp))
        }
        Spacer(Modifier.width(14.dp))
        Column(Modifier.weight(1f)) {
            Text(n.titel, fontSize = 16.sp, maxLines = 1, overflow = TextOverflow.Ellipsis,
                 fontWeight = if (neu) FontWeight.SemiBold else FontWeight.Normal)
            Text(listOf(n.text, Zeitpunkt.kurz(n.at)).filter { it.isNotBlank() }.joinToString(" · "),
                 color = Lumen.label2, fontSize = 13.sp, maxLines = 1, overflow = TextOverflow.Ellipsis)
        }
        if (neu) {
            Box(Modifier.padding(horizontal = 8.dp).size(9.dp).clip(CircleShape)
                    .background(Color(0xFFFF453A)))
        }
        // Aufklappbar: der Pfeil zeigt nach unten, solange die Titel offen sind.
        Text("›", color = Lumen.label3, fontSize = 20.sp,
             modifier = if (n.klappbar) Modifier.rotate(if (offen) 270f else 90f) else Modifier)
    }
}
