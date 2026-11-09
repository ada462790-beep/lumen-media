// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

import SwiftUI
import LumenKit

// Downloads auf iPhone, iPad und Mac: der Knopf auf der Titelseite und
// die Liste unter „Ich" (Mac und iPad: in der Seitenleiste). Die Arbeit
// macht `Downloads` in LumenKit; hier steht nur, wie es aussieht.

/// Der Knopf auf der Titelseite. Fragt den Server, ob dieses Profil den
/// Titel laden darf; kennt der Server den Weg nicht oder sagt er nein, gibt
/// es keinen Knopf.
struct DownloadKnopf: View {
    @Environment(Session.self) private var session
    let item: Item

    @State private var info: DownloadInfo?
    @State private var wahl = false
    @State private var fehler: String?

    private var downloads: Downloads { Downloads.shared }
    private var eintrag: DownloadEintrag? { downloads.katalog.fuer(itemId: item.id) }

    var body: some View {
        Group {
            if let eintrag {
                vorhanden(eintrag)
            } else if let info, info.erlaubt, !info.dateien.isEmpty {
                Button {
                    if info.dateien.count > 1 { wahl = true } else { Task { await laden(info.dateien[0]) } }
                } label: {
                    Label("Laden", systemImage: "arrow.down.circle")
                }
                .buttonStyle(KopfKnopf())
                .accessibilityLabel("Herunterladen")
            }
        }
        .task(id: item.id) { await infoHolen() }
        .confirmationDialog("Welche Fassung laden?", isPresented: $wahl, titleVisibility: .visible) {
            ForEach(info?.dateien ?? []) { d in
                Button(d.beschriftung) { Task { await laden(d) } }
            }
            Button("Abbrechen", role: .cancel) {}
        } message: {
            if let frei = Downloads.freierPlatz() {
                Text("Frei auf diesem Gerät: \(Downloads.groesseText(frei))")
            }
        }
        .alert("Download", isPresented: Binding(
            get: { fehler != nil }, set: { if !$0 { fehler = nil } })) {
            Button("OK", role: .cancel) {}
        } message: {
            Text(fehler ?? "")
        }
    }

    @ViewBuilder
    private func vorhanden(_ e: DownloadEintrag) -> some View {
        switch e.zustand {
        case .fertig:
            Menu {
                Button("Download löschen", systemImage: "trash", role: .destructive) {
                    downloads.loeschen(e.fileId)
                }
            } label: {
                Label("Geladen", systemImage: "checkmark.circle.fill")
            }
            .buttonStyle(KopfKnopf())
            .accessibilityLabel("Heruntergeladen. Antippen zum Löschen.")
        case .wartet, .laeuft:
            Button {
                downloads.loeschen(e.fileId)
            } label: {
                Label {
                    Text(verbatim: "\(Int((e.anteil * 100).rounded())) %")
                        .monospacedDigit()
                } icon: {
                    ProgressView(value: e.anteil)
                        .progressViewStyle(.circular)
                        .controlSize(.mini)
                }
            }
            .buttonStyle(KopfKnopf())
            .accessibilityLabel(Text("Wird geladen, \(Int((e.anteil * 100).rounded())) Prozent. Antippen zum Abbrechen."))
        case .fehler:
            Button {
                if let d = info?.dateien.first(where: { $0.fileId == e.fileId }) ?? info?.dateien.first {
                    Task { await laden(d) }
                } else {
                    fehler = e.fehler
                }
            } label: {
                Label("Erneut laden", systemImage: "exclamationmark.arrow.circlepath")
            }
            .buttonStyle(KopfKnopf())
        }
    }

    private func infoHolen() async {
        do {
            info = try await session.client.downloadInfo(itemId: item.id)
        } catch {
            // 404 (Server ohne Downloads), kein Netz, nicht erlaubt: kein
            // Knopf. Ein Fehler hier wäre nur Lärm auf der Titelseite.
            info = nil
        }
    }

    private func laden(_ datei: DownloadDatei) async {
        guard let server = session.aktiverServer, let me = session.me else { return }
        let ok = await downloads.laden(item: item, datei: datei, client: session.client,
                                       serverId: server.id, accountId: me.account.id)
        if !ok, let meldung = downloads.meldung {
            fehler = meldung
            downloads.meldung = nil
        }
    }
}

/// Alle Downloads: was läuft, was fertig ist, wie viel Platz es braucht.
struct DownloadsView: View {
    @Environment(Session.self) private var session
    @State private var abspielen: DownloadEintrag?

    private var downloads: Downloads { Downloads.shared }
    private var eintraege: [DownloadEintrag] {
        downloads.katalog.eintraege.sorted { $0.angelegt > $1.angelegt }
    }

    var body: some View {
        List {
            Section {
                LabeledContent("Belegt", value: Downloads.groesseText(downloads.katalog.belegt))
                if downloads.katalog.nochNoetig > 0 {
                    LabeledContent("Kommt noch dazu", value: Downloads.groesseText(downloads.katalog.nochNoetig))
                }
                if let frei = Downloads.freierPlatz() {
                    LabeledContent("Frei auf diesem Gerät", value: Downloads.groesseText(frei))
                }
            } footer: {
                Text("Geladene Titel spielen auch ohne Netz. Wo du stehen bleibst, geht an den Server, sobald er wieder erreichbar ist.")
            }

            let laufend = eintraege.filter { $0.zustand != .fertig }
            if !laufend.isEmpty {
                Section("Wird geladen") {
                    ForEach(laufend) { e in zeile(e) }
                        .onDelete { stellen in loeschen(stellen, aus: laufend) }
                }
            }
            let fertig = eintraege.filter { $0.zustand == .fertig }
            if !fertig.isEmpty {
                Section("Auf diesem Gerät") {
                    ForEach(fertig) { e in
                        Button { abspielen = e } label: { zeile(e) }
                            .buttonStyle(.plain)
                            .contextMenu {
                                Button("Löschen", systemImage: "trash", role: .destructive) {
                                    downloads.loeschen(e.fileId)
                                }
                            }
                    }
                    .onDelete { stellen in loeschen(stellen, aus: fertig) }
                }
            }
        }
        .overlay {
            if eintraege.isEmpty {
                ContentUnavailableView("Noch nichts geladen", systemImage: "arrow.down.circle",
                    description: Text("Auf der Titelseite eines Films oder einer Folge lädst du ihn für unterwegs."))
            }
        }
        .navigationTitle("Downloads")
        .onAppear { downloads.vorbereiten() }
        .playerZeigen(item: $abspielen) { e in
            PlayerAuftrag(item: e.item, offlineDatei: e.fileId)
        }
    }

    private func zeile(_ e: DownloadEintrag) -> some View {
        HStack(spacing: 12) {
            Image(systemName: symbol(e))
                .font(.title3)
                .foregroundStyle(e.zustand == .fehler ? Theme.bad : Theme.label2)
                .frame(width: 28)
                .accessibilityHidden(true)
            VStack(alignment: .leading, spacing: 4) {
                Text(e.anzeige).font(.body).lineLimit(2)
                Text(unterzeile(e)).font(.caption).foregroundStyle(Theme.label2)
                if e.zustand == .laeuft || e.zustand == .wartet {
                    ProgressView(value: e.anteil)
                }
                if e.zustand == .fehler, let f = e.fehler {
                    Text(f).font(.caption).foregroundStyle(Theme.bad)
                }
            }
            Spacer(minLength: 0)
        }
        .padding(.vertical, 4)
        .contentShape(Rectangle())
        .accessibilityElement(children: .combine)
    }

    private func symbol(_ e: DownloadEintrag) -> String {
        switch e.zustand {
        case .fertig: "play.circle"
        case .laeuft, .wartet: "arrow.down.circle"
        case .fehler: "exclamationmark.triangle"
        }
    }

    private func unterzeile(_ e: DownloadEintrag) -> String {
        var teile: [String] = []
        if let a = Fassungen.aufloesung(e.hoehe) { teile.append(a) }
        teile.append(Downloads.groesseText(max(e.groesse, e.geladen)))
        if e.zustand == .fertig, e.dauer > 0, e.stand > 1 {
            teile.append(String(localized: "noch \(Playback.clock(max(0, e.dauer - e.stand)))", bundle: .lumen, locale: .lumen))
        }
        if session.aktiverServer?.id != e.serverId {
            teile.append(String(localized: "von einem anderen Server", bundle: .lumen, locale: .lumen))
        }
        return teile.joined(separator: " · ")
    }

    private func loeschen(_ stellen: IndexSet, aus liste: [DownloadEintrag]) {
        for i in stellen where liste.indices.contains(i) {
            downloads.loeschen(liste[i].fileId)
        }
    }
}
