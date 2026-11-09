// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package org.lumen.tv

import org.lumen.texte.Sprachwahl
import android.app.Application
import android.content.Context
import android.content.SharedPreferences
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.compose.foundation.background
import androidx.compose.foundation.focusable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.darkColorScheme
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.scale
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.focus.onFocusChanged
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.security.crypto.EncryptedSharedPreferences
import androidx.security.crypto.MasterKey
import coil.compose.AsyncImage
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import org.lumen.core.*

/**
 * Lumen auf Android TV und Fire TV.
 *
 * Derselbe lumen-core, eine eigene Oberfläche — und das ist kein Luxus:
 * auf dem Fernseher gibt es keinen Zeiger, sondern einen Fokus, der von
 * Element zu Element springt. Alles ist grösser, weil man drei Meter
 * entfernt sitzt, und alles Wichtige muss ohne Zielen erreichbar sein.
 *
 * Was der Browser kann, kann der Fernseher auch — bis auf die Verwaltung:
 * die gibt es nur im Browser auf dem Rechner.
 */
class LumenTvApplication : Application(), coil.ImageLoaderFactory {
    /** Die gespeicherten Server — und zugleich Adresse und Token des gewählten. */
    lateinit var store: Serverliste
        private set

    /**
     * Der Client der laufenden Sitzung — gesetzt von [TvRoot]. Der
     * Bildlader fragt ihn nach einem neuen Medienschlüssel, wenn ein Bild
     * mit 401 zurückkommt.
     */
    @Volatile var client: ApiClient? = null

    override fun newImageLoader(): coil.ImageLoader = lumenBildlader(this) { client }

    override fun onConfigurationChanged(newConfig: android.content.res.Configuration) {
        super.onConfigurationChanged(newConfig)
        Sprachwahl.konfigurationGeaendert(this)
    }

    /** Ein Hinweis, falls die Anmeldungen nicht verschlüsselt gespeichert
     *  werden konnten — die Oberfläche zeigt ihn einmal an. */
    @Volatile var speicherHinweis: String? = null

    override fun onCreate() {
        super.onCreate()
        // Zuerst die Sprache: alles danach, das Text braucht, soll ihn
        // schon in der richtigen bekommen.
        Sprachwahl.einrichten(this)
        val sicher = sichereEinstellungen(this)
        speicherHinweis = sicher.hinweis
        val prefs = sicher.prefs
        store = if (prefs != null) {
            // Früher lag der eine Server samt Token unverschlüsselt in
            // „lumen". Die Liste übernimmt ihn beim ersten Start in die
            // verschlüsselte Ablage und löscht ihn dort — niemand muss sich
            // neu anmelden.
            val alt = TvAblage(getSharedPreferences("lumen", Context.MODE_PRIVATE))
            Serverliste(TvAblage(prefs), alt = alt)
        } else {
            // Nie ein Token im Klartext: lieber nach jedem Neustart neu anmelden.
            Serverliste(MemorySchluesselablage())
        }
        DeviceProfile.install(DeviceProfile(
            name = android.os.Build.MODEL.ifBlank { tx(R.string.fernseher) },
            kind = "tv",
            codecs = listOf("h264", "hevc"),
            containers = listOf("mp4", "mkv", "hls"),
            audio = listOf("aac", "ac3", "eac3", "mp3"),
            maxHeight = 2160,
            hdr = true))
    }
}

/** Die verschlüsselte Ablage — oder keine, und was die Person wissen muss. */
internal class TvSichereAblage(val prefs: SharedPreferences?, val hinweis: String?)

/**
 * Wo Serverliste und Tokens liegen: verschlüsselt, mit einem Schlüssel aus
 * dem Keystore — jedes Token ist der Schlüssel zu einem Server.
 *
 * Kein stiller Rückfall auf Klartext mehr. Ist die Datei nicht mehr zu
 * entschlüsseln (etwa nach einer Wiederherstellung ohne den Schlüssel),
 * wird sie samt Schlüssel verworfen und neu angelegt; geht selbst das nicht,
 * bleibt die Anmeldung nur im Arbeitsspeicher. Beides sagt der Fernseher.
 */
private fun sichereEinstellungen(context: Context): TvSichereAblage {
    fun oeffnen(): SharedPreferences {
        val key = MasterKey.Builder(context)
            .setKeyScheme(MasterKey.KeyScheme.AES256_GCM)
            .build()
        return EncryptedSharedPreferences.create(
            context, "lumen-secure", key,
            EncryptedSharedPreferences.PrefKeyEncryptionScheme.AES256_SIV,
            EncryptedSharedPreferences.PrefValueEncryptionScheme.AES256_GCM)
    }
    // Breit gefangen, mit Absicht: Tink und der Keystore werfen je nach
    // Gerät sehr verschiedene Ausnahmen. Koroutinen gibt es hier keine.
    try {
        return TvSichereAblage(oeffnen(), null)
    } catch (e: Exception) {
        // Unten: verwerfen und neu anlegen.
    }
    return try {
        context.deleteSharedPreferences("lumen-secure")
        java.security.KeyStore.getInstance("AndroidKeyStore").apply { load(null) }
            .deleteEntry(MasterKey.DEFAULT_MASTER_KEY_ALIAS)
        TvSichereAblage(oeffnen(),
            tx(R.string.die_gespeicherten_anmeldungen_liessen_si))
    } catch (e: Exception) {
        TvSichereAblage(null,
            tx(R.string.dieser_fernseher_kann_die_anmeldung))
    }
}

/** Die Schlüssel-Wert-Ablage des Kerns über Android-Einstellungen. */
private class TvAblage(private val prefs: SharedPreferences) : Schluesselablage {
    override fun lesen(schluessel: String): String? = prefs.getString(schluessel, null)

    override fun schreiben(schluessel: String, wert: String?) {
        prefs.edit().apply {
            if (wert == null) remove(schluessel) else putString(schluessel, wert)
        }.commit()
    }
}

class TvActivity : ComponentActivity() {
    // Die Activity spricht die Sprache, die Sprachwahl festhält.
    override fun attachBaseContext(newBase: Context) {
        super.attachBaseContext(Sprachwahl.kontext(newBase))
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        val store = (application as LumenTvApplication).store

        setContent {
            MaterialTheme(colorScheme = darkColorScheme(
                background = Tv.background, surface = Tv.background,
                onBackground = Color.White, onSurface = Color.White)) {
                Surface(Modifier.fillMaxSize(), color = Tv.background) {
                    TvRoot(store)
                }
            }
        }
    }
}

internal object Tv {
    val background = Color(0xFF050507)
    val label2 = Color(0x9EFFFFFF)
    val label3 = Color(0x61FFFFFF)
    val accent = Color(0xFF0A84FF)
    val bad = Color(0xFFFF6961)
    val good = Color(0xFF30D158)
    val star = Color(0xFFFFD60A)
}

/** Was „Über Lumen" anzeigt — vom Server beim Verbinden genannt. */
internal data class TvServerInfo(
    val version: String,
    val license: String,
    val sourceUrl: String?,
    val ffmpeg: Boolean,
    /** Wie der gewählte Server in der Serverliste heisst. */
    val serverName: String? = null,
    /** Die Sprache, die der Server vorgibt, und welche er anbietet. */
    val sprache: String? = null,
    val sprachen: List<Sprache> = emptyList(),
)

@Composable
private fun TvRoot(store: Serverliste) {
    val controller = remember { SessionController(store) }
    val anwendung = androidx.compose.ui.platform.LocalContext.current.applicationContext
    SideEffect { (anwendung as? LumenTvApplication)?.client = controller.client }
    var stage by remember { mutableStateOf<Stage>(Stage.StartingUp) }
    // Eine Einladung einlösen — aus der Profilwahl heraus.
    var einladung by remember { mutableStateOf(false) }
    var einladungFehler by remember { mutableStateOf<String?>(null) }
    // Am Fernseher ist der Code der Hauptweg; die Profilauswahl gibt es
    // daneben, für wen das Passwort griffbereit hat.
    var profilHier by remember { mutableStateOf(false) }
    val scope = rememberCoroutineScope()

    suspend fun <T> hintergrund(block: () -> T): T = withContext(Dispatchers.IO) { block() }

    /** Etwas im Hintergrund tun und danach den Stand übernehmen. */
    fun schritt(block: () -> Unit) {
        scope.launch {
            stage = Stage.StartingUp
            hintergrund(block)
            stage = controller.stage
        }
    }

    LaunchedEffect(stage) { if (stage is Stage.SignedIn || stage is Stage.ChoosingServer) profilHier = false }

    // Die Sprache folgt dem Profil — vor der Anmeldung dem Server.
    val activity = androidx.compose.ui.platform.LocalContext.current as? android.app.Activity
    val sprache = remember(stage) { controller.sprache }
    val angemeldet = stage is Stage.SignedIn
    LaunchedEffect(sprache, angemeldet) {
        val a = activity ?: return@LaunchedEffect
        Sprachwahl.abgleichen(a, sprache, angemeldet) { wunsch ->
            scope.launch {
                hintergrund {
                    try { controller.client.updatePrefs(mapOf(Sprachen.PREF to wunsch)) } catch (e: LumenException) {}
                    controller.refreshMe()
                }
                stage = controller.stage
            }
        }
    }

    // Konnte die Anmeldung nicht verschlüsselt gespeichert werden, soll man
    // das wissen — einmal, mit einem Druck auf „Verstanden" weg.
    var speicherHinweis by remember { mutableStateOf((anwendung as? LumenTvApplication)?.speicherHinweis) }
    speicherHinweis?.let { text ->
        TvDialogRahmen(onDismiss = {
            speicherHinweis = null
            (anwendung as? LumenTvApplication)?.speicherHinweis = null
        }) {
            val ok = remember { FocusRequester() }
            LaunchedEffect(Unit) { kotlinx.coroutines.delay(100); runCatching { ok.requestFocus() } }
            Text(tx(R.string.anmeldung_nicht_dauerhaft_gespeichert), fontSize = 28.sp, fontWeight = FontWeight.Bold)
            Spacer(Modifier.height(10.dp))
            Text(text, color = Tv.label2, fontSize = 17.sp)
            Spacer(Modifier.height(24.dp))
            TvButton(tx(R.string.verstanden), modifier = Modifier.focusRequester(ok)) {
                speicherHinweis = null
                (anwendung as? LumenTvApplication)?.speicherHinweis = null
            }
        }
    }

    LaunchedEffect(Unit) {
        hintergrund { controller.start() }
        stage = controller.stage
    }

    if (einladung && stage is Stage.ChoosingProfile) {
        androidx.activity.compose.BackHandler { einladung = false; einladungFehler = null }
        TvEinladung(einladungFehler, onAbbrechen = { einladung = false; einladungFehler = null }) { code, name, art, geheim ->
            hintergrund { controller.redeemInvite(code, name, art, geheim) }
            einladungFehler = controller.lastError
            if (controller.stage is Stage.SignedIn) einladung = false
            stage = controller.stage
        }
        return
    }

    when (val s = stage) {
        is Stage.StartingUp -> Mitte { CircularProgressIndicator(color = Tv.label2) }

        is Stage.NeedsServer -> TvServerScreen(s.message,
            onZurueck = if (controller.server.isNotEmpty()) ({ schritt { controller.serverWahl() } }) else null,
        ) { adresse, fingerabdruck ->
            stage = Stage.StartingUp
            hintergrund { controller.useServer(adresse, fingerabdruck) }
            profilHier = false
            stage = controller.stage
        }

        is Stage.ChoosingServer -> TvServerListe(
            s, angemeldet = { store.angemeldet(it) },
            onVerbinden = { id -> schritt { controller.verbinden(id) } },
            onHinzufuegen = { schritt { controller.serverHinzufuegen() } },
            onUmbenennen = { id, name -> schritt { controller.serverUmbenennen(id, name) } },
            onEntfernen = { id -> schritt { controller.serverEntfernen(id) } })

        is Stage.NeedsSetup -> Mitte {
            Column(horizontalAlignment = Alignment.CenterHorizontally) {
                Text(tx(R.string.dieser_server_ist_noch_nicht),
                     fontSize = 36.sp, fontWeight = FontWeight.Bold)
                Spacer(Modifier.height(14.dp))
                Text(tx(R.string.richte_ihn_im_browser_ein),
                     color = Tv.label2, fontSize = 18.sp, textAlign = TextAlign.Center,
                     modifier = Modifier.widthIn(max = 720.dp))
                Spacer(Modifier.height(28.dp))
                TvButton(tx(R.string.anderer_server)) { schritt { controller.serverWahl() } }
            }
        }

        is Stage.ChoosingProfile -> if (!profilHier) {
            TvCodeAnmelden(controller,
                serverName = controller.aktiverServer?.name,
                onProfilHier = { profilHier = true },
                onServerWechseln = { schritt { controller.serverWahl() } },
                onAngemeldet = { stage = controller.stage },
                profilHierText = if (s.unterwegs) tx(R.string.mit_name_und_pin) else tx(R.string.profil_hier_waehlen))
        } else if (s.unterwegs) {
            // Von unterwegs zeigt der Server keine Profile. Am Fernseher
            // bleibt der Code der bequemere Weg — das hier ist für wen den
            // Namen und die PIN griffbereit hat.
            // Ohne Zwischenstand „lädt": sonst wären nach einem falschen PIN
            // auch Name und PIN weg, und mit der Fernbedienung tippt man
            // ungern zweimal.
            TvUnterwegs(
                onMitCode = { profilHier = false },
                onServerWechseln = { schritt { controller.serverWahl() } }) { name, geheim ->
                hintergrund { controller.unlockByName(name, geheim) }
                stage = controller.stage
                controller.lastError
            }
        } else TvProfiles(controller.client, s.profiles,
            onEinladung = { einladung = true },
            onMitCode = { profilHier = false },
            onServerWechseln = { schritt { controller.serverWahl() } }) { card, code ->
            stage = Stage.StartingUp
            hintergrund { controller.unlock(card, code) }
            stage = controller.stage
        }

        is Stage.Unlocking -> TvUnlock(controller.client, s.profile, controller.lastError) { code ->
            stage = Stage.StartingUp
            hintergrund { controller.unlock(s.profile, code) }
            stage = controller.stage
        }

        // Die Einstellungen des Profils gehen mit: „Folgen automatisch
        // weiter", „Vorspann überspringen", die gemerkten Sprachen für Ton
        // und Untertitel. Was der Player davon ändert, kommt hierher zurück.
        // Dazu Name und Profilbild für die Navigation und der Zähler der
        // eigenen Neuigkeiten.
        is Stage.SignedIn -> key(s.me.account.id) {
            TvHome(
                controller.client, s.me,
                TvServerInfo(controller.serverVersion, controller.license,
                             controller.sourceUrl, controller.hasFfmpeg,
                             controller.aktiverServer?.name,
                             controller.serverSprache, controller.serverSprachen),
                onAbmelden = {
                    scope.launch {
                        stage = Stage.StartingUp
                        hintergrund { controller.signOut() }
                        // Abgemeldet: zur Profilauswahl, nicht gleich zum
                        // Code. Von unterwegs gibt es keine Auswahl — dann
                        // doch der Code, der Hauptweg am Fernseher.
                        profilHier = (controller.stage as? Stage.ChoosingProfile)?.unterwegs != true
                        stage = controller.stage
                    }
                },
                onServerWechseln = { schritt { controller.serverWahl() } })
        }
    }
}

@Composable
internal fun Mitte(content: @Composable () -> Unit) {
    Box(Modifier.fillMaxSize(), Alignment.Center) { content() }
}

@Composable
private fun TvServerScreen(message: String?, onZurueck: (() -> Unit)?,
                           onConnect: suspend (String, String?) -> Unit) {
    var adresse by remember { mutableStateOf("") }
    var fingerabdruck by remember { mutableStateOf("") }
    var vonAussen by remember { mutableStateOf(false) }
    var senden by remember { mutableStateOf(false) }

    LaunchedEffect(senden) {
        if (senden) { onConnect(adresse, Pinnen.saubern(fingerabdruck)); senden = false }
    }

    if (onZurueck != null) androidx.activity.compose.BackHandler { onZurueck() }

    Mitte {
        Column(horizontalAlignment = Alignment.CenterHorizontally) {
            Text("Lumen", fontSize = 56.sp, fontWeight = FontWeight.Bold)
            Spacer(Modifier.height(12.dp))
            Text(tx(R.string.gib_die_adresse_deines_servers),
                 color = Tv.label2, fontSize = 20.sp)
            Spacer(Modifier.height(28.dp))
            androidx.compose.material3.OutlinedTextField(
                value = adresse,
                onValueChange = { adresse = it },
                singleLine = true,
                placeholder = { Text("192.168.1.20:8097") },
                modifier = Modifier.width(560.dp))
            // Von unterwegs läuft die Verbindung verschlüsselt, und ein
            // Heimserver stellt sein Zertifikat meist selbst aus. Dann
            // braucht der Fernseher einmal dessen Fingerabdruck.
            if (vonAussen) {
                Spacer(Modifier.height(14.dp))
                androidx.compose.material3.OutlinedTextField(
                    value = fingerabdruck,
                    onValueChange = { fingerabdruck = it },
                    singleLine = true,
                    placeholder = { Text(tx(R.string.fingerabdruck_64_zeichen_aus_09)) },
                    modifier = Modifier.width(560.dp))
                if (fingerabdruck.isNotBlank() && Pinnen.saubern(fingerabdruck) == null) {
                    Spacer(Modifier.height(8.dp))
                    Text(tx(R.string.das_sind_nicht_64_zeichen), color = Tv.bad, fontSize = 16.sp)
                }
            }
            if (message != null) {
                Spacer(Modifier.height(12.dp))
                Text(message, color = Tv.bad, fontSize = 16.sp,
                     textAlign = TextAlign.Center, modifier = Modifier.widthIn(max = 760.dp))
            }
            Spacer(Modifier.height(24.dp))
            Row(horizontalArrangement = Arrangement.spacedBy(18.dp)) {
                TvButton(tx(R.string.verbinden)) { senden = true }
                if (!vonAussen) TvButton(tx(R.string.von_unterwegs_verbinden)) { vonAussen = true }
                if (onZurueck != null) TvButton(tx(R.string.zurueck_zu_den_servern)) { onZurueck() }
            }
        }
    }
}

@Composable
private fun TvProfiles(client: ApiClient, profiles: List<ProfileCard>,
                       onEinladung: () -> Unit,
                       onMitCode: () -> Unit,
                       onServerWechseln: () -> Unit,
                       onUnlock: suspend (ProfileCard, String) -> Unit) {
    var gewaehlt by remember { mutableStateOf<ProfileCard?>(null) }
    var ohneCode by remember { mutableStateOf<ProfileCard?>(null) }
    val anfang = remember { FocusRequester() }

    // Ein Profil ohne Code braucht keine Nachfrage.
    LaunchedEffect(ohneCode) {
        ohneCode?.let { onUnlock(it, ""); ohneCode = null }
    }

    val karte = gewaehlt
    if (karte != null) {
        androidx.activity.compose.BackHandler { gewaehlt = null }
        TvUnlock(client, karte, null) { code -> onUnlock(karte, code) }
        return
    }

    LaunchedEffect(Unit) {
        kotlinx.coroutines.delay(100)
        runCatching { anfang.requestFocus() }
    }
    // Zurück führt zum Code — dem Hauptweg am Fernseher.
    androidx.activity.compose.BackHandler { onMitCode() }

    Mitte {
        Column(horizontalAlignment = Alignment.CenterHorizontally) {
            Text(tx(R.string.wer_schaut), fontSize = 46.sp, fontWeight = FontWeight.Bold)
            Spacer(Modifier.height(48.dp))
            LazyRow(horizontalArrangement = Arrangement.spacedBy(48.dp),
                    contentPadding = PaddingValues(horizontal = 48.dp, vertical = 12.dp)) {
                items(profiles.size, key = { profiles[it].id }) { i ->
                    val profil = profiles[i]
                    TvFocusable(
                        modifier = if (i == 0) Modifier.focusRequester(anfang) else Modifier,
                        onClick = { if (profil.needsCode) gewaehlt = profil else ohneCode = profil },
                    ) {
                        Column(horizontalAlignment = Alignment.CenterHorizontally) {
                            TvAvatar(profil.initial, profil.avatar, 140.dp,
                                     client.profilbildUrl(profil.id, profil.avatar))
                            Spacer(Modifier.height(14.dp))
                            Text(profil.name, fontSize = 20.sp)
                        }
                    }
                }
            }
            Spacer(Modifier.height(40.dp))
            // Wer einen Code bekommen hat, legt sich hier sein Profil an —
            // am Fernseher ist der Code einfacher als ein Link.
            Row(horizontalArrangement = Arrangement.spacedBy(18.dp)) {
                TvButton(tx(R.string.mit_code_anmelden)) { onMitCode() }
                TvButton(tx(R.string.ich_habe_eine_einladung)) { onEinladung() }
                TvButton(tx(R.string.server_wechseln)) { onServerWechseln() }
            }
        }
    }
}

@Composable
private fun TvUnlock(client: ApiClient, profile: ProfileCard, fehler: String?,
                     onSubmit: suspend (String) -> Unit) {
    var code by remember { mutableStateOf("") }
    var senden by remember { mutableStateOf(false) }
    LaunchedEffect(senden) { if (senden) { onSubmit(code); senden = false } }

    Mitte {
        Column(horizontalAlignment = Alignment.CenterHorizontally) {
            TvAvatar(profile.initial, profile.avatar, 120.dp,
                     client.profilbildUrl(profile.id, profile.avatar))
            Spacer(Modifier.height(16.dp))
            Text(profile.name, fontSize = 32.sp, fontWeight = FontWeight.SemiBold)
            Spacer(Modifier.height(24.dp))
            androidx.compose.material3.OutlinedTextField(
                value = code,
                onValueChange = { code = it },
                singleLine = true,
                visualTransformation =
                    androidx.compose.ui.text.input.PasswordVisualTransformation(),
                placeholder = { Text(if (profile.lockKind == "pin") tx(R.string.pin) else tx(R.string.passwort)) },
                modifier = Modifier.width(420.dp))
            if (fehler != null) {
                Spacer(Modifier.height(12.dp))
                Text(fehler, color = Tv.bad, fontSize = 16.sp)
            }
            Spacer(Modifier.height(22.dp))
            TvButton(tx(R.string.weiter)) { senden = true }
        }
    }
}

/**
 * Von unterwegs anmelden: Profilname und PIN oder Passwort. Der Server
 * zeigt von aussen keine Profilauswahl — sie verriete die Namen im
 * Haushalt. Die Meldung des Servers steht unverändert darunter, auch die
 * nach zu vielen Versuchen mit der Wartezeit.
 */
@Composable
private fun TvUnterwegs(onMitCode: () -> Unit, onServerWechseln: () -> Unit,
                        onSubmit: suspend (String, String) -> String?) {
    var name by remember { mutableStateOf("") }
    var geheim by remember { mutableStateOf("") }
    var fehler by remember { mutableStateOf<String?>(null) }
    var senden by remember { mutableStateOf(false) }
    LaunchedEffect(senden) {
        if (senden) {
            fehler = onSubmit(name, geheim)
            senden = false
        }
    }
    androidx.activity.compose.BackHandler { onMitCode() }

    Mitte {
        Column(horizontalAlignment = Alignment.CenterHorizontally) {
            Text(tx(R.string.von_unterwegs_anmelden), fontSize = 40.sp, fontWeight = FontWeight.Bold)
            Spacer(Modifier.height(12.dp))
            Text(tx(R.string.von_ausserhalb_des_heimnetzes_zeigt),
                 color = Tv.label2, fontSize = 20.sp, textAlign = TextAlign.Center,
                 modifier = Modifier.widthIn(max = 760.dp))
            Spacer(Modifier.height(28.dp))
            androidx.compose.material3.OutlinedTextField(
                value = name,
                onValueChange = { name = it },
                singleLine = true,
                placeholder = { Text(tx(R.string.profilname)) },
                modifier = Modifier.width(420.dp))
            Spacer(Modifier.height(14.dp))
            androidx.compose.material3.OutlinedTextField(
                value = geheim,
                onValueChange = { geheim = it },
                singleLine = true,
                visualTransformation =
                    androidx.compose.ui.text.input.PasswordVisualTransformation(),
                placeholder = { Text(tx(R.string.pin_oder_passwort)) },
                modifier = Modifier.width(420.dp))
            fehler?.let { f ->
                Spacer(Modifier.height(12.dp))
                Text(f, color = Tv.bad, fontSize = 16.sp, textAlign = TextAlign.Center,
                     modifier = Modifier.widthIn(max = 760.dp))
            }
            Spacer(Modifier.height(22.dp))
            Row(horizontalArrangement = Arrangement.spacedBy(18.dp)) {
                TvButton(if (senden) tx(R.string.einen_moment) else tx(R.string.anmelden)) {
                    if (!senden && name.isNotBlank() && geheim.isNotEmpty()) senden = true
                }
                TvButton(tx(R.string.mit_code)) { onMitCode() }
                TvButton(tx(R.string.server_wechseln)) { onServerWechseln() }
            }
        }
    }
}

// ---------------------------------------------------------------- Das Gerüst

/**
 * Was läuft: ein Titel, und falls er aus einer Liste kommt, die ganze
 * Liste samt Platz darin — damit es danach weitergeht. [ab] ist die Stelle
 * in Sekunden („Von vorn" ist 0), null heisst: beim gespeicherten Stand.
 */
internal data class TvAbspielen(
    val titel: Item,
    val liste: List<Item> = emptyList(),
    val index: Int = -1,
    val ab: Double? = null,
    val listenName: String? = null,
    // Die gewählte Fassung des ersten Titels (Issue #8); null: die erste.
    val dateiId: String? = null,
)

/**
 * Das Gerüst nach der Anmeldung: oben die Navigation wie im Browser —
 * Home · Filme · Serien · Suche · Merkliste · Favoriten · Filmreihen · Listen ·
 * Neuigkeiten · Wünsche · Profil —, darunter der gewählte Bereich. Darüber
 * liegt ein Stapel von Seiten (Titel, Reihe, Liste, Person, Genre); „Zurück"
 * geht ihn Schritt für Schritt ab, dann zurück zu Home.
 */
@Composable
private fun TvHome(client: ApiClient, me: Me, info: TvServerInfo, onAbmelden: () -> Unit,
                   onServerWechseln: () -> Unit) {
    // Das Konto, wie es gerade ist — ein neues Profilbild soll gleich oben stehen.
    var ich by remember { mutableStateOf(me) }
    // Die Einstellungen, wie der Player sie braucht — und wie er sie
    // ändert, wenn jemand eine Sprache wählt: hier gleich, beim Server
    // im Hintergrund. Beim nächsten Start kommen sie von dort zurück.
    var prefs by remember { mutableStateOf(me.prefs) }
    fun prefSetzen(key: String, wert: String) {
        prefs = prefs + (key to wert)
        Thread { try { client.updatePrefs(mapOf(key to wert)) } catch (e: LumenException) {} }.start()
    }
    // Die Sprache wartet auf den Server: was er danach für das Profil
    // nennt, gilt — dann baut sich die Activity in der neuen Sprache auf.
    val activity = androidx.compose.ui.platform.LocalContext.current as? android.app.Activity
    val sprachScope = rememberCoroutineScope()
    fun spracheSetzen(wert: String) {
        prefs = prefs + (Sprachen.PREF to wert)
        sprachScope.launch {
            val neu = oderNull { withContext(Dispatchers.IO) { client.updatePrefs(mapOf(Sprachen.PREF to wert)) } }
            if (neu != null) ich = neu
            val gilt = neu?.sprache ?: wert.ifEmpty { null } ?: info.sprache
            activity?.let { Sprachwahl.anwenden(it, gilt) }
        }
    }
    var bereich by remember { mutableStateOf(TvBereich.HOME) }
    var stapel by remember { mutableStateOf<List<TvSeite>>(emptyList()) }
    var laeuft by remember { mutableStateOf<TvAbspielen?>(null) }
    // Zählt hoch, sobald sich an den Listen etwas geändert hat — angelegt,
    // umbenannt, gelöscht, ein Titel hinein oder heraus. Was Listen zeigt,
    // lädt dann neu.
    var listenStand by remember { mutableStateOf(0) }
    // Zählt hoch, wenn der Player zugeht: eine offene Titelseite lädt dann
    // neu — der Stand hat sich geändert, vielleicht auch der Haken.
    var titelStand by remember { mutableStateOf(0) }
    // Der Zähler der eigenen Neuigkeiten — für den Punkt in der Navigation.
    var ungelesen by remember { mutableStateOf(me.neuigkeiten) }
    // Genre, Sortierung und Buchstabe der Raster bleiben, wenn man von
    // einem Titel zurückkommt — ebenso, ob die Favoriten Serien zeigen.
    var filmWahl by remember { mutableStateOf(TvRasterWahl()) }
    var serienWahl by remember { mutableStateOf(TvRasterWahl()) }
    var favoritenSerien by remember { mutableStateOf(false) }
    var merklisteGesehen by remember { mutableStateOf(false) }
    // Was die Startseite und die Raster zuletzt zeigten — damit sie nach
    // einem Titel nicht leer neu laden und oben stehen. Dazu der Halter, der
    // die Scrollstellen der Bereiche über das Öffnen einer Seite rettet.
    var startDaten by remember { mutableStateOf<TvStartDaten?>(null) }
    val rasterSpeicher = remember { HashMap<String, Seite>() }
    val halter = androidx.compose.runtime.saveable.rememberSaveableStateHolder()
    val rueckkehr = remember { TvRueckkehr() }
    // Die Sucheingabe bleibt ebenso — wer von einem Treffer zurückkommt,
    // findet seine Suche wieder.
    var suche by remember { mutableStateOf("") }
    // Ein einziger Abspieler für die bewegte Vorschau, solange das Gerüst
    // lebt — er wird für jede Karte wiederverwendet.
    val context = androidx.compose.ui.platform.LocalContext.current
    val vorschau = remember { TvVorschauSpieler(context) }
    DisposableEffect(Unit) { onDispose { vorschau.freigeben() } }
    // Geht die App in den Hintergrund (Home-Taste, anderer Eingang), lief
    // die stumme Vorschau bisher weiter — und zog Strom und Leitung.
    val besitzer = androidx.lifecycle.compose.LocalLifecycleOwner.current
    DisposableEffect(besitzer) {
        val beobachter = androidx.lifecycle.LifecycleEventObserver { _, e ->
            if (e == androidx.lifecycle.Lifecycle.Event.ON_STOP) vorschau.anhalten()
        }
        besitzer.lifecycle.addObserver(beobachter)
        onDispose { besitzer.lifecycle.removeObserver(beobachter) }
    }

    fun oeffnen(seite: TvSeite) { stapel = stapel + seite }
    val titelOeffnen: (Item) -> Unit = {
        // Beim Zurückkommen soll genau diese Karte den Fokus bekommen.
        rueckkehr.id = it.id
        oeffnen(TvSeite.Titel(TvTitelZiel(it)))
    }

    // Zurück auf Home: den Zähler frisch vom Server.
    LaunchedEffect(stapel.isEmpty(), bereich) {
        if (stapel.isEmpty() && bereich == TvBereich.HOME) {
            try {
                ungelesen = withContext(Dispatchers.IO) { client.me() }.neuigkeiten
            } catch (e: LumenException) { /* dann bleibt der alte */ }
        }
    }

    // Läuft etwas oder ist eine andere Seite offen, schweigt die Vorschau.
    val startseiteSichtbar = laeuft == null && stapel.isEmpty() && bereich == TvBereich.HOME
    LaunchedEffect(startseiteSichtbar) { if (!startseiteSichtbar) vorschau.anhalten() }

    val spielt = laeuft
    if (spielt != null) {
        key(spielt) {
            TvPlayer(client, spielt, prefs, onPref = { k, w -> prefSetzen(k, w) }) {
                laeuft = null
                // Zurück auf eine Titelseite, die neu lädt — der Stand hat sich geändert.
                titelStand++
            }
        }
        return
    }

    androidx.activity.compose.BackHandler(enabled = stapel.isNotEmpty() || bereich != TvBereich.HOME) {
        when {
            stapel.isNotEmpty() -> stapel = stapel.dropLast(1)
            // Was unter „Ich" liegt, geht zurück zu „Ich".
            !bereich.oben -> bereich = TvBereich.PROFIL
            else -> bereich = TvBereich.HOME
        }
    }

    // Die oberste Seite des Stapels liegt über allem anderen.
    when (val oben = stapel.lastOrNull()) {
        is TvSeite.Titel -> {
            key(oben, titelStand) {
                TvTitel(client, oben.ziel, prefs,
                    onPlay = { laeuft = it },
                    onOpen = titelOeffnen,
                    onPerson = { oeffnen(TvSeite.Person(it)) },
                    onReihe = { oeffnen(TvSeite.ReiheOffen(it)) },
                    onListeGeaendert = { listenStand++ },
                    onClose = { stapel = stapel.dropLast(1) })
            }
            return
        }
        is TvSeite.ReiheOffen -> {
            TvReihe(client, oben.id, onOpen = titelOeffnen, onPlay = { laeuft = TvAbspielen(it) })
            return
        }
        is TvSeite.ListeOffen -> {
            TvListe(client, oben.id, listenStand,
                onPlay = { name, eintraege, n ->
                    laeuft = TvAbspielen(n.titel, eintraege, n.index, listenName = name)
                },
                onOpen = { item, liste, i -> oeffnen(TvSeite.Titel(TvTitelZiel(item, liste, i))) },
                onGeaendert = { listenStand++ },
                onGeloescht = { listenStand++; stapel = stapel.dropLast(1) })
            return
        }
        is TvSeite.Person -> {
            TvPersonSeite(client, oben.name, onOpen = titelOeffnen)
            return
        }
        is TvSeite.Genre -> {
            // Ein Genre aus der Suche: das Raster der Filme, schon gefiltert.
            TvRaster(client, "movie", TvRasterWahl(genre = oben.name), onWahl = {},
                     onOpen = titelOeffnen, fest = true)
            return
        }
        null -> Unit
    }

    CompositionLocalProvider(LocalTvRueckkehr provides rueckkehr) {
    Box(Modifier.fillMaxSize()) {
        Column(Modifier.fillMaxSize()) {
            TvNavLeiste(bereich, ungelesen, ich, client) { bereich = it }
            Box(Modifier.fillMaxSize()) {
                halter.SaveableStateProvider(bereich.name) {
                when (bereich) {
                    TvBereich.HOME -> TvStartseite(client, vorschau, listenStand,
                        startDaten, { startDaten = it },
                        onOpen = titelOeffnen,
                        onReihe = { oeffnen(TvSeite.ReiheOffen(it.id)) },
                        onListe = { oeffnen(TvSeite.ListeOffen(it.id)) },
                        onListeAngelegt = { listenStand++; oeffnen(TvSeite.ListeOffen(it)) })
                    TvBereich.FILME -> TvRaster(client, "movie", filmWahl, { filmWahl = it }, titelOeffnen,
                                                zwischenspeicher = rasterSpeicher)
                    TvBereich.SERIEN -> TvRaster(client, "series", serienWahl, { serienWahl = it }, titelOeffnen,
                                                 zwischenspeicher = rasterSpeicher)
                    TvBereich.SUCHE -> TvSuche(client, suche, { suche = it },
                        onOpen = titelOeffnen,
                        onPerson = { oeffnen(TvSeite.Person(it)) },
                        onGenre = { oeffnen(TvSeite.Genre(it)) })
                    TvBereich.MERKLISTE -> TvMerkliste(client, merklisteGesehen, { merklisteGesehen = it },
                        onOpen = titelOeffnen) {
                        bereich = TvBereich.FILME
                    }
                    TvBereich.FAVORITEN -> TvFavoriten(client, favoritenSerien,
                        onSerien = { favoritenSerien = it }, onOpen = titelOeffnen)
                    TvBereich.REIHEN -> TvFilmreihen(client) { oeffnen(TvSeite.ReiheOffen(it.id)) }
                    TvBereich.LISTEN -> TvListen(client, listenStand,
                        onOpen = { oeffnen(TvSeite.ListeOffen(it)) },
                        onGeaendert = { listenStand++ })
                    TvBereich.NEUIGKEITEN -> TvNeuigkeiten(client,
                        onGelesen = { ungelesen = 0 },
                        onOpen = titelOeffnen,
                        onHome = { bereich = TvBereich.HOME })
                    TvBereich.WUENSCHE -> TvWuensche(client)
                    TvBereich.PROFIL -> TvProfil(client, ich, prefs, info,
                        onPref = { k, w -> if (k == Sprachen.PREF) spracheSetzen(w) else prefSetzen(k, w) },
                        onIch = { ich = it },
                        onAbmelden = onAbmelden,
                        onServerWechseln = onServerWechseln,
                        ungelesen = ungelesen,
                        onBereich = { bereich = it })
                }
                }
            }
        }
    }
    }
}

/** Was die Startseite zuletzt zeigte — im Gerüst gehalten. */
internal data class TvStartDaten(val reihen: List<HomeRow>, val filmreihen: List<Reihe>)

// ---------------------------------------------------------------- Startseite

/**
 * Home: das Bild des fokussierten Titels gross im Hintergrund, nach einer
 * Sekunde Fokus läuft stumm ein Schnipsel daraus. Darunter alle Reihen, die
 * der Server schickt; nach der ersten die Filmreihen, am Ende die Listen.
 */
@Composable
private fun TvStartseite(
    client: ApiClient,
    vorschau: TvVorschauSpieler,
    listenStand: Int,
    vorher: TvStartDaten?,
    onGeladen: (TvStartDaten) -> Unit,
    onOpen: (Item) -> Unit,
    onReihe: (Reihe) -> Unit,
    onListe: (Liste) -> Unit,
    onListeAngelegt: (String) -> Unit,
) {
    var rows by remember { mutableStateOf(vorher?.reihen ?: emptyList()) }
    var reihen by remember { mutableStateOf(vorher?.filmreihen ?: emptyList()) }
    var listen by remember { mutableStateOf<List<Liste>>(emptyList()) }
    var fokus by remember { mutableStateOf<Item?>(null) }
    var fehler by remember { mutableStateOf<String?>(null) }
    var geladen by remember { mutableStateOf(vorher != null) }
    var versuch by remember { mutableStateOf(0) }
    var neueListe by remember { mutableStateOf(false) }
    var listenHinweis by remember { mutableStateOf<String?>(null) }
    val scope = rememberCoroutineScope()

    // Schon Geladenes steht sofort da; aufgefrischt wird still im
    // Hintergrund (etwa „Weiterschauen" nach einem Film). Ein Fehler zählt
    // nur, solange noch nichts da ist.
    LaunchedEffect(versuch) {
        var neu = false
        try {
            rows = withContext(Dispatchers.IO) { client.home() }
            fehler = null
            neu = true
        } catch (e: LumenException) {
            if (rows.isEmpty()) fehler = e.message
        }
        geladen = true
        // Reihen sind eine Zugabe. Klappt das nicht — etwa bei einem
        // älteren Server —, bleibt die Startseite trotzdem stehen.
        withContext(Dispatchers.IO) { oderNull { client.collections() } }?.let { reihen = it }
        if (neu) onGeladen(TvStartDaten(rows, reihen))
    }

    LaunchedEffect(listenStand) {
        try {
            // Die eigenen zuerst, dann die geteilten. sortedBy ist stabil,
            // die Reihenfolge des Servers bleibt innerhalb beider Gruppen.
            listen = withContext(Dispatchers.IO) { client.playlists() }.sortedBy { !it.meine }
        } catch (e: LumenException) {
            listen = emptyList()
        }
    }

    if (neueListe) {
        TvNameDialog(tx(R.string.neue_liste), "", tx(R.string.anlegen), onDismiss = { neueListe = false }) { name ->
            neueListe = false
            scope.launch {
                try {
                    val angelegt = withContext(Dispatchers.IO) { client.createPlaylist(name) }
                    // Gleich hinein in die neue Liste — dort geht es weiter.
                    onListeAngelegt(angelegt.id)
                } catch (e: LumenException) {
                    listenHinweis = e.message
                }
            }
        }
    }

    val gefuellt = rows.filter { it.items.isNotEmpty() }
    val gezeigt = fokus ?: gefuellt.firstOrNull()?.items?.firstOrNull()

    Box(Modifier.fillMaxSize()) {
        // Das Bild des fokussierten Titels im Hintergrund — so sieht man
        // schon beim Durchblättern, worum es geht.
        if (gezeigt != null) {
            AsyncImage(
                model = client.wideUrl(gezeigt),
                contentDescription = null,
                contentScale = ContentScale.Crop,
                modifier = Modifier.fillMaxSize())
            // Nach einer Sekunde Fokus läuft stumm ein Schnipsel aus dem Film
            // über dem Bild — nur hier am Fernseher, nicht am Telefon.
            TvVorschauBild(vorschau, client, fokus)
            Box(Modifier.fillMaxSize().background(Brush.horizontalGradient(
                listOf(Color(0xF2050507), Color(0x8C050507), Color(0xE6050507)))))
        }

        LazyColumn(contentPadding = PaddingValues(start = 56.dp, top = 12.dp, bottom = 48.dp)) {
            if (gezeigt != null) {
                item(key = "lumen:kopf") {
                    Column(Modifier.padding(bottom = 32.dp, end = 56.dp)) {
                        Text(gezeigt.title, fontSize = 50.sp, fontWeight = FontWeight.Bold,
                             maxLines = 2, overflow = TextOverflow.Ellipsis)
                        Spacer(Modifier.height(8.dp))
                        Text(gezeigt.subtitleLine, color = Tv.label2, fontSize = 18.sp)
                        gezeigt.overview?.takeIf { it.isNotBlank() }?.let {
                            Spacer(Modifier.height(10.dp))
                            Text(it, color = Tv.label2, fontSize = 16.sp, maxLines = 3,
                                 overflow = TextOverflow.Ellipsis,
                                 modifier = Modifier.widthIn(max = 860.dp))
                        }
                    }
                }
            }
            // Alle Reihen, die der Server schickt — kein Schlüssel ist hier
            // fest verdrahtet. Die Vorsilbe hält sie von den eigenen fern.
            gefuellt.forEachIndexed { i, row ->
                item(key = "reihe:" + row.key) {
                    TvKartenReihe(client, row.title, row.items, onFocus = { fokus = it }, onOpen = onOpen)
                }
                if (i == 0 && reihen.isNotEmpty()) {
                    item(key = "lumen:filmreihen") {
                        Column(Modifier.padding(bottom = 40.dp)) {
                            Text(tx(R.string.filmreihen), fontSize = 24.sp, fontWeight = FontWeight.SemiBold)
                            Spacer(Modifier.height(16.dp))
                            LazyRow(horizontalArrangement = Arrangement.spacedBy(28.dp),
                                    contentPadding = PaddingValues(vertical = 12.dp, horizontal = 4.dp)) {
                                items(reihen, key = { it.id }) { reihe ->
                                    TvKachel(
                                        bild = client.reiheBildUrl(reihe),
                                        titel = reihe.name,
                                        zeile = reihe.anzahlText,
                                        onClick = { onReihe(reihe) })
                                }
                            }
                        }
                    }
                }
            }

            // Die Listen stehen immer da — auch leer: dann nur mit „Neue
            // Liste", denn anlegen soll man sie auch hier können (Issue #1).
            item(key = "lumen:listen") {
                Column(Modifier.padding(bottom = 40.dp)) {
                    Text(tx(R.string.listen), fontSize = 24.sp, fontWeight = FontWeight.SemiBold)
                    listenHinweis?.let {
                        Spacer(Modifier.height(6.dp))
                        Text(it, color = Tv.bad, fontSize = 16.sp)
                    }
                    Spacer(Modifier.height(16.dp))
                    LazyRow(horizontalArrangement = Arrangement.spacedBy(28.dp),
                            contentPadding = PaddingValues(vertical = 12.dp, horizontal = 4.dp)) {
                        items(listen, key = { it.id }) { liste ->
                            TvKachel(
                                bild = liste.vorschau.firstOrNull()?.let { client.posterUrl(it) },
                                titel = liste.name,
                                // Wer eine geteilte Liste angelegt hat — mehr nicht.
                                zeile = if (liste.meine) liste.anzahlText
                                        else tx(R.string.x_von_x, liste.anzahlText, liste.von.name),
                                onClick = { onListe(liste) })
                        }
                        item(key = "lumen:neue-liste") {
                            TvKachel(bild = null, titel = tx(R.string.neue_liste), zeile = tx(R.string.anlegen),
                                     symbol = "+", onClick = { neueListe = true })
                        }
                    }
                }
            }
        }

        fehler?.let {
            Mitte {
                Column(horizontalAlignment = Alignment.CenterHorizontally) {
                    Text(tx(R.string.das_hat_nicht_geklappt), fontSize = 28.sp, fontWeight = FontWeight.Bold)
                    Spacer(Modifier.height(10.dp))
                    Text(it, color = Tv.label2, fontSize = 20.sp, textAlign = TextAlign.Center,
                         modifier = Modifier.widthIn(max = 760.dp))
                    Spacer(Modifier.height(24.dp))
                    TvButton(tx(R.string.nochmal_versuchen)) { fehler = null; geladen = false; versuch++ }
                }
            }
        }
        if (!geladen && fehler == null && gefuellt.isEmpty()) {
            Mitte { CircularProgressIndicator(color = Tv.label2) }
        }
        if (geladen && fehler == null && gefuellt.isEmpty()) {
            Mitte {
                Text(tx(R.string.noch_nichts_da_sobald_filme),
                     color = Tv.label2, fontSize = 20.sp)
            }
        }
    }
}

/** Eine Reihe von Karten mit Überschrift — auf Home und auf der Titelseite. */
@Composable
internal fun TvKartenReihe(
    client: ApiClient,
    titel: String,
    eintraege: List<Item>,
    onFocus: (Item) -> Unit = {},
    onOpen: (Item) -> Unit,
    hier: String? = null,
    breite: Dp = 200.dp,
) {
    Column(Modifier.padding(bottom = 36.dp)) {
        Text(titel, fontSize = 24.sp, fontWeight = FontWeight.SemiBold)
        Spacer(Modifier.height(12.dp))
        LazyRow(horizontalArrangement = Arrangement.spacedBy(28.dp),
                contentPadding = PaddingValues(vertical = 12.dp, horizontal = 4.dp)) {
            items(eintraege.distinctBy { it.id }, key = { it.id }) { item ->
                // Ein Druck öffnet die Titelseite; dort steht „Abspielen"
                // schon unter dem Fokus.
                TvCard(client, item,
                    onFocus = { onFocus(item) },
                    onClick = { onOpen(item) },
                    breite = breite,
                    hervorgehoben = item.id == hier)
            }
        }
    }
}

// ---------------------------------------------------------------- Filmreihen

/**
 * Eine Filmreihe: ihre Filme in der Reihenfolge der Reihe, dazu „Ansehen"
 * oder „Weiter mit …" — der erste Film, der noch nicht gesehen ist.
 */
@Composable
private fun TvReihe(client: ApiClient, reiheId: String, onOpen: (Item) -> Unit, onPlay: (Item) -> Unit) {
    var reihe by remember(reiheId) { mutableStateOf<Reihe?>(null) }
    var fehler by remember(reiheId) { mutableStateOf<String?>(null) }
    var fokus by remember(reiheId) { mutableStateOf<Item?>(null) }
    var versuch by remember(reiheId) { mutableStateOf(0) }
    val anfang = remember { FocusRequester() }

    LaunchedEffect(reiheId, versuch) {
        try {
            reihe = withContext(Dispatchers.IO) { client.collection(reiheId) }
        } catch (e: LumenException) {
            fehler = e.message
        }
    }

    val r = reihe
    if (r == null) {
        TvLaden(fehler) { fehler = null; versuch++ }
        return
    }

    // Gleich auf „Ansehen" stehen, damit niemand erst suchen muss, wo der
    // Fokus ist. Klappt das nicht, holt ihn der erste Tastendruck.
    LaunchedEffect(r.id) {
        kotlinx.coroutines.delay(100)
        runCatching { anfang.requestFocus() }
    }

    Box(Modifier.fillMaxSize()) {
        // Der Hintergrund der Reihe selbst, sonst der des fokussierten Films.
        val bild = if (fokus == null && r.backdrop != null) client.reiheBildUrl(r, breit = true) else null
        if (bild != null) {
            AsyncImage(model = bild, contentDescription = null, contentScale = ContentScale.Crop,
                       modifier = Modifier.fillMaxSize())
            Box(Modifier.fillMaxSize().background(Brush.horizontalGradient(
                listOf(Color(0xF2050507), Color(0xB3050507), Color(0xE6050507)))))
        } else {
            TvHintergrund(client, fokus ?: r.items.firstOrNull())
        }
        Column(Modifier.fillMaxSize().padding(start = 56.dp, top = 48.dp, end = 56.dp)) {
            Text(r.name, fontSize = 46.sp, fontWeight = FontWeight.Bold,
                 maxLines = 2, overflow = TextOverflow.Ellipsis)
            Spacer(Modifier.height(8.dp))
            Text(r.unterzeile, color = Tv.label2, fontSize = 18.sp)
            r.overview?.takeIf { it.isNotBlank() }?.let {
                Spacer(Modifier.height(10.dp))
                Text(it, color = Tv.label2, fontSize = 16.sp, maxLines = 3,
                     overflow = TextOverflow.Ellipsis,
                     modifier = Modifier.widthIn(max = 860.dp))
            }
            r.naechster?.let { n ->
                Spacer(Modifier.height(24.dp))
                TvButton(r.abspielText, gross = true, modifier = Modifier.focusRequester(anfang)) { onPlay(n) }
            }
            Spacer(Modifier.height(24.dp))
            LazyRow(horizontalArrangement = Arrangement.spacedBy(28.dp),
                    contentPadding = PaddingValues(vertical = 16.dp, horizontal = 4.dp)) {
                items(r.items.size, key = { r.items[it].id }) { i ->
                    val film = r.items[i]
                    TvCard(client, film,
                        onFocus = { fokus = film },
                        onClick = { onOpen(film) })
                }
            }
        }
    }
}

// ---------------------------------------------------------------- Listen

/**
 * Eine Liste: oben ein grosses „Abspielen", das die ganze Liste am Stück
 * spielt, darunter die Titel. Ein Druck auf einen Titel öffnet seine
 * Titelseite — mit „Ab hier abspielen", „Aus der Liste nehmen" und dem
 * Verschieben nach vorn oder hinten.
 *
 * Wer die Liste angelegt hat, kann sie hier umbenennen, teilen und löschen.
 */
@Composable
private fun TvListe(
    client: ApiClient,
    listeId: String,
    stand: Int,
    onPlay: (String, List<Item>, AmStueck.Naechstes) -> Unit,
    onOpen: (Item, Liste, Int) -> Unit,
    onGeaendert: () -> Unit,
    onGeloescht: () -> Unit,
) {
    var liste by remember(listeId) { mutableStateOf<Liste?>(null) }
    var fehler by remember(listeId) { mutableStateOf<String?>(null) }
    var hinweis by remember(listeId) { mutableStateOf<String?>(null) }
    var fokus by remember(listeId) { mutableStateOf<Item?>(null) }
    var sucht by remember { mutableStateOf(false) }
    var umbenennen by remember { mutableStateOf(false) }
    var loeschen by remember { mutableStateOf(false) }
    val scope = rememberCoroutineScope()
    val abspielenFokus = remember { FocusRequester() }

    var versuch by remember(listeId) { mutableStateOf(0) }
    // Neu laden, wenn sich an den Listen etwas getan hat — etwa ein Titel
    // über seine Titelseite herausgenommen wurde.
    LaunchedEffect(listeId, stand, versuch) {
        try {
            liste = withContext(Dispatchers.IO) { client.playlist(listeId) }
        } catch (e: LumenException) {
            fehler = e.message
        }
    }

    val l = liste
    if (l == null) {
        TvLaden(fehler) { fehler = null; versuch++ }
        return
    }

    /** Die Liste ab [ab] abspielen. Was dort zuerst läuft, weiss der Server. */
    fun abspielenAb(ab: Int) {
        if (sucht) return
        sucht = true
        hinweis = null
        val eintraege = l.items
        scope.launch {
            try {
                val erstes = withContext(Dispatchers.IO) {
                    Abspielfolge(client).naechstesInListe(eintraege, ab)
                }
                if (erstes != null) onPlay(l.name, eintraege, erstes)
                else hinweis = tx(R.string.ab_hier_ist_nichts_was)
            } catch (e: LumenException) {
                hinweis = e.message
            }
            sucht = false
        }
    }

    /** Teilen oder Mitschreiben umschalten — sofort sichtbar, dann zum Server. */
    fun freigabe(geteilt: Boolean? = null, gemeinsam: Boolean? = null) {
        liste = l.copy(
            geteilt = geteilt ?: (l.geteilt || gemeinsam == true),
            gemeinsam = gemeinsam ?: (l.gemeinsam && geteilt != false))
        scope.launch {
            try {
                withContext(Dispatchers.IO) {
                    // Wer die Freigabe zurücknimmt, nimmt auch das
                    // Mitschreiben zurück — sonst käme es beim nächsten
                    // Teilen unbemerkt wieder.
                    if (geteilt == false) client.updatePlaylist(l.id, geteilt = false, gemeinsam = false)
                    else client.updatePlaylist(l.id, geteilt = geteilt, gemeinsam = gemeinsam)
                }
                onGeaendert()
            } catch (e: LumenException) {
                hinweis = e.message
            }
        }
    }

    // Der Fokus steht gleich auf „Abspielen" — das will man hier meistens.
    LaunchedEffect(l.id) {
        kotlinx.coroutines.delay(100)
        runCatching { abspielenFokus.requestFocus() }
    }

    Box(Modifier.fillMaxSize()) {
        TvHintergrund(client, fokus ?: l.items.firstOrNull())
        Column(Modifier.fillMaxSize().padding(start = 56.dp, top = 48.dp, end = 56.dp)) {
            Text(l.name, fontSize = 46.sp, fontWeight = FontWeight.Bold,
                 maxLines = 2, overflow = TextOverflow.Ellipsis)
            Spacer(Modifier.height(8.dp))
            Text(
                buildList {
                    add(l.anzahlText)
                    if (!l.meine) add(tx(R.string.von_x, l.von.name) + if (l.gemeinsam) tx(R.string.du_darfst_mitschreiben) else "")
                    else if (l.gemeinsam) add(tx(R.string.geteilt_alle_duerfen_mitschreiben))
                    else if (l.geteilt) add(tx(R.string.im_haushalt_geteilt))
                    else add(tx(R.string.nur_fuer_dich))
                }.joinToString(" · "),
                color = Tv.label2, fontSize = 18.sp)
            Spacer(Modifier.height(28.dp))
            LazyRow(horizontalArrangement = Arrangement.spacedBy(18.dp),
                    contentPadding = PaddingValues(vertical = 4.dp, horizontal = 4.dp),
                    verticalAlignment = Alignment.CenterVertically) {
                if (l.items.isNotEmpty()) {
                    item {
                        TvButton(tx(R.string.abspielen), gross = true,
                                 modifier = Modifier.focusRequester(abspielenFokus)) { abspielenAb(0) }
                    }
                }
                // Umbenennen, Teilen und Löschen nur für die Person, die die
                // Liste angelegt hat — der Server lässt sonst nichts zu.
                if (l.meine) {
                    item {
                        TvButton(tx(R.string.umbenennen),
                                 modifier = if (l.items.isEmpty()) Modifier.focusRequester(abspielenFokus)
                                            else Modifier) { umbenennen = true }
                    }
                    item {
                        TvButton(if (l.geteilt) tx(R.string.im_haushalt_zeigen_an) else tx(R.string.im_haushalt_zeigen_aus)) {
                            freigabe(geteilt = !l.geteilt)
                        }
                    }
                    item {
                        TvButton(if (l.gemeinsam) tx(R.string.alle_duerfen_mitschreiben_an) else tx(R.string.alle_duerfen_mitschreiben_aus)) {
                            freigabe(gemeinsam = !l.gemeinsam)
                        }
                    }
                    item { TvButton(tx(R.string.loeschen)) { loeschen = true } }
                }
            }
            if (l.meine) {
                Spacer(Modifier.height(10.dp))
                Text(if (l.geteilt) tx(R.string.wer_sie_sieht_sieht_nur)
                     else tx(R.string.nur_du_siehst_diese_liste),
                     color = Tv.label3, fontSize = 15.sp)
            }
            if (l.items.isEmpty()) {
                Spacer(Modifier.height(16.dp))
                Text(
                    if (l.darfAendern) tx(R.string.noch_leer_auf_der_seite)
                    else tx(R.string.diese_liste_ist_noch_leer),
                    color = Tv.label2, fontSize = 20.sp)
            }
            hinweis?.let {
                Spacer(Modifier.height(12.dp))
                Text(it, color = Tv.label2, fontSize = 17.sp)
            }
            Spacer(Modifier.height(24.dp))
            LazyRow(horizontalArrangement = Arrangement.spacedBy(28.dp),
                    contentPadding = PaddingValues(vertical = 16.dp, horizontal = 4.dp)) {
                items(l.items.size, key = { l.items[it].id }) { i ->
                    val titel = l.items[i]
                    TvCard(client, titel,
                        onFocus = { fokus = titel },
                        onClick = { onOpen(titel, l, i) },
                        // Wer etwas hineingelegt hat, zählt nur, wo mehrere mitschreiben.
                        unterzeile = if (l.gemeinsam || !l.meine) titel.hinzugefuegtVon?.let { tx(R.string.von_x, it) } else null)
                }
            }
        }
    }

    if (umbenennen) {
        TvNameDialog(tx(R.string.liste_umbenennen), l.name, tx(R.string.sichern), onDismiss = { umbenennen = false }) { name ->
            umbenennen = false
            liste = l.copy(name = name)
            scope.launch {
                try {
                    withContext(Dispatchers.IO) { client.updatePlaylist(l.id, name = name) }
                    onGeaendert()
                } catch (e: LumenException) {
                    hinweis = e.message
                }
            }
        }
    }

    if (loeschen) {
        TvFrage(
            titel = tx(R.string.x_loeschen, l.name),
            text = if (l.geteilt) tx(R.string.die_liste_verschwindet_auch_bei)
                   else tx(R.string.die_filme_selbst_bleiben),
            ja = tx(R.string.loeschen),
            onNein = { loeschen = false },
        ) {
            loeschen = false
            scope.launch {
                try {
                    withContext(Dispatchers.IO) { client.deletePlaylist(l.id) }
                    onGeloescht()
                } catch (e: LumenException) {
                    hinweis = e.message
                }
            }
        }
    }
}

// ---------------------------------------------------------------- Bausteine

@Composable
internal fun TvLaden(fehler: String?, onNochmal: (() -> Unit)? = null) {
    Mitte {
        if (fehler != null) {
            Column(horizontalAlignment = Alignment.CenterHorizontally) {
                Text(fehler, color = Tv.bad, fontSize = 20.sp)
                if (onNochmal != null) {
                    Spacer(Modifier.height(20.dp))
                    TvButton(tx(R.string.nochmal_versuchen)) { onNochmal() }
                }
            }
        } else {
            CircularProgressIndicator(color = Tv.label2)
        }
    }
}

/** Das Bild eines Titels gross im Hintergrund, abgedunkelt für die Schrift. */
@Composable
internal fun TvHintergrund(client: ApiClient, item: Item?) {
    if (item == null) return
    AsyncImage(
        model = client.wideUrl(item),
        contentDescription = null,
        contentScale = ContentScale.Crop,
        modifier = Modifier.fillMaxSize())
    Box(Modifier.fillMaxSize().background(Brush.horizontalGradient(
        listOf(Color(0xF2050507), Color(0xB3050507), Color(0xE6050507)))))
}

/**
 * Ein Titel als Karte: Poster (sonst ein Standbild), Abzeichen, Balken
 * oder Haken, Name darunter. Fokussiert wird sie grösser und weiss umrandet.
 */
@Composable
internal fun TvCard(
    client: ApiClient,
    item: Item,
    onFocus: () -> Unit,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    breite: Dp = 220.dp,
    hervorgehoben: Boolean = false,
    unterzeile: String? = null,
) {
    var fokussiert by remember { mutableStateOf(false) }
    val skalierung by animateFloatAsState(if (fokussiert) 1.08f else 1f, label = "fokus")

    Column(
        modifier
            .width(breite)
            .scale(skalierung)
            .rueckkehrFokus(item.id)
            .onFocusChanged { if (it.isFocused) { fokussiert = true; onFocus() } else fokussiert = false }
            .clickable(role = Role.Button, onClick = onClick)
    ) {
        Box {
            Box(
                Modifier
                    .width(breite).height(breite * 1.5f)
                    .clip(RoundedCornerShape(10.dp))
                    .background(Color(0xFF16161B))
                    .then(when {
                        fokussiert -> Modifier.border(3.dp, Color.White, RoundedCornerShape(10.dp))
                        hervorgehoben -> Modifier.border(2.dp, Tv.label2, RoundedCornerShape(10.dp))
                        else -> Modifier
                    }),
                contentAlignment = Alignment.Center,
            ) {
                // Poster, sonst ein Standbild aus dem Film — der Server
                // sagt, was es gibt.
                val url = client.posterUrl(item)
                if (url == null) {
                    Text(item.title, color = Tv.label3, fontSize = 16.sp,
                         textAlign = TextAlign.Center, modifier = Modifier.padding(12.dp))
                } else {
                    AsyncImage(model = url, contentDescription = null,
                               contentScale = ContentScale.Crop,
                               modifier = Modifier.fillMaxSize())
                }
            }
            // „Neu", „3 neue Folgen", 4K, HDR — wie im Browser.
            TvKartenAbzeichen(item, Modifier.align(Alignment.TopCenter))
            // Der Balken: beim Film der Stand, bei der Serie der Anteil der
            // eigenen gesehenen Folgen — der Server rechnet beides in progress.
            if (item.progress > 1 && !item.finished) {
                Box(Modifier.align(Alignment.BottomCenter).fillMaxWidth()
                        .height(4.dp).background(Color(0x73000000))) {
                    Box(Modifier
                        .fillMaxWidth((item.progress / 100).coerceIn(0.0, 1.0).toFloat())
                        .fillMaxHeight().background(Color.White))
                }
            }
            if (item.finished) {
                Box(Modifier.align(Alignment.BottomEnd).padding(8.dp).size(26.dp)
                        .clip(androidx.compose.foundation.shape.CircleShape).background(Color.White),
                    contentAlignment = Alignment.Center) {
                    Text("✓", color = Color.Black, fontSize = 14.sp, fontWeight = FontWeight.Bold)
                }
            }
        }
        Spacer(Modifier.height(10.dp))
        Text(item.title, fontSize = 16.sp, maxLines = 1, overflow = TextOverflow.Ellipsis,
             fontWeight = if (hervorgehoben) FontWeight.SemiBold else FontWeight.Normal)
        // „4 von 10 Folgen" unter einer angefangenen Serie, sonst Jahr und Wertung.
        val zweite = unterzeile ?: item.serienStand ?: listOfNotNull(
            item.jahresText,
            item.imdbRating?.takeIf { it > 0 }?.let { "★ " + Titelseite.komma(it) },
        ).joinToString(" · ")
        if (zweite.isNotEmpty()) {
            Text(zweite, color = Tv.label2, fontSize = 14.sp, maxLines = 1, overflow = TextOverflow.Ellipsis)
        }
    }
}

/** Eine Kachel für etwas, das kein einzelner Titel ist: eine Reihe, eine Liste. */
@Composable
internal fun TvKachel(bild: String?, titel: String, zeile: String,
                      symbol: String? = null, breite: Dp = 220.dp, onClick: () -> Unit) {
    var fokussiert by remember { mutableStateOf(false) }
    val skalierung by animateFloatAsState(if (fokussiert) 1.08f else 1f, label = "fokus")

    Column(
        Modifier
            .width(breite)
            .scale(skalierung)
            .onFocusChanged { fokussiert = it.isFocused }
            .clickable(role = Role.Button, onClick = onClick)
    ) {
        Box(
            Modifier
                .width(breite).height(breite * 1.5f)
                .clip(RoundedCornerShape(10.dp))
                .background(Color(0xFF16161B))
                .then(if (fokussiert)
                    Modifier.border(3.dp, Color.White, RoundedCornerShape(10.dp))
                else Modifier),
            contentAlignment = Alignment.Center,
        ) {
            if (symbol != null) {
                // Eine Kachel, die etwas tut statt etwas zu zeigen: „+".
                Text(symbol, color = Color.White, fontSize = 72.sp, fontWeight = FontWeight.Light)
            } else if (bild == null) {
                Text(titel, color = Tv.label3, fontSize = 16.sp,
                     textAlign = TextAlign.Center, modifier = Modifier.padding(12.dp))
            } else {
                AsyncImage(model = bild, contentDescription = null,
                           contentScale = ContentScale.Crop,
                           modifier = Modifier.fillMaxSize())
            }
        }
        Spacer(Modifier.height(10.dp))
        Text(titel, fontSize = 16.sp, maxLines = 1, overflow = TextOverflow.Ellipsis)
        Text(zeile, color = Tv.label2, fontSize = 14.sp, maxLines = 1,
             overflow = TextOverflow.Ellipsis)
    }
}

/**
 * Das Profilbild: Farbe mit Anfangsbuchstabe, eine eingebaute Figur oder
 * das eigene Bild ([bildUrl]).
 */
@Composable
internal fun TvAvatar(initial: String, key: String, size: Dp,
                      bildUrl: String? = null) {
    val figur = Profilbild.figur(key)
    val farben = when (key) {
        "av-b" -> listOf(Color(0xFFE67D54), Color(0xFFC75A4A))
        "av-c" -> listOf(Color(0xFFA885E8), Color(0xFF8261D1))
        "av-d" -> listOf(Color(0xFF9FD4D8), Color(0xFF3F8F99))
        "av-k" -> listOf(Color(0xFF7DC788), Color(0xFF54A66B))
        else -> listOf(Color(0xFF6B9EED), Color(0xFF4278DB))
    }
    Box(
        Modifier.size(size).clip(androidx.compose.foundation.shape.CircleShape)
            .then(if (figur != null) Modifier.background(Color(figur.farbe))
                  else Modifier.background(Brush.linearGradient(farben))),
        contentAlignment = Alignment.Center,
    ) {
        if (figur != null) {
            TvFigur(figur, Modifier.fillMaxSize(0.62f))
        } else {
            Text(initial, fontSize = (size.value * 0.4f).sp,
                 fontWeight = FontWeight.Medium, color = Color.White)
        }
        if (bildUrl != null) {
            AsyncImage(model = bildUrl, contentDescription = null,
                       contentScale = ContentScale.Crop, modifier = Modifier.fillMaxSize())
        }
    }
}

@Composable
internal fun TvFocusable(
    modifier: Modifier = Modifier,
    onClick: () -> Unit,
    content: @Composable () -> Unit,
) {
    var fokussiert by remember { mutableStateOf(false) }
    val skalierung by animateFloatAsState(if (fokussiert) 1.1f else 1f, label = "fokus")
    Box(
        modifier
            .scale(skalierung)
            .onFocusChanged { fokussiert = it.isFocused }
            .clickable(role = Role.Button, onClick = onClick)
    ) { content() }
}

@Composable
internal fun TvButton(
    text: String,
    modifier: Modifier = Modifier,
    gross: Boolean = false,
    onClick: () -> Unit,
) {
    var fokussiert by remember { mutableStateOf(false) }
    Box(
        modifier
            .onFocusChanged { fokussiert = it.isFocused }
            .clickable(role = Role.Button, onClick = onClick)
            .clip(RoundedCornerShape(if (gross) 34.dp else 28.dp))
            .background(if (fokussiert) Color.White else Color(0x22FFFFFF))
            .padding(horizontal = if (gross) 40.dp else 28.dp,
                     vertical = if (gross) 16.dp else 12.dp),
    ) {
        Text(text, fontSize = if (gross) 22.sp else 18.sp, fontWeight = FontWeight.SemiBold,
             color = if (fokussiert) Color.Black else Color.White, maxLines = 1)
    }
}
