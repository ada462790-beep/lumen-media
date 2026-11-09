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
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.ArrowBack
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import kotlinx.coroutines.launch
import org.lumen.core.Wish

/**
 * Die Wünsche: was ich mir gewünscht habe, und wie es steht — dazu ein
 * Feld, um mir etwas zu wünschen. Der Besitzer sieht den Titel und wer ihn
 * gewünscht hat, sonst nichts.
 *
 * Annehmen und Ablehnen gehören zur Verwaltung, und die gibt es nur im
 * Browser. Hier steht deshalb nur der Stand.
 */
@Composable
fun WuenscheScreen(model: LumenViewModel, onBack: () -> Unit) {
    var wuensche by remember { mutableStateOf<List<Wish>?>(null) }
    var eingabe by remember { mutableStateOf("") }
    var hinweis by remember { mutableStateOf<String?>(null) }
    var reload by remember { mutableStateOf(0) }
    val error by model.error.collectAsStateWithLifecycle()
    val scope = rememberCoroutineScope()

    LaunchedEffect(reload) { wuensche = model.fetch { it.wishes() } ?: wuensche ?: emptyList() }

    fun wuenschen() {
        val t = eingabe.trim()
        if (t.isEmpty()) return
        scope.launch {
            val w = model.fetch { it.wish(t) }
            if (w != null) {
                eingabe = ""
                hinweis = tx(R.string.wunsch_geschickt)
                reload++
            }
        }
    }

    LazyColumn(contentPadding = PaddingValues(16.dp)) {
        item {
            IconButton(onClick = onBack) { Icon(Icons.Filled.ArrowBack, tx(R.string.zurueck), tint = Color.White) }
            Text(tx(R.string.wuensche), fontSize = 28.sp, fontWeight = FontWeight.Bold)
            Text(if (model.isAdmin) tx(R.string.was_sich_der_haushalt_wuenscht)
                 else tx(R.string.was_du_dir_gewuenscht_hast),
                 color = Lumen.label2, fontSize = 14.sp)
            Spacer(Modifier.height(18.dp))
            Row(verticalAlignment = Alignment.CenterVertically) {
                OutlinedTextField(
                    value = eingabe,
                    onValueChange = { eingabe = it; hinweis = null },
                    singleLine = true,
                    placeholder = { Text(tx(R.string.titel_wuenschen), color = Lumen.label3) },
                    keyboardOptions = KeyboardOptions(imeAction = ImeAction.Send),
                    keyboardActions = KeyboardActions(onSend = { wuenschen() }),
                    shape = RoundedCornerShape(12.dp),
                    colors = OutlinedTextFieldDefaults.colors(
                        focusedContainerColor = Lumen.fill,
                        unfocusedContainerColor = Lumen.fill,
                        focusedBorderColor = Lumen.accent,
                        unfocusedBorderColor = Color.Transparent,
                    ),
                    modifier = Modifier.weight(1f),
                )
                Spacer(Modifier.width(10.dp))
                PrimaryButton(tx(R.string.wuenschen), eingabe.isNotBlank()) { wuenschen() }
            }
            Text(tx(R.string.der_besitzer_sieht_den_titel),
                 color = Lumen.label3, fontSize = 12.sp, modifier = Modifier.padding(top = 8.dp))
            hinweis?.let { Text(it, color = Lumen.good, fontSize = 14.sp, modifier = Modifier.padding(top = 6.dp)) }
            error?.let { Text(it, color = Lumen.bad, fontSize = 14.sp, modifier = Modifier.padding(top = 6.dp)) }
            Spacer(Modifier.height(18.dp))
        }
        val liste = wuensche
        when {
            liste == null -> item {
                Box(Modifier.fillMaxWidth().height(80.dp), Alignment.Center) {
                    CircularProgressIndicator(color = Lumen.label2)
                }
            }
            liste.isEmpty() -> item { Text(tx(R.string.noch_keine_wuensche), color = Lumen.label2) }
            else -> items(liste, key = { it.id }) { w -> WunschZeile(w) }
        }
    }
}

@Composable
private fun WunschZeile(w: Wish) {
    Row(Modifier.fillMaxWidth().padding(vertical = 10.dp), verticalAlignment = Alignment.CenterVertically) {
        Column(Modifier.weight(1f)) {
            Text(w.titelZeile, fontSize = 16.sp)
            Text(w.vonText, color = Lumen.label2, fontSize = 13.sp)
        }
        val (grund, schrift) = wunschFarben(w.state)
        Text(w.stateLabel, color = schrift, fontSize = 12.sp, fontWeight = FontWeight.SemiBold,
             modifier = Modifier.clip(CircleShape).background(grund)
                 .padding(horizontal = 10.dp, vertical = 4.dp))
    }
}

/** Die Farben des Stands — dieselben wie im Browser. */
fun wunschFarben(state: String): Pair<Color, Color> = when (state) {
    "working" -> Color(0x290A84FF) to Color(0xFF4AA3FF)
    "done" -> Color(0x2930D158) to Color(0xFF30D158)
    "declined" -> Color(0x24FF453A) to Color(0xFFFF6961)
    else -> Color(0x3D787880) to Lumen.label2
}
