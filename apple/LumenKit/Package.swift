// swift-tools-version: 5.9
//
// Diese Zeile muss die allererste der Datei sein — SwiftPM liest sie,
// bevor es irgendetwas anderes anschaut. Stand sie weiter unten, galt
// das Paket als uralt und `platforms:` als unbekannt. Die Lizenzkopfzeile
// steht deshalb darunter statt wie überall sonst darüber.
//
// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

import PackageDescription

// LumenKit ist alles, was keine Oberfläche ist: die Verbindung zum Server,
// die Datenmodelle, die Anmeldung und die Entscheidung, wie ein Film
// abgespielt wird.
//
// Dass das ein eigenes Paket ist, hat einen Grund: iPhone, iPad, Mac und
// Apple TV teilen sich denselben Code, und er lässt sich ohne Oberfläche
// testen — `swift test` im Ordner apple/LumenKit genügt.

let package = Package(
    name: "LumenKit",
    // Die Texte im Code sind Schweizer Hochdeutsch („ss"); die übrigen
    // Sprachen stehen im Katalog Resources/Localizable.xcstrings.
    defaultLocalization: "de-CH",
    platforms: [.iOS(.v17), .macOS(.v14), .tvOS(.v17)],
    products: [
        .library(name: "LumenKit", targets: ["LumenKit"]),
    ],
    targets: [
        .target(
            name: "LumenKit",
            // Fehlermeldungen und Zeilen wie „3 neue Folgen" entstehen hier,
            // nicht in einer Ansicht — darum hat LumenKit einen eigenen
            // Katalog. Nachgeschlagen wird er über `Bundle.lumenKit`.
            resources: [.process("Resources")]),
        .testTarget(name: "LumenKitTests", dependencies: ["LumenKit"]),
    ]
)
