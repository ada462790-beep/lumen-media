// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package org.lumen.core

/**
 * Was nach dem Ende eines Titels als Nächstes läuft.
 *
 * Zwei Fälle: eine Liste, die am Stück durchläuft, und eine Serie, bei der
 * nach einer Folge die nächste kommt. Beides entscheidet sich hier, ohne
 * Netz und ohne Android — die Daten kommen als Werte und Funktionen herein.
 * So lässt sich jeder Fall in Tests.kt nachstellen, und Telefon und
 * Fernseher entscheiden garantiert gleich.
 *
 * Die Folgen einer Serie kommen als [Sequence]: eine Staffel wird erst
 * geholt, wenn die vorige keine offene Folge mehr hat. Bei einer Serie mit
 * zehn Staffeln wären es sonst zehn Abfragen, wo meist eine reicht.
 */
object AmStueck {

    /** Wie lange „Als Nächstes" stehen bleibt, bevor es von selbst weitergeht. */
    const val COUNTDOWN_SEKUNDEN = 5

    /** Ein Treffer: der Eintrag an [index] der Liste, und was davon läuft. */
    data class Naechstes(
        // -1, wenn es um keine Liste geht, sondern um die nächste Folge.
        val index: Int,
        // Bei einem Film der Film, bei einer Serie die Folge.
        val titel: Item,
        // Was über „Als Nächstes:" steht.
        val beschriftung: String,
    )

    /**
     * Die Staffeln in der Reihenfolge zum Durchschauen. Staffel 0 sind die
     * Specials — die gehören ans Ende, nicht vor die erste Folge. Der
     * Server schickt sie schon so (store.folgenOrdnung), wie Browser und
     * Apple-Apps sie zeigen; hier bleibt es zur Sicherheit, falls ein
     * älterer Server antwortet.
     */
    fun staffelnOrdnen(staffeln: List<Item>): List<Item> =
        staffeln.sortedWith(compareBy<Item>({ istSpecials(it) }, { it.seasonNo ?: Int.MAX_VALUE }))

    /**
     * Ob eine Staffel die Specials sind. Der Server lässt die Nummer 0 im
     * JSON weg (omitempty) — eine Staffel ohne Nummer ist darum Staffel 0,
     * genau wie der Browser es liest.
     */
    fun istSpecials(staffel: Item): Boolean = (staffel.seasonNo ?: 0) == 0

    /** Wie eine Staffel im Folgen-Feld heisst: Staffel 0 sind die Specials. */
    fun staffelName(staffel: Item): String =
        if (istSpecials(staffel)) Texte.t(Tx.SPECIALS) else staffel.title

    /** Die Folgen einer Staffel nach Nummer; ohne Nummer (oder 0) ans Ende,
     *  bei Gleichstand in der Reihenfolge des Servers — wie überall. */
    fun folgenOrdnen(folgen: List<Item>): List<Item> =
        folgen.sortedBy { f -> f.episodeNo?.takeIf { it > 0 } ?: Int.MAX_VALUE }

    /** Alle Folgen einer Serie hintereinander, Staffel für Staffel. */
    fun folgenDerSerie(staffeln: List<Item>, folgenDerStaffel: (Item) -> List<Item>): Sequence<Item> =
        staffelnOrdnen(staffeln).asSequence().flatMap { staffel ->
            folgenOrdnen(folgenDerStaffel(staffel)).asSequence()
        }

    /**
     * Die erste Folge, die noch nicht fertig geschaut ist und sich abspielen
     * lässt. Null, wenn alles gesehen ist oder keine Datei da ist.
     */
    fun ersteOffeneFolge(folgen: Sequence<Item>, spielbar: (Item) -> Boolean = { true }): Item? =
        folgen.firstOrNull { !it.finished && spielbar(it) }

    /**
     * Die Folge nach [aktuellId] — über das Staffelende hinweg. Ob sie schon
     * gesehen ist, spielt keine Rolle: wer Folge 3 noch einmal schaut, will
     * danach Folge 4 und nicht irgendeine ungesehene weiter hinten.
     */
    fun folgeNach(folgen: Sequence<Item>, aktuellId: String,
                  spielbar: (Item) -> Boolean = { true }): Item? {
        var aktuell: Item? = null
        for (folge in folgen) {
            val vorher = aktuell
            if (vorher != null) {
                // Aus einer regulären Staffel nicht von selbst in die
                // Specials — wie im Browser und auf Apple-Geräten.
                if (!istSpecials(vorher) && istSpecials(folge)) return null
                if (spielbar(folge)) return folge
            } else if (folge.id == aktuellId) {
                aktuell = folge
            }
        }
        return null
    }

    /**
     * Der nächste abspielbare Eintrag einer Liste, ab [ab] einschliesslich.
     *
     * Ein Film läuft, wenn er eine Datei hat — auch wenn er schon gesehen
     * ist: wer eine Liste abspielt, will die Liste sehen. Eine Serie steht
     * für ihre erste offene Folge, genau eine; danach geht es in der Liste
     * weiter. Sonst käme man aus einer Serie mit vierzig Folgen nie mehr
     * heraus. Eine Serie ohne offene Folge wird übersprungen.
     */
    fun naechstes(
        eintraege: List<Item>,
        ab: Int,
        folgenVon: (Item) -> Sequence<Item>,
        spielbar: (Item) -> Boolean,
    ): Naechstes? {
        for (i in ab.coerceAtLeast(0) until eintraege.size) {
            val eintrag = eintraege[i]
            if (eintrag.kind == "series") {
                val folge = ersteOffeneFolge(folgenVon(eintrag), spielbar)
                if (folge != null) return Naechstes(i, folge, beschriftung(folge, eintrag.title))
            } else if (spielbar(eintrag)) {
                return Naechstes(i, eintrag, beschriftung(eintrag, null))
            }
        }
        return null
    }

    /** „Nordlicht · Staffel 1, Folge 2: Der Anfang" — oder schlicht der Filmtitel. */
    fun beschriftung(titel: Item, serie: String?): String {
        if (titel.kind != "episode") return titel.title
        val nummer = buildList {
            titel.seasonNo?.let { add(Texte.t(Tx.STAFFEL_N, it)) }
            titel.episodeNo?.let { add(Texte.t(Tx.FOLGE_N, it)) }
        }.joinToString(", ")
        val vorn = listOfNotNull(serie?.takeIf { it.isNotBlank() }, nummer.ifEmpty { null })
            .joinToString(" · ")
        return if (vorn.isEmpty()) titel.title else "$vorn: ${titel.title}"
    }
}

/**
 * [AmStueck] mit Daten vom Server.
 *
 * Blockiert, wie alles in lumen-core; aufrufen im Hintergrund. Ein Objekt
 * pro Wiedergabe: es merkt sich, welche Titel eine Datei haben, damit eine
 * lange Liste nicht bei jedem Schritt von vorn nachgefragt wird.
 */
class Abspielfolge(private val client: ApiClient) {
    private val spielbar = HashMap<String, Boolean>()

    /**
     * Gibt es eine Datei dazu? Ein Titel, den es nicht mehr gibt oder den
     * diese Person nicht sehen darf, ist einfach nicht abspielbar. Ist
     * dagegen der Server weg, soll das auffallen — sonst würde die ganze
     * Liste stillschweigend übersprungen.
     */
    fun istSpielbar(titel: Item): Boolean =
        spielbar.getOrPut(titel.id) { detailOderNull(titel.id)?.playable != null }

    /** Der nächste Eintrag der Liste ab [ab] einschliesslich. */
    fun naechstesInListe(eintraege: List<Item>, ab: Int): AmStueck.Naechstes? =
        AmStueck.naechstes(eintraege, ab,
            folgenVon = { serie -> detailOderNull(serie.id)?.let { folgen(it) } ?: emptySequence() },
            spielbar = { istSpielbar(it) })

    /**
     * Die Folge nach [folge]. Der Weg zur Serie geht über die Staffel: eine
     * Folge kennt nur ihre Staffel, die Staffel ihre Serie.
     */
    fun folgeNach(folge: Item): AmStueck.Naechstes? {
        if (folge.kind != "episode") return null
        val staffelId = folge.parentId ?: return null
        val staffel = detailOderNull(staffelId) ?: return null
        val serieId = staffel.item.parentId ?: return null
        val serie = detailOderNull(serieId) ?: return null
        val naechste = AmStueck.folgeNach(folgen(serie), folge.id) { istSpielbar(it) }
            ?: return null
        return AmStueck.Naechstes(-1, naechste, AmStueck.beschriftung(naechste, serie.item.title))
    }

    private fun folgen(serie: ItemDetail): Sequence<Item> =
        AmStueck.folgenDerSerie(serie.seasons) { client.children(it.id) }

    private fun detailOderNull(id: String): ItemDetail? =
        try {
            client.item(id)
        } catch (e: LumenException.Server) {
            if (e.status == 403 || e.status == 404) null else throw e
        }
}
