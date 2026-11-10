// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package org.lumen.app

import org.lumen.texte.Sprachwahl
import android.content.Intent
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.BackHandler
import androidx.activity.compose.setContent
import androidx.annotation.StringRes
import androidx.activity.enableEdgeToEdge
import androidx.activity.viewModels
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.grid.GridCells
import androidx.compose.foundation.lazy.grid.LazyVerticalGrid
import androidx.compose.foundation.lazy.grid.items
import androidx.compose.foundation.lazy.grid.rememberLazyGridState
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.*
import androidx.compose.material.icons.outlined.Notifications
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.listSaver
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import org.lumen.core.*

class MainActivity : ComponentActivity() {

    /** Dasselbe ViewModel, das die Oberfläche benutzt — auch für onNewIntent. */
    private val model: LumenViewModel by viewModels {
        LumenViewModel.Factory((application as LumenApplication).store)
    }

    // Jede Activity spricht die Sprache, die Sprachwahl festhält.
    override fun attachBaseContext(newBase: android.content.Context) {
        super.attachBaseContext(Sprachwahl.kontext(newBase))
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        // Nur beim ersten Start: nach einer Wiederherstellung ist der Link
        // schon verarbeitet worden.
        if (savedInstanceState == null) inviteCode(intent)?.let { model.pendingInvite = it }

        setContent {
            LumenTheme {
                val model = model
                // Der Bildlader fragt diesen Client nach einem neuen
                // Medienschlüssel, wenn ein Bild mit 401 zurückkommt.
                SideEffect { (application as LumenApplication).client = model.client }
                // Die Sprache folgt dem Profil — vor der Anmeldung dem Server.
                val stand by model.stage.collectAsStateWithLifecycle()
                val sprache = remember(stand) { model.sprache }
                val angemeldet = stand is Stage.SignedIn
                LaunchedEffect(sprache, angemeldet) {
                    Sprachwahl.abgleichen(this@MainActivity, sprache, angemeldet) { wunsch ->
                        model.send(onDone = { model.refreshMe() }) {
                            it.updatePrefs(mapOf(Sprachen.PREF to wunsch))
                        }
                    }
                }
                Surface(Modifier.fillMaxSize(), color = Lumen.background) {
                    Root(model)
                }
                // Konnte die Anmeldung nicht verschlüsselt gespeichert
                // werden, soll man das wissen — statt dass Lumen still auf
                // Klartext ausweicht.
                val anwendung = application as LumenApplication
                var speicherHinweis by remember { mutableStateOf(anwendung.speicherHinweis) }
                speicherHinweis?.let { text ->
                    AlertDialog(
                        onDismissRequest = { speicherHinweis = null; anwendung.speicherHinweis = null },
                        title = { Text(tx(R.string.anmeldung_nicht_dauerhaft_gespeichert)) },
                        text = { Text(text) },
                        confirmButton = {
                            TextButton(onClick = { speicherHinweis = null; anwendung.speicherHinweis = null }) {
                                Text(tx(R.string.verstanden))
                            }
                        },
                    )
                }
            }
        }
    }

    /** Ob gerade ein Film läuft, der beim Verlassen ins Bild-im-Bild darf. */
    var bildImBild: Boolean = false

    /**
     * Tasten für den Player (Tastatur, Gamepad). Hier statt in Compose:
     * solange nichts in Compose den Fokus hat, kämen die Tasten dort nie an.
     */
    var tastenGriff: ((android.view.KeyEvent) -> Boolean)? = null

    // onKeyDown statt dispatchKeyEvent (das ist bei ComponentActivity eine
    // eingeschränkte Schnittstelle): Was keine Ansicht nimmt, landet hier.
    override fun onKeyDown(keyCode: Int, event: android.view.KeyEvent): Boolean =
        tastenGriff?.invoke(event) == true || super.onKeyDown(keyCode, event)

    override fun onUserLeaveHint() {
        super.onUserLeaveHint()
        // Zum Startbildschirm gewechselt, während ein Film läuft: im
        // kleinen Fenster weiter, statt anzuhalten.
        if (bildImBild && packageManager.hasSystemFeature(
                android.content.pm.PackageManager.FEATURE_PICTURE_IN_PICTURE)) {
            try {
                enterPictureInPictureMode(bildImBildParameter())
            } catch (e: IllegalStateException) {
                // Manche Geräte lehnen es trotz Merkmal ab — dann hält der
                // Film eben an, wie beim gewöhnlichen Verlassen.
            }
        }
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        setIntent(intent)
        // Ein Einladungslink, während Lumen schon läuft: früher kam der nie an.
        inviteCode(intent)?.let { model.pendingInvite = it }
    }

    /** `lumen://einladung/ABC` oder ein Link mit `?einladung=ABC` */
    private fun inviteCode(intent: Intent?): String? {
        val data = intent?.data ?: return null
        if (data.scheme == "lumen" && data.host == "einladung") {
            return data.lastPathSegment
        }
        return data.getQueryParameter("einladung")
    }
}

@Composable
private fun Root(model: LumenViewModel) {
    val stage by model.stage.collectAsStateWithLifecycle()
    // Eine Einladung: aus einem Link (lumen://einladung/…) oder von Hand
    // eingetippt. Null heisst: keine offen. Ein leerer Code heisst: das
    // Formular ist offen, der Code fehlt noch.
    var einladung by remember { mutableStateOf<String?>(null) }
    // Mit einem Code von einem anderen Gerät anmelden — aus der Profilwahl.
    var mitCode by remember { mutableStateOf(false) }
    val wartendeEinladung = model.pendingInvite
    LaunchedEffect(stage, wartendeEinladung) {
        // Eingelöst: das Formular ist erledigt — auch nach einem späteren
        // Abmelden soll es nicht wieder aufgehen.
        if (stage is Stage.SignedIn) { einladung = null; mitCode = false }
        // Angemeldet bleibt der Code liegen, bis die Frage unten beantwortet ist.
        if (wartendeEinladung != null && stage !is Stage.StartingUp && stage !is Stage.SignedIn) {
            einladung = wartendeEinladung
            model.pendingInvite = null
        }
    }
    // Ein Einladungslink, während jemand angemeldet ist: Eine Einladung legt
    // ein neues Profil an — das geht nur von der Profilwahl aus.
    if (wartendeEinladung != null && stage is Stage.SignedIn) {
        AlertDialog(
            onDismissRequest = { model.pendingInvite = null },
            title = { Text(tx(R.string.einladung_erhalten)) },
            text = { Text(tx(R.string.mit_einer_einladung_legst_du)) },
            confirmButton = {
                TextButton(onClick = { model.signOut() }) { Text(tx(R.string.abmelden_und_einloesen)) }
            },
            dismissButton = {
                TextButton(onClick = { model.pendingInvite = null }) { Text(tx(R.string.abbrechen)) }
            },
        )
    }
    val offeneEinladung = einladung
    if (offeneEinladung != null && (stage is Stage.ChoosingProfile || stage is Stage.Unlocking)) {
        BackHandler { einladung = null; model.fehlerWeg() }
        EinladungScreen(model, offeneEinladung) { einladung = null; model.fehlerWeg() }
        return
    }
    if (mitCode && stage is Stage.ChoosingProfile) {
        CodeAnmeldenScreen(model) { mitCode = false }
        return
    }
    when (val s = stage) {
        is Stage.StartingUp -> Box(Modifier.fillMaxSize(), Alignment.Center) {
            CircularProgressIndicator(color = Lumen.label2)
        }
        is Stage.NeedsServer -> ServerScreen(model, s.message)
        is Stage.ChoosingServer -> ServerListeScreen(model, s)
        is Stage.NeedsSetup -> FirstRunScreen(model)
        // Von unterwegs gibt der Server keine Profilauswahl heraus — dann
        // Name und PIN, mit dem Code als zweitem Weg.
        is Stage.ChoosingProfile -> if (s.unterwegs) {
            UnterwegsScreen(model, onMitCode = { mitCode = true }) { einladung = "" }
        } else {
            ProfilesScreen(model, s.profiles, onMitCode = { mitCode = true }) { einladung = "" }
        }
        is Stage.Unlocking -> UnlockScreen(model, s.profile)
        is Stage.SignedIn -> LibraryScaffold(model)
    }
}

// ---------------------------------------------------------------- Einrichten

@Composable
private fun ServerScreen(model: LumenViewModel, message: String?) {
    var address by remember { mutableStateOf("") }
    var fingerabdruck by remember { mutableStateOf("") }
    var vonAussen by remember { mutableStateOf(false) }
    val busy by model.busy.collectAsStateWithLifecycle()

    Column(
        Modifier.fillMaxSize().padding(28.dp),
        verticalArrangement = Arrangement.Center,
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Text("Lumen", fontSize = 34.sp, fontWeight = FontWeight.Bold)
        Spacer(Modifier.height(10.dp))
        Text(
            tx(R.string.gib_die_adresse_deines_servers),
            color = Lumen.label2,
            textAlign = androidx.compose.ui.text.style.TextAlign.Center,
        )
        Spacer(Modifier.height(24.dp))
        Field(tx(R.string.adresse), address, { address = it }, hint = "lumen.local:8097")

        // Von unterwegs läuft die Verbindung verschlüsselt, und ein
        // Heimserver stellt sein Zertifikat meist selbst aus. Dann braucht
        // die App einmal dessen Fingerabdruck — der steht in der Verwaltung
        // unter „Von aussen erreichbar". Im Heimnetz braucht ihn niemand,
        // darum ist das hier zugeklappt.
        Spacer(Modifier.height(12.dp))
        TextButton(onClick = { vonAussen = !vonAussen }) {
            Text(if (vonAussen) tx(R.string.von_unterwegs_verbinden_2) else tx(R.string.von_unterwegs_verbinden),
                 color = Lumen.label2, fontSize = 14.sp)
        }
        if (vonAussen) {
            Text(
                tx(R.string.trag_die_adresse_deines_vermittlers),
                color = Lumen.label3, fontSize = 13.sp,
                textAlign = androidx.compose.ui.text.style.TextAlign.Center,
                modifier = Modifier.padding(bottom = 10.dp))
            Field(tx(R.string.fingerabdruck), fingerabdruck, { fingerabdruck = it },
                  hint = tx(R.string.t_64_zeichen_aus_09_und))
            if (fingerabdruck.isNotBlank() && Pinnen.saubern(fingerabdruck) == null) {
                Text(tx(R.string.das_sind_nicht_64_zeichen),
                     color = Lumen.bad, fontSize = 13.sp,
                     modifier = Modifier.padding(top = 6.dp))
            }
        }

        if (message != null) {
            Spacer(Modifier.height(10.dp))
            Text(message, color = Lumen.bad, fontSize = 14.sp)
        }
        Spacer(Modifier.height(22.dp))
        if (busy) CircularProgressIndicator(color = Lumen.label2)
        else PrimaryButton(tx(R.string.verbinden), address.isNotBlank()) {
            model.useServer(address, Pinnen.saubern(fingerabdruck))
        }
        // Wer schon Server gespeichert hat, kommt hier zurück zur Liste.
        if (model.server.isNotEmpty()) {
            BackHandler(enabled = !busy) { model.serverWahl() }
            Spacer(Modifier.height(12.dp))
            TextButton(onClick = { model.serverWahl() }, enabled = !busy) {
                Text(tx(R.string.zurueck_zu_den_servern), color = Lumen.label2)
            }
        }
    }
}

@Composable
private fun FirstRunScreen(model: LumenViewModel) {
    var name by remember { mutableStateOf("") }
    var password by remember { mutableStateOf("") }
    val error by model.error.collectAsStateWithLifecycle()
    val busy by model.busy.collectAsStateWithLifecycle()

    Column(
        Modifier.fillMaxSize().padding(28.dp).verticalScrollable(),
        verticalArrangement = Arrangement.Center,
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Text(tx(R.string.willkommen_bei_lumen), fontSize = 28.sp, fontWeight = FontWeight.Bold)
        Spacer(Modifier.height(10.dp))
        Text(
            tx(R.string.du_richtest_diesen_server_ein),
            color = Lumen.label2,
            textAlign = androidx.compose.ui.text.style.TextAlign.Center,
        )
        Spacer(Modifier.height(24.dp))
        Field(tx(R.string.dein_name), name, { name = it })
        Spacer(Modifier.height(14.dp))
        Field(tx(R.string.passwort), password, { password = it }, secret = true,
              hint = tx(R.string.mindestens_acht_zeichen_damit_meldest))
        if (error != null) {
            Spacer(Modifier.height(10.dp))
            Text(error!!, color = Lumen.bad, fontSize = 14.sp)
        }
        Spacer(Modifier.height(22.dp))
        if (busy) CircularProgressIndicator(color = Lumen.label2)
        else PrimaryButton(tx(R.string.server_einrichten),
            name.isNotBlank() && password.length >= 8) { model.setUpOwner(name, password) }
        Spacer(Modifier.height(12.dp))
        TextButton(onClick = { model.serverWahl() }, enabled = !busy) {
            Text(tx(R.string.anderer_server), color = Lumen.label2)
        }
    }
}

@Composable
private fun ProfilesScreen(model: LumenViewModel, profiles: List<ProfileCard>,
                           onMitCode: () -> Unit,
                           onEinladung: () -> Unit) {
    Column(
        Modifier.fillMaxSize().padding(28.dp),
        verticalArrangement = Arrangement.Center,
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Text(tx(R.string.wer_schaut), fontSize = 28.sp, fontWeight = FontWeight.Bold)
        Spacer(Modifier.height(32.dp))
        LazyVerticalGrid(
            columns = GridCells.Adaptive(120.dp),
            horizontalArrangement = Arrangement.spacedBy(20.dp),
            verticalArrangement = Arrangement.spacedBy(24.dp),
            modifier = Modifier.fillMaxWidth().weight(1f, fill = false),
        ) {
            items(profiles, key = { it.id }) { profile ->
                Column(
                    Modifier.clickableRow { model.chooseProfile(profile) },
                    horizontalAlignment = Alignment.CenterHorizontally,
                ) {
                    Avatar(profile.initial, profile.avatar, 88.dp,
                           model.profilbildUrl(profile.id, profile.avatar))
                    Spacer(Modifier.height(10.dp))
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        Text(profile.name)
                        if (profile.needsCode) {
                            Spacer(Modifier.width(4.dp))
                            Icon(Icons.Filled.Lock, null,
                                 tint = Lumen.label3, modifier = Modifier.size(12.dp))
                        }
                    }
                }
            }
        }
        Spacer(Modifier.height(24.dp))
        // Wer einen Code bekommen hat, legt sich hier sein Profil an —
        // einfacher als einen Link aufs Telefon zu bringen.
        TextButton(onClick = onEinladung) {
            Text(tx(R.string.ich_habe_eine_einladung), color = Lumen.label2)
        }
        // Ohne Passwort: ein anderes, schon angemeldetes Gerät gibt frei.
        TextButton(onClick = onMitCode) {
            Text(tx(R.string.mit_code_von_einem_anderen), color = Lumen.label2)
        }
        TextButton(onClick = { model.serverWahl() }) {
            Text(model.aktiverServer?.let { tx(R.string.server_wechseln_x, it.name) } ?: tx(R.string.server_wechseln),
                 color = Lumen.label3, fontSize = 14.sp)
        }
    }
}

/**
 * Von unterwegs anmelden. Der Server zeigt von aussen keine Profilauswahl —
 * sie verriete, wer im Haushalt wohnt, oft auch die Namen der Kinder. Also
 * tippt man den Namen seines Profils ein, dazu PIN oder Passwort. Ein
 * Profil ohne beides lässt sich von unterwegs nicht öffnen; dafür gibt es
 * den Code von einem Gerät, das schon angemeldet ist.
 */
@Composable
private fun UnterwegsScreen(model: LumenViewModel,
                            onMitCode: () -> Unit,
                            onEinladung: () -> Unit) {
    var name by remember { mutableStateOf("") }
    var geheim by remember { mutableStateOf("") }
    val error by model.error.collectAsStateWithLifecycle()
    val busy by model.busy.collectAsStateWithLifecycle()

    Column(
        Modifier.fillMaxSize().padding(28.dp).verticalScrollable(),
        verticalArrangement = Arrangement.Center,
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Text(tx(R.string.von_unterwegs_anmelden), fontSize = 28.sp, fontWeight = FontWeight.Bold,
             textAlign = androidx.compose.ui.text.style.TextAlign.Center)
        Spacer(Modifier.height(10.dp))
        Text(
            tx(R.string.von_ausserhalb_deines_heimnetzes_zeigt),
            color = Lumen.label2,
            textAlign = androidx.compose.ui.text.style.TextAlign.Center,
        )
        Spacer(Modifier.height(24.dp))
        Column(Modifier.widthIn(max = 420.dp), verticalArrangement = Arrangement.spacedBy(14.dp)) {
            Field(tx(R.string.profilname), name, { name = it })
            Field(tx(R.string.pin_oder_passwort), geheim, { geheim = it }, secret = true)
        }
        // Die Meldung des Servers, wie sie kommt — auch die nach zu vielen
        // Versuchen, mit der Wartezeit darin.
        if (error != null) {
            Spacer(Modifier.height(10.dp))
            Text(error!!, color = Lumen.bad, fontSize = 14.sp,
                 textAlign = androidx.compose.ui.text.style.TextAlign.Center)
        }
        Spacer(Modifier.height(22.dp))
        if (busy) CircularProgressIndicator(color = Lumen.label2)
        else PrimaryButton(tx(R.string.anmelden), name.isNotBlank() && geheim.isNotEmpty()) {
            model.unlockByName(name, geheim)
        }
        Spacer(Modifier.height(16.dp))
        // Ohne Passwort, oder für ein Profil ohne PIN: ein anderes,
        // schon angemeldetes Gerät gibt frei.
        TextButton(onClick = { model.fehlerWeg(); onMitCode() }) {
            Text(tx(R.string.mit_code_von_einem_anderen), color = Lumen.label2)
        }
        TextButton(onClick = { model.fehlerWeg(); onEinladung() }) {
            Text(tx(R.string.ich_habe_eine_einladung), color = Lumen.label2)
        }
        TextButton(onClick = { model.serverWahl() }) {
            Text(model.aktiverServer?.let { tx(R.string.server_wechseln_x, it.name) } ?: tx(R.string.server_wechseln),
                 color = Lumen.label3, fontSize = 14.sp)
        }
    }
}

/**
 * Eine Einladung einlösen: Code, Name und wie man sich anmeldet — mit
 * Passwort, PIN oder ohne. Dieselben Regeln wie im Browser; was offensichtlich
 * nicht passt, meldet die App, bevor der Code beim Server verbraucht wird.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun EinladungScreen(model: LumenViewModel, vorgabe: String, onBack: () -> Unit) {
    var code by remember { mutableStateOf(vorgabe) }
    var name by remember { mutableStateOf("") }
    var art by remember { mutableStateOf("password") }
    var geheim by remember { mutableStateOf("") }
    val error by model.error.collectAsStateWithLifecycle()
    val busy by model.busy.collectAsStateWithLifecycle()

    Column(
        Modifier.fillMaxSize().padding(28.dp).verticalScrollable(),
        verticalArrangement = Arrangement.Center,
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Text(tx(R.string.du_bist_eingeladen), fontSize = 28.sp, fontWeight = FontWeight.Bold)
        Spacer(Modifier.height(10.dp))
        Text(
            tx(R.string.leg_dir_ein_profil_an),
            color = Lumen.label2,
            textAlign = androidx.compose.ui.text.style.TextAlign.Center,
        )
        Spacer(Modifier.height(24.dp))
        Column(Modifier.widthIn(max = 420.dp), verticalArrangement = Arrangement.spacedBy(14.dp)) {
            Field(tx(R.string.einladungscode), code, { code = it })
            Field(tx(R.string.dein_name), name, { name = it })
            Text(tx(R.string.anmelden_mit), color = Lumen.label2, fontSize = 13.sp)
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                Einladung.ARTEN.forEach { (wert, text) ->
                    FilterChip(
                        selected = art == wert,
                        onClick = { art = wert; geheim = "" },
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
                Text(Einladung.hinweis(art), color = Lumen.label3, fontSize = 13.sp)
            } else {
                Field(if (art == "pin") tx(R.string.pin) else tx(R.string.passwort), geheim, { geheim = it },
                      secret = true, hint = Einladung.hinweis(art))
            }
        }
        if (error != null) {
            Spacer(Modifier.height(10.dp))
            Text(error!!, color = Lumen.bad, fontSize = 14.sp)
        }
        Spacer(Modifier.height(22.dp))
        if (busy) CircularProgressIndicator(color = Lumen.label2)
        else PrimaryButton(tx(R.string.profil_anlegen), code.isNotBlank() && name.isNotBlank()) {
            model.redeemInvite(code, name, art, geheim)
        }
        Spacer(Modifier.height(12.dp))
        TextButton(onClick = onBack) { Text(tx(R.string.zur_uebersicht), color = Lumen.label2) }
    }
}

@Composable
private fun UnlockScreen(model: LumenViewModel, profile: ProfileCard) {
    var secret by remember { mutableStateOf("") }
    val error by model.error.collectAsStateWithLifecycle()
    val busy by model.busy.collectAsStateWithLifecycle()

    Column(
        Modifier.fillMaxSize().padding(28.dp),
        verticalArrangement = Arrangement.Center,
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Avatar(profile.initial, profile.avatar, 84.dp, model.profilbildUrl(profile.id, profile.avatar))
        Spacer(Modifier.height(14.dp))
        Text(profile.name, fontSize = 20.sp, fontWeight = FontWeight.SemiBold)
        Spacer(Modifier.height(20.dp))
        Field(if (profile.lockKind == "pin") tx(R.string.pin) else tx(R.string.passwort),
              secret, { secret = it }, secret = true,
              modifier = Modifier.widthIn(max = 280.dp))
        if (error != null) {
            Spacer(Modifier.height(10.dp))
            Text(error!!, color = Lumen.bad, fontSize = 14.sp)
        }
        Spacer(Modifier.height(20.dp))
        if (busy) CircularProgressIndicator(color = Lumen.label2)
        else PrimaryButton(tx(R.string.weiter), secret.isNotEmpty()) { model.unlock(profile, secret) }
        Spacer(Modifier.height(12.dp))
        TextButton(onClick = { model.signOut() }) {
            Text(tx(R.string.anderes_profil), color = Lumen.label2)
        }
    }
}

// ---------------------------------------------------------------- Mediathek

private enum class Tab(@StringRes private val titelId: Int, val icon: androidx.compose.ui.graphics.vector.ImageVector) {
    Home(R.string.startseite, Icons.Filled.Home),
    Movies(R.string.filme, Icons.Filled.Movie),
    Series(R.string.serien, Icons.Filled.Tv),
    Watchlist(R.string.merkliste, Icons.Filled.Bookmark),
    // Ein eigener Reiter wie im Browser: „mag ich" ist etwas anderes als
    // „will ich noch sehen" — und soll nicht hinter der Merkliste stecken.
    Favorites(R.string.favoriten, Icons.Filled.Favorite),
    Profile(R.string.profil, Icons.Filled.Person);

    /** Der Name in der Sprache, die gerade gilt — ein Enum entsteht nur einmal. */
    val title: String get() = tx(titelId)
}

/**
 * Was über den Reitern offen ist. Ein Stapel statt einer einzelnen
 * Titelseite: aus einer Reihe geht es zu einem Film, von dort zum nächsten
 * Film der Reihe — und „Zurück" soll jeden Schritt einzeln zurückgehen.
 */
private sealed interface Ziel {
    data class Titel(val id: String) : Ziel
    data class ReiheOffen(val id: String) : Ziel
    data class ListeOffen(val id: String) : Ziel
    /** Die Suche, und was dahinter liegt: alles mit einer Person, ein Genre. */
    data object Suche : Ziel
    data class Person(val name: String) : Ziel
    data class Genre(val name: String) : Ziel
    /** Die eigenen Neuigkeiten: neue Folgen, neue Filme in Reihen. */
    data object Neuigkeiten : Ziel
    /** Die Wünsche: was ich mir gewünscht habe, und wie es steht. */
    data object Wuensche : Ziel
    /** Einen Fernseher mit dem Code anmelden, der dort steht. */
    data object FernseherAnmelden : Ziel
    /** PIN oder Passwort des eigenen Profils ändern. */
    data object Sperre : Ziel
    /** Was auf diesem Gerät heruntergeladen ist. */
    data object Downloads : Ziel
}

/**
 * Der Stapel als Text — damit er auch übersteht, dass Android die App im
 * Hintergrund beendet und später wiederherstellt. Ein Ziel ist eine Zeile
 * „art:wert"; was sich nicht lesen lässt, fällt weg.
 */
private val ZielStapelSaver = listSaver<List<Ziel>, String>(
    save = { stapel ->
        stapel.map {
            when (it) {
                is Ziel.Titel -> "titel:" + it.id
                is Ziel.ReiheOffen -> "reihe:" + it.id
                is Ziel.ListeOffen -> "liste:" + it.id
                Ziel.Suche -> "suche:"
                is Ziel.Person -> "person:" + it.name
                is Ziel.Genre -> "genre:" + it.name
                Ziel.Neuigkeiten -> "neuigkeiten:"
                Ziel.Wuensche -> "wuensche:"
                Ziel.FernseherAnmelden -> "fernseher:"
                Ziel.Sperre -> "sperre:"
                Ziel.Downloads -> "downloads:"
            }
        }
    },
    restore = { texte ->
        texte.mapNotNull { t ->
            val wert = t.substringAfter(':')
            when (t.substringBefore(':')) {
                "titel" -> Ziel.Titel(wert)
                "reihe" -> Ziel.ReiheOffen(wert)
                "liste" -> Ziel.ListeOffen(wert)
                "suche" -> Ziel.Suche
                "person" -> Ziel.Person(wert)
                "genre" -> Ziel.Genre(wert)
                "neuigkeiten" -> Ziel.Neuigkeiten
                "wuensche" -> Ziel.Wuensche
                "fernseher" -> Ziel.FernseherAnmelden
                "sperre" -> Ziel.Sperre
                "downloads" -> Ziel.Downloads
                else -> null
            }
        }
    },
)

/** Die Wahl über einem Raster, ebenso übers Beenden im Hintergrund hinweg. */
private val RasterWahlSaver = listSaver<RasterWahl, String?>(
    save = { listOf(it.genre, it.sortierung.name, it.buchstabe, if (it.absteigend) "1" else "0", it.mischung.toString()) },
    restore = { l ->
        RasterWahl(l[0], Sortierung.entries.firstOrNull { it.name == l[1] } ?: Sortierung.TITEL, l[2],
                   absteigend = l.getOrNull(3) == "1", mischung = l.getOrNull(4)?.toIntOrNull() ?: 0)
    },
)

/** Die Sortierung ins Profil schreiben (sort.movie, sort.series) — wie im
 *  Browser gilt sie dann beim nächsten Start und auf anderen Geräten. */
private fun sortierungMerken(model: LumenViewModel, kind: String, alt: RasterWahl, neu: RasterWahl) {
    if (alt.sortierung == neu.sortierung && alt.absteigend == neu.absteigend) return
    val wert = neu.sortierung.wert(neu.absteigend)
    model.send { it.updatePrefs(mapOf(Sortierung.prefSchluessel(kind) to wert)) }
}

/** Die Ansichten im Reiter Merkliste. */
private object MerkAnsicht {
    const val MERKLISTE = 0
    const val GESEHEN = 1
    const val LISTEN = 2
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun LibraryScaffold(model: LumenViewModel) {
    // rememberSaveable: Reiter, Stapel und Wahl überstehen auch, dass
    // Android die App im Hintergrund beendet. Was läuft, hält das ViewModel.
    var tab by rememberSaveable { mutableStateOf(Tab.Home) }
    var stapel by rememberSaveable(stateSaver = ZielStapelSaver) { mutableStateOf(emptyList<Ziel>()) }
    // Welche Ansicht innerhalb eines Reiters gilt. Steht hier oben, damit
    // sie nicht verloren geht, wenn eine Titelseite darüber aufgeht.
    var reihenZeigen by rememberSaveable { mutableStateOf(false) }
    var merkAnsicht by rememberSaveable { mutableStateOf(MerkAnsicht.MERKLISTE) }
    // Genre, Sortierung und Buchstabe der Raster — sie bleiben, wenn man
    // von einem Titel zurückkommt.
    // Die Sortierung kommt beim ersten Mal aus dem Profil.
    var filmWahl by rememberSaveable(stateSaver = RasterWahlSaver) {
        mutableStateOf(RasterWahl.aus(model.me?.prefs, "movie"))
    }
    var serienWahl by rememberSaveable(stateSaver = RasterWahlSaver) {
        mutableStateOf(RasterWahl.aus(model.me?.prefs, "series"))
    }
    // Favoriten: Filme oder Serien, wie im Browser getrennt.
    var favoritenSerien by rememberSaveable { mutableStateOf(false) }

    // Die Scrollstellen der Reiter leben hier, nicht in den Ansichten: die
    // verschwinden, sobald eine Titelseite darüber aufgeht, und kämen sonst
    // oben wieder an. Vor jedem frühen return, damit sie auch den Player
    // überstehen.
    val heimListe = rememberLazyListState()
    val filmRaster = rememberLazyGridState()
    val serienRaster = rememberLazyGridState()
    val merkRaster = rememberLazyGridState()
    val favRaster = rememberLazyGridState()
    // Und alles, was die Reiter sonst über rememberSaveable merken (etwa die
    // waagrechten Reihen der Startseite), hebt dieser Halter je Reiter auf.
    val halter = androidx.compose.runtime.saveable.rememberSaveableStateHolder()

    // Der Zähler der Neuigkeiten hängt an /me — gelesen über den Zustand,
    // damit der Punkt verschwindet, sobald die Liste offen war.
    val stage by model.stage.collectAsStateWithLifecycle()
    val ungelesen = (stage as? Stage.SignedIn)?.me?.neuigkeiten ?: 0

    fun oeffnen(ziel: Ziel) { stapel = stapel + ziel }
    fun zurueck() { stapel = stapel.dropLast(1) }
    val titelOeffnen: (Item) -> Unit = { oeffnen(Ziel.Titel(it.id)) }

    model.offline?.let { d ->
        key(d.itemId) { OfflineSpieler(model, d) { model.offline = null } }
        return
    }
    // Ist ein Chromecast verbunden, läuft „Abspielen" dort — das Telefon
    // wird zur Fernbedienung.
    CastBeobachten(model)
    val target = model.abspielen
    if (target != null) {
        if (model.castVerbunden && model.castAbgelehnt != target) {
            key(target) {
                CastWiedergabe(model, target,
                    onHier = { model.castAbgelehnt = target },
                    onClose = { model.abspielen = null })
            }
        } else {
            key(target) { PlayerScreen(model, target) { model.abspielen = null; model.castAbgelehnt = null } }
        }
        return
    }

    // Die Zurück-Geste geht erst den Stapel zurück, dann von einem anderen
    // Reiter zur Startseite — erst von dort schliesst sie die App, wie man
    // es von Android-Apps mit unterer Leiste kennt.
    BackHandler(enabled = stapel.isNotEmpty() || tab != Tab.Home) {
        if (stapel.isNotEmpty()) zurueck() else tab = Tab.Home
    }

    when (val oben = stapel.lastOrNull()) {
        is Ziel.Titel -> {
            DetailScreen(model, oben.id, onBack = { zurueck() },
                         onPlay = { model.abspielen = it },
                         onOpen = titelOeffnen,
                         onPerson = { oeffnen(Ziel.Person(it)) },
                         onReihe = { oeffnen(Ziel.ReiheOffen(it)) },
                         onDownloads = { oeffnen(Ziel.Downloads) })
            return
        }
        is Ziel.ReiheOffen -> {
            ReiheScreen(model, oben.id, onBack = { zurueck() }, onOpen = titelOeffnen,
                        onPlay = { model.abspielen = Abspielen(it) })
            return
        }
        is Ziel.ListeOffen -> {
            ListeScreen(model, oben.id, onBack = { zurueck() }, onOpen = titelOeffnen,
                        onPlay = { name, eintraege, n ->
                            model.abspielen = Abspielen(n.titel, eintraege, n.index, listenName = name)
                        })
            return
        }
        Ziel.Suche -> {
            SuchScreen(model, onBack = { zurueck() }, onOpen = titelOeffnen,
                       onPerson = { oeffnen(Ziel.Person(it)) },
                       onGenre = { oeffnen(Ziel.Genre(it)) })
            return
        }
        is Ziel.Person -> {
            PersonScreen(model, oben.name, onBack = { zurueck() }, onOpen = titelOeffnen)
            return
        }
        is Ziel.Genre -> {
            GenreScreen(model, oben.name, onBack = { zurueck() }, onOpen = titelOeffnen)
            return
        }
        Ziel.Neuigkeiten -> {
            NeuigkeitenScreen(model, onBack = { zurueck() },
                              onOpen = { oeffnen(Ziel.Titel(it)) },
                              onHome = { stapel = emptyList(); tab = Tab.Home })
            return
        }
        Ziel.Wuensche -> {
            WuenscheScreen(model, onBack = { zurueck() })
            return
        }
        Ziel.FernseherAnmelden -> {
            FernseherAnmeldenScreen(model, onBack = { zurueck() })
            return
        }
        Ziel.Sperre -> {
            SperreScreen(model, onBack = { zurueck() })
            return
        }
        Ziel.Downloads -> {
            DownloadsScreen(model, onBack = { zurueck() },
                            onPlay = { model.offline = it })
            return
        }
        null -> Unit
    }

    val inhalt: @Composable () -> Unit = {
        val suchen = { oeffnen(Ziel.Suche) }
        val neuigkeiten = { oeffnen(Ziel.Neuigkeiten) }
        halter.SaveableStateProvider(tab.name) {
        when (tab) {
            Tab.Home -> HomeScreen(model, heimListe, titelOeffnen, suchen, ungelesen, neuigkeiten,
                onReihe = { oeffnen(Ziel.ReiheOffen(it.id)) },
                onReihen = { tab = Tab.Movies; reihenZeigen = true })
            // Filmreihen gehören zu den Filmen, Listen zur Merkliste —
            // die untere Leiste hat schon sechs Reiter, ein siebter
            // würde zu eng. Oben im Reiter wählt man die Ansicht.
            Tab.Movies -> Column {
                Umschalter(tx(R.string.alle_filme), tx(R.string.filmreihen), reihenZeigen) { reihenZeigen = it }
                Box(Modifier.fillMaxSize()) {
                    if (reihenZeigen) {
                        ReihenScreen(model) { oeffnen(Ziel.ReiheOffen(it.id)) }
                    } else {
                        BrowseScreen(model, "movie", filmRaster, filmWahl,
                                     { sortierungMerken(model, "movie", filmWahl, it); filmWahl = it }, titelOeffnen, suchen)
                    }
                }
            }
            Tab.Series -> BrowseScreen(model, "series", serienRaster, serienWahl,
                                       { sortierungMerken(model, "series", serienWahl, it); serienWahl = it },
                                       titelOeffnen, suchen)
            // Merkliste, Gesehen und Listen in einem Reiter — eine Wahl
            // oben, nicht zwei ineinander. Die Favoriten haben ihren
            // eigenen Reiter, wie im Browser.
            Tab.Watchlist -> Column {
                Dreiwahl(listOf(tx(R.string.merkliste), tx(R.string.gesehen), tx(R.string.listen)), merkAnsicht) { merkAnsicht = it }
                Box(Modifier.fillMaxSize()) {
                    when (merkAnsicht) {
                        MerkAnsicht.LISTEN -> ListenScreen(model) { oeffnen(Ziel.ListeOffen(it)) }
                        MerkAnsicht.GESEHEN -> WatchlistScreen(model, "gesehen", merkRaster, titelOeffnen) {
                            tab = Tab.Movies; reihenZeigen = false
                        }
                        else -> WatchlistScreen(model, "merkliste", merkRaster, titelOeffnen) {
                            tab = Tab.Movies; reihenZeigen = false
                        }
                    }
                }
            }
            Tab.Favorites -> FavoritenScreen(model, favRaster, favoritenSerien, { favoritenSerien = it }, titelOeffnen)
            Tab.Profile -> ProfileScreen(
                model,
                onNeuigkeiten = neuigkeiten,
                onReihen = { tab = Tab.Movies; reihenZeigen = true },
                onListen = { tab = Tab.Watchlist; merkAnsicht = MerkAnsicht.LISTEN },
                onFavoriten = { tab = Tab.Favorites },
                onWuensche = { oeffnen(Ziel.Wuensche) },
                onFernseher = { oeffnen(Ziel.FernseherAnmelden) },
                onSperre = { oeffnen(Ziel.Sperre) },
                onDownloads = { oeffnen(Ziel.Downloads) },
            )
        }
        }
    }

    // Auf dem Tablet quer gehört die Navigation an den Rand, wie bei jeder
    // Android-App mit viel Platz; auf dem Telefon nach unten, zum Daumen.
    BoxWithConstraints(Modifier.fillMaxSize()) {
        val breit = maxWidth >= 840.dp
        if (breit) {
            Row(Modifier.fillMaxSize().background(Lumen.background)) {
                NavigationRail(containerColor = Color(0xFF101014),
                               modifier = Modifier.fillMaxHeight().systemBarsPadding()) {
                    Spacer(Modifier.height(12.dp))
                    Tab.entries.forEach { entry ->
                        NavigationRailItem(
                            selected = tab == entry,
                            onClick = { tab = entry },
                            icon = { TabSymbol(entry, ungelesen) },
                            label = { Text(entry.title, fontSize = 12.sp) },
                            colors = NavigationRailItemDefaults.colors(
                                selectedIconColor = Lumen.label,
                                selectedTextColor = Lumen.label,
                                unselectedIconColor = Lumen.label3,
                                unselectedTextColor = Lumen.label3,
                                indicatorColor = Lumen.fill,
                            ),
                        )
                    }
                }
                Box(Modifier.weight(1f).fillMaxHeight().systemBarsPadding()) { inhalt() }
            }
        } else {
            Scaffold(
                containerColor = Lumen.background,
                bottomBar = {
                    NavigationBar(containerColor = Color(0xFF101014)) {
                        Tab.entries.forEach { entry ->
                            NavigationBarItem(
                                selected = tab == entry,
                                onClick = { tab = entry },
                                icon = { TabSymbol(entry, ungelesen) },
                                label = { Text(entry.title, fontSize = 11.sp, maxLines = 1) },
                                colors = NavigationBarItemDefaults.colors(
                                    selectedIconColor = Lumen.label,
                                    selectedTextColor = Lumen.label,
                                    unselectedIconColor = Lumen.label3,
                                    unselectedTextColor = Lumen.label3,
                                    indicatorColor = Lumen.fill,
                                ),
                            )
                        }
                    }
                },
            ) { padding ->
                Box(Modifier.padding(padding)) { inhalt() }
            }
        }
    }
}

/** Das Symbol eines Reiters — am Profil mit Punkt, wenn Neuigkeiten ungelesen sind. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun TabSymbol(entry: Tab, ungelesen: Int) {
    if (entry == Tab.Profile && ungelesen > 0) {
        BadgedBox(badge = { Badge(containerColor = Color(0xFFFF453A)) }) {
            Icon(entry.icon, entry.title)
        }
    } else {
        Icon(entry.icon, entry.title)
    }
}

/**
 * Zwei Ansichten innerhalb eines Reiters. [zweite] ist true, wenn die
 * rechte gewählt ist.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun Umschalter(links: String, rechts: String, zweite: Boolean,
                       onChange: (Boolean) -> Unit) {
    Row(
        Modifier.fillMaxWidth().padding(start = 16.dp, end = 16.dp, top = 8.dp),
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        listOf(false to links, true to rechts).forEach { (wert, text) ->
            FilterChip(
                selected = zweite == wert,
                onClick = { onChange(wert) },
                label = { Text(text) },
                colors = FilterChipDefaults.filterChipColors(
                    labelColor = Lumen.label2,
                    selectedContainerColor = Lumen.fill,
                    selectedLabelColor = Lumen.label,
                ),
            )
        }
    }
}

/** Wie [Umschalter], nur mit mehr als zwei Ansichten — [gewaehlt] ist die Stelle. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun Dreiwahl(namen: List<String>, gewaehlt: Int, onChange: (Int) -> Unit) {
    Row(
        Modifier.fillMaxWidth().padding(start = 16.dp, end = 16.dp, top = 8.dp),
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        namen.forEachIndexed { i, text ->
            FilterChip(
                selected = gewaehlt == i,
                onClick = { onChange(i) },
                label = { Text(text) },
                colors = FilterChipDefaults.filterChipColors(
                    labelColor = Lumen.label2,
                    selectedContainerColor = Lumen.fill,
                    selectedLabelColor = Lumen.label,
                ),
            )
        }
    }
}

/**
 * Die Startseite: oben der erste Titel gross, darunter alle Reihen, die
 * der Server schickt, in seiner Reihenfolge — „Weiterschauen", „Neu
 * dazugekommen", Genres, „Zufällig" und was noch kommt. Nach der ersten
 * Reihe stehen die Filmreihen, wie im Browser. Kein Schlüssel ist hier
 * fest verdrahtet: eine neue Reihe auf dem Server steht ohne neue App da.
 *
 * Was zuletzt da war, hält das ViewModel: Kommt man von einem Titel zurück,
 * steht die Seite gleich wieder an derselben Stelle und wird nur still
 * aufgefrischt — etwa „Weiterschauen" nach einem Film.
 *
 * Bewegte Vorschau gibt es hier nicht — auf dem Telefon wäre es Akku für
 * nichts. Die läuft nur am Fernseher.
 */
@Composable
private fun HomeScreen(model: LumenViewModel, liste: androidx.compose.foundation.lazy.LazyListState,
                       onOpen: (Item) -> Unit, onSearch: () -> Unit,
                       ungelesen: Int, onNeuigkeiten: () -> Unit,
                       onReihe: (Reihe) -> Unit, onReihen: () -> Unit) {
    val vorher = model.startseite
    var rows by remember { mutableStateOf(vorher?.reihen ?: emptyList()) }
    var reihen by remember { mutableStateOf(vorher?.filmreihen ?: emptyList()) }
    var loading by remember { mutableStateOf(vorher == null) }
    var fehler by remember { mutableStateOf<String?>(null) }
    var versuch by remember { mutableStateOf(0) }

    LaunchedEffect(versuch) {
        val antwort = model.abfragen { it.home() }
        antwort.wert?.let { rows = it }
        // Ein Fehler zählt nur, wenn noch nichts da ist — sonst bleibt die
        // Seite, wie sie war, statt unter einer Meldung zu verschwinden.
        fehler = if (rows.isEmpty()) antwort.fehler else null
        loading = false
        // Reihen sind eine Zugabe: klappt das nicht, bleibt die Startseite stehen.
        withContext(Dispatchers.IO) { oderNull { model.client.collections() } }?.let { reihen = it }
        if (antwort.wert != null) model.startseite = Startseite(rows, reihen)
        // Den Zähler der Neuigkeiten nebenbei auffrischen, wie der Browser.
        model.refreshMe()
    }

    val gefuellt = rows.filter { it.items.isNotEmpty() }
    val erster = gefuellt.firstOrNull()?.items?.firstOrNull()

    LazyColumn(state = liste, contentPadding = PaddingValues(bottom = 16.dp)) {
        item(key = "lumen:suche") {
            Row(Modifier.padding(start = 16.dp, end = 8.dp, top = 16.dp, bottom = 14.dp),
                verticalAlignment = Alignment.CenterVertically) {
                SuchLeiste(onSearch, Modifier.weight(1f))
                IconButton(onClick = onNeuigkeiten) {
                    if (ungelesen > 0) {
                        BadgedBox(badge = {
                            Badge(containerColor = Color(0xFFFF453A), contentColor = Color.White) {
                                Text(if (ungelesen > 9) "9+" else ungelesen.toString())
                            }
                        }) { Icon(Icons.Filled.Notifications, tx(R.string.neuigkeiten_x_ungelesen, ungelesen)) }
                    } else {
                        Icon(Icons.Outlined.Notifications, tx(R.string.neuigkeiten), tint = Lumen.label2)
                    }
                }
            }
        }
        if (erster != null) {
            item(key = "lumen:hero") { Hero(model, erster) { onOpen(erster) } }
        }
        // Doppelte Titel in einer Reihe würden die Schlüssel der Liste
        // sprengen — „Zufällig" ist dafür nicht gefeit.
        gefuellt.forEachIndexed { i, row ->
            item(key = "reihe:" + row.key) { Reihenzeile(model, row.title, row.items, onOpen) }
            if (i == 0 && reihen.isNotEmpty()) {
                item(key = "lumen:filmreihen") {
                    Column(Modifier.padding(bottom = 22.dp)) {
                        Row(Modifier.padding(start = 16.dp, bottom = 10.dp).clickableRow(onReihen),
                            verticalAlignment = Alignment.CenterVertically) {
                            Text(tx(R.string.filmreihen), fontSize = 18.sp, fontWeight = FontWeight.SemiBold)
                            Text("  ›", fontSize = 18.sp, color = Lumen.label2)
                        }
                        LazyRow(
                            contentPadding = PaddingValues(horizontal = 16.dp),
                            horizontalArrangement = Arrangement.spacedBy(14.dp),
                        ) {
                            items(reihen.take(12), key = { it.id }) { r -> ReiheKarte(model, r) { onReihe(r) } }
                        }
                    }
                }
            }
        }
    }
    StateOverlay(loading && rows.isEmpty(), fehler, gefuellt.isEmpty() && !loading && fehler == null,
        if (model.isAdmin) tx(R.string.noch_nichts_da_lege_im)
        else tx(R.string.noch_nichts_da_sobald_der),
        onRetry = { loading = true; fehler = null; versuch++ })
}

/** Der erste Titel gross oben: Bild, Name, eine Zeile, der Anfang der Beschreibung. */
@Composable
private fun Hero(model: LumenViewModel, item: Item, onClick: () -> Unit) {
    Box(Modifier.fillMaxWidth().padding(horizontal = 16.dp).padding(bottom = 22.dp)
            .clickableRow(onClick)) {
        Artwork(model.client.wideUrl(item), item.title,
                Modifier.fillMaxWidth().aspectRatio(16f / 9f))
        Box(Modifier.matchParentSize().clip(androidx.compose.foundation.shape.RoundedCornerShape(Lumen.corner))
                .background(androidx.compose.ui.graphics.Brush.verticalGradient(
                    listOf(Color.Transparent, Color(0xE6050507)))))
        Column(Modifier.align(Alignment.BottomStart).padding(16.dp)) {
            Text(item.title, fontSize = 26.sp, fontWeight = FontWeight.Bold, maxLines = 2,
                 overflow = TextOverflow.Ellipsis)
            val zeile = buildList {
                item.jahresText?.let { add(it) }
                item.runtimeMs?.takeIf { it > 0 }?.let { add(Item.duration(it)) }
                if (item.genres.isNotEmpty()) add(item.genres.take(2).joinToString(" · "))
                item.imdbRating?.takeIf { it > 0 }?.let { add("★ " + Titelseite.komma(it)) }
                item.rottenTomatoes?.let { add(Titelseite.tomaten(it)) }
            }.joinToString(" · ")
            if (zeile.isNotEmpty()) Text(zeile, color = Lumen.label2, fontSize = 13.sp)
            item.overview?.takeIf { it.isNotBlank() }?.let {
                Spacer(Modifier.height(6.dp))
                Text(it, color = Lumen.label2, fontSize = 13.sp, maxLines = 2,
                     overflow = TextOverflow.Ellipsis)
            }
            Spacer(Modifier.height(12.dp))
            PrimaryButton(tx(R.string.ansehen), onClick = onClick)
        }
    }
}

/** Eine Reihe von Postern mit Überschrift — auf der Startseite und der Titelseite. */
@Composable
fun Reihenzeile(model: LumenViewModel, titel: String, eintraege: List<Item>,
                onOpen: (Item) -> Unit, hier: String? = null) {
    Column(Modifier.padding(bottom = 22.dp)) {
        Text(titel, fontSize = 18.sp, fontWeight = FontWeight.SemiBold,
             modifier = Modifier.padding(start = 16.dp, bottom = 10.dp))
        LazyRow(
            contentPadding = PaddingValues(horizontal = 16.dp),
            horizontalArrangement = Arrangement.spacedBy(14.dp),
        ) {
            items(eintraege.distinctBy { it.id }, key = { it.id }) { item ->
                PosterCard(model, item, onOpen, hervorgehoben = item.id == hier)
            }
        }
    }
}

/**
 * Was über einem Raster gewählt ist: Genre, Sortierung und — nur bei A–Z —
 * der Anfangsbuchstabe. Eine Grösse, damit sie im Gerüst
 * stehen bleibt, wenn eine Titelseite darüber aufgeht.
 */
data class RasterWahl(
    val genre: String? = null,
    val sortierung: Sortierung = Sortierung.TITEL,
    val buchstabe: String? = null,
    /** Die Richtung: absteigend heisst Z bis A, das Neueste zuerst. */
    val absteigend: Boolean = false,
    /** Zählt bei „Neu mischen" hoch — eine neue Wahl, also neu laden. */
    val mischung: Int = 0,
) {
    companion object {
        /** Die gemerkte Sortierung aus den Prefs des Profils, sonst Titel aufsteigend. */
        fun aus(prefs: Map<String, String>?, kind: String): RasterWahl {
            val (art, ab) = Sortierung.aus(prefs?.get(Sortierung.prefSchluessel(kind)), kind)
                ?: (Sortierung.TITEL to false)
            return RasterWahl(sortierung = art, absteigend = ab)
        }
    }
}

/**
 * Filme oder Serien als Raster — mit Genre-Knöpfen oben, der Sortierung,
 * bei A–Z der Buchstabenleiste und der Zahl der Titel. Beim
 * Herunterscrollen kommt die nächste Seite dazu, wie im Browser; vorher
 * waren nach zweihundert Titeln Schluss.
 */
@Composable
private fun BrowseScreen(model: LumenViewModel, kind: String,
                         raster: androidx.compose.foundation.lazy.grid.LazyGridState,
                         wahl: RasterWahl, onWahl: (RasterWahl) -> Unit,
                         onOpen: (Item) -> Unit, onSearch: () -> Unit) {
    val genre = wahl.genre
    // Unter diesem Schlüssel hält das ViewModel, was zuletzt geladen war —
    // samt nachgeladener Seiten. So steht das Raster nach der Rückkehr von
    // einem Titel gleich wieder da, an derselben Stelle.
    val cacheSchluessel = "$kind|${wahl.genre}|${wahl.sortierung.name}|${wahl.absteigend}|${wahl.mischung}|${wahl.buchstabe}"
    var seite by remember(kind, wahl) { mutableStateOf(model.raster[cacheSchluessel]) }
    // Die Genres vom ersten Laden: gefiltert schickt der Server dieselben,
    // aber bis er antwortet, sollen die Knöpfe nicht verschwinden.
    var genres by remember(kind) { mutableStateOf<List<String>>(emptyList()) }
    // Wie viele Titel unter jedem Buchstaben stehen — für die Leiste.
    var buchstaben by remember(kind, genre) { mutableStateOf(Buchstaben(emptyMap())) }
    var laedt by remember(kind, wahl) { mutableStateOf(seite == null) }
    var mehrLaedt by remember(kind, wahl) { mutableStateOf(false) }
    var fehler by remember(kind, wahl) { mutableStateOf<String?>(null) }
    var versuch by remember(kind, wahl) { mutableStateOf(0) }
    val scope = rememberCoroutineScope()

    // Eine andere Wahl ist ein anderes Raster: dann wieder von oben.
    val waehlen: (RasterWahl) -> Unit = { neu ->
        onWahl(neu)
        scope.launch { raster.scrollToItem(0) }
    }

    // Beim Nachladen mit dem Samen der ersten Seite — sonst käme bei
    // „Zufällig" eine neue Mischung, und Titel stünden doppelt da.
    fun holen(c: ApiClient, offset: Int = 0, seed: Long? = null): Seite =
        c.seite(kind, genre, sort = wahl.sortierung.schluessel, offset = offset,
                buchstabe = wahl.buchstabe.takeIf { wahl.sortierung.mitLeiste },
                dir = if (wahl.absteigend) "desc" else "asc", seed = seed)

    LaunchedEffect(kind, wahl, versuch) {
        // Schon da: nicht neu laden — sonst schrumpfte das Raster auf die
        // erste Seite, und die Stelle wäre weg.
        val schon = seite
        if (schon != null) {
            schon.genres.takeIf { it.isNotEmpty() }?.let { genres = it }
            laedt = false
            return@LaunchedEffect
        }
        laedt = true
        val antwort = model.abfragen { holen(it) }
        seite = antwort.wert
        fehler = antwort.fehler
        antwort.wert?.let { model.raster[cacheSchluessel] = it }
        seite?.genres?.takeIf { it.isNotEmpty() }?.let { genres = it }
        laedt = false
    }
    // Die Leiste ist Beiwerk: fehlt sie, bleibt das Raster trotzdem.
    LaunchedEffect(kind, genre) {
        buchstaben = withContext(Dispatchers.IO) {
            oderNull { model.client.buchstaben(kind, genre) }
        } ?: Buchstaben(emptyMap())
    }

    fun mehr() {
        val s = seite ?: return
        if (!s.weiter || mehrLaedt) return
        mehrLaedt = true
        scope.launch {
            val n = model.fetch { holen(it, offset = s.naechsterOffset, seed = s.seed) }
            if (n != null && seite === s) {
                val zusammen = s.mit(n)
                seite = zusammen
                model.raster[cacheSchluessel] = zusammen
            }
            mehrLaedt = false
        }
    }

    // Nah am Ende? Dann die nächste Seite holen.
    val amEnde by remember {
        derivedStateOf {
            val info = raster.layoutInfo
            val letzter = info.visibleItemsInfo.lastOrNull()?.index ?: 0
            info.totalItemsCount > 0 && letzter >= info.totalItemsCount - 12
        }
    }
    LaunchedEffect(amEnde, seite) { if (amEnde) mehr() }

    val items = seite?.items ?: emptyList()
    Column {
        // Die Suche hat ihre eigene Seite — mit Personen, Genres und der
        // Deutung der Eingabe. Hier steht nur der Einstieg dorthin.
        SuchLeiste(onSearch, Modifier.padding(horizontal = 16.dp, vertical = 8.dp))
        LazyVerticalGrid(
            columns = GridCells.Adaptive(Lumen.posterWidth),
            state = raster,
            contentPadding = PaddingValues(16.dp),
            horizontalArrangement = Arrangement.spacedBy(14.dp),
            verticalArrangement = Arrangement.spacedBy(18.dp),
        ) {
            item(span = { androidx.compose.foundation.lazy.grid.GridItemSpan(maxLineSpan) },
                 key = "lumen:kopf") {
                Column {
                    Text(if (kind == "movie") tx(R.string.filme) else tx(R.string.serien),
                         fontSize = 28.sp, fontWeight = FontWeight.Bold)
                    seite?.let { Text(it.zeile(genre), color = Lumen.label2, fontSize = 14.sp) }
                    if (genres.isNotEmpty()) {
                        Spacer(Modifier.height(12.dp))
                        // Ein anderes Genre: der Buchstabe gilt dort womöglich nicht.
                        GenreKnoepfe(genres, genre) { waehlen(wahl.copy(genre = it, buchstabe = null)) }
                    }
                    Spacer(Modifier.height(8.dp))
                    SortierKnoepfe(kind, wahl,
                        onWahl = { waehlen(wahl.copy(sortierung = it, absteigend = it.vorgabeAbsteigend, buchstabe = null)) },
                        onRichtung = {
                            waehlen(if (wahl.sortierung == Sortierung.ZUFALL) wahl.copy(mischung = wahl.mischung + 1)
                                    else wahl.copy(absteigend = !wahl.absteigend, buchstabe = null))
                        })
                    if (wahl.sortierung.mitLeiste) {
                        Spacer(Modifier.height(8.dp))
                        BuchstabenLeiste(buchstaben, wahl.buchstabe, wahl.absteigend) { waehlen(wahl.copy(buchstabe = it)) }
                    }
                }
            }
            items(items, key = { it.id }) { item -> PosterCard(model, item, onOpen) }
            if (seite?.weiter == true) {
                item(span = { androidx.compose.foundation.lazy.grid.GridItemSpan(maxLineSpan) },
                     key = "lumen:mehr") {
                    Box(Modifier.fillMaxWidth().padding(20.dp), Alignment.Center) {
                        CircularProgressIndicator(color = Lumen.label2, strokeWidth = 2.dp,
                                                  modifier = Modifier.size(22.dp))
                    }
                }
            }
        }
    }
    StateOverlay(laedt && items.isEmpty(), fehler, items.isEmpty() && !laedt && fehler == null,
        if (wahl.buchstabe != null) tx(R.string.unter_diesem_buchstaben_steht_nichts) else tx(R.string.hier_ist_noch_nichts),
        onRetry = { fehler = null; versuch++ })
}

/**
 * Die Richtung vorn — „Aufsteigend"/„Absteigend", bei „Zufällig" „Neu
 * mischen" —, dahinter die Sortierungen zum Wischen, wie im Browser.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun SortierKnoepfe(kind: String, wahl: RasterWahl, onWahl: (Sortierung) -> Unit, onRichtung: () -> Unit) {
    LazyRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        item(key = "lumen:richtung") {
            AssistChip(onClick = onRichtung,
                       label = { Text(if (wahl.sortierung == Sortierung.ZUFALL) Sortierung.neuMischen
                                      else Sortierung.richtung(wahl.absteigend)) },
                       colors = AssistChipDefaults.assistChipColors(labelColor = Lumen.label))
        }
        items(Sortierung.faelle(kind), key = { it.schluessel }) { s ->
            FilterChip(selected = wahl.sortierung == s, onClick = { onWahl(s) },
                       label = { Text(s.titel(kind)) },
                       colors = FilterChipDefaults.filterChipColors(
                           labelColor = Lumen.label2, selectedContainerColor = Lumen.fill,
                           selectedLabelColor = Lumen.label))
        }
    }
}

/**
 * „Alle“, „#“ und A bis Z. Ein Buchstabe ohne Titel steht grau da und
 * lässt sich nicht wählen — so sieht man, wo nichts ist, ohne ins Leere
 * zu tippen.
 */
@Composable
private fun BuchstabenLeiste(buchstaben: Buchstaben, gewaehlt: String?, absteigend: Boolean, onWahl: (String?) -> Unit) {
    LazyRow(horizontalArrangement = Arrangement.spacedBy(2.dp)) {
        item(key = "lumen:alle") {
            BuchstabenPunkt(tx(R.string.alle), gewaehlt == null, true, tx(R.string.alle_buchstaben)) { onWahl(null) }
        }
        items(Buchstaben.reihe(absteigend), key = { it }) { b ->
            val n = buchstaben.anzahl(b)
            BuchstabenPunkt(b, gewaehlt == b, n > 0,
                (if (b == Buchstaben.RAUTE) tx(R.string.zahlen_und_zeichen) else tx(R.string.buchstabe_x, b)) + if (n > 0) ", " + Texte.n(TxN.TITEL_ANZAHL, n) else "") {
                onWahl(b)
            }
        }
    }
}

@Composable
private fun BuchstabenPunkt(text: String, gewaehlt: Boolean, waehlbar: Boolean, beschreibung: String,
                            onClick: () -> Unit) {
    Box(
        Modifier
            .heightIn(min = 36.dp).widthIn(min = 32.dp)
            .clip(androidx.compose.foundation.shape.RoundedCornerShape(10.dp))
            .background(if (gewaehlt) Lumen.fill else Color.Transparent)
            .then(if (waehlbar) Modifier.clickableRow(onClick) else Modifier)
            .semantics { contentDescription = beschreibung }
            .padding(horizontal = 8.dp),
        contentAlignment = Alignment.Center,
    ) {
        Text(text, fontSize = 14.sp,
             fontWeight = if (gewaehlt) FontWeight.SemiBold else FontWeight.Normal,
             color = when {
                 gewaehlt -> Lumen.label
                 waehlbar -> Lumen.label2
                 else -> Color(0x2EFFFFFF)
             })
    }
}

/** „Alle" und die Genres — wie die Reiter über dem Raster im Browser. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun GenreKnoepfe(genres: List<String>, gewaehlt: String?, onGenre: (String?) -> Unit) {
    LazyRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        item(key = "lumen:alle") {
            FilterChip(selected = gewaehlt == null, onClick = { onGenre(null) },
                       label = { Text(tx(R.string.alle)) },
                       colors = FilterChipDefaults.filterChipColors(
                           labelColor = Lumen.label2, selectedContainerColor = Lumen.fill,
                           selectedLabelColor = Lumen.label))
        }
        items(genres, key = { it }) { g ->
            FilterChip(selected = gewaehlt == g, onClick = { onGenre(g) },
                       label = { Text(g) },
                       colors = FilterChipDefaults.filterChipColors(
                           labelColor = Lumen.label2, selectedContainerColor = Lumen.fill,
                           selectedLabelColor = Lumen.label))
        }
    }
}

/**
 * Merkliste oder Gesehen — welche, wählt der Reiter oben ([liste] ist
 * „merkliste" oder „gesehen"). Der Server filtert und ordnet: nur die
 * eigene Liste, auch was weit hinten in der Mediathek steht.
 */
@Composable
private fun WatchlistScreen(model: LumenViewModel, liste: String,
                            raster: androidx.compose.foundation.lazy.grid.LazyGridState,
                            onOpen: (Item) -> Unit, onFilme: () -> Unit) {
    var items by remember(liste) { mutableStateOf<List<Item>>(emptyList()) }
    var loading by remember(liste) { mutableStateOf(true) }
    var fehler by remember(liste) { mutableStateOf<String?>(null) }
    var versuch by remember(liste) { mutableStateOf(0) }

    LaunchedEffect(liste, versuch) {
        loading = true
        val antwort = model.abfragen { it.listeGanz(liste) }
        antwort.wert?.let { items = it.items }
        fehler = antwort.fehler
        loading = false
    }

    LazyVerticalGrid(
        columns = GridCells.Adaptive(Lumen.posterWidth),
        state = raster,
        contentPadding = PaddingValues(16.dp),
        horizontalArrangement = Arrangement.spacedBy(14.dp),
        verticalArrangement = Arrangement.spacedBy(18.dp),
    ) {
        item(span = { androidx.compose.foundation.lazy.grid.GridItemSpan(maxLineSpan) }) {
            Text(if (liste == "gesehen") tx(R.string.fertig_geschaut_zuletzt_beendetes_zuerst)
                 else tx(R.string.nur_du_siehst_diese_liste), color = Lumen.label2, fontSize = 14.sp)
        }
        items(items, key = { it.id }) { item -> PosterCard(model, item, onOpen) }
    }
    when {
        // Ein Fehler ist kein leere Liste: „Noch nichts gemerkt" wäre hier gelogen.
        fehler != null && items.isEmpty() ->
            StateOverlay(false, fehler, onRetry = { fehler = null; versuch++ })
        !loading && items.isEmpty() -> Column(Modifier.fillMaxSize().padding(32.dp),
                verticalArrangement = Arrangement.Center,
                horizontalAlignment = Alignment.CenterHorizontally) {
            if (liste == "gesehen") {
                Text(tx(R.string.noch_nichts_zu_ende_geschaut),
                     color = Lumen.label2, textAlign = androidx.compose.ui.text.style.TextAlign.Center)
            } else {
                Text(tx(R.string.noch_nichts_gemerkt_auf_einer),
                     color = Lumen.label2, textAlign = androidx.compose.ui.text.style.TextAlign.Center)
                Spacer(Modifier.height(18.dp))
                PrimaryButton(tx(R.string.filme_ansehen), onClick = onFilme)
            }
        }
        else -> StateOverlay(loading && items.isEmpty(), null, false)
    }
}

/**
 * Die Favoriten: was mit dem Herz markiert ist, getrennt nach
 * Filmen und Serien wie im Browser. Nur das eigene Profil sieht sie.
 */
@Composable
private fun FavoritenScreen(model: LumenViewModel,
                            raster: androidx.compose.foundation.lazy.grid.LazyGridState,
                            serien: Boolean, onSerien: (Boolean) -> Unit,
                            onOpen: (Item) -> Unit) {
    val kind = if (serien) "series" else "movie"
    var items by remember(kind) { mutableStateOf<List<Item>>(emptyList()) }
    var loading by remember(kind) { mutableStateOf(true) }
    var error by remember(kind) { mutableStateOf<String?>(null) }
    var versuch by remember(kind) { mutableStateOf(0) }

    LaunchedEffect(kind, versuch) {
        loading = true
        val antwort = model.abfragen { it.listeGanz("favoriten", kind) }
        antwort.wert?.let { items = it.items }
        error = antwort.fehler
        loading = false
    }

    Column {
        Umschalter(tx(R.string.filme), tx(R.string.serien), serien, onSerien)
        LazyVerticalGrid(
            columns = GridCells.Adaptive(Lumen.posterWidth),
            state = raster,
            contentPadding = PaddingValues(16.dp),
            horizontalArrangement = Arrangement.spacedBy(14.dp),
            verticalArrangement = Arrangement.spacedBy(18.dp),
        ) {
            item(span = { androidx.compose.foundation.lazy.grid.GridItemSpan(maxLineSpan) }) {
                Text(tx(R.string.nur_du_siehst_deine_favoriten), color = Lumen.label2, fontSize = 14.sp)
            }
            items(items, key = { it.id }) { item -> PosterCard(model, item, onOpen) }
        }
    }
    StateOverlay(loading && items.isEmpty(), error.takeIf { items.isEmpty() },
        items.isEmpty() && !loading && error == null,
        if (serien) tx(R.string.noch_keine_lieblingsserien_auf_einer)
        else tx(R.string.noch_keine_lieblingsfilme_auf_einer),
        onRetry = { error = null; versuch++ })
}

/** Ein Titel als Poster mit Namen darunter — überall gleich. */
@Composable
fun PosterCard(model: LumenViewModel, item: Item, onOpen: (Item) -> Unit,
               hervorgehoben: Boolean = false) {
    Column(Modifier.width(Lumen.posterWidth).clickableRow { onOpen(item) }) {
        Box {
            Artwork(
                model.client.posterUrl(item), item.title,
                Modifier.width(Lumen.posterWidth).height(Lumen.posterHeight)
                    .then(if (hervorgehoben) Modifier.border(2.dp, Color.White,
                        androidx.compose.foundation.shape.RoundedCornerShape(Lumen.corner)) else Modifier))
            // „Neu", „3 neue Folgen", 4K, HDR — oben über dem Bild.
            KartenAbzeichen(item, Modifier.align(Alignment.TopCenter))
            // Bei Serien zählt der Server die eigenen Folgen: der Balken
            // zeigt den Anteil, alle gesehen heisst fertig. Der Haken steht
            // unten, damit oben Platz für die Abzeichen bleibt.
            if (item.finished) {
                SeenBadge(Modifier.align(Alignment.BottomEnd))
            } else if (item.progress > 1) {
                ProgressStripe(item.progress, Modifier.align(Alignment.BottomCenter))
            }
        }
        Spacer(Modifier.height(7.dp))
        Text(item.title, fontSize = 14.sp, maxLines = 1, overflow = TextOverflow.Ellipsis,
             fontWeight = if (hervorgehoben) FontWeight.SemiBold else FontWeight.Normal)
        Text(
            // „4 von 10 Folgen" bei einer angefangenen Serie — sonst Jahr und Wertung.
            item.serienStand ?: buildString {
                // Bei Serien der Zeitraum: „seit 2014", „2014–2020".
                item.jahresText?.let { append(it) }
                item.imdbRating?.takeIf { it > 0 }?.let {
                    if (isNotEmpty()) append(" · ")
                    append("★ " + Titelseite.komma(it))
                }
            },
            fontSize = 12.sp, color = Lumen.label2, maxLines = 1,
        )
    }
}
