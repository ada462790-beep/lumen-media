// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

import Foundation
import Observation

// Downloads: einen Titel mitnehmen — in den Zug, ins Flugzeug, in die
// Ferienwohnung ohne Netz. Auf iPhone, iPad und Mac; der Fernseher steht
// zu Hause am Server und braucht das nicht.
//
// Drei Teile:
// - `DownloadEintrag` beschreibt einen Download — was, von welchem Server,
//   wie weit, und wo man offline stehen geblieben ist.
// - `DownloadKatalog` ist die reine Buchhaltung darüber, ohne Netz und ohne
//   Dateien: so lässt sie sich prüfen.
// - `Downloads` lädt im Hintergrund (eine URLSession, die weiterläuft, wenn
//   die App ruht), legt die Dateien ab und reicht den Stand nach.
//
// Was der Server dazu sagt — ob das Profil darf, welche Dateien es gibt —,
// steht in `DownloadInfo`. Kennt er den Weg nicht (404), gibt es keinen
// Download-Knopf.

/// Ein Download.
public struct DownloadEintrag: Codable, Identifiable, Hashable, Sendable {
    public enum Zustand: String, Codable, Sendable {
        case wartet, laeuft, fertig, fehler
    }

    /// Die Datei auf dem Server — zugleich die Kennung des Downloads.
    public let fileId: String
    /// Der Titel, wie er beim Herunterladen war: zum Anzeigen und zum
    /// Abspielen ohne Server.
    public let item: Item
    /// Zu welchem Server und welchem Profil der Download gehört. Der Stand
    /// geht nur dorthin zurück — nie an ein anderes Profil.
    public let serverId: String
    public let accountId: String
    public let dateiname: String
    public let groesse: Int64
    public let hoehe: Int?
    public var zustand: Zustand
    public var geladen: Int64
    public var fehler: String?
    /// Der Name der Datei im Download-Ordner, sobald sie fertig ist.
    public var lokalerName: String?
    /// Wo offline zuletzt gestoppt wurde, und die Länge des Titels.
    public var stand: Double
    public var dauer: Double
    /// Der Stand ist offline entstanden und noch nicht beim Server.
    public var standOffen: Bool
    public let angelegt: Date

    public var id: String { fileId }

    public init(fileId: String, item: Item, serverId: String, accountId: String,
                dateiname: String, groesse: Int64, hoehe: Int?, angelegt: Date = Date()) {
        self.fileId = fileId
        self.item = item
        self.serverId = serverId
        self.accountId = accountId
        self.dateiname = dateiname
        self.groesse = groesse
        self.hoehe = hoehe
        self.zustand = .wartet
        self.geladen = 0
        self.fehler = nil
        self.lokalerName = nil
        self.stand = item.resumeSeconds
        self.dauer = item.runtimeSeconds
        self.standOffen = false
        self.angelegt = angelegt
    }

    /// 0 bis 1 — für den Balken.
    public var anteil: Double {
        if zustand == .fertig { return 1 }
        guard groesse > 0 else { return 0 }
        return min(1, max(0, Double(geladen) / Double(groesse)))
    }

    /// „S1 F3 · Titel" bei Folgen, sonst der Titel.
    public var anzeige: String {
        if item.kind == "episode", let s = item.seasonNo, let e = item.episodeNo {
            return String(localized: "S\(s) F\(e) · \(item.title)", bundle: .lumenKit, locale: .lumen)
        }
        return item.title
    }
}

/// Die Buchhaltung der Downloads — ohne Netz und ohne Dateien.
public struct DownloadKatalog: Codable, Sendable, Equatable {
    public private(set) var eintraege: [DownloadEintrag] = []

    public init(eintraege: [DownloadEintrag] = []) {
        self.eintraege = eintraege
    }

    public subscript(fileId: String) -> DownloadEintrag? {
        eintraege.first { $0.fileId == fileId }
    }

    /// Der Download einer Datei, wenn es ihn gibt — sonst der eines
    /// beliebigen Fassung desselben Titels.
    public func fuer(itemId: String) -> DownloadEintrag? {
        eintraege.first { $0.item.id == itemId && $0.zustand == .fertig }
            ?? eintraege.first { $0.item.id == itemId }
    }

    /// Einen Download aufnehmen. Dieselbe Datei zweimal gibt es nicht —
    /// dann bleibt der vorhandene, ausser er war gescheitert.
    @discardableResult
    public mutating func aufnehmen(_ neu: DownloadEintrag) -> Bool {
        if let i = eintraege.firstIndex(where: { $0.fileId == neu.fileId }) {
            guard eintraege[i].zustand == .fehler else { return false }
            eintraege[i] = neu
            return true
        }
        eintraege.append(neu)
        return true
    }

    public mutating func entfernen(_ fileId: String) {
        eintraege.removeAll { $0.fileId == fileId }
    }

    public mutating func fortschritt(_ fileId: String, geladen: Int64) {
        aendern(fileId) {
            $0.zustand = .laeuft
            $0.geladen = geladen
            $0.fehler = nil
        }
    }

    public mutating func fertig(_ fileId: String, lokalerName: String, groesse: Int64?) {
        aendern(fileId) {
            $0.zustand = .fertig
            $0.lokalerName = lokalerName
            if let groesse { $0.geladen = groesse }
            $0.fehler = nil
        }
    }

    public mutating func gescheitert(_ fileId: String, fehler: String) {
        aendern(fileId) {
            $0.zustand = .fehler
            $0.fehler = fehler
        }
    }

    /// Offline gespielt: der Stand bleibt hier, bis der Server erreichbar ist.
    public mutating func standOffline(_ itemId: String, position: Double, dauer: Double) {
        for i in eintraege.indices where eintraege[i].item.id == itemId {
            eintraege[i].stand = max(0, position)
            if dauer > 0 { eintraege[i].dauer = dauer }
            eintraege[i].standOffen = true
        }
    }

    /// Der Stand ist beim Server angekommen.
    public mutating func standGemeldet(_ itemId: String, position: Double) {
        for i in eintraege.indices where eintraege[i].item.id == itemId {
            // Inzwischen weitergeschaut? Dann bleibt der neuere Stand offen.
            if abs(eintraege[i].stand - position) < 1 { eintraege[i].standOffen = false }
        }
    }

    /// Was beim nächsten Kontakt mit diesem Server und Profil nachzureichen
    /// ist — je Titel einmal.
    public func offeneStaende(serverId: String, accountId: String) -> [DownloadEintrag] {
        var gesehen = Set<String>()
        return eintraege.filter {
            $0.standOffen && $0.serverId == serverId && $0.accountId == accountId
                && gesehen.insert($0.item.id).inserted
        }
    }

    /// Wie viel Platz die fertigen Downloads belegen.
    public var belegt: Int64 {
        eintraege.filter { $0.zustand == .fertig }.reduce(0) { $0 + max($1.geladen, $1.groesse) }
    }

    /// Wie viel die laufenden noch brauchen werden.
    public var nochNoetig: Int64 {
        eintraege.filter { $0.zustand == .wartet || $0.zustand == .laeuft }
            .reduce(0) { $0 + max(0, $1.groesse - $1.geladen) }
    }

    private mutating func aendern(_ fileId: String, _ f: (inout DownloadEintrag) -> Void) {
        guard let i = eintraege.firstIndex(where: { $0.fileId == fileId }) else { return }
        f(&eintraege[i])
    }

    /// Welche Datei ein Download nimmt, wenn es mehrere gibt und niemand
    /// gewählt hat: die mit der grössten Höhe, die noch aufs Gerät passt.
    /// Gibt es keine, die passt, nil.
    public static func vorschlag(_ dateien: [DownloadDatei], frei: Int64?) -> DownloadDatei? {
        let passend = dateien.filter { frei == nil || $0.groesse <= frei! }
        return passend.max { a, b in
            let ha = a.hoehe ?? 0, hb = b.hoehe ?? 0
            if ha != hb { return ha < hb }
            return a.groesse > b.groesse
        }
    }

    /// Eine Endung für die Datei auf dem Gerät — die des Servers, damit
    /// AVPlayer erkennt, was es ist.
    public static func lokalerName(fileId: String, dateiname: String) -> String {
        let endung = (dateiname as NSString).pathExtension.lowercased()
        let sauber = fileId.filter { $0.isLetter || $0.isNumber || $0 == "-" || $0 == "_" }
        return endung.isEmpty ? sauber : "\(sauber).\(endung)"
    }
}

// MARK: - Herunterladen

/// Lädt im Hintergrund, hält den Katalog und reicht den offline
/// entstandenen Stand nach.
///
/// Eine einzige Instanz für die App (`Downloads.shared`): die Hintergrund-
/// Sitzung des Systems gibt es pro Kennung nur einmal.
@MainActor
@Observable
public final class Downloads {

    public static let shared = Downloads()

    /// Die Kennung der Hintergrund-Sitzung. Startet das System die App
    /// neu, um fertige Downloads abzuliefern, findet es sie darüber wieder.
    public nonisolated static let sitzungsKennung = "org.lumen.downloads"

    public private(set) var katalog = DownloadKatalog()
    /// Eine Meldung, die die Ansicht einmal zeigen darf.
    public var meldung: String?

    @ObservationIgnored private var sitzung: URLSession?
    @ObservationIgnored private let empfaenger = DownloadEmpfaenger()
    @ObservationIgnored private var geladen = false

    private init() {}

    // MARK: Ordner

    /// Wo die Dateien liegen: im Programmordner der App, nicht in den
    /// Dokumenten — dort sähe man sie in der Dateien-App, und eine Sicherung
    /// nähme Gigabytes mit, die der Server ohnehin hat.
    public nonisolated static var ordner: URL {
        let basis = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask).first
            ?? FileManager.default.temporaryDirectory
        return basis.appendingPathComponent("Lumen", isDirectory: true)
            .appendingPathComponent("Downloads", isDirectory: true)
    }

    nonisolated static var katalogDatei: URL { ordner.appendingPathComponent("katalog.json") }

    public func lokaleDatei(_ eintrag: DownloadEintrag) -> URL? {
        guard eintrag.zustand == .fertig, let name = eintrag.lokalerName else { return nil }
        let url = Self.ordner.appendingPathComponent(name)
        return FileManager.default.fileExists(atPath: url.path) ? url : nil
    }

    /// Freier Platz auf dem Gerät, soweit das System es sagt.
    public nonisolated static func freierPlatz() -> Int64? {
        // Auf tvOS gibt es keine Downloads (und keinen „wichtigen" Platz).
        #if canImport(Darwin) && !os(tvOS)
        let url = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask).first
            ?? FileManager.default.temporaryDirectory
        if let werte = try? url.resourceValues(forKeys: [.volumeAvailableCapacityForImportantUsageKey]),
           let frei = werte.volumeAvailableCapacityForImportantUsage {
            return frei
        }
        #endif
        return nil
    }

    // MARK: Start

    /// Katalog lesen und die Hintergrund-Sitzung wieder aufnehmen. Das
    /// System liefert danach ab, was fertig wurde, während die App ruhte.
    public func vorbereiten() {
        guard !geladen else { return }
        geladen = true
        if let daten = try? Data(contentsOf: Self.katalogDatei),
           let k = try? JSONDecoder().decode(DownloadKatalog.self, from: daten) {
            katalog = k
        }
        // Was als fertig gilt, aber fehlt (von Hand gelöscht, Speicher
        // aufgeräumt), ist nicht mehr fertig.
        for e in katalog.eintraege where e.zustand == .fertig && lokaleDatei(e) == nil {
            katalog.gescheitert(e.fileId, fehler: String(localized: "Die Datei fehlt auf diesem Gerät.", bundle: .lumenKit, locale: .lumen))
        }
        empfaenger.downloads = self
        _ = sitzungHolen()
    }

    private func sitzungHolen() -> URLSession {
        if let sitzung { return sitzung }
        #if canImport(Darwin)
        let aufbau = URLSessionConfiguration.background(withIdentifier: Self.sitzungsKennung)
        #else
        // Nur für die Prüfung von LumenKit ausserhalb von Apple-Geräten.
        let aufbau = URLSessionConfiguration.default
        #endif
        #if os(iOS)
        // Grosse Dateien lieber dann, wenn das Gerät am Strom und im WLAN
        // hängt — aber nicht so lange warten, dass es nie losgeht.
        aufbau.isDiscretionary = false
        aufbau.sessionSendsLaunchEvents = true
        #endif
        aufbau.allowsCellularAccess = true
        let neu = URLSession(configuration: aufbau, delegate: empfaenger, delegateQueue: nil)
        sitzung = neu
        return neu
    }

    // MARK: Aufträge

    /// Einen Download beginnen. Gibt false zurück, wenn es ihn schon gibt.
    @discardableResult
    public func laden(item: Item, datei: DownloadDatei, client: APIClient,
                      serverId: String, accountId: String) async -> Bool {
        vorbereiten()
        if let frei = Self.freierPlatz(), datei.groesse > 0, datei.groesse + katalog.nochNoetig > frei {
            meldung = String(localized: "Auf diesem Gerät ist nicht genug Platz frei.", bundle: .lumenKit, locale: .lumen)
            return false
        }
        let eintrag = DownloadEintrag(fileId: datei.fileId, item: item, serverId: serverId,
                                      accountId: accountId, dateiname: datei.dateiname,
                                      groesse: datei.groesse, hoehe: datei.hoehe)
        guard katalog.aufnehmen(eintrag) else { return false }
        speichern()
        let anfrage = await client.downloadAnfrage(fileId: datei.fileId)
        let auftrag = sitzungHolen().downloadTask(with: anfrage)
        auftrag.taskDescription = datei.fileId
        auftrag.resume()
        return true
    }

    /// Abbrechen oder löschen — die Datei ist danach weg.
    public func loeschen(_ fileId: String) {
        vorbereiten()
        if let e = katalog[fileId], let name = e.lokalerName {
            try? FileManager.default.removeItem(at: Self.ordner.appendingPathComponent(name))
        }
        sitzung?.getAllTasks { auftraege in
            for a in auftraege where a.taskDescription == fileId { a.cancel() }
        }
        katalog.entfernen(fileId)
        speichern()
    }

    // MARK: Offline-Stand

    /// Was `Playback` offline meldet, landet hier.
    public func standMelden(itemId: String, position: Double, dauer: Double) {
        katalog.standOffline(itemId, position: position, dauer: dauer)
        speichern()
    }

    /// Der Stand für `Playback.startOffline`.
    public var offlineMelder: Playback.Offline {
        Playback.Offline { [weak self] itemId, position, dauer in
            self?.standMelden(itemId: itemId, position: position, dauer: dauer)
        }
    }

    /// Den offline entstandenen Stand an den Server — für dieses Profil
    /// auf diesem Server. Was nicht klappt, bleibt für das nächste Mal.
    public func nachreichen(client: APIClient, serverId: String, accountId: String) async {
        vorbereiten()
        for e in katalog.offeneStaende(serverId: serverId, accountId: accountId) {
            do {
                try await client.saveProgress(itemId: e.item.id, position: e.stand, duration: e.dauer)
                if e.dauer > 0, e.stand / e.dauer >= Playback.finishedAt {
                    try? await client.markFinished(e.item.id)
                }
                katalog.standGemeldet(e.item.id, position: e.stand)
            } catch {
                // Server weg oder Anmeldung ungültig: später nochmals.
                break
            }
        }
        speichern()
    }

    // MARK: Vom Empfänger

    fileprivate func fortschritt(_ fileId: String, geladen: Int64) {
        guard katalog[fileId] != nil else { return }
        katalog.fortschritt(fileId, geladen: geladen)
        // Nicht bei jedem Häppchen auf die Platte — der Balken reicht.
    }

    fileprivate func fertig(_ fileId: String, lokalerName: String, groesse: Int64?) {
        guard katalog[fileId] != nil else {
            // Inzwischen gelöscht: die Datei gehört niemandem.
            try? FileManager.default.removeItem(at: Self.ordner.appendingPathComponent(lokalerName))
            return
        }
        katalog.fertig(fileId, lokalerName: lokalerName, groesse: groesse)
        speichern()
    }

    fileprivate func gescheitert(_ fileId: String, fehler: String) {
        guard let e = katalog[fileId], e.zustand != .fertig else { return }
        katalog.gescheitert(fileId, fehler: fehler)
        speichern()
    }

    private func speichern() {
        let k = katalog
        let ziel = Self.katalogDatei
        let ordner = Self.ordner
        Task.detached(priority: .utility) {
            try? FileManager.default.createDirectory(at: ordner, withIntermediateDirectories: true)
            if let daten = try? JSONEncoder().encode(k) {
                try? daten.write(to: ziel, options: .atomic)
            }
        }
    }

    /// „7.9 GB" — wie überall in Lumen, in der Schreibweise der Sprache.
    public nonisolated static func groesseText(_ bytes: Int64) -> String {
        Fassungen.groesse(bytes) ?? "0 MB" // sprache: fest
    }
}

/// Der Empfänger der Hintergrund-Sitzung. Er läuft nicht auf dem
/// Hauptfaden; eine fertige Datei muss er sofort verschieben — das System
/// löscht sie, sobald diese Methode zurückkehrt.
final class DownloadEmpfaenger: NSObject, URLSessionDownloadDelegate, @unchecked Sendable {
    /// Gesetzt von `Downloads.vorbereiten`, vor dem ersten Auftrag.
    weak var downloads: Downloads?

    func urlSession(_ session: URLSession, downloadTask: URLSessionDownloadTask,
                    didFinishDownloadingTo location: URL) {
        guard let fileId = downloadTask.taskDescription else { return }
        let status = (downloadTask.response as? HTTPURLResponse)?.statusCode ?? 0
        guard (200...299).contains(status) else {
            let text = Self.fehlerText(status: status, datei: location)
            Task { @MainActor [ziel = downloads] in ziel?.gescheitert(fileId, fehler: text) }
            return
        }
        let name = DownloadKatalog.lokalerName(
            fileId: fileId,
            dateiname: downloadTask.response?.suggestedFilename ?? downloadTask.originalRequest?.url?.lastPathComponent ?? "")
        let ziel = Downloads.ordner.appendingPathComponent(name)
        do {
            try FileManager.default.createDirectory(at: Downloads.ordner, withIntermediateDirectories: true)
            try? FileManager.default.removeItem(at: ziel)
            try FileManager.default.moveItem(at: location, to: ziel)
            #if canImport(Darwin)
            var ziel2 = ziel
            var werte = URLResourceValues()
            werte.isExcludedFromBackup = true
            try? ziel2.setResourceValues(werte)
            #endif
            let groesse = (try? FileManager.default.attributesOfItem(atPath: ziel.path)[.size] as? NSNumber)?.int64Value
            Task { @MainActor [ziel = downloads] in ziel?.fertig(fileId, lokalerName: name, groesse: groesse) }
        } catch {
            let text = error.localizedDescription
            Task { @MainActor [ziel = downloads] in ziel?.gescheitert(fileId, fehler: text) }
        }
    }

    func urlSession(_ session: URLSession, downloadTask: URLSessionDownloadTask,
                    didWriteData bytesWritten: Int64, totalBytesWritten: Int64,
                    totalBytesExpectedToWrite: Int64) {
        guard let fileId = downloadTask.taskDescription else { return }
        Task { @MainActor [ziel = downloads] in ziel?.fortschritt(fileId, geladen: totalBytesWritten) }
    }

    func urlSession(_ session: URLSession, task: URLSessionTask, didCompleteWithError error: (any Error)?) {
        guard let error, let fileId = task.taskDescription else { return }
        // Abgebrochen heisst: gelöscht — dann gibt es nichts zu melden.
        if APIClient.istAbbruch(error) { return }
        let text = LumenError.unreachable(error).errorDescription ?? error.localizedDescription
        Task { @MainActor [ziel = downloads] in ziel?.gescheitert(fileId, fehler: text) }
    }

    /// Was der Server statt der Datei schickte: seine Meldung, übersetzt
    /// nach der festen Kennung, wenn es eine gibt.
    static func fehlerText(status: Int, datei: URL) -> String {
        struct Rumpf: Decodable { let error: String?; let code: String? }
        let rumpf = (try? Data(contentsOf: datei)).flatMap { try? JSONDecoder().decode(Rumpf.self, from: $0) }
        if let code = rumpf?.code, APIClient.bekannteCodes.contains(code) {
            return LumenError.abgelehnt(code: code, meldung: rumpf?.error ?? "", status: status)
                .errorDescription ?? ""
        }
        if let text = rumpf?.error, !text.isEmpty { return text }
        return String(localized: "Der Download hat nicht geklappt.", bundle: .lumenKit, locale: .lumen)
    }
}
