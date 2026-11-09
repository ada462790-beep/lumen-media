// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

import Foundation
import Security

/// Die gespeicherten Server und je Server das Geräte-Token.
///
/// Wer mehrere Server kennt — den eigenen, den der Eltern —, soll nicht
/// jedes Mal die Adresse neu eintippen und sich neu anmelden. Darum steht
/// hier eine Liste: Name, Adresse und Fingerabdruck in den Einstellungen,
/// denn das ist kein Geheimnis; das Token je Server getrennt im
/// Schlüsselbund.
///
/// Früher kannte die App genau einen Server. Was davon noch in den
/// Einstellungen und im Schlüsselbund liegt, wird beim ersten Zugriff in
/// die Liste übernommen — niemand muss sich deshalb neu anmelden.
public final class TokenStore: @unchecked Sendable {

    public static let shared = TokenStore()

    private let fach: any Geheimfach
    private let defaults: UserDefaults
    private let lock = NSLock()

    // Die Schlüssel. Die alten bleiben genannt, damit die Übernahme sie findet.
    static let schluesselListe = "lumen.serverliste"
    static let schluesselZuletzt = "lumen.serverliste.zuletzt"
    /// Welcher Eintrag aus den alten Einzel-Daten entstanden ist — für den
    /// Fall, dass der Schlüsselbund bei der Übernahme gerade nicht lesbar war.
    static let schluesselUebernommen = "lumen.serverliste.uebernommen"
    static let alteAdresse = "lumen.server"
    static let alterAbdruck = "lumen.fingerabdruck"
    static let altesKonto = "lumen.token"

    /// Unter diesem Namen liegt das Token eines Servers im Schlüsselbund.
    static func konto(_ id: String) -> String { "lumen.token." + id }

    public convenience init(service: String = "org.lumen.app", defaults: UserDefaults = .standard) {
        self.init(fach: Schluesselbund(service: service), defaults: defaults)
    }

    /// Mit einem anderen Geheimfach — die Tests nehmen eines im Speicher,
    /// weil der Schlüsselbund auf einem Prüfrechner gesperrt sein kann.
    public init(fach: any Geheimfach, defaults: UserDefaults) {
        self.fach = fach
        self.defaults = defaults
    }

    // MARK: - Liste

    /// Alle gespeicherten Server, in der Reihenfolge, in der sie dazukamen.
    public var server: [ServerEintrag] {
        lock.lock(); defer { lock.unlock() }
        return liste()
    }

    /// Einen Eintrag anlegen oder — gleiche Kennung — ersetzen.
    public func speichern(_ eintrag: ServerEintrag) {
        lock.lock(); defer { lock.unlock() }
        var alle = liste()
        if let i = alle.firstIndex(where: { $0.id == eintrag.id }) {
            alle[i] = eintrag
        } else {
            alle.append(eintrag)
        }
        schreiben(alle)
    }

    /// Einen anderen Namen geben. Leer heisst: so lassen, wie er war.
    public func umbenennen(id: String, name: String) {
        let neu = name.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !neu.isEmpty else { return }
        lock.lock(); defer { lock.unlock() }
        var alle = liste()
        guard let i = alle.firstIndex(where: { $0.id == id }) else { return }
        alle[i].name = String(neu.prefix(60))
        alle[i].selbstBenannt = true
        schreiben(alle)
    }

    /// Den Namen übernehmen, den der Server selbst nennt — aber nur, wenn
    /// der Nutzer ihn nicht umbenannt hat. Gibt true, wenn sich etwas
    /// geändert hat.
    @discardableResult
    public func nameVomServer(id: String, name: String?) -> Bool {
        let neu = (name ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
        guard !neu.isEmpty else { return false }
        lock.lock(); defer { lock.unlock() }
        var alle = liste()
        guard let i = alle.firstIndex(where: { $0.id == id }),
              !alle[i].selbstBenannt, alle[i].name != String(neu.prefix(60))
        else { return false }
        alle[i].name = String(neu.prefix(60))
        schreiben(alle)
        return true
    }

    /// Den Server vergessen — samt seinem Token. Beim Server abmelden muss
    /// vorher die Sitzung (DELETE /me/device); hier geht es nur ums Gerät.
    public func entfernen(id: String) {
        lock.lock(); defer { lock.unlock() }
        var alle = liste()
        alle.removeAll { $0.id == id }
        schreiben(alle)
        fach.loeschen(Self.konto(id))
        if defaults.string(forKey: Self.schluesselZuletzt) == id {
            defaults.removeObject(forKey: Self.schluesselZuletzt)
        }
        if defaults.string(forKey: Self.schluesselUebernommen) == id {
            fach.loeschen(Self.altesKonto)
            defaults.removeObject(forKey: Self.schluesselUebernommen)
        }
    }

    /// Der Server, mit dem die App zuletzt verbunden war. Beim Start geht
    /// es dorthin, ohne zu fragen.
    public var zuletztBenutzt: String? {
        get {
            lock.lock(); defer { lock.unlock() }
            _ = liste()
            return defaults.string(forKey: Self.schluesselZuletzt)
        }
        set {
            lock.lock(); defer { lock.unlock() }
            if let newValue { defaults.set(newValue, forKey: Self.schluesselZuletzt) }
            else { defaults.removeObject(forKey: Self.schluesselZuletzt) }
        }
    }

    // MARK: - Token je Server

    public func token(fuer id: String) -> String? {
        lock.lock(); defer { lock.unlock() }
        _ = liste()
        if let token = fach.lesen(Self.konto(id)) { return token }
        // War der Schlüsselbund bei der Übernahme nicht lesbar (vor dem
        // ersten Entsperren nach einem Neustart etwa), liegt das Token noch
        // unter dem alten Namen. Dann jetzt umziehen.
        if defaults.string(forKey: Self.schluesselUebernommen) == id,
           let alt = fach.lesen(Self.altesKonto) {
            umziehen(alt, nach: id)
            return alt
        }
        return nil
    }

    /// nil löscht das Token dieses Servers.
    public func setzeToken(_ token: String?, fuer id: String) {
        lock.lock(); defer { lock.unlock() }
        if let token, !token.isEmpty {
            fach.schreiben(token, konto: Self.konto(id))
        } else {
            fach.loeschen(Self.konto(id))
            // Auch ein nicht umgezogenes altes Token gehört dazu.
            if defaults.string(forKey: Self.schluesselUebernommen) == id {
                fach.loeschen(Self.altesKonto)
            }
        }
    }

    // MARK: - Innen (nur mit gehaltenem Schloss)

    private func liste() -> [ServerEintrag] {
        uebernehmenFallsNoetig()
        guard let daten = defaults.data(forKey: Self.schluesselListe),
              let alle = try? JSONDecoder().decode([ServerEintrag].self, from: daten)
        else { return [] }
        return alle
    }

    private func schreiben(_ alle: [ServerEintrag]) {
        if let daten = try? JSONEncoder().encode(alle) {
            defaults.set(daten, forKey: Self.schluesselListe)
        }
    }

    /// Die alten Einzel-Daten in die Liste übernehmen — genau einmal.
    private func uebernehmenFallsNoetig() {
        guard defaults.object(forKey: Self.schluesselListe) == nil else { return }

        var alle: [ServerEintrag] = []
        if let url = defaults.url(forKey: Self.alteAdresse) {
            let eintrag = ServerEintrag(
                name: ServerEintrag.vorschlag(fuer: url),
                adresse: url,
                fingerabdruck: Pinnen.saubern(defaults.string(forKey: Self.alterAbdruck)))
            alle = [eintrag]
            defaults.set(eintrag.id, forKey: Self.schluesselZuletzt)
            defaults.set(eintrag.id, forKey: Self.schluesselUebernommen)
            if let alt = fach.lesen(Self.altesKonto) {
                umziehen(alt, nach: eintrag.id)
            }
        }
        schreiben(alle)
        defaults.removeObject(forKey: Self.alteAdresse)
        defaults.removeObject(forKey: Self.alterAbdruck)
    }

    /// Erst schreiben und nachlesen, dann das alte löschen: so geht das
    /// Token nie verloren, auch wenn der Schlüsselbund zickt.
    private func umziehen(_ token: String, nach id: String) {
        if fach.schreiben(token, konto: Self.konto(id)), fach.lesen(Self.konto(id)) == token {
            fach.loeschen(Self.altesKonto)
            defaults.removeObject(forKey: Self.schluesselUebernommen)
        }
    }
}

// MARK: - Ein gespeicherter Server

public struct ServerEintrag: Codable, Identifiable, Hashable, Sendable {
    public let id: String
    /// Was in der Liste steht — vorgeschlagen, vom Nutzer umbenennbar.
    public var name: String
    /// Die Adresse im Heimnetz, oder von unterwegs die des Vermittlers.
    public var adresse: URL
    /// Der Fingerabdruck des selbst ausgestellten Zertifikats, falls die
    /// Verbindung über https und einen Vermittler läuft.
    public var fingerabdruck: String?
    /// Hat der Nutzer den Namen selbst vergeben? Dann bleibt er, auch wenn
    /// der Server sich später anders nennt.
    public var selbstBenannt: Bool

    public init(id: String = UUID().uuidString, name: String, adresse: URL,
                fingerabdruck: String? = nil, selbstBenannt: Bool = false) {
        self.id = id
        self.name = name
        self.adresse = adresse
        self.fingerabdruck = fingerabdruck
        self.selbstBenannt = selbstBenannt
    }

    private enum CodingKeys: String, CodingKey {
        case id, name, adresse, fingerabdruck, selbstBenannt
    }

    /// Eigens geschrieben, damit Listen ohne `selbstBenannt` lesbar bleiben.
    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.decode(String.self, forKey: .id)
        name = try c.decode(String.self, forKey: .name)
        adresse = try c.decode(URL.self, forKey: .adresse)
        fingerabdruck = try c.decodeIfPresent(String.self, forKey: .fingerabdruck)
        selbstBenannt = try c.decodeIfPresent(Bool.self, forKey: .selbstBenannt) ?? false
    }

    /// Mit festgenageltem Zertifikat — also von unterwegs.
    public var vonUnterwegs: Bool { fingerabdruck != nil }

    /// „192.168.1.20:8097" statt „http://192.168.1.20:8097".
    public var adresseKurz: String {
        guard let host = adresse.host else { return adresse.absoluteString }
        let schema = adresse.scheme == "https" ? "https://" : ""
        return schema + host + (adresse.port.map { ":\($0)" } ?? "")
    }

    /// Ein Name, bevor jemand einen vergibt: der, den der Besitzer dem
    /// Server gegeben hat (`name` in /hello); sonst der Rechnername aus der
    /// Adresse. Ein blosses „Lumen" unterscheidet nichts.
    public static func vorschlag(fuer url: URL, name: String? = nil) -> String {
        let genannt = (name ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
        if !genannt.isEmpty && genannt.lowercased() != "lumen" {
            return String(genannt.prefix(60))
        }
        guard var host = url.host, !host.isEmpty else { return "Lumen" }
        if host.lowercased().hasSuffix(".local") { host = String(host.dropLast(6)) }
        // „lumen.local" heisst einfach Lumen; „nas.local" bleibt „nas",
        // eine IP-Adresse bleibt, wie sie ist.
        if host.isEmpty || host.lowercased() == "lumen" { return "Lumen" }
        return host
    }
}

// MARK: - Geheimfach

/// Wo ein Geheimnis liegt. In der App der Schlüsselbund, in den Tests ein
/// Fach im Speicher.
public protocol Geheimfach: Sendable {
    func lesen(_ konto: String) -> String?
    /// true, wenn es danach wirklich drinsteht.
    @discardableResult
    func schreiben(_ wert: String, konto: String) -> Bool
    func loeschen(_ konto: String)
}

/// Der Schlüsselbund: nicht in den Einstellungen, denn dort stünde das
/// Token im Klartext in einer Datei, die jede Sicherung mitnimmt. Im
/// Schlüsselbund ist es verschlüsselt und verlässt das Gerät nicht.
///
/// Auf dem Mac gibt es zwei Schlüsselbunde: den alten dateibasierten und
/// den, den iPhone und iPad auch haben (`kSecUseDataProtectionKeychain`).
/// Lumen schreibt in den neuen — der fragt nicht nach dem Anmeldepasswort
/// und hält sich an `AfterFirstUnlockThisDeviceOnly`. Was eine ältere
/// Fassung noch in den alten geschrieben hat, wird beim Lesen umgezogen.
public final class Schluesselbund: Geheimfach, @unchecked Sendable {
    private let service: String
    private let lock = NSLock()

    public init(service: String = "org.lumen.app") {
        self.service = service
    }

    public func lesen(_ konto: String) -> String? {
        lock.lock(); defer { lock.unlock() }
        if let text = lesen(konto, modern: true) { return text }
        #if os(macOS)
        // Noch im alten Schlüsselbund? Dann umziehen — erst schreiben und
        // nachlesen, dann das alte löschen, damit nichts verloren geht.
        if let alt = lesen(konto, modern: false) {
            if schreiben(alt, konto: konto, modern: true), lesen(konto, modern: true) == alt {
                SecItemDelete(abfrage(konto, modern: false) as CFDictionary)
            }
            return alt
        }
        #endif
        return nil
    }

    @discardableResult
    public func schreiben(_ wert: String, konto: String) -> Bool {
        lock.lock(); defer { lock.unlock() }
        if schreiben(wert, konto: konto, modern: true) { return true }
        #if os(macOS)
        // Ohne Signatur (ein Bau aus Xcode ohne Team) verweigert der Mac den
        // neuen Schlüsselbund. Dann lieber im alten als gar nicht.
        return schreiben(wert, konto: konto, modern: false)
        #else
        return false
        #endif
    }

    public func loeschen(_ konto: String) {
        lock.lock(); defer { lock.unlock() }
        SecItemDelete(abfrage(konto, modern: true) as CFDictionary)
        #if os(macOS)
        SecItemDelete(abfrage(konto, modern: false) as CFDictionary)
        #endif
    }

    // MARK: Innen (mit gehaltenem Schloss)

    private func lesen(_ konto: String, modern: Bool) -> String? {
        var query = abfrage(konto, modern: modern)
        query[kSecReturnData as String] = true
        query[kSecMatchLimit as String] = kSecMatchLimitOne

        var item: CFTypeRef?
        guard SecItemCopyMatching(query as CFDictionary, &item) == errSecSuccess,
              let data = item as? Data,
              let text = String(data: data, encoding: .utf8), !text.isEmpty
        else { return nil }
        return text
    }

    private func schreiben(_ wert: String, konto: String, modern: Bool) -> Bool {
        let data = Data(wert.utf8)
        // Erst versuchen zu ändern; gibt es den Eintrag noch nicht, anlegen.
        let geaendert = SecItemUpdate(
            abfrage(konto, modern: modern) as CFDictionary,
            [kSecValueData as String: data] as CFDictionary)
        switch geaendert {
        case errSecSuccess:
            return true
        case errSecItemNotFound:
            var query = abfrage(konto, modern: modern)
            query[kSecValueData as String] = data
            // Nur auf diesem Gerät, und nur wenn es einmal entsperrt wurde:
            // so bleibt das Token auch nach einem Neustart nutzbar, wandert
            // aber nicht in eine Sicherung auf einen anderen Rechner.
            query[kSecAttrAccessible as String] = kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly
            let angelegt = SecItemAdd(query as CFDictionary, nil)
            if angelegt == errSecDuplicateItem {
                // Inzwischen von anderswo angelegt: dann eben ändern.
                return SecItemUpdate(abfrage(konto, modern: modern) as CFDictionary,
                                     [kSecValueData as String: data] as CFDictionary) == errSecSuccess
            }
            return angelegt == errSecSuccess
        default:
            // Gesperrt (errSecInteractionNotAllowed), fehlende Berechtigung
            // … — nicht so tun, als wäre es gespeichert.
            return false
        }
    }

    private func abfrage(_ konto: String, modern: Bool) -> [String: Any] {
        var q: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: konto,
        ]
        #if os(macOS)
        if modern { q[kSecUseDataProtectionKeychain as String] = true }
        #endif
        return q
    }
}

/// Ein Geheimfach im Speicher — für Tests und Vorschauen.
public final class SpeicherGeheimfach: Geheimfach, @unchecked Sendable {
    private var inhalt: [String: String]
    private let lock = NSLock()

    public init(_ inhalt: [String: String] = [:]) {
        self.inhalt = inhalt
    }

    public func lesen(_ konto: String) -> String? {
        lock.lock(); defer { lock.unlock() }
        return inhalt[konto]
    }

    @discardableResult
    public func schreiben(_ wert: String, konto: String) -> Bool {
        lock.lock(); defer { lock.unlock() }
        inhalt[konto] = wert
        return true
    }

    public func loeschen(_ konto: String) {
        lock.lock(); defer { lock.unlock() }
        inhalt[konto] = nil
    }

    /// Alle Namen, unter denen etwas liegt.
    public var konten: Set<String> {
        lock.lock(); defer { lock.unlock() }
        return Set(inhalt.keys)
    }
}
