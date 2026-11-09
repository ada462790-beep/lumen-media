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
    namespace = "org.lumen.tv"
    compileSdk = 35

    defaultConfig {
        applicationId = "org.lumen.tv"
        minSdk = 26
        targetSdk = 35
        versionCode = 1
        versionName = "0.1.0"
    }

    buildTypes {
        release {
            isMinifyEnabled = true
            proguardFiles(getDefaultProguardFile("proguard-android-optimize.txt"),
                          "proguard-rules.pro")
        }
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
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
    implementation(libs.androidx.activity.compose)

    implementation(platform(libs.androidx.compose.bom))
    implementation(libs.androidx.ui)
    implementation(libs.androidx.material3)

    // Eigene Bausteine für den Fernseher: auf dem Sofa gibt es keinen
    // Zeiger, sondern einen Fokus, der springt.
    implementation(libs.androidx.tv.foundation)
    implementation(libs.androidx.tv.material)

    implementation(libs.androidx.media3.exoplayer)
    implementation(libs.androidx.media3.exoplayer.hls)
    implementation(libs.androidx.media3.ui)

    implementation(libs.coil.compose)
    implementation(libs.androidx.security.crypto)

    coreLibraryDesugaring(libs.desugar.jdk.libs)
}
