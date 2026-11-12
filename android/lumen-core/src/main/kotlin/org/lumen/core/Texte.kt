// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package org.lumen.core

import java.util.Locale

/**
 * Die Sätze, die lumen-core selbst bildet: Abzeichen, Knöpfe der
 * Titelseite, Fehlermeldungen, Prüfungen vor dem Abschicken.
 *
 * lumen-core kennt kein Android und damit keine strings.xml. Darum steht
 * hier zu jedem Satz eine Kennung ([Tx], [TxN]) samt der Schweizer
 * Fassung als Vorgabe. Die Apps setzen beim Start einen [Uebersetzer], der
 * die Kennung in ihre Ressourcen (lumen-texte, `kern_…`) übersetzt —
 * Deutsch (Schweiz, Deutschland, Österreich) oder Englisch (USA, GB).
 * Ohne Übersetzer, etwa in den Prüfungen ohne Gerät, gilt die Vorgabe.
 *
 * Platzhalter wie in Android: `%1$s`, in der Mehrzahl `%1$d`; ein
 * Prozentzeichen ist `%%`. tools/sprache_android_pruefen.py hält die
 * Vorgaben mit lumen-texte/src/main/res/values/strings.xml gleich.
 */
enum class Tx(val vorgabe: String) {
    // Folgen und Staffeln
    SPECIALS("Specials"),
    STAFFEL_N("Staffel %1\$s"),
    FOLGE_N("Folge %1\$s"),

    // Anmelden mit Code
    CODE_ABGELAUFEN("Abgelaufen — ein neuer Code kommt gleich"),
    CODE_NOCH_GUELTIG("Noch %1\$s gültig"),
    CODE_ERKLAERUNG("Öffne Lumen auf dem Handy oder im Browser → Profil → Fernseher anmelden und gib den Code ein."),
    CODE_ERKLAERUNG_HANDY("Öffne Lumen auf einem anderen Gerät, auf dem du schon angemeldet bist → Profil → Fernseher anmelden und gib den Code ein."),
    GERAET_ANGEMELDET("%1\$s ist angemeldet"),
    DAS_GERAET("Das Gerät"),
    CODE_SECHS_ZIFFERN("Der Code hat sechs Ziffern."),

    // Abzeichen und Technik
    NEU("Neu"),
    UNTERTITEL("Untertitel"),
    UNTERTITEL_SPRACHEN("Untertitel %1\$s"),
    DIREKT_ABSPIELBAR("Direkt abspielbar"),
    WIRD_UMGEWANDELT("Wird umgewandelt"),
    STEREO("Stereo"),
    GROESSE_KLEIN("Klein"),
    GROESSE_NORMAL("Normal"),
    GROESSE_GROSS("Gross"),

    // Zahlen und Daten
    MUSTER_TAG_MONAT("d. MMM"),
    MUSTER_DATUM("d.M.yyyy"),
    DEZIMALZEICHEN(","),
    TAUSENDERZEICHEN("’"),
    ZITAT("„%1\$s“"),

    // Verbindung und Fehler
    FEHLER_ALLGEMEIN("Das hat nicht geklappt."),
    SERVER_NICHT_ERREICHBAR("Der Server ist nicht erreichbar. Läuft er, und ist dieses Gerät im gleichen Netz?"),
    ANMELDUNG_UNGUELTIG("Die Anmeldung gilt nicht mehr."),
    ANTWORT_UNLESBAR("Die Antwort von %1\$s war nicht lesbar. Vermutlich passen App und Server nicht zusammen."),
    ZERTIFIKAT_UNBEKANNT("Dieser Server weist sich mit einem Zertifikat aus, das dieses Gerät nicht kennt — normal, wenn er es selbst ausgestellt hat. Trag den Fingerabdruck ein, den der Server unter „Von aussen erreichbar“ anzeigt."),
    ZERTIFIKAT_FEHLT("Der Server hat kein Zertifikat geschickt."),
    ZERTIFIKAT_ANDERS("Dieser Server meldet sich mit einem anderen Zertifikat als dem gespeicherten. Entweder hat sich der Server geändert — oder es ist nicht deiner."),
    ADRESSE_UNGUELTIG("Das sieht nicht nach einer Adresse aus."),
    WIE_HEISST_PROFIL("Wie heisst dein Profil?"),
    UNTERWEGS_NUR_MIT_GEHEIMNIS("Von unterwegs geht es nur mit PIN oder Passwort."),

    // Chromecast
    CAST_UNTERWEGS("Auf einen Chromecast schicken geht nur, wenn dein Server im selben Heimnetz erreichbar ist. Von unterwegs (über den Vermittler) kann der Chromecast den Film nicht selbst holen."),
    CAST_NICHT_IM_NETZ("Dein Server ist unter einer Adresse ausserhalb des Heimnetzes eingetragen. Ein Chromecast kann ihn so nicht erreichen — verbinde Lumen mit der Adresse im Heimnetz, etwa 192.168.1.20:8097."),

    // Fassungen und Downloads
    TEIL_X_VON_Y("Teil %1\$s von %2\$s"),
    DOWNLOAD_NICHT_FREIGEGEBEN("Für dieses Profil sind Downloads nicht freigegeben."),
    DOWNLOAD_ZU_VIELE("Es laufen gerade zu viele Downloads. Versuch es später noch einmal."),
    DOWNLOAD_FEHLER("Der Download hat nicht geklappt."),

    // Titel und Personen
    SEIT_JAHR("seit %1\$s"),
    SERIE_X_VON_Y("%1\$s von %2\$s Folgen"),
    AB_ALTER("ab %1\$s"),
    DAUER_STD_MIN("%1\$s Std. %2\$s Min."),
    DAUER_MIN("%1\$s Min."),
    GEBOREN("geboren %1\$s"),
    GEBOREN_IN("geboren %1\$s in %2\$s"),
    GESTORBEN("gestorben %1\$s"),
    QUELLE("Quelle: %1\$s"),
    NUR_ENGLISCH("Biografie nur auf Englisch verfügbar"),
    MIT_PERSON("mit %1\$s"),
    ANSEHEN("Ansehen"),
    WEITER("Weiter"),
    WEITER_MIT("Weiter mit %1\$s"),
    WEITER_BEI("Weiter bei %1\$s"),
    WEITERSCHAUEN("Weiterschauen"),
    N_GESEHEN("%1\$s gesehen"),
    REIHE_TMDB("zusammengestellt nach TMDb"),
    REIHE_LUMEN("von Lumen am Titel erkannt"),
    TOMATEN("%1\$s %% Rotten Tomatoes"),
    DATEI_FEHLT("fehlt"),

    // Geräte und Wiedergabe
    GERAET_TV("Fernseher"),
    GERAET_HANDY("Handy"),
    GERAET_TABLET("Tablet"),
    GERAET_BROWSER("Browser"),
    GERAET("Gerät"),
    WIEDERGABE_UMGEWANDELT("wird umgewandelt"),
    WIEDERGABE_DIREKT("direkt"),

    // Wünsche
    WUNSCH_OFFEN("offen"),
    WUNSCH_IN_ARBEIT("in Arbeit"),
    WUNSCH_ERFUELLT("erfüllt"),
    WUNSCH_ABGELEHNT("abgelehnt"),
    VON_X("von %1\$s"),
    VON_X_DU("von %1\$s (du)"),

    // Spuren
    SPRACHE_UNBEKANNT("Unbekannt"),
    SPUR_ERZWUNGEN("erzwungen"),
    SPUR_HOERGESCHAEDIGT("für Hörgeschädigte"),
    SPUR_DATEI("Datei"),

    // Sortierung
    SORT_TITEL("Titel"),
    SORT_HINZUGEFUEGT("Hinzugefügt"),
    SORT_ERSCHIENEN("Erscheinungsdatum"),
    SORT_BEWERTUNG("Bewertung"),
    SORT_LAUFZEIT("Laufzeit"),
    SORT_ZULETZT_GESEHEN("Zuletzt gesehen"),
    SORT_UNGESEHEN("Ungesehene zuerst"),
    SORT_ZUFALL("Zufällig"),
    SORT_AUFSTEIGEND("Aufsteigend"),
    SORT_ABSTEIGEND("Absteigend"),
    SORT_NEU_MISCHEN("Neu mischen"),

    // Schutz und Einladung
    PIN("PIN"),
    PASSWORT("Passwort"),
    OHNE("Ohne"),
    KEIN_SCHUTZ("Kein Schutz"),
    ZUERST_BISHERIGER_PIN("Gib zuerst deinen bisherigen PIN ein."),
    ZUERST_BISHERIGES_PASSWORT("Gib zuerst dein bisheriges Passwort ein."),
    UNBEKANNTE_SCHUTZART("Unbekannte Art, das Profil zu schützen."),
    PIN_ZU_KURZ("Ein PIN hat mindestens vier Ziffern."),
    PASSWORT_ZU_KURZ("Ein Passwort hat mindestens acht Zeichen."),
    EINGABEN_UNGLEICH("Die beiden Eingaben stimmen nicht überein."),
    NEUE_PIN_FESTLEGEN("Neue PIN festlegen"),
    NEUES_PASSWORT_FESTLEGEN("Neues Passwort festlegen"),
    NOCH_EINMAL("Noch einmal"),
    BISHERIGES_FALSCH("Der bisherige PIN oder das bisherige Passwort stimmt nicht."),
    VOLLZUGRIFF_BRAUCHT_SCHUTZ("Mit Vollzugriff muss dein Profil geschützt bleiben. Wähle PIN oder Passwort."),
    ZU_VIELE_VERSUCHE("Zu viele Versuche. Warte einen Moment."),
    EINLADUNG_OHNE_CODE("Ohne Einladungscode geht es nicht."),
    WIE_HEISST_DU("Wie heisst du? Der Name steht dann auf deinem Profil."),
    UNBEKANNTE_ANMELDEART("Unbekannte Art, dich anzumelden."),
    HINWEIS_PIN("Mindestens vier Ziffern."),
    HINWEIS_PASSWORT("Mindestens acht Zeichen."),
    HINWEIS_OFFEN("Dieses Profil ist ohne Code offen. Gut für den Fernseher im Wohnzimmer, weniger gut für ein Gerät, das herumliegt."),

    // Einstellungen
    WIE_IN_DER_DATEI("Wie in der Datei"),
    AUS("Aus"),
    ROLLE_BESITZER("Besitzer dieses Servers"),
    ROLLE_VOLLZUGRIFF("Vollzugriff"),
    ROLLE_KIND("Kinderprofil"),
    ROLLE_HAUSHALT("Haushalt"),

    // Bildschirmzeit der Kinderprofile (unter „Ich")
    BZ_SCHLUSS("Für heute ist Schluss."),
    BZ_HEUTE_NOCH("Heute noch %1\$d Min."),
    BZ_OHNE_GRENZE("Heute ohne Grenze"),
    BZ_GEBRAUCHT("%1\$d von %2\$d Min. gebraucht"),
    BZ_FENSTER("heute %1\$s bis %2\$s"),
}

/** Sätze mit Mehrzahl: [eins] für genau eins, [mehr] sonst — wie `<plurals>` in Android. */
enum class TxN(val eins: String, val mehr: String) {
    NEUE_FOLGEN("Neue Folge", "%1\$d neue Folgen"),
    SERIE_ALLE_GESEHEN("%1\$d Folge gesehen", "alle %1\$d Folgen gesehen"),
    TITEL_ANZAHL("%1\$d Titel", "%1\$d Titel"),
    TREFFER("%1\$d Treffer", "%1\$d Treffer"),
    FILME_ANZAHL("%1\$d Film", "%1\$d Filme"),
    REIHEN_ANZAHL("%1\$d Reihe", "%1\$d Reihen"),
}

/** Übersetzt Kennungen in die Sprache der Oberfläche — von der App gesetzt. */
interface Uebersetzer {
    fun text(k: Tx, args: Array<out Any?>): String
    fun mehrzahl(k: TxN, anzahl: Int, args: Array<out Any?>): String
}

object Texte {
    /** Ohne Übersetzer gelten die Vorgaben (de-CH). */
    @Volatile var uebersetzer: Uebersetzer? = null

    /**
     * Die Sprache der Oberfläche als BCP-47-Kennung („de-CH", „en-GB").
     * Bestimmt Monatsnamen, Sprachnamen der Spuren und den Kopf
     * Accept-Language, mit dem der Server seine Meldungen übersetzt.
     */
    @Volatile var sprache: String = VORGABE

    val locale: Locale get() = Locale.forLanguageTag(sprache)

    fun t(k: Tx, vararg a: Any?): String =
        uebersetzer?.text(k, a) ?: formatieren(k.vorgabe, a)

    fun n(k: TxN, anzahl: Int, vararg a: Any?): String {
        uebersetzer?.let { return it.mehrzahl(k, anzahl, a) }
        val muster = if (anzahl == 1) k.eins else k.mehr
        return formatieren(muster, if (a.isEmpty()) arrayOf(anzahl) else a)
    }

    /** Wie Android: ohne Argumente wird nichts ersetzt, sonst `String.format`. */
    fun formatieren(muster: String, a: Array<out Any?>): String =
        if (a.isEmpty()) muster else String.format(locale, muster, *a)

    const val VORGABE = "de-CH"
}
