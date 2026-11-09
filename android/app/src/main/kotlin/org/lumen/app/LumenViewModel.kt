// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package org.lumen.app

import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.lifecycle.ViewModel
import androidx.lifecycle.ViewModelProvider
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import org.lumen.core.*

/**
 * Hält den Zustand für die Oberfläche.
 *
 * [SessionController] und [ApiClient] blockieren — das ist Absicht, so
 * bleiben sie ohne Android prüfbar. Hier werden sie in einen
 * Hintergrund-Zusammenhang gelegt; der Hauptfaden bleibt frei.
 */
class LumenViewModel(private val store: Serverliste) : ViewModel() {

    private val controller = SessionController(store)
    val client: ApiClient get() = controller.client

    private val _stage = MutableStateFlow<Stage>(Stage.StartingUp)
    val stage: StateFlow<Stage> = _stage.asStateFlow()

    private val _error = MutableStateFlow<String?>(null)
    val error: StateFlow<String?> = _error.asStateFlow()

    private val _busy = MutableStateFlow(false)
    val busy: StateFlow<Boolean> = _busy.asStateFlow()

    /**
     * Ein Einladungscode aus einem Link, bis die Ansicht ihn abholt. Ein
     * Compose-Zustand, damit auch ein Link ankommt, während die App schon
     * läuft (onNewIntent) — vorher wurde der still übergangen.
     */
    var pendingInvite: String? by mutableStateOf(null)

    // ---------------------------------------------------------- Was offen ist

    /**
     * Was gerade abgespielt wird. Hier statt in der Ansicht, damit es eine
     * Neuzusammensetzung der Oberfläche übersteht.
     */
    var abspielen: Abspielen? by mutableStateOf(null)

    /** Was ohne Netz läuft — ein heruntergeladener Titel. */
    var offline: Heruntergeladen? by mutableStateOf(null)

    /**
     * Was die Startseite zuletzt gezeigt hat. Kommt man von einem Titel
     * zurück, steht sie sofort wieder da — samt Scrollstelle — und wird
     * nur still im Hintergrund aufgefrischt, statt leer neu zu laden.
     */
    var startseite: Startseite? = null

    // ---------------------------------------------------------- Chromecast

    /** Ob ein Chromecast verbunden ist — dann spielt „Abspielen" dort. */
    var castVerbunden: Boolean by mutableStateOf(false)

    /** Ob das, was in [abspielen] steht, gerade auf dem Chromecast läuft. */
    var aufCast: Boolean by mutableStateOf(false)

    /** „Hier abspielen" trotz Chromecast — gilt für den einen Titel. */
    var castAbgelehnt: Abspielen? by mutableStateOf(null)

    /** Wie der Chromecast den Server erreicht — oder warum nicht. Null: noch unbekannt. */
    var castAdresse: CastAdresse.Ergebnis? by mutableStateOf(null)
        private set
    private var castAdresseFuer: String? = null

    /** Einmal je Server ermitteln (löst dafür im Hintergrund einen Namen auf). */
    suspend fun castBasisErmitteln() {
        val url = client.baseUrl
        if (castAdresseFuer == url && castAdresse != null) return
        castAdresse = CastHilfe.basisErmitteln(url, aktiverServer?.vonUnterwegs == true)
        castAdresseFuer = url
    }

    suspend fun castBasis(): CastAdresse.Ergebnis? { castBasisErmitteln(); return castAdresse }

    /** Der Server kennt PUT /me/sperre nicht (404): der Punkt bleibt weg. */
    var sperreUnbekannt: Boolean by mutableStateOf(false)

    /** Die zuletzt geladenen Raster (Filme, Serien), je nach Wahl. */
    val raster = HashMap<String, Seite>()

    val me: Me? get() = (stage.value as? Stage.SignedIn)?.me
    /** Nur der Besitzer — für das, was der Server nur ihm erlaubt. */
    val isOwner: Boolean get() = me?.account?.isOwner == true
    /** Wer am Server Vollzugriff hat — nur für Hinweistexte; verwaltet wird
     *  auf Android nichts. */
    val isAdmin: Boolean get() = me?.account?.isAdmin == true
    val serverVersion: String get() = controller.serverVersion
    /** Die Sprache für die Oberfläche: des Profils, sonst des Servers; null: keine Angabe. */
    val sprache: String? get() = controller.sprache
    val serverSprache: String? get() = controller.serverSprache
    val serverSprachen: List<Sprache> get() = controller.serverSprachen
    val hasFfmpeg: Boolean get() = controller.hasFfmpeg
    val license: String get() = controller.license
    val sourceUrl: String? get() = controller.sourceUrl

    init { run { controller.start() } }

    /**
     * Zählt hoch, sobald ein eigenes Profilbild hochgeladen wurde — es
     * hängt als ?v= an der Bildadresse, damit nicht das alte aus dem
     * Zwischenspeicher kommt.
     */
    private val _avatarStand = MutableStateFlow(0L)
    val avatarStand: StateFlow<Long> = _avatarStand.asStateFlow()
    fun avatarGeaendert() { _avatarStand.value = System.currentTimeMillis() }

    /** Die Bildadresse zu einem Profil, falls es ein eigenes Bild hat. */
    fun profilbildUrl(accountId: String?, avatar: String?): String? =
        client.profilbildUrl(accountId, avatar, _avatarStand.value)

    fun useServer(address: String, fingerabdruck: String? = null) =
        run { controller.useServer(address, fingerabdruck) }
    fun chooseProfile(card: ProfileCard) = run { controller.chooseProfile(card) }
    fun unlock(card: ProfileCard, secret: String) = run { controller.unlock(card, secret) }
    /** Von unterwegs: Profilname und PIN oder Passwort statt der Auswahl. */
    fun unlockByName(name: String, secret: String) = run { controller.unlockByName(name, secret) }
    fun setUpOwner(name: String, password: String) = run { controller.setUpOwner(name, password) }
    fun redeemInvite(code: String, name: String, pin: String) =
        run { controller.redeemInvite(code, name, pin) }
    /** Eine Einladung einlösen — mit Passwort, PIN oder ohne, wie im Browser. */
    fun redeemInvite(code: String, name: String, lockKind: String, secret: String) =
        run { controller.redeemInvite(code, name, lockKind, secret) }
    /** Die Meldung von vorhin wegräumen — etwa beim Wechsel zwischen zwei Ansichten. */
    fun fehlerWeg() { _error.value = null }
    fun signOut() = run { controller.signOut() }
    fun refreshMe() = run { controller.refreshMe() }

    // ---------------------------------------------------------- Mehrere Server

    /** Die gespeicherten Server und der gewählte — fürs Profil und die Liste. */
    val server: List<GespeicherterServer> get() = controller.server
    val aktiverServer: GespeicherterServer? get() = controller.aktiverServer
    fun angemeldetBei(id: String): Boolean = controller.serverliste?.angemeldet(id) == true

    /** „Server wechseln": zur Liste, ohne abzumelden. */
    fun serverWahl() = run { controller.serverWahl() }
    fun serverHinzufuegen() = run { controller.serverHinzufuegen() }
    fun verbinden(id: String) = run { controller.verbinden(id) }
    fun serverUmbenennen(id: String, name: String) = run { controller.serverUmbenennen(id, name) }
    fun serverEntfernen(id: String) = run { controller.serverEntfernen(id) }

    // ---------------------------------------------------------- Mit Code

    /** Einen Code holen, den jemand an einem anderen Gerät freigibt. */
    suspend fun codeStarten(): Pair<AnmeldeCode?, String?> = withContext(Dispatchers.IO) {
        val c = controller.codeStarten()
        c to controller.lastError
    }

    /** Einmal nachfragen; ist er freigegeben, geht es in die Mediathek. */
    suspend fun codeNachfragen(geheim: String): CodeStand {
        val stand = withContext(Dispatchers.IO) { controller.codeNachfragen(geheim) }
        if (stand == CodeStand.ANGEMELDET) {
            _stage.value = controller.stage
            _error.value = null
        }
        return stand
    }

    /**
     * Einen Fernseher anmelden: den Code freigeben, der dort steht. Zurück
     * kommt der Name des Geräts — oder null und die Meldung des Servers.
     */
    suspend fun fernseherAnmelden(code: String): Pair<String?, String?> = withContext(Dispatchers.IO) {
        val geraet = controller.codeFreigeben(code)
        geraet to controller.lastError
    }

    private fun run(block: () -> Unit) {
        viewModelScope.launch {
            _busy.value = true
            withContext(Dispatchers.IO) { block() }
            _stage.value = controller.stage
            _error.value = controller.lastError
            _busy.value = false
        }
    }

    /**
     * Eine Abfrage im Hintergrund, mit Fehlerbehandlung an einer Stelle.
     * Gibt null zurück, wenn es schiefging — die Meldung steht dann in
     * [error].
     */
    suspend fun <T> fetch(block: (ApiClient) -> T): T? = withContext(Dispatchers.IO) {
        try {
            val result = block(client)
            withContext(Dispatchers.Main) { _error.value = null }
            result
        } catch (e: LumenException.SignedOut) {
            // Abmelden spricht noch mit dem Server (Gerät austragen, dann
            // die Profilwahl holen) — das gehört in den Hintergrund, in dem
            // wir hier schon sind. Auf dem Hauptfaden wäre es bei einem
            // abgelaufenen Token ein NetworkOnMainThreadException-Absturz.
            controller.signOut()
            val danach = controller.stage
            withContext(Dispatchers.Main) { _stage.value = danach }
            null
        } catch (e: LumenException) {
            withContext(Dispatchers.Main) { _error.value = e.message }
            null
        }
    }

    /** Etwas abschicken, bei dem nur ein Fehler interessiert. */
    fun send(onDone: (() -> Unit)? = null, block: (ApiClient) -> Unit) {
        viewModelScope.launch {
            fetch { block(it) }
            onDone?.invoke()
        }
    }

    /**
     * Wie [fetch], aber die Meldung kommt zurück statt in das gemeinsame
     * [error] — für Ansichten mit eigenem Fehlerzustand und „Nochmal
     * versuchen", die schon Geladenes nicht mit einer Meldung überdecken
     * wollen.
     */
    suspend fun <T> abfragen(block: (ApiClient) -> T): Antwort<T> = withContext(Dispatchers.IO) {
        try {
            Antwort(block(client), null)
        } catch (e: LumenException.SignedOut) {
            controller.signOut()
            val danach = controller.stage
            withContext(Dispatchers.Main) { _stage.value = danach }
            Antwort(null, e.message)
        } catch (e: LumenException) {
            Antwort(null, e.message ?: tx(R.string.das_hat_nicht_geklappt))
        }
    }

    class Factory(private val store: Serverliste) : ViewModelProvider.Factory {
        @Suppress("UNCHECKED_CAST")
        override fun <T : ViewModel> create(modelClass: Class<T>): T =
            LumenViewModel(store) as T
    }
}

/** Ein Ergebnis oder die Meldung, warum keines kam. */
data class Antwort<T>(val wert: T?, val fehler: String?)

/** Was die Startseite zeigt: die Reihen des Servers und die Filmreihen. */
data class Startseite(val reihen: List<HomeRow>, val filmreihen: List<Reihe>)
