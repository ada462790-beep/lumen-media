// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

import Foundation

// Die Wege, die mit dieser Runde dazukamen: Profilschutz, „gesehen" für
// ganze Serien und Staffeln, Downloads, Vorschaubilder beim Spulen, das Ende
// einer HLS-Sitzung und was der Verwaltung am Mac noch fehlte.
//
// Ein Teil davon steht im Server erst, wenn die anderen Arbeiten dieser
// Runde zusammengeführt sind. Darum gilt überall: antwortet der Server mit
// 404 (`LumenError.fehltAufServer`), blendet die App die Funktion still aus.

extension APIClient {

    // MARK: - Profil schützen

    /// Die eigene Sperre ändern: `art` ist „pin", „password" oder „none";
    /// `alt` das bisherige Geheimnis (leer, wenn es keins gab). 403 mit
    /// `falsch` heisst: `alt` stimmt nicht; 409 mit
    /// `verwaltung_braucht_sperre`: wer verwalten darf, kann die Sperre
    /// nicht abschalten.
    public func sperreAendern(alt: String, art: String, neu: String) async throws {
        struct Body: Encodable { let alt: String, art: String, neu: String }
        _ = try await request("PUT", "/me/sperre", body: Body(alt: alt, art: art, neu: neu))
    }

    /// Die Sperre eines anderen Profils setzen oder zurücksetzen — für den
    /// Haushalt, wenn jemand seinen PIN vergessen hat. Verlangt die
    /// Bestätigung wie jede Rechteänderung, darum über `Session.verwaltet`.
    public func adminSperreSetzen(_ accountId: String, art: String, neu: String) async throws {
        struct Body: Encodable { let art: String, neu: String }
        _ = try await request("PUT", "/admin/accounts/\(accountId)/sperre", body: Body(art: art, neu: neu))
    }

    // MARK: - Gesehen

    /// Als gesehen markieren. Bei einer Serie alle Folgen, mit `staffel`
    /// nur die Folgen dieser Staffel.
    public func markFinished(_ id: String, staffel: Int?) async throws {
        var q: [String: String] = [:]
        if let staffel { q["staffel"] = String(staffel) }
        _ = try await request("POST", "/items/\(id)/finished", query: q)
    }

    /// Den Haken wieder wegnehmen — bei einer Serie für alle Folgen, mit
    /// `staffel` für die Folgen dieser Staffel.
    public func clearFinished(_ id: String, staffel: Int?) async throws {
        var q: [String: String] = [:]
        if let staffel { q["staffel"] = String(staffel) }
        _ = try await request("DELETE", "/items/\(id)/finished", query: q)
    }

    // MARK: - Downloads

    /// Ob dieses Profil den Titel herunterladen darf, und welche Dateien
    /// es dafür gibt.
    public func downloadInfo(itemId: String) async throws -> DownloadInfo {
        try await get("/items/\(itemId)/download/info")
    }

    /// Die Anfrage für den Download einer Datei — mit dem Token im Kopf,
    /// wie jede andere Anfrage. Für die Hintergrund-Übertragung, die die
    /// Anfrage selbst ausführt; das Token steht so nie in einer Adresse.
    public func downloadAnfrage(fileId: String) -> URLRequest {
        var req = URLRequest(url: adresse("/stream/\(fileId)/download"))
        if let token { req.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization") }
        return req
    }

    // MARK: - Vorschaubilder beim Spulen

    /// Die Vorschaubilder einer Datei. nil, solange der Server sie noch
    /// erzeugt (202) — oder wenn es keine gibt (404).
    public func trickplay(fileId: String) async throws -> Trickplay? {
        do {
            let data = try await request("GET", "/stream/\(fileId)/trickplay")
            // 202 hat keinen lesbaren Rumpf: dann ist noch nichts bereit.
            guard !data.isEmpty, let info = try? decode(Trickplay.self, from: data, path: "/stream/trickplay"),
                  info.brauchbar
            else { return nil }
            return info
        } catch let e as LumenError where e.status == 404 {
            return nil
        }
    }

    /// Ein Blatt mit Vorschaubildern (JPEG), mit dem Token im Kopf.
    public func trickplayBlatt(fileId: String, nr: Int) async throws -> Data {
        try await request("GET", "/stream/\(fileId)/trickplay/\(nr).jpg")
    }

    // MARK: - Wiedergabe beenden

    /// Die Umwandlung einer HLS-Sitzung sofort beenden, statt zu warten,
    /// bis der Server merkt, dass niemand mehr Segmente holt.
    public func hlsBeenden(fileId: String, sitzung: String) async throws {
        _ = try await request("DELETE", "/stream/\(fileId)/hls/\(sitzung)")
    }

    // MARK: - Verwaltung (nur am Mac)

    /// Den Namen setzen, unter dem die Apps diesen Server zeigen. Leer
    /// heisst: keiner. Höchstens 40 Zeichen.
    public func adminServerName(_ name: String) async throws {
        struct Body: Encodable { let name: String }
        _ = try await request("PUT", "/admin/server-name", body: Body(name: name))
    }

    /// Die Metadaten-Quellen. Die Schlüssel selbst kommen nie zurück —
    /// nur, ob einer eingerichtet ist.
    public func adminMetadatenQuellen() async throws -> MetadatenQuellen {
        try await get("/admin/metadata")
    }

    /// Quellen ändern. Was nil ist, bleibt; ein leerer Schlüssel entfernt
    /// ihn. Die Antwort ist der neue Stand.
    public func adminMetadatenQuellenSetzen(tmdbKey: String? = nil, omdbKey: String? = nil,
                                            sprache: String? = nil, region: String? = nil,
                                            imdb: Bool? = nil) async throws -> MetadatenQuellen {
        struct Body: Encodable {
            let tmdbKey: String?, omdbKey: String?, language: String?, region: String?, useImdb: Bool?
        }
        return try await send("PUT", "/admin/metadata", body: Body(
            tmdbKey: tmdbKey, omdbKey: omdbKey, language: sprache, region: region, useImdb: imdb))
    }

    /// Für alle Titel ohne Metadaten welche holen — im Hintergrund. Die
    /// Antwort sagt nur, ob es losging oder schon lief.
    public func adminMetadatenAufloesen(libraryId: String? = nil) async throws -> String {
        struct Antwort: Decodable { let status: String }
        var q: [String: String] = [:]
        if let libraryId, !libraryId.isEmpty { q["library"] = libraryId }
        let a: Antwort = try await send("POST", "/admin/metadata/resolve", query: q)
        return a.status
    }

    /// Titel, die Lumen nicht sicher erkannt hat.
    public func adminNichtErkannt(limit: Int = 100) async throws -> [Item] {
        try await get("/admin/metadata/unmatched", ["limit": String(limit)])
    }

    /// Der Fernzugriff: verbunden oder nicht, und der Fingerabdruck des
    /// Zertifikats, den die Apps von unterwegs brauchen.
    public func adminFernzugriff() async throws -> Fernzugriff {
        try await get("/admin/remote")
    }

    // MARK: - Hilfe

    /// Eine Adresse unter /api/v1, ohne Abfrage.
    func adresse(_ path: String) -> URL {
        baseURL.appendingPathComponent("api/v1").appendingPathComponent(
            path.hasPrefix("/") ? String(path.dropFirst()) : path)
    }
}

// MARK: - Modelle

/// GET /items/{id}/download/info
public struct DownloadInfo: Codable, Sendable, Equatable {
    public let erlaubt: Bool
    public let dateien: [DownloadDatei]

    public init(erlaubt: Bool, dateien: [DownloadDatei]) {
        self.erlaubt = erlaubt
        self.dateien = dateien
    }

    private enum CodingKeys: String, CodingKey { case erlaubt, dateien }

    /// Eigens geschrieben: fehlt `dateien` oder ist es null, ist die Liste
    /// leer — ein Server, der nichts anbietet, soll keinen Lesefehler geben.
    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        erlaubt = try c.decodeIfPresent(Bool.self, forKey: .erlaubt) ?? false
        dateien = try c.decodeIfPresent([DownloadDatei].self, forKey: .dateien) ?? []
    }
}

public struct DownloadDatei: Codable, Sendable, Hashable, Identifiable {
    public let fileId: String
    public let dateiname: String
    public let groesse: Int64
    public let hoehe: Int?

    public var id: String { fileId }

    public init(fileId: String, dateiname: String, groesse: Int64, hoehe: Int?) {
        self.fileId = fileId
        self.dateiname = dateiname
        self.groesse = groesse
        self.hoehe = hoehe
    }

    /// „1080p · MKV · 7,9 GB" — für die Wahl der Fassung.
    public var beschriftung: String {
        var teile: [String] = []
        if let a = Fassungen.aufloesung(hoehe) { teile.append(a) }
        let endung = (dateiname as NSString).pathExtension.uppercased()
        if !endung.isEmpty { teile.append(endung) }
        if let g = Fassungen.groesse(groesse) { teile.append(g) }
        if teile.isEmpty { return dateiname }
        return teile.joined(separator: " · ")
    }
}

/// GET /admin/metadata — welche Quellen eingerichtet sind.
public struct MetadatenQuellen: Codable, Sendable, Equatable {
    public struct Quelle: Codable, Sendable, Equatable {
        public let configured: Bool
        public let enabled: Bool?
        public let name: String?
        public let what: String?
    }
    public let tmdb: Quelle?
    public let imdb: Quelle?
    public let language: String?
    public let region: String?
    public let ready: Bool?
    public let note: String?
}

/// GET /admin/remote — der Zustand des Vermittlers für den Fernzugriff.
public struct Fernzugriff: Codable, Sendable, Equatable {
    public let an: Bool
    public let verbunden: Bool?
    public let vermittler: String?
    public let seit: Date?
    public let letzterFehler: String?
    public let versuche: Int?
    public let eigenerFingerabdruck: String?
    public let hinweis: String?
}

/// Eine laufende HLS-Sitzung des Servers. AVPlayer holt die Abspielliste
/// selbst; die Kennung steht in der Adresse der Medienliste
/// (`…/stream/{datei}/hls/{sitzung}/media.m3u8`).
public struct HLSSitzung: Sendable, Equatable {
    public let fileId: String
    public let kennung: String

    public init(fileId: String, kennung: String) {
        self.fileId = fileId
        self.kennung = kennung
    }

    /// Die Kennung aus einer Adresse — nil, wenn sie nicht zu dieser Datei
    /// gehört oder nur die Hauptliste ist.
    public static func kennung(in adresse: String, fileId: String) -> String? {
        let pfad = URLComponents(string: adresse)?.path ?? adresse
        let teile = pfad.split(separator: "/").map(String.init)
        guard let i = teile.firstIndex(of: "stream"), i + 3 < teile.count,
              teile[i + 1] == fileId, teile[i + 2] == "hls"
        else { return nil }
        let kennung = teile[i + 3]
        guard !kennung.isEmpty, !kennung.hasSuffix(".m3u8") else { return nil }
        return kennung
    }
}

/// Die Vorschaubilder einer Datei: Blätter mit `spalten` × `zeilen`
/// Kacheln, jede `breite` × `hoehe` Pixel, eine alle `intervall`.
public struct Trickplay: Codable, Sendable, Equatable {
    public let intervall: Double
    public let breite: Int
    public let hoehe: Int
    public let spalten: Int
    public let zeilen: Int
    public let bilder: Int
    public let blaetter: [String]

    public init(intervall: Double, breite: Int, hoehe: Int, spalten: Int, zeilen: Int,
                bilder: Int, blaetter: [String]) {
        self.intervall = intervall
        self.breite = breite
        self.hoehe = hoehe
        self.spalten = spalten
        self.zeilen = zeilen
        self.bilder = bilder
        self.blaetter = blaetter
    }

    /// Ob sich damit etwas zeigen lässt.
    public var brauchbar: Bool {
        intervallSekunden > 0 && breite > 0 && hoehe > 0 && spalten > 0 && zeilen > 0 && bilder > 0
    }

    /// Der Abstand in Sekunden. Der Vertrag nennt keine Einheit; Werte ab
    /// 100 sind sicher Millisekunden (niemand macht ein Bild alle zwei
    /// Minuten), kleinere Sekunden.
    public var intervallSekunden: Double {
        intervall >= 100 ? intervall / 1000 : intervall
    }

    /// Wo das Bild für diese Stelle liegt: welches Blatt, und darin die
    /// Kachel in Pixeln.
    public struct Kachel: Equatable, Sendable {
        public let blatt: Int
        public let x: Int
        public let y: Int
        public let breite: Int
        public let hoehe: Int
    }

    public func kachel(bei sekunden: Double) -> Kachel? {
        guard brauchbar, sekunden.isFinite else { return nil }
        let nr = min(bilder - 1, max(0, Int(sekunden / intervallSekunden)))
        let jeBlatt = spalten * zeilen
        let blatt = nr / jeBlatt
        let imBlatt = nr % jeBlatt
        return Kachel(blatt: blatt,
                      x: (imBlatt % spalten) * breite,
                      y: (imBlatt / spalten) * hoehe,
                      breite: breite, hoehe: hoehe)
    }
}
