// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package org.lumen.app

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.filled.ArrowBack
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.filled.CheckCircle
import androidx.compose.material.icons.filled.Dns
import androidx.compose.material.icons.filled.MoreVert
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import org.lumen.core.*

// ---------------------------------------------------------------- Server

/**
 * Die gespeicherten Server, wie bei Emby: antippen verbindet, „Server
 * hinzufügen" führt zur bekannten Adresseingabe. Umbenennen und Entfernen
 * stecken im Menü am Rand jeder Zeile.
 *
 * Gezeigt wird nur, was dieses Gerät selbst weiss — Name, Adresse, ob es
 * dort angemeldet ist. Nichts darüber, wer auf welchem Server was schaut.
 */
@Composable
fun ServerListeScreen(model: LumenViewModel, stand: Stage.ChoosingServer) {
    val busy by model.busy.collectAsStateWithLifecycle()
    var umbenennen by remember { mutableStateOf<GespeicherterServer?>(null) }
    var entfernen by remember { mutableStateOf<GespeicherterServer?>(null) }

    // Zurück: zum Server, von dem man kam — solange er antwortet.
    val zurueck = stand.aktivId?.takeIf { stand.message == null }
    BackHandler(enabled = zurueck != null && !busy) { zurueck?.let { model.verbinden(it) } }

    LazyColumn(
        Modifier.fillMaxSize().systemBarsPadding(),
        contentPadding = PaddingValues(horizontal = 16.dp, vertical = 24.dp),
    ) {
        item {
            Text(tx(R.string.server), fontSize = 32.sp, fontWeight = FontWeight.Bold,
                 modifier = Modifier.padding(start = 4.dp))
            Text(tx(R.string.tippe_auf_einen_server_um),
                 color = Lumen.label2, fontSize = 15.sp,
                 modifier = Modifier.padding(start = 4.dp, top = 4.dp, bottom = 18.dp))
            stand.message?.let {
                Text(it, color = Lumen.bad, fontSize = 14.sp,
                     modifier = Modifier.padding(start = 4.dp, bottom = 14.dp))
            }
        }
        items(stand.server, key = { it.id }) { s ->
            ServerZeile(
                server = s,
                aktiv = s.id == stand.aktivId,
                angemeldet = model.angemeldetBei(s.id),
                enabled = !busy,
                onClick = { model.verbinden(s.id) },
                onUmbenennen = { umbenennen = s },
                onEntfernen = { entfernen = s },
            )
            Spacer(Modifier.height(10.dp))
        }
        item {
            Spacer(Modifier.height(8.dp))
            if (busy) {
                Box(Modifier.fillMaxWidth().padding(16.dp), Alignment.Center) {
                    CircularProgressIndicator(color = Lumen.label2)
                }
            } else {
                OutlinedButton(
                    onClick = { model.serverHinzufuegen() },
                    shape = CircleShape,
                    colors = ButtonDefaults.outlinedButtonColors(contentColor = Lumen.label),
                    modifier = Modifier.fillMaxWidth(),
                ) {
                    Icon(Icons.Filled.Add, null, modifier = Modifier.size(18.dp))
                    Spacer(Modifier.width(8.dp))
                    Text(tx(R.string.server_hinzufuegen), modifier = Modifier.padding(vertical = 6.dp))
                }
            }
        }
    }

    umbenennen?.let { s ->
        var name by remember(s.id) { mutableStateOf(s.name) }
        AlertDialog(
            onDismissRequest = { umbenennen = null },
            title = { Text(tx(R.string.umbenennen)) },
            text = {
                Column {
                    Field(tx(R.string.name), name, { name = it.take(Serverliste.NAME_LAENGE) },
                          hint = tx(R.string.leer_lassen_fuer_den_namen))
                }
            },
            confirmButton = {
                TextButton(onClick = { umbenennen = null; model.serverUmbenennen(s.id, name) }) {
                    Text(tx(R.string.sichern))
                }
            },
            dismissButton = { TextButton(onClick = { umbenennen = null }) { Text(tx(R.string.abbrechen)) } },
            containerColor = Color(0xFF16161A))
    }

    entfernen?.let { s ->
        AlertDialog(
            onDismissRequest = { entfernen = null },
            title = { Text(tx(R.string.x_entfernen, s.name)) },
            text = {
                Text(tx(R.string.dieses_geraet_vergisst_den_server))
            },
            confirmButton = {
                TextButton(onClick = { entfernen = null; model.serverEntfernen(s.id) }) {
                    Text(tx(R.string.entfernen), color = Lumen.bad)
                }
            },
            dismissButton = { TextButton(onClick = { entfernen = null }) { Text(tx(R.string.abbrechen)) } },
            containerColor = Color(0xFF16161A))
    }
}

@Composable
private fun ServerZeile(
    server: GespeicherterServer,
    aktiv: Boolean,
    angemeldet: Boolean,
    enabled: Boolean,
    onClick: () -> Unit,
    onUmbenennen: () -> Unit,
    onEntfernen: () -> Unit,
) {
    var menue by remember { mutableStateOf(false) }
    val zeile = buildList {
        add(server.adresseKurz)
        if (server.vonUnterwegs) add(tx(R.string.von_unterwegs))
        if (angemeldet) add("angemeldet")
    }.joinToString(" · ")

    Surface(
        onClick = onClick,
        enabled = enabled,
        shape = RoundedCornerShape(Lumen.corner),
        color = Lumen.surface,
        modifier = Modifier.fillMaxWidth(),
    ) {
        Row(Modifier.padding(start = 14.dp, top = 12.dp, bottom = 12.dp, end = 4.dp),
            verticalAlignment = Alignment.CenterVertically) {
            Box(Modifier.size(44.dp).clip(CircleShape).background(Lumen.fill), Alignment.Center) {
                Icon(Icons.Filled.Dns, null, tint = Lumen.label2, modifier = Modifier.size(22.dp))
            }
            Spacer(Modifier.width(14.dp))
            Column(Modifier.weight(1f)) {
                Text(server.name, fontSize = 17.sp, fontWeight = FontWeight.SemiBold,
                     maxLines = 1, overflow = TextOverflow.Ellipsis)
                Text(zeile, color = Lumen.label3, fontSize = 13.sp,
                     maxLines = 1, overflow = TextOverflow.Ellipsis)
            }
            if (aktiv) {
                Icon(Icons.Filled.CheckCircle, tx(R.string.zuletzt_benutzt), tint = Lumen.accent,
                     modifier = Modifier.size(20.dp))
            }
            Box {
                IconButton(onClick = { menue = true }) {
                    Icon(Icons.Filled.MoreVert, tx(R.string.mehr), tint = Lumen.label2)
                }
                DropdownMenu(expanded = menue, onDismissRequest = { menue = false }) {
                    DropdownMenuItem(text = { Text(tx(R.string.umbenennen)) },
                                     onClick = { menue = false; onUmbenennen() })
                    DropdownMenuItem(text = { Text(tx(R.string.entfernen), color = Lumen.bad) },
                                     onClick = { menue = false; onEntfernen() })
                }
            }
        }
    }
}

// ---------------------------------------------------------------- Mit Code anmelden

/**
 * Dieses Gerät mit einem Code anmelden, den jemand an einem anderen Gerät
 * freigibt — praktisch, wenn das Passwort gerade niemand weiss. Derselbe
 * Weg wie am Fernseher: Code zeigen, alle drei Sekunden nachfragen, nach
 * Ablauf einen neuen holen.
 */
@Composable
fun CodeAnmeldenScreen(model: LumenViewModel, onBack: () -> Unit) {
    BackHandler(onBack = onBack)
    var code by remember { mutableStateOf<AnmeldeCode?>(null) }
    var rest by remember { mutableStateOf(0L) }
    var fehler by remember { mutableStateOf<String?>(null) }

    // Solange die Ansicht offen ist: Code holen, nachfragen, erneuern.
    // Verlässt man sie, endet die Schleife mit ihr.
    LaunchedEffect(Unit) {
        while (true) {
            val (neu, meldung) = model.codeStarten()
            if (neu == null) {
                fehler = meldung
                delay(Anmeldecode.NACHFRAGEN_MS * 2)
                continue
            }
            fehler = null
            code = neu
            val bis = System.currentTimeMillis() + neu.gueltigSek * 1000L
            while (System.currentTimeMillis() < bis) {
                delay(Anmeldecode.NACHFRAGEN_MS)
                when (model.codeNachfragen(neu.geheim)) {
                    CodeStand.ANGEMELDET -> return@LaunchedEffect
                    CodeStand.ABGELAUFEN -> break
                    CodeStand.FEHLER -> fehler = tx(R.string.der_server_antwortet_gerade_nicht)
                    CodeStand.WARTET -> fehler = null
                }
            }
            code = null
        }
    }
    // Die Uhr läuft im Sekundentakt, auch zwischen zwei Nachfragen.
    LaunchedEffect(code) {
        val c = code ?: return@LaunchedEffect
        val bis = System.currentTimeMillis() + c.gueltigSek * 1000L
        while (true) {
            rest = ((bis - System.currentTimeMillis()) / 1000).coerceAtLeast(0)
            if (rest == 0L) break
            delay(1000)
        }
    }

    Column(
        Modifier.fillMaxSize().systemBarsPadding().padding(28.dp).verticalScrollable(),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Row(Modifier.fillMaxWidth()) {
            IconButton(onClick = onBack) { Icon(Icons.Filled.ArrowBack, tx(R.string.zurueck), tint = Color.White) }
        }
        Spacer(Modifier.height(24.dp))
        Text(tx(R.string.mit_code_anmelden), fontSize = 28.sp, fontWeight = FontWeight.Bold)
        Spacer(Modifier.height(10.dp))
        Text(Anmeldecode.ERKLAERUNG_HANDY, color = Lumen.label2, textAlign = TextAlign.Center,
             modifier = Modifier.widthIn(max = 420.dp))
        Spacer(Modifier.height(36.dp))
        val c = code
        if (c == null) {
            CircularProgressIndicator(color = Lumen.label2)
        } else {
            Surface(shape = RoundedCornerShape(20.dp), color = Lumen.surface) {
                Text(c.anzeige, fontSize = 48.sp, fontWeight = FontWeight.Bold,
                     fontFamily = FontFamily.Monospace, letterSpacing = 4.sp,
                     modifier = Modifier.padding(horizontal = 28.dp, vertical = 18.dp))
            }
            Spacer(Modifier.height(14.dp))
            Text(Anmeldecode.restText(rest), color = Lumen.label3, fontSize = 14.sp)
            Spacer(Modifier.height(18.dp))
            Row(verticalAlignment = Alignment.CenterVertically) {
                CircularProgressIndicator(color = Lumen.label3, strokeWidth = 2.dp,
                                          modifier = Modifier.size(14.dp))
                Spacer(Modifier.width(8.dp))
                Text(tx(R.string.wartet_auf_die_freigabe), color = Lumen.label3, fontSize = 14.sp)
            }
        }
        fehler?.let {
            Spacer(Modifier.height(14.dp))
            Text(it, color = Lumen.bad, fontSize = 14.sp, textAlign = TextAlign.Center)
        }
    }
}

// ---------------------------------------------------------------- Fernseher anmelden

/**
 * Einen Fernseher (oder ein anderes Gerät) mit dem eigenen Profil anmelden:
 * den sechsstelligen Code eingeben, der dort steht. Freigegeben wird nur
 * das eigene Profil — für niemand anderen.
 */
@Composable
fun FernseherAnmeldenScreen(model: LumenViewModel, onBack: () -> Unit) {
    var eingabe by remember { mutableStateOf("") }
    var fehler by remember { mutableStateOf<String?>(null) }
    var fertig by remember { mutableStateOf<String?>(null) }
    var sendet by remember { mutableStateOf(false) }
    val scope = rememberCoroutineScope()
    val feld = remember { FocusRequester() }

    fun senden() {
        if (sendet || !Anmeldecode.vollstaendig(eingabe)) return
        sendet = true
        fehler = null
        scope.launch {
            val (geraet, meldung) = model.fernseherAnmelden(eingabe)
            sendet = false
            if (geraet != null) fertig = geraet else fehler = meldung
        }
    }

    LaunchedEffect(Unit) {
        delay(150)
        runCatching { feld.requestFocus() }
    }

    Column(
        Modifier.fillMaxSize().padding(16.dp).verticalScrollable(),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Row(Modifier.fillMaxWidth()) {
            IconButton(onClick = onBack) { Icon(Icons.Filled.ArrowBack, tx(R.string.zurueck), tint = Color.White) }
        }
        Spacer(Modifier.height(16.dp))
        Text(tx(R.string.fernseher_anmelden), fontSize = 28.sp, fontWeight = FontWeight.Bold)
        Spacer(Modifier.height(10.dp))

        val name = fertig
        if (name != null) {
            Spacer(Modifier.height(28.dp))
            Icon(Icons.Filled.Check, null, tint = Lumen.good, modifier = Modifier.size(56.dp))
            Spacer(Modifier.height(14.dp))
            Text(Anmeldecode.angemeldetText(name), fontSize = 20.sp, fontWeight = FontWeight.SemiBold,
                 textAlign = TextAlign.Center)
            Spacer(Modifier.height(8.dp))
            Text(tx(R.string.dort_bist_du_jetzt_mit),
                 color = Lumen.label2, textAlign = TextAlign.Center, fontSize = 14.sp,
                 modifier = Modifier.widthIn(max = 420.dp))
            Spacer(Modifier.height(26.dp))
            PrimaryButton(tx(R.string.fertig), onClick = onBack)
            Spacer(Modifier.height(8.dp))
            TextButton(onClick = { fertig = null; eingabe = "" }) {
                Text(tx(R.string.noch_ein_geraet_anmelden), color = Lumen.label2)
            }
        } else {
            FernseherCodeEingabe(eingabe, fehler, sendet, feld,
                onEingabe = { eingabe = Anmeldecode.saubern(it); fehler = null },
                onSenden = { senden() })
        }
    }
}

@Composable
private fun FernseherCodeEingabe(
    eingabe: String,
    fehler: String?,
    sendet: Boolean,
    feld: FocusRequester,
    onEingabe: (String) -> Unit,
    onSenden: () -> Unit,
) {
    Column(horizontalAlignment = Alignment.CenterHorizontally) {
        Text(tx(R.string.am_fernseher_steht_ein_sechsstelliger),
             color = Lumen.label2, textAlign = TextAlign.Center,
             modifier = Modifier.widthIn(max = 420.dp))
        Spacer(Modifier.height(28.dp))
        OutlinedTextField(
            value = eingabe,
            onValueChange = onEingabe,
            singleLine = true,
            placeholder = {
                Text("000000", color = Lumen.label3, fontSize = 34.sp,
                     fontFamily = FontFamily.Monospace, textAlign = TextAlign.Center,
                     modifier = Modifier.fillMaxWidth())
            },
            textStyle = TextStyle(fontSize = 34.sp, fontFamily = FontFamily.Monospace,
                                  letterSpacing = 4.sp, textAlign = TextAlign.Center,
                                  color = Lumen.label),
            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.NumberPassword,
                                              imeAction = ImeAction.Done),
            keyboardActions = KeyboardActions(onDone = { onSenden() }),
            shape = RoundedCornerShape(16.dp),
            colors = OutlinedTextFieldDefaults.colors(
                focusedContainerColor = Lumen.fill,
                unfocusedContainerColor = Lumen.fill,
                focusedBorderColor = Lumen.accent,
                unfocusedBorderColor = Color.Transparent,
            ),
            modifier = Modifier.widthIn(max = 300.dp).fillMaxWidth().focusRequester(feld),
        )
        fehler?.let {
            Spacer(Modifier.height(12.dp))
            Text(it, color = Lumen.bad, fontSize = 14.sp, textAlign = TextAlign.Center)
        }
        Spacer(Modifier.height(24.dp))
        if (sendet) CircularProgressIndicator(color = Lumen.label2)
        else PrimaryButton(tx(R.string.anmelden), Anmeldecode.vollstaendig(eingabe), onClick = onSenden)
    }
}
