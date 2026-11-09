// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

import CryptoKit
import Foundation
// SecTrustCopyCertificateChain und SecCertificateCopyData kommen von hier.
// Foundation reicht sie womöglich durch — darauf soll sich nichts
// verlassen; TokenStore.swift bindet es für dieselbe Sache ebenso ein.
import Security

/// Das Zertifikat des eigenen Servers festnageln.
///
/// Von unterwegs läuft alles über TLS — sonst könnte der Vermittler
/// mitlesen. Nur hat ein Heimserver selten einen Namen, für den eine
/// öffentliche Stelle ein Zertifikat ausstellt: er heisst „nas" und steht
/// hinter einer Adresse, die niemandem gehört. Lumen stellt sich deshalb
/// selbst eines aus.
///
/// Für die App heisst das: die übliche Prüfung schlägt fehl. Die Antwort
/// darauf ist nicht, die Prüfung abzuschalten — dann wäre jede Verbindung
/// angreifbar und die ganze Verschlüsselung Theater. Die Antwort ist, das
/// eine erwartete Zertifikat zu merken und nur dieses zu akzeptieren.
///
/// Das ist strenger als das Übliche, nicht lockerer: ein öffentliches
/// Zertifikat akzeptiert jedes, das irgendeine der hundert eingebauten
/// Stellen unterschrieben hat. Hier gilt genau eines.
///
/// Den Fingerabdruck zeigt der Server in der Verwaltung unter „Von aussen
/// erreichbar".
public enum Pinnen {

    /// Doppelpunkte und Leerzeichen weg, alles klein — damit sich ein
    /// abgetippter Fingerabdruck mit einem kopierten vergleichen lässt.
    /// Gibt nil, wenn daraus kein SHA-256 wird.
    public static func saubern(_ roh: String?) -> String? {
        guard let roh else { return nil }
        let sauber = roh
            .replacingOccurrences(of: ":", with: "")
            .replacingOccurrences(of: " ", with: "")
            .trimmingCharacters(in: .whitespacesAndNewlines)
            .lowercased()
        guard sauber.count == 64,
              sauber.allSatisfy({ $0.isHexDigit && !$0.isUppercase })
        else { return nil }
        return sauber
    }

    /// SHA-256 eines Zertifikats als Hex — so, wie der Server ihn anzeigt.
    public static func fingerabdruck(_ roh: Data) -> String {
        SHA256.hash(data: roh).map { String(format: "%02x", $0) }.joined()
    }

    /// Vergleich in gleichbleibender Zeit. Ein gewöhnlicher Vergleich
    /// bricht beim ersten abweichenden Zeichen ab; wer die Zeit misst,
    /// könnte daraus Zeichen für Zeichen erraten, was erwartet wird.
    public static func gleichLang(_ a: String, _ b: String) -> Bool {
        let x = Array(a.utf8), y = Array(b.utf8)
        guard x.count == y.count else { return false }
        var unterschied: UInt8 = 0
        for i in x.indices { unterschied |= x[i] ^ y[i] }
        return unterschied == 0
    }
}

/// Prüft bei jeder Verbindung, ob das Zertifikat das erwartete ist.
///
/// Ohne Fingerabdruck tut dieser Prüfer gar nichts — dann gilt die übliche
/// Prüfung des Systems. Stillschweigend alles durchwinken tut er nie.
final class Zertifikatspruefer: NSObject, URLSessionDelegate, Sendable {
    let erwartet: String?

    init(erwartet: String?) {
        self.erwartet = Pinnen.saubern(erwartet)
    }

    func urlSession(
        _ session: URLSession,
        didReceive challenge: URLAuthenticationChallenge,
        completionHandler: @escaping @Sendable (URLSession.AuthChallengeDisposition,
                                                URLCredential?) -> Void
    ) {
        guard challenge.protectionSpace.authenticationMethod == NSURLAuthenticationMethodServerTrust,
              let trust = challenge.protectionSpace.serverTrust
        else {
            completionHandler(.performDefaultHandling, nil)
            return
        }
        guard let erwartet else {
            // Kein Fingerabdruck hinterlegt: das System soll prüfen.
            completionHandler(.performDefaultHandling, nil)
            return
        }
        guard let kette = SecTrustCopyCertificateChain(trust) as? [SecCertificate],
              let erstes = kette.first
        else {
            completionHandler(.cancelAuthenticationChallenge, nil)
            return
        }

        let roh = SecCertificateCopyData(erstes) as Data
        if Pinnen.gleichLang(erwartet, Pinnen.fingerabdruck(roh)) {
            completionHandler(.useCredential, URLCredential(trust: trust))
        } else {
            // Ein anderes Zertifikat ist ein anderer Server.
            completionHandler(.cancelAuthenticationChallenge, nil)
        }
    }
}
