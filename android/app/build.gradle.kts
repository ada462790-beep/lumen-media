// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

plugins {
    alias(libs.plugins.android.application)
    alias(libs.plugins.kotlin.android)
    alias(libs.plugins.compose.compiler)
}

android {
    namespace = "org.lumen.app"
    compileSdk = 35

    defaultConfig {
        applicationId = "org.lumen.app"
        minSdk = 26          // Android 8 — deckt praktisch jedes Gerät im Umlauf ab
        targetSdk = 35
        versionCode = 1
        versionName = "0.1.0"
    }

    buildTypes {
        release {
            isMinifyEnabled = true
            isShrinkResources = true
            proguardFiles(getDefaultProguardFile("proguard-android-optimize.txt"),
                          "proguard-rules.pro")
        }
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
        // lumen-core nutzt java.net.http; auf älteren Android-Fassungen
        // bringt die Entzuckerung die nötigen Klassen mit.
        isCoreLibraryDesugaringEnabled = true
    }
    kotlinOptions { jvmTarget = "17" }
    buildFeatures { compose = true }

    sourceSets["main"].kotlin.srcDirs("src/main/kotlin")

    // Die Sprache wechselt zur Laufzeit mit dem Profil, nicht mit dem
    // Gerät. Ein App Bundle darf darum keine Sprachen weglassen — sonst
    // fehlten genau die, die jemand im Profil wählt.
    bundle {
        language { enableSplit = false }
    }

    lint {
        // Eine fehlende Übersetzung ist ein Fehler, keine Warnung.
        error += listOf("MissingTranslation", "ExtraTranslation", "StringFormatMatches",
                        "StringFormatCount", "MissingQuantity", "UnusedQuantity")
    }
}

dependencies {
    implementation(project(":lumen-core"))
    implementation(project(":lumen-texte"))

    implementation(libs.androidx.core.ktx)
    implementation(libs.androidx.lifecycle.runtime.ktx)
    implementation(libs.androidx.lifecycle.runtime.compose)
    implementation(libs.androidx.lifecycle.viewmodel.compose)
    implementation(libs.androidx.activity.compose)

    implementation(platform(libs.androidx.compose.bom))
    implementation(libs.androidx.ui)
    implementation(libs.androidx.ui.graphics)
    implementation(libs.androidx.ui.tooling.preview)
    implementation(libs.androidx.material3)
    implementation(libs.androidx.material.icons)
    debugImplementation(libs.androidx.ui.tooling)

    implementation(libs.androidx.media3.exoplayer)
    implementation(libs.androidx.media3.exoplayer.hls)
    implementation(libs.androidx.media3.ui)
    implementation(libs.androidx.media3.session)

    implementation(libs.coil.compose)
    implementation(libs.androidx.security.crypto)

    // Offline-Downloads: laufen im Hintergrund weiter, auch wenn die App zu ist.
    implementation(libs.androidx.work.runtime.ktx)

    // Chromecast: Knopf, Geräteauswahl und die Sitzung zum Fernseher.
    implementation(libs.play.services.cast.framework)
    implementation(libs.androidx.mediarouter)

    coreLibraryDesugaring(libs.desugar.jdk.libs)
}
