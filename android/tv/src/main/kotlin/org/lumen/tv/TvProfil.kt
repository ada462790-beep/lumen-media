// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package org.lumen.tv

import org.lumen.texte.Sprachwahl
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.focusable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.OutlinedTextField
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
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.text.input.VisualTransformation
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import org.lumen.core.*

/**
 * Das Profil am Fernseher — die Einstellungen aus dem Browser, mit der
 * Fernbedienung bedienbar: Profil und Server wechseln, Wiedergabe (Folgen automatisch
 * weiter, Vorspann überspringen, Sprachen für Ton und Untertitel), wie
 * Untertitel aussehen, Privatsphäre, Profilbild, Verlauf, Geräte und
 * „Über Lumen".
 *
 * Ein eigenes Bild hochladen geht nur am Telefon oder im Browser — am
 * Fernseher gibt es keine Fotos. Die Verwaltung gibt es nur im Browser.
 */
@Composable
internal fun TvProfil(
    client: ApiClient,
    ich: Me,
    prefs: Map<String, String>,
    info: TvServerInfo,
    onPref: (String, String) -> Unit,
    onIch: (Me) -> Unit,
    onAbmelden: () -> Unit,
    onServerWechseln: () -> Unit = {},
    ungelesen: Int = 0,
    onBereich: (TvBereich) -> Unit = {},
) {
    var sperreOffen by remember { mutableStateOf(false) }
    var sperreUnbekannt by remember { mutableStateOf(false) }
    var geraete by remember { mutableStateOf<List<Device>?>(null) }
    var sprachwahl by remember { mutableStateOf<String?>(null) }
    var verlaufFrage by remember { mutableStateOf(false) }
    var ueberallFrage by remember { mutableStateOf(false) }
    var hinweis by remember { mutableStateOf<String?>(null) }
    val scope = rememberCoroutineScope()
    val ctx = androidx.compose.ui.platform.LocalContext.current
    val a = ich.account
    val stil = UntertitelStil.aus(prefs)

    LaunchedEffect(Unit) {
        geraete = try { withContext(Dispatchers.IO) { client.devices() } } catch (e: LumenException) { emptyList() }
    }

    // Was gerade im eigenen Profil läuft. Der Zähler lädt nach dem Beenden neu.
    var wiedergaben by remember { mutableStateOf<List<EigeneWiedergabe>>(emptyList()) }
    var wiedergabenStand by remember { mutableStateOf(0) }
    LaunchedEffect(wiedergabenStand) {
        wiedergaben = try {
            withContext(Dispatchers.IO) { client.meineWiedergaben() }
        } catch (e: LumenException) {
            emptyList()
        }
    }

    // Wie viel Bildschirmzeit heute bleibt — nur Kinderprofile haben eine
    // Regel. Beiwerk: ohne Antwort bleibt der Abschnitt weg.
    var bildschirmzeit by remember { mutableStateOf<Bildschirmzeit?>(null) }
    LaunchedEffect(a.id, a.isKid) {
        bildschirmzeit = if (!a.isKid) null
            else withContext(Dispatchers.IO) { oderNull { client.bildschirmzeit() } }
    }

    fun wiedergabeBeenden(id: String) {
        scope.launch {
            try {
                withContext(Dispatchers.IO) { client.wiedergabeBeenden(id) }
            } catch (e: LumenException) {
                // Meist: lief schon nicht mehr. Die Liste stimmt gleich wieder.
                hinweis = e.message
            }
            wiedergabenStand++
        }
    }

    fun profilbild(avatar: String) {
        if (a.avatar == avatar) return
        scope.launch {
            try {
                onIch(withContext(Dispatchers.IO) { client.setAvatar(avatar) })
            } catch (e: LumenException) {
                hinweis = e.message
            }
        }
    }

    LazyColumn(contentPadding = PaddingValues(start = 56.dp, end = 56.dp, top = 16.dp, bottom = 64.dp)) {
        item {
            Row(verticalAlignment = Alignment.CenterVertically, modifier = Modifier.padding(bottom = 24.dp)) {
                TvAvatar(a.initial, a.avatar, 88.dp, client.profilbildUrl(a.id, a.avatar))
                Spacer(Modifier.width(22.dp))
                Column(Modifier.weight(1f)) {
                    Text(a.name, fontSize = 36.sp, fontWeight = FontWeight.Bold)
                    Text(Einstellungen.rolle(a), color = Tv.label2, fontSize = 18.sp)
                }
                TvButton(tx(R.string.profil_wechseln)) { onAbmelden() }
                Spacer(Modifier.width(14.dp))
                TvButton(tx(R.string.server_wechseln)) { onServerWechseln() }
            }
            hinweis?.let { Text(it, color = Tv.bad, fontSize = 17.sp, modifier = Modifier.padding(bottom = 12.dp)) }
        }

        bildschirmzeit?.takeIf { it.aktiv }?.let { bz ->
            item { TvGruppe(tx(R.string.bildschirmzeit)) }
            item { TvEinstellung(bz.hauptzeile, wert = bz.nebenzeile) }
        }

        // Was nicht oben in der Navigation steht, liegt hier.
        item { TvGruppe(tx(R.string.mehr)) }
        item {
            TvEinstellung(tx(R.string.neuigkeiten), wert = if (ungelesen > 0) tx(R.string.x_ungelesen, ungelesen) else tx(R.string.alles_gelesen)) {
                onBereich(TvBereich.NEUIGKEITEN)
            }
        }
        item { TvEinstellung(tx(R.string.favoriten), wert = tx(R.string.was_du_mit_dem_herz)) { onBereich(TvBereich.FAVORITEN) } }
        item { TvEinstellung(tx(R.string.filmreihen), wert = tx(R.string.von_lumen_zusammengestellt)) { onBereich(TvBereich.REIHEN) } }
        item { TvEinstellung(tx(R.string.listen), wert = tx(R.string.eigene_und_geteilte)) { onBereich(TvBereich.LISTEN) } }
        item { TvEinstellung(tx(R.string.wuensche), wert = tx(R.string.ansehen_und_wuenschen)) { onBereich(TvBereich.WUENSCHE) } }

        if (!sperreUnbekannt) {
            item { TvGruppe(tx(R.string.profil_schuetzen)) }
            item {
                TvEinstellung(when (a.lockKind) {
                        "pin" -> tx(R.string.pin_aendern)
                        "password" -> tx(R.string.passwort_aendern)
                        else -> tx(R.string.pin_oder_passwort_festlegen)
                    }, wert = Profilschutz.name(a.lockKind)) { sperreOffen = true }
            }
        }

        // Die Sprache gehört zum Profil: gilt im Browser und in allen Apps.
        item { TvGruppe(Sprachwahl.titel(ctx)) }
        item {
            TvEinstellung(Sprachwahl.titel(ctx),
                          wert = Sprachwahl.wahlText(ctx, prefs, info.sprachen, info.sprache)) {
                sprachwahl = Sprachen.PREF
            }
        }
        item { TvFuss(Sprachwahl.hinweis(ctx)) }

        item { TvGruppe(tx(R.string.wiedergabe)) }
        item {
            TvEinstellung(tx(R.string.folgen_automatisch_weiter), schalter = Einstellungen.autoplay(prefs)) {
                onPref(Einstellungen.AUTOPLAY, Einstellungen.schalter(!Einstellungen.autoplay(prefs)))
            }
        }
        item {
            TvEinstellung(tx(R.string.vorspann_ueberspringen), schalter = Einstellungen.vorspann(prefs)) {
                onPref(Einstellungen.VORSPANN, Einstellungen.schalter(!Einstellungen.vorspann(prefs)))
            }
        }
        // Die Sprachen, die der Player vorbelegt. Lumen merkt sie sich auch,
        // wenn man im Player eine Spur wählt.
        item { TvEinstellung(tx(R.string.tonsprache), wert = Einstellungen.tonText(prefs)) { sprachwahl = Spuren.PREF_TON } }
        item {
            TvEinstellung(tx(R.string.untertitel), wert = Einstellungen.untertitelText(prefs)) {
                sprachwahl = Spuren.PREF_UNTERTITEL
            }
        }
        item { TvEinstellung(tx(R.string.qualitaet_unterwegs), wert = ich.limits.remoteQuality) }

        item { TvGruppe(tx(R.string.untertitel)) }
        item {
            // Ein Druck schaltet weiter: Klein, Normal, Gross.
            val name = UntertitelStil.GROESSEN.firstOrNull { it.first == stil.groesse }?.second ?: tx(R.string.normal)
            TvEinstellung(tx(R.string.groesse), wert = name) {
                onPref(UntertitelStil.PREF_GROESSE, Einstellungen.naechste(UntertitelStil.GROESSEN, stil.groesse))
            }
        }
        item {
            TvEinstellung(tx(R.string.dunkler_hintergrund_hinter_dem_text), schalter = stil.hintergrund) {
                onPref(UntertitelStil.PREF_HINTERGRUND, Einstellungen.schalter(!stil.hintergrund))
            }
        }
        item { TvFuss(tx(R.string.gilt_im_browser_und_in)) }

        item { TvGruppe(tx(R.string.privatsphaere)) }
        item {
            TvEinstellung(tx(R.string.bewertungen_im_haushalt_zeigen), schalter = Einstellungen.bewertungenTeilen(prefs)) {
                onPref(Einstellungen.BEWERTUNGEN_TEILEN,
                       Einstellungen.schalter(!Einstellungen.bewertungenTeilen(prefs)))
            }
        }
        item {
            TvFuss(tx(R.string.dein_verlauf_und_deine_bewertungen))
        }

        item { TvGruppe(tx(R.string.profilbild)) }
        item {
            LazyRow(horizontalArrangement = Arrangement.spacedBy(14.dp),
                    contentPadding = PaddingValues(vertical = 8.dp, horizontal = 4.dp)) {
                items(Profilbild.FARBEN, key = { it }) { farbe ->
                    TvBildWahl(a.avatar == farbe, { profilbild(farbe) }) { TvAvatar(a.initial, farbe, 56.dp) }
                }
                items(Figuren.angeboten(ich.figuren), key = { it.name }) { f ->
                    val schluessel = "fig-${f.name}"
                    TvBildWahl(a.avatar == schluessel, { profilbild(schluessel) }) {
                        TvAvatar(a.initial, schluessel, 56.dp)
                    }
                }
            }
        }
        item { TvFuss(tx(R.string.eine_farbe_oder_eine_figur)) }

        item { TvGruppe(tx(R.string.verlauf)) }
        item { TvEinstellung(tx(R.string.verlauf_loeschen_2), farbe = Tv.bad) { verlaufFrage = true } }
        item { TvFuss(tx(R.string.entfernt_alle_fortschritte_und_haken)) }

        item { TvGruppe(tx(R.string.geraete)) }
        val liste = geraete
        if (liste == null) {
            item { TvLadeZeile() }
        } else {
            items(liste.size) { i -> TvEinstellung(liste[i].name.ifBlank { tx(R.string.ohne_namen) }, wert = liste[i].artText) }
        }
        // Ein vergessener Fernseher bei den Eltern, ein Tablet im Hotel: hier
        // lässt es sich selbst beenden. Nur, wenn gerade etwas läuft.
        if (wiedergaben.isNotEmpty()) {
            item {
                Text(tx(R.string.laeuft_gerade_in_deinem_profil), color = Tv.label2, fontSize = 18.sp,
                     modifier = Modifier.padding(start = 20.dp, top = 18.dp, bottom = 6.dp))
            }
            items(wiedergaben.size) { i ->
                val w = wiedergaben[i]
                Row(
                    Modifier.widthIn(max = 980.dp).fillMaxWidth()
                        .padding(horizontal = 20.dp, vertical = 8.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Column(Modifier.weight(1f)) {
                        Text(w.geraet, fontSize = 20.sp, maxLines = 1, overflow = TextOverflow.Ellipsis)
                        Text(w.zeile, color = Tv.label2, fontSize = 16.sp,
                             maxLines = 1, overflow = TextOverflow.Ellipsis)
                    }
                    Spacer(Modifier.width(16.dp))
                    TvButton(tx(R.string.beenden)) { wiedergabeBeenden(w.id) }
                }
            }
        }
        item { TvEinstellung(tx(R.string.ueberall_abmelden_2), farbe = Tv.bad) { ueberallFrage = true } }

        item { TvGruppe(tx(R.string.server)) }
        item { TvEinstellung(tx(R.string.server_wechseln), wert = info.serverName ?: "") { onServerWechseln() } }
        item { TvFuss(tx(R.string.mehrere_server_wechseln_geht_ohne)) }

        item { TvGruppe(tx(R.string.ueber_lumen)) }
        item { TvEinstellung(tx(R.string.fassung), wert = info.version) }
        if (info.license.isNotEmpty()) item { TvEinstellung(tx(R.string.lizenz), wert = info.license) }
        info.sourceUrl?.let { url -> item { TvEinstellung(tx(R.string.quelltext), wert = url) } }
        item {
            TvFuss(tx(R.string.lumen_ist_freie_software_du) +
                       if (info.ffmpeg) "" else tx(R.string.ffmpeg_fehlt_auf_dem_server))
        }
    }

    sprachwahl?.let { key ->
        val wahl = when (key) {
            Sprachen.PREF -> Sprachwahl.wahl(ctx, info.sprachen, info.sprache)
            Spuren.PREF_TON -> Einstellungen.TON_WAHL
            else -> Einstellungen.UNTERTITEL_WAHL
        }
        val aktuell = prefs[key]?.trim().orEmpty().ifEmpty { if (key == Spuren.PREF_UNTERTITEL) Spuren.AUS else "" }
        TvAuswahlDialog(
            titel = when (key) {
                Sprachen.PREF -> Sprachwahl.titel(ctx)
                Spuren.PREF_TON -> tx(R.string.tonsprache)
                else -> tx(R.string.untertitel)
            },
            wahl = wahl, aktuell = aktuell,
            onWahl = { wert -> sprachwahl = null; onPref(key, wert) },
            onClose = { sprachwahl = null })
    }

    if (sperreOffen) {
        TvSperreDialog(client, a.lockKind,
            onFehlt = { sperreUnbekannt = true; sperreOffen = false },
            onFertig = {
                sperreOffen = false
                // Das Konto neu holen, damit „PIN ändern" stimmt.
                scope.launch {
                    oderNull { withContext(Dispatchers.IO) { client.me() } }?.let { onIch(it) }
                }
            },
            onAbbrechen = { sperreOffen = false })
    }

    if (verlaufFrage) {
        TvFrage(tx(R.string.verlauf_loeschen), tx(R.string.alle_fortschritte_und_haken_werden),
                tx(R.string.loeschen), onNein = { verlaufFrage = false }) {
            verlaufFrage = false
            scope.launch {
                hinweis = try {
                    withContext(Dispatchers.IO) { client.clearHistory() }
                    null
                } catch (e: LumenException) {
                    e.message
                }
            }
        }
    }

    if (ueberallFrage) {
        TvFrage(tx(R.string.ueberall_abmelden), tx(R.string.dein_profil_wird_auf_allen),
                tx(R.string.abmelden), onNein = { ueberallFrage = false }) {
            ueberallFrage = false
            scope.launch {
                try {
                    withContext(Dispatchers.IO) { client.signOutEverywhere() }
                    onAbmelden()
                } catch (e: LumenException) {
                    hinweis = e.message
                }
            }
        }
    }
}

/** Die Überschrift einer Gruppe, in gedämpften Grossbuchstaben. */
@Composable
private fun TvGruppe(titel: String) {
    Text(titel.uppercase(), color = Tv.label3, fontSize = 15.sp, fontWeight = FontWeight.SemiBold,
         letterSpacing = 1.5.sp, modifier = Modifier.padding(top = 26.dp, bottom = 8.dp, start = 4.dp))
}

@Composable
private fun TvFuss(text: String) {
    Text(text, color = Tv.label3, fontSize = 15.sp,
         modifier = Modifier.widthIn(max = 900.dp).padding(start = 20.dp, top = 6.dp))
}

/**
 * Eine Zeile der Einstellungen: links die Beschriftung, rechts der Wert
 * oder ein Schalter. Ohne [onClick] nur zum Lesen — fokussierbar bleibt
 * sie trotzdem, damit sich die Seite mit der Fernbedienung blättern lässt.
 */
@Composable
internal fun TvEinstellung(
    text: String,
    wert: String? = null,
    schalter: Boolean? = null,
    farbe: Color = Color.White,
    onClick: (() -> Unit)? = null,
) {
    var fokussiert by remember { mutableStateOf(false) }
    Row(
        Modifier
            .widthIn(max = 980.dp)
            .fillMaxWidth()
            .onFocusChanged { fokussiert = it.isFocused }
            .then(if (onClick != null) Modifier.clickable(role = Role.Button, onClick = onClick) else Modifier.focusable())
            .clip(RoundedCornerShape(14.dp))
            .background(if (fokussiert) Color.White else Color.Transparent)
            .padding(horizontal = 20.dp, vertical = 14.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(text, fontSize = 20.sp, color = if (fokussiert) Color.Black else farbe,
             modifier = Modifier.weight(1f), maxLines = 1, overflow = TextOverflow.Ellipsis)
        if (wert != null) {
            Text(wert, fontSize = 18.sp, color = if (fokussiert) Color(0x99000000) else Tv.label2,
                 maxLines = 1, overflow = TextOverflow.Ellipsis,
                 modifier = Modifier.padding(start = 16.dp).widthIn(max = 520.dp))
        }
        if (schalter != null) {
            Spacer(Modifier.width(16.dp))
            TvSchalter(schalter, fokussiert)
        }
    }
}

/** Ein Schalter wie am Telefon: grün und rechts, wenn an. */
@Composable
private fun TvSchalter(an: Boolean, fokussiert: Boolean) {
    Box(
        Modifier.width(56.dp).height(32.dp).clip(CircleShape)
            .background(if (an) Tv.good else if (fokussiert) Color(0x33000000) else Color(0x33FFFFFF))
            .padding(3.dp),
        contentAlignment = if (an) Alignment.CenterEnd else Alignment.CenterStart,
    ) {
        Box(Modifier.size(26.dp).clip(CircleShape).background(Color.White))
    }
}

/** Ein Profilbild zum Wählen: rund, das gewählte mit Ring, fokussiert grösser. */
@Composable
private fun TvBildWahl(gewaehlt: Boolean, onClick: () -> Unit, inhalt: @Composable () -> Unit) {
    var fokussiert by remember { mutableStateOf(false) }
    Box(
        Modifier
            .size(70.dp)
            .onFocusChanged { fokussiert = it.isFocused }
            .clickable(role = Role.Button, onClick = onClick)
            .clip(CircleShape)
            .border(if (fokussiert) 4.dp else if (gewaehlt) 3.dp else 0.dp,
                    if (fokussiert) Color.White else if (gewaehlt) Tv.accent else Color.Transparent,
                    CircleShape)
            .padding(7.dp),
        contentAlignment = Alignment.Center,
    ) { inhalt() }
}

/** Eine Auswahl in einem eigenen Fenster: der Haken am gewählten, dort steht auch der Fokus. */
@Composable
internal fun TvAuswahlDialog(
    titel: String,
    wahl: List<Pair<String, String>>,
    aktuell: String,
    onWahl: (String) -> Unit,
    onClose: () -> Unit,
) {
    val anfang = remember { FocusRequester() }
    LaunchedEffect(Unit) {
        delay(100)
        runCatching { anfang.requestFocus() }
    }
    val gewaehlt = wahl.indexOfFirst { it.first == aktuell }.coerceAtLeast(0)
    TvDialogRahmen(onDismiss = onClose) {
        Text(titel, fontSize = 30.sp, fontWeight = FontWeight.Bold)
        Spacer(Modifier.height(16.dp))
        Column(Modifier.heightIn(max = 460.dp).verticalScroll(rememberScrollState())) {
            wahl.forEachIndexed { i, (wert, name) ->
                TvZeile(text = name, rechts = if (wert == aktuell) "✓" else "",
                        modifier = if (i == gewaehlt) Modifier.focusRequester(anfang) else Modifier) {
                    onWahl(wert)
                }
            }
        }
    }
}

// ---------------------------------------------------------------- Einladung

/**
 * Eine Einladung einlösen: Code, Name und wie man sich anmeldet — mit
 * Passwort, PIN oder ohne. Am Fernseher ist der Code einfacher als der
 * Link; der Browser zeigt ihn deshalb neben dem Link an.
 */
@Composable
internal fun TvEinladung(
    fehler: String?,
    onAbbrechen: () -> Unit,
    onEinloesen: suspend (String, String, String, String) -> Unit,
) {
    var code by remember { mutableStateOf("") }
    var name by remember { mutableStateOf("") }
    var art by remember { mutableStateOf("password") }
    var geheim by remember { mutableStateOf("") }
    var senden by remember { mutableStateOf(false) }
    val anfang = remember { FocusRequester() }

    LaunchedEffect(senden) { if (senden) { onEinloesen(code, name, art, geheim); senden = false } }
    LaunchedEffect(Unit) {
        delay(120)
        runCatching { anfang.requestFocus() }
    }

    Mitte {
        Column(horizontalAlignment = Alignment.CenterHorizontally,
               modifier = Modifier.verticalScroll(rememberScrollState()).padding(vertical = 32.dp)) {
            Text(tx(R.string.du_bist_eingeladen), fontSize = 42.sp, fontWeight = FontWeight.Bold)
            Spacer(Modifier.height(10.dp))
            Text(tx(R.string.leg_dir_ein_profil_an),
                 color = Tv.label2, fontSize = 18.sp, textAlign = TextAlign.Center,
                 modifier = Modifier.widthIn(max = 720.dp))
            Spacer(Modifier.height(26.dp))
            OutlinedTextField(value = code, onValueChange = { code = it }, singleLine = true,
                              placeholder = { Text(tx(R.string.einladungscode)) },
                              textStyle = TextStyle(fontSize = 22.sp),
                              modifier = Modifier.width(480.dp).focusRequester(anfang))
            Spacer(Modifier.height(12.dp))
            OutlinedTextField(value = name, onValueChange = { name = it }, singleLine = true,
                              placeholder = { Text(tx(R.string.dein_name)) },
                              textStyle = TextStyle(fontSize = 22.sp),
                              modifier = Modifier.width(480.dp))
            Spacer(Modifier.height(18.dp))
            Text(tx(R.string.anmelden_mit), color = Tv.label2, fontSize = 16.sp)
            Spacer(Modifier.height(8.dp))
            Row(horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                Einladung.ARTEN.forEach { (wert, text) ->
                    TvWahlPille(text, art == wert) { art = wert; geheim = "" }
                }
            }
            Spacer(Modifier.height(14.dp))
            if (art != "none") {
                OutlinedTextField(value = geheim, onValueChange = { geheim = it }, singleLine = true,
                                  placeholder = { Text(if (art == "pin") tx(R.string.pin) else tx(R.string.passwort)) },
                                  visualTransformation = if (art == "none") VisualTransformation.None
                                                         else PasswordVisualTransformation(),
                                  textStyle = TextStyle(fontSize = 22.sp),
                                  modifier = Modifier.width(480.dp))
                Spacer(Modifier.height(6.dp))
            }
            Text(Einladung.hinweis(art), color = Tv.label3, fontSize = 15.sp,
                 textAlign = TextAlign.Center, modifier = Modifier.widthIn(max = 600.dp))
            if (fehler != null) {
                Spacer(Modifier.height(12.dp))
                Text(fehler, color = Tv.bad, fontSize = 17.sp)
            }
            Spacer(Modifier.height(22.dp))
            Row(horizontalArrangement = Arrangement.spacedBy(18.dp)) {
                TvButton(tx(R.string.profil_anlegen)) { senden = true }
                TvButton(tx(R.string.zur_uebersicht)) { onAbbrechen() }
            }
        }
    }
}

/**
 * PIN oder Passwort am Fernseher ändern: bisheriges, Art, neues zweimal.
 * Dieselben Regeln und Meldungen wie am Telefon (Profilschutz in lumen-core).
 */
@Composable
private fun TvSperreDialog(client: ApiClient, bisher: String, onFehlt: () -> Unit,
                           onFertig: () -> Unit, onAbbrechen: () -> Unit) {
    var alt by remember { mutableStateOf("") }
    var art by remember { mutableStateOf(if (bisher == "none") "pin" else bisher) }
    var neu by remember { mutableStateOf("") }
    var wiederholt by remember { mutableStateOf("") }
    var fehler by remember { mutableStateOf<String?>(null) }
    var sendet by remember { mutableStateOf(false) }
    var fertig by remember { mutableStateOf(false) }
    val scope = rememberCoroutineScope()
    val anfang = remember { FocusRequester() }
    LaunchedEffect(Unit) { kotlinx.coroutines.delay(100); runCatching { anfang.requestFocus() } }

    TvDialogRahmen(onDismiss = onAbbrechen) {
        Text(tx(R.string.profil_schuetzen), fontSize = 28.sp, fontWeight = FontWeight.Bold)
        Spacer(Modifier.height(16.dp))
        if (fertig) {
            Text(tx(R.string.gespeichert_ab_jetzt_gilt_der), color = Tv.label2, fontSize = 18.sp)
            Spacer(Modifier.height(20.dp))
            TvButton(tx(R.string.fertig), modifier = Modifier.focusRequester(anfang)) { onFertig() }
            return@TvDialogRahmen
        }
        if (bisher != "none") {
            androidx.compose.material3.OutlinedTextField(
                value = alt, onValueChange = { alt = it; fehler = null }, singleLine = true,
                visualTransformation = androidx.compose.ui.text.input.PasswordVisualTransformation(),
                placeholder = { Text(if (bisher == "pin") tx(R.string.bisheriger_pin) else tx(R.string.bisheriges_passwort)) },
                modifier = Modifier.width(420.dp).focusRequester(anfang))
            Spacer(Modifier.height(12.dp))
        }
        Row(horizontalArrangement = Arrangement.spacedBy(10.dp),
            modifier = if (bisher == "none") Modifier.focusRequester(anfang) else Modifier) {
            Profilschutz.ARTEN.forEach { (wert, text) ->
                TvWahlPille(text, art == wert) { art = wert; neu = ""; wiederholt = ""; fehler = null }
            }
        }
        Spacer(Modifier.height(12.dp))
        if (art != "none") {
            androidx.compose.material3.OutlinedTextField(
                value = neu, onValueChange = { neu = it; fehler = null }, singleLine = true,
                visualTransformation = androidx.compose.ui.text.input.PasswordVisualTransformation(),
                placeholder = { Text(if (art == "pin") tx(R.string.neuer_pin) else tx(R.string.neues_passwort)) },
                modifier = Modifier.width(420.dp))
            Spacer(Modifier.height(12.dp))
            androidx.compose.material3.OutlinedTextField(
                value = wiederholt, onValueChange = { wiederholt = it; fehler = null }, singleLine = true,
                visualTransformation = androidx.compose.ui.text.input.PasswordVisualTransformation(),
                placeholder = { Text(tx(R.string.noch_einmal)) },
                modifier = Modifier.width(420.dp))
        } else {
            Text(Einladung.hinweis("none"), color = Tv.label2, fontSize = 16.sp)
        }
        fehler?.let {
            Spacer(Modifier.height(12.dp))
            Text(it, color = Tv.bad, fontSize = 16.sp)
        }
        Spacer(Modifier.height(20.dp))
        Row(horizontalArrangement = Arrangement.spacedBy(18.dp)) {
            TvButton(if (sendet) tx(R.string.einen_moment) else tx(R.string.speichern)) {
                if (sendet) return@TvButton
                fehler = Profilschutz.pruefen(bisher, alt, art, neu, wiederholt)
                if (fehler != null) return@TvButton
                sendet = true
                scope.launch {
                    try {
                        withContext(Dispatchers.IO) { client.sperreAendern(alt, art, if (art == "none") "" else neu) }
                        fertig = true
                    } catch (e: LumenException) {
                        if (Profilschutz.fehltBeimServer(e)) onFehlt() else fehler = Profilschutz.meldung(e)
                    }
                    sendet = false
                }
            }
            TvButton(tx(R.string.abbrechen)) { onAbbrechen() }
        }
    }
}
