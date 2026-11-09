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
import androidx.compose.foundation.focusable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
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
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.delay
import kotlinx.coroutines.withContext
import org.lumen.core.*

// ---------------------------------------------------------------- Server

/**
 * Die gespeicherten Server am Fernseher: eine Zeile je Server, „OK"
 * verbindet. Rechts daneben „Bearbeiten" für Umbenennen und Entfernen —
 * ein langes Drücken findet auf der Fernbedienung niemand.
 */
@Composable
internal fun TvServerListe(
    stand: Stage.ChoosingServer,
    angemeldet: (String) -> Boolean,
    onVerbinden: (String) -> Unit,
    onHinzufuegen: () -> Unit,
    onUmbenennen: (String, String) -> Unit,
    onEntfernen: (String) -> Unit,
) {
    var bearbeiten by remember { mutableStateOf<GespeicherterServer?>(null) }
    var umbenennen by remember { mutableStateOf<GespeicherterServer?>(null) }
    var entfernen by remember { mutableStateOf<GespeicherterServer?>(null) }
    val anfang = remember { FocusRequester() }

    LaunchedEffect(Unit) {
        delay(120)
        runCatching { anfang.requestFocus() }
    }

    // Zurück: zum Server, von dem man kam — solange er antwortet.
    val zurueck = stand.aktivId?.takeIf { stand.message == null }
    if (zurueck != null) androidx.activity.compose.BackHandler { onVerbinden(zurueck) }

    // Der zuletzt benutzte bekommt den Fokus — meist will man genau dorthin.
    val vorn = stand.server.indexOfFirst { it.id == stand.aktivId }.coerceAtLeast(0)

    LazyColumn(
        Modifier.fillMaxSize(),
        contentPadding = PaddingValues(horizontal = 96.dp, vertical = 56.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        item {
            Column(Modifier.widthIn(max = 980.dp).fillMaxWidth().padding(bottom = 28.dp)) {
                Text(tx(R.string.server), fontSize = 46.sp, fontWeight = FontWeight.Bold)
                Spacer(Modifier.height(6.dp))
                Text(tx(R.string.waehle_einen_server), color = Tv.label2, fontSize = 20.sp)
                stand.message?.let {
                    Spacer(Modifier.height(14.dp))
                    Text(it, color = Tv.bad, fontSize = 18.sp)
                }
            }
        }
        items(stand.server.size, key = { stand.server[it].id }) { i ->
            val s = stand.server[i]
            Row(
                Modifier.widthIn(max = 980.dp).fillMaxWidth().padding(bottom = 14.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                TvServerZeile(
                    s, aktiv = s.id == stand.aktivId, angemeldet = angemeldet(s.id),
                    modifier = Modifier.weight(1f).then(
                        if (i == vorn) Modifier.focusRequester(anfang) else Modifier),
                ) { onVerbinden(s.id) }
                Spacer(Modifier.width(16.dp))
                TvButton(tx(R.string.bearbeiten)) { bearbeiten = s }
            }
        }
        item {
            Box(Modifier.widthIn(max = 980.dp).fillMaxWidth().padding(top = 18.dp)) {
                TvButton(tx(R.string.server_hinzufuegen)) { onHinzufuegen() }
            }
        }
    }

    bearbeiten?.let { s ->
        TvAuswahlDialog(
            titel = s.name,
            wahl = listOf("umbenennen" to tx(R.string.umbenennen), "entfernen" to tx(R.string.entfernen)),
            aktuell = "",
            onWahl = { w ->
                bearbeiten = null
                if (w == "umbenennen") umbenennen = s else entfernen = s
            },
            onClose = { bearbeiten = null })
    }
    umbenennen?.let { s ->
        TvNameDialog(tx(R.string.umbenennen), s.name, tx(R.string.sichern), onDismiss = { umbenennen = null }) { name ->
            umbenennen = null
            onUmbenennen(s.id, name)
        }
    }
    entfernen?.let { s ->
        TvFrage(tx(R.string.x_entfernen, s.name),
                tx(R.string.dieser_fernseher_vergisst_den_server),
                tx(R.string.entfernen), onNein = { entfernen = null }) {
            entfernen = null
            onEntfernen(s.id)
        }
    }
}

@Composable
private fun TvServerZeile(
    server: GespeicherterServer,
    aktiv: Boolean,
    angemeldet: Boolean,
    modifier: Modifier = Modifier,
    onClick: () -> Unit,
) {
    var fokussiert by remember { mutableStateOf(false) }
    val zeile = buildList {
        add(server.adresseKurz)
        if (server.vonUnterwegs) add(tx(R.string.von_unterwegs))
        if (angemeldet) add("angemeldet")
    }.joinToString(" · ")
    Row(
        modifier
            .onFocusChanged { fokussiert = it.isFocused }
            .clickable(role = Role.Button, onClick = onClick)
            .clip(RoundedCornerShape(18.dp))
            .background(if (fokussiert) Color.White else Color(0x14FFFFFF))
            .padding(horizontal = 24.dp, vertical = 18.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        // Ein Kreis mit dem Anfangsbuchstaben — wie ein Profil, nur für den Server.
        Box(
            Modifier.size(52.dp).clip(CircleShape)
                .background(if (fokussiert) Color(0x1A000000) else Color(0x22FFFFFF)),
            Alignment.Center,
        ) {
            Text(server.name.take(1).uppercase(), fontSize = 22.sp, fontWeight = FontWeight.SemiBold,
                 color = if (fokussiert) Color.Black else Color.White)
        }
        Spacer(Modifier.width(20.dp))
        Column(Modifier.weight(1f)) {
            Text(server.name, fontSize = 24.sp, fontWeight = FontWeight.SemiBold,
                 color = if (fokussiert) Color.Black else Color.White,
                 maxLines = 1, overflow = TextOverflow.Ellipsis)
            Text(zeile, fontSize = 16.sp, color = if (fokussiert) Color(0x99000000) else Tv.label3,
                 maxLines = 1, overflow = TextOverflow.Ellipsis)
        }
        if (aktiv) {
            Text(tx(R.string.zuletzt_benutzt), fontSize = 15.sp,
                 color = if (fokussiert) Color(0x99000000) else Tv.accent)
        }
    }
}

// ---------------------------------------------------------------- Mit Code anmelden

/**
 * Der Hauptweg am Fernseher: ein grosser Code, den jemand am Handy oder im
 * Browser eingibt. Alle drei Sekunden fragt der Fernseher nach; läuft der
 * Code ab, holt er von selbst einen neuen. Daneben bleibt die gewohnte
 * Profilauswahl — für wen das Passwort griffbereit hat.
 */
@Composable
internal fun TvCodeAnmelden(
    controller: SessionController,
    serverName: String?,
    onProfilHier: () -> Unit,
    onServerWechseln: () -> Unit,
    onAngemeldet: () -> Unit,
    // Von unterwegs gibt es keine Profilauswahl, nur Name und PIN.
    profilHierText: String = tx(R.string.profil_hier_waehlen),
) {
    var code by remember { mutableStateOf<AnmeldeCode?>(null) }
    var bis by remember { mutableStateOf(0L) }
    var rest by remember { mutableStateOf(0L) }
    var fehler by remember { mutableStateOf<String?>(null) }
    val anfang = remember { FocusRequester() }

    LaunchedEffect(Unit) {
        delay(120)
        runCatching { anfang.requestFocus() }
    }

    // Code holen, nachfragen, erneuern — solange die Ansicht offen ist.
    LaunchedEffect(Unit) {
        while (true) {
            val neu = withContext(Dispatchers.IO) { controller.codeStarten() }
            if (neu == null) {
                fehler = controller.lastError
                delay(Anmeldecode.NACHFRAGEN_MS * 2)
                continue
            }
            fehler = null
            code = neu
            bis = System.currentTimeMillis() + neu.gueltigSek * 1000L
            while (System.currentTimeMillis() < bis) {
                delay(Anmeldecode.NACHFRAGEN_MS)
                when (withContext(Dispatchers.IO) { controller.codeNachfragen(neu.geheim) }) {
                    CodeStand.ANGEMELDET -> { onAngemeldet(); return@LaunchedEffect }
                    CodeStand.ABGELAUFEN -> break
                    CodeStand.FEHLER -> fehler = tx(R.string.der_server_antwortet_gerade_nicht)
                    CodeStand.WARTET -> fehler = null
                }
            }
            code = null
        }
    }
    // Die Uhr unter dem Code, im Sekundentakt.
    LaunchedEffect(bis) {
        while (bis > 0) {
            rest = ((bis - System.currentTimeMillis()) / 1000).coerceAtLeast(0)
            if (rest == 0L) break
            delay(1000)
        }
    }

    Mitte {
        Row(
            Modifier.widthIn(max = 1180.dp).fillMaxWidth().padding(horizontal = 64.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Column(Modifier.weight(1f).padding(end = 48.dp)) {
                Text(tx(R.string.mit_code_anmelden), fontSize = 46.sp, fontWeight = FontWeight.Bold)
                serverName?.let {
                    Spacer(Modifier.height(6.dp))
                    Text(tx(R.string.bei_x, it), color = Tv.label3, fontSize = 20.sp)
                }
                Spacer(Modifier.height(20.dp))
                Text(Anmeldecode.ERKLAERUNG, color = Tv.label2, fontSize = 22.sp,
                     lineHeight = 32.sp, modifier = Modifier.widthIn(max = 560.dp))
                Spacer(Modifier.height(40.dp))
                Row(horizontalArrangement = Arrangement.spacedBy(18.dp)) {
                    TvButton(profilHierText, modifier = Modifier.focusRequester(anfang)) { onProfilHier() }
                    TvButton(tx(R.string.server_wechseln)) { onServerWechseln() }
                }
            }
            Column(
                Modifier
                    .clip(RoundedCornerShape(28.dp))
                    .background(Color(0x14FFFFFF))
                    .border(1.dp, Color(0x1FFFFFFF), RoundedCornerShape(28.dp))
                    .padding(horizontal = 56.dp, vertical = 44.dp)
                    .widthIn(min = 420.dp),
                horizontalAlignment = Alignment.CenterHorizontally,
            ) {
                val c = code
                if (c == null) {
                    Box(Modifier.height(110.dp), Alignment.Center) {
                        CircularProgressIndicator(color = Tv.label2)
                    }
                } else {
                    Text(c.anzeige, fontSize = 88.sp, fontWeight = FontWeight.Bold,
                         fontFamily = FontFamily.Monospace, letterSpacing = 6.sp,
                         textAlign = TextAlign.Center)
                }
                Spacer(Modifier.height(16.dp))
                Text(if (c == null) tx(R.string.code_wird_geholt) else Anmeldecode.restText(rest),
                     color = Tv.label3, fontSize = 18.sp)
                fehler?.let {
                    Spacer(Modifier.height(12.dp))
                    Text(it, color = Tv.bad, fontSize = 16.sp, textAlign = TextAlign.Center,
                         modifier = Modifier.widthIn(max = 420.dp))
                }
            }
        }
    }
}
