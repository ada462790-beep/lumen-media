// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package org.lumen.texte

import org.lumen.core.Texte
import org.lumen.core.Tx
import org.lumen.core.TxN
import org.lumen.core.Uebersetzer

/**
 * Macht aus den Kennungen von lumen-core Text aus den Ressourcen — in der
 * Sprache, die [Sprachwahl] gerade eingestellt hat.
 *
 * Ein `when` über alle Kennungen statt eines Nachschlagens nach Namen:
 * fehlt eine, übersetzt das Programm nicht, und die Ressourcen überstehen
 * das Verkleinern beim Bauen der Release-Fassung.
 */
internal object KernUebersetzer : Uebersetzer {
    override fun text(k: Tx, args: Array<out Any?>): String {
        val res = Sprachwahl.ressourcen ?: return Texte.formatieren(k.vorgabe, args)
        val id = id(k)
        return if (args.isEmpty()) res.getString(id) else res.getString(id, *args)
    }

    override fun mehrzahl(k: TxN, anzahl: Int, args: Array<out Any?>): String {
        val a: Array<out Any?> = if (args.isEmpty()) arrayOf(anzahl) else args
        val res = Sprachwahl.ressourcen
            ?: return Texte.formatieren(if (anzahl == 1) k.eins else k.mehr, a)
        return res.getQuantityString(id(k), anzahl, *a)
    }

    private fun id(k: Tx): Int = when (k) {
        Tx.SPECIALS -> R.string.kern_specials
        Tx.STAFFEL_N -> R.string.kern_staffel_n
        Tx.FOLGE_N -> R.string.kern_folge_n
        Tx.CODE_ABGELAUFEN -> R.string.kern_code_abgelaufen
        Tx.CODE_NOCH_GUELTIG -> R.string.kern_code_noch_gueltig
        Tx.CODE_ERKLAERUNG -> R.string.kern_code_erklaerung
        Tx.CODE_ERKLAERUNG_HANDY -> R.string.kern_code_erklaerung_handy
        Tx.GERAET_ANGEMELDET -> R.string.kern_geraet_angemeldet
        Tx.DAS_GERAET -> R.string.kern_das_geraet
        Tx.CODE_SECHS_ZIFFERN -> R.string.kern_code_sechs_ziffern
        Tx.NEU -> R.string.kern_neu
        Tx.UNTERTITEL -> R.string.kern_untertitel
        Tx.UNTERTITEL_SPRACHEN -> R.string.kern_untertitel_sprachen
        Tx.DIREKT_ABSPIELBAR -> R.string.kern_direkt_abspielbar
        Tx.WIRD_UMGEWANDELT -> R.string.kern_wird_umgewandelt
        Tx.STEREO -> R.string.kern_stereo
        Tx.GROESSE_KLEIN -> R.string.kern_groesse_klein
        Tx.GROESSE_NORMAL -> R.string.kern_groesse_normal
        Tx.GROESSE_GROSS -> R.string.kern_groesse_gross
        Tx.MUSTER_TAG_MONAT -> R.string.kern_muster_tag_monat
        Tx.MUSTER_DATUM -> R.string.kern_muster_datum
        Tx.DEZIMALZEICHEN -> R.string.kern_dezimalzeichen
        Tx.TAUSENDERZEICHEN -> R.string.kern_tausenderzeichen
        Tx.ZITAT -> R.string.kern_zitat
        Tx.FEHLER_ALLGEMEIN -> R.string.kern_fehler_allgemein
        Tx.SERVER_NICHT_ERREICHBAR -> R.string.kern_server_nicht_erreichbar
        Tx.ANMELDUNG_UNGUELTIG -> R.string.kern_anmeldung_ungueltig
        Tx.ANTWORT_UNLESBAR -> R.string.kern_antwort_unlesbar
        Tx.ZERTIFIKAT_UNBEKANNT -> R.string.kern_zertifikat_unbekannt
        Tx.ZERTIFIKAT_FEHLT -> R.string.kern_zertifikat_fehlt
        Tx.ZERTIFIKAT_ANDERS -> R.string.kern_zertifikat_anders
        Tx.ADRESSE_UNGUELTIG -> R.string.kern_adresse_ungueltig
        Tx.WIE_HEISST_PROFIL -> R.string.kern_wie_heisst_profil
        Tx.UNTERWEGS_NUR_MIT_GEHEIMNIS -> R.string.kern_unterwegs_nur_mit_geheimnis
        Tx.CAST_UNTERWEGS -> R.string.kern_cast_unterwegs
        Tx.CAST_NICHT_IM_NETZ -> R.string.kern_cast_nicht_im_netz
        Tx.TEIL_X_VON_Y -> R.string.kern_teil_x_von_y
        Tx.DOWNLOAD_NICHT_FREIGEGEBEN -> R.string.kern_download_nicht_freigegeben
        Tx.DOWNLOAD_ZU_VIELE -> R.string.kern_download_zu_viele
        Tx.DOWNLOAD_FEHLER -> R.string.kern_download_fehler
        Tx.SEIT_JAHR -> R.string.kern_seit_jahr
        Tx.SERIE_X_VON_Y -> R.string.kern_serie_x_von_y
        Tx.AB_ALTER -> R.string.kern_ab_alter
        Tx.DAUER_STD_MIN -> R.string.kern_dauer_std_min
        Tx.DAUER_MIN -> R.string.kern_dauer_min
        Tx.GEBOREN -> R.string.kern_geboren
        Tx.GEBOREN_IN -> R.string.kern_geboren_in
        Tx.GESTORBEN -> R.string.kern_gestorben
        Tx.QUELLE -> R.string.kern_quelle
        Tx.NUR_ENGLISCH -> R.string.kern_nur_englisch
        Tx.MIT_PERSON -> R.string.kern_mit_person
        Tx.ANSEHEN -> R.string.kern_ansehen
        Tx.WEITER -> R.string.kern_weiter
        Tx.WEITER_MIT -> R.string.kern_weiter_mit
        Tx.WEITER_BEI -> R.string.kern_weiter_bei
        Tx.WEITERSCHAUEN -> R.string.kern_weiterschauen
        Tx.N_GESEHEN -> R.string.kern_n_gesehen
        Tx.REIHE_TMDB -> R.string.kern_reihe_tmdb
        Tx.REIHE_LUMEN -> R.string.kern_reihe_lumen
        Tx.TOMATEN -> R.string.kern_tomaten
        Tx.DATEI_FEHLT -> R.string.kern_datei_fehlt
        Tx.GERAET_TV -> R.string.kern_geraet_tv
        Tx.GERAET_HANDY -> R.string.kern_geraet_handy
        Tx.GERAET_TABLET -> R.string.kern_geraet_tablet
        Tx.GERAET_BROWSER -> R.string.kern_geraet_browser
        Tx.GERAET -> R.string.kern_geraet
        Tx.WIEDERGABE_UMGEWANDELT -> R.string.kern_wiedergabe_umgewandelt
        Tx.WIEDERGABE_DIREKT -> R.string.kern_wiedergabe_direkt
        Tx.WUNSCH_OFFEN -> R.string.kern_wunsch_offen
        Tx.WUNSCH_IN_ARBEIT -> R.string.kern_wunsch_in_arbeit
        Tx.WUNSCH_ERFUELLT -> R.string.kern_wunsch_erfuellt
        Tx.WUNSCH_ABGELEHNT -> R.string.kern_wunsch_abgelehnt
        Tx.VON_X -> R.string.kern_von_x
        Tx.VON_X_DU -> R.string.kern_von_x_du
        Tx.SPRACHE_UNBEKANNT -> R.string.kern_sprache_unbekannt
        Tx.SPUR_ERZWUNGEN -> R.string.kern_spur_erzwungen
        Tx.SPUR_HOERGESCHAEDIGT -> R.string.kern_spur_hoergeschaedigt
        Tx.SPUR_DATEI -> R.string.kern_spur_datei
        Tx.SORT_TITEL -> R.string.kern_sort_titel
        Tx.SORT_HINZUGEFUEGT -> R.string.kern_sort_hinzugefuegt
        Tx.SORT_ERSCHIENEN -> R.string.kern_sort_erschienen
        Tx.SORT_BEWERTUNG -> R.string.kern_sort_bewertung
        Tx.SORT_LAUFZEIT -> R.string.kern_sort_laufzeit
        Tx.SORT_ZULETZT_GESEHEN -> R.string.kern_sort_zuletzt_gesehen
        Tx.SORT_UNGESEHEN -> R.string.kern_sort_ungesehen
        Tx.SORT_ZUFALL -> R.string.kern_sort_zufall
        Tx.SORT_AUFSTEIGEND -> R.string.kern_sort_aufsteigend
        Tx.SORT_ABSTEIGEND -> R.string.kern_sort_absteigend
        Tx.SORT_NEU_MISCHEN -> R.string.kern_sort_neu_mischen
        Tx.PIN -> R.string.kern_pin
        Tx.PASSWORT -> R.string.kern_passwort
        Tx.OHNE -> R.string.kern_ohne
        Tx.KEIN_SCHUTZ -> R.string.kern_kein_schutz
        Tx.ZUERST_BISHERIGER_PIN -> R.string.kern_zuerst_bisheriger_pin
        Tx.ZUERST_BISHERIGES_PASSWORT -> R.string.kern_zuerst_bisheriges_passwort
        Tx.UNBEKANNTE_SCHUTZART -> R.string.kern_unbekannte_schutzart
        Tx.PIN_ZU_KURZ -> R.string.kern_pin_zu_kurz
        Tx.PASSWORT_ZU_KURZ -> R.string.kern_passwort_zu_kurz
        Tx.EINGABEN_UNGLEICH -> R.string.kern_eingaben_ungleich
        Tx.NEUE_PIN_FESTLEGEN -> R.string.kern_neue_pin_festlegen
        Tx.NEUES_PASSWORT_FESTLEGEN -> R.string.kern_neues_passwort_festlegen
        Tx.NOCH_EINMAL -> R.string.kern_noch_einmal
        Tx.BISHERIGES_FALSCH -> R.string.kern_bisheriges_falsch
        Tx.VOLLZUGRIFF_BRAUCHT_SCHUTZ -> R.string.kern_vollzugriff_braucht_schutz
        Tx.ZU_VIELE_VERSUCHE -> R.string.kern_zu_viele_versuche
        Tx.EINLADUNG_OHNE_CODE -> R.string.kern_einladung_ohne_code
        Tx.WIE_HEISST_DU -> R.string.kern_wie_heisst_du
        Tx.UNBEKANNTE_ANMELDEART -> R.string.kern_unbekannte_anmeldeart
        Tx.HINWEIS_PIN -> R.string.kern_hinweis_pin
        Tx.HINWEIS_PASSWORT -> R.string.kern_hinweis_passwort
        Tx.HINWEIS_OFFEN -> R.string.kern_hinweis_offen
        Tx.WIE_IN_DER_DATEI -> R.string.kern_wie_in_der_datei
        Tx.AUS -> R.string.kern_aus
        Tx.ROLLE_BESITZER -> R.string.kern_rolle_besitzer
        Tx.ROLLE_VOLLZUGRIFF -> R.string.kern_rolle_vollzugriff
        Tx.ROLLE_KIND -> R.string.kern_rolle_kind
        Tx.ROLLE_HAUSHALT -> R.string.kern_rolle_haushalt
        Tx.BZ_SCHLUSS -> R.string.kern_bz_schluss
        Tx.BZ_HEUTE_NOCH -> R.string.kern_bz_heute_noch
        Tx.BZ_OHNE_GRENZE -> R.string.kern_bz_ohne_grenze
        Tx.BZ_GEBRAUCHT -> R.string.kern_bz_gebraucht
        Tx.BZ_FENSTER -> R.string.kern_bz_fenster
    }

    private fun id(k: TxN): Int = when (k) {
        TxN.NEUE_FOLGEN -> R.plurals.kern_neue_folgen
        TxN.SERIE_ALLE_GESEHEN -> R.plurals.kern_serie_alle_gesehen
        TxN.TITEL_ANZAHL -> R.plurals.kern_titel_anzahl
        TxN.TREFFER -> R.plurals.kern_treffer
        TxN.FILME_ANZAHL -> R.plurals.kern_filme_anzahl
        TxN.REIHEN_ANZAHL -> R.plurals.kern_reihen_anzahl
    }
}
