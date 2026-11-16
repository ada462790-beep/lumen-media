// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

import Foundation

/// Wie breit ein Bild angefragt wird (`?w=`). Der Server kennt die Stufen
/// 185, 342, 500, 780, 1280 und 1920 (bildgroesse.go) und rundet auf die
/// nächste auf; die Apps fragen gleich nach einer Stufe, damit dieselbe
/// Karte immer dieselbe Adresse hat und aus dem Zwischenspeicher kommt.
///
/// Vorher fragten die Apps ohne Breite — für jede Kachel ein Poster in
/// 780 und für jede Folge ein Standbild in voller Grösse.
public enum Bildbreite {
    public static let stufen = [185, 342, 500, 780, 1280, 1920]

    /// Die kleinste Stufe, die mindestens `pixel` breit ist.
    public static func stufe(_ pixel: Double) -> Int {
        stufen.first { Double($0) >= pixel } ?? stufen[stufen.count - 1]
    }

    /// Ein Poster in einer Reihe oder einem Raster (rund 130–200 Punkte,
    /// auf dem Fernseher rund 250 Punkte bei doppelter Dichte).
    public static var karte: Int {
        #if os(tvOS)
        return 780
        #else
        return 500
        #endif
    }

    /// Ein breites Bild in einer Karte: Folgen, „Weiterschauen".
    public static var breiteKarte: Int {
        #if os(iOS)
        return 780
        #else
        return 1280
        #endif
    }

    /// Das grosse Bild oben auf Start- und Titelseite.
    public static let kopf = 1920
}

/// Der Zwischenspeicher für Bilder. Poster mit Stand (`?v=`) darf das
/// Gerät ein Jahr aufbewahren (Cache-Control immutable); der Speicher, den
/// iOS von sich aus vorsieht, fasst aber nur wenige Megabyte auf der
/// Platte — nach dem nächsten Öffnen kamen die Poster wieder über die
/// Leitung. `AsyncImage` lädt über `URLSession.shared` und damit über
/// `URLCache.shared`.
public enum Bildspeicher {
    public static func einrichten() {
        let mb = 1024 * 1024
        #if os(tvOS)
        let platte = 256 * mb
        #else
        let platte = 512 * mb
        #endif
        guard URLCache.shared.diskCapacity < platte else { return }
        URLCache.shared = URLCache(memoryCapacity: 64 * mb, diskCapacity: platte, directory: nil)
    }
}
