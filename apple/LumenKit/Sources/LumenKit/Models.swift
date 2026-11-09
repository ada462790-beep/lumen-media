// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

import Foundation

// Die Datenmodelle. Jedes Feld heisst genau so wie im Server — geprüft wird
// das von tools/schnittstelle_pruefen.py, das die Go-Strukturen und diese
// Datei gegeneinanderhält. Ein Tippfehler hier wäre sonst erst im Betrieb
// aufgefallen, als leerer Bildschirm.

// MARK: - Konto

public struct Account: Codable, Identifiable, Hashable, Sendable {
    public let id: String
    public let name: String
    public let avatar: String
    public let isOwner: Bool
    public let isKid: Bool
    public let lockKind: String      // none | pin | password
    public let disabled: Bool
    public let createdAt: Date
    public let expiresAt: Date?
    /// Darf in die Verwaltung: der Besitzer immer, sonst über eine Gruppe
    /// mit „Verwaltung" (Issue #6). Dieselbe Grenze wie für den Besitzer:
    /// auch ein Mitverwalter sieht nicht, wer was schaut.
    ///
    /// Wahlfrei, damit die App mit einem älteren Server redet, der das
    /// Feld noch nicht schickt — dann zählt wie früher nur `isOwner`.
    public let isAdmin: Bool?
    /// Womit diese Person ihre Verwaltung schützt: none | pin | password
    /// | totp. Der Server nennt sie nur ihr selbst.
    public let adminLock: String?
    /// Die Gruppe, in der das Konto steckt, falls eine.
    public let groupId: String?
    public let groupName: String?
    /// Über die Gruppe dieselben Rechte wie der Besitzer („Vollzugriff").
    /// `isOwner` bleibt dem einen echten Besitzer vorbehalten.
    public let vollzugriff: Bool?

    public var needsCode: Bool { lockKind != "none" }
    public var initial: String { String(name.prefix(1)).uppercased() }
    /// Ob die Verwaltung gezeigt wird. Nicht `isOwner`: Mitverwalter
    /// dürfen dasselbe wie der Besitzer, bis auf Gruppen und Updates.
    public var darfVerwalten: Bool { isAdmin ?? isOwner }
    /// Ob die Verwaltung erst entsperrt werden muss.
    public var hatVerwaltungssperre: Bool {
        guard let adminLock else { return false }
        return adminLock != "none" && !adminLock.isEmpty
    }
}

/// Was vor der Anmeldung über ein Profil sichtbar ist: Name, Bild, ob ein
/// Code nötig ist. Mehr gibt der Server an dieser Stelle nicht heraus.
public struct ProfileCard: Codable, Identifiable, Hashable, Sendable {
    public let id: String
    public let name: String
    public let avatar: String
    public let lockKind: String
    public let isKid: Bool

    public var needsCode: Bool { lockKind != "none" }
    public var initial: String { String(name.prefix(1)).uppercased() }
}

public struct Limits: Codable, Hashable, Sendable {
    public let maxAge: Int
    public let allowUnrated: Bool
    public let maxStreams: Int
    public let allowTranscode: Bool
    public let allowDownload: Bool
    public let maxDownloads: Int
    public let remoteQuality: String

    /// Zum Bearbeiten in der Verwaltung. Ersetzt den eingebauten
    /// Initialisierer (der wäre nur innerhalb von LumenKit sichtbar).
    public init(maxAge: Int, allowUnrated: Bool, maxStreams: Int, allowTranscode: Bool,
                allowDownload: Bool, maxDownloads: Int, remoteQuality: String) {
        self.maxAge = maxAge
        self.allowUnrated = allowUnrated
        self.maxStreams = maxStreams
        self.allowTranscode = allowTranscode
        self.allowDownload = allowDownload
        self.maxDownloads = maxDownloads
        self.remoteQuality = remoteQuality
    }
}

extension Limits {
    /// Was der Server einem neuen Erwachsenen-Konto gibt (DefaultLimits).
    public static let vorgabe = Limits(maxAge: 18, allowUnrated: true, maxStreams: 2,
                                       allowTranscode: true, allowDownload: true,
                                       maxDownloads: 20, remoteQuality: "1080p")
}

public struct Me: Codable, Equatable, Sendable {
    public let account: Account
    public let limits: Limits
    public let prefs: [String: String]
    public let libraries: [String]
    /// Wie viele Neuigkeiten dieses Profil noch nicht gesehen hat — für
    /// den Punkt an „Neuigkeiten". Nur die eigenen; ein älterer Server
    /// schickt das Feld nicht.
    public let neuigkeiten: Int?
    /// Die Namen der eingebauten Figuren, die der Server als Profilbild
    /// annimmt („fuchs", „eule" …). Die Zeichnungen liegen in `Figuren`.
    public let figuren: [String]?
    /// Die Sprache, in der dieses Profil die Apps sieht („de-CH", „en-GB"
    /// …): die eigene Wahl, sonst die Vorgabe des Servers. Die eigene Wahl
    /// allein steht in `prefs["sprache"]` — leer heisst „wie der Server".
    /// Ein älterer Server schickt das Feld nicht.
    public let sprache: String?

    /// Die Zahl am Punkt, nie negativ.
    public var ungelesen: Int { max(0, neuigkeiten ?? 0) }
}

extension Me {
    private enum CodingKeys: String, CodingKey {
        case account, limits, prefs, libraries, neuigkeiten, figuren, sprache
    }

    /// Eigens geschrieben, weil der Server für ein Profil ohne eigene
    /// Bibliotheken `null` schicken kann — und `prefs` bei einem frischen
    /// Profil fehlen darf. Beides soll die Anmeldung nicht scheitern lassen.
    /// In einer Erweiterung, damit der gewöhnliche Initialisierer bleibt.
    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        self.init(
            account: try c.decode(Account.self, forKey: .account),
            limits: try c.decode(Limits.self, forKey: .limits),
            prefs: try c.decodeIfPresent([String: String].self, forKey: .prefs) ?? [:],
            libraries: try c.decodeIfPresent([String].self, forKey: .libraries) ?? [],
            neuigkeiten: try c.decodeIfPresent(Int.self, forKey: .neuigkeiten),
            figuren: try c.decodeIfPresent([String].self, forKey: .figuren),
            sprache: try c.decodeIfPresent(String.self, forKey: .sprache))
    }
}

public struct Hello: Codable, Sendable {
    public let server: String
    public let version: String
    public let setUp: Bool
    public let ffmpeg: Bool
    public let uptimeMs: Int64
    /// Lumen steht unter der AGPL. Deren §13 verlangt, dass wer den Dienst
    /// über das Netz benutzt, an den Quelltext kommt — darum nennt der Server
    /// beides hier, und die Einstellungen zeigen es an.
    ///
    /// Wahlfrei, damit die App auch mit einem älteren Server redet, der die
    /// zwei Felder noch nicht schickt.
    public let license: String?
    public let sourceUrl: String?
    /// Der Name, den der Besitzer dem Server gegeben hat. Fehlt, wenn er
    /// keinen gesetzt hat — oder bei einem älteren Server.
    public let name: String?
    /// Die Sprache, die der Server für alle Profile vorgibt, die keine
    /// eigene gewählt haben — und die Sprachen, die er kennt. Vor der
    /// Anmeldung spricht die App die Vorgabe. Ein älterer Server schickt
    /// beides nicht; dann gilt die Sprache des Geräts.
    public let sprache: String?
    public let sprachen: [SprachAngebot]?
}

public struct SignIn: Codable, Sendable {
    public let token: String
    public let account: Account
}

/// Der Medienschlüssel für Bild-, Video- und Untertiteladressen
/// (GET /me/media-key). Er gilt 6 Stunden nach der letzten Benutzung,
/// höchstens 48 Stunden, und nach einem Neustart des Servers nicht mehr.
public struct Medienschluessel: Codable, Sendable {
    public let key: String
    public let gueltigBis: Date?
    public let gueltigSek: Int
}

// MARK: - Mediathek

public struct Item: Codable, Identifiable, Hashable, Sendable {
    public let id: String
    public let libraryId: String
    public let kind: String          // movie | series | season | episode
    public let parentId: String?
    public let title: String
    public let originalTitle: String?
    public let year: Int?
    /// Bei Serien (Issue #13): das letzte Jahr und ob noch Folgen kommen.
    /// Fehlt `laeuftNoch`, weiss der Server es nicht — dann bleibt es beim
    /// blossen Startjahr, statt „seit 2014" über eine längst beendete
    /// Serie zu schreiben.
    public let endYear: Int?
    public let laeuftNoch: Bool?
    public let seasonNo: Int?
    public let episodeNo: Int?
    public let overview: String?
    public let ageRating: String?
    public let ageYears: Int?
    public let runtimeMs: Int64?
    public let premiered: String?
    public let imdbId: String?
    public let imdbRating: Double?
    public let imdbVotes: Int?
    /// Rotten Tomatoes: Anteil guter Kritiken in Prozent, wenn bekannt.
    public let rottenTomatoes: Int?
    public let matchState: String
    public let addedAt: Date
    public let genres: [String]?

    // Was dem angemeldeten Profil gehört. Der Server schickt das nur an
    // dieses eine Profil — niemand sonst sieht es, auch der Besitzer nicht.
    public let progress: Double?     // Prozent
    public let finished: Bool?
    public let myRating: Int?
    public let onList: Bool?
    /// Das Herz (Issue #10). Fehlt bei Go, wenn es nicht gesetzt ist.
    public let favorite: Bool?
    public let resumeMs: Int64?

    public let poster: String?       // Pfad auf dem Server, ohne Token
    public let backdrop: String?
    /// Ein Standbild aus dem Film selbst. Der Server schickt es nur, wenn
    /// Poster oder Hintergrund fehlen — als Ersatz, damit nicht ein leeres
    /// Rechteck dasteht, wo es ein Bild gäbe.
    public let frame: String?

    /// Wer einen Titel in eine geteilte Liste gelegt hat. Nur in den
    /// Einträgen einer Liste gesetzt, sonst immer nil.
    public let hinzugefuegtVon: String?

    /// Bei Serien: wie viele Folgen dieses Profil gesehen hat, von wie
    /// vielen. Nur die eigenen — der Server rechnet das je Konto, und
    /// niemand sonst bekommt es zu sehen.
    public let gesehen: Int?
    public let folgen: Int?

    /// Abzeichen auf der Karte: neu dazugekommen (sieben Tage), neue
    /// Folgen seit einer Woche, Qualität (SD/HD/4K), HDR-Art (HDR10, HLG,
    /// DV …) und die meisten Tonkanäle der besten Datei.
    public let neu: Bool?
    public let neueFolgen: Int?
    public let qualitaet: String?
    public let hdr: String?
    public let kanaele: Int?

    public var isSeen: Bool { finished == true }

    /// „🍅 93 % Rotten Tomatoes" — ab 60 % gilt ein Titel als frisch.
    public var tomatenText: String? {
        guard let p = rottenTomatoes else { return nil }
        return (p >= 60 ? "🍅" : "🟢") + " " + String(localized: "\(p) % Rotten Tomatoes", bundle: .lumenKit, locale: .lumen)
    }

    /// Das Abzeichen links oben: „Neu", „Neue Folge", „3 neue Folgen".
    /// Eine ganz neue Serie ist „Neu", nicht „12 neue Folgen" — wie im
    /// Browser (app.js: abzeichen).
    public var abzeichenNeu: String? {
        if neu == true { return String(localized: "Neu", bundle: .lumenKit, locale: .lumen) }
        // Einzahl und Mehrzahl („Neue Folge") stehen im Katalog.
        if let n = neueFolgen, n > 0 { return String(localized: "\(n) neue Folgen", bundle: .lumenKit, locale: .lumen) }
        return nil
    }

    /// Die Abzeichen rechts oben: 4K und die HDR-Art. HD und SD zeigt die
    /// Karte nicht — das ist der Normalfall und sagt nichts.
    public var abzeichenTechnik: [String] {
        var out: [String] = []
        if qualitaet == "4K" { out.append("4K") }
        if let name = Technik.hdrName(hdr) { out.append(name) }
        return out
    }

    /// „4 von 10 Folgen" oder „alle 10 Folgen gesehen" — die Zeile unter
    /// einer Serienkarte. nil, wenn es nichts zu sagen gibt: kein Stand,
    /// keine Folgen, kein Serien-Eintrag.
    public var folgenStand: String? {
        guard kind == "series", let folgen, folgen > 0, let gesehen, gesehen > 0 else { return nil }
        if gesehen >= folgen { return String(localized: "alle \(folgen) Folgen gesehen", bundle: .lumenKit, locale: .lumen) }
        return String(localized: "\(gesehen) von \(folgen) Folgen", bundle: .lumenKit, locale: .lumen)
    }

    /// Der Anteil gesehener Folgen in Prozent, für den Balken auf der
    /// Serienkarte. 0, wenn nichts bekannt ist.
    public var folgenAnteil: Double {
        guard kind == "series", let folgen, folgen > 0, let gesehen else { return 0 }
        return min(100, Double(gesehen) / Double(folgen) * 100)
    }
    public var isOnList: Bool { onList == true }
    public var isFavorite: Bool { favorite == true }

    /// Das Jahr, wie es unter einem Titel steht (Issue #13): bei Serien
    /// „seit 2014", solange sie läuft, „2014–2020", wenn sie beendet ist —
    /// sonst nur das Jahr. Dieselbe Regel wie im Browser (app.js: jahre).
    /// nil, wenn der Titel kein Jahr hat.
    public var jahresText: String? {
        guard let year, year > 0 else { return nil }
        guard kind == "series", let laeuftNoch else { return String(year) }
        if laeuftNoch { return String(localized: "seit \(String(year))", bundle: .lumenKit, locale: .lumen) }
        // Ein Halbgeviertstrich, kein Bindestrich: so schreibt man Spannen.
        if let endYear, endYear > 0, endYear != year { return "\(year)–\(endYear)" }
        return String(year)
    }

    public var percent: Double { progress ?? 0 }
    public var resumeSeconds: Double { Double(resumeMs ?? 0) / 1000 }
    public var runtimeSeconds: Double { Double(runtimeMs ?? 0) / 1000 }

    /// Welches Bild als Poster dient: das Poster, sonst das Standbild.
    ///
    /// Fehlt beides, bleibt es beim Poster — dann antwortet der Server mit
    /// nichts, und `Artwork` zeigt den Titel. Ein älterer Server ohne
    /// `frame` verhält sich damit genau wie bisher.
    public var posterArt: String {
        if poster == nil, frame != nil { return "frame" }
        return "poster"
    }

    /// Welches Bild in die Breite geht: Hintergrund, Standbild, Poster.
    /// Ein Standbild ist breit wie der Film und passt dorthin besser als
    /// ein hochkant beschnittenes Poster.
    public var wideArt: String {
        if backdrop != nil { return "backdrop" }
        if frame != nil { return "frame" }
        return "poster"
    }

    /// „2019 · 1 Std. 58 Min. · Drama“ — die Zeile unter dem Titel.
    public var subtitleLine: String {
        var parts: [String] = []
        if let jahr = jahresText { parts.append(jahr) }
        if let runtimeMs, runtimeMs > 0 { parts.append(Self.duration(ms: runtimeMs)) }
        if let ageYears { parts.append(String(localized: "ab \(ageYears)", bundle: .lumenKit, locale: .lumen)) }
        if let genres, !genres.isEmpty { parts.append(genres.prefix(2).joined(separator: " · ")) }
        return parts.joined(separator: " · ")
    }

    public static func duration(ms: Int64) -> String {
        let minutes = Int((Double(ms) / 60000).rounded())
        if minutes >= 60 {
            let stunden = minutes / 60, rest = minutes % 60
            return String(localized: "\(stunden) Std. \(rest) Min.", bundle: .lumenKit, locale: .lumen)
        }
        return String(localized: "\(minutes) Min.", bundle: .lumenKit, locale: .lumen)
    }
}

public struct MediaFile: Codable, Identifiable, Hashable, Sendable {
    public let id: String
    public let itemId: String
    public let path: String
    public let sizeBytes: Int64
    public let partNo: Int
    public let edition: String?
    public let container: String?
    public let durationMs: Int64?
    public let bitrate: Int?
    public let width: Int?
    public let height: Int?
    public let videoCodec: String?
    public let hdr: String?
    public let missing: Bool?
    /// Die Fassung, die jemand in der Verwaltung gewählt hat (Issue #8).
    /// Fehlt, wenn keine gewählt ist — dann nimmt Lumen die grösste.
    public let preferred: Bool?
    /// Tonspuren und Untertitel, Kapitel — gefüllt, sobald der Server die
    /// Datei ausgemessen hat. Ein älterer Server schickt nichts davon.
    public let tracks: [Track]?
    public let chapters: [Chapter]?

    public var isMissing: Bool { missing == true }
    public var isPreferred: Bool { preferred == true }

    /// „1080p · MKV · 4,2 GB" — Auflösung, Format, Grösse. Für das
    /// Aufräumen, wo man Fassungen nebeneinander vergleicht; der Codec
    /// hilft dort weniger als das Format.
    public var formatZeile: String {
        var parts: [String] = []
        if let height, height > 0 { parts.append("\(height)p") }
        if let container, !container.isEmpty { parts.append(container.uppercased()) }
        if sizeBytes > 0 { parts.append(Self.groesse(sizeBytes)) }
        return parts.joined(separator: " · ")
    }

    /// „4.2 GB" (in Deutschland „4,2 GB"), unter einem GB „830 MB".
    public static func groesse(_ bytes: Int64) -> String {
        let gb = Double(bytes) / 1_000_000_000
        if gb < 1 { return Sprache.zahl(Int((Double(bytes) / 1_000_000).rounded())) + " MB" }
        return gb.formatted(.number.precision(.fractionLength(1)).locale(.lumen)) + " GB"
    }
    public var durationSeconds: Double { Double(durationMs ?? 0) / 1000 }

    /// Die Tonspuren, in der Reihenfolge der Datei.
    public var tonspuren: [Track] { (tracks ?? []).filter { $0.kind == "audio" } }
    /// Die Untertitel, die sich als Text ausliefern lassen. Bild-Untertitel
    /// (PGS, DVD) bleiben weg — die kann der Server nur einbrennen.
    public var textspuren: [Track] { (tracks ?? []).filter { $0.kind == "subtitle" && !$0.isBitmap } }

    /// „1080p · h264 · 4.2 GB“
    public var techLine: String {
        var parts: [String] = []
        if let height, height > 0 { parts.append("\(height)p") }
        if let videoCodec, !videoCodec.isEmpty { parts.append(videoCodec) }
        if let hdr, !hdr.isEmpty { parts.append(hdr) }
        if sizeBytes > 0 { parts.append(Self.groesse(sizeBytes)) }
        return parts.joined(separator: " · ")
    }
}

/// Eine Spur in einer Datei: Ton oder Untertitel. `index` ist die Nummer,
/// mit der der Server sie anspricht — `?spur=` am Strom für den Ton, der
/// Pfad `/subtitles/{index}.vtt` für Untertitel. Externe Untertitel
/// (.srt neben dem Film) zählen ab 1000.
public struct Track: Codable, Identifiable, Hashable, Sendable {
    public let index: Int
    public let kind: String          // audio | subtitle
    public let codec: String?
    public let language: String?     // zwei Buchstaben, oder „und"
    public let title: String?
    public let channels: Int?
    public let forced: Bool?
    public let hearingImpaired: Bool?
    public let external: Bool?
    public let bitmap: Bool?

    public var id: Int { index }
    public var isAudio: Bool { kind == "audio" }
    public var isSubtitle: Bool { kind == "subtitle" }
    public var isForced: Bool { forced == true }
    public var isHearingImpaired: Bool { hearingImpaired == true }
    public var isExternal: Bool { external == true }
    public var isBitmap: Bool { bitmap == true }

    /// Die Sprache als Kürzel, nie leer: „de", „en", sonst „und".
    public var sprache: String {
        guard let language, !language.isEmpty else { return "und" }
        return language
    }

    /// „Deutsch (5.1)", „Englisch (erzwungen)", „Unbekannt (Datei)" —
    /// derselbe Name wie im Browser und in der HLS-Liste des Servers.
    public var name: String { Spuren.name(self) }
}

/// Ein Kapitel in einer Datei. `kind` sagt, ob es der Vorspann oder der
/// Abspann ist; der Server leitet daraus `intro` und `creditsMs` ab.
public struct Chapter: Codable, Identifiable, Hashable, Sendable {
    public let index: Int
    public let startMs: Int64
    public let endMs: Int64
    public let title: String?
    public let kind: String          // intro | credits | normal

    public var id: Int { index }
}

/// Wo der Vorspann einer Folge liegt. `quelle` ist „kapitel", wenn die
/// Datei es selbst sagt, „serie", wenn jemand mit Zugang zur Verwaltung
/// ihn für die ganze Serie markiert hat.
public struct Vorspann: Codable, Hashable, Sendable {
    public let vonMs: Int64
    public let bisMs: Int64
    public let quelle: String

    public var von: Double { Double(vonMs) / 1000 }
    public var bis: Double { Double(bisMs) / 1000 }
    public var vonSerie: Bool { quelle == "serie" }

    /// Liegt diese Stelle im Vorspann? Mit etwas Luft an beiden Enden,
    /// damit der Knopf nicht schon in der ersten Sekunde aufspringt und
    /// nicht nach dem Sprung noch einmal.
    public func enthaelt(_ sekunden: Double) -> Bool {
        sekunden >= von + 0.5 && sekunden < bis - 1
    }
}

public struct SharedRating: Codable, Hashable, Sendable {
    public let name: String
    public let avatar: String
    public let stars: Int
}

public struct ItemDetail: Codable, Sendable {
    public let item: Item
    public let files: [MediaFile]
    public let sharedRatings: [SharedRating]?
    public let seasons: [Item]?
    /// Die Filmreihe, zu der ein Film gehört. Nur bei Filmen, und nur
    /// wenn das Profil mindestens zwei Filme daraus sehen darf.
    public let collection: Reihe?
    /// Bei Folgen (und Filmen mit Kapiteln): wo der Vorspann liegt, wo der
    /// Abspann beginnt, zu welcher Serie die Folge gehört.
    public let intro: Vorspann?
    public let creditsMs: Int64?
    public let seriesId: String?
    /// Die Besetzung, mit Bildern über die eigene Bilderroute des Servers.
    public let cast: [Mitwirkender]?
    /// Titel, die zu diesem passen: dieselbe Reihe, geteilte Besetzung,
    /// gleiche Genres — nur aus dem, was dieses Profil sehen darf.
    public let similar: [Item]?

    /// Die Datei, die sich abspielen lässt — fehlende übersprungen.
    public var playable: MediaFile? { files.first { !$0.isMissing } }
}

/// Jemand aus der Besetzung eines Titels. `bild` ist ein Pfad wie
/// `/api/v1/art/{id}/cast03`, ohne Token.
public struct Mitwirkender: Codable, Identifiable, Hashable, Sendable {
    public let name: String
    public let rolle: String?
    public let bild: String?

    public var id: String { name }
    public var initial: String { String(name.prefix(1)).uppercased() }
    /// Die Bildart für die Bilderroute — „cast03" aus dem Pfad.
    public var bildArt: String? {
        guard let bild, let letzte = bild.split(separator: "/").last, !letzte.isEmpty else { return nil }
        return String(letzte)
    }
}

/// Die Startseite: Reihen, wie der Server sie zusammenstellt. Die App
/// kennt keinen Schlüssel fest — jede Reihe, die kommt, wird gezeigt
/// („Weiterschauen", „Noch nicht angefangen", je ein Genre, „Zufällig" …).
public struct HomeRow: Codable, Identifiable, Sendable {
    public let key: String
    public let title: String
    public let wide: Bool?
    public let items: [Item]

    public var id: String { key }
    public var isWide: Bool { wide == true }
}

public struct Home: Codable, Sendable {
    public let rows: [HomeRow]
}

// MARK: - Neuigkeiten

/// Eine Neuigkeit: neue Folgen einer Serie, die ich schaue oder gemerkt
/// habe, ein neuer Film in einer Reihe, aus der ich etwas gesehen habe,
/// oder was in der Woche dazukam. Alles aus dem eigenen Stand — niemand
/// sonst bekommt diese Liste zu sehen.
public struct Neuigkeit: Codable, Identifiable, Hashable, Sendable {
    public let art: String           // folgen | reihe | neu
    public let itemId: String?
    public let titel: String
    public let text: String
    public let at: Date
    public let anzahl: Int?

    public var id: String { "\(art)-\(itemId ?? "")-\(at.timeIntervalSince1970)" } // sprache: fest

    /// Ist sie jünger als das letzte Öffnen der Liste?
    public func istNeu(gelesenBis: Date?) -> Bool {
        guard let gelesenBis else { return true }
        return at > gelesenBis
    }

    /// Das Symbol dazu — Fernseher, Stapel, Film.
    public var symbol: String {
        switch art {
        case "folgen": "tv"
        case "reihe": "square.stack"
        default: "film"
        }
    }
}

/// Die Antwort auf GET /news.
public struct Neuigkeiten: Codable, Sendable {
    public let eintraege: [Neuigkeit]
    public let ungelesen: Int
    /// Bis wann alles gelesen ist. Wer die Liste nie geöffnet hat, bekommt
    /// vom Server die Nullzeit von Go („0001-01-01T00:00:00Z") — die gilt
    /// hier als „nie", und ein unlesbarer Wert ebenso, statt die ganze
    /// Antwort scheitern zu lassen.
    public let gelesenBis: Date?

    private enum CodingKeys: String, CodingKey { case eintraege, ungelesen, gelesenBis }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        eintraege = try c.decodeIfPresent([Neuigkeit].self, forKey: .eintraege) ?? []
        ungelesen = try c.decodeIfPresent(Int.self, forKey: .ungelesen) ?? 0
        let bis = (try? c.decodeIfPresent(Date.self, forKey: .gelesenBis)) ?? nil
        // Vor 1971 heisst: die Nullzeit, also nie.
        gelesenBis = bis.flatMap { $0.timeIntervalSince1970 > 0 ? $0 : nil }
    }
}

// MARK: - Suche

/// Jemand aus der Besetzung, auf den die Suche passt — als Sprungbrett zu
/// „alles mit …". `bild` ist ein Pfad auf dem Server, ohne Token.
public struct PersonTreffer: Codable, Identifiable, Hashable, Sendable {
    public let name: String
    public let anzahl: Int
    public let itemId: String?
    public let bild: String?

    public var id: String { name }
    public var initial: String { String(name.prefix(1)).uppercased() }
    /// „3 Titel" — Filme und Serien gemischt.
    public var anzahlText: String { String(localized: "\(anzahl) Titel", bundle: .lumenKit, locale: .lumen) }
}

/// Woher die Angaben über eine Person stammen — „TMDb", „Wikipedia".
public struct PersonQuelle: Codable, Hashable, Sendable {
    public let name: String
    public let url: String

    /// Die Adresse, aber nur, wenn sie eine Web-Adresse ist. Was hier steht,
    /// kommt aus der Datenbank des Servers; ein „javascript:" oder eine
    /// fremde App-Adresse soll daraus kein Knopf werden — wie `nurHttp`
    /// im Browser.
    public var link: URL? { Session.sourceLink(url) }
}

/// Die Personenseite (Issue #9, GET /persons/{name}): Foto, Lebensdaten,
/// Biografie, Quellen. Alles ausser dem Namen kann fehlen — kennt TMDb die
/// Person nicht, bleibt es beim Namen und ihren Titeln.
///
/// `bild` ist ein Pfad wie `/api/v1/persons/Tom%20Hanks/bild`, ohne
/// Schlüssel; die Adresse dazu baut `APIClient.personBildURL`.
public struct PersonInfo: Codable, Hashable, Sendable {
    public let name: String
    public let bio: String?
    /// Die Sprache der Biografie — „de", sonst meist „en".
    public let bioSprache: String?
    /// JJJJ-MM-TT, so wie TMDb es schreibt.
    public let geboren: String?
    public let gestorben: String?
    public let geburtsort: String?
    public let quellen: [PersonQuelle]?
    public let bild: String?

    /// Die Biografie, falls es eine gibt — ohne Leerzeilen an den Enden.
    public var biografie: String? {
        guard let text = bio?.trimmingCharacters(in: .whitespacesAndNewlines), !text.isEmpty else { return nil }
        return text
    }

    /// Steht die Biografie nicht auf Deutsch? Dann sagt die Seite es, statt
    /// stillschweigend Englisch zu zeigen.
    public var bioNichtDeutsch: Bool {
        guard biografie != nil, let sprache = bioSprache, !sprache.isEmpty else { return false }
        return sprache.lowercased() != "de"
    }

    /// „geboren 23.12.1968 in Queens · gestorben 1.2.2020" — leer, wenn
    /// nichts bekannt ist.
    public var lebensdaten: String {
        var teile: [String] = []
        if let geboren, !geboren.isEmpty {
            var text = String(localized: "geboren \(Self.datum(geboren))", bundle: .lumenKit, locale: .lumen)
            if let ort = geburtsort, !ort.isEmpty { text = String(localized: "geboren \(Self.datum(geboren)) in \(ort)", bundle: .lumenKit, locale: .lumen) }
            teile.append(text)
        }
        if let gestorben, !gestorben.isEmpty { teile.append(String(localized: "gestorben \(Self.datum(gestorben))", bundle: .lumenKit, locale: .lumen)) }
        return teile.joined(separator: " · ")
    }

    /// „TMDb, Wikipedia" — die Namen der Quellen.
    public var quellenText: String {
        (quellen ?? []).map(\.name).filter { !$0.isEmpty }.joined(separator: ", ")
    }

    /// „1968-12-23" → „23.12.1968", wie im Browser ohne führende Null —
    /// auf Englisch „12/23/1968" bzw. „23/12/1968". Nur ein Jahr bleibt
    /// ein Jahr; Unlesbares bleibt, wie es kam.
    public static func datum(_ text: String) -> String {
        let t = text.prefix(10).split(separator: "-")
        if t.count == 3, let j = Int(t[0]), let m = Int(t[1]), let d = Int(t[2]) {
            return Sprache.datum(jahr: j, monat: m, tag: d)
        }
        return text
    }
}

/// Wie der Server die Eingabe gedeutet hat: „Komödie 90er" wird zu Genre
/// Komödie und den Jahren 1990 bis 1999, „Filme mit Tom Hanks" zu einer
/// Person. Was übrig bleibt, steht in `text`.
public struct Deutung: Codable, Hashable, Sendable {
    public let text: String?
    public let genre: String?
    public let person: String?
    public let jahrVon: Int?
    public let jahrBis: Int?

    /// Ob mehr herauskam als blosser Text.
    public var gedeutet: Bool { person != nil || genre != nil || jahrVon != nil }

    /// „mit Tom Hanks · Komödie · 1990–1999" — die Zeile über den Treffern.
    public var beschreibung: String {
        var teile: [String] = []
        if let person, !person.isEmpty { teile.append(String(localized: "mit \(person)", bundle: .lumenKit, locale: .lumen)) }
        if let genre, !genre.isEmpty { teile.append(genre) }
        if let jahrVon {
            if let jahrBis, jahrBis != jahrVon { teile.append("\(jahrVon)–\(jahrBis)") }
            else { teile.append(String(jahrVon)) }
        }
        if let text, !text.isEmpty { teile.append("„\(text)“") }
        return teile.joined(separator: " · ")
    }
}

/// Die Antwort auf GET /search: Titel, dazu passende Personen und Genres,
/// und wie die Eingabe gedeutet wurde. Personen, Genres und Deutung fehlen
/// bei leerer Eingabe und bei einem älteren Server.
public struct Suchergebnis: Codable, Sendable {
    public let items: [Item]
    public let personen: [PersonTreffer]?
    public let genres: [String]?
    public let deutung: Deutung?

    public var leer: Bool {
        items.isEmpty && (personen ?? []).isEmpty && (genres ?? []).isEmpty
    }

    /// „12 Treffer · mit Tom Hanks · 1990–1999"
    public var trefferzeile: String {
        var zeile = String(localized: "\(items.count) Treffer", bundle: .lumenKit, locale: .lumen)
        if let deutung, deutung.gedeutet { zeile += " · " + deutung.beschreibung }
        return zeile
    }
}

// MARK: - Raster

/// Eine Seite des Rasters „Filme" oder „Serien" (GET /items?seite=1):
/// die Titel ab `offset`, wie viele es insgesamt sind, ob es weitergeht,
/// und die Genres aller Titel dieser Art — für die Filterknöpfe. Im
/// Server ist die Antwort eine map, darum ohne Gegenstück im Prüfer.
public struct ItemSeite: Codable, Sendable {
    public let items: [Item]
    public let gesamt: Int
    public let offset: Int
    public let weiter: Bool
    /// Go schickt `null`, wenn kein Titel ein Genre hat.
    public let genres: [String]?
}

// MARK: - Filmreihen

/// Eine Filmreihe: Filme, die zusammengehören, in ihrer Reihenfolge.
///
/// `source` sagt, woher die Gruppierung stammt — „tmdb", wenn die
/// Metadaten sie kennen, „auto", wenn Lumen sie selbst erkannt hat.
public struct Reihe: Codable, Identifiable, Hashable, Sendable {
    public let id: String
    public let name: String
    public let overview: String?
    public let source: String
    public let hidden: Bool?
    public let anzahl: Int
    /// Die Bilder der Reihe selbst (von TMDb), als Pfad auf dem Server.
    /// Fehlen sie, nehmen die Apps das Bild des ersten Films —
    /// `APIClient.reiheBildURL`.
    public let poster: String?
    public let backdrop: String?
    public let items: [Item]

    public var isHidden: Bool { hidden == true }

    /// „3 Filme" — gezählt wird, was dieses Profil sehen darf.
    public var anzahlText: String { String(localized: "\(anzahl) Filme", bundle: .lumenKit, locale: .lumen) }
}

// MARK: - Listen

/// Wer eine Liste angelegt hat. Mehr als Name und Bild gibt es nicht —
/// eine Liste erzählt nichts darüber, was diese Person schaut.
public struct ListenPerson: Codable, Identifiable, Hashable, Sendable {
    public let id: String
    public let name: String
    public let avatar: String

    public var initial: String { String(name.prefix(1)).uppercased() }
}

/// Eine eigene Liste. Sie gehört der Person, die sie angelegt hat; ob der
/// Haushalt sie sieht (`geteilt`) und ob andere mitschreiben dürfen
/// (`gemeinsam`), entscheidet allein sie.
///
/// `enthaelt` steht nur da, wenn nach einem Titel gefragt wurde und er
/// drin ist. `vorschau` kommt mit der Übersicht, `items` nur mit einer
/// einzelnen Liste.
public struct Liste: Codable, Identifiable, Hashable, Sendable {
    public let id: String
    public let name: String
    public let von: ListenPerson
    public let meine: Bool
    public let geteilt: Bool
    public let gemeinsam: Bool
    public let darfAendern: Bool
    public let anzahl: Int
    public let erstelltAm: Date
    public let enthaelt: Bool?
    public let vorschau: [Item]?
    public let items: [Item]?

    public var hasItem: Bool { enthaelt == true }

    /// „5 Titel" — Filme und Serien gemischt, darum nicht „Filme".
    public var anzahlText: String { String(localized: "\(anzahl) Titel", bundle: .lumenKit, locale: .lumen) }
}

// MARK: - Wiedergabe

public struct PlaybackPlan: Codable, Sendable {
    public let direct: Bool
    public let reason: String?
    public let videoCodec: String
    public let audioCodec: String
    public let height: Int
    public let bitrate: Int
}

public struct PlanResponse: Codable, Sendable {
    public let plan: PlaybackPlan
    public let file: MediaFile
}

public struct SeekPoint: Codable, Sendable {
    public let requested: Double
    public let start: Double
    public let mode: String
}

// MARK: - Wünsche

public struct Wish: Codable, Identifiable, Hashable, Sendable {
    public let id: String
    public let by: String
    public let avatar: String
    public let title: String
    public let year: Int?
    public let state: String         // open | working | done | declined
    public let note: String?
    public let createdAt: Date
    public let mine: Bool

    public var stateLabel: String {
        switch state {
        case "open": String(localized: "offen", bundle: .lumenKit, locale: .lumen)
        case "working": String(localized: "in Arbeit", bundle: .lumenKit, locale: .lumen)
        case "done": String(localized: "erfüllt", bundle: .lumenKit, locale: .lumen)
        case "declined": String(localized: "abgelehnt", bundle: .lumenKit, locale: .lumen)
        default: state
        }
    }
}

// MARK: - Verwaltungssperre

/// Wie es um die eigene Verwaltungssperre steht (GET /admin/lock).
public struct Verwaltungssperre: Codable, Hashable, Sendable {
    public let sperre: String        // none | pin | password | totp
    public let entsperrt: Bool

    public var aktiv: Bool { sperre != "none" && !sperre.isEmpty }

    /// „PIN", „Passwort", „Authenticator-App", „keine" — für Listen und
    /// Dialoge an einer Stelle, damit es überall gleich heisst.
    public static func name(_ sperre: String) -> String {
        switch sperre {
        case "pin": String(localized: "PIN", bundle: .lumenKit, locale: .lumen)
        case "password": String(localized: "Passwort", bundle: .lumenKit, locale: .lumen)
        case "totp": String(localized: "Authenticator-App", bundle: .lumenKit, locale: .lumen)
        default: String(localized: "keine", bundle: .lumenKit, locale: .lumen)
        }
    }
}

/// Die Antwort auf das Entsperren (POST /admin/unlock) und auf das Setzen
/// der Sperre (PUT /admin/lock): ein Token, das die nächsten Verwaltungs-
/// Anfragen als Kopfzeile `X-Lumen-Admin` mitnehmen. Es lebt nur im
/// Speicher — nach dem Beenden der App ist die Verwaltung wieder zu.
public struct Entsperrung: Codable, Hashable, Sendable {
    public let token: String
    /// Wie lange das Token ohne Verwaltung gilt — nur beim Entsperren.
    public let gueltigMin: Int?
    /// Die eben gesetzte Sperre — nur beim Setzen.
    public let sperre: String?
}

/// Der Anlauf zu einer Authenticator-App (POST /admin/lock/totp): das
/// Geheimnis als QR-Code und zum Abtippen. Gespeichert wird es erst, wenn
/// ein Code daraus stimmt.
public struct TOTPEinrichtung: Codable, Hashable, Sendable {
    public let geheimnis: String
    /// `data:image/png;base64,…` — ein fertiges Bild.
    public let qr: String
    /// Welche Apps gehen.
    public let app: String

    /// Das PNG aus der Daten-Adresse, zum Anzeigen.
    public var qrPNG: Data? {
        guard let komma = qr.firstIndex(of: ",") else { return nil }
        return Data(base64Encoded: String(qr[qr.index(after: komma)...]))
    }
}

// MARK: - Verwaltung (Besitzer und Mitverwalter)

public struct Library: Codable, Identifiable, Hashable, Sendable {
    public let id: String
    public let name: String
    public let kind: String
    public let path: String
    public let language: String
    public let scannedAt: Date?
    public let count: Int
    /// Läuft gerade ein Durchlauf — und wie weit ist er.
    public let scanning: Bool?
    public let stand: ScanFortschritt?
    /// Liegt der Ordner noch da? Eine Platte, die nicht eingehängt ist,
    /// sieht sonst aus wie eine leere Bibliothek.
    public let erreichbar: Bool?
    public let problem: String?
    /// Alle wie viele Minuten Lumen von selbst nachsieht; 0 = nur auf
    /// Knopfdruck. Steht `scanAt`, gilt stattdessen täglich diese Uhrzeit.
    public let intervalMin: Int?
    /// „HH:MM" — täglich zu dieser Zeit nachsehen. Leer heisst: nach Takt.
    public let scanAt: String?
    /// Was der letzte Durchlauf ergab.
    public let lastResult: ScanErgebnis?
    /// Werden gerade Beschreibungen und Bilder geholt — und wie weit.
    public let metadaten: MetadatenStand?
    /// Angenommen, wartet aber, bis eine andere Arbeit fertig ist.
    public let wartet: Bool?

    /// Läuft hier gerade irgendetwas — Durchlauf, Warten oder Metadaten?
    /// Dann fragt die Verwaltung im Takt nach.
    public var beschaeftigt: Bool {
        stand?.laeuft == true || wartet == true || metadaten?.laeuft == true
    }

    /// Die Wahl im Menü „Von selbst nachsehen": die Minuten als Text, oder
    /// „zeit" für täglich zu einer festen Uhrzeit — wie im Browser.
    public var taktWahl: String {
        if let scanAt, !scanAt.isEmpty { return "zeit" }
        return String(intervalMin ?? 0)
    }
}

/// Was ein Durchlauf hinterlassen hat (store.ScanErgebnis).
public struct ScanErgebnis: Codable, Hashable, Sendable {
    public let at: Date
    public let added: Int
    public let updated: Int
    public let missing: Int
    public let unmatched: Int
    public let tookMs: Int64
    /// Von selbst, nicht auf Knopfdruck.
    public let auto: Bool

    /// „von selbst: 3 neu, 1 fehlt" oder „auf Knopfdruck: nichts Neues" —
    /// derselbe Satz wie im Browser.
    public var text: String {
        var teile: [String] = []
        if added > 0 { teile.append(String(localized: "\(added) neu", bundle: .lumenKit, locale: .lumen)) }
        if updated > 0 { teile.append(String(localized: "\(updated) geändert", bundle: .lumenKit, locale: .lumen)) }
        if missing > 0 { teile.append(String(localized: "\(missing) fehlen", bundle: .lumenKit, locale: .lumen)) }
        if unmatched > 0 { teile.append(String(localized: "\(unmatched) nicht erkannt", bundle: .lumenKit, locale: .lumen)) }
        let was = teile.isEmpty ? String(localized: "nichts Neues", bundle: .lumenKit, locale: .lumen) : teile.joined(separator: ", ")
        return auto ? String(localized: "von selbst: \(was)", bundle: .lumenKit, locale: .lumen) : String(localized: "auf Knopfdruck: \(was)", bundle: .lumenKit, locale: .lumen)
    }
}

/// Wie weit das Holen der Metadaten ist (meta.Stand).
public struct MetadatenStand: Codable, Hashable, Sendable {
    public let laeuft: Bool
    public let library: String?
    public let gesamt: Int
    public let fertig: Int
    public let titel: String?

    public var anteil: Double {
        guard gesamt > 0 else { return 0 }
        return min(1, Double(fertig) / Double(gesamt))
    }
}

/// Die Antwort auf POST /admin/metadata/reload: wie viele Titel neu
/// geholt werden. Das Holen selbst läuft im Hintergrund weiter.
public struct MetadatenAuftrag: Codable, Sendable {
    public let status: String
    public let titel: Int
}

/// Ein Eintrag bei der Metadaten-Quelle, den man einem Titel zuweisen
/// kann (GET /admin/items/{id}/metadata/search). `poster` ist eine
/// Adresse bei der Quelle, nicht beim eigenen Server.
public struct TitelTreffer: Codable, Identifiable, Hashable, Sendable {
    public let sourceId: String
    public let title: String
    public let original: String?
    public let year: Int?
    public let overview: String?
    public let poster: String?

    public var id: String { sourceId }
}

/// Wie weit das Einlesen einer Bibliothek ist.
///
/// `gesamt` steht nach einem kurzen Vorlauf fest; `gefunden` wächst
/// mit. Nur mit `gesamt` lässt sich ein Anteil angeben.
public struct ScanFortschritt: Codable, Hashable, Sendable {
    public let laeuft: Bool
    public let gesamt: Int
    public let gefunden: Int
    public let fertig: Int
    public let datei: String
    public let seitMs: Int64

    public var anteil: Double? {
        guard gesamt > 0 else { return nil }
        return min(1, Double(fertig) / Double(gesamt))
    }
}

/// Was der Besitzer über eine laufende Wiedergabe sieht.
///
/// Nur die Last — kein Titel, keine Person, kein Gerät, kein Ort, keine
/// Startzeit. Der Server gibt nicht mehr heraus; diese Struktur hat deshalb
/// auch keine Felder dafür, und der Test im Server lässt genau diese zu.
/// Nur noch direkt oder umgewandelt: Gründe, Qualität und Bitrate machten
/// eine Wiedergabe mit den Grenzen je Konto einer Person zuordenbar. Die
/// Kennung gilt nur für die eine Abfrage (der Server vergibt bei jeder
/// Liste neue).
public struct LiveSession: Codable, Identifiable, Hashable, Sendable {
    /// Zufallskennung, nur für „Beenden". Der Server lässt sie künftig weg —
    /// dann gibt es den Knopf nicht, und die Zeile bekommt für die Liste
    /// eine eigene Kennung (`id`), die nie zum Server geht.
    private let rohKennung: String?
    public let mode: String          // direct | transcode
    public let id: String

    public var kennung: String? { rohKennung }

    private enum CodingKeys: String, CodingKey {
        case rohKennung = "id", mode
    }

    public init(kennung: String?, mode: String) {
        self.rohKennung = kennung
        self.mode = mode
        self.id = kennung ?? UUID().uuidString
    }

    /// Jedes Feld darf fehlen: die anonyme Liste wird serverseitig kleiner,
    /// und eine schmalere Antwort soll die Ansicht nicht leer lassen.
    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        self.init(
            kennung: try c.decodeIfPresent(String.self, forKey: .rohKennung),
            mode: try c.decodeIfPresent(String.self, forKey: .mode) ?? "")
    }

    /// Nur was der Server geschickt hat — die Kennung für die Liste nicht.
    public func encode(to encoder: Encoder) throws {
        var c = encoder.container(keyedBy: CodingKeys.self)
        try c.encodeIfPresent(rohKennung, forKey: .rohKennung)
        try c.encode(mode, forKey: .mode)
    }
}

/// Eine eigene laufende Wiedergabe — unter „Ich → Geräte", damit man einen
/// vergessenen Fernseher selbst beenden kann. Nur das eigene Profil sieht sie.
public struct EigeneWiedergabe: Codable, Identifiable, Hashable, Sendable {
    public let id: String
    public let deviceName: String
    public let deviceKind: String
    public let mode: String
    public let detail: String
}

extension EigeneWiedergabe {
    /// „direkt" oder „wird umgewandelt" — wie in der Verwaltung.
    public var artText: String {
        mode == "direct" ? String(localized: "direkt", bundle: .lumenKit, locale: .lumen) : String(localized: "wird umgewandelt", bundle: .lumenKit, locale: .lumen)
    }

    /// „wird umgewandelt · 4K HEVC → 1080p H.264" — die technische Last,
    /// nie der Titel.
    public var zeile: String {
        [artText, detail].filter { !$0.isEmpty }.joined(separator: " · ")
    }
}

public struct ServerLoad: Codable, Sendable {
    public let total: Int
    public let transcodes: Int
    /// Grob, wie sehr der Server rechnet: gering, mittel, hoch.
    public let last: String?
}

public struct Sessions: Codable, Sendable {
    public let sessions: [LiveSession]
    public let load: ServerLoad?
    public let note: String?

    private enum CodingKeys: String, CodingKey { case sessions, load, note }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        sessions = try c.decodeIfPresent([LiveSession].self, forKey: .sessions) ?? []
        load = try? c.decodeIfPresent(ServerLoad.self, forKey: .load)
        note = try c.decodeIfPresent(String.self, forKey: .note)
    }
}

public struct LogLine: Codable, Identifiable, Sendable {
    public let at: Date
    public let level: String
    public let area: String
    public let message: String

    public var id: String { "\(at.timeIntervalSince1970)-\(message)" }
}

// MARK: - Verwaltung: Aufräumen, Konten, Einladungen
//
// Nur auf dem Mac gezeigt — die Verwaltung gehört an den Schreibtisch.
// Auch hier steht nichts darüber, wer was schaut: ein Konto nennt seine
// Rechte und wie viele Geräte es hat, mehr nicht.

/// Ein Titel mit mehreren Dateien — mehrere Fassungen desselben Films.
public struct Duplikat: Codable, Identifiable, Hashable, Sendable {
    public let itemId: String
    public let title: String
    public let year: Int?
    public let files: [MediaFile]

    public var id: String { itemId }

    /// Die Dateien nach ihrer Teilnummer.
    public var dateienGeordnet: [MediaFile] {
        files.sorted { $0.partNo < $1.partNo }
    }

    /// Sind das Teile eines Films (Teil 1, 2, 3 … ohne Lücke) statt
    /// Fassungen? Dann gibt es nichts zu wählen und nichts abzutrennen,
    /// nur zusammenzuführen. Der Server prüft dasselbe noch einmal, bevor
    /// er etwas anfasst (fassungen.go: adminTeileZusammenfuehren).
    public var sindTeile: Bool {
        let geordnet = dateienGeordnet
        guard geordnet.count > 1 else { return false }
        return geordnet.enumerated().allSatisfy { $0.element.partNo == $0.offset + 1 }
    }
}

/// Die Antwort auf einen Auftrag, der im Hintergrund weiterläuft (202) —
/// etwa das Zusammenführen von Teilen. `status` ist für Menschen geschrieben.
public struct Auftrag: Codable, Sendable {
    public let status: String
}

/// Die Antwort auf GET /admin/cleanup. Go schickt leere Listen als `null`.
public struct Aufraeumen: Codable, Sendable {
    public let duplicates: [Duplikat]?
    public let missing: [MediaFile]?
    public let unmatched: [Item]?
    public let pending: Int?
}

/// Eine Filmreihe in der Liste der Verwaltung — auch die aufgelösten,
/// ohne ihre Filme.
public struct ReiheZeile: Codable, Identifiable, Hashable, Sendable {
    public let id: String
    public let name: String
    public let source: String
    public let hidden: Bool
    public let anzahl: Int
}

/// Ein Konto, wie die Verwaltung es sieht: wer, welche Rechte, wie viele
/// Geräte. Kein Verlauf, kein „zuletzt geschaut".
public struct KontoEintrag: Codable, Identifiable, Hashable, Sendable {
    public let id: String
    public let name: String
    public let avatar: String
    public let isOwner: Bool
    public let isKid: Bool
    public let isAdmin: Bool?
    public let groupId: String?
    public let groupName: String?
    public let limits: Limits
    public let libraries: [String]?
    public let devices: Int
    /// Über die Gruppe dieselben Rechte wie der Besitzer.
    public let vollzugriff: Bool?

    /// „schauen", „verwalten" oder „voll" — wie der Server sie setzt.
    public var rolle: String {
        if vollzugriff == true { return "voll" }
        return isAdmin == true ? "verwalten" : "schauen"
    }
}

/// Eine frische Einladung: der Code, und bis wann er gilt.
public struct Einladung: Codable, Hashable, Sendable {
    public let code: String
    public let expiresAt: Date
    public let used: Bool
}

public struct Device: Codable, Hashable, Sendable {
    public let accountId: String
    public let name: String
    public let kind: String
    public let createdAt: Date
    public let lastSeen: Date
}

// MARK: - Sicherung und Übersicht der Verwaltung

/// Eine vorhandene Sicherung der Datenbank. Der Server nennt nur den Tag,
/// keine Uhrzeit — eine Uhrzeit sagte mehr darüber, wann jemand schaut,
/// als für das Wiederherstellen nötig ist.
public struct Sicherung: Codable, Identifiable, Hashable, Sendable {
    /// 1 ist die neueste, 2 die davor. Mehr gibt es nicht.
    public let nr: Int
    /// JJJJ-MM-TT, so wie der Server es schreibt.
    public let tag: String
    /// Ganze MB, abgerundet — eine kleine Datenbank ist dann 0.
    public let mb: Int64

    public var id: Int { nr }

    /// „Neueste" und „Davor" statt einer Nummer: wer wiederherstellt,
    /// denkt in „gestern" und „vorgestern", nicht in Generationen.
    public var name: String {
        switch nr {
        case 1: String(localized: "Neueste", bundle: .lumenKit, locale: .lumen)
        case 2: String(localized: "Davor", bundle: .lumenKit, locale: .lumen)
        default: String(localized: "Sicherung \(nr)", bundle: .lumenKit, locale: .lumen)
        }
    }

    /// Abgerundet auf ganze MB wäre eine frische Datenbank „0 MB" — das
    /// sähe aus, als wäre die Sicherung leer.
    public var groesse: String {
        mb < 1 ? String(localized: "unter 1 MB", bundle: .lumenKit, locale: .lumen) : Sprache.zahl(Int(mb)) + " MB"
    }

    public func tagText(heute: Date = .now) -> String { Self.tagText(tag, heute: heute) }

    /// „heute" oder TT.MM.JJJJ. Ein Tag, den wir nicht lesen können, bleibt
    /// so stehen, wie er kam — lieber roh als gar nicht.
    public static func tagText(_ tag: String, heute: Date = .now) -> String {
        let teile = tag.split(separator: "-")
        guard teile.count == 3 else { return tag }
        // Der Server rechnet den Tag in seiner Ortszeit; Mac und Server
        // stehen im selben Haushalt, also in derselben Zone.
        let formatierer = DateFormatter()
        formatierer.locale = Locale(identifier: "en_US_POSIX")
        formatierer.calendar = Calendar(identifier: .gregorian)
        formatierer.timeZone = .current
        formatierer.dateFormat = "yyyy-MM-dd"
        if formatierer.string(from: heute) == tag { return String(localized: "heute", bundle: .lumenKit, locale: .lumen) }
        if let j = Int(teile[0]), let m = Int(teile[1]), let d = Int(teile[2]) {
            return Sprache.datum(jahr: j, monat: m, tag: d, zweistellig: true)
        }
        return "\(teile[2]).\(teile[1]).\(teile[0])"
    }
}

/// Die Antwort auf GET und POST /admin/backups. Go schickt eine leere
/// Liste als `null`, darum ist sie optional.
public struct Sicherungen: Codable, Sendable {
    public let sicherungen: [Sicherung]?
    /// Der Tag der letzten Sicherung, leer, wenn es noch keine gab.
    public let zuletzt: String?
    /// Leer, wenn die letzte Sicherung geklappt hat.
    public let fehler: String?
    public let generationen: Int?
    public let hinweis: String?

    public var liste: [Sicherung] { (sicherungen ?? []).sorted { $0.nr < $1.nr } }
}

/// Die Antwort auf POST /admin/backups/{nr}/restore. Danach startet der
/// Server neu und ist ein paar Sekunden nicht erreichbar.
public struct Wiederherstellung: Codable, Sendable {
    public let status: String
}

/// Freier Platz auf dem Server, in ganzen MB: für die Datenbank und für
/// die Zwischendateien beim Umwandeln. Oft dieselbe Platte.
public struct FreierPlatz: Codable, Hashable, Sendable {
    public let datenMB: UInt64
    public let umwandlungMB: UInt64

    /// Unter 5 GB wird es eng: eine Umwandlung legt schnell ein paar
    /// GB Zwischendateien an, und eine volle Platte nimmt die Datenbank mit.
    public static let knappAbMB: UInt64 = 5120

    public var knapp: Bool { min(datenMB, umwandlungMB) < Self.knappAbMB }

    /// „12.4 GB" ab einem GB, darunter „830 MB" — in der Schreibweise der
    /// gewählten Sprache (Schweiz und Englisch mit Punkt, Deutschland und
    /// Österreich mit Komma).
    public static func text(_ mb: UInt64) -> String {
        guard mb >= 1024 else { return Sprache.zahl(Int(mb)) + " MB" }
        let gb = Double(mb) / 1024
        return gb.formatted(.number.precision(.fractionLength(1)).locale(.lumen)) + " GB"
    }
}

/// Was die Mac-Verwaltung aus GET /admin/overview liest. Nur die Felder,
/// die sie zeigt — und alle optional, weil ein älterer Server sie noch
/// nicht schickt. Fehlt eins, fehlt die Zeile, nicht die ganze Seite.
public struct Uebersicht: Codable, Sendable {
    public let version: String?
    public let platz: FreierPlatz?
    /// Tag der letzten Sicherung (JJJJ-MM-TT), leer, wenn es noch keine gab.
    public let sicherung: String?
}

// MARK: - Updates (nur der Besitzer)

/// Was der Besitzer festlegt: täglich nachsehen, und ob Lumen sich im
/// Zeitfenster von selbst aktualisiert. Uhrzeiten als „HH:MM".
public struct UpdateEinstellung: Codable, Hashable, Sendable {
    /// „aus" oder „taeglich".
    public let pruefen: String
    public let pruefZeit: String
    /// „aus" oder „fenster".
    public let auto: String
    public let fensterVon: String
    public let fensterBis: String
}

/// Eine Veröffentlichung auf GitHub.
public struct UpdateRelease: Codable, Hashable, Sendable {
    public let version: String
    public let tag: String
    /// Die Seite zum Lesen, was sich geändert hat.
    public let url: String
    public let published: Date?
    public let notizen: String?
}

/// Eine Zeile im Aktualisierungs-Protokoll.
public struct UpdateEintrag: Codable, Identifiable, Hashable, Sendable {
    public let at: Date
    public let von: String
    public let nach: String
    /// „ok", „fehler" oder „verschoben".
    public let ergebnis: String
    public let notiz: String?
    public let auto: Bool

    public var id: String { "\(at.timeIntervalSince1970)-\(nach)-\(ergebnis)" }

    /// Wie das Kästchen im Browser: eingesetzt, fehlgeschlagen, verschoben.
    public var ergebnisText: String {
        switch ergebnis {
        case "ok": String(localized: "eingesetzt", bundle: .lumenKit, locale: .lumen)
        case "fehler": String(localized: "fehlgeschlagen", bundle: .lumenKit, locale: .lumen)
        default: String(localized: "verschoben", bundle: .lumenKit, locale: .lumen)
        }
    }
}

/// Die Antwort auf GET /admin/update — im Server eine map. Was nur
/// manchmal dabei ist, ist optional.
public struct UpdateStand: Codable, Sendable {
    public let installiert: String
    /// Läuft Lumen in einem Container? Dann tauscht man das Abbild von
    /// aussen, und `anleitung` sagt wie.
    public let container: Bool
    public let einstellung: UpdateEinstellung
    public let protokoll: [UpdateEintrag]?
    /// Gerade wird eine neue Fassung eingesetzt.
    public let laeuft: Bool
    /// Nur die Zahl der laufenden Wiedergaben — keine Namen.
    public let wiedergaben: Int
    public let tokenGesetzt: Bool
    /// „umgebung" (LUMEN_GITHUB_TOKEN), „verwaltung" oder leer.
    public let tokenQuelle: String
    public let tokenUngueltig: Bool
    public let releaseSeite: String
    public let geprueft: Date?
    public let fehler: String?
    public let neueste: UpdateRelease?
    public let neuer: Bool?
    public let verschoben: Date?
    public let anleitung: String?

    public var gibtNeueres: Bool { neuer == true && neueste != nil }

    /// Die Zeile in der Übersicht, wie `updateKurz` im Browser.
    public var kurz: String {
        if gibtNeueres, let n = neueste { return String(localized: "\(n.version) verfügbar", bundle: .lumenKit, locale: .lumen) }
        if fehler != nil { return String(localized: "Prüfen klappte nicht", bundle: .lumenKit, locale: .lumen) }
        return String(localized: "\(installiert) · aktuell", bundle: .lumenKit, locale: .lumen)
    }
}

/// Die Antwort auf POST /admin/update/run.
public struct UpdateLauf: Codable, Sendable {
    public let status: String
    public let nach: String
}

// MARK: - Gruppen (nur der Besitzer)

/// Eine Gruppe bündelt Bibliotheken und Grenzen; wer darin ist, bekommt
/// sie statt der eigenen Einträge. Mit `verwaltung` dürfen ihre Mitglieder
/// mitverwalten — sehen aber wie der Besitzer nicht, wer was schaut.
public struct Gruppe: Codable, Identifiable, Hashable, Sendable {
    public let id: String
    public let name: String
    public let verwaltung: Bool
    /// Verwaltung mit allen Rechten des Besitzers.
    public let vollzugriff: Bool?
    public let libraries: [String]?
    public let limits: Limits
    public let mitglieder: Int

    public var bibliotheken: [String] { libraries ?? [] }

    /// „2 Personen · 3 Bibliotheken · höchstens 2 gleichzeitig"
    public var zeile: String {
        let n = bibliotheken.count
        return String(localized: "\(mitglieder) Personen", bundle: .lumenKit, locale: .lumen) + " · "
            + String(localized: "\(n) Bibliotheken", bundle: .lumenKit, locale: .lumen)
            + " · " + String(localized: "höchstens \(limits.maxStreams) gleichzeitig", bundle: .lumenKit, locale: .lumen)
    }
}
