// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package org.lumen.core

/**
 * Der Zustand der App — ohne Android, damit er sich testen lässt.
 *
 * Die Oberfläche liest daraus und ruft die Methoden auf. Wo die Daten
 * herkommen und wo das Token liegt, weiss sie nicht.
 */
sealed interface Stage {
    data object StartingUp : Stage
    data class NeedsServer(val message: String? = null) : Stage
    /** Die gespeicherten Server — antippen verbindet, wie bei Emby. */
    data class ChoosingServer(
        val server: List<GespeicherterServer>,
        val aktivId: String? = null,
        val message: String? = null,
    ) : Stage
    data object NeedsSetup : Stage
    /**
     * Die Profilwahl. Mit [unterwegs] gibt es keine Auswahl: Der Server
     * zeigt von aussen keine Profile (sie verrieten die Namen im Haushalt),
     * und die App bietet stattdessen „Von unterwegs anmelden" mit Profilname
     * und PIN oder Passwort an — und den Code von einem anderen Gerät.
     * [profiles] ist dann leer.
     */
    data class ChoosingProfile(
        val profiles: List<ProfileCard>,
        val unterwegs: Boolean = false,
    ) : Stage
    data class Unlocking(val profile: ProfileCard) : Stage
    data class SignedIn(val me: Me) : Stage
}

/** Wo Serveradresse und Token liegen. Auf Android verschlüsselt. */
interface TokenStore {
    var serverUrl: String?
    var token: String?

    /** Der Fingerabdruck des Server-Zertifikats, falls er selbst eines
     *  ausgestellt hat. Nur für https nötig — im Heimnetz über http gibt
     *  es nichts festzunageln. */
    var fingerabdruck: String?
}

/** Eine Ablage im Arbeitsspeicher — für Tests und die Kommandozeile. */
class MemoryTokenStore(
    override var serverUrl: String? = null,
    override var token: String? = null,
    override var fingerabdruck: String? = null,
) : TokenStore

/**
 * Führt durch die Schritte bis zur Mediathek.
 *
 * Alle Methoden blockieren; auf Android ruft sie das ViewModel in einem
 * Hintergrund-Zusammenhang auf. So bleibt diese Klasse frei von Kotlin-
 * Nebenläufigkeit und damit prüfbar ohne Gerät.
 */
class SessionController(
    private val store: TokenStore,
    val client: ApiClient = ApiClient(store.serverUrl ?: "", store.token),
) {
    var stage: Stage = Stage.StartingUp
        private set
    var serverVersion: String = ""
        private set
    var hasFfmpeg: Boolean = true
        private set

    // Lizenz und Fundstelle des Quelltexts, wie der Server sie nennt. Die AGPL
    // verlangt, dass wer den Dienst benutzt, daran kommt — die Einstellungen
    // zeigen beides unter „Über Lumen".
    var license: String = ""
        private set
    var sourceUrl: String? = null
        private set
    var lastError: String? = null
        private set

    // Die Sprache, die der Server vorgibt, und welche er anbietet — aus
    // /hello. Gilt vor der Anmeldung; danach die Sprache des Profils.
    var serverSprache: String? = null
        private set
    var serverSprachen: List<Sprache> = emptyList()
        private set

    private fun noteLicense(hello: Hello) {
        license = hello.license ?: ""
        sourceUrl = sourceLink(hello.sourceUrl)
        serverSprache = hello.sprache
        serverSprachen = hello.sprachen
    }

    /**
     * Welche Sprache die Oberfläche sprechen soll: die des Profils, solange
     * jemand angemeldet ist, sonst die Vorgabe des Servers. Ein Server, der
     * „sprache" in /me noch nicht kennt, bewahrt die Wahl trotzdem in den
     * Prefs auf — die gilt dann. Null heisst: keine Angabe, es bleibt, was galt.
     */
    val sprache: String?
        get() {
            val m = me ?: return serverSprache
            return m.sprache ?: m.prefs[Sprachen.PREF]?.trim()?.takeIf { it.isNotEmpty() } ?: serverSprache
        }

    val me: Me? get() = (stage as? Stage.SignedIn)?.me

    /** Der Besitzer des Servers — für das, was nur er darf: Gruppen, Updates. */
    val isOwner: Boolean get() = me?.account?.isOwner == true

    /** Vollzugriff am Server (Besitzer oder über eine Gruppe). Auf Android
     *  ändert das nur Hinweistexte — eine Verwaltung gibt es hier nicht. */
    val isAdmin: Boolean get() = me?.account?.isAdmin == true

    /**
     * Die Liste der gespeicherten Server — wenn die Ablage eine ist. Mit
     * einem gewöhnlichen [TokenStore] kennt der Controller genau einen
     * Server, wie früher.
     */
    val serverliste: Serverliste? = store as? Serverliste

    /** Alle gespeicherten Server, in der Reihenfolge, in der sie dazukamen. */
    val server: List<GespeicherterServer> get() = serverliste?.server ?: emptyList()

    /** Der Server, mit dem die App gerade spricht. */
    val aktiverServer: GespeicherterServer? get() = serverliste?.aktiv

    /** Beim Start: Server erreichbar? Eingerichtet? Token noch gültig? */
    fun start() {
        val url = store.serverUrl
        if (url.isNullOrEmpty()) {
            // Gibt es gespeicherte Server, aber keiner ist gewählt (etwa
            // nachdem der gewählte entfernt wurde), zeigt die App die Liste.
            stage = if (server.isNotEmpty()) Stage.ChoosingServer(server) else Stage.NeedsServer()
            return
        }
        anDenAktiven()
        connect()
        // Ein gespeicherter Server, der gerade nicht antwortet: die Liste
        // zeigen, mit der Meldung — von dort geht es zu jedem anderen.
        val s = stage
        if (s is Stage.NeedsServer && serverliste != null) {
            stage = Stage.ChoosingServer(server, aktiverServer?.id, s.message)
        }
    }

    /** Den Client auf den gewählten Server einstellen — mit dessen Token. */
    private fun anDenAktiven() {
        client.baseUrl = store.serverUrl ?: ""
        client.token = store.token
        client.pinnen(store.fingerabdruck)
    }

    // ---------------------------------------------------------- Mehrere Server

    /**
     * Zur Liste der Server — aus den Einstellungen („Server wechseln") oder
     * aus der Profilwahl. Abgemeldet wird dabei nicht: wer zurückkommt,
     * ist auf jedem Server noch so angemeldet, wie er ihn verlassen hat.
     */
    fun serverWahl(message: String? = null) {
        lastError = null
        stage = if (server.isEmpty()) Stage.NeedsServer(message)
                else Stage.ChoosingServer(server, aktiverServer?.id, message)
    }

    /** „Server hinzufügen": die bekannte Eingabe der Adresse. */
    fun serverHinzufuegen() {
        lastError = null
        stage = Stage.NeedsServer()
    }

    /**
     * Mit einem gespeicherten Server verbinden. Ist das Gerät dort noch
     * angemeldet, geht es gleich in die Mediathek, sonst zur Profilwahl.
     * Antwortet er nicht, bleibt die Liste stehen — mit der Meldung.
     */
    fun verbinden(id: String) {
        val liste = serverliste ?: return
        lastError = null
        if (liste.waehlen(id) == null) { serverWahl(); return }
        anDenAktiven()
        connect()
        val s = stage
        if (s is Stage.NeedsServer) stage = Stage.ChoosingServer(server, id, s.message)
    }

    /** Einen Server umbenennen; leer heisst: wieder der Name vom Server. */
    fun serverUmbenennen(id: String, name: String) {
        serverliste?.umbenennen(id, name)
        val s = stage
        if (s is Stage.ChoosingServer) stage = s.copy(server = server)
    }

    /**
     * Einen Server vergessen, samt Token. War es der gewählte, ist das
     * Gerät danach bei keinem angemeldet — die App zeigt die übrigen oder,
     * ist keiner mehr da, die Eingabe der Adresse.
     */
    fun serverEntfernen(id: String) {
        val liste = serverliste ?: return
        val warAktiv = liste.aktiv?.id == id
        // Erst beim Server abmelden, dann vergessen — sonst bliebe ein
        // gültiges Token zurück, das niemand mehr kennt. Antwortet der
        // Server nicht, wird trotzdem vergessen.
        val eintrag = liste.finde(id)
        val tok = liste.tokenVon(id)
        if (eintrag != null && tok != null) {
            val dort = if (warAktiv) client else ApiClient(eintrag.adresse, tok).also {
                it.pinnen(eintrag.fingerabdruck)
            }
            abmeldenBeimServer(dort)
        }
        liste.entfernen(id)
        if (warAktiv) {
            client.signOut()
            }
        val s = stage
        if (warAktiv || s is Stage.ChoosingServer) serverWahl()
    }

    /**
     * Eine Adresse eintragen und sofort prüfen.
     *
     * [fingerabdruck] gehört dazu, wenn der Server von unterwegs über
     * https erreicht wird und sein Zertifikat selbst ausgestellt hat. Im
     * Heimnetz bleibt er leer.
     */
    @JvmOverloads
    fun useServer(address: String, fingerabdruck: String? = null) {
        val url = ApiClient.normalizeAddress(address)
        if (url == null) {
            stage = Stage.NeedsServer(Texte.t(Tx.ADRESSE_UNGUELTIG))
            return
        }
        val liste = serverliste
        // Mit einer Serverliste gehört der Fingerabdruck zu *diesem* Server:
        // der des bisher gewählten darf nicht auf einen neuen übergehen.
        val bekannt = liste?.findeAdresse(url)
        val pin = if (liste != null) fingerabdruck ?: bekannt?.fingerabdruck
                  else fingerabdruck ?: store.fingerabdruck
        client.baseUrl = url
        client.pinnen(pin)
        // Das Token eines anderen Servers hat bei diesem nichts verloren —
        // auch nicht in der ersten Frage nach /hello.
        client.token = null
        try {
            val hello = client.hello()
            if (liste != null) {
                liste.merken(url, pin, Serverliste.standardName(url, hello))
                // Kennt die App diesen Server schon, gilt sein Token weiter.
                client.token = store.token
                    } else {
                store.serverUrl = url
                store.fingerabdruck = pin
            }
            serverVersion = hello.version
            hasFfmpeg = hello.ffmpeg
            noteLicense(hello)
            if (!hello.setUp) { stage = Stage.NeedsSetup; return }
            if (liste != null && client.isSignedIn) {
                if (mitGespeichertemToken()) return
            }
            stage = profilwahl()
        } catch (e: LumenException) {
            stage = Stage.NeedsServer(e.message)
        }
    }

    /**
     * Mit dem gespeicherten Token hinein. Wahr heisst: fertig — angemeldet
     * oder, bei einem Fehler, der nichts über die Anmeldung sagt, zurück
     * zur Adresse mit der Meldung. Falsch heisst: weiter zur Profilwahl.
     *
     * Vergessen wird das Token nur, wenn der Server es zurückweist (401).
     * Eine Zeitüberschreitung, ein überlasteter Server (503, 429) oder eine
     * Antwort, die diese App nicht lesen kann, sagt nichts über die
     * Anmeldung — wer dann neu anmelden müsste, hätte sie grundlos verloren.
     * Wie Session.nachHallo auf Apple: „Nochmal versuchen" kommt mit dem
     * Token wieder hinein.
     */
    private fun mitGespeichertemToken(): Boolean =
        try {
            angemeldet(client.me())
            true
        } catch (e: LumenException.SignedOut) {
            // Token abgelaufen oder zurückgezogen.
            store.token = null
            client.signOut()
            false
        } catch (e: LumenException) {
            stage = Stage.NeedsServer(e.message)
            true
        }

    private fun connect() {
        try {
            val hello = client.hello()
            serverVersion = hello.version
            hasFfmpeg = hello.ffmpeg
            noteLicense(hello)
            aktiverServer?.let { serverliste?.nameVomServer(it.id, Serverliste.standardName(it.adresse, hello)) }

            if (!hello.setUp) { stage = Stage.NeedsSetup; return }

            if (client.isSignedIn && mitGespeichertemToken()) return
            stage = profilwahl()
        } catch (e: LumenException) {
            stage = Stage.NeedsServer(e.message)
        }
    }

    /**
     * Angemeldet: erst den Medienschlüssel holen, dann in die Mediathek. In
     * dieser Reihenfolge, damit schon der erste Bildschirm Adressen mit
     * Schlüssel baut — sonst liefe jedes Poster erst in eine 401.
     */
    private fun angemeldet(me: Me) {
        client.medienSchluesselSichern()
        stage = Stage.SignedIn(me)
    }

    /**
     * Die Profilwahl — oder, von unterwegs, die Anmeldung mit Name und PIN.
     * Andere Fehler gehen weiter an den Aufrufer.
     */
    private fun profilwahl(): Stage.ChoosingProfile =
        try {
            Stage.ChoosingProfile(client.profiles())
        } catch (e: LumenException.Unterwegs) {
            Stage.ChoosingProfile(emptyList(), unterwegs = true)
        }

    /**
     * Von unterwegs anmelden: Profilname und PIN oder Passwort. Die Meldung
     * des Servers — „das stimmt nicht", oder nach zu vielen Versuchen, wie
     * lange zu warten ist (429) — steht danach unverändert in [lastError].
     */
    fun unlockByName(name: String, secret: String, deviceName: String = DeviceProfile.current.name) {
        lastError = null
        if (name.isBlank()) { lastError = Texte.t(Tx.WIE_HEISST_PROFIL); return }
        if (secret.isEmpty()) { lastError = Texte.t(Tx.UNTERWEGS_NUR_MIT_GEHEIMNIS); return }
        try {
            client.unlockByName(name, secret, deviceName)
            store.token = client.token
            angemeldet(client.me())
        } catch (e: LumenException) {
            lastError = e.message
        }
    }

    fun chooseProfile(card: ProfileCard) {
        if (card.needsCode) stage = Stage.Unlocking(card) else unlock(card, "")
    }

    fun unlock(card: ProfileCard, secret: String, deviceName: String = DeviceProfile.current.name) {
        lastError = null
        try {
            client.unlock(card.id, secret, deviceName)
            store.token = client.token
            angemeldet(client.me())
        } catch (e: LumenException) {
            lastError = e.message
        }
    }

    fun setUpOwner(name: String, password: String,
                   deviceName: String = DeviceProfile.current.name) {
        lastError = null
        try {
            client.setUpOwner(name, password, deviceName)
            store.token = client.token
            angemeldet(client.me())
        } catch (e: LumenException) {
            lastError = e.message
        }
    }

    fun redeemInvite(code: String, name: String, pin: String) =
        redeemInvite(code, name, if (pin.isEmpty()) "none" else "pin", pin)

    /**
     * Eine Einladung einlösen und gleich angemeldet sein. [lockKind] ist
     * password, pin oder none — wie im Browser. Was offensichtlich nicht
     * geht (zu kurzer PIN, leerer Name), meldet [Einladung.pruefen], bevor
     * der Code beim Server verbraucht wird.
     */
    fun redeemInvite(code: String, name: String, lockKind: String, secret: String) {
        lastError = Einladung.pruefen(code, name, lockKind, secret)
        if (lastError != null) return
        try {
            client.redeemInvite(Einladung.codeSaubern(code), name.trim(), lockKind,
                                if (lockKind == "none") "" else secret)
            store.token = client.token
            angemeldet(client.me())
        } catch (e: LumenException) {
            lastError = e.message
        }
    }

    /**
     * Zur Profilwahl, ohne sich abzumelden — etwa von der Eingabe eines
     * Einladungscodes zurück. Steht der Server nicht, bleibt alles, wie es ist.
     */
    fun zurProfilwahl() {
        if (stage is Stage.SignedIn) return
        try { stage = profilwahl() } catch (e: LumenException) { lastError = e.message }
    }

    // ---------------------------------------------------------- Mit Code

    /**
     * Einen Code holen, den jemand an einem anderen Gerät freigibt. Null,
     * wenn es nicht ging — die Meldung steht dann in [lastError].
     */
    fun codeStarten(deviceName: String = DeviceProfile.current.name): AnmeldeCode? {
        lastError = null
        return try {
            client.codeStarten(deviceName)
        } catch (e: LumenException) {
            lastError = e.message
            null
        }
    }

    /**
     * Einmal nachfragen. Ist der Code freigegeben, wird das Token gemerkt
     * und die App ist angemeldet — wie nach dem Entsperren eines Profils.
     */
    fun codeNachfragen(geheim: String): CodeStand {
        return try {
            client.codeAbfragen(geheim) ?: return CodeStand.WARTET
            store.token = client.token
            lastError = null
            angemeldet(client.me())
            CodeStand.ANGEMELDET
        } catch (e: LumenException.Server) {
            if (e.status == 410) CodeStand.ABGELAUFEN else { lastError = e.message; CodeStand.FEHLER }
        } catch (e: LumenException) {
            lastError = e.message
            CodeStand.FEHLER
        }
    }

    /**
     * Einen Code freigeben, der an einem anderen Gerät steht — angemeldet.
     * Zurück kommt der Name des Geräts, sonst null und die Meldung in
     * [lastError].
     */
    fun codeFreigeben(code: String): String? {
        lastError = null
        if (!Anmeldecode.vollstaendig(code)) {
            lastError = Texte.t(Tx.CODE_SECHS_ZIFFERN)
            return null
        }
        return try {
            client.codeFreigeben(code)
        } catch (e: LumenException) {
            lastError = e.message
            null
        }
    }

    /** Dieses Gerät beim Server abmelden; Fehler zählen nicht. */
    private fun abmeldenBeimServer(c: ApiClient) {
        if (!c.isSignedIn) return
        try { c.geraetAbmelden() } catch (e: LumenException) { /* nicht erreichbar oder schon abgemeldet */ }
    }

    fun signOut() {
        abmeldenBeimServer(client)
        store.token = null
        client.signOut()
        stage = try {
            profilwahl()
        } catch (e: LumenException) {
            Stage.NeedsServer(e.message)
        }
    }

    /** Nach einer Änderung an den eigenen Einstellungen neu laden. */
    fun refreshMe() {
        if (stage !is Stage.SignedIn) return
        try { angemeldet(client.me()) } catch (e: LumenException) { /* später */ }
    }

    /**
     * Ab wann ein Film als gesehen gilt. Wer den Abspann überspringt, hat
     * ihn trotzdem gesehen.
     */
    companion object { const val FINISHED_AT = 0.92 }
}

/** „1:24:07" oder „7:12" */
fun clock(seconds: Double): String {
    if (seconds.isNaN() || seconds.isInfinite() || seconds < 0) return "0:00"
    val total = seconds.toInt()
    val h = total / 3600
    val m = (total / 60) % 60
    val s = total % 60
    return if (h > 0) "%d:%02d:%02d".format(h, m, s) else "%d:%02d".format(m, s)
}

/**
 * Die Fundstelle des Quelltexts, aber nur wenn sie eine Web-Adresse ist.
 *
 * Der Server sagt der App, wo ihr Quelltext liegt — das verlangt §13 der
 * AGPL. Nur steht da, was der Betreiber einträgt, und das ist nicht
 * zwangsläufig eine Adresse. Alles andere gibt `null`, dann zeigen die
 * Einstellungen die Zeile gar nicht erst an, statt einen Knopf anzubieten,
 * der etwas Unerwartetes öffnet.
 */
fun sourceLink(raw: String?): String? {
    val t = raw?.trim() ?: return null
    if (!t.startsWith("https://", true) && !t.startsWith("http://", true)) return null
    val host = t.substringAfter("//").substringBefore('/').substringBefore('?')
    return if (host.isEmpty()) null else t
}
