// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

import SwiftUI
import LumenKit

// Das Profil auf dem Fernseher: dieselben Einstellungen wie im Browser,
// soweit sie hier etwas bedeuten — sie stehen am Konto, wer sie am
// iPhone ändert, hat sie auch hier. Hell und Dunkel gibt es nicht zu
// wählen: auf dem Fernseher liegt fast alles über Bildern, und das Apple
// TV stellt die Erscheinung ohnehin selbst ein.

struct TVProfilView: View {
    @Environment(Session.self) private var session
    @State private var fehler: String?
    @State private var verlaufFragen = false
    /// Was gerade in diesem Profil läuft — auf welchem Gerät, nicht was.
    @State private var wiedergaben: [EigeneWiedergabe] = []
    /// Wie viel Bildschirmzeit heute bleibt — nur bei Kinderprofilen.
    @State private var bildschirmzeit: Bildschirmzeit?

    private var prefs: [String: String] { session.me?.prefs ?? [:] }

    var body: some View {
        List {
            if let account = session.me?.account {
                Section {
                    HStack(spacing: 30) {
                        ProfilAvatar(name: account.name, avatar: account.avatar,
                                     accountId: account.id, size: 120)
                        VStack(alignment: .leading, spacing: 6) {
                            Text(account.name).font(.title2.weight(.semibold))
                            Text(rolle(account)).font(.callout).foregroundStyle(Theme.label2)
                        }
                    }
                    .padding(.vertical, 10)
                    Button {
                        Task { await session.signOut() }
                    } label: {
                        Label("Profil wechseln", systemImage: "person.2")
                    }
                    // Die Anmeldung bleibt: wer zurückwechselt, ist gleich drin.
                    Button {
                        session.serverWechseln()
                    } label: {
                        LabeledContent {
                            Text(session.aktiverServer?.name ?? "")
                        } label: {
                            Label("Server wechseln", systemImage: "server.rack")
                        }
                    }
                    NavigationLink {
                        TVProfilbildView()
                    } label: {
                        Label("Profilbild", systemImage: "face.smiling")
                    }
                }
            }

            BildschirmzeitAbschnitt(stand: bildschirmzeit)

            // Was nicht in die Leiste passt, und der eigene Schutz.
            TVIchBereiche()

            if let fehler {
                Section { Text(fehler).foregroundStyle(Theme.bad) }
            }

            Section("Wiedergabe") {
                Toggle("Folgen automatisch weiter", isOn: schalter("autoplay", standard: true))
                Toggle("Vorspann überspringen", isOn: schalter("skipIntro", standard: false))
                if let limits = session.me?.limits {
                    LabeledContent("Qualität unterwegs", value: limits.remoteQuality)
                }
            }

            Section {
                NavigationLink {
                    TVAuswahlView(titel: String(localized: "Ton", bundle: .lumen, locale: .lumen), optionen: Sprachwahl.ton,
                                  gewaehlt: prefs[Sprachwahl.prefTon] ?? "") { wert in
                        await setzen([Sprachwahl.prefTon: wert])
                    }
                } label: {
                    LabeledContent("Ton", value: Sprachwahl.tonName(prefs[Sprachwahl.prefTon]))
                }
                NavigationLink {
                    TVAuswahlView(titel: String(localized: "Untertitel", bundle: .lumen, locale: .lumen), optionen: Sprachwahl.untertitel,
                                  gewaehlt: untertitelWert) { wert in
                        await setzen([Sprachwahl.prefUntertitel: wert])
                    }
                } label: {
                    LabeledContent("Untertitel", value: Sprachwahl.untertitelName(prefs[Sprachwahl.prefUntertitel]))
                }
            } header: {
                Text("Sprachen")
            } footer: {
                Text("Was beim Start vorgewählt ist. Wählst du im Player etwas anderes, merkt sich Lumen das.")
            }

            Section {
                NavigationLink {
                    TVAuswahlView(titel: String(localized: "Sprache", bundle: .lumen, locale: .lumen),
                                  optionen: AppSprache.optionen(session),
                                  gewaehlt: AppSprache.gewaehlt(session)) { wert in
                        fehler = await session.spracheWaehlen(wert.isEmpty ? nil : wert)
                    }
                } label: {
                    LabeledContent("Sprache", value: AppSprache.anzeige(session))
                }
            } header: {
                Text("Sprache")
            } footer: {
                Text("Gilt für dein Profil — in allen Apps und im Browser.")
            }

            Section {
                NavigationLink {
                    TVAuswahlView(titel: String(localized: "Grösse der Untertitel", bundle: .lumen, locale: .lumen),
                                  optionen: UntertitelStil.Groesse.allCases.map {
                                      Sprachwahl.Wahl(wert: $0.rawValue, name: $0.name)
                                  },
                                  gewaehlt: UntertitelStil(prefs: prefs).groesse.rawValue) { wert in
                        await setzen([UntertitelStil.prefGroesse: wert])
                    }
                } label: {
                    LabeledContent("Grösse", value: UntertitelStil(prefs: prefs).groesse.name)
                }
                Toggle("Dunkler Hintergrund hinter dem Text",
                       isOn: schalter(UntertitelStil.prefHintergrund, standard: true))
            } header: {
                Text("Untertitel")
            } footer: {
                Text("Gilt, wenn Lumen die Untertitel selbst zeigt. Bei umgewandelten Filmen zeigt sie das Apple TV — dort gelten die Einstellungen des Geräts unter Bedienungshilfen.")
            }

            Section {
                Toggle("Bewertungen im Haushalt zeigen", isOn: schalter("shareRatings", standard: false))
            } header: {
                Text("Privatsphäre")
            } footer: {
                Text("Dein Verlauf und deine Bewertungen gehören dir. Auch der Besitzer des Servers kann nicht sehen, wann oder wie lange du etwas schaust.")
            }

            // Ein Gerät, das noch läuft, obwohl niemand mehr davor sitzt:
            // hier lässt es sich beenden. Nur, wenn überhaupt etwas läuft.
            if !wiedergaben.isEmpty {
                Section {
                    ForEach(wiedergaben) { w in
                        Button(role: .destructive) {
                            Task { await wiedergabeBeenden(w) }
                        } label: {
                            HStack(spacing: 24) {
                                VStack(alignment: .leading, spacing: 6) {
                                    Text(w.deviceName)
                                    Text(w.zeile).font(.caption).foregroundStyle(Theme.label2)
                                }
                                Spacer()
                                Text("Beenden").foregroundStyle(Theme.bad)
                            }
                        }
                    }
                } header: {
                    Text("Läuft gerade in deinem Profil")
                }
            }

            Section {
                Button(role: .destructive) {
                    verlaufFragen = true
                } label: {
                    Text("Verlauf löschen")
                }
            } header: {
                Text("Verlauf")
            } footer: {
                Text("Entfernt alle Fortschritte und Haken. Die Dateien bleiben.")
            }

            Section {
                LabeledContent("Fassung", value: session.serverVersion)
                if !session.license.isEmpty {
                    LabeledContent("Lizenz", value: session.license)
                }
                if let src = session.sourceURL {
                    LabeledContent("Quelltext", value: src.absoluteString)
                }
            } header: {
                Text("Über Lumen")
            } footer: {
                Text("Lumen ist freie Software. Du darfst sie benutzen, verändern und weitergeben — wer sie verändert und betreibt, muss seine Änderungen ebenfalls offenlegen.")
            }
        }
        .navigationTitle("Profil")
        .task {
            await wiedergabenLaden()
            bildschirmzeit = await BildschirmzeitAbschnitt.laden(session)
        }
        .confirmationDialog("Verlauf löschen?", isPresented: $verlaufFragen) {
            Button("Löschen", role: .destructive) {
                Task {
                    do { try await session.client.clearHistory() }
                    catch { fehler = Session.text(error) }
                }
            }
            Button("Abbrechen", role: .cancel) {}
        } message: {
            Text("Alle Fortschritte und Haken werden entfernt. Die Dateien bleiben.")
        }
    }

    /// Leer heisst bei den Untertiteln: aus.
    private var untertitelWert: String {
        let w = prefs[Sprachwahl.prefUntertitel] ?? ""
        return w.isEmpty ? String(localized: "aus", bundle: .lumen, locale: .lumen) : w
    }

    private func rolle(_ a: Account) -> String {
        if a.isOwner { return String(localized: "Besitzer dieses Servers", bundle: .lumen, locale: .lumen) }
        let gruppe = a.groupName.map { " · " + String(localized: "Gruppe „\($0)“", bundle: .lumen, locale: .lumen) } ?? ""
        if a.darfVerwalten { return String(localized: "Mitverwalter", bundle: .lumen, locale: .lumen) + gruppe }
        if a.isKid { return String(localized: "Kinderprofil", bundle: .lumen, locale: .lumen) + gruppe }
        return String(localized: "Haushalt", bundle: .lumen, locale: .lumen) + gruppe
    }

    /// Ein Schalter, der sofort beim Server landet.
    private func schalter(_ key: String, standard: Bool) -> Binding<Bool> {
        Binding(
            get: { prefs[key].map { $0 == "1" } ?? standard },
            set: { neu in Task { await setzen([key: neu ? "1" : "0"]) } })
    }

    private func wiedergabenLaden() async {
        wiedergaben = (try? await session.client.eigeneWiedergaben()) ?? []
    }

    /// Läuft sie schon nicht mehr (404), ist das Ziel auch erreicht — in
    /// jedem Fall danach neu laden.
    private func wiedergabeBeenden(_ w: EigeneWiedergabe) async {
        try? await session.client.eigeneWiedergabeBeenden(w.id)
        await wiedergabenLaden()
    }

    private func setzen(_ werte: [String: String]) async {
        fehler = await session.prefsSetzen(werte)
    }
}

// MARK: - Eine Auswahl

/// Eine Liste zum Auswählen: der gewählte Eintrag trägt den Haken, ein
/// Klick wählt und führt zurück.
struct TVAuswahlView: View {
    @Environment(\.dismiss) private var dismiss
    let titel: String
    let optionen: [Sprachwahl.Wahl]
    let gewaehlt: String
    let waehle: (String) async -> Void

    var body: some View {
        List {
            Section(titel) {
                ForEach(optionen) { o in
                    Button {
                        Task {
                            await waehle(o.wert)
                            dismiss()
                        }
                    } label: {
                        HStack {
                            Text(o.name)
                            Spacer()
                            if o.wert == gewaehlt {
                                Image(systemName: "checkmark").font(.body.weight(.semibold))
                            }
                        }
                    }
                }
            }
        }
        .navigationTitle(titel)
    }
}

// MARK: - Profilbild

/// Eine Farbe oder eine Figur. Ein eigenes Bild lädt man am iPhone, am
/// Mac oder im Browser hoch — hier lässt es sich nur behalten.
struct TVProfilbildView: View {
    @Environment(Session.self) private var session
    @State private var fehler: String?

    private let spalten = [GridItem(.adaptive(minimum: 130, maximum: 150), spacing: 30)]

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 30) {
                TVSeitenkopf(titel: String(localized: "Profilbild", bundle: .lumen, locale: .lumen),
                             zeile: String(localized: "Eine Farbe oder eine Figur. Ein eigenes Bild lädst du am iPhone, am Mac oder im Browser hoch.", bundle: .lumen, locale: .lumen))
                    .padding(.top, 40)
                if let fehler { Text(fehler).foregroundStyle(Theme.bad) }
                if let account = session.me?.account {
                    LazyVGrid(columns: spalten, alignment: .leading, spacing: 30) {
                        if account.avatar == "bild" {
                            knopf("bild", gewaehlt: true, name: String(localized: "Eigenes Bild", bundle: .lumen, locale: .lumen)) {
                                ProfilAvatar(name: account.name, avatar: "bild", accountId: account.id, size: 110)
                            }
                        }
                        ForEach(Profilbild.farben, id: \.self) { farbe in
                            knopf(farbe, gewaehlt: account.avatar == farbe, name: String(localized: "Farbe", bundle: .lumen, locale: .lumen)) {
                                Avatar(initial: account.initial, colorKey: farbe, size: 110)
                            }
                        }
                        ForEach(Figuren.geordnet, id: \.name) { figur in
                            knopf("fig-" + figur.name, gewaehlt: account.avatar == "fig-" + figur.name,
                                  name: figur.name.capitalized) {
                                FigurBild(figur: figur, size: 110)
                            }
                        }
                    }
                }
            }
            .padding(.horizontal, 60)
            .padding(.bottom, 60)
        }
    }

    private func knopf<Inhalt: View>(_ wert: String, gewaehlt: Bool, name: String,
                                     @ViewBuilder inhalt: () -> Inhalt) -> some View {
        Button {
            Task { fehler = await session.avatarSetzen(wert) }
        } label: {
            inhalt()
                .overlay(Circle().strokeBorder(gewaehlt ? Theme.accent : .clear, lineWidth: 5))
                .padding(8)
        }
        .buttonStyle(.borderless)
        .accessibilityLabel(name)
        .accessibilityAddTraits(gewaehlt ? .isSelected : [])
    }
}
