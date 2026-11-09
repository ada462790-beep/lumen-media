// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

import SwiftUI
import LumenKit

#if os(macOS)
// Was in der Verwaltung des Mac noch fehlte und der Browser schon konnte:
// der Name des Servers, die Metadaten-Quellen samt „fehlende holen" und
// den nicht erkannten Titeln, und der Fernzugriff mit dem Fingerabdruck,
// den die Apps von unterwegs brauchen.
//
// Jeder Teil fragt den Server einzeln. Kennt er einen Weg nicht (älterer
// Server), fehlt nur dieser Teil.

struct ServerEinstellungenView: View {
    @Environment(Session.self) private var session

    // Name
    @State private var name = ""
    @State private var nameMeldung: String?

    // Metadaten
    @State private var quellen: MetadatenQuellen?
    @State private var tmdbKey = ""
    @State private var omdbKey = ""
    @State private var sprache = ""
    @State private var region = ""
    @State private var imdb = true
    @State private var quellenMeldung: String?
    @State private var nichtErkannt: [Item]?
    @State private var aufloesenMeldung: String?

    // Fernzugriff
    @State private var fern: Fernzugriff?

    // Sprache
    @State private var spracheMeldung: String?

    @State private var laedt = true

    var body: some View {
        Form {
            if session.isOwner {
                Section {
                    TextField("Name", text: $name, prompt: Text("z. B. Lumen bei Familie Muster"))
                    HStack {
                        if let nameMeldung { Text(nameMeldung).font(.caption).foregroundStyle(Theme.label2) }
                        Spacer()
                        Button("Namen speichern") { Task { await nameSpeichern() } }
                    }
                } header: {
                    Text("Name des Servers")
                } footer: {
                    Text("So steht der Server in der Liste der Apps. Höchstens 40 Zeichen; leer heisst ohne Namen.")
                }

                // Die Sprache für alle Profile, die keine eigene gewählt
                // haben, und für den Bildschirm vor der Anmeldung. Nur wenn
                // der Server Sprachen kennt — ein älterer nimmt sie nicht an.
                if session.serverSprache != nil {
                    Section {
                        Picker("Sprache", selection: serverSprache) {
                            ForEach(session.sprachen) { s in Text(verbatim: s.name).tag(s.id) }
                        }
                        if let spracheMeldung {
                            Text(spracheMeldung).font(.caption).foregroundStyle(Theme.label2)
                        }
                    } header: {
                        Text("Sprache des Servers")
                    } footer: {
                        Text("Gilt für alle Profile, die unter Einstellungen keine eigene Sprache gewählt haben, und für die Anmeldung. Wer selbst eine wählt, behält sie.")
                    }
                }
            }

            if let quellen {
                Section {
                    LabeledContent("TMDb", value: zustand(quellen.tmdb))
                    SecureField("Neuer TMDb-Schlüssel", text: $tmdbKey)
                    LabeledContent("IMDb-Wertung (OMDb)", value: zustand(quellen.imdb))
                    SecureField("Neuer OMDb-Schlüssel", text: $omdbKey)
                    Toggle("IMDb-Wertung zeigen", isOn: $imdb)
                    TextField("Sprache", text: $sprache, prompt: Text(verbatim: "de-DE"))
                    TextField("Region", text: $region, prompt: Text(verbatim: "CH"))
                    HStack {
                        if let quellenMeldung { Text(quellenMeldung).font(.caption).foregroundStyle(Theme.label2) }
                        Spacer()
                        Button("Quellen speichern") { Task { await quellenSpeichern() } }
                    }
                } header: {
                    Text("Metadaten-Quellen")
                } footer: {
                    Text(quellen.note ?? String(localized: "Die Schlüssel werden nie wieder angezeigt — nur, ob einer eingerichtet ist.", bundle: .lumen, locale: .lumen))
                }

                Section {
                    HStack {
                        Text("Titel ohne Beschreibung und Bild jetzt nachschlagen.")
                        Spacer()
                        Button("Fehlende holen") { Task { await aufloesen() } }
                            .disabled(quellen.ready == false)
                    }
                    if let aufloesenMeldung {
                        Text(aufloesenMeldung).font(.caption).foregroundStyle(Theme.label2)
                    }
                    if let nichtErkannt {
                        if nichtErkannt.isEmpty {
                            Text("Alle Titel sind erkannt.").foregroundStyle(Theme.label2)
                        } else {
                            ForEach(nichtErkannt) { item in
                                NavigationLink { DetailView(itemId: item.id) } label: {
                                    LabeledContent(item.title, value: item.jahresText ?? "")
                                }
                            }
                        }
                    }
                } header: {
                    Text("Nicht erkannt")
                } footer: {
                    Text("Auf der Titelseite sagst du Lumen unter „Anderen Titel zuweisen“, welcher Film es wirklich ist.")
                }
            }

            if let fern {
                Section {
                    LabeledContent("Eingerichtet", value: fern.an ? String(localized: "ja", bundle: .lumen, locale: .lumen) : String(localized: "nein", bundle: .lumen, locale: .lumen))
                    if fern.an {
                        LabeledContent("Verbunden", value: fern.verbunden == true ? String(localized: "ja", bundle: .lumen, locale: .lumen) : String(localized: "nein", bundle: .lumen, locale: .lumen))
                        if let v = fern.vermittler, !v.isEmpty { LabeledContent("Vermittler", value: v) }
                        if let f = fern.letzterFehler, !f.isEmpty {
                            Text(f).font(.caption).foregroundStyle(Theme.warn)
                        }
                    }
                    if let abdruck = fern.eigenerFingerabdruck, !abdruck.isEmpty {
                        LabeledContent("Fingerabdruck") {
                            HStack {
                                Text(abdruck)
                                    .font(.system(.caption, design: .monospaced))
                                    .textSelection(.enabled)
                                Button("Kopieren") {
                                    NSPasteboard.general.clearContents()
                                    NSPasteboard.general.setString(abdruck, forType: .string)
                                }
                            }
                        }
                    }
                    if let h = fern.hinweis, !h.isEmpty {
                        Text(h).font(.caption).foregroundStyle(Theme.label2)
                    }
                } header: {
                    Text("Fernzugriff")
                } footer: {
                    Text("Den Fingerabdruck tippen die Apps ein, wenn sie den Server von unterwegs erreichen sollen. Er ist kein Geheimnis.")
                }
            }
        }
        .formStyle(.grouped)
        .overlay { if laedt { ProgressView() } }
        .navigationTitle("Server")
        .task { await load() }
    }

    private func zustand(_ q: MetadatenQuellen.Quelle?) -> String {
        guard let q else { return String(localized: "unbekannt", bundle: .lumen, locale: .lumen) }
        if !q.configured { return String(localized: "kein Schlüssel", bundle: .lumen, locale: .lumen) }
        if q.enabled == false { return String(localized: "eingerichtet, aus", bundle: .lumen, locale: .lumen) }
        return String(localized: "eingerichtet", bundle: .lumen, locale: .lumen)
    }

    /// Die Sprache des Servers: gewählt heisst gespeichert.
    private var serverSprache: Binding<String> {
        Binding(
            get: { session.serverSprache ?? Sprache.quelle },
            set: { neu in
                Task {
                    do {
                        try await session.serverSpracheSetzen(neu)
                        spracheMeldung = String(localized: "Gespeichert.", bundle: .lumen, locale: .lumen)
                    } catch {
                        spracheMeldung = LumenError.fehltAufServer(error)
                            ? String(localized: "Dein Server kann das noch nicht.", bundle: .lumen, locale: .lumen)
                            : Session.text(error)
                    }
                }
            })
    }

    private func load() async {
        defer { laedt = false }
        name = session.aktiverServer?.name ?? ""
        if let q = try? await session.verwaltet({ try await session.client.adminMetadatenQuellen() }) {
            quellen = q
            sprache = q.language ?? ""
            region = q.region ?? ""
            imdb = q.imdb?.enabled ?? true
        }
        nichtErkannt = try? await session.verwaltet { try await session.client.adminNichtErkannt() }
        fern = try? await session.verwaltet { try await session.client.adminFernzugriff() }
    }

    private func nameSpeichern() async {
        let neu = name.trimmingCharacters(in: .whitespacesAndNewlines)
        guard neu.count <= 40 else {
            nameMeldung = String(localized: "Höchstens 40 Zeichen.", bundle: .lumen, locale: .lumen)
            return
        }
        do {
            try await session.verwaltet { try await session.client.adminServerName(neu) }
            nameMeldung = String(localized: "Gespeichert. Die Apps zeigen ihn beim nächsten Verbinden.", bundle: .lumen, locale: .lumen)
        } catch {
            nameMeldung = LumenError.fehltAufServer(error)
                ? String(localized: "Dein Server kann das noch nicht.", bundle: .lumen, locale: .lumen) : Session.text(error)
        }
    }

    private func quellenSpeichern() async {
        let tmdb = tmdbKey.trimmingCharacters(in: .whitespaces)
        let omdb = omdbKey.trimmingCharacters(in: .whitespaces)
        do {
            let neu = try await session.verwaltet {
                try await session.client.adminMetadatenQuellenSetzen(
                    tmdbKey: tmdb.isEmpty ? nil : tmdb,
                    omdbKey: omdb.isEmpty ? nil : omdb,
                    sprache: sprache, region: region, imdb: imdb)
            }
            quellen = neu
            tmdbKey = ""
            omdbKey = ""
            quellenMeldung = String(localized: "Gespeichert.", bundle: .lumen, locale: .lumen)
        } catch {
            quellenMeldung = Session.text(error)
        }
    }

    private func aufloesen() async {
        do {
            let status = try await session.verwaltet { try await session.client.adminMetadatenAufloesen() }
            // Der Server meldet den Zustand als feste Kennung, nicht als Satz.
            aufloesenMeldung = status == "läuft schon" // sprache: fest
                ? String(localized: "Läuft schon — das dauert einen Moment.", bundle: .lumen, locale: .lumen)
                : String(localized: "Läuft im Hintergrund. Schau später wieder herein.", bundle: .lumen, locale: .lumen)
        } catch {
            aufloesenMeldung = Session.text(error)
        }
    }
}

/// PIN oder Passwort eines anderen Profils zurücksetzen — wenn jemand im
/// Haushalt seinen vergessen hat. Verlangt die Bestätigung wie jede
/// Rechteänderung; der Dialog kommt von selbst.
struct SperreZuruecksetzen: View {
    @Environment(Session.self) private var session
    let konto: KontoEintrag

    @State private var art: Schutzart = .pin
    @State private var neu = ""
    @State private var meldung: String?
    @State private var nichtVerfuegbar = false
    @State private var bestaetigen = false

    var body: some View {
        if !nichtVerfuegbar {
            Section {
                Picker("Neuer Schutz", selection: $art) {
                    ForEach(Schutzart.allCases) { a in Text(a.name).tag(a) }
                }
                if art != .none {
                    SecureField(art == .pin ? "Neuer PIN" : "Neues Passwort", text: $neu)
                }
                HStack {
                    if let meldung { Text(meldung).font(.caption).foregroundStyle(Theme.label2) }
                    Spacer()
                    Button("Zurücksetzen") { bestaetigen = true }
                }
                // Der Server löscht dabei den privaten Stand des Profils
                // (store/vergessen.go) — das muss vorher dastehen.
                .confirmationDialog("PIN zurücksetzen?", isPresented: $bestaetigen) {
                    Button("Zurücksetzen", role: .destructive) { Task { await setzen() } }
                    Button("Abbrechen", role: .cancel) {}
                } message: {
                    Text("Der Verlauf, die Merkliste und die Bewertungen dieses Profils werden gelöscht – sonst könnte die Verwaltung sie einsehen.")
                }
            } header: {
                Text("PIN oder Passwort")
            } footer: {
                Text("Für den Fall, dass \(konto.name) den eigenen vergessen hat. Das alte Geheimnis braucht es dafür nicht.")
            }
        }
    }

    private func setzen() async {
        if let fehler = SchutzPruefung.fehler(art: art, neu: neu, wiederholt: neu) {
            meldung = fehler
            return
        }
        let id = konto.id
        let a = art.rawValue
        let geheim = art == .none ? "" : neu
        do {
            try await session.verwaltet { try await session.client.adminSperreSetzen(id, art: a, neu: geheim) }
            neu = ""
            meldung = String(localized: "Zurückgesetzt.", bundle: .lumen, locale: .lumen)
        } catch {
            if LumenError.fehltAufServer(error) {
                nichtVerfuegbar = true
            } else {
                meldung = Session.text(error)
            }
        }
    }
}
#endif
