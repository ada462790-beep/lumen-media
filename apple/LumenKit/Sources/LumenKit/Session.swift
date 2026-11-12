// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

import Foundation
import Observation

/// Der Zustand der App: welcher Server, wer angemeldet ist, was zu sehen ist.
///
/// Eine einzige Stelle, an der das steht — die Ansichten lesen daraus und
/// rufen die Methoden auf, mehr nicht.
@MainActor
@Observable
public final class Session {

    public enum Stage: Equatable {
        case startingUp
        case needsServer(String?)       // noch kein Server, ggf. mit Fehlermeldung
        case choosingServer             // mehrere gespeichert: welcher?
        case needsSetup                 // Server läuft, aber niemand hat ihn eingerichtet
        case choosingProfile([ProfileCard])
        case unlocking(ProfileCard)
        /// Von unterwegs: keine Profilauswahl, sondern Profilname und PIN
        /// oder Passwort — oder ein Code von einem angemeldeten Gerät.
        case unterwegs
        case signedIn(Me)
    }

    public private(set) var stage: Stage = .startingUp
    public private(set) var serverVersion: String = ""
    public private(set) var hasFFmpeg = true
    /// Lizenz und Fundstelle des Quelltexts, wie der Server sie nennt. Die
    /// AGPL verlangt, dass wer den Dienst benutzt, daran kommt — die
    /// Einstellungen zeigen beides unter „Über Lumen".
    public private(set) var license: String = ""
    public private(set) var sourceURL: URL?
    public var lastError: String?

    // MARK: Sprache

    /// Die Sprache, in der die App gerade spricht („de-CH" … „en-GB") —
    /// die des Profils, vor der Anmeldung die des Servers. Die Ansichten
    /// lesen sie für `.environment(\.locale, …)` und bauen sich neu auf,
    /// wenn sie wechselt. Siehe `Sprache`.
    public private(set) var sprache: String = Sprache.aktuell
    /// Was der Server vorgibt (GET /hello → `sprache`); nil bei einem
    /// älteren Server, der keine Sprache kennt.
    public private(set) var serverSprache: String?
    /// Die Sprachen, die der Server anbietet — sonst die fünf der App.
    public private(set) var sprachen: [SprachAngebot] = Sprache.alle

    /// Die Locale zur Sprache — für `.environment(\.locale, …)`.
    public var locale: Locale { Locale(identifier: sprache) }

    /// Ein Einladungscode, der über einen Link hereinkam, bis die Ansicht
    /// ihn abholt.
    public var pendingInvite: String?

    public private(set) var client: APIClient
    private let store: TokenStore

    /// Die gespeicherten Server, wie der Bildschirm „Server" sie zeigt.
    public private(set) var server: [ServerEintrag] = []
    /// Mit welchem davon die App gerade redet.
    public private(set) var aktiverServer: ServerEintrag?
    /// Für welche Server dieses Gerät ein Token hat — die Liste zeigt dort
    /// „angemeldet". Wer damit angemeldet ist, steht hier nicht.
    public private(set) var angemeldeteServer: Set<String> = []
    /// Die Adresse, die beim Scheitern schon im Feld stehen soll — damit
    /// „nicht erreichbar" nicht heisst, alles neu zu tippen.
    public private(set) var adressVorschlag: String?

    public init(store: TokenStore = .shared) {
        self.store = store
        let liste = store.server
        let letzter = store.zuletztBenutzt.flatMap { id in liste.first { $0.id == id } }
        let url = letzter?.adresse ?? URL(string: "http://lumen.local:8097")!
        // Ohne Token: das liest `start` → `verbinden` aus dem Schlüsselbund,
        // abseits des Hauptfadens. Ein gesperrter Schlüsselbund soll den
        // ersten Bildschirm nicht aufhalten.
        let client = APIClient(baseURL: url)
        self.client = client
        self.server = liste
        self.aktiverServer = letzter
        Task { [weak self] in
            await client.setAbmeldeHandler { [weak self] in
                Task { @MainActor in await self?.anmeldungAbgelaufen() }
            }
        }
    }

    public var me: Me? {
        if case .signedIn(let me) = stage { return me }
        return nil
    }

    /// Besitzerrechte — der Besitzer selbst oder jemand mit Vollzugriff —
    /// für das, was der Server nur ihnen erlaubt (Gruppen, Updates,
    /// Sicherungen). Die Verwaltung an sich fragt `isAdmin`.
    public var isOwner: Bool { me?.account.isOwner == true || me?.account.vollzugriff == true }

    /// Besitzer oder Mitverwalter: wer die Verwaltung sieht.
    public var isAdmin: Bool { me?.account.darfVerwalten == true }

    // MARK: - Verwaltungssperre

    /// Die Sperre, die gerade im Weg steht — pin, password oder totp.
    /// Solange sie gesetzt ist, zeigen die Apps den Entsperr-Dialog.
    /// nil heisst: nichts zu tun.
    public private(set) var adminSperre: String?

    /// Wer auf das Entsperren wartet, um seine Anfrage zu wiederholen.
    @ObservationIgnored private var entsperrWarter: [CheckedContinuation<Bool, Never>] = []

    /// Eine Verwaltungs-Anfrage, die bei gesperrter Verwaltung (423) den
    /// Entsperr-Dialog aufruft und danach von selbst noch einmal läuft.
    ///
    ///     let libs = try await session.verwaltet { try await session.client.adminLibraries() }
    ///
    /// Bricht die Person den Dialog ab, kommt der Fehler `locked` zurück,
    /// und die Ansicht zeigt ihn wie jeden anderen.
    ///
    /// Dasselbe für die Bestätigung (Rechte ändern): sagt der Server
    /// „bestaetigen", fragt der Bestätigungs-Dialog nach PIN, Passwort oder
    /// Code, und die Anfrage läuft mit dem Code noch einmal — bei „stimmt
    /// nicht" so oft, bis es stimmt oder die Person abbricht.
    public func verwaltet<T>(_ arbeit: () async throws -> T) async throws -> T {
        var code: String?
        var entsperrt = false
        // Der Bestätigungs-Dialog bleibt offen, solange geprüft wird, und
        // schliesst sich erst hier: nach Erfolg oder einem anderen Fehler.
        // Bei „stimmt nicht" bleibt er stehen und fragt noch einmal.
        defer { if code != nil { bestaetigungFertig() } }
        while true {
            do {
                guard let code else { return try await arbeit() }
                return try await APIClient.bestaetigung.withValue(code) { try await arbeit() }
            } catch LumenError.locked(let art) {
                // Nur einmal entsperren: wer danach wieder „gesperrt" hört,
                // soll den Fehler sehen und nicht im Kreis laufen.
                guard !entsperrt, await entsperrenAbwarten(art) else { throw LumenError.locked(art) }
                entsperrt = true
            } catch LumenError.bestaetigen(let falsch) {
                guard let neu = await bestaetigungAbwarten(falsch: falsch) else {
                    code = nil
                    throw LumenError.server(String(localized: "Abgebrochen.", bundle: .lumenKit, locale: .lumen), 0)
                }
                code = neu
            }
        }
    }

    // MARK: - Bestätigung

    /// Ob gerade nach der Bestätigung gefragt wird. Dann zeigt die Mac-App
    /// den Dialog „Rechte ändern".
    public private(set) var bestaetigungOffen = false
    /// Ob der letzte Code nicht stimmte — der Dialog sagt es dann.
    public private(set) var bestaetigungFalsch = false
    /// Ob der eingegebene Code gerade beim Server geprüft wird. Der Dialog
    /// bleibt dabei offen und zeigt, dass es arbeitet.
    public private(set) var bestaetigungPrueft = false
    /// Zählt jeden abgelehnten Versuch — der Dialog leert daran sein Feld.
    public private(set) var bestaetigungVersuch = 0

    @ObservationIgnored private var bestaetigungWarter: [CheckedContinuation<String?, Never>] = []

    private func bestaetigungAbwarten(falsch: Bool) async -> String? {
        bestaetigungFalsch = falsch
        bestaetigungPrueft = false
        if falsch { bestaetigungVersuch &+= 1 }
        bestaetigungOffen = true
        return await withCheckedContinuation { fortsetzung in
            bestaetigungWarter.append(fortsetzung)
        }
    }

    /// Der Code aus dem Dialog: die wartende Anfrage läuft damit weiter.
    /// Der Dialog bleibt offen, bis der Server geantwortet hat.
    public func bestaetigen(code: String) {
        guard !bestaetigungWarter.isEmpty else {
            // Niemand wartet mehr (etwa nach einem Serverwechsel): dann
            // gibt es nichts zu bestätigen, und der Dialog geht zu.
            bestaetigungFertig()
            return
        }
        bestaetigungPrueft = true
        weckeBestaetigung(code)
    }

    /// Abgebrochen: die wartende Anfrage scheitert mit „Abgebrochen.".
    public func bestaetigenAbbrechen() {
        bestaetigungFertig()
        weckeBestaetigung(nil)
    }

    /// Der Dialog geht zu — nach Erfolg, Abbruch oder einem anderen Fehler.
    private func bestaetigungFertig() {
        bestaetigungOffen = false
        bestaetigungPrueft = false
        bestaetigungFalsch = false
    }

    private func weckeBestaetigung(_ code: String?) {
        let warter = bestaetigungWarter
        bestaetigungWarter = []
        for w in warter { w.resume(returning: code) }
    }

    /// Den Dialog aufrufen und warten, bis entsperrt (true) oder
    /// abgebrochen (false) wurde. Mehrere Anfragen zugleich warten auf
    /// denselben Dialog.
    private func entsperrenAbwarten(_ art: String) async -> Bool {
        adminSperre = art
        return await withCheckedContinuation { fortsetzung in
            entsperrWarter.append(fortsetzung)
        }
    }

    /// Der Code aus dem Dialog. nil heisst: entsperrt, die wartenden
    /// Anfragen laufen weiter. Sonst die Meldung des Servers — „das stimmt
    /// nicht" (403) oder „zu viele Versuche" (429) —, und der Dialog bleibt.
    public func adminEntsperren(code: String) async -> String? {
        do {
            _ = try await client.adminUnlock(code: code)
        } catch {
            return Self.text(error)
        }
        adminSperre = nil
        wecke(entsperrt: true)
        return nil
    }

    /// Der Dialog wurde weggewischt: die wartenden Anfragen scheitern.
    public func adminEntsperrenAbbrechen() {
        adminSperre = nil
        wecke(entsperrt: false)
    }

    /// Die Verwaltung wieder zusperren. Fehlschläge sind egal: das Token
    /// ist danach hier auf jeden Fall weg.
    public func adminSperren() async {
        try? await client.adminRelock()
        await client.setAdminToken(nil)
    }

    private func wecke(entsperrt: Bool) {
        let warter = entsperrWarter
        entsperrWarter = []
        for w in warter { w.resume(returning: entsperrt) }
    }

    // MARK: - Start

    /// Beim Start: welcher Server? Erreichbar? Eingerichtet? Token noch gültig?
    ///
    /// Der zuletzt benutzte Server kommt ohne Frage. Gibt es keinen
    /// solchen und mehr als einen gespeicherten, zeigt die App die Liste.
    public func start() async {
        listeAuffrischen()
        if let id = store.zuletztBenutzt, let letzter = server.first(where: { $0.id == id }) {
            await verbinden(letzter)
            return
        }
        switch server.count {
        case 0: stage = .needsServer(nil)
        case 1: await verbinden(server[0])
        default: stage = .choosingServer
        }
    }

    /// Einen Server eintragen. Erreichbarkeit wird sofort geprüft — eine
    /// Adresse, die erst beim nächsten Schritt scheitert, hilft niemandem.
    ///
    /// `fingerabdruck` gehört dazu, wenn der Server von unterwegs über
    /// https erreicht wird und sein Zertifikat selbst ausgestellt hat. Im
    /// Heimnetz bleibt er leer.
    ///
    /// Ist die Adresse schon gespeichert, gilt dieser Eintrag — samt
    /// Fingerabdruck und Token. Sonst kommt ein neuer in die Liste.
    public func useServer(address: String, fingerabdruck: String? = nil) async {
        guard let url = Self.normalize(address) else {
            adressVorschlag = address
            stage = .needsServer(String(localized: "Das sieht nicht nach einer Adresse aus.", bundle: .lumenKit, locale: .lumen))
            return
        }
        let vorhanden = server.first { $0.adresse == url }
        let abdruck = Pinnen.saubern(fingerabdruck) ?? vorhanden?.fingerabdruck
        stage = .startingUp
        // Das Token des bisherigen Servers geht keinen anderen etwas an.
        await client.setToken(nil)
        await client.setBaseURL(url)
        await client.pinnen(abdruck)
        do {
            let hello = try await client.hello()
            var eintrag = vorhanden ?? ServerEintrag(
                name: ServerEintrag.vorschlag(fuer: url, name: hello.name), adresse: url)
            eintrag.fingerabdruck = abdruck
            store.speichern(eintrag)
            aktivieren(eintrag)
            listeAuffrischen()
            adressVorschlag = nil
            await client.setToken(await tokenLesen(eintrag.id))
            await nachHallo(hello)
        } catch {
            // Das Getippte bleibt im Feld stehen.
            adressVorschlag = address
            stage = .needsServer(Self.text(error))
        }
    }

    // MARK: - Mehrere Server

    /// Mit einem gespeicherten Server verbinden — mit seinem Token, wenn
    /// dieses Gerät dort schon angemeldet ist.
    public func serverWaehlen(_ eintrag: ServerEintrag) async {
        lastError = nil
        stage = .startingUp
        await verbinden(eintrag)
    }

    /// Zur Liste der Server. Abgemeldet wird dabei nicht: das Token bleibt,
    /// und wer zurückwechselt, ist gleich wieder drin.
    public func serverWechseln() {
        if adminSperre != nil { adminEntsperrenAbbrechen() }
        if bestaetigungOffen || !bestaetigungWarter.isEmpty { bestaetigenAbbrechen() }
        listeAuffrischen()
        lastError = nil
        stage = server.isEmpty ? .needsServer(nil) : .choosingServer
    }

    /// Die Adress-Eingabe für einen weiteren Server.
    public func serverHinzufuegen() {
        lastError = nil
        adressVorschlag = nil
        stage = .needsServer(nil)
    }

    /// Von der Adress-Eingabe zurück zur Liste, ohne etwas einzutragen.
    public func zurueckZurServerliste() {
        listeAuffrischen()
        guard !server.isEmpty else { return }
        lastError = nil
        stage = .choosingServer
    }

    public func serverUmbenennen(_ eintrag: ServerEintrag, in name: String) {
        store.umbenennen(id: eintrag.id, name: name)
        listeAuffrischen()
    }

    /// Den Server vergessen, samt Token auf diesem Gerät. Vorher meldet
    /// sich das Gerät dort ab (DELETE /me/device) — klappt das nicht, weil
    /// der Server gerade fehlt, wird trotzdem vergessen.
    public func serverEntfernen(_ eintrag: ServerEintrag) async {
        if let token = await tokenLesen(eintrag.id) {
            if aktiverServer?.id == eintrag.id {
                try? await client.diesesGeraetAbmelden()
            } else {
                let fremd = APIClient(baseURL: eintrag.adresse, token: token)
                await fremd.pinnen(eintrag.fingerabdruck)
                try? await fremd.diesesGeraetAbmelden()
            }
        }
        store.entfernen(id: eintrag.id)
        if aktiverServer?.id == eintrag.id {
            aktiverServer = nil
            await client.setToken(nil)
        }
        listeAuffrischen()
        if server.isEmpty {
            stage = .needsServer(nil)
        } else if aktiverServer == nil {
            stage = .choosingServer
        }
    }

    private func verbinden(_ eintrag: ServerEintrag) async {
        // Erst das alte Token weg, dann die Adresse wechseln: keine
        // Anfrage soll mit dem Token des einen Servers beim anderen landen.
        await client.setToken(nil)
        aktivieren(eintrag)
        await client.setBaseURL(eintrag.adresse)
        await client.pinnen(eintrag.fingerabdruck)
        await client.setToken(await tokenLesen(eintrag.id))
        await connect()
    }

    private func aktivieren(_ eintrag: ServerEintrag) {
        aktiverServer = eintrag
        store.zuletztBenutzt = eintrag.id
    }

    private func listeAuffrischen() {
        server = store.server
        if let id = aktiverServer?.id {
            aktiverServer = server.first { $0.id == id }
        }
        // Welche Server ein Token haben, steht im Schlüsselbund — und der
        // antwortet nicht immer sofort (gesperrt, iCloud-Abgleich). Darum
        // abseits des Hauptfadens; die Liste zeigt „angemeldet" gleich danach.
        angemeldetStand &+= 1
        let stand = angemeldetStand
        let ids = server.map(\.id)
        let store = self.store
        Task { [weak self] in
            let mitToken = await Task.detached {
                Set(ids.filter { store.token(fuer: $0) != nil })
            }.value
            guard let self, self.angemeldetStand == stand else { return }
            self.angemeldeteServer = mitToken
        }
    }

    /// Zählt jedes Nachsehen und jede Änderung: eine Antwort, die eine
    /// spätere Änderung überholt hat, gilt nicht mehr.
    @ObservationIgnored private var angemeldetStand = 0

    /// Das Token eines Servers aus dem Schlüsselbund — abseits des
    /// Hauptfadens.
    private func tokenLesen(_ id: String) async -> String? {
        let store = self.store
        return await Task.detached { store.token(fuer: id) }.value
    }

    /// Das Token des gerade aktiven Servers merken — oder vergessen.
    private func speichereToken(_ token: String?) {
        guard let id = aktiverServer?.id else { return }
        store.setzeToken(token, fuer: id)
        angemeldetStand &+= 1
        if token == nil { angemeldeteServer.remove(id) } else { angemeldeteServer.insert(id) }
    }

    private func noteLicense(_ hello: Hello) {
        license = hello.license ?? ""
        sourceURL = Self.sourceLink(hello.sourceUrl)
    }

    /// Die Fundstelle des Quelltexts, aber nur wenn sie eine Web-Adresse ist.
    ///
    /// Der Server sagt der App, wo ihr Quelltext liegt — das verlangt §13 der
    /// AGPL. Nur steht dort, was der Betreiber einträgt, und das ist nicht
    /// zwangsläufig eine Adresse. Alles andere gibt nil, dann zeigen die
    /// Einstellungen die Zeile gar nicht erst an, statt einen Knopf
    /// anzubieten, der etwas Unerwartetes öffnet.
    nonisolated public static func sourceLink(_ raw: String?) -> URL? {
        guard let raw = raw?.trimmingCharacters(in: .whitespacesAndNewlines),
              !raw.isEmpty,
              let url = URL(string: raw),
              let scheme = url.scheme?.lowercased(),
              scheme == "https" || scheme == "http",
              let host = url.host, !host.isEmpty
        else { return nil }
        return url
    }

    private func connect() async {
        do {
            let hello = try await client.hello()
            await nachHallo(hello)
        } catch {
            adressVorschlag = aktiverServer?.adresseKurz
            stage = .needsServer(Self.text(error))
        }
    }

    /// Der Server hat geantwortet: einrichten, Profil wählen oder gleich hinein.
    private func nachHallo(_ hello: Hello) async {
        serverVersion = hello.version
        serverSprache = Sprache.bekannt(hello.sprache)
        if let angebot = hello.sprachen?.filter({ Sprache.bekannt($0.id) != nil }), !angebot.isEmpty {
            sprachen = angebot
        } else {
            sprachen = Sprache.alle
        }
        spracheAnwenden()
        hasFFmpeg = hello.ffmpeg
        noteLicense(hello)
        // Hat der Besitzer den Server benannt, steht das in der Liste —
        // ausser der Nutzer hat ihn hier selbst umbenannt.
        if let id = aktiverServer?.id, store.nameVomServer(id: id, name: hello.name) {
            listeAuffrischen()
        }

        guard hello.setUp else { stage = .needsSetup; return }

        if await client.token != nil {
            do {
                try await hinein()
                return
            } catch LumenError.signedOut {
                // Token abgelaufen oder zurückgezogen — zurück zur Auswahl.
                await signOutLocally()
            } catch {
                // Alles andere — Server überlastet (5xx, 429), eine Antwort,
                // die diese App nicht lesen kann — sagt nichts über die
                // Anmeldung. Das Token bleibt; „Nochmal versuchen" auf dem
                // Adress-Bildschirm kommt mit ihm wieder hinein.
                adressVorschlag = aktiverServer?.adresseKurz
                stage = .needsServer(Self.text(error))
                return
            }
        }
        await profilwahlZeigen()
    }

    /// Die Profilauswahl — oder, von unterwegs, wo der Server keine Liste
    /// herausgibt, die Anmeldung mit Profilname und PIN.
    private func profilwahlZeigen() async {
        do {
            stage = .choosingProfile(try await client.profiles())
        } catch LumenError.unterwegs {
            stage = .unterwegs
        } catch {
            stage = .needsServer(Self.text(error))
        }
    }

    /// Angemeldet: erst prüfen, ob das Token gilt, dann den Medienschlüssel
    /// holen — vor der Mediathek, damit schon die ersten Bilder eine
    /// Adresse haben. Klappt nur der Schlüssel nicht, geht es trotzdem
    /// hinein; die Bilder fehlen dann, und vor jeder Wiedergabe wird ohnehin
    /// neu geholt.
    private func hinein() async throws {
        let me = try await client.me()
        try? await client.medienSchluesselHolen(erzwingen: true)
        angemeldet(me)
    }

    // MARK: - Anmelden

    public func chooseProfile(_ card: ProfileCard) async {
        if card.needsCode {
            stage = .unlocking(card)
        } else {
            await unlock(card, secret: "")
        }
    }

    public func unlock(_ card: ProfileCard, secret: String) async {
        lastError = nil
        await anmeldenMitSchutz(wer: card.id, secret: secret) { client, geheim, neu, neuArt in
            _ = try await client.unlock(profileId: card.id, secret: geheim,
                                        deviceName: DeviceProfile.current.name,
                                        neu: neu, neuArt: neuArt)
        }
    }

    /// Nach einem Zurücksetzen durch die Verwaltung: Mit dem Einmal-Code
    /// gibt es erst eine Anmeldung, wenn die Person ihren eigenen Schutz
    /// festlegt. Der Server sagt das mit 409 („neue_pin_noetig") und nennt
    /// die Art. Dann fragt das Eingabefeld zweimal — „Neue PIN festlegen",
    /// „Noch einmal" —, damit ein Tippfehler niemanden aussperrt; erst bei
    /// gleichen Eingaben geht die Anmeldung mit dem neuen Schutz noch
    /// einmal. Die Ansicht zeigt `neuerSchutz?.titel` und leert das Feld
    /// nach jedem Schritt.
    public private(set) var neuerSchutz: NeuerSchutz?

    /// Das Festlegen abbrechen — etwa beim Zurück zur Profilauswahl.
    public func neuenSchutzAbbrechen() { neuerSchutz = nil }

    private func anmeldenMitSchutz(
        wer: String, secret: String,
        senden: (APIClient, String, String?, String) async throws -> Void
    ) async {
        let laufend = neuerSchutz?.wer == wer ? neuerSchutz : nil
        if laufend == nil { neuerSchutz = nil }
        if var schritt = laufend, schritt.erste == nil {
            // Erste Eingabe des neuen Schutzes: noch nichts schicken.
            schritt.erste = secret
            neuerSchutz = schritt
            return
        }
        if var schritt = laufend, schritt.erste != secret {
            schritt.erste = nil
            neuerSchutz = schritt
            lastError = String(localized: "Die beiden Eingaben stimmen nicht überein.", bundle: .lumenKit, locale: .lumen)
            return
        }
        do {
            if let schritt = laufend {
                try await senden(client, schritt.code, secret, schritt.art)
            } else {
                try await senden(client, secret, nil, "pin")
            }
            neuerSchutz = nil
            let neu = await client.token
            speichereToken(neu)
            try await hinein()
        } catch {
            let fehler = error as? LumenError
            if case .neuerSchutzNoetig(let art, _)? = fehler {
                // Der Server will den eigenen Schutz — auch nach einem
                // zweiten Versuch mit dem Einmal-Code als neuer PIN.
                neuerSchutz = NeuerSchutz(wer: wer, code: laufend?.code ?? secret, art: art)
            } else if var schritt = laufend, fehler?.status == 400 {
                // Die Wahl passte nicht (zu kurz …): von vorn, mit der
                // Meldung des Servers.
                schritt.erste = nil
                neuerSchutz = schritt
            } else {
                // Abgelaufen, zu viele Versuche, kein Netz: zurück zum
                // gewöhnlichen Feld; die Meldung bleibt stehen.
                neuerSchutz = nil
            }
            lastError = Self.text(error)
        }
    }

    /// Von unterwegs anmelden: mit Profilname und PIN oder Passwort. Was
    /// der Server dazu sagt — falsch, zu viele Versuche („bitte N Minuten
    /// warten"), Profil ohne PIN —, steht danach unverändert in `lastError`.
    public func unlockUnterwegs(name: String, secret: String) async {
        lastError = nil
        let wer = "name:" + name.trimmingCharacters(in: .whitespacesAndNewlines).lowercased()
        await anmeldenMitSchutz(wer: wer, secret: secret) { client, geheim, neu, neuArt in
            _ = try await client.unlock(name: name, secret: geheim,
                                        deviceName: DeviceProfile.current.name,
                                        neu: neu, neuArt: neuArt)
        }
    }

    public func setUpOwner(name: String, password: String) async {
        lastError = nil
        do {
            _ = try await client.setUpOwner(name: name, password: password,
                                            deviceName: DeviceProfile.current.name)
            let neu = await client.token
            speichereToken(neu)
            try await hinein()
        } catch {
            lastError = Self.text(error)
        }
    }

    public func redeemInvite(code: String, name: String, pin: String) async {
        lastError = nil
        do {
            _ = try await client.redeemInvite(
                code: code, name: name,
                lockKind: pin.isEmpty ? "none" : "pin", secret: pin)
            let neu = await client.token
            speichereToken(neu)
            try await hinein()
        } catch {
            lastError = Self.text(error)
        }
    }

    /// Abmelden: erst beim Server (dieses Token gilt danach nicht mehr),
    /// dann hier. Ein Fehler dort hält das Abmelden hier nicht auf.
    public func signOut() async {
        if adminSperre != nil { adminEntsperrenAbbrechen() }
        if bestaetigungOffen || !bestaetigungWarter.isEmpty { bestaetigenAbbrechen() }
        if await client.token != nil {
            try? await client.diesesGeraetAbmelden()
        }
        await signOutLocally()
        await profilwahlZeigen()
    }

    private func signOutLocally() async {
        neuerSchutz = nil
        speichereToken(nil)
        await client.setToken(nil)
        // Ohne Profil gilt wieder, was der Server vorgibt.
        spracheAnwenden(mitProfil: false)
    }

    /// Angemeldet, mit frischem Stand des Profils — und dessen Sprache.
    private func angemeldet(_ me: Me) {
        stage = .signedIn(me)
        spracheAnwenden()
    }

    /// Die wirksame Sprache neu bestimmen: Profil, sonst Server, sonst
    /// Gerät. Ändert sie sich, merkt `Sprache` sie sich für den nächsten
    /// Start, und die Ansichten bauen sich in der neuen Sprache auf.
    private func spracheAnwenden(mitProfil: Bool = true) {
        // Ein Profil ohne eigene Wahl hat in /me die Vorgabe des Servers;
        // ein älterer Server schickt nur die Prefs.
        let profil = mitProfil ? me.flatMap { $0.sprache ?? $0.prefs["sprache"] } : nil
        let neu = Sprache.wirksam(profil: profil, server: serverSprache)
        // Nur merken, was Profil oder Server sagen. Die Sprache des Geräts
        // als Notlösung festzuhalten hiesse, einen späteren Wechsel am
        // Gerät zu überhören.
        Sprache.setzen(neu, merken: Sprache.bekannt(profil) != nil || serverSprache != nil)
        if sprache != neu { sprache = neu }
    }

    /// Die eigene Sprache wählen: eine der fünf, oder nil für „wie der
    /// Server". Gibt die Meldung des Servers zurück, wenn es nicht klappt.
    @discardableResult
    public func spracheWaehlen(_ id: String?) async -> String? {
        await prefsSetzen(["sprache": id.flatMap(Sprache.bekannt) ?? ""])
    }

    /// Die eigene Wahl des Profils („" = wie der Server), wie sie in den
    /// Prefs steht.
    public var eigeneSprache: String? {
        Sprache.bekannt(me?.prefs["sprache"])
    }

    /// Die Sprache des Servers setzen (nur Mac, Besitzer oder Vollzugriff).
    /// Danach gilt sie für alle Profile ohne eigene Wahl — auch für dieses,
    /// darum wird /me neu geholt.
    public func serverSpracheSetzen(_ id: String) async throws {
        guard let id = Sprache.bekannt(id) else { return }
        try await verwaltet { try await client.adminSpracheSetzen(id) }
        serverSprache = id
        await refreshMe()
        spracheAnwenden()
    }

    /// Der Server hat das Token mitten in der Sitzung zurückgewiesen —
    /// etwa nach „Überall abmelden" auf einem anderen Gerät. Offene Dialoge
    /// gehen zu, damit keine Anfrage für immer wartet, und die App wechselt
    /// zur Anmeldung.
    func anmeldungAbgelaufen() async {
        guard case .signedIn = stage else { return }
        if adminSperre != nil { adminEntsperrenAbbrechen() }
        if bestaetigungOffen || !bestaetigungWarter.isEmpty { bestaetigenAbbrechen() }
        await signOutLocally()
        await profilwahlZeigen()
        lastError = String(localized: "Deine Anmeldung gilt nicht mehr. Bitte melde dich neu an.", bundle: .lumenKit, locale: .lumen)
    }

    /// Ein anderes Gerät hat den Code freigegeben (siehe `CodeAnmeldung`):
    /// das Token gehört jetzt diesem Gerät, für den aktiven Server.
    public func mitCodeAnmelden(token: String) async {
        lastError = nil
        await client.setToken(token)
        speichereToken(token)
        do {
            try await hinein()
        } catch {
            lastError = Self.text(error)
        }
    }

    /// Nach einer Änderung an den eigenen Einstellungen neu laden.
    public func refreshMe() async {
        guard case .signedIn = stage else { return }
        if let fresh = try? await client.me() { angemeldet(fresh) }
        // Nebenbei: Läuft der Medienschlüssel bald ab (App lange offen,
        // Fernseher über Nacht an), gibt es hier einen neuen.
        try? await client.medienSchluesselHolen()
    }

    /// Einstellungen des Profils ändern — Sprachen, Untertitel, Schalter.
    /// Der Server antwortet mit dem frischen Stand, der gleich gilt. Gibt
    /// die Meldung des Servers zurück, wenn es nicht klappt.
    @discardableResult
    public func prefsSetzen(_ werte: [String: String]) async -> String? {
        guard case .signedIn = stage else { return nil }
        do {
            angemeldet(try await client.updatePrefs(werte))
            return nil
        } catch {
            return Self.text(error)
        }
    }

    // MARK: - Profilbild und Neuigkeiten

    /// Zählt hoch, sobald ein eigenes Bild hochgeladen wurde — es hängt als
    /// `?v=` an der Bildadresse, damit nirgends das alte stehen bleibt.
    public private(set) var avatarStand = 0

    /// Die Adresse des eigenen Bilds eines Kontos, mit dem Stand.
    public func avatarURL(accountId: String) async -> URL {
        await client.avatarURL(accountId: accountId, stand: avatarStand)
    }

    /// Eine Farbe oder Figur wählen. Gibt die Meldung des Servers zurück,
    /// wenn es nicht klappt.
    public func avatarSetzen(_ wert: String) async -> String? {
        do {
            angemeldet(try await client.setAvatar(wert))
            return nil
        } catch {
            return Self.text(error)
        }
    }

    /// Ein eigenes Bild hochladen (JPEG). Der Server verkleinert es.
    public func avatarHochladen(_ jpeg: Data) async -> String? {
        do {
            let me = try await client.uploadAvatar(jpeg, art: "image/jpeg")
            avatarStand += 1
            angemeldet(me)
            return nil
        } catch {
            return Self.text(error)
        }
    }

    /// Die Liste der Neuigkeiten wurde geöffnet: alles bis jetzt gilt als
    /// gelesen, der Punkt verschwindet.
    public func neuigkeitenGelesen() async {
        try? await client.newsSeen()
        await refreshMe()
    }

    // MARK: - Hilfen

    /// „192.168.1.20“, „lumen.local:8097“, „http://…“ — alles soll gehen.
    ///
    /// nonisolated, weil es nur rechnet: so lässt es sich aus jedem
    /// Zusammenhang aufrufen, auch aus einem Test.
    public nonisolated static func normalize(_ input: String) -> URL? {
        var text = input.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !text.isEmpty else { return nil }
        if !text.contains("://") { text = "http://" + text }
        guard var comps = URLComponents(string: text), let host = comps.host, !host.isEmpty
        else { return nil }
        if comps.port == nil && comps.scheme == "http" { comps.port = 8097 }
        comps.path = ""
        comps.query = nil
        return comps.url
    }

    nonisolated static func unerreichbar(_ error: any Error) -> Bool {
        if case .unreachable? = error as? LumenError { return true }
        return false
    }

    public nonisolated static func text(_ error: any Error) -> String {
        (error as? LumenError)?.errorDescription ?? error.localizedDescription
    }
}

/// Der Schritt beim Festlegen des eigenen Schutzes nach einem Zurücksetzen
/// (Session.neuerSchutz). `wer`: die Kennung des Profils oder „name:…" von
/// unterwegs; `erste`: die erste Eingabe, nil solange sie fehlt.
public struct NeuerSchutz: Equatable, Sendable {
    public let wer: String
    let code: String
    /// pin oder password — wie der Server es nennt.
    public let art: String
    var erste: String?

    /// Kommt gerade die zweite Eingabe?
    public var zweiteEingabe: Bool { erste != nil }

    /// Die Überschrift über dem Eingabefeld.
    public var titel: String {
        if erste != nil {
            return String(localized: "Noch einmal", bundle: .lumenKit, locale: .lumen)
        }
        return art == "password"
            ? String(localized: "Neues Passwort festlegen", bundle: .lumenKit, locale: .lumen)
            : String(localized: "Neue PIN festlegen", bundle: .lumenKit, locale: .lumen)
    }
}
