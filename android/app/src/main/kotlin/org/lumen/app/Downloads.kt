// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package org.lumen.app

import android.Manifest
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.content.pm.ServiceInfo
import android.net.Uri
import android.os.Build
import androidx.activity.compose.BackHandler
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.ArrowBack
import androidx.compose.material.icons.filled.Delete
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.ui.viewinterop.AndroidView
import androidx.core.app.NotificationCompat
import androidx.media3.common.MediaItem
import androidx.media3.exoplayer.ExoPlayer
import androidx.media3.ui.PlayerView
import androidx.work.Constraints
import androidx.work.CoroutineWorker
import androidx.work.ExistingWorkPolicy
import androidx.work.ForegroundInfo
import androidx.work.NetworkType
import androidx.work.OneTimeWorkRequestBuilder
import androidx.work.WorkManager
import androidx.work.WorkerParameters
import androidx.work.workDataOf
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import org.lumen.core.*
import java.io.File

// ---------------------------------------------------------------- Ablage

/**
 * Wo heruntergeladene Filme liegen: im eigenen Ordner der App auf dem
 * Gerät (keine Berechtigung nötig, beim Deinstallieren weg). Neben jeder
 * Datei eine kleine JSON-Datei mit dem, was die Liste und der Player ohne
 * Netz brauchen — und dem Stand beim Offline-Schauen, bis er beim Server ist.
 *
 * Downloads gehören einem Profil auf einem Server: die Liste zeigt nur
 * die des angemeldeten Profils.
 */
internal class DownloadAblage(private val context: Context) {
    private val ordner: File
        get() = File(context.getExternalFilesDir(null) ?: context.filesDir, "downloads").also { it.mkdirs() }

    private fun metaDatei(itemId: String) = File(ordner, Herunterladen.sichererName(itemId) + ".json")

    /** Die Datei des Films selbst. */
    fun datei(d: Heruntergeladen): File =
        File(ordner, Herunterladen.sichererName(d.itemId) + "-" + Herunterladen.sichererName(d.dateiname))

    fun lesen(itemId: String): Heruntergeladen? =
        metaDatei(itemId).takeIf { it.exists() }?.let { Heruntergeladen.aus(it.readText()) }

    /** Alle Downloads dieses Profils, die neuesten zuerst. */
    fun alle(kontoId: String, serverId: String): List<Heruntergeladen> =
        (ordner.listFiles { f -> f.name.endsWith(".json") } ?: emptyArray())
            .sortedByDescending { it.lastModified() }
            .mapNotNull { Heruntergeladen.aus(it.readText()) }
            .filter { it.kontoId == kontoId && it.serverId == serverId }

    /** Schreiben — über eine Zwischendatei, damit nie eine halbe übrig bleibt. */
    @Synchronized fun schreiben(d: Heruntergeladen) {
        val ziel = metaDatei(d.itemId)
        val tmp = File(ordner, ziel.name + ".tmp")
        tmp.writeText(d.alsJson())
        if (!tmp.renameTo(ziel)) { ziel.delete(); tmp.renameTo(ziel) }
    }

    /** Nur, wenn es den Eintrag noch gibt — ein gelöschter Download kommt so nicht zurück. */
    @Synchronized fun aktualisieren(d: Heruntergeladen) {
        if (metaDatei(d.itemId).exists()) schreiben(d)
    }

    @Synchronized fun loeschen(d: Heruntergeladen) {
        datei(d).delete()
        metaDatei(d.itemId).delete()
    }

    /**
     * Den Stand vom Offline-Schauen zum Server bringen — sobald es wieder
     * Netz gibt. Was nicht ankommt, bleibt markiert und versucht es beim
     * nächsten Mal wieder. Blockiert.
     */
    fun abgleichen(client: ApiClient, kontoId: String, serverId: String) {
        for (d in alle(kontoId, serverId).filter { it.stelleOffen && it.dauer > 0 }) {
            try {
                client.saveProgress(d.itemId, d.stelle, d.dauer)
                if (d.stelle / d.dauer >= SessionController.FINISHED_AT) client.markFinished(d.itemId)
                aktualisieren(d.copy(stelleOffen = false))
            } catch (e: LumenException) {
                return // kein Netz oder kein Server — später noch einmal
            }
        }
    }

    companion object {
        /** Einen Download anstossen — er läuft weiter, auch wenn die App zu ist. */
        fun starten(context: Context, d: Heruntergeladen) {
            DownloadAblage(context).schreiben(d)
            val auftrag = OneTimeWorkRequestBuilder<DownloadArbeiter>()
                .setInputData(workDataOf(DownloadArbeiter.ITEM to d.itemId))
                .setConstraints(Constraints.Builder().setRequiredNetworkType(NetworkType.CONNECTED).build())
                .addTag(DownloadArbeiter.TAG)
                .build()
            WorkManager.getInstance(context)
                .enqueueUniqueWork(DownloadArbeiter.name(d.itemId), ExistingWorkPolicy.KEEP, auftrag)
        }

        fun entfernen(context: Context, d: Heruntergeladen) {
            WorkManager.getInstance(context).cancelUniqueWork(DownloadArbeiter.name(d.itemId))
            DownloadAblage(context).loeschen(d)
        }
    }
}

// ---------------------------------------------------------------- Im Hintergrund

/**
 * Lädt eine Datei herunter — als Arbeit von WorkManager, mit einer
 * Benachrichtigung, die den Fortschritt zeigt. Bricht die Verbindung ab,
 * versucht WorkManager es später wieder, und der Download setzt dort fort,
 * wo er war (Range).
 */
class DownloadArbeiter(context: Context, params: WorkerParameters) : CoroutineWorker(context, params) {

    companion object {
        const val ITEM = "itemId"
        const val TAG = "lumen-download"
        const val KANAL = "downloads"
        fun name(itemId: String) = "download-$itemId"
    }

    override suspend fun doWork(): Result {
        val itemId = inputData.getString(ITEM) ?: return Result.failure()
        val ablage = DownloadAblage(applicationContext)
        val d = ablage.lesen(itemId) ?: return Result.failure()
        val client = clientFuer(d.serverId)
        if (client == null) {
            ablage.aktualisieren(d.copy(zustand = Heruntergeladen.FEHLER,
                fehler = tx(R.string.wechsle_in_lumen_zu_diesem)))
            return Result.failure()
        }
        try {
            setForeground(vordergrund(d))
        } catch (e: IllegalStateException) {
            // Android 12+ lässt aus dem Hintergrund keinen Vordergrunddienst
            // starten. Dann eben ohne — WorkManager setzt später fort.
        }
        val ziel = ablage.datei(d)
        var zuletzt = 0L
        return try {
            withContext(Dispatchers.IO) {
                client.herunterladen(d.fileId, ziel,
                    fortschritt = { geladen, gesamt ->
                        val jetzt = System.currentTimeMillis()
                        if (jetzt - zuletzt > 1000) {
                            zuletzt = jetzt
                            val neu = d.copy(zustand = Heruntergeladen.LAEDT, geladen = geladen,
                                             groesse = if (gesamt > 0) gesamt else d.groesse, fehler = null)
                            ablage.aktualisieren(neu)
                            setForegroundAsync(vordergrund(neu))
                        }
                    },
                    weiter = { !isStopped })
            }
            if (isStopped) {
                Result.retry()
            } else {
                val groesse = ziel.length()
                ablage.aktualisieren(d.copy(zustand = Heruntergeladen.FERTIG, geladen = groesse,
                                            groesse = groesse, fehler = null))
                Result.success()
            }
        } catch (e: LumenException.Unreachable) {
            ablage.aktualisieren(d.copy(zustand = Heruntergeladen.LAEDT, geladen = ziel.length(),
                                        fehler = tx(R.string.wartet_auf_den_server)))
            Result.retry()
        } catch (e: LumenException) {
            ablage.aktualisieren(d.copy(zustand = Heruntergeladen.FEHLER, geladen = ziel.length(),
                                        fehler = Herunterladen.meldung(e)))
            Result.failure()
        }
    }

    /**
     * Ein Client für den Server des Downloads. Nur für den gewählten Server
     * — dessen Adresse und Token kennt die Ablage. Läuft die App noch, wird
     * ihr Client genommen (mit seinem Medienschlüssel).
     */
    private fun clientFuer(serverId: String): ApiClient? {
        val app = applicationContext as LumenApplication
        val store = app.store
        if (store.aktiv?.id != serverId) return null
        app.client?.takeIf { it.isSignedIn }?.let { return it }
        val adresse = store.serverUrl ?: return null
        val token = store.token ?: return null
        return ApiClient(adresse, token).also { it.pinnen(store.fingerabdruck) }
    }

    private fun vordergrund(d: Heruntergeladen): ForegroundInfo {
        val nm = applicationContext.getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager
        // Jedes Mal anlegen: ein bestehender Kanal behält alles ausser dem
        // Namen — der folgt so der Sprache, die gerade gilt.
        nm.createNotificationChannel(NotificationChannel(KANAL, tx(R.string.downloads), NotificationManager.IMPORTANCE_LOW))
        val oeffnen = PendingIntent.getActivity(applicationContext, 0,
            Intent(applicationContext, MainActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_SINGLE_TOP),
            PendingIntent.FLAG_IMMUTABLE)
        val n = NotificationCompat.Builder(applicationContext, KANAL)
            .setSmallIcon(android.R.drawable.stat_sys_download)
            .setContentTitle(d.titel)
            .setContentText(if (d.groesse > 0) tx(R.string.x_von_x, d.prozent, Herunterladen.groesse(d.groesse))
                            else tx(R.string.wird_heruntergeladen))
            .setProgress(100, d.prozent, d.groesse <= 0)
            .setOngoing(true)
            .setOnlyAlertOnce(true)
            .setContentIntent(oeffnen)
            .build()
        val nr = d.itemId.hashCode()
        return if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
            ForegroundInfo(nr, n, ServiceInfo.FOREGROUND_SERVICE_TYPE_DATA_SYNC)
        } else {
            ForegroundInfo(nr, n)
        }
    }
}

// ---------------------------------------------------------------- Knopf auf der Titelseite

/**
 * „Herunterladen" auf der Titelseite eines Films oder einer Folge — nur,
 * wenn der Server es kennt und dem Profil erlaubt. Läuft der Download,
 * zeigt der Knopf, wie weit; ist er fertig, steht das da.
 */
@Composable
fun DownloadKnopf(model: LumenViewModel, item: Item, onZuDenDownloads: () -> Unit) {
    val context = LocalContext.current
    val ablage = remember { DownloadAblage(context) }
    var info by remember(item.id) { mutableStateOf<DownloadInfo?>(null) }
    var eintrag by remember(item.id) { mutableStateOf(ablage.lesen(item.id)) }
    var wahlOffen by remember { mutableStateOf(false) }
    var meldung by remember { mutableStateOf<String?>(null) }
    // Android 13+: ohne Erlaubnis keine Benachrichtigung — der Download läuft
    // trotzdem, man sieht ihn dann nur in der App.
    val erlaubnis = rememberLauncherForActivityResult(ActivityResultContracts.RequestPermission()) { }

    LaunchedEffect(item.id) { info = model.abfragen { it.downloadInfo(item.id) }.wert }
    // Solange er lädt, alle zwei Sekunden nachsehen.
    LaunchedEffect(item.id, eintrag?.zustand) {
        while (eintrag?.zustand == Heruntergeladen.LAEDT) {
            delay(2000)
            eintrag = ablage.lesen(item.id)
        }
    }

    fun starten(datei: DownloadDatei) {
        val ich = model.me?.account?.id ?: return
        val server = model.aktiverServer?.id ?: return
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            erlaubnis.launch(Manifest.permission.POST_NOTIFICATIONS)
        }
        val d = Heruntergeladen(
            itemId = item.id, fileId = datei.fileId, titel = item.title,
            unterzeile = if (item.kind == "episode")
                Titelseite.folgenKennung(item)
            else item.jahresText ?: "",
            dateiname = datei.dateiname, groesse = datei.groesse, zustand = Heruntergeladen.LAEDT,
            dauer = item.runtimeSeconds, kontoId = ich, serverId = server)
        DownloadAblage.starten(context, d)
        eintrag = d
        meldung = null
    }

    val e = eintrag
    val i = info
    when {
        e != null && e.fertig -> QuietButton(tx(R.string.heruntergeladen), onClick = onZuDenDownloads)
        e != null && e.zustand == Heruntergeladen.LAEDT ->
            QuietButton(if (e.groesse > 0) tx(R.string.laedt_x, e.prozent) else tx(R.string.laedt), onClick = onZuDenDownloads)
        i == null || !i.erlaubt || i.dateien.isEmpty() -> Unit
        else -> QuietButton(if (e?.zustand == Heruntergeladen.FEHLER) tx(R.string.erneut_herunterladen) else tx(R.string.herunterladen)) {
            if (i.dateien.size == 1) starten(i.dateien[0]) else wahlOffen = true
        }
    }
    if (wahlOffen && i != null) {
        AlertDialog(
            onDismissRequest = { wahlOffen = false },
            title = { Text(tx(R.string.welche_fassung)) },
            text = {
                Column {
                    i.dateien.forEach { f ->
                        TextButton(onClick = { wahlOffen = false; starten(f) }) { Text(f.beschriftung) }
                    }
                }
            },
            confirmButton = { TextButton(onClick = { wahlOffen = false }) { Text(tx(R.string.abbrechen)) } },
        )
    }
    meldung?.let { Text(it, color = Lumen.bad, fontSize = 13.sp) }
}

// ---------------------------------------------------------------- Liste

/**
 * „Downloads" im Profil: was auf diesem Gerät liegt — zum Abspielen ohne
 * Netz und zum Löschen. Beim Öffnen geht der Stand vom Offline-Schauen an
 * den Server, falls es wieder Netz gibt.
 */
@Composable
fun DownloadsScreen(model: LumenViewModel, onBack: () -> Unit, onPlay: (Heruntergeladen) -> Unit) {
    val context = LocalContext.current
    val ablage = remember { DownloadAblage(context) }
    val ich = model.me?.account?.id ?: ""
    val server = model.aktiverServer?.id ?: ""
    var liste by remember { mutableStateOf(ablage.alle(ich, server)) }
    var loeschen by remember { mutableStateOf<Heruntergeladen?>(null) }
    val scope = rememberCoroutineScope()

    LaunchedEffect(Unit) {
        withContext(Dispatchers.IO) { ablage.abgleichen(model.client, ich, server) }
        // Laufende Downloads: alle zwei Sekunden auffrischen.
        while (true) {
            liste = withContext(Dispatchers.IO) { ablage.alle(ich, server) }
            delay(2000)
        }
    }

    BackHandler { onBack() }
    LazyColumn(contentPadding = PaddingValues(16.dp)) {
        item {
            IconButton(onClick = onBack) { Icon(Icons.Filled.ArrowBack, tx(R.string.zurueck), tint = Color.White) }
            Text(tx(R.string.downloads), fontSize = 28.sp, fontWeight = FontWeight.Bold)
            Text(tx(R.string.zum_schauen_ohne_netz_nur),
                 color = Lumen.label2, fontSize = 14.sp)
            Spacer(Modifier.height(16.dp))
        }
        if (liste.isEmpty()) {
            item {
                Text(tx(R.string.noch_nichts_heruntergeladen_auf_der),
                     color = Lumen.label2)
            }
        }
        items(liste, key = { it.itemId }) { d ->
            Row(Modifier.fillMaxWidth().padding(vertical = 8.dp), verticalAlignment = Alignment.CenterVertically) {
                Column(Modifier.weight(1f)) {
                    Text(d.titel, fontSize = 16.sp, maxLines = 1, overflow = TextOverflow.Ellipsis)
                    val zeile = when (d.zustand) {
                        Heruntergeladen.FERTIG -> listOf(d.unterzeile, Herunterladen.groesse(d.groesse))
                            .filter { it.isNotEmpty() }.joinToString(" · ")
                        Heruntergeladen.LAEDT -> d.fehler ?: if (d.groesse > 0) tx(R.string.laedt_x, d.prozent) else tx(R.string.laedt)
                        else -> d.fehler ?: tx(R.string.der_download_hat_nicht_geklappt)
                    }
                    Text(zeile, fontSize = 13.sp,
                         color = if (d.zustand == Heruntergeladen.FEHLER) Lumen.bad else Lumen.label2)
                    if (d.zustand == Heruntergeladen.LAEDT && d.groesse > 0) {
                        Spacer(Modifier.height(4.dp))
                        LinearProgressIndicator(progress = { d.prozent / 100f },
                                                modifier = Modifier.fillMaxWidth(), color = Lumen.label)
                    }
                }
                if (d.fertig) {
                    IconButton(onClick = { onPlay(d) }) { Icon(Icons.Filled.PlayArrow, tx(R.string.abspielen), tint = Color.White) }
                } else if (d.zustand == Heruntergeladen.FEHLER) {
                    TextButton(onClick = { DownloadAblage.starten(context, d.copy(zustand = Heruntergeladen.LAEDT, fehler = null)) }) {
                        Text(tx(R.string.erneut), color = Lumen.label2)
                    }
                }
                IconButton(onClick = { loeschen = d }) { Icon(Icons.Filled.Delete, tx(R.string.loeschen), tint = Lumen.label2) }
            }
        }
    }

    loeschen?.let { d ->
        AlertDialog(
            onDismissRequest = { loeschen = null },
            title = { Text(if (d.fertig) tx(R.string.download_loeschen) else tx(R.string.download_abbrechen)) },
            text = { Text(tx(R.string.der_titel_bleibt_auf_dem)) },
            confirmButton = {
                TextButton(onClick = {
                    loeschen = null
                    scope.launch {
                        withContext(Dispatchers.IO) { DownloadAblage.entfernen(context, d) }
                        liste = liste.filter { it.itemId != d.itemId }
                    }
                }) { Text(tx(R.string.loeschen), color = Lumen.bad) }
            },
            dismissButton = { TextButton(onClick = { loeschen = null }) { Text(tx(R.string.behalten)) } },
        )
    }
}

// ---------------------------------------------------------------- Ohne Netz abspielen

/**
 * Ein heruntergeladener Titel, ohne Netz. Der Stand wird hier gemerkt
 * (alle paar Sekunden und beim Verlassen) und geht an den Server, sobald
 * es wieder Netz gibt — sofort versucht beim Schliessen, sonst beim
 * nächsten Öffnen der Downloads. Untertitel und Tonspuren in der Datei
 * wählt man über die Knöpfe des Players.
 */
@androidx.annotation.OptIn(androidx.media3.common.util.UnstableApi::class)
@Composable
fun OfflineSpieler(model: LumenViewModel, start: Heruntergeladen, onClose: () -> Unit) {
    val context = LocalContext.current
    val ablage = remember { DownloadAblage(context) }
    val datei = remember { ablage.datei(start) }
    val player = remember {
        ExoPlayer.Builder(context).build().apply {
            setMediaItem(MediaItem.fromUri(Uri.fromFile(datei)), (start.stelle * 1000).toLong())
            prepare()
            playWhenReady = true
        }
    }
    val ich = model.me?.account?.id ?: ""
    val server = model.aktiverServer?.id ?: ""

    fun merken() {
        val dauer = (player.duration.takeIf { it > 0 } ?: 0L) / 1000.0
        val stelle = player.currentPosition / 1000.0
        if (stelle <= 0) return
        val alt = ablage.lesen(start.itemId) ?: return
        ablage.aktualisieren(alt.copy(stelle = stelle, dauer = if (dauer > 0) dauer else alt.dauer, stelleOffen = true))
    }

    Vollbild()
    BeimVerlassenAnhalten(player) { merken() }
    val imFenster = imBildImBild(aktiv = true)
    Tasten(player)
    LaunchedEffect(Unit) {
        while (true) { delay(5000); merken() }
    }
    DisposableEffect(Unit) {
        onDispose {
            merken()
            player.release()
            // Gibt es Netz, gleich zum Server damit; sonst später.
            model.send { c -> ablage.abgleichen(c, ich, server) }
        }
    }
    BackHandler { onClose() }

    Box(Modifier.fillMaxSize().background(Color.Black)) {
        AndroidView(
            factory = { ctx ->
                PlayerView(ctx).apply {
                    this.player = player
                    useController = true
                    setShowSubtitleButton(true)
                    setShowNextButton(false)
                    setShowPreviousButton(false)
                    controllerShowTimeoutMs = 2800
                }
            },
            update = { it.useController = !imFenster },
            modifier = Modifier.fillMaxSize())
        if (!datei.exists()) {
            Text(tx(R.string.die_datei_ist_nicht_mehr), modifier = Modifier.align(Alignment.Center))
        }
    }
}

