// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

import Foundation
#if canImport(UIKit)
import UIKit
#endif
#if canImport(AVFoundation)
import AVFoundation
#endif

/// Was dieses Gerät abspielen kann — und wie es heisst.
///
/// Der Server entscheidet daraufhin, ob er die Datei direkt schickt oder
/// umwandelt. Lieber einmal zu viel umgewandelt als ein schwarzes Bild:
/// darum wird nur aufgezählt, was wirklich sicher ist.
public struct DeviceProfile: Sendable {
    public let name: String
    public let kind: String          // tv | phone | tablet | desktop
    public let codecs: [String]
    public let containers: [String]
    public let audio: [String]
    public let maxHeight: Int
    public let hdr: Bool

    public static let current: DeviceProfile = .detect()

    /// Die Abfrageparameter, die jeder Wiedergabe-Anfrage beiliegen.
    public func query() -> [String: String] {
        [
            "codecs": codecs.joined(separator: ","),
            "containers": containers.joined(separator: ","),
            "audio": audio.joined(separator: ","),
            "maxHeight": String(maxHeight),
            "hdr": hdr ? "1" : "0",
        ]
    }

    /// AVPlayer spielt MP4 und MOV unmittelbar ab. MKV kann es nicht —
    /// dafür gibt es HLS, und das kann jedes Apple-Gerät von Haus aus.
    public static func playsDirectly(container: String?) -> Bool {
        guard let container = container?.lowercased() else { return false }
        return ["mp4", "m4v", "mov"].contains(container)
    }

    // MARK: - Erkennen

    static func detect() -> DeviceProfile {
        #if os(tvOS)
        return DeviceProfile(
            name: deviceName(fallback: "Apple TV"), kind: "tv",
            codecs: hevc ? ["h264", "hevc"] : ["h264"],
            containers: ["mp4", "mov", "hls"],
            audio: ["aac", "ac3", "eac3"],   // Apple TV reicht Tonformate durch
            maxHeight: 2160, hdr: true)
        #elseif os(macOS)
        return DeviceProfile(
            name: deviceName(fallback: "Mac"), kind: "desktop", // sprache: fest
            codecs: hevc ? ["h264", "hevc"] : ["h264"],
            containers: ["mp4", "mov", "hls"],
            audio: ["aac", "ac3", "eac3"],
            maxHeight: 2160, hdr: true)
        #else
        let idiom = UIDevice.current.userInterfaceIdiom
        let isPad = idiom == .pad
        // Mehr als der Bildschirm hergibt, bringt nichts — es kostet nur
        // Leitung und lässt den Server unnötig rechnen.
        let scale = UIScreen.main.nativeBounds.height
        let cap = Int(max(1080, min(2160, scale)))
        return DeviceProfile(
            name: deviceName(fallback: isPad ? "iPad" : "iPhone"),
            kind: isPad ? "tablet" : "phone",
            codecs: hevc ? ["h264", "hevc"] : ["h264"],
            containers: ["mp4", "mov", "hls"],
            audio: ["aac"],
            maxHeight: cap, hdr: true)
        #endif
    }

    /// HEVC kann jedes Apple-Gerät seit 2017; gefragt wird trotzdem, weil
    /// ein altes iPad sonst nur Ton bekäme.
    private static var hevc: Bool {
        #if canImport(AVFoundation)
        return AVURLAsset.isPlayableExtendedMIMEType("video/mp4; codecs=\"hvc1\"")
        #else
        return false
        #endif
    }

    private static func deviceName(fallback: String) -> String {
        #if canImport(UIKit) && !os(tvOS)
        let n = UIDevice.current.name
        return n.isEmpty ? fallback : n
        #elseif os(macOS)
        let n = Host.current().localizedName ?? ""
        return n.isEmpty ? fallback : n
        #else
        return fallback
        #endif
    }
}
