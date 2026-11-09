// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

// Reines Kotlin, kein Android. Darum lässt sich dieser Teil überall
// übersetzen und ohne Gerät testen — und genau das passiert auch:
// src/test/kotlin/Tests.kt läuft gegen einen echten Server.

plugins {
    alias(libs.plugins.kotlin.jvm)
}

kotlin {
    jvmToolchain(17)
}

// Keine Abhängigkeiten. Das ist Absicht und soll so bleiben — auch keine
// Test-Bibliothek: die Prüfungen in src/test/kotlin sind ein Programm mit
// einer main-Funktion, das selbst zählt und am Ende sagt, was nicht stimmt.
dependencies {}

sourceSets {
    main { kotlin.srcDirs("src/main/kotlin") }

    // Ein eigener Satz statt des üblichen „test": der übliche erwartet
    // JUnit, und ohne JUnit liefe `gradle test` stillschweigend durch,
    // ohne eine einzige Behauptung zu prüfen. Das wäre schlimmer als gar
    // kein Testlauf, weil es nach einem aussieht.
    create("pruefung") {
        kotlin.srcDirs("src/test/kotlin")
        compileClasspath += sourceSets["main"].output
        runtimeClasspath += sourceSets["main"].output
    }
}

/**
 * Die Prüfungen laufen lassen.
 *
 *     gradle :lumen-core:pruefen
 *     gradle :lumen-core:pruefen -Pserver=http://192.168.1.20:8097
 *
 * Ohne Serveradresse laufen nur die Prüfungen, die keinen brauchen. Mit
 * einer läuft der ganze Ablauf dagegen: anmelden, Startseite, Abspielplan,
 * Fortschritt, Merkliste, abmelden.
 */
val pruefen by tasks.registering(JavaExec::class) {
    group = "verification"
    description = "Prüft lumen-core; mit -Pserver=… auch gegen einen laufenden Server."
    mainClass.set("TestsKt")
    classpath = sourceSets["pruefung"].runtimeClasspath
    (project.findProperty("server") as String?)?.let { args(it) }
    (project.findProperty("tls") as String?)?.let { args(it) }
    (project.findProperty("fingerabdruck") as String?)?.let { args(it) }
}

// `gradle check` soll sie mitnehmen — sonst vergisst man sie.
tasks.named("check") { dependsOn(pruefen) }
