// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

// Die Sprachen der beiden Apps an einer Stelle: die Übersetzungen der
// Sätze aus lumen-core (kern_…), die Wahl der Sprache zur Laufzeit und
// was Telefon und Fernseher dafür gemeinsam brauchen.
//
// lumen-core bleibt reines Kotlin ohne Android; es liefert Kennungen
// (Tx, TxN), und dieser Teil macht daraus Text aus strings.xml.

plugins {
    alias(libs.plugins.android.library)
    alias(libs.plugins.kotlin.android)
}

android {
    namespace = "org.lumen.texte"
    compileSdk = 35

    defaultConfig {
        minSdk = 26
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
        isCoreLibraryDesugaringEnabled = true
    }
    kotlinOptions { jvmTarget = "17" }

    sourceSets["main"].kotlin.srcDirs("src/main/kotlin")

    lint {
        // Eine fehlende Übersetzung ist ein Fehler, keine Warnung.
        error += listOf("MissingTranslation", "ExtraTranslation", "StringFormatMatches",
                        "StringFormatCount", "MissingQuantity", "UnusedQuantity")
    }
}

dependencies {
    api(project(":lumen-core"))
    implementation(libs.androidx.core.ktx)
    coreLibraryDesugaring(libs.desugar.jdk.libs)
}
