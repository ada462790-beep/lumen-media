// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package org.lumen.app

import androidx.compose.foundation.layout.*
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.ArrowBack
import androidx.compose.material.icons.filled.Check
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import org.lumen.core.*

/**
 * Das eigene Profil schützen: PIN, Passwort oder keines.
 *
 * Wer am Familien-Tablet sitzt, soll nicht ins Profil der anderen kommen —
 * und wer seinen PIN ändern will, soll dafür nicht an den Rechner müssen.
 * Gefragt wird immer zuerst nach dem bisherigen; ein Profil mit
 * Vollzugriff darf seinen Schutz nicht ganz entfernen (der Server sagt
 * dann 409, und die App erklärt es).
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun SperreScreen(model: LumenViewModel, onBack: () -> Unit) {
    val stage by model.stage.collectAsStateWithLifecycle()
    val me = (stage as? Stage.SignedIn)?.me ?: return
    val bisher = me.account.lockKind
    var alt by remember { mutableStateOf("") }
    var art by remember { mutableStateOf(if (bisher == "none") "pin" else bisher) }
    var neu by remember { mutableStateOf("") }
    var wiederholt by remember { mutableStateOf("") }
    var fehler by remember { mutableStateOf<String?>(null) }
    var sendet by remember { mutableStateOf(false) }
    var fertig by remember { mutableStateOf(false) }
    val scope = rememberCoroutineScope()

    fun senden() {
        if (sendet) return
        fehler = Profilschutz.pruefen(bisher, alt, art, neu, wiederholt)
        if (fehler != null) return
        sendet = true
        scope.launch {
            val meldung = withContext(Dispatchers.IO) {
                try {
                    model.client.sperreAendern(alt, art, if (art == "none") "" else neu)
                    null
                } catch (e: LumenException) {
                    if (Profilschutz.fehltBeimServer(e)) {
                        model.sperreUnbekannt = true
                        tx(R.string.dein_server_kann_das_noch)
                    } else {
                        Profilschutz.meldung(e)
                    }
                }
            }
            sendet = false
            if (meldung == null) {
                fertig = true
                model.refreshMe()
            } else {
                fehler = meldung
            }
        }
    }

    Column(Modifier.fillMaxSize().padding(16.dp).verticalScrollable()) {
        IconButton(onClick = onBack) { Icon(Icons.Filled.ArrowBack, tx(R.string.zurueck), tint = Color.White) }
        Spacer(Modifier.height(8.dp))
        Text(tx(R.string.profil_schuetzen), fontSize = 28.sp, fontWeight = FontWeight.Bold)
        Spacer(Modifier.height(6.dp))
        Text(when (bisher) {
                "pin" -> tx(R.string.dein_profil_ist_mit_einem_2)
                "password" -> tx(R.string.dein_profil_ist_mit_einem)
                else -> tx(R.string.dein_profil_ist_offen_jeder)
             }, color = Lumen.label2)
        Spacer(Modifier.height(22.dp))

        if (fertig) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Icon(Icons.Filled.Check, null, tint = Lumen.good)
                Spacer(Modifier.width(10.dp))
                Text(tx(R.string.gespeichert_ab_jetzt_gilt_der), fontWeight = FontWeight.SemiBold)
            }
            Spacer(Modifier.height(18.dp))
            PrimaryButton(tx(R.string.fertig), onClick = onBack)
            return@Column
        }

        Column(Modifier.widthIn(max = 460.dp), verticalArrangement = Arrangement.spacedBy(14.dp)) {
            if (bisher != "none") {
                Field(if (bisher == "pin") tx(R.string.bisheriger_pin) else tx(R.string.bisheriges_passwort),
                      alt, { alt = it; fehler = null }, secret = true)
            }
            Text(tx(R.string.neuer_schutz), color = Lumen.label2, fontSize = 13.sp)
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                Profilschutz.ARTEN.forEach { (wert, text) ->
                    FilterChip(
                        selected = art == wert,
                        onClick = { art = wert; neu = ""; wiederholt = ""; fehler = null },
                        label = { Text(text) },
                        colors = FilterChipDefaults.filterChipColors(
                            labelColor = Lumen.label2,
                            selectedContainerColor = Lumen.fill,
                            selectedLabelColor = Lumen.label,
                        ),
                    )
                }
            }
            if (art == "none") {
                Text(Einladung.hinweis("none"), color = Lumen.label3, fontSize = 13.sp)
            } else {
                Field(if (art == "pin") tx(R.string.neuer_pin) else tx(R.string.neues_passwort), neu,
                      { neu = it; fehler = null }, secret = true, hint = Einladung.hinweis(art))
                Field(if (art == "pin") tx(R.string.neuen_pin_wiederholen) else tx(R.string.neues_passwort_wiederholen),
                      wiederholt, { wiederholt = it; fehler = null }, secret = true)
            }
        }
        fehler?.let {
            Spacer(Modifier.height(12.dp))
            Text(it, color = Lumen.bad, fontSize = 14.sp)
        }
        Spacer(Modifier.height(22.dp))
        if (sendet) CircularProgressIndicator(color = Lumen.label2)
        else PrimaryButton(tx(R.string.speichern), onClick = { senden() })
    }
}
