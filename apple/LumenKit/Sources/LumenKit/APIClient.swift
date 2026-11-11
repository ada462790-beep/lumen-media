// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

import Foundation

/// Alles, was die Apps vom Server wissen, geht durch diese Klasse.
///
/// Sie kennt genau einen Server und genau ein Token. Wechselt das Profil,
/// wechselt das Token — mehr Zustand gibt es hier nicht.
public actor APIClient {
    public private(set) var baseURL: URL
    public private(set) var token: String?

    /// Das Entsperr-Token der Verwaltung. Nur im Speicher, nie
    /// im Schlüsselbund: wer die App neu startet, entsperrt neu — so ist
    /// die Sperre gemeint. Es geht als `X-Lumen-Admin` an jede
    /// Verwaltungs-Anfrage mit; der Server verlängert es bei jeder.
    public private(set) var adminToken: String?

    /// Der Medienschlüssel: was in Bild-, Video- und Untertiteladressen
    /// steht, statt des Tokens. Ein Player kann keine Kopfzeile mitschicken,
    /// und eine Adresse landet im Protokoll jedes Reverse-Proxys davor —
    /// das Token darf darum nie in einer Adresse stehen. Der Schlüssel
    /// öffnet nur Medien, läuft ab und führt zu niemandem.
    ///
    /// Nur im Speicher, nie im Schlüsselbund: Der Server vergisst ihn bei
    /// jedem Neustart ohnehin, und nach einem Start der App holt die
    /// Sitzung einen frischen.
    public private(set) var medienSchluessel: String?
    /// Bis wann er nach unserer Rechnung sicher gilt. Der Server verlängert
    /// ihn bei jeder Benutzung (bis zu 48 Stunden) — hier steht also eher
    /// zu wenig als zu viel, und das ist die richtige Seite zum Irren.
    public private(set) var medienSchluesselBis: Date?
    /// Ein Abruf, der gerade läuft. Viele Bilder zugleich sollen nicht
    /// viele Schlüssel holen.
    private var schluesselAbruf: Task<Void, Error>?

    /// Wird gerufen, wenn der Server das Token mitten in der Sitzung
    /// zurückweist (401) — etwa nach „Überall abmelden" auf einem anderen
    /// Gerät. Die Sitzung wechselt dann zur Anmeldung, statt jede Ansicht
    /// einzeln „Die Anmeldung gilt nicht mehr" sagen zu lassen.
    private var beiAbmeldung: (@Sendable () -> Void)?

    public func setAbmeldeHandler(_ handler: (@Sendable () -> Void)?) {
        beiAbmeldung = handler
    }

    /// Unter dieser Restzeit holt die App einen neuen Schlüssel. Eine
    /// Stunde reicht für alles, was ohne Wiedergabe-Start lädt; vor jeder
    /// Wiedergabe wird ohnehin neu geholt.
    public static let schluesselVorlauf: TimeInterval = 3600

    private var session: URLSession
    private let decoder: JSONDecoder
    private let encoder: JSONEncoder

    /// Die Kopfzeile, in der das Entsperr-Token reist — derselbe Name wie
    /// `kopfVerwaltung` im Server.
    public static let adminHeader = "X-Lumen-Admin"

    /// Die Kopfzeile, in der die Bestätigung reist — derselbe Name wie
    /// `kopfBestaetigung` im Server. Rechte ändern (Gruppen, Grenzen eines
    /// Kontos) verlangt PIN, Passwort oder Code noch einmal.
    public static let bestaetigungHeader = "X-Lumen-Bestaetigung"

    /// Der Code für die Bestätigung, solange eine Anfrage ihn braucht.
    /// An die laufende Aufgabe gebunden statt an den Client: so geht er
    /// genau mit der Anfrage mit, die nach ihm gefragt hat, und mit keiner
    /// anderen, die zufällig gleichzeitig läuft. `Session.verwaltet` setzt
    /// ihn, wenn der Server „bestaetigen" sagt, und wiederholt die Anfrage.
    public static let bestaetigung = TaskLocal<String?>(wrappedValue: nil)

    public init(baseURL: URL, token: String? = nil, session: URLSession = .shared) {
        self.baseURL = baseURL
        self.token = token
        self.session = session
        self.decoder = JSONDecoder.lumen
        self.encoder = JSONEncoder()
    }

    /// Ein neues Anmelde-Token heisst eine andere Person — oder niemand.
    /// Das Entsperr-Token der vorigen fällt damit weg.
    public func setToken(_ value: String?) {
        token = value
        adminToken = nil
        schluesselVergessen()
    }
    /// Ein anderer Server kennt den Schlüssel des einen nicht.
    public func setBaseURL(_ value: URL) {
        if value != baseURL { schluesselVergessen() }
        baseURL = value
    }
    public func setAdminToken(_ value: String?) { adminToken = value }

    /// Das Zertifikat des Servers festnageln.
    ///
    /// Von unterwegs läuft alles über TLS, und ein Heimserver stellt sein
    /// Zertifikat meist selbst aus — dem traut kein Gerät von sich aus.
    /// Statt die Prüfung abzuschalten wird genau eines zugelassen; den
    /// Fingerabdruck zeigt der Server in der Verwaltung an.
    ///
    /// nil heisst: zurück zur gewöhnlichen Prüfung des Systems.
    public func pinnen(_ fingerabdruck: String?) {
        guard let sauber = Pinnen.saubern(fingerabdruck) else {
            session = .shared
            return
        }
        let aufbau = URLSessionConfiguration.ephemeral
        aufbau.waitsForConnectivity = false
        session = URLSession(configuration: aufbau,
                             delegate: Zertifikatspruefer(erwartet: sauber),
                             delegateQueue: nil)
    }

    // MARK: - Medienschlüssel

    /// Für Tests und für die Sitzung: einen Schlüssel von Hand setzen.
    public func setMedienSchluessel(_ key: String?, gueltigBis: Date? = nil) {
        medienSchluessel = key
        medienSchluesselBis = key == nil ? nil : (gueltigBis ?? Date().addingTimeInterval(6 * 3600))
    }

    private func schluesselVergessen() {
        medienSchluessel = nil
        medienSchluesselBis = nil
        schluesselAbruf = nil
    }

    /// Ob der Schlüssel fehlt oder in weniger als einer Stunde abläuft.
    public var medienSchluesselBaldFaellig: Bool {
        guard medienSchluessel != nil, let bis = medienSchluesselBis else { return true }
        return bis.timeIntervalSinceNow < Self.schluesselVorlauf
    }

    /// Den Medienschlüssel holen, wenn keiner da ist oder er bald abläuft —
    /// mit `erzwingen` in jedem Fall. Ohne Token gibt es keinen.
    ///
    /// Vor jeder Wiedergabe mit `erzwingen`: Ein Film dauert Stunden, und
    /// der Schlüssel in der Adresse, die AVPlayer bekommt, muss bis zum
    /// letzten Segment halten. Der Server verlängert ihn mit jedem Segment.
    public func medienSchluesselHolen(erzwingen: Bool = false) async throws {
        guard let tokenJetzt = token, !tokenJetzt.isEmpty else {
            schluesselVergessen()
            return
        }
        if !erzwingen, !medienSchluesselBaldFaellig { return }
        // Läuft schon ein Abruf, ist dessen Ergebnis frisch genug — auch
        // für den, der erzwingen wollte.
        if let laufend = schluesselAbruf {
            try await laufend.value
            return
        }
        let abruf = Task { () async throws -> Void in
            let antwort: Medienschluessel = try await self.get("/me/media-key")
            self.schluesselUebernehmen(antwort, fuer: tokenJetzt)
        }
        schluesselAbruf = abruf
        defer { if schluesselAbruf == abruf { schluesselAbruf = nil } }
        try await abruf.value
    }

    /// Nur übernehmen, wenn das Token noch dasselbe ist: Wer sich während
    /// des Abrufs abgemeldet hat, soll keinen Schlüssel der vorigen Person
    /// behalten.
    private func schluesselUebernehmen(_ antwort: Medienschluessel, fuer tokenBeimAbruf: String) {
        guard token == tokenBeimAbruf, !antwort.key.isEmpty else { return }
        medienSchluessel = antwort.key
        // Mit der eigenen Uhr rechnen, nicht mit `gueltigBis`: Die Uhr des
        // NAS und die des Geräts gehen selten gleich.
        let sek = antwort.gueltigSek > 0 ? antwort.gueltigSek : 6 * 3600
        medienSchluesselBis = Date().addingTimeInterval(TimeInterval(sek))
    }

    /// Läuft der Schlüssel bald ab, holt das einen neuen im Hintergrund.
    /// Für die Adressen, die gleich gebraucht werden, gilt der alte noch.
    private func schluesselNachsehen() {
        guard token != nil, schluesselAbruf == nil, medienSchluesselBaldFaellig else { return }
        Task { try? await self.medienSchluesselHolen() }
    }

    /// Eine Medienadresse hat nicht geladen. War es ein abgelaufener
    /// Schlüssel (401 — etwa nach einem Neustart des Servers), gibt es
    /// einmal einen neuen und dieselbe Adresse mit ihm; sonst nil (ein Bild,
    /// das es nicht gibt, bleibt weg, ohne dass dafür Schlüssel geholt
    /// werden).
    ///
    /// Nachgesehen wird mit HEAD: Der Server beantwortet es auf denselben
    /// Wegen, aber ohne das Bild noch einmal zu schicken.
    public func nachFehlschlag(_ adresse: URL) async -> URL? {
        guard token != nil,
              let teile = URLComponents(url: adresse, resolvingAgainstBaseURL: false),
              let alt = teile.queryItems?.first(where: { $0.name == "key" })?.value
        else { return nil }
        // Hat inzwischen schon jemand einen neuen geholt, reicht der.
        if let neu = medienSchluessel, neu != alt {
            return Self.tauscheSchluessel(in: teile, gegen: neu)
        }
        var probe = URLRequest(url: adresse)
        probe.httpMethod = "HEAD"
        guard let ergebnis = try? await session.data(for: probe),
              (ergebnis.1 as? HTTPURLResponse)?.statusCode == 401
        else { return nil }
        do {
            try await medienSchluesselHolen(erzwingen: true)
        } catch {
            return nil
        }
        guard let neu = medienSchluessel, neu != alt else { return nil }
        return Self.tauscheSchluessel(in: teile, gegen: neu)
    }

    private static func tauscheSchluessel(in teile: URLComponents, gegen neu: String) -> URL? {
        var teile = teile
        teile.queryItems = teile.queryItems?.map {
            $0.name == "key" ? URLQueryItem(name: "key", value: neu) : $0
        }
        return teile.url
    }

    /// Die Abfrage für eine Medienadresse: der Schlüssel, nie das Token.
    private func mitSchluessel(_ query: [String: String] = [:]) -> [String: String]? {
        // Auch wenn noch keiner da ist (der erste Abruf scheiterte etwa,
        // weil das Netz kurz weg war): dann kommt er für die nächsten.
        schluesselNachsehen()
        guard let key = medienSchluessel else { return nil }
        var q = query
        q["key"] = key
        return q
    }

    // MARK: - Grundlage

    private func url(_ path: String, _ query: [String: String] = [:]) -> URL {
        var comps = URLComponents(
            url: baseURL.appendingPathComponent("api/v1"), resolvingAgainstBaseURL: false)!
        // Ein Pfad mit „%" ist schon kodiert (`pfadTeil`) — etwa ein Name
        // mit Schrägstrich wie „AC/DC", der sonst zwei Pfadteile ergäbe.
        // Alle anderen Pfade bestehen aus Kennungen und bleiben, wie sie sind.
        if path.contains("%") {
            comps.percentEncodedPath = comps.percentEncodedPath + path
        } else {
            comps.path = comps.path + path
        }
        if !query.isEmpty {
            comps.queryItems = query
                .sorted { $0.key < $1.key }
                .map { URLQueryItem(name: $0.key, value: $0.value) }
        }
        return comps.url!
    }

    /// `roh` schickt Bytes so, wie sie sind, mit ihrer Art — für das
    /// Profilbild, das der Server selbst verkleinert. Sonst geht `body`
    /// als JSON.
    func request(_ method: String, _ path: String,
                         query: [String: String] = [:],
                         body: (any Encodable)? = nil,
                         roh: (daten: Data, art: String)? = nil) async throws -> Data {
        var req = URLRequest(url: url(path, query))
        req.httpMethod = method
        let hadToken = !(token ?? "").isEmpty
        // Das Token, mit dem diese Anfrage losgeht — kommt 401 zurück,
        // während inzwischen schon ein anderes gilt, betrifft es das neue nicht.
        let gesendet = token
        if let token { req.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization") }
        if let adminToken, path.hasPrefix("/admin/") {
            req.setValue(adminToken, forHTTPHeaderField: Self.adminHeader)
        }
        // Die Sprache der App: Meldungen des Servers kommen nach der
        // Anmeldung ohnehin in der des Profils, davor hilft das hier.
        req.setValue(Sprache.aktuell, forHTTPHeaderField: "Accept-Language")
        if let code = Self.bestaetigung.get(), !code.isEmpty {
            req.setValue(code, forHTTPHeaderField: Self.bestaetigungHeader)
        }
        if let roh {
            req.setValue(roh.art, forHTTPHeaderField: "Content-Type")
            req.httpBody = roh.daten
        } else if let body {
            req.setValue("application/json", forHTTPHeaderField: "Content-Type")
            req.httpBody = try encoder.encode(AnyEncodable(body))
        }

        let data: Data
        let response: URLResponse
        do {
            (data, response) = try await session.data(for: req)
        } catch {
            // Eine abgebrochene Anfrage (die Ansicht ist weg, die Aufgabe
            // beendet) ist kein Netzproblem — „nicht erreichbar" wäre falsch
            // und schickte die Sitzung zur Adresseingabe.
            if Self.istAbbruch(error) { throw CancellationError() }
            throw LumenError.unreachable(error)
        }
        guard let http = response as? HTTPURLResponse else {
            throw LumenError.server(String(localized: "Unerwartete Antwort.", bundle: .lumenKit, locale: .lumen), 0)
        }

        switch http.statusCode {
        case 200...299:
            return data
        case 423:
            // Die Verwaltung ist gesperrt — der Server sagt, womit. Ein
            // altes Token taugt dann auch nicht mehr; weg damit, sonst
            // ginge es mit ihm bei jeder Anfrage wieder von vorn los.
            // Steht vor dem 401-Fall, weil der mit `fallthrough` in den
            // nächsten Fall springt — und das soll `default` sein.
            adminToken = nil
            throw LumenError.locked(Self.sperre(from: data))
        case 401 where hadToken:
            // Mit Token heisst 401: die Anmeldung gilt nicht mehr — und
            // mit ihr der Medienschlüssel. Die Sitzung erfährt es über
            // `beiAbmeldung` und zeigt die Anmeldung.
            if token == gesendet {
                token = nil
                adminToken = nil
                schluesselVergessen()
                beiAbmeldung?()
            }
            throw LumenError.signedOut
        case 428 where Self.code(from: data) == "bestaetigen":
            // Rechte ändern verlangt eine Bestätigung — die Ansicht fragt
            // nach dem Code und schickt die Anfrage noch einmal.
            throw LumenError.bestaetigen(falsch: false)
        case 403 where Self.code(from: data) == "bestaetigen":
            // Der mitgeschickte Code stimmte nicht.
            throw LumenError.bestaetigen(falsch: true)
        case 403 where Self.code(from: data) == "unterwegs":
            // Von unterwegs gibt es keine Profilauswahl. Die App zeigt dann
            // die Anmeldung mit Profilname und PIN.
            throw LumenError.unterwegs(Self.message(from: data))
        case 400...599 where Self.code(from: data).map { Self.bekannteCodes.contains($0) } ?? false:
            // Antworten mit fester Kennung, nach der sich die App richtet
            // (PIN falsch, Downloads aus, zu viele Downloads …).
            throw LumenError.abgelehnt(code: Self.code(from: data) ?? "",
                                       meldung: Self.message(from: data),
                                       status: http.statusCode)
        case 401:
            // Ohne Token weist der Server gerade das zurück, was wir ihm
            // eben gegeben haben — einen falschen PIN etwa. Wer beides
            // vermengt, sagt beim Vertippen „Deine Anmeldung ist abgelaufen".
            fallthrough
        default:
            throw LumenError.server(Self.message(from: data), http.statusCode)
        }
    }

    /// Die Kennungen, die als `LumenError.abgelehnt` ankommen. Andere
    /// bleiben `server` — deren Fänger (etwa 409 beim Update) sollen sich
    /// nicht ändern.
    static let bekannteCodes: Set<String> = [
        "falsch", "verwaltung_braucht_sperre", "kein_download", "zu_viele_downloads",
        "wird_vorbereitet",  // = untertitelWirdVorbereitet
        "bildschirmzeit",    // = bildschirmzeit
    ]

    /// Die Bildschirmzeit des Kinderprofils ist um (HTTP 403). Die Meldung
    /// des Servers sagt, ab wann es weitergeht — die App zeigt sie, wie sie ist.
    public static let bildschirmzeit = "bildschirmzeit"  // sprache: fest (Kennung des Servers)

    /// Die Untertitelspur wird noch aus der Datei gezogen (202, bei HLS 503).
    public static let untertitelWirdVorbereitet = "wird_vorbereitet"

    /// Abgebrochen, nicht gescheitert: URLError.cancelled oder eine
    /// abgebrochene Aufgabe.
    public nonisolated static func istAbbruch(_ error: any Error) -> Bool {
        if error is CancellationError { return true }
        if let u = error as? URLError, u.code == .cancelled { return true }
        return false
    }

    /// Der Server legt seine Meldungen als `{"error": "…"}` ab — und sie
    /// sind für Menschen geschrieben, also werden sie unverändert gezeigt.
    private static func message(from data: Data) -> String {
        struct Body: Decodable { let error: String }
        if let b = try? JSONDecoder().decode(Body.self, from: data), !b.error.isEmpty {
            return b.error
        }
        return String(localized: "Das hat nicht geklappt.", bundle: .lumenKit, locale: .lumen)
    }

    /// Manche Antworten tragen neben dem Text eine feste Kennung, nach der
    /// sich die App richtet — `{"error": "…", "code": "unterwegs"}`.
    private static func code(from data: Data) -> String? {
        struct Body: Decodable { let code: String? }
        return (try? JSONDecoder().decode(Body.self, from: data))?.code
    }

    /// Bei 423 steht im Rumpf `{"error": "…", "sperre": "pin"}`.
    private static func sperre(from data: Data) -> String {
        struct Body: Decodable { let sperre: String? }
        return (try? JSONDecoder().decode(Body.self, from: data))?.sperre ?? "password"
    }

    func get<T: Decodable>(_ path: String, _ query: [String: String] = [:]) async throws -> T {
        let data = try await request("GET", path, query: query)
        return try decode(T.self, from: data, path: path)
    }

    func send<T: Decodable>(_ method: String, _ path: String,
                                    query: [String: String] = [:],
                                    body: (any Encodable)? = nil) async throws -> T {
        let data = try await request(method, path, query: query, body: body)
        return try decode(T.self, from: data, path: path)
    }

    func decode<T: Decodable>(_ type: T.Type, from data: Data, path: String) throws -> T {
        do {
            return try decoder.decode(type, from: data)
        } catch {
            // Eine unlesbare Antwort heisst fast immer: App und Server
            // passen nicht zusammen. Das soll man dem Fehler ansehen.
            throw LumenError.mismatch(path: path, underlying: error)
        }
    }

    // MARK: - Vor der Anmeldung

    public func hello() async throws -> Hello { try await get("/hello") }

    public func profiles() async throws -> [ProfileCard] { try await get("/profiles") }

    public func setUpOwner(name: String, password: String,
                           deviceName: String) async throws -> SignIn {
        struct Body: Encodable {
            let name: String, password: String, deviceName: String, deviceKind: String
        }
        let result: SignIn = try await send("POST", "/setup", body: Body(
            name: name, password: password, deviceName: deviceName,
            deviceKind: DeviceProfile.current.kind))
        setToken(result.token)
        return result
    }

    public func unlock(profileId: String, secret: String,
                       deviceName: String) async throws -> SignIn {
        struct Body: Encodable {
            let profileId: String, secret: String, deviceName: String, deviceKind: String
        }
        let result: SignIn = try await send("POST", "/unlock", body: Body(
            profileId: profileId, secret: secret, deviceName: deviceName,
            deviceKind: DeviceProfile.current.kind))
        setToken(result.token)
        return result
    }

    /// Von unterwegs: Es gibt keine Profilauswahl (sie verriete die Namen
    /// im Haushalt), also meldet man sich mit Profilname und PIN oder
    /// Passwort an. Ein Profil ohne PIN öffnet sich von aussen nicht — die
    /// Meldung des Servers sagt das, und die App zeigt sie unverändert.
    public func unlock(name: String, secret: String,
                       deviceName: String) async throws -> SignIn {
        struct Body: Encodable {
            let name: String, secret: String, deviceName: String, deviceKind: String
        }
        let result: SignIn = try await send("POST", "/unlock", body: Body(
            name: name.trimmingCharacters(in: .whitespacesAndNewlines),
            secret: secret, deviceName: deviceName,
            deviceKind: DeviceProfile.current.kind))
        setToken(result.token)
        return result
    }

    public func redeemInvite(code: String, name: String,
                             lockKind: String, secret: String) async throws -> SignIn {
        struct Body: Encodable {
            let code: String, name: String, lockKind: String, secret: String
        }
        let result: SignIn = try await send("POST", "/invite/redeem", body: Body(
            code: code, name: name, lockKind: lockKind, secret: secret))
        setToken(result.token)
        return result
    }

    // MARK: - Anmelden mit Code

    /// Einen Code anfragen, den jemand an einem anderen Gerät freigibt.
    /// Ohne Anmeldung; das Geheimnis bleibt hier, nur der Code wird gezeigt.
    public func codeStarten() async throws -> CodeStart {
        struct Body: Encodable { let deviceName: String, deviceKind: String }
        return try await send("POST", "/code/start", body: Body(
            deviceName: DeviceProfile.current.name,
            deviceKind: DeviceProfile.current.kind))
    }

    /// Nachfragen, ob der Code freigegeben ist. Der Server antwortet mit
    /// 202 und `status`, solange er wartet, mit 200 und dem Token, sobald
    /// freigegeben, und mit 410, wenn der Code abgelaufen ist — das kommt
    /// als `LumenError.server(_, 410)`.
    ///
    /// Das Token wird hier nicht übernommen: das entscheidet die Sitzung,
    /// die es auch für den richtigen Server speichert.
    public func codeAbfragen(geheim: String) async throws -> CodeStand {
        try await get("/code/\(geheim)")
    }

    /// Einen Code, den ein anderes Gerät zeigt, für das eigene Profil
    /// freigeben. Gibt den Namen des Geräts zurück.
    public func codeFreigeben(_ code: String) async throws -> CodeFreigabe {
        struct Body: Encodable { let code: String }
        return try await send("POST", "/code/approve", body: Body(code: code))
    }

    // MARK: - Ich

    public func me() async throws -> Me { try await get("/me") }

    public func updatePrefs(_ prefs: [String: String]) async throws -> Me {
        struct Body: Encodable { let prefs: [String: String] }
        return try await send("PATCH", "/me", body: Body(prefs: prefs))
    }

    /// Das Profilbild setzen: eine Farbe („av-b"), eine Figur
    /// („fig-fuchs") oder „bild", wenn schon ein eigenes hochgeladen ist.
    public func setAvatar(_ wert: String) async throws -> Me {
        struct Body: Encodable { let avatar: String }
        return try await send("PATCH", "/me", body: Body(avatar: wert))
    }

    /// Ein eigenes Bild hochladen — JPEG, PNG oder GIF, roh im Rumpf. Der
    /// Server schneidet die Mitte aus und verkleinert auf 256 × 256; danach
    /// ist das Profilbild „bild".
    public func uploadAvatar(_ daten: Data, art: String = "image/jpeg") async throws -> Me {
        let data = try await request("POST", "/me/avatar", roh: (daten: daten, art: art))
        return try decode(Me.self, from: data, path: "/me/avatar")
    }

    /// Das eigene Bild eines Kontos. Zu Hause auch ohne Anmeldung: die
    /// Profilauswahl zeigt es, bevor jemand angemeldet ist. Von unterwegs
    /// nur angemeldet — dann mit dem Medienschlüssel. `stand` hängt als
    /// `?v=` daran, damit nach dem Hochladen nicht das alte aus dem
    /// Zwischenspeicher kommt.
    public func avatarURL(accountId: String, stand: Int = 0) -> URL {
        var q: [String: String] = stand > 0 ? ["v": String(stand)] : [:]
        if let mit = mitSchluessel(q) { q = mit }
        return url("/avatar/\(accountId)", q)
    }

    public func devices() async throws -> [Device] { try await get("/me/devices") }

    /// Die eigenen laufenden Wiedergaben — damit man einen vergessenen
    /// Fernseher selbst beenden kann. Nur die des eigenen Profils.
    public func eigeneWiedergaben() async throws -> [EigeneWiedergabe] {
        try await get("/me/playbacks")
    }

    /// Eine eigene Wiedergabe beenden. Läuft sie schon nicht mehr, kommt
    /// 404 mit „diese wiedergabe läuft nicht mehr".
    public func eigeneWiedergabeBeenden(_ id: String) async throws {
        _ = try await request("DELETE", "/me/playbacks/\(id)")
    }

    // MARK: - Neuigkeiten

    /// Neue Folgen meiner Serien, neue Filme meiner Reihen, neu
    /// Dazugekommenes — nur aus dem eigenen Stand.
    public func news() async throws -> Neuigkeiten { try await get("/news") }

    /// Alles bis jetzt gilt als gelesen.
    public func newsSeen() async throws {
        _ = try await request("POST", "/news/seen")
    }

    /// Genau dieses Gerät beim Server abmelden: sein Token gilt danach
    /// nicht mehr.
    public func diesesGeraetAbmelden() async throws {
        _ = try await request("DELETE", "/me/device")
    }

    public func signOutEverywhere() async throws {
        _ = try await request("POST", "/me/sign-out-everywhere")
    }

    public func clearHistory() async throws {
        _ = try await request("DELETE", "/me/progress")
    }

    // MARK: - Mediathek

    public func home() async throws -> Home { try await get("/home") }

    public func items(kind: String? = nil, search: String? = nil, genre: String? = nil,
                      person: String? = nil, sort: String? = nil, limit: Int = 120) async throws -> [Item] {
        var q: [String: String] = ["limit": String(limit)]
        if let kind { q["kind"] = kind }
        if let search, !search.isEmpty { q["q"] = search }
        if let genre { q["genre"] = genre }
        if let person, !person.isEmpty { q["person"] = person }
        if let sort { q["sort"] = sort }
        return try await get("/items", q)
    }

    /// Eine Seite des Rasters „Filme" oder „Serien": Titel ab `offset`,
    /// die Gesamtzahl, ob es weitergeht, und die Genres für die Knöpfe —
    /// dieselbe Anfrage wie im Browser (api.js: seite). Ohne `genre` alle.
    ///
    /// `sort` ist eine `Sortierung` (title, added, premiered, rating,
    /// runtime, lastwatched, unwatched, random), `dir` asc oder desc (nil:
    /// die übliche Richtung), `seed` der Samen von „random" aus der ersten
    /// Seite; nil lässt den Server wählen — bei einer Liste die
    /// Reihenfolge, in der gemerkt wurde. `buchstabe` springt zu den Titeln mit diesem Anfang („A" …
    /// „Z", die Raute für Zahlen und Zeichen). `liste` ist merkliste oder
    /// favoriten: dann nur die eigenen.
    public func seite(kind: String?, genre: String? = nil, sort: String? = "title",
                      dir: String? = nil, seed: Int64? = nil,
                      buchstabe: String? = nil, liste: String? = nil,
                      limit: Int = 60, offset: Int = 0) async throws -> ItemSeite {
        var q: [String: String] = [
            "seite": "1", "limit": String(limit), "offset": String(offset),
        ]
        if let kind, !kind.isEmpty { q["kind"] = kind }
        if let sort, !sort.isEmpty { q["sort"] = sort }
        if let dir, !dir.isEmpty { q["dir"] = dir }
        if let seed, seed > 0 { q["seed"] = String(seed) }
        if let genre, !genre.isEmpty { q["genre"] = genre }
        // Die Raute kodiert URLComponents selbst als %23 — roh stünde es für den
        // Anker der Adresse, und der Server bekäme gar nichts.
        if let buchstabe, !buchstabe.isEmpty { q["buchstabe"] = buchstabe }
        if let liste, !liste.isEmpty { q["liste"] = liste }
        return try await get("/items", q)
    }

    /// Wie viele Titel mit welchem Buchstaben anfangen — `{"A": 12, "B": 3}`, die Raute für Zahlen.
    /// Ein Buchstabe, der fehlt, hat keine Titel; sein Knopf bleibt grau.
    public func buchstaben(kind: String, genre: String? = nil) async throws -> [String: Int] {
        var q: [String: String] = ["kind": kind]
        if let genre, !genre.isEmpty { q["genre"] = genre }
        return try await get("/items/letters", q)
    }

    /// Die eigene Merkliste, zuletzt Gemerktes zuerst. Früher holte die App
    /// die ersten Titel der Mediathek und siebte — wer mehr hatte, sah
    /// gemerkte Titel weiter hinten nie.
    public func merkliste(limit: Int = 200) async throws -> ItemSeite {
        try await seite(kind: nil, sort: nil, liste: "merkliste", limit: limit)
    }

    /// Was dieses Profil fertig geschaut hat, zuletzt Beendetes zuerst —
    /// Serien, sobald alle Folgen gesehen sind. Nur das eigene Profil.
    public func gesehen(limit: Int = 500) async throws -> ItemSeite {
        try await seite(kind: nil, sort: nil, liste: "gesehen", limit: limit)
    }

    /// Die eigenen Favoriten einer Art (movie | series).
    public func favoriten(kind: String, limit: Int = 200) async throws -> ItemSeite {
        try await seite(kind: kind, sort: nil, liste: "favoriten", limit: limit)
    }

    /// Die Suche: Titel, dazu passende Personen und Genres, und wie der
    /// Server die Eingabe gedeutet hat („Komödie 90er"). Eine leere Eingabe
    /// gibt ein leeres Ergebnis, ohne Anfrage.
    public func search(_ text: String, limit: Int = 60) async throws -> Suchergebnis {
        let eingabe = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !eingabe.isEmpty else {
            return Suchergebnis(items: [], personen: [], genres: [], reihen: [], sammlungen: [], deutung: nil)
        }
        return try await get("/search", ["q": eingabe, "limit": String(limit)])
    }

    public func item(_ id: String) async throws -> ItemDetail {
        try await get("/items/\(id)")
    }

    public func children(of id: String) async throws -> [Item] {
        try await get("/items/\(id)/children")
    }

    // MARK: - Personen

    /// Ein Name als einzelner Pfadteil: alles ausser Buchstaben, Ziffern
    /// und `-._~` wird kodiert — auch der Schrägstrich in „AC/DC" und
    /// Umlaute. Was herauskommt, enthält darum nur ASCII.
    static func pfadTeil(_ text: String) -> String {
        let erlaubt = CharacterSet(charactersIn:
            "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-._~")
        return text.addingPercentEncoding(withAllowedCharacters: erlaubt) ?? text
    }

    /// Foto, Lebensdaten und Biografie einer Person. 404, wenn
    /// sie in keinem Titel steht, den dieses Profil sehen darf.
    public func person(_ name: String) async throws -> PersonInfo {
        let teil = Self.pfadTeil(name)
        return try await get("/persons/\(teil)")
    }

    /// Das Foto zur Personenseite, mit dem Medienschlüssel. nil, wenn der
    /// Server keins hat (`bild` fehlt) oder niemand angemeldet ist.
    public func personBildURL(_ info: PersonInfo) -> URL? {
        guard info.bild != nil, token != nil, let q = mitSchluessel() else { return nil }
        let teil = Self.pfadTeil(info.name)
        return url("/persons/\(teil)/bild", q)
    }

    // MARK: - Filmreihen

    public func collections() async throws -> [Reihe] { try await get("/collections") }

    public func collection(_ id: String) async throws -> Reihe {
        try await get("/collections/\(id)")
    }

    /// Das Bild einer Filmreihe: ihr eigenes (von TMDb), sonst das des
    /// ersten Films — wie `reiheBildUrl` auf Android. Das eigene liegt
    /// unter derselben Bilderroute wie die eines Titels, mit der Kennung
    /// der Reihe.
    public func reiheBildURL(_ reihe: Reihe, wide: Bool = false) -> URL? {
        if (wide ? reihe.backdrop : reihe.poster) != nil {
            return artworkURL(itemId: reihe.id, kind: wide ? "backdrop" : "poster")
        }
        guard let erster = reihe.items.first else { return nil }
        return artworkURL(for: erster, wide: wide)
    }

    // MARK: - Eigene Sammlungen

    /// Alle Sammlungen, aus denen dieses Profil etwas sehen darf — je mit
    /// den ersten Titeln fürs Bild. Ein älterer Server kennt sie nicht
    /// (404); die Ansichten zeigen dann einfach keine.
    public func sammlungen() async throws -> [Sammlung] { try await get("/sammlungen") }

    /// Eine Sammlung mit allen Titeln, die dieses Profil sehen darf.
    public func sammlung(_ id: String) async throws -> Sammlung {
        try await get("/sammlungen/\(id)")
    }

    /// Das Bild einer Sammlung: ihr eigenes, sonst das Poster des ersten
    /// Titels. Das eigene liegt wie bei den Filmreihen unter der
    /// Bilderroute der Titel, mit der Kennung der Sammlung.
    public func sammlungBildURL(_ sammlung: Sammlung) -> URL? {
        if sammlung.bild != nil {
            return artworkURL(itemId: sammlung.id, kind: "poster")
        }
        guard let erster = sammlung.items.first else { return nil }
        return artworkURL(for: erster)
    }

    // MARK: - Bildschirmzeit

    /// Wie viel Bildschirmzeit dem eigenen Profil heute bleibt. Bei
    /// Profilen ohne Regel `aktiv == false`.
    public func bildschirmzeit() async throws -> Bildschirmzeit {
        try await get("/me/bildschirmzeit")
    }

    // MARK: - Listen

    /// Alle Listen, die dieses Profil sehen darf: die eigenen und die, die
    /// andere im Haushalt geteilt haben. Mit `itemId` steht an jeder Liste
    /// zusätzlich, ob dieser Titel schon drin ist.
    public func playlists(itemId: String? = nil) async throws -> [Liste] {
        var q: [String: String] = [:]
        if let itemId { q["item"] = itemId }
        return try await get("/playlists", q)
    }

    public func playlist(_ id: String) async throws -> Liste {
        try await get("/playlists/\(id)")
    }

    public func createPlaylist(name: String) async throws -> Liste {
        struct Body: Encodable { let name: String }
        return try await send("POST", "/playlists", body: Body(name: name))
    }

    /// Nur was nicht nil ist, geht mit — der Server lässt den Rest, wie er
    /// ist. `gemeinsam: true` macht die Liste dort zugleich geteilt.
    public func updatePlaylist(_ id: String, name: String? = nil,
                               geteilt: Bool? = nil, gemeinsam: Bool? = nil) async throws {
        struct Body: Encodable { let name: String?, geteilt: Bool?, gemeinsam: Bool? }
        _ = try await request("POST", "/playlists/\(id)", body: Body(
            name: name, geteilt: geteilt, gemeinsam: gemeinsam))
    }

    public func deletePlaylist(_ id: String) async throws {
        _ = try await request("DELETE", "/playlists/\(id)")
    }

    public func addToPlaylist(_ id: String, itemId: String) async throws {
        struct Body: Encodable { let itemId: String }
        _ = try await request("POST", "/playlists/\(id)/items", body: Body(itemId: itemId))
    }

    public func removeFromPlaylist(_ id: String, itemId: String) async throws {
        _ = try await request("DELETE", "/playlists/\(id)/items/\(itemId)")
    }

    /// Die ganze neue Reihenfolge auf einmal — nicht einzelne Schritte, die
    /// sich mit jemand anderem überkreuzen könnten.
    public func orderPlaylist(_ id: String, itemIds: [String]) async throws {
        struct Body: Encodable { let itemIds: [String] }
        _ = try await request("POST", "/playlists/\(id)/order", body: Body(itemIds: itemIds))
    }

    // MARK: - Mein Stand

    public func saveProgress(itemId: String, position: Double, duration: Double) async throws {
        struct Body: Encodable { let positionMs: Int64, durationMs: Int64 }
        _ = try await request("POST", "/items/\(itemId)/progress", body: Body(
            positionMs: Int64(position * 1000), durationMs: Int64(duration * 1000)))
    }

    public func markFinished(_ id: String) async throws {
        _ = try await request("POST", "/items/\(id)/finished")
    }

    public func clearFinished(_ id: String) async throws {
        _ = try await request("DELETE", "/items/\(id)/finished")
    }

    public func rate(_ id: String, stars: Int, shared: Bool) async throws {
        struct Body: Encodable { let stars: Int, shared: Bool }
        _ = try await request("PUT", "/items/\(id)/rating", body: Body(stars: stars, shared: shared))
    }

    public func addToWatchlist(_ id: String) async throws {
        _ = try await request("PUT", "/watchlist/\(id)")
    }

    public func removeFromWatchlist(_ id: String) async throws {
        _ = try await request("DELETE", "/watchlist/\(id)")
    }

    /// Das Herz setzen — anders als die Merkliste nicht „will
    /// ich noch sehen", sondern „hat mir richtig gefallen".
    public func addFavorite(_ id: String) async throws {
        _ = try await request("PUT", "/favorites/\(id)")
    }

    public func removeFavorite(_ id: String) async throws {
        _ = try await request("DELETE", "/favorites/\(id)")
    }

    // MARK: - Wünsche

    public func wishes() async throws -> [Wish] { try await get("/wishes") }

    public func wish(title: String, year: Int = 0) async throws -> Wish {
        struct Body: Encodable { let title: String, year: Int }
        return try await send("POST", "/wishes", body: Body(title: title, year: year))
    }

    // MARK: - Wiedergabe

    public func plan(fileId: String) async throws -> PlanResponse {
        try await get("/stream/\(fileId)/plan", DeviceProfile.current.query())
    }

    public func seekPoint(fileId: String, seconds: Double) async throws -> SeekPoint {
        var q = DeviceProfile.current.query()
        q["t"] = String(Int(seconds))
        return try await get("/stream/\(fileId)/seek", q)
    }

    /// Die Adresse, die AVPlayer bekommt.
    ///
    /// Der Medienschlüssel hängt als Abfrageparameter dran, nicht als
    /// Kopfzeile: AVPlayer lädt Segmente selbst nach, und dafür eigene
    /// Kopfzeilen mitzugeben, geht nur über nicht öffentliche
    /// Schnittstellen. Das Token selbst steht nie in der Adresse. Vor dem
    /// Aufruf `medienSchluesselHolen(erzwingen: true)` — das macht
    /// `Playback`.
    ///
    /// `spur` ist der Index einer Tonspur, wenn nicht die erste laufen
    /// soll. Die direkte Datei kennt den Parameter nicht — der Ton steckt
    /// im Strom —, darum geht es dann über HLS, das den Ton neu zusammen-
    /// setzt und nebenbei die Text-Untertitel als Spurgruppe mitbringt.
    public func streamURL(for file: MediaFile, plan: PlaybackPlan,
                          startingAt seconds: Double = 0, spur: Int? = nil) -> URL {
        var q = DeviceProfile.current.query()
        if let mit = mitSchluessel(q) { q = mit }
        if let spur, spur >= 0 { q["spur"] = String(spur) }

        if spur == nil, plan.direct, DeviceProfile.playsDirectly(container: file.container) {
            return url("/stream/\(file.id)", q)
        }
        // Sonst HLS — das kann jedes Apple-Gerät von Haus aus, samt Springen.
        return url("/stream/\(file.id)/hls/master.m3u8", q)
    }

    /// Läuft diese Adresse über HLS? Danach richtet sich, ob der Player
    /// seine eigene Spurwahl für Untertitel anbieten kann.
    public static func istHLS(_ url: URL) -> Bool {
        url.path.hasSuffix("/hls/master.m3u8")
    }

    /// Eine Untertitelspur als WebVTT, mit dem Token als Kopfzeile. Die
    /// Zeiten darin gelten für die ganze Datei — der Player legt sie
    /// selbst über das Bild (`Untertitel.lesen`).
    ///
    /// Eingebettete Spuren zieht der Server erst aus der Datei; solange
    /// antwortet er 202 mit `{"code": "wird_vorbereitet"}` (und
    /// Retry-After: 3). Das ist JSON, kein WebVTT — dann kommt
    /// `LumenError.abgelehnt` mit `untertitelWirdVorbereitet`, und der Player
    /// fragt etwas später noch einmal.
    public func untertitel(fileId: String, spur: Int) async throws -> String {
        let datei = "\(spur).vtt"
        let data = try await request("GET", "/stream/\(fileId)/subtitles/\(datei)")
        if Self.code(from: data) == Self.untertitelWirdVorbereitet {
            throw LumenError.abgelehnt(code: Self.untertitelWirdVorbereitet, meldung: "", status: 202)
        }
        guard let text = String(data: data, encoding: .utf8) else {
            throw LumenError.server(String(localized: "Diese Untertitel lassen sich nicht lesen.", bundle: .lumenKit, locale: .lumen), 422)
        }
        return text
    }

    /// Ohne Medienschlüssel nil: Die Sitzung holt ihn, bevor die
    /// Mediathek erscheint, also heisst das „nicht angemeldet".
    public func artworkURL(itemId: String, kind: String = "poster") -> URL? {
        guard token != nil, let q = mitSchluessel() else { return nil }
        return url("/art/\(itemId)/\(kind)", q)
    }

    /// Die bewegte Vorschau: zwölf stumme Sekunden aus dem Film als MP4
    /// (H.264). Der Server schneidet sie beim ersten Abruf; gibt es keine,
    /// antwortet er mit 404, und das Bild bleibt stehen.
    public func vorschauURL(itemId: String) -> URL? {
        artworkURL(itemId: itemId, kind: "vorschau")
    }

    /// Das passende Bild zu einem Titel, mit Ersatz, wenn eins fehlt:
    /// hochkant Poster vor Standbild, breit Hintergrund vor Standbild vor
    /// Poster. Die Wahl steht am Titel (`posterArt`, `wideArt`), damit sie
    /// sich ohne Netz prüfen lässt.
    public func artworkURL(for item: Item, wide: Bool = false) -> URL? {
        artworkURL(itemId: item.id, kind: wide ? item.wideArt : item.posterArt)
    }

    // MARK: - Verwaltung

    public func adminLibraries() async throws -> [Library] { try await get("/admin/libraries") }

    public func adminScan(libraryId: String) async throws {
        _ = try await request("POST", "/admin/libraries/\(libraryId)/scan")
    }

    /// Entfernt eine Bibliothek aus Lumen. Die Dateien auf der Platte
    /// bleiben liegen — gelöscht wird nur, was Lumen darüber weiss.
    public func adminDeleteLibrary(_ id: String) async throws {
        _ = try await request("DELETE", "/admin/libraries/\(id)")
    }

    /// Eine Filmreihe auflösen. Die Filme bleiben in der Mediathek; es
    /// verschwindet nur die Gruppierung.
    public func adminDissolveCollection(_ id: String) async throws {
        struct Body: Encodable { let aufgeloest: Bool }
        _ = try await request("POST", "/admin/collections/\(id)", body: Body(aufgeloest: true))
    }

    /// Den Vorspann einer Serie markieren — für alle, die sie schauen.
    /// Der Server verlangt zwischen einer Sekunde und zehn Minuten.
    public func adminSetIntro(seriesId: String, vonMs: Int64, bisMs: Int64) async throws {
        struct Body: Encodable { let vonMs: Int64, bisMs: Int64 }
        _ = try await request("PUT", "/admin/items/\(seriesId)/intro", body: Body(vonMs: vonMs, bisMs: bisMs))
    }

    /// Die Markierung wieder entfernen. Kapitel in den Dateien bleiben
    /// davon unberührt.
    public func adminClearIntro(seriesId: String) async throws {
        _ = try await request("DELETE", "/admin/items/\(seriesId)/intro")
    }

    /// Einen Wunsch annehmen (working), erfüllen (done), ablehnen
    /// (declined) oder wieder öffnen (open).
    public func adminSetWishState(_ id: String, state: String, note: String = "") async throws {
        struct Body: Encodable { let state: String, note: String }
        _ = try await request("PUT", "/admin/wishes/\(id)", body: Body(state: state, note: note))
    }

    /// Doppelte Titel, fehlende Dateien, nicht erkannte Titel.
    public func adminCleanup() async throws -> Aufraeumen { try await get("/admin/cleanup") }

    /// Bei mehreren Fassungen eines Titels: diese Datei soll gelten.
    public func adminFassungWaehlen(itemId: String, fileId: String) async throws {
        struct Body: Encodable { let fileId: String }
        _ = try await request("PUT", "/admin/items/\(itemId)/fassung", body: Body(fileId: fileId))
    }

    /// Eine Datei, die in Wahrheit ein anderer Film ist, wird ein eigener
    /// Titel — mit dem, was ihr Name hergibt, und eigenen Angaben danach.
    public func adminDateiAbtrennen(_ fileId: String) async throws {
        _ = try await request("POST", "/admin/files/\(fileId)/abtrennen")
    }

    /// Teile eines Films (Teil 1, 2, 3 …) zu einer Datei zusammenführen.
    /// Nur der Besitzer. Der Server antwortet gleich (202) und arbeitet im
    /// Hintergrund; die Teile legt er beiseite, statt sie zu löschen.
    public func adminTeileZusammenfuehren(_ itemId: String) async throws -> Auftrag {
        try await send("POST", "/admin/items/\(itemId)/teile-zusammenfuehren")
    }

    /// Alle Filmreihen, auch die aufgelösten — ohne ihre Filme.
    public func adminCollections() async throws -> [ReiheZeile] { try await get("/admin/collections") }

    /// Eine aufgelöste Filmreihe zurückholen.
    public func adminRestoreCollection(_ id: String) async throws {
        struct Body: Encodable { let aufgeloest: Bool }
        _ = try await request("POST", "/admin/collections/\(id)", body: Body(aufgeloest: false))
    }

    public func adminAccounts() async throws -> [KontoEintrag] { try await get("/admin/accounts") }

    /// Eine Einladung, die sieben Tage und einmal gilt — mit diesen
    /// Bibliotheken. Die Grenzen bleiben, wie der Server sie voreinstellt,
    /// genau wie beim Knopf im Browser.
    public func adminCreateInvite(libraries: [String], days: Int = 7) async throws -> Einladung {
        struct Body: Encodable { let libraries: [String], limits: [String: String], days: Int }
        return try await send("POST", "/admin/invites", body: Body(libraries: libraries, limits: [:], days: days))
    }

    public func adminSessions() async throws -> Sessions { try await get("/admin/sessions") }

    public func adminStopSession(_ id: String) async throws {
        _ = try await request("DELETE", "/admin/sessions/\(id)")
    }

    public func adminLog(level: String = "all") async throws -> [LogLine] {
        try await get("/admin/log", level == "all" ? [:] : ["level": level])
    }

    // MARK: - Bibliotheken: Takt und Metadaten

    /// Wie oft Lumen von selbst nachsieht: alle `intervalMin` Minuten (0 =
    /// nur auf Knopfdruck), oder mit `scanAt` („HH:MM") täglich um diese Zeit.
    public func adminSetLibraryTakt(_ id: String, intervalMin: Int, scanAt: String) async throws {
        struct Body: Encodable { let intervalMin: Int, scanAt: String }
        _ = try await request("POST", "/admin/libraries/\(id)", body: Body(intervalMin: intervalMin, scanAt: scanAt))
    }

    /// Beschreibungen, Besetzung und Bilder einer Bibliothek (oder mit nil
    /// aller) noch einmal holen. Von Hand zugewiesene bleiben. Der Server
    /// antwortet gleich und holt im Hintergrund.
    public func adminMetadataReload(libraryId: String? = nil) async throws -> MetadatenAuftrag {
        var query: [String: String] = [:]
        if let libraryId, !libraryId.isEmpty { query["library"] = libraryId }
        return try await send("POST", "/admin/metadata/reload", query: query)
    }

    /// Die Metadaten eines einzelnen Titels noch einmal holen.
    public func adminItemReload(_ id: String) async throws {
        _ = try await request("POST", "/admin/items/\(id)/metadata/reload")
    }

    /// Was die Quelle zu einem Suchwort findet — auch ein Link von TMDb
    /// oder IMDb geht. Leeres Suchwort heisst: nach dem Titel suchen.
    public func adminItemSearch(_ id: String, query text: String, year: String = "") async throws -> [TitelTreffer] {
        var query: [String: String] = [:]
        let q = text.trimmingCharacters(in: .whitespacesAndNewlines)
        let jahr = year.trimmingCharacters(in: .whitespacesAndNewlines)
        if !q.isEmpty { query["q"] = q }
        if !jahr.isEmpty { query["year"] = jahr }
        return try await get("/admin/items/\(id)/metadata/search", query)
    }

    /// Einen Titel an den gewählten Eintrag der Quelle hängen.
    public func adminItemAssign(_ id: String, sourceId: String) async throws {
        struct Body: Encodable { let sourceId: String }
        _ = try await request("POST", "/admin/items/\(id)/metadata/assign", body: Body(sourceId: sourceId))
    }

    // MARK: - Gruppen und Rechte (Bestätigung nötig)

    /// Gruppen darf nur der Besitzer sehen und ändern. Jede Änderung hier
    /// verlangt eine Bestätigung — darum immer über `session.verwaltet`.
    public func adminGroups() async throws -> [Gruppe] { try await get("/admin/groups") }

    public func adminCreateGroup(name: String, verwaltung: Bool, vollzugriff: Bool = false,
                                 libraries: [String], limits: Limits) async throws -> Gruppe {
        try await send("POST", "/admin/groups", body: GruppenRumpf(
            name: name, verwaltung: verwaltung || vollzugriff, vollzugriff: vollzugriff,
            libraries: libraries, limits: limits))
    }

    public func adminUpdateGroup(_ id: String, name: String, verwaltung: Bool, vollzugriff: Bool = false,
                                 libraries: [String], limits: Limits) async throws -> Gruppe {
        try await send("PUT", "/admin/groups/\(id)", body: GruppenRumpf(
            name: name, verwaltung: verwaltung || vollzugriff, vollzugriff: vollzugriff,
            libraries: libraries, limits: limits))
    }

    /// Rechte direkt am Konto: "schauen", "verwalten" oder "voll"
    /// (Vollzugriff — alles wie der Besitzer). Verlangt eine Bestätigung.
    public func adminSetRolle(_ accountId: String, rolle: String) async throws {
        struct Body: Encodable { let rolle: String }
        _ = try await request("PUT", "/admin/accounts/\(accountId)/rolle", body: Body(rolle: rolle))
    }

    /// Eine Gruppe auflösen. Die Konten behalten ihre eigenen Einträge.
    public func adminDeleteGroup(_ id: String) async throws {
        _ = try await request("DELETE", "/admin/groups/\(id)")
    }

    /// Ein Konto in eine Gruppe stecken — leer heisst: aus der Gruppe nehmen.
    public func adminSetAccountGroup(_ accountId: String, groupId: String) async throws {
        struct Body: Encodable { let groupId: String }
        _ = try await request("PUT", "/admin/accounts/\(accountId)/group", body: Body(groupId: groupId))
    }

    /// Die eigenen Grenzen und Bibliotheken eines Kontos. In einer Gruppe
    /// ruhen sie, solange es drin ist.
    public func adminSetAccountLimits(_ accountId: String, limits: Limits, libraries: [String]) async throws {
        struct Body: Encodable { let limits: Limits, libraries: [String] }
        _ = try await request("PUT", "/admin/accounts/\(accountId)/limits",
                              body: Body(limits: limits, libraries: libraries))
    }

    // MARK: - Updates (nur der Besitzer)

    public func adminUpdate() async throws -> UpdateStand { try await get("/admin/update") }

    /// Jetzt bei GitHub nachsehen. Die Antwort ist der neue Stand.
    public func adminUpdateCheck() async throws -> UpdateStand { try await send("POST", "/admin/update/check") }

    /// Einstellungen speichern. `token` nil lässt das Token, wie es ist;
    /// leer entfernt es; sonst ersetzt es das alte.
    public func adminUpdateSettings(pruefen: String, pruefZeit: String, auto: String,
                                    fensterVon: String, fensterBis: String,
                                    token: String? = nil) async throws -> UpdateStand {
        struct Body: Encodable {
            let pruefen: String, pruefZeit: String, auto: String
            let fensterVon: String, fensterBis: String
            let token: String?
        }
        return try await send("POST", "/admin/update/settings", body: Body(
            pruefen: pruefen, pruefZeit: pruefZeit, auto: auto,
            fensterVon: fensterVon, fensterBis: fensterBis, token: token))
    }

    /// Die neueste Fassung einsetzen. Laufen Wiedergaben, antwortet der
    /// Server mit 409 — erst mit `trotzdem` setzt er sie dann ein.
    public func adminUpdateRun(trotzdem: Bool = false) async throws -> UpdateLauf {
        try await send("POST", "/admin/update/run", query: trotzdem ? ["trotzdem": "1"] : [:])
    }

    /// Version, freier Platz, letzte Sicherung. Darf jeder, der verwaltet.
    public func adminOverview() async throws -> Uebersicht { try await get("/admin/overview") }

    // MARK: - Sicherung (nur der Besitzer)

    /// Die vorhandenen Sicherungen, wann zuletzt gesichert wurde und ob
    /// das geklappt hat.
    public func adminBackups() async throws -> Sicherungen { try await get("/admin/backups") }

    /// Jetzt sichern statt erst nachts. Die Antwort ist dieselbe wie bei
    /// `adminBackups`, schon mit der neuen Sicherung.
    public func adminBackupNow() async throws -> Sicherungen { try await send("POST", "/admin/backups") }

    /// Sicherung `nr` zurückholen. Der Server antwortet noch, legt sie
    /// bereit und startet dann neu — eine Sekunde später ist er für eine
    /// Weile nicht erreichbar.
    public func adminRestoreBackup(_ nr: Int) async throws -> Wiederherstellung {
        try await send("POST", "/admin/backups/\(nr)/restore")
    }

    // MARK: - Verwaltungssperre

    /// Welche Sperre diese Person hat und ob sie gerade offen ist. Geht
    /// auch gesperrt — sonst käme niemand je hinein.
    public func adminLock() async throws -> Verwaltungssperre { try await get("/admin/lock") }

    /// PIN, Passwort oder Code aus der Authenticator-App. Stimmt er, hält
    /// der Client das Token und schickt es von nun an mit. Stimmt er nicht,
    /// antwortet der Server mit 403 „das stimmt nicht", nach zu vielen
    /// Versuchen mit 429.
    public func adminUnlock(code: String) async throws -> Entsperrung {
        struct Body: Encodable { let code: String }
        let result: Entsperrung = try await send("POST", "/admin/unlock", body: Body(code: code))
        adminToken = result.token
        return result
    }

    /// Die Verwaltung wieder zusperren — auf dem Server und hier.
    public func adminRelock() async throws {
        _ = try await request("POST", "/admin/relock")
        adminToken = nil
    }

    /// Den Anlauf zu einer Authenticator-App holen: QR-Code und Geheimnis.
    /// Gilt erst, wenn `adminSetLock(kind: "totp", code:)` einen Code daraus
    /// bestätigt.
    public func adminLockTOTPBegin() async throws -> TOTPEinrichtung {
        try await send("POST", "/admin/lock/totp")
    }

    /// Die eigene Sperre setzen: `none` (mit leerem `secret`), `pin` oder
    /// `password` mit dem Geheimnis, `totp` mit einem Code aus der eben
    /// eingerichteten App. Die Antwort bringt ein frisches Token mit, damit
    /// die Person nicht gleich ausgesperrt ist.
    /// Die Sprache des Servers setzen: die Vorgabe für alle Profile ohne
    /// eigene Wahl und für den Bildschirm vor der Anmeldung. Nur für den
    /// Besitzer und wer Vollzugriff hat; nur auf dem Mac angeboten.
    public func adminSpracheSetzen(_ sprache: String) async throws {
        struct Body: Encodable { let sprache: String }
        _ = try await request("PUT", "/admin/sprache", body: Body(sprache: sprache))
    }

    public func adminSetLock(kind: String, secret: String = "", code: String = "") async throws -> Entsperrung {
        struct Body: Encodable { let kind: String, secret: String, code: String }
        let result: Entsperrung = try await send("PUT", "/admin/lock", body: Body(
            kind: kind, secret: secret, code: code))
        adminToken = result.token
        return result
    }
}

// MARK: - Fehler

public enum LumenError: Error, LocalizedError, Sendable {
    case unreachable(any Error)
    case signedOut
    case server(String, Int)
    case mismatch(path: String, underlying: any Error)
    /// Die Verwaltung ist gesperrt (HTTP 423). Dabei steht, womit:
    /// pin, password oder totp — danach richtet sich der Entsperr-Dialog.
    case locked(String)
    /// Von unterwegs gibt es keine Profilauswahl (HTTP 403 mit `code`
    /// „unterwegs"). Dabei steht die Meldung des Servers.
    case unterwegs(String)
    /// Rechte ändern verlangt PIN, Passwort oder Code noch einmal (HTTP
    /// 428 mit `code` „bestaetigen"); `falsch`: der mitgeschickte stimmte
    /// nicht (HTTP 403).
    case bestaetigen(falsch: Bool)
    /// Eine Antwort mit fester Kennung (`APIClient.bekannteCodes`): die App
    /// sagt dazu einen eigenen, übersetzbaren Satz.
    case abgelehnt(code: String, meldung: String, status: Int)

    /// Der HTTP-Status, falls der Server geantwortet hat.
    public var status: Int? {
        switch self {
        case .server(_, let s): s
        case .abgelehnt(_, _, let s): s
        case .signedOut: 401
        case .locked: 423
        default: nil
        }
    }

    /// Die feste Kennung einer Antwort, falls es eine gab.
    public var code: String? {
        if case .abgelehnt(let code, _, _) = self { return code }
        return nil
    }

    /// Kennt der Server diesen Weg noch nicht? Dann blendet die App die
    /// Funktion still aus, statt einen Fehler zu zeigen — der Server und
    /// die Apps werden nicht immer gleichzeitig aktualisiert.
    public static func fehltAufServer(_ error: any Error) -> Bool {
        guard let s = (error as? LumenError)?.status else { return false }
        return s == 404 || s == 405 || s == 501
    }

    public var errorDescription: String? {
        switch self {
        case .unreachable:
            String(localized: "Der Server ist nicht erreichbar. Läuft er, und ist dieses Gerät im gleichen Netz?",
                   bundle: .lumenKit, locale: .lumen)
        case .signedOut:
            String(localized: "Die Anmeldung gilt nicht mehr.", bundle: .lumenKit, locale: .lumen)
        case .server(let message, _):
            message
        case .mismatch(let path, _):
            String(localized: "Die Antwort von \(path) war nicht lesbar. Vermutlich passen App und Server nicht zusammen.",
                   bundle: .lumenKit, locale: .lumen)
        case .locked:
            String(localized: "Die Verwaltung ist gesperrt.", bundle: .lumenKit, locale: .lumen)
        case .unterwegs(let message):
            message
        case .bestaetigen(let falsch):
            falsch ? String(localized: "Das stimmt nicht.", bundle: .lumenKit, locale: .lumen)
                : String(localized: "Zur Sicherheit braucht es noch einmal deinen PIN, dein Passwort oder den Code aus der Authenticator-App.", bundle: .lumenKit, locale: .lumen)
        case .abgelehnt(let code, let meldung, _):
            switch code {
            case "falsch":
                String(localized: "Das stimmt nicht.", bundle: .lumenKit, locale: .lumen)
            case "verwaltung_braucht_sperre":
                String(localized: "Wer verwalten darf, braucht einen PIN oder ein Passwort. Ohne geht es bei diesem Profil nicht.", bundle: .lumenKit, locale: .lumen)
            case "kein_download":
                String(localized: "Für dein Profil sind Downloads ausgeschaltet.", bundle: .lumenKit, locale: .lumen)
            case "zu_viele_downloads":
                String(localized: "Du hast schon so viele Titel heruntergeladen, wie dein Profil darf. Lösche zuerst einen.", bundle: .lumenKit, locale: .lumen)
            default:
                meldung
            }
        }
    }
}

// MARK: - Datum

extension JSONDecoder {
    /// Go schreibt Zeiten als RFC 3339 — mal mit Bruchteilen von Sekunden,
    /// mal ohne. Die eingebaute `.iso8601`-Behandlung kann nur eine der
    /// beiden Formen; beide zu können ist hier keine Feinheit, sondern der
    /// Unterschied zwischen „läuft“ und „jede Antwort unlesbar“.
    public static var lumen: JSONDecoder {
        let d = JSONDecoder()
        d.dateDecodingStrategy = .custom { decoder in
            let text = try decoder.singleValueContainer().decode(String.self)
            if let date = rfc3339WithFraction.date(from: text) { return date }
            if let date = rfc3339Plain.date(from: text) { return date }
            throw DecodingError.dataCorrupted(.init(
                codingPath: decoder.codingPath,
                debugDescription: "Unbekanntes Zeitformat: \(text)"))
        }
        return d
    }

    private static let rfc3339WithFraction: ISO8601DateFormatter = {
        let f = ISO8601DateFormatter()
        f.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        return f
    }()

    private static let rfc3339Plain: ISO8601DateFormatter = {
        let f = ISO8601DateFormatter()
        f.formatOptions = [.withInternetDateTime]
        return f
    }()
}

/// Was eine Gruppe ausmacht — beim Anlegen und beim Ändern derselbe Rumpf.
private struct GruppenRumpf: Encodable {
    let name: String
    let verwaltung: Bool
    var vollzugriff = false
    let libraries: [String]
    let limits: Limits
}

/// Kleiner Umschlag, damit `request` einen beliebigen Rumpf nehmen kann.
private struct AnyEncodable: Encodable {
    private let encodeIt: (Encoder) throws -> Void
    init(_ wrapped: any Encodable) { encodeIt = wrapped.encode(to:) }
    func encode(to encoder: Encoder) throws { try encodeIt(encoder) }
}
