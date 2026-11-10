// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

import org.lumen.core.*

/**
 * Tests für lumen-core.
 *
 * Bewusst ohne JUnit: lumen-core hat keine Abhängigkeiten, und das soll
 * auch für seine Tests gelten. Ein `main` mit Behauptungen genügt, läuft
 * überall und braucht kein Gradle.
 *
 *     kotlinc src/main/kotlin src/test/kotlin -include-runtime -d tests.jar
 *     java -jar tests.jar [adresse-eines-laufenden-servers]
 *     java -jar tests.jar --code adresse [zweite-adresse]
 *
 * Ohne Adresse laufen nur die Tests, die keinen Server brauchen. Mit
 * `--code` geht es gegen einen frisch gestarteten, noch leeren Server:
 * Besitzer einrichten, Anmelden mit Code, und — mit einer zweiten
 * Adresse — der Wechsel zwischen zwei Servern.
 */

private var geprueft = 0
private val fehler = mutableListOf<String>()

private fun pruefe(was: String, bedingung: Boolean) {
    geprueft++
    if (!bedingung) fehler.add(was)
}

private fun <T> gleich(was: String, erwartet: T, bekommen: T) {
    geprueft++
    if (erwartet != bekommen) fehler.add("$was: erwartet <$erwartet>, bekommen <$bekommen>")
}

private fun wirftFehler(was: String, block: () -> Unit) {
    geprueft++
    try {
        block()
        fehler.add("$was: es kam kein Fehler")
    } catch (e: Exception) {
        // genau so soll es sein
    }
}

fun main(args: Array<String>) {
    jsonTests()
    modellTests()
    reihenListenTests()
    amStueckTests()
    spurenTests()
    markenTests()
    sucheTests()
    adressTests()
    anzeigeTests()
    uhrTests()
    abzeichenTests()
    technikTests()
    untertitelStilTests()
    profilbildTests()
    neuigkeitenTests()
    titelseiteTests()
    fassungenTests()
    rasterTests()
    einladungTests()
    einstellungenTests()

    lizenzTests()
    pinTests()
    verwaltungTests()
    medienschluesselTests()
    unterwegsTests()
    wiedergabenTests()
    jahresTests()
    personTests()
    stoebernTests()
    serverlisteTests()
    anmeldecodeTests()
    trickplayTests()
    herunterladenTests()
    profilschutzTests()
    neueSchnittstellenTests()
    castTests()
    spracheTests()

    val server = args.firstOrNull()
    if (server == "--code") {
        val adresse = args.getOrNull(1) ?: error("--code braucht eine Adresse")
        println("Anmelden mit Code gegen: $adresse")
        codeServerTests(adresse, args.getOrNull(2))
    } else if (server != null) {
        println("Gegen den laufenden Server: $server")
        serverTests(server)
    } else {
        println("Kein Server angegeben — die Tests dagegen bleiben aus.")
    }

    // Zweites und drittes Argument: die verschluesselte Adresse und der
    // Fingerabdruck, den der Server beim Start ausgibt.
    if (server != "--code" && args.size >= 3) pinServerTests(args[1], args[2])

    println()
    if (fehler.isEmpty()) {
        println("✓ $geprueft Behauptungen, alle erfüllt.")
    } else {
        fehler.forEach { println("✗ $it") }
        println("\n${fehler.size} von $geprueft fehlgeschlagen.")
        kotlin.system.exitProcess(1)
    }
}

// ---------------------------------------------------------------- JSON

private fun jsonTests() {
    gleich("Zeichenkette", "hallo", Json.parse("\"hallo\"").text)
    gleich("Zahl", 42, Json.parse("42").int)
    gleich("Kommazahl", 7.8, Json.parse("7.8").double)
    gleich("negative Zahl", -3, Json.parse("-3").int)
    gleich("Exponent", 1500.0, Json.parse("1.5e3").double)
    gleich("wahr", true, Json.parse("true").bool)
    pruefe("null", Json.parse("null") == JsonValue.Null)

    val obj = Json.parse("""{"a":1,"b":{"c":"tief"},"d":[1,2,3]}""")
    gleich("verschachtelt", "tief", obj["b"]["c"].text)
    gleich("Liste", 3, obj["d"].list.size)
    pruefe("fehlendes Feld ist null", obj["gibtsnicht"] == null)
    pruefe("Feld mit null ist null", Json.parse("""{"x":null}""")["x"] == null)

    // Umlaute und Fluchtzeichen — daran scheitern einfache Leser gern.
    gleich("Umlaute", "Grüsse", Json.parse("\"Grüsse\"").text)
    gleich("Fluchtzeichen", "a\"b\\c\nd", Json.parse("\"a\\\"b\\\\c\\nd\"").text)
    gleich("\\u", "ü", Json.parse("\"\\u00fc\"").text)
    gleich("Pfeil aus dem Server", "4K hevc → 1080p h264",
        Json.parse("\"4K hevc → 1080p h264\"").text)

    gleich("leeres Objekt", 0, (Json.parse("{}") as JsonValue.Obj).fields.size)
    gleich("leere Liste", 0, Json.parse("[]").list.size)
    gleich("Leerzeichen egal", 1, Json.parse("  {  \"a\" :  1  }  ")["a"].int)

    wirftFehler("abgeschnittenes Objekt") { Json.parse("""{"a":1""") }
    wirftFehler("Komma zu viel") { Json.parse("""{"a":1,}""") }
    wirftFehler("unbeendete Zeichenkette") { Json.parse("\"abc") }
    wirftFehler("Müll am Ende") { Json.parse("""{"a":1} dazu""") }
    wirftFehler("gar nichts") { Json.parse("") }

    // Schreiben
    gleich("schreiben", """{"a":1,"b":"x","c":true}""",
        JsonWriter.obj("a" to 1, "b" to "x", "c" to true))
    gleich("schreiben mit Anführungszeichen", """{"t":"er sagte \"ja\""}""",
        JsonWriter.obj("t" to "er sagte \"ja\""))
    gleich("schreiben mit Karte", """{"prefs":{"a":"1"}}""",
        JsonWriter.obj("prefs" to mapOf("a" to "1")))

    // Hin und zurück: was wir schreiben, müssen wir auch lesen können.
    val rund = JsonWriter.obj("titel" to "Grüsse \"aus\" dem Süden\n", "jahr" to 2019)
    gleich("hin und zurück", "Grüsse \"aus\" dem Süden\n", Json.parse(rund)["titel"].text)
}

// ---------------------------------------------------------------- Modelle

private fun modellTests() {
    // So knapp, wie der Server einen frisch gescannten Titel schickt.
    val knapp = Item.from(Json.parse("""
        {"id":"it_1","libraryId":"lib_1","kind":"movie","title":"Nordlicht",
         "matchState":"pending","addedAt":"2026-10-23T13:35:52.1+02:00"}
    """))
    gleich("Titel", "Nordlicht", knapp.title)
    pruefe("kein Jahr", knapp.year == null)
    pruefe("nicht gesehen", !knapp.finished)
    gleich("kein Fortschritt", 0.0, knapp.progress)
    gleich("keine Genres", 0, knapp.genres.size)

    val voll = Item.from(Json.parse("""
        {"id":"it_1","libraryId":"lib_1","kind":"movie","title":"Die Bucht",
         "year":2019,"runtimeMs":7080000,"ageYears":16,"genres":["Drama","Mystery"],
         "imdbRating":7.8,"imdbVotes":48213,"matchState":"matched",
         "addedAt":"2026-10-23T13:35:52.1+02:00",
         "progress":42.5,"finished":false,"myRating":4,"onList":true,
         "resumeMs":1800000,"poster":"/api/v1/art/it_1/poster"}
    """))
    gleich("Fortschritt", 42.5, voll.progress)
    gleich("Merkliste", true, voll.onList)
    gleich("Bewertung", 4, voll.myRating)
    gleich("Weiterschauen", 1800.0, voll.resumeSeconds)
    gleich("Untertitelzeile", "2019 · 1 Std. 58 Min. · ab 16 · Drama · Mystery",
        voll.subtitleLine)

    // Ein Pflichtfeld, das fehlt, heisst: App und Server passen nicht
    // zusammen. Das muss auffallen, nicht stillschweigend null werden.
    wirftFehler("Titel fehlt") {
        Item.from(Json.parse("""{"id":"x","kind":"movie"}"""))
    }

}

// ---------------------------------------------------------------- Reihen und Listen

private fun reihenListenTests() {
    // Eine Reihe, wie GET /collections sie schickt — ohne Beschreibung und
    // ohne "hidden": das lässt der Server weg, solange es falsch ist.
    val reihe = Reihe.from(Json.parse("""
        {"id":"col_1","name":"Die Bucht – Reihe","source":"tmdb","anzahl":2,
         "items":[{"id":"it_1","kind":"movie","title":"Die Bucht","year":2019},
                  {"id":"it_2","kind":"movie","title":"Die Bucht II","year":2022}]}
    """))
    gleich("Reihe: Name", "Die Bucht – Reihe", reihe.name)
    gleich("Reihe: Anzahl", 2, reihe.anzahl)
    gleich("Reihe: Filme in Reihenfolge", listOf("it_1", "it_2"), reihe.items.map { it.id })
    pruefe("Reihe: ohne Beschreibung", reihe.overview == null)
    pruefe("Reihe: nicht aufgelöst, wenn das Feld fehlt", !reihe.hidden)
    gleich("Reihe: Anzahl als Text", "2 Filme", reihe.anzahlText)
    wirftFehler("Reihe ohne Namen") { Reihe.from(Json.parse("""{"id":"col_1"}""")) }

    // Die Titelseite eines Films mit und ohne Reihe.
    val mit = ItemDetail.from(Json.parse("""
        {"item":{"id":"it_1","kind":"movie","title":"Die Bucht"},"files":[],
         "sharedRatings":[],"collection":{"id":"col_1","name":"Reihe","source":"auto",
         "anzahl":2,"items":[]}}
    """))
    gleich("Titelseite kennt ihre Reihe", "col_1", mit.collection?.id)
    val ohne = ItemDetail.from(Json.parse("""
        {"item":{"id":"it_1","kind":"movie","title":"Die Bucht"},"files":[]}
    """))
    pruefe("Titelseite ohne Reihe", ohne.collection == null)

    // Eine Liste aus der Übersicht: mit Vorschau, ohne Titel.
    val liste = Liste.from(Json.parse("""
        {"id":"pl_1","name":"Filmabend","von":{"id":"acc_1","name":"Mara","avatar":"av-b"},
         "meine":false,"geteilt":true,"gemeinsam":true,"darfAendern":true,"anzahl":3,
         "erstelltAm":"2026-10-23T13:35:52+02:00",
         "vorschau":[{"id":"it_1","kind":"movie","title":"Die Bucht"}]}
    """))
    gleich("Liste: Name", "Filmabend", liste.name)
    gleich("Liste: von", "Mara", liste.von.name)
    pruefe("Liste: nicht meine", !liste.meine)
    pruefe("Liste: gemeinsam", liste.gemeinsam && liste.darfAendern)
    pruefe("Liste: ohne ?item= kein Haken", !liste.enthaelt)
    gleich("Liste: Vorschau", 1, liste.vorschau.size)
    gleich("Liste: keine Titel in der Übersicht", 0, liste.items.size)

    // Eine einzelne Liste: mit Titeln, und wer sie hineingelegt hat.
    val voll = Liste.from(Json.parse("""
        {"id":"pl_1","name":"Filmabend","von":{"id":"acc_1","name":"Mara","avatar":"av-b"},
         "meine":true,"geteilt":false,"gemeinsam":false,"darfAendern":true,"anzahl":2,
         "erstelltAm":"2026-10-23T13:35:52+02:00","enthaelt":true,
         "items":[{"id":"it_1","kind":"movie","title":"Die Bucht","hinzugefuegtVon":"Jonas"},
                  {"id":"it_2","kind":"series","title":"Nordlicht"}]}
    """))
    pruefe("Liste: Haken mit ?item=", voll.enthaelt)
    gleich("Liste: Titel", 2, voll.items.size)
    gleich("Liste: hineingelegt von", "Jonas", voll.items[0].hinzugefuegtVon)
    pruefe("Liste: ohne Angabe niemand", voll.items[1].hinzugefuegtVon == null)
    pruefe("Liste: ohne Vorschau", voll.vorschau.isEmpty())
    wirftFehler("Liste ohne Namen") { Liste.from(Json.parse("""{"id":"pl_1"}""")) }

    // Das Standbild springt ein, wo Poster oder Hintergrund fehlen.
    val nurBild = Item.from(Json.parse("""
        {"id":"it_9","kind":"movie","title":"Ohne Poster","frame":"/api/v1/art/it_9/frame"}
    """))
    gleich("ohne Poster das Standbild", "frame", nurBild.posterKind)
    gleich("ohne Hintergrund das Standbild", "frame", nurBild.wideKind)
    val mitPoster = Item.from(Json.parse("""
        {"id":"it_8","kind":"movie","title":"Mit Poster","poster":"/api/v1/art/it_8/poster",
         "frame":"/api/v1/art/it_8/frame"}
    """))
    gleich("mit Poster das Poster", "poster", mitPoster.posterKind)
    gleich("ohne Hintergrund lieber Standbild als Poster", "frame", mitPoster.wideKind)
    val allesDa = Item.from(Json.parse("""
        {"id":"it_7","kind":"movie","title":"Alles da","poster":"/p","backdrop":"/b"}
    """))
    gleich("mit Hintergrund der Hintergrund", "backdrop", allesDa.wideKind)
    gleich("ganz ohne Bilder bleibt es beim Poster", "poster",
        Item.from(Json.parse("""{"id":"x","title":"Leer"}""")).wideKind)

    val client = ApiClient("http://nas:8097", "TOKEN")
    client.medienSchluesselSetzen("KEY")
    gleich("Standbild-Adresse samt Schlüssel",
        "http://nas:8097/api/v1/art/it_9/frame?key=KEY", client.posterUrl(nurBild))
    gleich("Hintergrund-Adresse samt Schlüssel",
        "http://nas:8097/api/v1/art/it_7/backdrop?key=KEY", client.wideUrl(allesDa))
    pruefe("ohne Anmeldung keine Bildadresse", ApiClient("http://nas:8097").posterUrl(nurBild) == null)
}

// ---------------------------------------------------------------- Am Stück

/** Ein Titel, so knapp wie nötig — für die Wiedergabe am Stück. */
private fun titel(id: String, kind: String, name: String, staffel: Int? = null,
                  folge: Int? = null, gesehen: Boolean = false): Item =
    Item.from(Json.parse(JsonWriter.obj(
        "id" to id, "kind" to kind, "title" to name,
        "seasonNo" to staffel, "episodeNo" to folge, "finished" to gesehen)))

private fun amStueckTests() {
    // Staffel 0 sind Specials: die kommen nach der letzten Staffel.
    val specials = titel("s0", "season", "Specials", staffel = 0)
    val erste = titel("s1", "season", "Staffel 1", staffel = 1)
    val zweite = titel("s2", "season", "Staffel 2", staffel = 2)
    gleich("Staffeln: Specials zuletzt", listOf("s1", "s2", "s0"),
        AmStueck.staffelnOrdnen(listOf(specials, zweite, erste)).map { it.id })

    // Nordlicht: Folge 1 gesehen, 2 offen. Staffel 2 hat eine Folge.
    val folgen = mapOf(
        "s1" to listOf(
            titel("e3", "episode", "Das Ende", 1, 3),
            titel("e1", "episode", "Der Anfang", 1, 1, gesehen = true),
            titel("e2", "episode", "Die Mitte", 1, 2)),
        "s2" to listOf(titel("e4", "episode", "Neu", 2, 1)),
        "s0" to listOf(titel("x1", "episode", "Hinter den Kulissen", 0, 1)),
    )
    val geholt = mutableListOf<String>()
    val nordlicht = AmStueck.folgenDerSerie(listOf(specials, zweite, erste)) { s ->
        geholt.add(s.id)
        folgen[s.id] ?: emptyList()
    }
    gleich("Folgen in Reihenfolge, über Staffeln hinweg",
        listOf("e1", "e2", "e3", "e4", "x1"), nordlicht.map { it.id }.toList())

    geholt.clear()
    gleich("erste offene Folge", "e2", AmStueck.ersteOffeneFolge(nordlicht)?.id)
    gleich("dafür reicht die erste Staffel", listOf("s1"), geholt.toList())
    gleich("ohne Datei zur nächsten offenen", "e3",
        AmStueck.ersteOffeneFolge(nordlicht) { it.id != "e2" }?.id)

    gleich("nach Folge 2 kommt Folge 3", "e3", AmStueck.folgeNach(nordlicht, "e2")?.id)
    gleich("nach Staffelende die nächste Staffel", "e4", AmStueck.folgeNach(nordlicht, "e3")?.id)
    gleich("auch nach einer gesehenen Folge die nächste", "e2",
        AmStueck.folgeNach(nordlicht, "e1")?.id)
    gleich("eine Folge ohne Datei wird übersprungen", "e4",
        AmStueck.folgeNach(nordlicht, "e2") { it.id != "e3" }?.id)
    pruefe("aus der letzten Staffel nicht von selbst in die Specials",
        AmStueck.folgeNach(nordlicht, "e4") == null)
    pruefe("auch nicht über eine Folge ohne Datei hinweg",
        AmStueck.folgeNach(nordlicht, "e3") { it.id != "e4" } == null)
    gleich("Folgen ohne Nummer ans Ende der Staffel", listOf("e1", "e2", "o"),
        AmStueck.folgenOrdnen(listOf(titel("o", "episode", "Ohne", 1, 0), titel("e2", "episode", "Zwei", 1, 2),
            titel("e1", "episode", "Eins", 1, 1))).map { it.id })
    pruefe("nach der letzten Folge nichts", AmStueck.folgeNach(nordlicht, "x1") == null)
    pruefe("unbekannte Folge: nichts", AmStueck.folgeNach(nordlicht, "gibtsnicht") == null)

    // Eine Liste: Film, Serie, Film ohne Datei, fertige Serie, leere Serie, Film.
    val alleGesehen = titel("se2", "series", "Fertig")
    val leer = titel("se3", "series", "Leer")
    val liste = listOf(
        titel("m1", "movie", "Die Bucht", gesehen = true),
        titel("se1", "series", "Nordlicht"),
        titel("m2", "movie", "Ohne Datei"),
        alleGesehen,
        leer,
        titel("m3", "movie", "Abspann"),
    )
    val serien = mapOf(
        "se1" to nordlicht,
        "se2" to sequenceOf(titel("f1", "episode", "Alt", 1, 1, gesehen = true)),
    )
    val folgenVon: (Item) -> Sequence<Item> = { serien[it.id] ?: emptySequence() }
    val spielbar: (Item) -> Boolean = { it.id != "m2" }

    val a = AmStueck.naechstes(liste, 0, folgenVon, spielbar)
    gleich("Liste: ein gesehener Film läuft trotzdem", "m1", a?.titel?.id)
    gleich("Liste: an erster Stelle", 0, a?.index)
    gleich("Liste: Film heisst wie der Film", "Die Bucht", a?.beschriftung)

    val b = AmStueck.naechstes(liste, 1, folgenVon, spielbar)
    gleich("Liste: Serie spielt ihre erste offene Folge", "e2", b?.titel?.id)
    gleich("Liste: und bleibt an ihrem Platz", 1, b?.index)
    gleich("Liste: Folge mit Serie und Nummer",
        "Nordlicht · Staffel 1, Folge 2: Die Mitte", b?.beschriftung)

    val c = AmStueck.naechstes(liste, 2, folgenVon, spielbar)
    gleich("Liste: ohne Datei, fertig und leer übersprungen", "m3", c?.titel?.id)
    gleich("Liste: am Ende der Liste", 5, c?.index)

    pruefe("Liste: nach dem letzten nichts mehr",
        AmStueck.naechstes(liste, 6, folgenVon, spielbar) == null)
    pruefe("leere Liste: nichts",
        AmStueck.naechstes(emptyList(), 0, folgenVon, spielbar) == null)
    gleich("Liste: ein Anfang vor null zählt als null", "m1",
        AmStueck.naechstes(liste, -3, folgenVon, spielbar)?.titel?.id)

    gleich("Beschriftung ohne Serie", "Staffel 2, Folge 1: Neu",
        AmStueck.beschriftung(titel("e4", "episode", "Neu", 2, 1), null))
    gleich("Beschriftung ohne Nummern", "Nordlicht: Pilot",
        AmStueck.beschriftung(titel("e9", "episode", "Pilot"), "Nordlicht"))
}

// ---------------------------------------------------------------- Spuren

/** Eine Spur, so wie der Server sie in files[].tracks schickt. */
private fun spur(index: Int, kind: String, sprache: String? = null, titel: String? = null,
                 kanaele: Int = 0, erzwungen: Boolean = false, hoer: Boolean = false,
                 extern: Boolean = false, bitmap: Boolean = false): Track =
    Track.from(Json.parse(JsonWriter.obj(
        "index" to index, "kind" to kind, "language" to sprache, "title" to titel,
        "channels" to kanaele, "forced" to erzwungen, "hearingImpaired" to hoer,
        "external" to extern, "bitmap" to bitmap)))

private fun spurenTests() {
    // Die Datei einer Folge: zwei Tonspuren, Untertitel in drei Fassungen,
    // Bild-Untertitel, eine .srt daneben.
    val spuren = listOf(
        spur(1, "audio", "de", kanaele = 6),
        spur(2, "audio", "en", titel = "Kommentar", kanaele = 2),
        spur(3, "subtitle", "de"),
        spur(4, "subtitle", "de", erzwungen = true),
        spur(5, "subtitle", "en", hoer = true),
        spur(6, "subtitle", "de", bitmap = true),
        spur(1000, "subtitle", "fr", extern = true),
    )

    // Anzeigenamen — dieselben wie im Browser und in der HLS-Liste.
    gleich("Name mit 5.1", "Deutsch (5.1)", spuren[0].anzeigename)
    gleich("Name mit Titel", "Englisch (Kommentar)", spuren[1].anzeigename)
    gleich("Name schlicht", "Deutsch", spuren[2].anzeigename)
    gleich("Name erzwungen", "Deutsch (erzwungen)", spuren[3].anzeigename)
    gleich("Name für Hörgeschädigte", "Englisch (für Hörgeschädigte)", spuren[4].anzeigename)
    gleich("Name Datei", "Französisch (Datei)", spuren[6].anzeigename)
    gleich("unbekannte Sprache in Grossbuchstaben", "XX", Spuren.sprache("xx"))
    gleich("ohne Sprache", "Unbekannt", Spuren.sprache(null))
    gleich("und ist auch unbekannt", "Unbekannt", Spuren.sprache("und"))
    gleich("Titel gleich Sprache wird nicht doppelt genannt", "Deutsch",
        spur(9, "audio", "de", titel = "deutsch").anzeigename)

    // Was angeboten wird: Bild-Untertitel nicht.
    gleich("Tonspuren", listOf(1, 2), Spuren.ton(spuren).map { it.index })
    gleich("Textspuren ohne Bitmap", listOf(3, 4, 5, 1000), Spuren.text(spuren).map { it.index })
    pruefe("wählbar", Spuren.waehlbar(spuren))
    pruefe("eine Tonspur, keine Untertitel: nichts zu wählen",
        !Spuren.waehlbar(listOf(spur(1, "audio", "de"), spur(6, "subtitle", "de", bitmap = true))))
    gleich("Stelle der Tonspur", 1, Spuren.tonPosition(spuren, 2))
    gleich("Stelle einer fremden Spur", -1, Spuren.tonPosition(spuren, 7))

    // Vorbelegen nach den gemerkten Sprachen.
    val ohne = Spuren.vorbelegen(spuren, emptyMap())
    gleich("ohne Prefs: Ton bleibt Vorgabe", -1, ohne.ton)
    gleich("ohne Prefs: erzwungene Untertitel zur ersten Tonspur", 4, ohne.untertitel)

    val englisch = Spuren.vorbelegen(spuren, mapOf("tonSprache" to "en"))
    gleich("gemerkte Tonsprache", 2, englisch.ton)
    pruefe("zu Englisch keine erzwungenen Untertitel", englisch.untertitel == null)

    val mitText = Spuren.vorbelegen(spuren, mapOf("tonSprache" to "en", "untertitel" to "de"))
    gleich("gemerkte Untertitel: lieber die vollständige Spur", 3, mitText.untertitel)
    gleich("gemerkte Untertitel: nur erzwungen da, dann die", 4,
        Spuren.vorbelegen(spuren.filter { it.index != 3 }, mapOf("untertitel" to "de")).untertitel)
    // „aus" heisst: keine gewählten — die erzwungenen zur Tonsprache laufen
    // trotzdem, denn ohne sie versteht man die fremdsprachigen Stellen nicht.
    gleich("Untertitel aus: erzwungene laufen trotzdem mit", 4,
        Spuren.vorbelegen(spuren, mapOf("untertitel" to "aus")).untertitel)
    pruefe("unbekannte Sprache gemerkt: nichts",
        Spuren.vorbelegen(spuren, mapOf("tonSprache" to "ja", "untertitel" to "ja")).untertitel == null)
    gleich("unbekannte Tonsprache: Vorgabe", -1,
        Spuren.vorbelegen(spuren, mapOf("tonSprache" to "ja")).ton)
    pruefe("Bild-Untertitel werden nie vorbelegt",
        Spuren.vorbelegen(listOf(spur(6, "subtitle", "de", bitmap = true)),
            mapOf("untertitel" to "de")).untertitel == null)

    // Was in die Prefs kommt.
    gleich("Pref Ton", "en", Spuren.prefWertTon(spuren[1]))
    pruefe("Pref Ton ohne Sprache: nichts", Spuren.prefWertTon(spur(7, "audio")) == null)
    gleich("Pref Untertitel", "de", Spuren.prefWertUntertitel(spuren[3]))
    gleich("Pref Untertitel ohne Sprache", "und", Spuren.prefWertUntertitel(spur(8, "subtitle")))
    gleich("Pref Untertitel aus", "aus", Spuren.prefWertUntertitel(null))

    // Die Datei trägt ihre Spuren; eine ältere Antwort ohne bleibt lesbar.
    val datei = MediaFile.from(Json.parse("""
        {"id":"f1","itemId":"it_1","path":"/x/y.mkv","durationMs":2700000,
         "tracks":[{"index":1,"kind":"audio","language":"de","channels":2},
                   {"index":3,"kind":"subtitle","language":"de","forced":true}],
         "chapters":[{"index":0,"startMs":0,"endMs":60000,"title":"Intro","kind":"intro"},
                     {"index":1,"startMs":60000,"endMs":2600000,"kind":"normal"}]}
    """))
    gleich("Datei kennt Spuren", 2, datei.tracks.size)
    gleich("Datei kennt Kapitel", "intro", datei.chapters[0].kind)
    pruefe("Datei ohne Spuren", MediaFile.from(Json.parse("""{"id":"f2"}""")).tracks.isEmpty())

    // Die Adressen: Tonspur am Strom, Untertitel als WebVTT.
    val client = ApiClient("http://nas:8097", "TOKEN")
    client.medienSchluesselSetzen("KEY")
    val direkt = PlaybackPlan.from(Json.parse("""{"direct":true,"videoCodec":"h264"}"""))
    val umgewandelt = PlaybackPlan.from(Json.parse("""{"direct":false,"videoCodec":"h264"}"""))
    val mp4 = MediaFile.from(Json.parse("""{"id":"f1","container":"mp4"}"""))
    pruefe("Strom ohne Spur ohne Parameter", !client.streamUrl(mp4, direkt).contains("spur="))
    pruefe("Strom mit Spur", client.streamUrl(mp4, direkt, 2).contains("spur=2"))
    pruefe("HLS mit Spur", client.streamUrl(mp4, umgewandelt, 2).let {
        it.contains("/hls/master.m3u8") && it.contains("spur=2") })
    pruefe("direkt heisst direkt", client.istDirekt(mp4, direkt) && !client.istDirekt(mp4, umgewandelt))
    gleich("Untertitel-Adresse",
        "http://nas:8097/api/v1/stream/f1/subtitles/4.vtt?key=KEY", client.subtitleUrl("f1", 4))
    gleich("Standbild-Adresse", "http://nas:8097/api/v1/art/it_1/frame?key=KEY",
        client.frameUrl("it_1"))
}

// ---------------------------------------------------------------- Vorspann und Abspann

private fun markenTests() {
    val detail = ItemDetail.from(Json.parse("""
        {"item":{"id":"e1","kind":"episode","title":"Der Anfang","parentId":"s1"},
         "files":[],"intro":{"vonMs":30000,"bisMs":90000,"quelle":"serie"},
         "creditsMs":2500000,"seriesId":"se1"}
    """))
    gleich("Vorspann von", 30000L, detail.intro?.vonMs)
    pruefe("Vorspann von der Serie", detail.intro?.vonSerie == true)
    gleich("Abspann ab", 2500000L, detail.creditsMs)
    gleich("Serie der Folge", "se1", detail.seriesId)
    val film = ItemDetail.from(Json.parse("""
        {"item":{"id":"m1","kind":"movie","title":"Die Bucht"},"files":[],"creditsMs":0}
    """))
    pruefe("Film ohne Vorspann", film.intro == null)
    pruefe("creditsMs 0 heisst unbekannt", film.creditsMs == null)

    val intro = detail.intro
    pruefe("vor dem Vorspann nicht drin", !Marken.imVorspann(intro, 10_000))
    pruefe("ganz am Anfang noch nicht — sonst flackert es", !Marken.imVorspann(intro, 30_200))
    pruefe("im Vorspann", Marken.imVorspann(intro, 45_000))
    pruefe("kurz vor dem Ende nicht mehr", !Marken.imVorspann(intro, 89_500))
    pruefe("ohne Vorspann nie", !Marken.imVorspann(null, 45_000))
    pruefe("Spanne gültig", Marken.vorspannGueltig(30_000, 90_000))
    pruefe("Spanne rückwärts ungültig", !Marken.vorspannGueltig(90_000, 30_000))
    pruefe("Spanne zu lang ungültig", !Marken.vorspannGueltig(0, 11 * 60 * 1000))

    gleich("Abspann laut Kapitel", 2_500_000L, Marken.abspannAb(2_500_000, 2_700_000))
    gleich("Abspann ohne Kapitel: 40 s vor dem Ende", 2_660_000L, Marken.abspannAb(null, 2_700_000))
    gleich("Abspann ohne Dauer: unbekannt", 0L, Marken.abspannAb(null, 0))
    pruefe("vor dem Abspann nichts", !Marken.imAbspann(null, 2_700_000, 2_000_000))
    pruefe("im Abspann", Marken.imAbspann(null, 2_700_000, 2_670_000))
    pruefe("in den letzten zwei Sekunden nicht mehr — das Ende regelt das",
        !Marken.imAbspann(null, 2_700_000, 2_699_000))
    pruefe("ohne Dauer nie", !Marken.imAbspann(null, 0, 100))
    gleich("Countdown ist die Restzeit", 40, Marken.countdown(2_700_000, 2_660_000))
    gleich("Countdown mindestens zehn Sekunden", 10, Marken.countdown(2_700_000, 2_697_000))
}

// ---------------------------------------------------------------- Suche

private fun sucheTests() {
    val ergebnis = Suchergebnis.from(Json.parse("""
        {"items":[{"id":"it_1","kind":"movie","title":"Forrest Gump","year":1994},
                  {"id":"it_2","kind":"movie","title":"Apollo 13","year":1995}],
         "personen":[{"name":"Tom Hanks","anzahl":7,"itemId":"it_1","bild":"/api/v1/art/it_1/cast00"},
                     {"name":"Tom Hardy","anzahl":1}],
         "genres":["Drama"],
         "deutung":{"person":"Tom Hanks","jahrVon":1990,"jahrBis":1999}}
    """))
    gleich("Treffer", 2, ergebnis.items.size)
    gleich("Personen", "Tom Hanks", ergebnis.personen[0].name)
    gleich("Person: Anzahl als Text", "7 Titel", ergebnis.personen[0].anzahlText)
    gleich("Person: Anfangsbuchstabe", "T", ergebnis.personen[1].initial)
    pruefe("Person ohne Bild", ergebnis.personen[1].bild == null)
    gleich("Porträt-Adresse samt Schlüssel", "http://nas:8097/api/v1/art/it_1/cast00?key=KEY",
        ApiClient("http://nas:8097", "TOKEN").also { it.medienSchluesselSetzen("KEY") }
            .bildUrl(ergebnis.personen[0].bild))
    pruefe("ohne Bild keine Adresse", ApiClient("http://nas:8097", "TOKEN").bildUrl(null) == null)
    pruefe("ohne Anmeldung keine Adresse", ApiClient("http://nas:8097").bildUrl("/api/v1/art/x/cast00") == null)
    gleich("Genres", listOf("Drama"), ergebnis.genres)
    pruefe("gedeutet", ergebnis.deutung?.gedeutet == true)
    gleich("Trefferzeile", "2 Treffer · mit Tom Hanks · 1990–1999", ergebnis.trefferZeile)
    pruefe("nicht leer", !ergebnis.leer)

    gleich("Deutung nur Genre und Jahr", "Komödie · 2019",
        Deutung.from(Json.parse("""{"genre":"Komödie","jahrVon":2019,"jahrBis":2019}""")).beschreibung)
    gleich("Deutung mit Resttext", "Drama · „Bucht“",
        Deutung.from(Json.parse("""{"text":"Bucht","genre":"Drama"}""")).beschreibung)
    pruefe("blosser Text ist nicht gedeutet",
        !Deutung.from(Json.parse("""{"text":"Bucht"}""")).gedeutet)

    // Ohne Deutung steht nur die Zahl; ein Treffer in der Einzahl.
    val einer = Suchergebnis.from(Json.parse("""
        {"items":[{"id":"it_1","kind":"movie","title":"Die Bucht"}],"personen":[],"genres":[]}
    """))
    gleich("ein Treffer", "1 Treffer", einer.trefferZeile)
    pruefe("leeres Ergebnis",
        Suchergebnis.from(Json.parse("""{"items":[],"personen":[],"genres":[]}""")).leer)
    wirftFehler("Person ohne Namen") { PersonTreffer.from(Json.parse("""{"anzahl":1}""")) }

    // Der Stand einer Serie auf der Karte — nur die eigenen Folgen.
    val serie = Item.from(Json.parse("""
        {"id":"se1","kind":"series","title":"Nordlicht","gesehen":4,"folgen":10,"progress":40}
    """))
    gleich("Serienstand", "4 von 10 Folgen", serie.serienStand)
    gleich("alle gesehen", "alle 10 Folgen gesehen",
        Item.from(Json.parse("""{"id":"se1","kind":"series","title":"N","gesehen":10,"folgen":10,"finished":true}"""))
            .serienStand)
    pruefe("nichts gesehen: kein Stand",
        Item.from(Json.parse("""{"id":"se1","kind":"series","title":"N","folgen":10}""")).serienStand == null)
    pruefe("ein Film hat keinen Serienstand",
        Item.from(Json.parse("""{"id":"m1","kind":"movie","title":"N","gesehen":1,"folgen":1}""")).serienStand == null)
}

// ---------------------------------------------------------------- Adressen

private fun adressTests() {
    val faelle = listOf(
        "192.168.1.20" to "http://192.168.1.20:8097",
        "192.168.1.20:8097" to "http://192.168.1.20:8097",
        "lumen.local" to "http://lumen.local:8097",
        "  lumen.local:8097  " to "http://lumen.local:8097",
        "http://nas:9000" to "http://nas:9000",
        "https://lumen.example.com" to "https://lumen.example.com",
        "http://nas:8097/irgendwo" to "http://nas:8097",
    )
    for ((eingabe, erwartet) in faelle) {
        gleich("Adresse „$eingabe“", erwartet, ApiClient.normalizeAddress(eingabe))
    }
    for (unsinn in listOf("", "   ", "http://")) {
        pruefe("Unsinn „$unsinn“ wird abgelehnt", ApiClient.normalizeAddress(unsinn) == null)
    }
}

// ---------------------------------------------------------------- Anzeige

private fun anzeigeTests() {
    gleich("Laufzeit lang", "1 Std. 58 Min.", Item.duration(7_080_000))
    gleich("Laufzeit kurz", "48 Min.", Item.duration(2_880_000))

    pruefe("MP4 geht direkt", DeviceProfile.playsDirectly("mp4"))
    pruefe("MKV geht direkt (ExoPlayer liest Matroska)", DeviceProfile.playsDirectly("MKV"))
    pruefe("AVI geht nicht direkt", !DeviceProfile.playsDirectly("avi"))
    pruefe("nichts geht nicht direkt", !DeviceProfile.playsDirectly(null))

    val q = DeviceProfile.current.query()
    pruefe("Gerät nennt Codecs", !q["codecs"].isNullOrEmpty())
    pruefe("Gerät nennt Höhe", !q["maxHeight"].isNullOrEmpty())
}

// ---------------------------------------------------------------- Server

/**
 * Der Durchstich: derselbe Weg, den die App auf dem Telefon geht.
 *
 * Hier zeigt sich, ob Pfade, Feldnamen und Fehlerbehandlung wirklich zum
 * Server passen — und nicht nur zu dem, was ich mir darunter vorgestellt habe.
 */
private fun serverTests(adresse: String) {
    val client = ApiClient(ApiClient.normalizeAddress(adresse)!!)

    val hello = client.hello()
    pruefe("Server meldet sich", hello.server == "Lumen")
    pruefe("Server ist eingerichtet", hello.setUp)
    println("  Server " + hello.version)

    // §13 der AGPL: wer den Dienst über das Netz benutzt, muss an den
    // Quelltext kommen. Darum muss der Server beides von sich aus nennen —
    // sonst zeigen die Einstellungen „Über Lumen" nichts an.
    pruefe("Server nennt seine Lizenz", hello.license == "AGPL-3.0-only")
    pruefe("Server nennt die Fundstelle des Quelltexts",
           hello.sourceUrl?.startsWith("http") == true)
    println("  Lizenz " + hello.license + " · " + hello.sourceUrl)

    val profile = client.profiles()
    pruefe("es gibt Profile", profile.isNotEmpty())
    val besitzer = profile.first()

    // Falscher Code muss scheitern — und zwar mit der Meldung des Servers.
    try {
        client.unlock(besitzer.id, "falsch", "Testgerät")
        fehler.add("falscher Code wurde angenommen")
        geprueft++
    } catch (e: LumenException.Server) {
        geprueft++
        pruefe("falscher Code gibt 401", e.status == 401)
    }

    val konto = client.unlock(besitzer.id, "einsehrgutespasswort", "Testgerät")
    pruefe("angemeldet", client.isSignedIn)
    pruefe("Besitzer erkannt", konto.isOwner)

    val me = client.me()
    gleich("Konto stimmt", konto.id, me.account.id)
    pruefe("Grenzen gelesen", me.limits.maxStreams > 0)

    val reihen = client.home()
    pruefe("Startseite hat Reihen", reihen.isNotEmpty())
    val titel = reihen.flatMap { it.items }.firstOrNull { it.kind == "movie" }
    pruefe("mindestens ein Film", titel != null)

    if (titel != null) {
        val detail = client.item(titel.id)
        gleich("Titelseite", titel.id, detail.item.id)
        pruefe("Beschreibung da", !detail.item.overview.isNullOrEmpty())
        pruefe("Poster da", detail.item.poster != null)

        val datei = detail.playable
        pruefe("abspielbare Datei", datei != null)

        if (datei != null) {
            val plan = client.plan(datei.id)
            pruefe("Abspielplan", plan.videoCodec.isNotEmpty())
            // Vor dem Abspielen: frischer Medienschlüssel. Das Token
            // gehört in keine Adresse.
            client.wiedergabeVorbereiten(datei.id)
            val adresseStrom = client.streamUrl(datei, plan)
            pruefe("Strom-Adresse enthält den Schlüssel", adresseStrom.contains("key="))
            pruefe("Strom-Adresse ohne Token", !adresseStrom.contains("token=") &&
                !adresseStrom.contains(client.token!!))
            pruefe("Strom-Adresse zeigt auf die Datei", adresseStrom.contains(datei.id))
            println("  Wiedergabe: ${if (plan.direct) "direkt" else "umwandeln (${plan.reason})"}")
        }

        // Mein Stand — und die Gegenprobe, dass er wirklich ankommt.
        client.saveProgress(titel.id, 90.0, 3600.0)
        val nachher = client.item(titel.id).item
        pruefe("Fortschritt gespeichert", nachher.resumeMs in 89_000..91_000)

        client.addToWatchlist(titel.id)
        pruefe("auf der Merkliste", client.item(titel.id).item.onList)
        client.removeFromWatchlist(titel.id)
        pruefe("von der Merkliste runter", !client.item(titel.id).item.onList)

        client.markFinished(titel.id)
        pruefe("als gesehen markiert", client.item(titel.id).item.finished)
        client.clearFinished(titel.id)
        pruefe("Haken zurückgenommen", !client.item(titel.id).item.finished)

        client.rate(titel.id, 4, shared = false)
        gleich("Bewertung gespeichert", 4, client.item(titel.id).item.myRating)
        pruefe("Bewertung bleibt privat", client.item(titel.id).sharedRatings.isEmpty())
    }

    val suche = client.items(search = "gibtsganzsicherniemals")
    gleich("Suche ohne Treffer", 0, suche.size)

    // Das Raster mit Genre-Knöpfen und die eigenen Geräte — beides holt
    // der Browser genauso.
    val seite = client.seite("movie", limit = 2)
    pruefe("Seite: höchstens zwei Titel", seite.items.size <= 2)
    pruefe("Seite: Gesamtzahl passt", seite.gesamt >= seite.items.size)
    gleich("Seite: weiter, wenn mehr da ist", seite.gesamt > seite.items.size, seite.weiter)
    if (seite.weiter) {
        val zweite = client.seite("movie", limit = 2, offset = seite.naechsterOffset)
        pruefe("zweite Seite beginnt dahinter", zweite.offset == seite.naechsterOffset)
        pruefe("zusammen nichts doppelt", seite.mit(zweite).items.map { it.id }.toSet().size ==
            seite.mit(zweite).items.size)
    }
    seite.genres.firstOrNull()?.let { g ->
        val gefiltert = client.seite("movie", genre = g)
        pruefe("Genre-Filter: alles mit dem Genre", gefiltert.items.all { g in it.genres })
        gleich("Genre-Filter: die Knöpfe bleiben alle", seite.genres, gefiltert.genres)
    }
    val geraete = client.devices()
    pruefe("das eigene Gerät steht in der Liste", geraete.any { it.name == "Testgerät" })

    val wunsch = client.wish("Ein Testwunsch")
    pruefe("Wunsch angelegt", wunsch.title == "Ein Testwunsch")
    pruefe("Wunsch ist meiner", wunsch.mine)
    pruefe("Wunsch in der Liste", client.wishes().any { it.id == wunsch.id })

    // Der Ablauf von „nichts eingerichtet" bis zur Mediathek, so wie die
    // App ihn geht.
    ablaufTests(adresse)

    // Verwaltung gibt es auf Android nicht mehr — die Gegenprobe bleibt:
    // abgemeldet kommt man an nichts heran, was eine Anmeldung braucht.
    client.signOut()
    pruefe("abgemeldet", !client.isSignedIn)
    try {
        client.devices()
        fehler.add("Geräteliste ohne Anmeldung erreichbar")
        geprueft++
    } catch (e: LumenException) {
        geprueft++
    }
}

// ---------------------------------------------------------------- Ablauf

/** Der Weg durch die Schritte, die ein Mensch beim ersten Start geht. */
private fun ablaufTests(adresse: String) {
    val ablage = MemoryTokenStore()
    val fuehrung = SessionController(ablage)

    fuehrung.start()
    pruefe("ohne Adresse wird nach dem Server gefragt",
        fuehrung.stage is Stage.NeedsServer)

    fuehrung.useServer("ganz.sicher.kein.server.invalid")
    val gescheitert = fuehrung.stage
    pruefe("unerreichbarer Server wird gemeldet", gescheitert is Stage.NeedsServer)
    pruefe("mit einer Begründung",
        (gescheitert as? Stage.NeedsServer)?.message?.isNotEmpty() == true)

    fuehrung.useServer(adresse)
    val gewaehlt = fuehrung.stage
    pruefe("eingerichteter Server führt zur Profilwahl", gewaehlt is Stage.ChoosingProfile)
    pruefe("Adresse gemerkt", ablage.serverUrl != null)

    val karten = (gewaehlt as Stage.ChoosingProfile).profiles
    pruefe("Profile da", karten.isNotEmpty())

    val besitzer = karten.first()
    fuehrung.chooseProfile(besitzer)
    pruefe("geschütztes Profil fragt nach dem Code",
        if (besitzer.needsCode) fuehrung.stage is Stage.Unlocking else true)

    fuehrung.unlock(besitzer, "falsch")
    pruefe("falscher Code meldet einen Fehler", fuehrung.lastError != null)
    pruefe("und meldet nicht „abgemeldet“",
        fuehrung.lastError?.contains("abgelaufen") != true &&
        fuehrung.lastError?.contains("gilt nicht mehr") != true)

    fuehrung.unlock(besitzer, "einsehrgutespasswort")
    pruefe("richtiger Code führt hinein", fuehrung.stage is Stage.SignedIn)
    pruefe("Token gemerkt", ablage.token != null)
    pruefe("Besitzer erkannt", fuehrung.isOwner)

    // Neu starten mit gemerktem Token: es darf nicht wieder gefragt werden.
    val zweiteSitzung = SessionController(ablage)
    zweiteSitzung.start()
    pruefe("gemerktes Token trägt über einen Neustart",
        zweiteSitzung.stage is Stage.SignedIn)

    // Ein ungültiges Token darf nicht in eine Sackgasse führen.
    val kaputt = MemoryTokenStore(serverUrl = ablage.serverUrl, token = "UNFUG")
    val dritte = SessionController(kaputt)
    dritte.start()
    pruefe("ungültiges Token führt zurück zur Profilwahl",
        dritte.stage is Stage.ChoosingProfile)
    pruefe("und wird weggeräumt", kaputt.token == null)

    // Einstellungen ändern. Steht hier, weil dieser Weg als einziger PATCH
    // benutzt — und PATCH kennt HttpURLConnection auf Android nicht. Die
    // App schickt stattdessen POST, und der Server nimmt beides entgegen.
    // Ohne diesen Test wäre genau das ungeprüft geblieben.
    val vorher = fuehrung.client.me().prefs["autoplay"]
    val gewechselt = if (vorher == "0") "1" else "0"
    val nachAenderung = fuehrung.client.updatePrefs(mapOf("autoplay" to gewechselt))
    gleich("Einstellung geändert", gewechselt, nachAenderung.prefs["autoplay"])
    gleich("und sie bleibt auch nach erneutem Nachfragen",
        gewechselt, fuehrung.client.me().prefs["autoplay"])
    // Wieder zurück, damit der Testlauf nichts hinterlässt.
    fuehrung.client.updatePrefs(mapOf("autoplay" to (vorher ?: "1")))

    fuehrung.signOut()
    pruefe("abgemeldet führt zur Profilwahl", fuehrung.stage is Stage.ChoosingProfile)
    pruefe("Token weg", ablage.token == null)
}

private fun uhrTests() {
    gleich("Uhr null", "0:00", clock(0.0))
    gleich("Uhr Minuten", "1:12", clock(72.0))
    gleich("Uhr Stunden", "1:24:07", clock(5047.0))
    gleich("Uhr Unfug", "0:00", clock(Double.NaN))
    gleich("Uhr unendlich", "0:00", clock(Double.POSITIVE_INFINITY))
}

// ---------------------------------------------------------------- Lizenz

/** Der Server nennt die Fundstelle seines Quelltexts. Daraus wird in den
 *  Einstellungen ein anklickbarer Link — also muss geprüft sein, was da
 *  steht, bevor die App ihn anbietet. */
private fun lizenzTests() {
    gleich("GitHub-Adresse geht durch",
        "https://github.com/jemand/lumen", sourceLink("https://github.com/jemand/lumen"))
    gleich("eigener Server mit Port geht durch",
        "http://192.168.1.9:3000/lumen.git", sourceLink("http://192.168.1.9:3000/lumen.git"))
    gleich("Leerzeichen stören nicht",
        "https://git.example.org/x", sourceLink("  https://git.example.org/x  "))
    gleich("javascript: wird abgelehnt", null, sourceLink("javascript:alert(1)"))
    gleich("data: wird abgelehnt", null, sourceLink("data:text/html,<b>x"))
    gleich("file: wird abgelehnt", null, sourceLink("file:///etc/passwd"))
    gleich("ohne Rechnernamen abgelehnt", null, sourceLink("https:///pfad"))
    gleich("leer bleibt leer", null, sourceLink(""))
    gleich("fehlt bleibt leer", null, sourceLink(null))
}

// ---------------------------------------------------------------- Pinnen

/** Der Fingerabdruck wird abgetippt oder aus der Verwaltung kopiert — mit
 *  Doppelpunkten, in Grossbuchstaben, mit Leerzeichen. Alles soll gehen,
 *  aber nichts Falsches soll durchrutschen. */
private fun pinTests() {
    val echt = "60:62:a4:8f:38:e8:ea:99:e9:62:26:ef:27:d8:f4:d1:" +
               "eb:91:ea:94:85:17:3c:45:7e:4a:4d:03:bc:91:f2:81"
    val blank = echt.replace(":", "")

    gleich("Doppelpunkte fallen weg", blank, Pinnen.saubern(echt))
    gleich("Grossbuchstaben werden klein", blank, Pinnen.saubern(echt.uppercase()))
    gleich("Leerzeichen stoeren nicht", blank, Pinnen.saubern("  $echt  "))
    gleich("ohne Eingabe nichts", null, Pinnen.saubern(null))
    gleich("leer ist nichts", null, Pinnen.saubern(""))
    gleich("zu kurz wird abgelehnt", null, Pinnen.saubern("60:62:a4"))
    gleich("etwas anderes als Hex wird abgelehnt", null, Pinnen.saubern("z".repeat(64)))

    // Derselbe Vergleichswert steht im Swift-Test. Weichen die beiden ab,
    // passt ein Fingerabdruck aus der Verwaltung auf einem der beiden
    // Geraete nicht — und niemand kaeme darauf, warum.
    //   echo -n lumen | sha256sum
    gleich("SHA-256 wie ueberall sonst",
        "456f45fddc2e725c2948d03317e2262c2ec8e86745af28dc7b2221987b858b50",
        java.security.MessageDigest.getInstance("SHA-256")
            .digest("lumen".toByteArray()).joinToString("") { "%02x".format(it) })
    pruefe("gleiche Zeichenketten gelten als gleich", Pinnen.gleichLang(blank, blank))
    pruefe("ein abweichendes Zeichen faellt auf",
        !Pinnen.gleichLang(blank, blank.dropLast(1) + "0"))
    pruefe("verschiedene Laengen fallen auf", !Pinnen.gleichLang(blank, blank.dropLast(2)))

    // Ohne brauchbaren Fingerabdruck gibt es keine Pruefung — dann gilt
    // die uebliche. Es darf auf keinen Fall eine herauskommen, die alles
    // durchwinkt.
    gleich("ohne Fingerabdruck keine Pruefung", null, Pinnen.pruefer(null))
    gleich("mit Unsinn keine Pruefung", null, Pinnen.pruefer("keinfingerabdruck"))
    pruefe("mit Fingerabdruck eine Pruefung", Pinnen.pruefer(echt) != null)
    // Die Namenspruefung winkt bewusst durch — der Fingerabdruck hat die
    // Gegenstelle schon bewiesen. Festhalten, dass sie nur zusammen mit
    // der Fabrik herauskommt: einzeln waere sie gefaehrlich.
    pruefe("die Namenspruefung gilt fuer jeden Namen",
        Pinnen.pruefer(echt)!!.namenspruefung.verify("irgendwas.example", null))

}

// ---------------------------------------------------------------- Verwaltung

/** Vollzugriff: was das Konto darüber sagt. */
private fun verwaltungTests() {
    // Ein älterer Server kennt isAdmin nicht: dann darf, wer Besitzer ist.
    val alt = Account.from(Json.parse("""
        {"id":"acc_1","name":"Alex","avatar":"av-a","isOwner":true,"isKid":false,
         "lockKind":"password","disabled":false}
    """))
    pruefe("ohne isAdmin gilt isOwner", alt.isAdmin)
    gleich("ohne adminLock keine Sperre", "none", alt.adminLock)
    pruefe("und damit auch keine zu öffnen", !alt.hatVerwaltungssperre)
    pruefe("ohne Gruppe keine Gruppe", alt.groupName == null)

    // Ein Mitverwalter: nicht Besitzer, aber Verwaltung — über die Gruppe.
    val mit = Account.from(Json.parse("""
        {"id":"acc_2","name":"Mara","avatar":"av-b","isOwner":false,"isKid":false,
         "lockKind":"pin","disabled":false,"groupId":"grp_1","groupName":"Eltern",
         "isAdmin":true,"adminLock":"totp"}
    """))
    pruefe("Mitverwalter darf in die Verwaltung", mit.isAdmin && !mit.isOwner)
    gleich("Gruppe", "Eltern", mit.groupName)
    gleich("Sperre", "totp", mit.adminLock)
    pruefe("Sperre ist zu öffnen", mit.hatVerwaltungssperre)

    // Ein gewöhnliches Mitglied: der Server schickt isAdmin ausdrücklich falsch.
    val normal = Account.from(Json.parse("""
        {"id":"acc_3","name":"Jonas","isOwner":false,"isAdmin":false}
    """))
    pruefe("Haushalt darf nicht in die Verwaltung", !normal.isAdmin)

}

/**
 * Das Pinnen gegen den wirklich laufenden Server.
 *
 * Die Pruefungen oben rechnen nur mit Zeichenketten. Ob die Verbindung
 * danach auch wirklich steht oder wirklich scheitert, zeigt sich erst an
 * einem Server mit selbst ausgestelltem Zertifikat — und genau der Fall
 * ist der Grund, warum es das Pinnen ueberhaupt gibt.
 */
private fun pinServerTests(httpsAdresse: String, fingerabdruck: String) {
    println("Gegen den verschluesselten Eingang: $httpsAdresse")

    // 1. Mit dem richtigen Fingerabdruck geht es.
    val richtig = ApiClient(ApiClient.normalizeAddress(httpsAdresse)!!)
    richtig.pinnen(fingerabdruck)
    try {
        val hello = richtig.hello()
        pruefe("mit dem richtigen Fingerabdruck kommt man durch", hello.server == "Lumen")
    } catch (e: Exception) {
        fehler.add("mit dem richtigen Fingerabdruck: ${e.message}")
        geprueft++
    }

    // 2. Mit einem abgewandelten nicht. Genau das soll das Pinnen leisten:
    //    ein anderes Zertifikat ist ein anderer Server.
    val blank = Pinnen.saubern(fingerabdruck)!!
    val verdreht = blank.dropLast(1) + (if (blank.last() == '0') '1' else '0')
    val falsch = ApiClient(ApiClient.normalizeAddress(httpsAdresse)!!)
    falsch.pinnen(verdreht)
    wirftFehler("ein abgewandelter Fingerabdruck wird abgewiesen") { falsch.hello() }

    // 3. Ganz ohne gilt die gewoehnliche Pruefung — und die schlaegt bei
    //    einem selbst ausgestellten Zertifikat fehl. Wichtig: sie schlaegt
    //    fehl und winkt nicht still durch.
    val ohne = ApiClient(ApiClient.normalizeAddress(httpsAdresse)!!)
    wirftFehler("ohne Fingerabdruck wird ein eigenes Zertifikat abgewiesen") { ohne.hello() }
}

// ---------------------------------------------------------------- Abzeichen, Technik, Profil

private fun titel(json: String): Item = Item.from(Json.parse(json))

private fun abzeichenTests() {
    val neu = titel("""{"id":"a","title":"A","neu":true,"neueFolgen":12,"qualitaet":"4K","hdr":"DV"}""")
    gleich("Neue Serie heisst Neu, nicht 12 neue Folgen", "Neu", neu.abzeichenLinks)
    gleich("4K und Dolby Vision rechts", listOf("4K", "Dolby Vision"), neu.abzeichenRechts)

    val eine = titel("""{"id":"b","title":"B","neueFolgen":1,"qualitaet":"HD","hdr":"HDR10"}""")
    gleich("Eine neue Folge", "Neue Folge", eine.abzeichenLinks)
    gleich("HD ist kein Abzeichen, HDR10 schon", listOf("HDR10"), eine.abzeichenRechts)

    val drei = titel("""{"id":"c","title":"C","neueFolgen":3}""")
    gleich("Drei neue Folgen", "3 neue Folgen", drei.abzeichenLinks)

    val nichts = titel("""{"id":"d","title":"D"}""")
    gleich("Ohne Angaben kein Abzeichen links", null, nichts.abzeichenLinks)
    pruefe("Ohne Angaben keins rechts", nichts.abzeichenRechts.isEmpty())
    gleich("Kanäle gelesen", 6, titel("""{"id":"e","title":"E","kanaele":6}""").kanaele)

    // Ähnliche Titel und Besetzung auf der Titelseite.
    val d = ItemDetail.from(Json.parse("""{"item":{"id":"m","title":"M","kind":"movie"},
        "cast":[{"name":"Alex Muster","rolle":"Hauptrolle","bild":"/api/v1/art/m/cast00"},{"name":"Bo"}],
        "similar":[{"id":"s1","title":"S1","neu":true},{"id":"s2","title":"S2"}]}"""))
    gleich("Zwei aus der Besetzung", 2, d.cast.size)
    gleich("Rolle gelesen", "Hauptrolle", d.cast[0].rolle)
    gleich("Ohne Bild null", null, d.cast[1].bild)
    gleich("Ähnliche gelesen", listOf("s1", "s2"), d.similar.map { it.id })
    pruefe("Ähnliche tragen Abzeichen", d.similar[0].neu)
    val alt = ItemDetail.from(Json.parse("""{"item":{"id":"m","title":"M"}}"""))
    pruefe("Älterer Server: keine Besetzung, keine Ähnlichen", alt.cast.isEmpty() && alt.similar.isEmpty())
}

private fun technikTests() {
    val datei = MediaFile.from(Json.parse("""{"id":"f","hdr":"HDR10","tracks":[
        {"index":1,"kind":"audio","language":"de","channels":6},
        {"index":2,"kind":"audio","language":"en","channels":2},
        {"index":3,"kind":"audio","language":"und","channels":2},
        {"index":4,"kind":"subtitle","language":"de"},
        {"index":5,"kind":"subtitle","language":"en","bitmap":true}]}"""))
    val film = titel("""{"id":"m","title":"M","qualitaet":"4K","hdr":"HDR10"}""")
    val zeile = Technik.abzeichen(film, datei, direkt = true)
    gleich("Technik-Zeile",
        listOf("4K", "HDR10", "5.1", "DE · EN", "Untertitel DE", "Direkt abspielbar"),
        zeile.map { it.text })
    pruefe("Untertitel mit Symbol", zeile.first { it.text.startsWith("Untertitel") }.untertitel)
    gleich("Direkt ist grün", "gut", zeile.last().art)
    gleich("Umgewandelt ist blau", "Wird umgewandelt",
        Technik.abzeichen(film, datei, direkt = false).last().text)
    pruefe("Ohne Plan kein Urteil",
        Technik.abzeichen(film, datei, direkt = null).none { it.art != "normal" })
    gleich("Stereo", "Stereo", Technik.tonName(2))
    gleich("7.1", "7.1", Technik.tonName(8))
    gleich("Mono nichts", null, Technik.tonName(1))
    pruefe("Ohne Datei und Qualität nichts",
        Technik.abzeichen(titel("""{"id":"x","title":"X"}"""), null, null).isEmpty())
    gleich("Ohne Datei: Kanäle aus der Karte", listOf("HD", "Stereo"),
        Technik.abzeichen(titel("""{"id":"x","title":"X","qualitaet":"HD","kanaele":2}"""), null, null)
            .map { it.text })
}

private fun untertitelStilTests() {
    val leer = UntertitelStil.aus(emptyMap())
    gleich("Vorgabe normal", "normal", leer.groesse)
    pruefe("Vorgabe mit Hintergrund", leer.hintergrund)
    gleich("Normal wie Media3", UntertitelStil.GRUNDGROESSE, leer.textAnteil)
    val gross = UntertitelStil.aus(mapOf("utGroesse" to "gross", "utHintergrund" to "0"))
    pruefe("Gross ist grösser", gross.textAnteil > leer.textAnteil)
    pruefe("Ohne Hintergrund", !gross.hintergrund)
    pruefe("Klein ist kleiner", UntertitelStil.aus(mapOf("utGroesse" to "klein")).textAnteil < leer.textAnteil)
    gleich("Unbekannte Grösse wird normal", "normal", UntertitelStil.aus(mapOf("utGroesse" to "riesig")).groesse)
}

private fun profilbildTests() {
    gleich("Zwölf Figuren", 12, Figuren.reihenfolge.size)
    gleich("Figur gefunden", "eule", Profilbild.figur("fig-eule")?.name)
    gleich("Farbe ist keine Figur", null, Profilbild.figur("av-b"))
    gleich("Unbekannte Figur", null, Profilbild.figur("fig-einhorn"))
    pruefe("Bild erkannt", Profilbild.istBild("bild") && !Profilbild.istBild("av-a"))
    gleich("Kreis als Pfad", "M9 13a1 1 0 1 0 2 0a1 1 0 1 0 -2 0z", Figuren.kreis(10.0, 13.0, 1.0))
    gleich("Kleiner Kreis", "M8.7 13a0.8 0.8 0 1 0 1.6 0a0.8 0.8 0 1 0 -1.6 0z", Figuren.kreis(9.5, 13.0, 0.8))
    gleich("Server bestimmt die Auswahl", listOf("wal", "fuchs"),
        Figuren.angeboten(listOf("wal", "unbekannt", "fuchs")).map { it.name })
    gleich("Ohne Liste alle", 12, Figuren.angeboten(emptyList()).size)
    // Jeder Pfad nur aus SVG-Befehlen und Zahlen.
    pruefe("Pfade sauber", Figuren.reihenfolge.all { f ->
        f.formen.all { it.pfad.matches(Regex("[MmLlHhVvCcSsAaZz0-9 .,-]+")) } })

    val c = ApiClient("http://nas:8097", "tok")
    gleich("Profilbild-Adresse ohne Schlüssel", "http://nas:8097/api/v1/avatar/acc_1?v=5",
        c.profilbildUrl("acc_1", "bild", 5))
    gleich("Profilbild vor der Anmeldung ohne alles", "http://nas:8097/api/v1/avatar/acc_1?v=5",
        ApiClient("http://nas:8097").profilbildUrl("acc_1", "bild", 5))
    c.medienSchluesselSetzen("k")
    gleich("Profilbild angemeldet mit Schlüssel", "http://nas:8097/api/v1/avatar/acc_1?key=k&v=5",
        c.profilbildUrl("acc_1", "bild", 5))
    gleich("Ohne eigenes Bild keine Adresse", null, c.profilbildUrl("acc_1", "fig-wal"))
    pruefe("Vorschau mit Schlüssel", c.vorschauUrl("it_1")?.endsWith("/api/v1/art/it_1/vorschau?key=k") == true)

    val me = Me.from(Json.parse("""{"account":{"id":"a","name":"Alex","avatar":"fig-wal"},
        "neuigkeiten":3,"figuren":["fuchs","eule"]}"""))
    gleich("Ungelesene aus /me", 3, me.neuigkeiten)
    gleich("Figuren aus /me", listOf("fuchs", "eule"), me.figuren)
    gleich("Specials", "Specials", AmStueck.staffelName(titel("""{"id":"s0","title":"Staffel 0","seasonNo":0}""")))
    gleich("Staffel", "Staffel 2", AmStueck.staffelName(titel("""{"id":"s2","title":"Staffel 2","seasonNo":2}""")))
}

private fun neuigkeitenTests() {
    val n = Neuigkeiten.from(Json.parse("""{"eintraege":[
        {"art":"folgen","itemId":"se_1","titel":"Nordlicht","text":"2 neue Folgen","at":"2026-10-29T09:20:40.5+01:00","anzahl":2},
        {"art":"neu","titel":"12 neue Titel","text":"zuletzt: X","at":"2026-10-20T08:00:00Z","anzahl":12}],
        "ungelesen":1,"gelesenBis":"2026-10-25T00:00:00Z"}"""))
    gleich("Zwei Einträge", 2, n.eintraege.size)
    gleich("Ungelesen", 1, n.ungelesen)
    pruefe("Erster ist neu", n.istNeu(n.eintraege[0]))
    pruefe("Zweiter ist gelesen", !n.istNeu(n.eintraege[1]))
    gleich("Ohne Titel-Kennung null", null, n.eintraege[1].itemId)
    val nie = Neuigkeiten.from(Json.parse("""{"eintraege":[{"art":"neu","titel":"T","text":"x","at":"2026-10-20T08:00:00Z"}],
        "ungelesen":1,"gelesenBis":"0001-01-01T00:00:00Z"}"""))
    pruefe("Nie gelesen: alles neu", nie.istNeu(nie.eintraege[0]))
    gleich("Kurzes Datum", "29. Okt.", Zeitpunkt.kurz("2026-10-29T09:20:40+01:00", java.time.ZoneId.of("Europe/Zurich")))
    gleich("Kaputtes Datum leer", "", Zeitpunkt.kurz("gestern"))
}

// ---------------------------------------------------------------- Titelseite, Raster, Einladung

// ---------------------------------------------------------------- Fassungen und Teile

private fun datei(json: String) = MediaFile.from(Json.parse(json))

private fun fassungenTests() {
    // „American Psycho (2000)": MKV in 1080p und AVI in 400p — zwei Fassungen.
    // Der Server schickt sie schon geordnet, die beste vorn.
    val mkv = datei("""{"id":"mkv","path":"/f/American Psycho (2000).mkv","partNo":1,"container":"mkv",
        "height":1080,"sizeBytes":7900000000,"durationMs":6120000,"preferred":true}""")
    val avi = datei("""{"id":"avi","path":"/f/American Psycho (2000).avi","partNo":1,"container":"avi",
        "height":400,"sizeBytes":700000000,"durationMs":6120000}""")
    val weg = datei("""{"id":"weg","partNo":1,"height":2160,"missing":true}""")
    gleich("partNo gelesen", 1, mkv.partNo)
    pruefe("preferred gelesen", mkv.preferred && !avi.preferred)
    gleich("ohne partNo: Teil 1", 1, datei("""{"id":"x"}""").partNo)

    gleich("Fassungen ohne fehlende", listOf("mkv", "avi"), Fassungen.liste(listOf(mkv, avi, weg)).map { it.id })
    pruefe("zwei Fassungen: fragen", Fassungen.mehrere(listOf(mkv, avi)))
    pruefe("eine Fassung: nicht fragen", !Fassungen.mehrere(listOf(mkv, weg)))
    gleich("Name 1080p", "1080p · MKV · 7,9 GB", Fassungen.name(mkv))
    gleich("Name klein, in MB", "400p · AVI · 700 MB", Fassungen.name(avi))
    gleich("4K", "4K", Fassungen.aufloesung(2160))
    gleich("720p", "720p", Fassungen.aufloesung(720))
    gleich("ohne Höhe nichts", null, Fassungen.aufloesung(null))
    gleich("ganze GB ohne Komma", "8 GB", Fassungen.groesse(8_000_000_000L))
    gleich("winzig: 1 MB", "1 MB", Fassungen.groesse(1234L))
    gleich("ohne Grösse nichts", "1080p", Fassungen.name(datei("""{"id":"y","height":1080}""")))

    gleich("gewählte Datei", "avi", Fassungen.datei(listOf(mkv, avi), "avi")?.id)
    gleich("ohne Wahl: die erste", "mkv", Fassungen.datei(listOf(mkv, avi), null)?.id)
    gleich("Wahl fehlt: die erste vorhandene", "mkv", Fassungen.datei(listOf(weg, mkv), "weg")?.id)

    val einzeln = Fassungen.teile(listOf(mkv, avi), avi)
    pruefe("Fassungen sind keine Teile", !einzeln.mehrere)
    gleich("eine Datei: Stelle wie gegeben", FilmTeile.Stelle(0, 0.0, 300.0), einzeln.stelle(300.0))
    gleich("eine Datei: keine Beschriftung", null, einzeln.beschriftung(0))

    // „Apocalypse Now (1979) a/b/c.avi": ein Film in drei Teilen.
    val a = datei("""{"id":"a","partNo":1,"durationMs":3000000}""")
    val b = datei("""{"id":"b","partNo":2,"durationMs":2000000}""")
    val c = datei("""{"id":"c","partNo":3,"durationMs":1000000}""")
    val teile = Fassungen.teile(listOf(c, a, b), a)
    gleich("Teile der Reihe nach", listOf("a", "b", "c"), teile.dateien.map { it.id })
    gleich("Dauer aller Teile", 6000.0, teile.gesamtSekunden)
    gleich("Anfang: Teil 1", FilmTeile.Stelle(0, 0.0, 0.0), teile.stelle(0.0))
    gleich("mitten in Teil 1", FilmTeile.Stelle(0, 0.0, 2999.0), teile.stelle(2999.0))
    gleich("genau an der Grenze: Teil 2", FilmTeile.Stelle(1, 3000.0, 0.0), teile.stelle(3000.0))
    gleich("in Teil 3", FilmTeile.Stelle(2, 5000.0, 600.0), teile.stelle(5600.0))
    gleich("hinter dem Ende: von vorn", FilmTeile.Stelle(0, 0.0, 0.0), teile.stelle(5998.0))
    gleich("Teil 2 beginnt nach Teil 1", 3000.5, teile.weiterAb(0))
    gleich("weiterAb landet in Teil 2", 1, teile.stelle(teile.weiterAb(0)).teil)
    gleich("weiterAb landet in Teil 3", 2, teile.stelle(teile.weiterAb(1)).teil)
    gleich("Beschriftung", "Teil 2 von 3", teile.beschriftung(1))
    pruefe("Teil 3 ist der letzte", teile.istLetzter(2) && !teile.istLetzter(1))
    gleich("vor Teil 3", 5000.0, teile.vorher(2))
    // Fehlt Teil 2, läuft nur Teil 1 — wie im Browser.
    gleich("Lücke: bis dahin", listOf("a"), Fassungen.teile(listOf(a, c), a).dateien.map { it.id })
    // Wer mit Teil 2 beginnt (etwa weil Teil 1 fehlt), spielt ihn allein.
    gleich("nur Teil 2", listOf("b"), Fassungen.teile(listOf(b), b).dateien.map { it.id })
}

private fun titelseiteTests() {
    val film = titel("""{"id":"m","title":"M","kind":"movie","year":2019,"ageYears":12,
        "runtimeMs":7080000,"genres":["Drama","Thriller","Krimi"],"imdbRating":7.8,"imdbVotes":12345,
        "resumeMs":4324000}""")
    gleich("Meta-Zeile wie im Browser",
        "2019 · ab 12 · 1 Std. 58 Min. · Drama · Thriller · Krimi · IMDb 7,8 (12’345)",
        Titelseite.metaZeile(film))
    pruefe("Stand über einer Minute", Titelseite.hatStand(film))
    gleich("Weiter bei", "Weiter bei 1:12:04", Titelseite.abspielText(film))
    val frisch = titel("""{"id":"n","title":"N","resumeMs":30000}""")
    gleich("Unter einer Minute: Ansehen", "Ansehen", Titelseite.abspielText(frisch))
    pruefe("und kein Von vorn", !Titelseite.hatStand(frisch))
    gleich("ohne alles leer", "", Titelseite.metaZeile(titel("""{"id":"x","title":"X"}""")))

    val serie = titel("""{"id":"s","title":"S","kind":"series","gesehen":3,"folgen":10}""")
    gleich("Serie angefangen", "Weiterschauen", Titelseite.serienText(serie))
    gleich("Serie neu", "Ansehen", Titelseite.serienText(titel("""{"id":"t","title":"T","kind":"series"}""")))
    gleich("Serie: angefangene Folge zählt",
        "Weiterschauen", Titelseite.serienText(titel("""{"id":"t","title":"T","kind":"series"}"""),
            listOf(titel("""{"id":"e","title":"E","progress":30}"""))))
    pruefe("Serienstand in der Meta-Zeile", Titelseite.metaZeile(serie).endsWith("3 von 10 Folgen"))

    val folge = titel("""{"id":"e","title":"E","kind":"episode","runtimeMs":2880000,"premiered":"2019-04-14"}""")
    gleich("Folgenzeile", "48 Min. · 14.4.2019", Titelseite.folgenZeile(folge))
    gleich("Erstausstrahlung gelesen", "2019-04-14", folge.premiered)
    gleich("Datum mit Uhrzeit", "1.12.2020", Titelseite.datum("2020-12-01T00:00:00Z"))
    gleich("Unsinn als Datum", null, Titelseite.datum("bald"))
    gleich("Folge angefangen", "Weiter", Titelseite.folgenKnopf(titel("""{"id":"e","title":"E","progress":40}""")))
    gleich("Folge gesehen", "Ansehen",
        Titelseite.folgenKnopf(titel("""{"id":"e","title":"E","progress":40,"finished":true}""")))

    val datei = MediaFile.from(Json.parse("""{"id":"f","path":"/m/Film (2019).mkv","height":1080,
        "videoCodec":"hevc","hdr":"HDR10","sizeBytes":4200000000}"""))
    gleich("Dateizeile", "1080p · hevc · HDR10 · 4,2 GB", Titelseite.dateiZeile(datei))
    gleich("Dateiname ohne Pfad", "Film (2019).mkv", datei.fileName)
    gleich("fehlende Datei", "fehlt",
        Titelseite.dateiZeile(MediaFile.from(Json.parse("""{"id":"g","missing":true}"""))))
    gleich("Tausender klein", "999", Titelseite.tausender(999))
    gleich("Tausender gross", "1’234’567", Titelseite.tausender(1234567))

    // Specials: der Server lässt die Nummer 0 weg.
    val ohneNummer = titel("""{"id":"s0","title":"Staffel 0","kind":"season"}""")
    gleich("Staffel ohne Nummer heisst Specials", "Specials", AmStueck.staffelName(ohneNummer))
    gleich("Specials ans Ende", listOf("s1", "s2", "s0"), AmStueck.staffelnOrdnen(listOf(ohneNummer,
        titel("""{"id":"s2","title":"Staffel 2","seasonNo":2}"""),
        titel("""{"id":"s1","title":"Staffel 1","seasonNo":1}"""))).map { it.id })

    // Filmreihe: Knopf, Unterzeile, eigenes Bild.
    val reihe = Reihe.from(Json.parse("""{"id":"col_1","name":"Dune","source":"tmdb","anzahl":3,
        "poster":"/api/v1/art/col_1/poster",
        "items":[{"id":"a","title":"Teil 1","finished":true},{"id":"b","title":"Teil 2"},{"id":"c","title":"Teil 3"}]}"""))
    gleich("Reihe: gesehen", 1, reihe.gesehen)
    gleich("Reihe: weiter mit", "b", reihe.naechster?.id)
    gleich("Reihe: Knopf", "Weiter mit Teil 2", reihe.abspielText)
    gleich("Reihe: Unterzeile", "3 Filme · 1 gesehen · zusammengestellt nach TMDb", reihe.unterzeile)
    gleich("Reihe: eigenes Poster", "/api/v1/art/col_1/poster", reihe.poster)
    val c = ApiClient("http://nas:8097", "tok")
    c.medienSchluesselSetzen("k")
    gleich("Reihe: Bild mit Schlüssel", "http://nas:8097/api/v1/art/col_1/poster?key=k", c.reiheBildUrl(reihe))
    pruefe("Reihe ohne Hintergrund nimmt den ersten Film",
        c.reiheBildUrl(reihe, breit = true)?.contains("/art/a/") == true)
    val alle = reihe.copy(items = reihe.items.map { it.copy(finished = true) })
    gleich("Reihe ganz gesehen: wieder von vorn", "a", alle.naechster?.id)
    gleich("Reihe ganz gesehen: Ansehen", "Ansehen", alle.abspielText)
    gleich("Reihe ohne Bilder", null, Reihe.from(Json.parse("""{"id":"r","name":"R"}""")).poster)
}

private fun rasterTests() {
    val s = Seite.from(Json.parse("""{"items":[{"id":"a","title":"A"},{"id":"b","title":"B"}],
        "gesamt":5,"offset":0,"weiter":true,"genres":["Drama","Komödie"]}"""))
    gleich("Seite: Titel", 2, s.items.size)
    gleich("Seite: nächster Offset", 2, s.naechsterOffset)
    gleich("Seite: Zeile", "5 Titel", s.zeile(null))
    gleich("Seite: Zeile mit Genre", "5 Titel · Komödie", s.zeile("Komödie"))
    val n = Seite.from(Json.parse("""{"items":[{"id":"b","title":"B"},{"id":"c","title":"C"}],
        "gesamt":5,"offset":2,"weiter":false,"genres":[]}"""))
    val zusammen = s.mit(n)
    gleich("Anhängen ohne Doppelte", listOf("a", "b", "c"), zusammen.items.map { it.id })
    pruefe("am Ende kein weiter", !zusammen.weiter)
    gleich("Genres bleiben", listOf("Drama", "Komödie"), zusammen.genres)
    gleich("Offset bleibt der erste", 0, zusammen.offset)

    val g = Device.from(Json.parse("""{"accountId":"a","name":"Wohnzimmer","kind":"tv",
        "createdAt":"2026-10-01T10:00:00Z","lastSeen":"2026-10-29T10:00:00Z"}"""))
    gleich("Gerät", "Wohnzimmer", g.name)
    gleich("Gerät: Art", "Fernseher", g.artText)
    gleich("Gerät: unbekannte Art", "konsole", Device("x", "konsole", "", "").artText)

    val w = Wish.from(Json.parse("""{"id":"w","by":"Mara","title":"Dune","year":2021,"state":"working","mine":true}"""))
    gleich("Wunsch: von", "von Mara (du)", w.vonText)
    gleich("Wunsch: Titel", "Dune (2021)", w.titelZeile)
    gleich("Wunsch: Stand", "in Arbeit", w.stateLabel)
    gleich("Wunsch ohne Jahr", "X", Wish("w", "A", "av-a", "X", 0, "open", false).titelZeile)
}

private fun einladungTests() {
    gleich("alles da", null, Einladung.pruefen("ABC123", "Mara", "password", "langesgeheimnis"))
    pruefe("ohne Code", Einladung.pruefen("  ", "Mara", "none", "") != null)
    pruefe("ohne Name", Einladung.pruefen("ABC", " ", "none", "") != null)
    pruefe("PIN zu kurz", Einladung.pruefen("ABC", "Mara", "pin", "123") != null)
    pruefe("PIN nur Ziffern", Einladung.pruefen("ABC", "Mara", "pin", "12a4") != null)
    gleich("PIN gut", null, Einladung.pruefen("ABC", "Mara", "pin", "1234"))
    pruefe("Passwort zu kurz", Einladung.pruefen("ABC", "Mara", "password", "kurz") != null)
    gleich("ohne Code offen", null, Einladung.pruefen("ABC", "Mara", "none", ""))
    pruefe("unbekannte Art", Einladung.pruefen("ABC", "Mara", "totp", "123456") != null)
    gleich("Code ohne Leerzeichen", "ABCD1234", Einladung.codeSaubern(" ABCD 1234 "))

    // Der Controller prüft vorab und verbraucht keinen Code beim Server.
    val s = SessionController(MemoryTokenStore(serverUrl = "http://127.0.0.1:1"))
    s.redeemInvite("ABC", "Mara", "pin", "12")
    gleich("zu kurzer PIN gemeldet", "Ein PIN hat mindestens vier Ziffern.", s.lastError)
    pruefe("und nicht angemeldet", s.stage !is Stage.SignedIn)
}

private fun einstellungenTests() {
    val leer = emptyMap<String, String>()
    pruefe("Autoplay ist voreingestellt an", Einstellungen.autoplay(leer))
    pruefe("Vorspann ist voreingestellt aus", !Einstellungen.vorspann(leer))
    pruefe("Bewertungen bleiben voreingestellt privat", !Einstellungen.bewertungenTeilen(leer))
    pruefe("Autoplay aus", !Einstellungen.autoplay(mapOf("autoplay" to "0")))
    gleich("Ton ohne Wahl", "Wie in der Datei", Einstellungen.tonText(leer))
    gleich("Ton Deutsch", "Deutsch", Einstellungen.tonText(mapOf("tonSprache" to "de")))
    gleich("Untertitel aus", "Aus", Einstellungen.untertitelText(mapOf("untertitel" to "aus")))
    gleich("Untertitel Englisch", "Englisch", Einstellungen.untertitelText(mapOf("untertitel" to "en")))
    gleich("Ton: erste Wahl ist die Datei", "", Einstellungen.TON_WAHL.first().first)
    gleich("Untertitel: erste Wahl ist aus", "aus", Einstellungen.UNTERTITEL_WAHL.first().first)
    gleich("weiterschalten", "en", Einstellungen.naechste(Einstellungen.TON_WAHL, "de"))
    gleich("vom Ende an den Anfang", "",
        Einstellungen.naechste(Einstellungen.TON_WAHL, Einstellungen.TON_WAHL.last().first))
    gleich("unbekannt fängt vorn an", "", Einstellungen.naechste(Einstellungen.TON_WAHL, "xx"))
    // Die gemerkte Sprache wirkt auf die Vorbelegung im Player.
    val spuren = listOf(
        Track(1, "audio", null, "en", null, 2, false, false, false, false),
        Track(2, "audio", null, "de", null, 6, false, false, false, false),
        Track(3, "subtitle", null, "en", null, 0, false, false, false, false))
    gleich("gemerkter Ton gilt", 2, Spuren.vorbelegen(spuren, mapOf("tonSprache" to "de")).ton)
    gleich("gemerkte Untertitel gelten", 3, Spuren.vorbelegen(spuren, mapOf("untertitel" to "en")).untertitel)

    val besitzer = Account.from(Json.parse("""{"id":"a","name":"Y","isOwner":true}"""))
    gleich("Rolle Besitzer", "Besitzer dieses Servers", Einstellungen.rolle(besitzer))
    val kind = Account.from(Json.parse("""{"id":"b","name":"K","isKid":true,"isAdmin":false}"""))
    gleich("Rolle Kind", "Kinderprofil", Einstellungen.rolle(kind))
}

// ---------------------------------------------------------------- Mehrere Server

private fun serverlisteTests() {
    // Früher: genau ein Server, Schlüssel server/token/fingerabdruck. Beim
    // ersten Start wird daraus der erste Eintrag — ohne neue Anmeldung.
    val fp = "ab".repeat(32)
    val alt = MemorySchluesselablage(mapOf(
        "server" to "http://nas.local:8097", "token" to "ALTES-TOKEN", "fingerabdruck" to fp))
    val liste = Serverliste(alt)
    gleich("übernommen: ein Eintrag", 1, liste.server.size)
    val erster = liste.server.first()
    gleich("übernommen: Adresse", "http://nas.local:8097", erster.adresse)
    gleich("übernommen: Name aus der Adresse", "nas", erster.name)
    gleich("übernommen: Fingerabdruck", fp, erster.fingerabdruck)
    gleich("übernommen: gewählt", erster.id, liste.aktiv?.id)
    gleich("übernommen: Token bleibt", "ALTES-TOKEN", liste.token)
    gleich("übernommen: TokenStore-Adresse", "http://nas.local:8097", liste.serverUrl)
    pruefe("alte Schlüssel weg", alt.lesen("server") == null && alt.lesen("token") == null &&
        alt.lesen("fingerabdruck") == null)
    pruefe("Token steht nicht in der Liste", alt.lesen(Serverliste.LISTE)?.contains("ALTES-TOKEN") == false)

    // Ein zweiter Start übernimmt nichts doppelt.
    val nochmal = Serverliste(alt)
    gleich("zweiter Start: weiterhin ein Eintrag", 1, nochmal.server.size)
    gleich("zweiter Start: Token da", "ALTES-TOKEN", nochmal.token)

    // Fernseher: die alten Daten lagen unverschlüsselt woanders.
    val klar = MemorySchluesselablage(mapOf("server" to "http://192.168.1.20:8097", "token" to "TV"))
    val sicher = MemorySchluesselablage()
    val tv = Serverliste(sicher, alt = klar)
    gleich("aus anderer Ablage übernommen", "TV", tv.token)
    gleich("Name ist die Adresse", "192.168.1.20", tv.aktiv?.name)
    pruefe("unverschlüsselte Ablage ist leer", klar.schluessel().isEmpty())

    // Nichts Altes: leere Liste, keiner gewählt.
    val leer = Serverliste(MemorySchluesselablage())
    pruefe("leer", leer.server.isEmpty() && leer.aktiv == null && leer.token == null)

    // Ein zweiter Server: eigenes Token, das erste bleibt unberührt.
    var zeit = 1000L
    val l = Serverliste(MemorySchluesselablage(mapOf("server" to "http://a:8097", "token" to "TA")),
                        uhr = { zeit })
    val a = l.aktiv!!
    zeit = 2000L
    val b = l.merken("http://b:8097", name = "Ferienhaus")
    gleich("zwei Server", 2, l.server.size)
    gleich("der neue ist gewählt", b.id, l.aktiv?.id)
    pruefe("der neue hat noch kein Token", l.token == null)
    l.token = "TB"
    pruefe("Token getrennt je Server", l.angemeldet(a.id) && l.angemeldet(b.id))
    l.waehlen(a.id)
    gleich("zurück zum ersten: sein Token", "TA", l.token)
    gleich("zuletzt benutzt aktualisiert", 2000L, l.aktiv?.zuletzt)
    gleich("Reihenfolge bleibt", listOf("a", "Ferienhaus"), l.server.map { it.name })

    // Dieselbe Adresse noch einmal: derselbe Eintrag, Token bleibt.
    val wieder = l.merken("HTTP://B:8097/")
    gleich("gleiche Adresse, gleicher Eintrag", b.id, wieder.id)
    gleich("kein dritter Eintrag", 2, l.server.size)
    gleich("Token des wiedergefundenen", "TB", l.token)

    // Umbenennen hält; der Name vom Server überschreibt es nicht.
    l.umbenennen(a.id, "  Wohnzimmer  ")
    gleich("umbenannt", "Wohnzimmer", l.finde(a.id)?.name)
    l.nameVomServer(a.id, "anders")
    gleich("Name vom Server ändert umbenannten nicht", "Wohnzimmer", l.finde(a.id)?.name)
    l.umbenennen(a.id, "")
    gleich("leerer Name: zurück zum Standard", "a", l.finde(a.id)?.name)
    l.nameVomServer(a.id, "Keller")
    gleich("dann gilt wieder der vom Server", "Keller", l.finde(a.id)?.name)
    l.umbenennen(a.id, "x".repeat(80))
    gleich("Name gekürzt", Serverliste.NAME_LAENGE, l.finde(a.id)?.name?.length)

    // Fingerabdruck gehört zum Eintrag.
    l.waehlen(b.id)
    l.fingerabdruck = fp
    gleich("Fingerabdruck am Eintrag", fp, l.finde(b.id)?.fingerabdruck)
    pruefe("der andere hat keinen", l.finde(a.id)?.fingerabdruck == null)
    pruefe("von unterwegs erkannt", l.finde(b.id)!!.vonUnterwegs)
    gleich("Adresse kurz", "b:8097", l.finde(b.id)?.adresseKurz)

    // Entfernen: samt Token, und der gewählte ist dann keiner mehr.
    l.entfernen(b.id)
    gleich("einer weniger", 1, l.server.size)
    pruefe("keiner gewählt", l.aktiv == null && l.serverUrl == null && l.token == null)
    pruefe("Token des entfernten weg", !l.angemeldet(b.id))
    pruefe("Token des anderen bleibt", l.angemeldet(a.id))

    // Kaputt gespeichert: die App startet trotzdem.
    val kaputt = Serverliste(MemorySchluesselablage(mapOf(Serverliste.LISTE to "{nicht json")))
    pruefe("kaputte Liste ergibt leere", kaputt.server.isEmpty())

    gleich("Standardname: Hostname", "192.168.1.20", Serverliste.standardName("http://192.168.1.20:8097"))
    gleich("Standardname: .local weg", "nas", Serverliste.standardName("http://nas.local:8097"))
    gleich("Standardname: eigener Name vom Server", "Keller",
        Serverliste.standardName("http://x:1", Hello("Keller", "1", true, true)))
    gleich("Standardname: der vom Besitzer gesetzte geht vor", "Mein NAS",
        Serverliste.standardName("http://x:1", Hello.from(Json.parse(
            """{"server":"Lumen","name":"Mein NAS","version":"1","setUp":true}"""))))
    pruefe("leerer Name zählt nicht", Hello.from(Json.parse("""{"server":"Lumen","name":"  "}""")).name == null)
    gleich("Standardname: „Lumen\" sagt nichts", "x",
        Serverliste.standardName("http://x:1", Hello("Lumen", "1", true, true)))

    // Der Controller ohne erreichbaren Server: die Liste mit Meldung,
    // nicht die leere Adresseingabe.
    val ablage = MemorySchluesselablage(mapOf("server" to "http://127.0.0.1:1", "token" to "T"))
    val sl = Serverliste(ablage)
    val c = SessionController(sl)
    c.start()
    val st = c.stage
    pruefe("unerreichbar beim Start: Serverliste", st is Stage.ChoosingServer)
    pruefe("mit Meldung", (st as? Stage.ChoosingServer)?.message?.isNotEmpty() == true)
    gleich("der gewählte ist markiert", sl.aktiv?.id, (st as? Stage.ChoosingServer)?.aktivId)
    pruefe("Token bleibt dabei", sl.token == "T")
    c.serverHinzufuegen()
    pruefe("hinzufügen: Adresseingabe", c.stage is Stage.NeedsServer)
    c.useServer("127.0.0.1:2")
    pruefe("unerreichbare neue Adresse wird nicht gemerkt", sl.server.size == 1)
    pruefe("Token wandert nicht zur neuen Adresse", c.client.token == null)
    c.serverWahl()
    pruefe("zurück zur Liste", c.stage is Stage.ChoosingServer)
    c.serverUmbenennen(sl.aktiv!!.id, "Daheim")
    gleich("umbenannt in der Ansicht", "Daheim", (c.stage as Stage.ChoosingServer).server.first().name)
    c.serverEntfernen(sl.server.first().id)
    pruefe("letzter entfernt: Adresseingabe", c.stage is Stage.NeedsServer)
    pruefe("und nichts mehr gespeichert", sl.server.isEmpty() && sl.token == null)

    // Mit gewöhnlichem TokenStore bleibt alles wie früher.
    val einfach = SessionController(MemoryTokenStore())
    einfach.start()
    pruefe("ohne Liste: Adresseingabe", einfach.stage is Stage.NeedsServer)
    pruefe("ohne Liste: keine Server", einfach.server.isEmpty())
}

private fun anmeldecodeTests() {
    gleich("Code anzeigen", "482 913", Anmeldecode.anzeige("482913"))
    gleich("unvollständig bleibt", "4829", Anmeldecode.anzeige("4829"))
    gleich("Eingabe säubern", "482913", Anmeldecode.saubern(" 482 9-13 7"))
    pruefe("vollständig", Anmeldecode.vollstaendig("482 913"))
    pruefe("unvollständig", !Anmeldecode.vollstaendig("48291"))
    gleich("Rest", "4:59", Anmeldecode.rest(299))
    gleich("Rest nie negativ", "0:00", Anmeldecode.rest(-5))
    gleich("Rest-Text", "Noch 0:07 gültig", Anmeldecode.restText(7))
    pruefe("abgelaufen-Text", Anmeldecode.restText(0).startsWith("Abgelaufen"))
    gleich("Meldung", "Wohnzimmer ist angemeldet", Anmeldecode.angemeldetText("Wohnzimmer"))
    gleich("Meldung ohne Namen", "Das Gerät ist angemeldet", Anmeldecode.angemeldetText(""))
    val c = AnmeldeCode.from(Json.parse("""{"code":"004711","geheim":"g","gueltigSek":300}"""))
    gleich("Code gelesen", "004 711", c.anzeige)
    gleich("Gültigkeit gelesen", 300, c.gueltigSek)
    wirftFehler("ohne Geheimnis geht es nicht") { AnmeldeCode.from(Json.parse("""{"code":"1"}""")) }

    // Freigeben prüft vorab, ohne den Server zu fragen.
    val s = SessionController(MemoryTokenStore(serverUrl = "http://127.0.0.1:1"))
    gleich("zu kurzer Code", null, s.codeFreigeben("123"))
    gleich("mit Meldung", "Der Code hat sechs Ziffern.", s.lastError)
}

/**
 * Gegen einen frisch gestarteten Server: einrichten, ein „Handy" meldet
 * einen „Fernseher" mit Code an. Mit [zweite] dazu der Wechsel zwischen
 * zwei Servern, jeder mit eigenem Token.
 */
private fun codeServerTests(adresse: String, zweite: String?) {
    val passwort = "einsehrgutespasswort"

    // Das Handy: richtet den Server ein (oder meldet sich an).
    val handy = ApiClient(ApiClient.normalizeAddress(adresse)!!)
    if (!handy.hello().setUp) {
        handy.setUpOwner("Alex", passwort, "Testhandy")
    } else {
        handy.unlock(handy.profiles().first().id, passwort, "Testhandy")
    }
    pruefe("Handy angemeldet", handy.isSignedIn)
    val besitzer = handy.me().account

    // Der Medienschlüssel gegen den echten Server: mit ihm kommt man an
    // Bilder (hier gibt es keine — also 404, nicht 401), ohne ihn nicht.
    val mk = handy.medienSchluesselHolen()
    pruefe("Medienschlüssel vom Server", mk.key.isNotEmpty() && mk.gueltigSek in 21000..21600)
    fun status(adresse: String): Int {
        val c = java.net.URI(adresse).toURL().openConnection() as java.net.HttpURLConnection
        return c.responseCode.also { c.disconnect() }
    }
    val bild = handy.artworkUrl("gibtsnicht")!!
    pruefe("Bildadresse mit Schlüssel, ohne Token", bild.contains("key=") && !bild.contains(handy.token!!))
    pruefe("mit Schlüssel angemeldet", status(bild) != 401)
    gleich("falscher Schlüssel: 401", 401, status(bild.replace(Regex("key=[^&]*"), "key=falsch")))
    gleich("ohne Schlüssel: 401", 401, status(bild.substringBefore('?')))
    gleich("nach 401 derselbe Weg, neuer Schlüssel", true,
        handy.nachAbgelaufenemSchluessel(bild.replace(Regex("key=[^&]*"), "key=falsch"))?.let { status(it) != 401 })
    gleich("nichts läuft", 0, handy.meineWiedergaben().size)
    try {
        handy.wiedergabeBeenden("gibtsnicht")
        fehler.add("unbekannte Wiedergabe beendet")
        geprueft++
    } catch (e: LumenException.Server) {
        geprueft++
        gleich("unbekannte Wiedergabe: 404", 404, e.status)
    }
    // Der Besitzer gibt dem Server einen Namen — den nimmt die Serverliste.
    rohSenden("PUT", handy.baseUrl + "/api/v1/admin/server-name", handy.token!!, """{"name":"Mein NAS"}""")
    gleich("hello nennt den Namen", "Mein NAS", handy.hello().name)

    // Der Fernseher: Adresse, dann Code statt Profilwahl.
    val tvAblage = MemorySchluesselablage()
    val tvListe = Serverliste(tvAblage)
    val tv = SessionController(tvListe)
    tv.start()
    pruefe("Fernseher fragt nach dem Server", tv.stage is Stage.NeedsServer)
    // Was für ein Gerät sich anmeldet, sagt das Geräteprofil — hier ein Fernseher.
    val vorher = DeviceProfile.current
    DeviceProfile.install(vorher.copy(name = "Testfernseher", kind = "tv"))
    tv.useServer(adresse)
    pruefe("Fernseher bei der Profilwahl", tv.stage is Stage.ChoosingProfile)
    gleich("Server gemerkt", 1, tvListe.server.size)
    gleich("mit dem Namen vom Besitzer", "Mein NAS", tvListe.aktiv?.name)

    val code = tv.codeStarten("Wohnzimmer")
    pruefe("Code geholt", code != null)
    if (code == null) { DeviceProfile.install(vorher); return }
    pruefe("sechs Ziffern", code.code.length == 6 && code.code.all { it.isDigit() })
    pruefe("Geheimnis ist lang", code.geheim.length >= 24)
    gleich("fünf Minuten gültig", 300, code.gueltigSek)
    DeviceProfile.install(vorher)
    gleich("noch nicht freigegeben", CodeStand.WARTET, tv.codeNachfragen(code.geheim))
    pruefe("dabei nicht angemeldet", tv.stage !is Stage.SignedIn)

    // Ein falscher Code findet nichts — und das Handy bleibt angemeldet.
    val falsch = if (code.code == "000000") "000001" else "000000"
    try {
        handy.codeFreigeben(falsch)
        fehler.add("falscher Code wurde freigegeben"); geprueft++
    } catch (e: LumenException.Server) {
        gleich("falscher Code gibt 404", 404, e.status)
    }
    pruefe("Handy nach falschem Code noch angemeldet", handy.isSignedIn)

    // Ohne Anmeldung lässt sich nichts freigeben.
    try {
        ApiClient(handy.baseUrl).codeFreigeben(code.code)
        fehler.add("Freigeben ohne Anmeldung ging"); geprueft++
    } catch (e: LumenException) {
        geprueft++
    }

    // Mit Leerzeichen eingegeben, wie man ihn abliest.
    val geraet = handy.codeFreigeben(Anmeldecode.anzeige(code.code))
    gleich("Gerätename zurück", "Wohnzimmer", geraet)
    gleich("Meldung am Handy", "Wohnzimmer ist angemeldet", Anmeldecode.angemeldetText(geraet))

    gleich("Fernseher angemeldet", CodeStand.ANGEMELDET, tv.codeNachfragen(code.geheim))
    val drin = tv.stage
    pruefe("Fernseher in der Mediathek", drin is Stage.SignedIn)
    gleich("mit dem Profil des Handys", besitzer.id, (drin as? Stage.SignedIn)?.me?.account?.id)
    pruefe("Token gemerkt, je Server", tvListe.token != null && tvListe.angemeldet(tvListe.aktiv!!.id))
    pruefe("ein eigenes Token, nicht das des Handys", tvListe.token != handy.token)
    gleich("ein zweites Abholen geht nicht", CodeStand.ABGELAUFEN, tv.codeNachfragen(code.geheim))
    pruefe("und lässt die Anmeldung stehen", tv.stage is Stage.SignedIn)
    val geraete = handy.devices()
    pruefe("Fernseher in den eigenen Geräten", geraete.any { it.name == "Wohnzimmer" && it.kind == "tv" })

    // Ein schon freigegebener Code geht nicht ein zweites Mal.
    try {
        handy.codeFreigeben(code.code)
        fehler.add("Code liess sich zweimal freigeben"); geprueft++
    } catch (e: LumenException.Server) {
        geprueft++
    }
    gleich("unbekanntes Geheimnis ist abgelaufen", CodeStand.ABGELAUFEN, tv.codeNachfragen("gibtsnicht"))

    // Neustart: der Fernseher ist gleich wieder drin.
    val neu = SessionController(Serverliste(tvAblage))
    neu.start()
    pruefe("Token trägt über den Neustart", neu.stage is Stage.SignedIn)

    // Das Handy meldet sich selbst mit Code an — derselbe Weg.
    val zweitesHandy = SessionController(Serverliste(MemorySchluesselablage()))
    zweitesHandy.useServer(adresse)
    val c2 = zweitesHandy.codeStarten("Tablet")!!
    handy.codeFreigeben(c2.code)
    gleich("Tablet mit Code angemeldet", CodeStand.ANGEMELDET, zweitesHandy.codeNachfragen(c2.geheim))

    if (zweite == null) {
        println("  Keine zweite Adresse — der Wechsel zwischen Servern bleibt aus.")
        return
    }

    // Zweiter Server: hinzufügen, einrichten, wechseln — jedes Token für sich.
    val ersterId = tvListe.aktiv!!.id
    val ersterToken = tvListe.token
    tv.serverWahl()
    pruefe("Serverliste aus der Mediathek", tv.stage is Stage.ChoosingServer)
    tv.serverHinzufuegen()
    tv.useServer(zweite)
    gleich("zweiter Server gemerkt", 2, tvListe.server.size)
    val zweiterId = tvListe.aktiv!!.id
    pruefe("zweiter ist gewählt", zweiterId != ersterId)
    pruefe("zweiter ohne Token", tvListe.token == null)
    if (tv.stage is Stage.NeedsSetup) {
        tv.setUpOwner("Ferien", passwort, "Wohnzimmer")
    } else {
        val k = (tv.stage as Stage.ChoosingProfile).profiles.first()
        tv.unlock(k, passwort)
    }
    pruefe("am zweiten angemeldet", tv.stage is Stage.SignedIn)
    val zweiterToken = tvListe.token
    pruefe("zweites Token", zweiterToken != null && zweiterToken != ersterToken)
    gleich("erstes Token unberührt", ersterToken, tvAblage.lesen(Serverliste.tokenSchluessel(ersterId)))

    tv.verbinden(ersterId)
    val zurueck = tv.stage
    pruefe("zurück am ersten, ohne Anmeldung", zurueck is Stage.SignedIn)
    gleich("mit dem richtigen Konto", besitzer.id, (zurueck as? Stage.SignedIn)?.me?.account?.id)
    gleich("Client trägt das erste Token", ersterToken, tv.client.token)

    tv.verbinden(zweiterId)
    pruefe("und wieder am zweiten", tv.stage is Stage.SignedIn)
    gleich("Client trägt das zweite Token", zweiterToken, tv.client.token)

    // Neustart: der zuletzt benutzte kommt von selbst.
    val dritter = SessionController(Serverliste(tvAblage))
    dritter.start()
    pruefe("Neustart: zuletzt benutzter", dritter.stage is Stage.SignedIn)
    gleich("Neustart: am zweiten", zweiterId, dritter.aktiverServer?.id)

    // Abmelden am zweiten lässt den ersten angemeldet — und meldet das
    // Gerät beim Server ab: das alte Token gilt dort nicht mehr.
    tv.signOut()
    pruefe("am zweiten abgemeldet", tvListe.token == null)
    wirftFehler("Token nach dem Abmelden beim Server ungültig") {
        ApiClient(ApiClient.normalizeAddress(zweite)!!, zweiterToken).me()
    }
    pruefe("am ersten noch angemeldet", tvListe.angemeldet(ersterId))
    tv.serverEntfernen(zweiterId)
    gleich("zweiter entfernt", 1, tvListe.server.size)
    pruefe("Liste bleibt sichtbar", tv.stage is Stage.ChoosingServer)
    tv.verbinden(ersterId)
    pruefe("erster weiterhin ohne Anmeldung", tv.stage is Stage.SignedIn)

    // Entfernen meldet das Gerät erst beim Server ab, dann ist es vergessen.
    tv.serverEntfernen(ersterId)
    pruefe("letzter entfernt: Adresseingabe", tv.stage is Stage.NeedsServer)
    wirftFehler("Token nach dem Entfernen beim Server ungültig") {
        ApiClient(ApiClient.normalizeAddress(adresse)!!, ersterToken).me()
    }
    pruefe("Handy bleibt angemeldet", runCatching { handy.me() }.isSuccess)
}

/** Eine Anfrage ohne den Client — für das, was die App selbst nie schickt. */
private fun rohSenden(methode: String, adresse: String, token: String, body: String): Int {
    val c = java.net.URI(adresse).toURL().openConnection() as java.net.HttpURLConnection
    c.requestMethod = methode
    c.setRequestProperty("Authorization", "Bearer $token")
    c.setRequestProperty("Content-Type", "application/json")
    c.doOutput = true
    c.outputStream.use { it.write(body.toByteArray()) }
    return c.responseCode.also { c.disconnect() }
}

// ---------------------------------------------------------------- Medienschlüssel

/** Eine JSON-Antwort für die kleinen Testserver. */
private fun fakeAntwort(ex: com.sun.net.httpserver.HttpExchange, code: Int, body: String) {
    val roh = body.toByteArray()
    ex.responseHeaders.add("Content-Type", "application/json")
    ex.sendResponseHeaders(code, if (roh.isEmpty()) -1L else roh.size.toLong())
    ex.responseBody.use { if (roh.isNotEmpty()) it.write(roh) }
}

private const val ME_JSON = """{"account":{"id":"acc_1","name":"Alex","isOwner":false}}"""

/**
 * Der Medienschlüssel statt des Tokens in den Adressen — gegen einen
 * kleinen Testserver, der Schlüssel durchnummeriert ausgibt.
 */
private fun medienschluesselTests() {
    val server = com.sun.net.httpserver.HttpServer.create(
        java.net.InetSocketAddress("127.0.0.1", 0), 0)
    var ausgegeben = 0
    val koepfe = mutableListOf<String?>()
    server.createContext("/api/v1/") { ex ->
        val pfad = ex.requestURI.path
        val kopf = ex.requestHeaders.getFirst("Authorization")
        when (pfad) {
            "/api/v1/me/media-key" -> {
                koepfe.add(kopf)
                if (kopf != "Bearer TOKEN") {
                    fakeAntwort(ex, 401, """{"error":"anmeldung abgelaufen"}""")
                } else {
                    val n = synchronized(koepfe) { ++ausgegeben }
                    fakeAntwort(ex, 200,
                        """{"key":"K$n","gueltigBis":"2026-11-03T20:00:00Z","gueltigSek":21600}""")
                }
            }
            "/api/v1/stream/f1/plan" -> fakeAntwort(ex, 200, """{"plan":{"direct":false,"videoCodec":"h264"}}""")
            else -> fakeAntwort(ex, 404, """{"error":"nicht gefunden"}""")
        }
    }
    server.start()
    try {
        val adresse = "http://127.0.0.1:${server.address.port}"
        val client = ApiClient(adresse, "TOKEN")
        client.imHintergrundErneuern = false
        var jetzt = 1_000_000_000L
        client.uhr = { jetzt }

        gleich("Modell: Schlüssel", "abc", Medienschluessel.from(Json.parse(
            """{"key":"abc","gueltigBis":"2026-11-03T20:00:00Z","gueltigSek":21600}""")).key)
        gleich("Modell: Dauer", 21600L, Medienschluessel.from(Json.parse(
            """{"key":"abc","gueltigSek":21600}""")).gueltigSek)

        // Ohne Schlüssel: die Adresse ohne — nie mit dem Token.
        gleich("ohne Schlüssel ohne alles", "$adresse/api/v1/art/it_1/poster", client.artworkUrl("it_1"))

        pruefe("Schlüssel geholt", client.medienSchluesselSichern())
        gleich("erster Schlüssel", "K1", client.medienSchluessel)
        gleich("nur mit dem Token im Kopf", "Bearer TOKEN", koepfe.last())
        gleich("Bild mit Schlüssel", "$adresse/api/v1/art/it_1/poster?key=K1", client.artworkUrl("it_1"))
        pruefe("kein Token in der Adresse", client.artworkUrl("it_1")?.contains("TOKEN") == false)

        // Noch lange gültig: nicht noch einmal fragen.
        client.medienSchluesselSichern()
        gleich("kein zweites Mal geholt", 1, ausgegeben)

        // Weniger als eine Stunde übrig: neu holen.
        jetzt += 5 * 60 * 60 * 1000L + 1
        client.medienSchluesselSichern()
        gleich("nach fünf Stunden ein neuer", "K2", client.medienSchluessel)

        // 401 auf ein Bild (Server neu gestartet): einmal neu holen, neu bauen.
        val alt = client.artworkUrl("it_1")!!
        gleich("nach 401 neue Adresse", "$adresse/api/v1/art/it_1/poster?key=K3",
            client.nachAbgelaufenemSchluessel(alt))
        // Ein zweites Bild mit dem alten Schlüssel holt keinen weiteren.
        gleich("gleichzeitige 401: kein weiterer Schlüssel", "$adresse/api/v1/stream/f1/subtitles/2.vtt?key=K3",
            client.nachAbgelaufenemSchluessel("$adresse/api/v1/stream/f1/subtitles/2.vtt?key=K2"))
        gleich("dabei nur einmal geholt", 3, ausgegeben)
        gleich("Adresse ohne Schlüssel bekommt einen", "$adresse/api/v1/avatar/acc_1?key=K3&v=5",
            client.nachAbgelaufenemSchluessel("$adresse/api/v1/avatar/acc_1?v=5"))
        gleich("fremde Adresse: nichts", null,
            client.nachAbgelaufenemSchluessel("http://anderswo:8097/api/v1/art/it_1/poster?key=K3"))
        gleich("keine Medienadresse: nichts", null,
            client.nachAbgelaufenemSchluessel("$adresse/api/v1/items?key=K3"))

        // Vor der Wiedergabe: ein frischer Schlüssel nur für den Film; die
        // Bilder behalten ihren (sonst lüde der Bildlader alles neu).
        val plan = client.wiedergabeVorbereiten("f1")
        pruefe("Plan gelesen", !plan.direct)
        val strom = client.streamUrl(MediaFile.from(Json.parse("""{"id":"f1","container":"mkv"}""")), plan)
        pruefe("Strom mit frischem Schlüssel", strom.contains("key=K4") && strom.contains("/hls/master.m3u8"))
        pruefe("Strom ohne Token", !strom.contains("token=") && !strom.contains("TOKEN"))
        pruefe("Untertitel mit dem Schlüssel des Films", client.subtitleUrl("f1", 3).endsWith("key=K4"))
        pruefe("Bilder behalten ihren Schlüssel", client.artworkUrl("it_1")!!.endsWith("key=K3"))
        client.wiedergabeVorbereiten("f1")
        pruefe("jeder Film bekommt einen neuen", client.subtitleUrl("f1", 3).endsWith("key=K5"))

        // Anderer Server: der Schlüssel des alten gilt dort nicht.
        val zweiter = ApiClient(adresse, "TOKEN").also { it.imHintergrundErneuern = false }
        zweiter.medienSchluesselSetzen("X")
        zweiter.baseUrl = "http://anderswo:8097"
        gleich("neuer Server, kein Schlüssel", null, zweiter.medienSchluessel)

        // Neues Token: der alte Schlüssel ist weg. Gilt das Token nicht,
        // gibt es auch keinen neuen.
        client.token = "ANDERS"
        gleich("anderes Token, kein Schlüssel", null, client.medienSchluessel)
        pruefe("abgelehntes Token: kein Schlüssel", !client.medienSchluesselSichern())
        pruefe("und abgemeldet", !client.isSignedIn)
        gleich("abgemeldet: keine Bildadresse", null, client.artworkUrl("it_1"))

        // Von selbst: Der Adressbauer stösst das Holen im Hintergrund an.
        val vonSelbst = ApiClient(adresse, "TOKEN")
        vonSelbst.artworkUrl("it_1")
        val bis = System.currentTimeMillis() + 3000
        while (vonSelbst.medienSchluessel == null && System.currentTimeMillis() < bis) Thread.sleep(20)
        pruefe("im Hintergrund geholt", vonSelbst.medienSchluessel != null)
    } finally {
        server.stop(0)
    }
}

// ---------------------------------------------------------------- Von unterwegs

/** Keine Profilauswahl von aussen: Anmelden mit Name und PIN. */
private fun unterwegsTests() {
    val server = com.sun.net.httpserver.HttpServer.create(
        java.net.InetSocketAddress("127.0.0.1", 0), 0)
    val koerper = mutableListOf<String>()
    server.createContext("/api/v1/") { ex ->
        val pfad = ex.requestURI.path
        when (pfad) {
            "/api/v1/hello" -> fakeAntwort(ex, 200, """{"server":"Lumen","version":"t","setUp":true}""")
            "/api/v1/profiles" -> fakeAntwort(ex, 403,
                """{"error":"von unterwegs gibt es keine profilauswahl","code":"unterwegs"}""")
            "/api/v1/unlock" -> {
                val text = ex.requestBody.readBytes().toString(Charsets.UTF_8)
                koerper.add(text)
                val j = Json.parse(text)
                when {
                    j["name"].text == "Zu" ->
                        fakeAntwort(ex, 429, """{"error":"Zu viele Versuche. Versuch es in 5 Minuten wieder."}""")
                    j["name"].text == "Alex" && j["secret"].text == "1234" ->
                        fakeAntwort(ex, 200, """{"token":"T","account":{"id":"acc_1","name":"Alex"}}""")
                    else -> fakeAntwort(ex, 401, """{"error":"das stimmt nicht"}""")
                }
            }
            "/api/v1/me" -> fakeAntwort(ex, 200, ME_JSON)
            "/api/v1/me/media-key" -> fakeAntwort(ex, 200, """{"key":"MK","gueltigSek":21600}""")
            "/api/v1/news" -> fakeAntwort(ex, 403, """{"error":"verboten"}""")
            else -> fakeAntwort(ex, 404, """{"error":"nicht gefunden"}""")
        }
    }
    server.start()
    try {
        val adresse = "http://127.0.0.1:${server.address.port}"
        try {
            ApiClient(adresse).profiles()
            fehler.add("Profile von unterwegs kamen durch")
            geprueft++
        } catch (e: LumenException.Unterwegs) {
            geprueft++
            gleich("unterwegs ist eine 403", 403, e.status)
        }

        val store = MemoryTokenStore(serverUrl = adresse)
        val s = SessionController(store)
        s.start()
        val stand = s.stage
        pruefe("von unterwegs: Anmelden mit Name", stand is Stage.ChoosingProfile && stand.unterwegs &&
            stand.profiles.isEmpty())

        s.unlockByName("Zu", "1")
        gleich("429: die Meldung des Servers", "Zu viele Versuche. Versuch es in 5 Minuten wieder.", s.lastError)
        s.unlockByName("Alex", "falsch")
        gleich("falscher PIN", "das stimmt nicht", s.lastError)
        pruefe("noch nicht angemeldet", s.stage is Stage.ChoosingProfile)
        s.unlockByName("Alex", "")
        pruefe("ohne PIN gar nicht erst gefragt", s.lastError != null && koerper.size == 2)

        s.unlockByName(" Alex ", "1234", "Testhandy")
        pruefe("angemeldet", s.stage is Stage.SignedIn)
        gleich("Token gemerkt", "T", store.token)
        gleich("Schlüssel vor dem ersten Bildschirm", "MK", s.client.medienSchluessel)
        val j = Json.parse(koerper.last())
        gleich("mit Namen, getrimmt", "Alex", j["name"].text)
        gleich("ohne Kennung", null, j["profileId"].text)
        gleich("mit Gerätenamen", "Testhandy", j["deviceName"].text)
        pruefe("mit Geräteart", j["deviceKind"].text != null)

        // Eine gewöhnliche 403 bleibt eine gewöhnliche.
        wirftFehler("andere 403 ist kein unterwegs") {
            try { ApiClient(adresse, "T").news() } catch (e: LumenException.Unterwegs) { }
            throw IllegalStateException("erwartet")
        }
    } finally {
        server.stop(0)
    }
}

// ---------------------------------------------------------------- Eigene Wiedergaben

private fun wiedergabenTests() {
    val liste = Json.parse("""[
        {"id":"w1","deviceName":"Wohnzimmer","deviceKind":"tv","mode":"transcode","detail":"4K hevc → 1080p h264"},
        {"id":"w2","deviceName":"","deviceKind":"phone","mode":"direct","detail":"1080p h264"},
        {"id":"w3","deviceName":"Mac","deviceKind":"mac","mode":"direct","detail":""}
    ]""").list.map { EigeneWiedergabe.from(it) }
    gleich("drei Wiedergaben", 3, liste.size)
    gleich("Kennung", "w1", liste[0].id)
    gleich("Gerät", "Wohnzimmer", liste[0].geraet)
    pruefe("umgewandelt", liste[0].wirdUmgewandelt && !liste[1].wirdUmgewandelt)
    gleich("Zeile umgewandelt", "wird umgewandelt · 4K hevc → 1080p h264", liste[0].zeile)
    gleich("Zeile direkt", "direkt · 1080p h264", liste[1].zeile)
    gleich("ohne Namen die Art", "Handy", liste[1].geraet)
    gleich("ohne Detail nur die Art", "direkt", liste[2].zeile)
    wirftFehler("ohne Kennung unbrauchbar") { EigeneWiedergabe.from(Json.parse("""{"mode":"direct"}""")) }

    // Gegen einen Testserver: holen und beenden.
    val server = com.sun.net.httpserver.HttpServer.create(
        java.net.InetSocketAddress("127.0.0.1", 0), 0)
    val laufend = mutableListOf("w1", "w2")
    server.createContext("/api/v1/me/playbacks") { ex ->
        val pfad = ex.requestURI.path
        when {
            ex.requestMethod == "GET" && pfad == "/api/v1/me/playbacks" -> fakeAntwort(ex, 200,
                laufend.joinToString(",", "[", "]") { """{"id":"$it","deviceName":"G $it","deviceKind":"tv","mode":"direct","detail":"720p h264"}""" })
            ex.requestMethod == "DELETE" -> {
                val id = pfad.substringAfterLast('/')
                if (laufend.remove(id)) fakeAntwort(ex, 204, "")
                else fakeAntwort(ex, 404, """{"error":"diese wiedergabe läuft nicht mehr"}""")
            }
            else -> fakeAntwort(ex, 404, """{"error":"nicht gefunden"}""")
        }
    }
    server.start()
    try {
        val c = ApiClient("http://127.0.0.1:${server.address.port}", "TOKEN")
        gleich("zwei laufen", listOf("w1", "w2"), c.meineWiedergaben().map { it.id })
        c.wiedergabeBeenden("w1")
        gleich("eine beendet", listOf("w2"), c.meineWiedergaben().map { it.id })
        try {
            c.wiedergabeBeenden("w1")
            fehler.add("zweites Beenden ohne Fehler")
            geprueft++
        } catch (e: LumenException.Server) {
            geprueft++
            gleich("schon beendet: Meldung des Servers", "diese wiedergabe läuft nicht mehr", e.message)
        }
    } finally {
        server.stop(0)
    }
}

// ---------------------------------------------------------------- Jahr, Person, Stöbern

/** Bei Serien der Zeitraum, sonst das Jahr. */
private fun jahresTests() {
    fun serie(rest: String) = titel("""{"id":"s","title":"S","kind":"series","year":2014$rest}""")
    gleich("läuft noch", "seit 2014", serie(""","laeuftNoch":true""").jahresText)
    gleich("abgeschlossen", "2014–2020", serie(""","laeuftNoch":false,"endYear":2020""").jahresText)
    gleich("abgeschlossen im selben Jahr", "2014", serie(""","laeuftNoch":false,"endYear":2014""").jahresText)
    gleich("abgeschlossen ohne Ende", "2014", serie(""","laeuftNoch":false""").jahresText)
    gleich("Serie ohne Angabe", "2014", serie(""","endYear":2020""").jahresText)
    gleich("Film bleibt ein Jahr", "1999",
        titel("""{"id":"m","title":"M","kind":"movie","year":1999,"laeuftNoch":false,"endYear":2005}""").jahresText)
    gleich("ohne Jahr nichts", null, titel("""{"id":"m","title":"M"}""").jahresText)
    gleich("Zeile unter dem Titel", "seit 2014",
        serie(""","laeuftNoch":true""").subtitleLine)
    pruefe("Titelseite nimmt den Zeitraum",
        Titelseite.metaZeile(serie(""","laeuftNoch":false,"endYear":2020""")).startsWith("2014–2020"))

    // Das Herz
    pruefe("Favorit gelesen", titel("""{"id":"a","title":"A","favorite":true}""").favorite)
    pruefe("ohne Angabe kein Favorit", !titel("""{"id":"a","title":"A"}""").favorite)
}

/** Die Personenseite. */
private fun personTests() {
    val p = PersonInfo.from(Json.parse("""{"name":"Jude Law","bio":"Ein Schauspieler.","bioSprache":"en",
        "geboren":"1972-12-29","geburtsort":"London","gestorben":"",
        "quellen":[{"name":"TMDb","url":"https://www.themoviedb.org/person/9642"},
                   {"name":"Wikipedia","url":"javascript:alert(1)"}],
        "bild":"/api/v1/persons/Jude%20Law/bild"}"""))
    gleich("Name", "Jude Law", p.name)
    gleich("Lebensdaten", "geboren 29.12.1972 in London", p.lebensZeile)
    gleich("Quellen", "Quelle: TMDb, Wikipedia", p.quellenZeile)
    pruefe("nur Englisch", p.nurEnglisch)
    gleich("https taugt als Link", "https://www.themoviedb.org/person/9642", p.quellen[0].sichereUrl)
    gleich("javascript: nicht", null, p.quellen[1].sichereUrl)
    gleich("intent: nicht", null, Quelle("x", "intent://x#Intent;end").sichereUrl)
    gleich("http ohne Host nicht", null, Quelle("x", "http:///x").sichereUrl)
    val tot = PersonInfo.from(Json.parse("""{"name":"X","bio":"Text","bioSprache":"de",
        "geboren":"1920-01-02","gestorben":"2001-03-04"}"""))
    gleich("mit Tod, ohne Ort", "geboren 2.1.1920 · gestorben 4.3.2001", tot.lebensZeile)
    pruefe("deutsch: kein Hinweis", !tot.nurEnglisch)
    val leer = PersonInfo.from(Json.parse("""{"name":"Y"}"""))
    gleich("ohne Daten keine Zeile", null, leer.lebensZeile)
    gleich("ohne Quellen keine Zeile", null, leer.quellenZeile)
    pruefe("ohne Bio kein Hinweis", !leer.nurEnglisch)
}

/** Herz und Stöbern gegen einen kleinen Testserver: was die App fragt. */
private fun stoebernTests() {
    gleich("Sortierungen wie im Browser",
        listOf("title", "added", "premiered", "rating", "runtime", "lastwatched", "unwatched", "random"),
        Sortierung.entries.map { it.schluessel })
    pruefe("Laufzeit nur bei Filmen", Sortierung.LAUFZEIT in Sortierung.faelle("movie") &&
        Sortierung.LAUFZEIT !in Sortierung.faelle("series"))
    gleich("Aufschrift", "Ungesehene zuerst", Sortierung.UNGESEHEN.titel("series"))
    pruefe("Leiste nur nach Titel", Sortierung.TITEL.mitLeiste && !Sortierung.HINZUGEFUEGT.mitLeiste)
    gleich("gemerkt gelesen", Sortierung.HINZUGEFUEGT to false, Sortierung.aus("added:asc", "movie"))
    gleich("ohne Richtung die übliche", Sortierung.BEWERTUNG to true, Sortierung.aus("rating", "movie"))
    gleich("Laufzeit gibt es bei Serien nicht", null, Sortierung.aus("runtime:asc", "series"))
    gleich("alter Wert unbekannt", null, Sortierung.aus("year-asc", "movie"))
    gleich("zum Merken", "title:desc", Sortierung.TITEL.wert(true))
    gleich("Profilschlüssel", "sort.series", Sortierung.prefSchluessel("series"))
    gleich("absteigend von Z", "Z", Buchstaben.reihe(true).first())
    gleich("absteigend die Raute zuletzt", Buchstaben.RAUTE, Buchstaben.reihe(true).last())
    gleich("27 Buchstaben", 27, Buchstaben.ALLE.size)
    gleich("zuerst die Raute", Buchstaben.RAUTE, Buchstaben.ALLE.first())
    val b = Buchstaben.from(Json.parse("""{"A":12,"#":3,"Q":"x"}"""))
    gleich("A gezählt", 12, b.anzahl("A"))
    pruefe("Raute wählbar", b.waehlbar(Buchstaben.RAUTE))
    pruefe("Z nicht wählbar", !b.waehlbar("Z"))
    pruefe("Unlesbares fällt weg", !b.waehlbar("Q"))

    val server = com.sun.net.httpserver.HttpServer.create(
        java.net.InetSocketAddress("127.0.0.1", 0), 0)
    val anfragen = mutableListOf<String>()
    val favoriten = mutableListOf<String>()
    server.createContext("/api/v1/") { ex ->
        val pfad = ex.requestURI.rawPath
        val frage = ex.requestURI.rawQuery ?: ""
        synchronized(anfragen) { anfragen.add("${ex.requestMethod} $pfad?$frage") }
        when {
            pfad.startsWith("/api/v1/favorites/") -> {
                val id = pfad.substringAfterLast('/')
                if (ex.requestMethod == "PUT") favoriten.add(id) else favoriten.remove(id)
                fakeAntwort(ex, 204, "")
            }
            pfad == "/api/v1/items/letters" -> fakeAntwort(ex, 200, """{"A":2,"#":1}""")
            pfad == "/api/v1/items" && "offset=0" in frage && "liste=" in frage ->
                fakeAntwort(ex, 200, """{"items":[{"id":"a","title":"A","favorite":true}],
                    "gesamt":2,"offset":0,"weiter":true,"genres":[]}""")
            pfad == "/api/v1/items" && "liste=" in frage ->
                fakeAntwort(ex, 200, """{"items":[{"id":"b","title":"B","favorite":true}],
                    "gesamt":2,"offset":1,"weiter":false,"genres":[]}""")
            pfad == "/api/v1/items" -> fakeAntwort(ex, 200,
                """{"items":[],"gesamt":0,"offset":0,"weiter":false,"genres":[]}""")
            pfad == "/api/v1/persons/Tom%20Hanks" -> fakeAntwort(ex, 200,
                """{"name":"Tom Hanks","bio":"B","bioSprache":"de","bild":"/api/v1/persons/Tom%20Hanks/bild"}""")
            else -> fakeAntwort(ex, 404, """{"error":"nicht gefunden"}""")
        }
    }
    server.start()
    try {
        val adresse = "http://127.0.0.1:${server.address.port}"
        val c = ApiClient(adresse, "TOKEN")
        c.imHintergrundErneuern = false
        c.medienSchluesselSetzen("K1")

        c.addFavorite("it_1")
        gleich("Herz gesetzt", listOf("it_1"), favoriten.toList())
        c.removeFavorite("it_1")
        gleich("Herz weg", emptyList<String>(), favoriten.toList())
        pruefe("PUT und DELETE", anfragen.any { it.startsWith("PUT /api/v1/favorites/it_1") } &&
            anfragen.any { it.startsWith("DELETE /api/v1/favorites/it_1") })

        val fav = c.listeGanz("favoriten", "movie")
        gleich("Favoriten über zwei Seiten", listOf("a", "b"), fav.items.map { it.id })
        pruefe("Favoriten: Liste, Art, Grösse", anfragen.any {
            "liste=favoriten" in it && "kind=movie" in it && "limit=200" in it && "seite=1" in it })
        pruefe("Liste ohne eigene Sortierung", anfragen.filter { "liste=" in it }.none { "sort=" in it })
        c.liste("merkliste")
        pruefe("Merkliste vom Server", anfragen.any { "liste=merkliste" in it && "kind=" !in it })

        c.seite("movie", sort = "title", buchstabe = Buchstaben.RAUTE)
        pruefe("Raute kodiert", anfragen.any { "buchstabe=%23" in it && "sort=title" in it })
        c.seite("series", sort = "premiered", dir = "asc")
        pruefe("Älteste zuerst", anfragen.any { "sort=premiered" in it && "dir=asc" in it && "kind=series" in it && "buchstabe" !in it })
        c.seite("movie", sort = "random", offset = 60, seed = 4711L)
        pruefe("Zufall mit Samen", anfragen.any { "sort=random" in it && "seed=4711" in it })

        val bs = c.buchstaben("movie", "Drama")
        gleich("Leiste gezählt", 2, bs.anzahl("A"))
        pruefe("Leiste mit Art und Genre", anfragen.any {
            it.startsWith("GET /api/v1/items/letters?") && "kind=movie" in it && "genre=Drama" in it })

        val p = c.person("Tom Hanks")
        gleich("Person mit Leerzeichen im Pfad", "Tom Hanks", p?.name)
        gleich("Foto mit Schlüssel", "$adresse/api/v1/persons/Tom%20Hanks/bild?key=K1",
            p?.let { c.personBildUrl(it) })
        pruefe("Foto ohne Token", p?.let { c.personBildUrl(it) }?.contains("TOKEN") == false)
        gleich("unbekannte Person: null", null, c.person("Niemand"))
    } finally {
        server.stop(0)
    }
}

// ---------------------------------------------------------------- Neue Nutzerfunktionen

/** Trickplay: Einheit des Abstands, die Kachel für eine Stelle, Ränder. */
private fun trickplayTests() {
    val t = Trickplay.from(Json.parse("""
        {"intervall":10000,"breite":320,"hoehe":180,"spalten":10,"zeilen":10,"bilder":250,
         "blaetter":["/api/v1/stream/f1/trickplay/0.jpg","/api/v1/stream/f1/trickplay/1.jpg",
                     "/api/v1/stream/f1/trickplay/2.jpg"]}
    """))
    pruefe("Trickplay brauchbar", t.brauchbar)
    gleich("Millisekunden erkannt", 10.0, t.intervallSekunden)
    gleich("Sekunden bleiben Sekunden", 5.0, t.copy(intervall = 5.0).intervallSekunden)
    gleich("Anfang", Trickplay.Kachel(0, 0, 0, 320, 180), t.kachel(0.0))
    // 125 s → Bild 12 → Blatt 0, Spalte 2, Zeile 1
    gleich("mitten im ersten Blatt", Trickplay.Kachel(0, 640, 180, 320, 180), t.kachel(125.0))
    // 1005 s → Bild 100 → Blatt 1, erste Kachel
    gleich("zweites Blatt", Trickplay.Kachel(1, 0, 0, 320, 180), t.kachel(1005.0))
    gleich("hinter dem Ende das letzte Bild", 2, t.kachel(99_999.0)?.blatt)
    gleich("vor dem Anfang das erste", 0, t.kachel(-5.0)?.blatt)
    pruefe("NaN gibt nichts", t.kachel(Double.NaN) == null)
    pruefe("ohne Blätter unbrauchbar", !t.copy(blaetter = emptyList()).brauchbar)
    pruefe("leere Antwort unbrauchbar", !Trickplay.from(Json.parse("{}")).brauchbar)
}

/** Herunterladen: Grössen, Namen, Meldungen und die gespeicherten Angaben. */
private fun herunterladenTests() {
    gleich("Gesamt aus Content-Range", 1000L, Herunterladen.gesamtGroesse("bytes 100-999/1000", 900, 100))
    gleich("Gesamt ohne Range", 1100L, Herunterladen.gesamtGroesse(null, 1000, 100))
    gleich("Gesamt unbekannt", 0L, Herunterladen.gesamtGroesse("bytes 0-9/*", -1, 0))
    gleich("MB", "850 MB", Herunterladen.groesse(850_000_000))
    gleich("GB mit Komma", "4,2 GB", Herunterladen.groesse(4_200_000_000))
    gleich("sicherer Name", "Der_Film_2020_.mkv", Herunterladen.sichererName("Der/Film:2020?.mkv"))
    pruefe("kein Punkt vorne", !Herunterladen.sichererName("..geheim").startsWith("."))

    val info = DownloadInfo.from(Json.parse("""
        {"erlaubt":true,"dateien":[{"fileId":"f1","dateiname":"Film.mkv","groesse":4200000000,"hoehe":1080},
                                   {"dateiname":"ohne Kennung"}]}
    """))
    pruefe("erlaubt", info.erlaubt)
    gleich("Datei ohne Kennung fällt weg", 1, info.dateien.size)
    gleich("Beschriftung", "1080p · 4,2 GB", info.dateien[0].beschriftung)
    pruefe("ohne Angabe nicht erlaubt", !DownloadInfo.from(Json.parse("{}")).erlaubt)

    gleich("kein_download", "Für dieses Profil sind Downloads nicht freigegeben.",
        Herunterladen.meldung(LumenException.Server("x", 403, "kein_download")))
    gleich("zu_viele_downloads", "Es laufen gerade zu viele Downloads. Versuch es später noch einmal.",
        Herunterladen.meldung(LumenException.Server("x", 429, "zu_viele_downloads")))

    val d = Heruntergeladen("it1", "f1", "Film", "2020", "Film.mkv", 1000, Heruntergeladen.FERTIG,
        geladen = 1000, stelle = 61.5, dauer = 5400.0, stelleOffen = true, kontoId = "a", serverId = "s")
    gleich("hin und zurück", d, Heruntergeladen.aus(d.alsJson()))
    gleich("Prozent", 50, d.copy(geladen = 500).prozent)
    pruefe("Unsinn gibt null", Heruntergeladen.aus("{kaputt") == null)
}

/** Profil schützen: was vor dem Server schon auffällt, und seine Codes. */
private fun profilschutzTests() {
    pruefe("bisheriger PIN fehlt", Profilschutz.pruefen("pin", "", "pin", "1234", "1234") != null)
    pruefe("offenes Profil braucht keinen alten", Profilschutz.pruefen("none", "", "pin", "1234", "1234") == null)
    pruefe("PIN zu kurz", Profilschutz.pruefen("none", "", "pin", "12", "12") != null)
    pruefe("PIN nur Ziffern", Profilschutz.pruefen("none", "", "pin", "12ab", "12ab") != null)
    pruefe("Passwort zu kurz", Profilschutz.pruefen("none", "", "password", "kurz", "kurz") != null)
    pruefe("Wiederholung stimmt nicht", Profilschutz.pruefen("none", "", "password", "langesgeheim", "anderes") != null)
    pruefe("ohne Schutz keine Wiederholung", Profilschutz.pruefen("pin", "1234", "none", "", "") == null)
    gleich("falsch", "Der bisherige PIN oder das bisherige Passwort stimmt nicht.",
        Profilschutz.meldung(LumenException.Server("x", 403, "falsch")))
    pruefe("Vollzugriff braucht Schutz",
        Profilschutz.meldung(LumenException.Server("x", 409, "verwaltung_braucht_sperre")).contains("Vollzugriff"))
    pruefe("404 ohne Code: Server kennt es nicht", Profilschutz.fehltBeimServer(LumenException.Server("x", 404)))
}

/**
 * Die neuen Schnittstellen gegen einen kleinen Testserver: PIN ändern,
 * Gesehen mit Staffel, Trickplay mit 200/202/404 und ein Download, der
 * nach einem Abbruch fortgesetzt wird.
 */
private fun neueSchnittstellenTests() {
    val server = com.sun.net.httpserver.HttpServer.create(java.net.InetSocketAddress("127.0.0.1", 0), 0)
    val gesehen = mutableListOf<String>()
    val inhalt = ByteArray(300_000) { (it % 251).toByte() }
    server.createContext("/api/v1/") { ex ->
        val pfad = ex.requestURI.path
        val anfrage = ex.requestURI.rawQuery
        gesehen.add("${ex.requestMethod} $pfad${anfrage?.let { "?$it" } ?: ""}")
        when {
            pfad == "/api/v1/me/media-key" -> fakeAntwort(ex, 200, """{"key":"MK","gueltigSek":21600}""")
            pfad == "/api/v1/me/sperre" -> {
                val j = Json.parse(ex.requestBody.readBytes().toString(Charsets.UTF_8))
                when {
                    j["alt"].text != "1234" -> fakeAntwort(ex, 403, """{"error":"stimmt nicht","code":"falsch"}""")
                    j["art"].text == "none" -> fakeAntwort(ex, 409, """{"error":"nein","code":"verwaltung_braucht_sperre"}""")
                    else -> fakeAntwort(ex, 204, "")
                }
            }
            pfad.endsWith("/finished") -> fakeAntwort(ex, 204, "")
            pfad == "/api/v1/stream/fertig/trickplay" -> fakeAntwort(ex, 200,
                """{"intervall":10,"breite":160,"hoehe":90,"spalten":5,"zeilen":5,"bilder":20,"blaetter":["/api/v1/stream/fertig/trickplay/0.jpg"]}""")
            pfad == "/api/v1/stream/wird/trickplay" -> fakeAntwort(ex, 202, "")
            pfad == "/api/v1/stream/f1/download" -> {
                pruefe("Download mit Schlüssel", anfrage?.contains("key=MK") == true)
                val range = ex.requestHeaders.getFirst("Range")
                val ab = range?.removePrefix("bytes=")?.removeSuffix("-")?.toIntOrNull() ?: 0
                if (ab > 0) ex.responseHeaders.add("Content-Range", "bytes $ab-${inhalt.size - 1}/${inhalt.size}")
                ex.sendResponseHeaders(if (ab > 0) 206 else 200, (inhalt.size - ab).toLong())
                ex.responseBody.use { it.write(inhalt, ab, inhalt.size - ab) }
            }
            pfad == "/api/v1/stream/gesperrt/download" ->
                fakeAntwort(ex, 403, """{"error":"nein","code":"kein_download"}""")
            else -> fakeAntwort(ex, 404, """{"error":"nicht gefunden"}""")
        }
    }
    server.start()
    try {
        val client = ApiClient("http://127.0.0.1:${server.address.port}", "TOKEN")

        client.sperreAendern("1234", "pin", "5678")
        pruefe("PIN geändert", gesehen.last() == "PUT /api/v1/me/sperre")
        try {
            client.sperreAendern("0000", "pin", "5678")
            fehler.add("falscher alter PIN angenommen"); geprueft++
        } catch (e: LumenException.Server) {
            geprueft++
            gleich("Code falsch", "falsch", e.code)
        }
        try {
            client.sperreAendern("1234", "none", "")
            fehler.add("Schutz entfernt trotz Vollzugriff"); geprueft++
        } catch (e: LumenException.Server) {
            geprueft++
            gleich("409", 409, e.status)
            gleich("Code verwaltung_braucht_sperre", "verwaltung_braucht_sperre", e.code)
        }

        client.markFinished("serie1", staffel = 2)
        gleich("Staffel in der Abfrage", "POST /api/v1/items/serie1/finished?staffel=2", gesehen.last())
        client.clearFinished("folge1")
        gleich("ohne Staffel ohne Abfrage", "DELETE /api/v1/items/folge1/finished", gesehen.last())

        val fertig = client.trickplay("fertig")
        pruefe("Trickplay bereit", fertig is TrickplayStand.Bereit)
        gleich("Trickplay 202", TrickplayStand.WirdErstellt, client.trickplay("wird"))
        gleich("Trickplay 404", TrickplayStand.Keins, client.trickplay("fehlt"))
        val blatt = client.trickplayBlattUrl("/api/v1/stream/fertig/trickplay/0.jpg")
        pruefe("Blatt mit Schlüssel", blatt?.endsWith("/api/v1/stream/fertig/trickplay/0.jpg?key=MK") == true)

        // Download: erst nach 100 000 Bytes abbrechen, dann fortsetzen.
        val ziel = java.io.File.createTempFile("lumen-test", ".bin")
        ziel.delete()
        var gelesen = 0L
        client.herunterladen("f1", ziel, fortschritt = { g, _ -> gelesen = g }, weiter = { ziel.length() < 100_000 })
        pruefe("abgebrochen, aber angefangen", ziel.length() in 1 until inhalt.size)
        var gesamt = 0L
        client.herunterladen("f1", ziel, fortschritt = { g, t -> gelesen = g; gesamt = t })
        gleich("fortgesetzt bis zum Ende", inhalt.size.toLong(), ziel.length())
        gleich("Gesamtgrösse aus Content-Range", inhalt.size.toLong(), gesamt)
        pruefe("Inhalt stimmt", ziel.readBytes().contentEquals(inhalt))
        ziel.delete()

        try {
            client.herunterladen("gesperrt", java.io.File.createTempFile("lumen-test", ".bin"))
            fehler.add("gesperrter Download kam durch"); geprueft++
        } catch (e: LumenException.Server) {
            geprueft++
            gleich("kein_download", "kein_download", e.code)
        }
    } finally {
        server.stop(0)
    }
}

/** Chromecast: welche Adresse er bekommt — oder warum keine. */
private fun castTests() {
    fun im(e: CastAdresse.Ergebnis) = (e as? CastAdresse.Ergebnis.Im)?.basis
    gleich("IP bleibt", "http://192.168.1.20:8097",
        im(CastAdresse.fuer("http://192.168.1.20:8097", false) { null }))
    gleich("Name wird zur IP", "http://192.168.1.20:8097",
        im(CastAdresse.fuer("http://nas.local:8097", false) { "192.168.1.20" }))
    pruefe("Name ohne Auflösung geht nicht",
        CastAdresse.fuer("http://nas.local:8097", false) { null } is CastAdresse.Ergebnis.Nicht)
    pruefe("öffentliche Adresse geht nicht",
        CastAdresse.fuer("http://8.8.8.8:8097", false) { null } is CastAdresse.Ergebnis.Nicht)
    gleich("unterwegs geht nicht", CastAdresse.Ergebnis.Nicht(CastAdresse.UNTERWEGS),
        CastAdresse.fuer("https://vermittler.example:443", true) { "192.168.1.20" })
    gleich("https behält den Namen", "https://lumen.zuhause.example",
        im(CastAdresse.fuer("https://lumen.zuhause.example", false) { "10.0.0.5" }))
    pruefe("172.16/12 ist Heimnetz", CastAdresse.imHeimnetz("172.20.1.1"))
    pruefe("172.32 nicht", !CastAdresse.imHeimnetz("172.32.1.1"))
    pruefe("kein IPv4", !CastAdresse.istIPv4("1.2.3"))
    gleich("umschreiben", "http://192.168.1.20:8097/api/v1/stream/f/hls/master.m3u8?key=K",
        CastAdresse.umschreiben("http://nas.local:8097/api/v1/stream/f/hls/master.m3u8?key=K",
            "http://nas.local:8097", "http://192.168.1.20:8097"))
    gleich("HLS-Typ", "application/x-mpegURL", CastAdresse.inhaltsTyp(false, "mkv"))
    gleich("MP4-Typ", "video/mp4", CastAdresse.inhaltsTyp(true, "mp4"))

    // Ein MKV läuft auf dem Telefon direkt, auf dem Chromecast nicht.
    val mkv = MediaFile.from(Json.parse("""{"id":"f","container":"mkv","videoCodec":"h264"}"""))
    val direkt = PlaybackPlan(true, null, "h264", 1080, 0)
    val client = ApiClient("http://192.168.1.20:8097", "T")
    pruefe("MKV direkt fürs Telefon", client.istDirekt(mkv, direkt))
    pruefe("MKV nicht direkt für den Chromecast", !client.istDirekt(mkv, direkt, DeviceProfile.chromecast))
    pruefe("Chromecast bekommt HLS", client.streamUrl(mkv, direkt, profil = DeviceProfile.chromecast)
        .contains("/hls/master.m3u8"))
}

/**
 * Sprachen: Kennungen lesen, die Vorgabe ohne Übersetzer und was mit
 * der Sprache der Oberfläche wechselt (Daten, Zahlen, Sprachnamen).
 */
fun spracheTests() {
    gleich("normalisieren", "de-CH", Sprachen.normalisieren("de_ch"))
    gleich("normalisieren gross", "en-GB", Sprachen.normalisieren("EN-gb"))
    gleich("unbekannt ist null", null, Sprachen.normalisieren("fr-FR"))
    gleich("leer ist null", null, Sprachen.normalisieren(" "))

    val hello = Hello.from(Json.parse("""{"server":"Lumen","version":"1","setUp":true,"ffmpeg":true,
        "sprache":"de-AT","sprachen":[{"id":"de-AT","name":"Deutsch (Österreich)"},{"id":"en-US"}]}"""))
    gleich("Hello: Vorgabe", "de-AT", hello.sprache)
    gleich("Hello: Liste", listOf(Sprache("de-AT", "Deutsch (Österreich)"), Sprache("en-US", "English (United States)")),
           hello.sprachen)
    val alt = Hello.from(Json.parse("""{"server":"Lumen","version":"1","setUp":true,"ffmpeg":true}"""))
    gleich("älterer Server: keine Vorgabe", null, alt.sprache)
    gleich("älterer Server: die fünf zur Wahl", Sprachen.UNTERSTUETZT, Sprachen.wahl(alt.sprachen).map { it.id })

    // Ohne Übersetzer: die Schweizer Vorgaben, mit Platzhaltern.
    val vorher = Texte.sprache
    try {
        gleich("Vorgabe", "Staffel 2", Texte.t(Tx.STAFFEL_N, 2))
        gleich("Mehrzahl eins", "1 Film", Texte.n(TxN.FILME_ANZAHL, 1))
        gleich("Mehrzahl mehr", "3 Filme", Texte.n(TxN.FILME_ANZAHL, 3))
        gleich("Prozent", "93 % Rotten Tomatoes", Texte.t(Tx.TOMATEN, 93))

        // Ein Übersetzer, wie ihn die Apps setzen.
        Texte.uebersetzer = object : Uebersetzer {
            override fun text(k: Tx, args: Array<out Any?>) = when (k) {
                Tx.STAFFEL_N -> "Season %1\$s"
                Tx.MUSTER_TAG_MONAT -> "MMM d"
                Tx.MUSTER_DATUM -> "M/d/yyyy"
                Tx.DEZIMALZEICHEN -> "."
                Tx.TAUSENDERZEICHEN -> ","
                else -> k.vorgabe
            }.let { Texte.formatieren(it, args) }
            override fun mehrzahl(k: TxN, anzahl: Int, args: Array<out Any?>) =
                if (anzahl == 1) "1 movie" else "$anzahl movies"
        }
        Texte.sprache = "en-US"
        gleich("übersetzt", "Season 2", Texte.t(Tx.STAFFEL_N, 2))
        gleich("Mehrzahl übersetzt", "3 movies", Texte.n(TxN.FILME_ANZAHL, 3))
        gleich("Datum en-US", "4/14/2019", Titelseite.datum("2019-04-14"))
        gleich("Kurzdatum en-US", "Oct 29",
               Zeitpunkt.kurz("2026-10-29T09:20:40+01:00", java.time.ZoneId.of("Europe/Zurich")))
        gleich("Komma en", "7.8", Titelseite.komma(7.8))
        gleich("Tausender en", "12,345", Titelseite.tausender(12345))
        gleich("Sprachname en", "German", Spuren.sprache("de"))
        Texte.sprache = "de-AT"
        Texte.uebersetzer = null
        gleich("Jänner in de-AT", "3. Jän.",
               Zeitpunkt.kurz("2026-01-03T09:00:00+01:00", java.time.ZoneId.of("Europe/Vienna")))
    } finally {
        Texte.uebersetzer = null
        Texte.sprache = vorher
    }
}
