// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

// Lumen für Android.
//
// Vier Teile:
//   lumen-core   reines Kotlin ohne Android — Verbindung, Modelle, Logik.
//                Lässt sich mit `kotlinc` übersetzen und mit `java -jar`
//                testen, ohne Gerät und ohne Gradle.
//   lumen-texte  die Übersetzungen der Sätze aus lumen-core und die Wahl
//                der Sprache — für beide Apps (siehe BEGRIFFE.md)
//   app          Handy und Tablet
//   tv           Android TV und Fire TV
//
// lumen-core hat bewusst keine einzige Abhängigkeit: eigener JSON-Leser,
// HTTP über das JDK. Das hält es prüfbar und klein.

plugins {
    alias(libs.plugins.android.application) apply false
    alias(libs.plugins.android.library) apply false
    alias(libs.plugins.kotlin.android) apply false
    alias(libs.plugins.kotlin.jvm) apply false
    alias(libs.plugins.compose.compiler) apply false
}
