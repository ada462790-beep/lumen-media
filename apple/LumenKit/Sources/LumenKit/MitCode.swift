// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

import Foundation
import Observation

// Anmelden mit Code — vor allem für den Fernseher, wo Tippen mit der
// Fernbedienung eine Qual ist (server/internal/api/code.go).
//
// 1. Dieses Gerät fragt einen Code an und zeigt ihn gross: „482 913".
// 2. Am Handy oder im Browser gibt jemand, der schon angemeldet ist, den
//    Code ein — und gibt damit sein eigenes Profil frei.
// 3. Dieses Gerät fragt alle drei Sekunden nach und bekommt, sobald
//    freigegeben, ein eigenes Token.
//
// Im Server sind die Antworten blosse maps, darum stehen die Strukturen
// hier und nicht in Models.swift.

/// Die Antwort auf POST /code/start.
public struct CodeStart: Codable, Sendable, Equatable {
    public let code: String
    public let geheim: String
    public let gueltigSek: Int
}

/// Die Antwort auf GET /code/{geheim}: wartet noch, oder das Token.
public struct CodeStand: Codable, Sendable {
    public let status: String?
    public let token: String?
}

/// Die Antwort auf POST /code/approve.
public struct CodeFreigabe: Codable, Sendable {
    public let geraet: String
}

public enum Anmeldecode {
    /// „482913" → „482 913": zwei Dreiergruppen lesen sich von der Couch
    /// aus leichter als sechs Ziffern am Stück.
    public static func lesbar(_ code: String) -> String {
        let ziffern = nurZiffern(code)
        guard ziffern.count > 3 else { return ziffern }
        return String(ziffern.prefix(3)) + " " + String(ziffern.dropFirst(3))
    }

    /// Was jemand eintippt oder einfügt, auf höchstens sechs Ziffern
    /// gebracht — „482 913", „482-913" und „482913" sind dasselbe.
    public static func nurZiffern(_ eingabe: String, hoechstens: Int = 6) -> String {
        String(eingabe.filter { $0.isASCII && $0.isNumber }.prefix(hoechstens))
    }

    /// Sechs Ziffern — mehr will der Server nicht.
    public static func vollstaendig(_ eingabe: String) -> Bool {
        nurZiffern(eingabe, hoechstens: 7).count == 6
    }

    /// „4:37" bis zum Ablauf, nie unter null.
    public static func restzeit(bis: Date, jetzt: Date = Date()) -> String {
        let sek = max(0, Int(bis.timeIntervalSince(jetzt).rounded(.up)))
        return String(sek / 60) + ":" + String(format: "%02d", sek % 60)
    }
}

/// Der Ablauf auf dem Gerät, das angemeldet werden will.
///
///     @State private var anmeldung = CodeAnmeldung()
///     …
///     .task { await anmeldung.laufen(session) }
///
/// Die Schleife endet von selbst, sobald die Ansicht verschwindet (die
/// Aufgabe wird abgebrochen) oder die Anmeldung geklappt hat. Läuft ein
/// Code ab, kommt gleich der nächste.
@MainActor
@Observable
public final class CodeAnmeldung {
    /// Der Code, der gerade gilt — nil, solange noch keiner da ist.
    public private(set) var code: String?
    /// Bis wann er gilt.
    public private(set) var bis: Date?
    /// Etwas ging schief; die Schleife versucht es trotzdem weiter.
    public private(set) var fehler: String?

    /// Wie oft nachgefragt wird.
    public let abstand: Duration

    public init(abstand: Duration = .seconds(3)) {
        self.abstand = abstand
    }

    public var codeLesbar: String? { code.map(Anmeldecode.lesbar) }

    public func laufen(_ session: Session) async {
        while !Task.isCancelled {
            let start: CodeStart
            do {
                start = try await session.client.codeStarten()
            } catch {
                code = nil
                bis = nil
                fehler = Session.text(error)
                if !(await warten(.seconds(5))) { return }
                continue
            }
            code = start.code
            bis = Date().addingTimeInterval(TimeInterval(start.gueltigSek))
            fehler = nil

            // Nachfragen, bis freigegeben oder abgelaufen.
            nachfragen: while !Task.isCancelled {
                if !(await warten(abstand)) { return }
                do {
                    let stand = try await session.client.codeAbfragen(geheim: start.geheim)
                    if let token = stand.token, !token.isEmpty {
                        if Task.isCancelled { return }
                        await session.mitCodeAnmelden(token: token)
                        return
                    }
                    fehler = nil
                } catch LumenError.server(_, let status) where status == 410 {
                    break nachfragen   // abgelaufen: neuen Code holen
                } catch {
                    // Kurz nicht erreichbar — weiter nachfragen, solange
                    // der Code gilt.
                    fehler = Session.text(error)
                    if let bis, Date() > bis { break nachfragen }
                }
            }
        }
    }

    /// false, wenn die Aufgabe abgebrochen wurde.
    private func warten(_ dauer: Duration) async -> Bool {
        do {
            try await Task.sleep(for: dauer)
            return !Task.isCancelled
        } catch {
            return false
        }
    }
}
