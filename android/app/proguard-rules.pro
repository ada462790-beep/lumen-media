# Lumen — eigener Streaming-Server fürs Heimnetz.
# Copyright (C) 2026 Die Lumen-Autoren
#
# Veröffentlicht unter der GNU Affero General Public License v3.0.
# Der vollständige Text steht in der Datei LICENSE.
# SPDX-License-Identifier: AGPL-3.0-only

# Regeln fürs Schrumpfen der Release-Fassung.
#
# Die Datei war in build.gradle.kts eingetragen, lag aber nicht da. Beim
# Debug-Bau fällt das nicht auf, weil dort gar nicht geschrumpft wird —
# erst `assembleRelease` wäre darüber gestolpert.
#
# Lumen braucht wenig: es gibt keine Spiegelung (Reflection), keine
# Bibliothek, die Klassen über ihren Namen sucht, und kein JSON-Werkzeug,
# das Felder zuordnet — der JSON-Leser in lumen-core liest von Hand.

# Die Modelle werden nicht über ihren Namen angesprochen, dürfen also
# umbenannt werden. Ihre Felder aber stehen in den Antworten des Servers,
# und `data class`-Methoden wie toString() helfen beim Suchen von Fehlern.
-keepclassmembers class org.lumen.core.** { <fields>; }

# Media3/ExoPlayer sucht seine Auswerter über den Namen.
-keep class androidx.media3.** { *; }
-dontwarn androidx.media3.**

# Zeilennummern behalten, sonst ist jeder Absturzbericht unlesbar.
-keepattributes SourceFile,LineNumberTable
-renamesourcefileattribute SourceFile

# Das Cast-SDK sucht seine Einstellungen über den Namen aus dem Manifest.
-keep class org.lumen.app.LumenCastOptions { *; }

# WorkManager legt den Download-Arbeiter über seinen Namen an.
-keep class org.lumen.app.DownloadArbeiter { *; }
