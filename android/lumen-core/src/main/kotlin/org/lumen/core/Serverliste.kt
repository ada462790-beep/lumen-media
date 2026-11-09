// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package org.lumen.core

import java.net.URI
import java.util.UUID

/**
 * Ein gespeicherter Server: wie er heisst, wo er steht und — falls er von
 * unterwegs über den Vermittler erreicht wird — der Fingerabdruck seines
 * Zertifikats. Das Geräte-Token liegt nicht hier, sondern getrennt je
 * Server in der Ablage; so steht es nie zusammen mit der ganzen Liste in
 * einem Feld.
 */
data class GespeicherterServer(
    val id: String,
    val name: String,
    val adresse: String,
    val fingerabdruck: String? = null,
    /** Von Hand umbenannt — dann überschreibt der Server den Namen nicht mehr. */
    val umbenannt: Boolean = false,
    /** Wann zuletzt benutzt, in Millisekunden seit 1970. */
    val zuletzt: Long = 0,
) {
    /** Die Adresse ohne http:// — so liest sie ein Mensch. */
    val adresseKurz: String get() = adresse.substringAfter("://")

    /** Von unterwegs, also über https mit festgenageltem Zertifikat. */
    val vonUnterwegs: Boolean get() = !fingerabdruck.isNullOrEmpty()
}

/**
 * Eine Ablage für Schlüssel und Werte. Auf Android sind das verschlüsselte
 * Einstellungen; hier im Kern genügt die Schnittstelle — damit bleibt die
 * ganze Logik der Serverliste ohne Gerät prüfbar.
 */
interface Schluesselablage {
    fun lesen(schluessel: String): String?

    /** `null` entfernt den Schlüssel. */
    fun schreiben(schluessel: String, wert: String?)
}

/** Eine Ablage im Arbeitsspeicher — für Tests und die Kommandozeile. */
class MemorySchluesselablage(anfang: Map<String, String> = emptyMap()) : Schluesselablage {
    private val werte = HashMap(anfang)

    @Synchronized override fun lesen(schluessel: String): String? = werte[schluessel]

    @Synchronized override fun schreiben(schluessel: String, wert: String?) {
        if (wert == null) werte.remove(schluessel) else werte[schluessel] = wert
    }

    @Synchronized fun schluessel(): Set<String> = werte.keys.toSet()
}

/**
 * Die Liste der Server, die sich diese App gemerkt hat — wie bei Emby:
 * antippen und verbinden, hinzufügen, umbenennen, entfernen.
 *
 * Zugleich ist sie der [TokenStore] des gerade gewählten Servers. Der
 * [SessionController] arbeitet damit wie bisher mit *einer* Adresse und
 * *einem* Token; welcher Server das ist, entscheidet [waehlen].
 *
 * Früher kannte die App genau einen Server, mit den Schlüsseln `server`,
 * `token` und `fingerabdruck`. Steht so etwas noch in [alt], wird es beim
 * ersten Start als erster Eintrag übernommen und danach dort gelöscht —
 * niemand muss sich neu anmelden.
 */
class Serverliste(
    private val ablage: Schluesselablage,
    alt: Schluesselablage = ablage,
    private val uhr: () -> Long = System::currentTimeMillis,
) : TokenStore {

    companion object {
        const val LISTE = "serverliste"
        const val AKTIV = "serverliste.aktiv"
        private const val TOKEN = "serverliste.token."

        // Die Schlüssel aus der Zeit, als es nur einen Server gab.
        const val ALT_SERVER = "server"
        const val ALT_TOKEN = "token"
        const val ALT_FINGERABDRUCK = "fingerabdruck"

        /** Das Höchstmass für einen Namen — er soll in eine Zeile passen. */
        const val NAME_LAENGE = 40

        fun tokenSchluessel(id: String) = TOKEN + id

        /**
         * Ein Name, solange niemand von Hand einen vergibt: der, den der
         * Besitzer dem Server gegeben hat („Mein NAS"). Hat er keinen, der
         * Rechnername aus der Adresse: „nas", „192.168.1.20" — „Lumen", wie
         * sich jeder Server meldet, unterscheidet zwei Server nicht.
         */
        fun standardName(adresse: String, hello: Hello? = null): String {
            hello?.name?.trim()?.takeIf { it.isNotEmpty() }?.let { return it.take(NAME_LAENGE) }
            val eigener = hello?.server?.trim().orEmpty()
            if (eigener.isNotEmpty() && !eigener.equals("Lumen", ignoreCase = true)) {
                return eigener.take(NAME_LAENGE)
            }
            val host = try { URI(adresse).host } catch (e: Exception) { null }
                ?: adresse.substringAfter("://").substringBefore(':').substringBefore('/')
            // „nas.local" heisst im Haushalt „nas".
            val kurz = if (host.endsWith(".local", ignoreCase = true)) host.dropLast(6) else host
            return kurz.ifEmpty { "Lumen" }.take(NAME_LAENGE)
        }

        /** Gleiche Adresse, auch wenn sie anders geschrieben wurde. */
        private fun gleicheAdresse(a: String, b: String): Boolean =
            a.trimEnd('/').equals(b.trimEnd('/'), ignoreCase = true)
    }

    init { uebernehmen(alt) }

    // ---------------------------------------------------------- Lesen

    /** Alle Server, in der Reihenfolge, in der sie dazukamen. */
    val server: List<GespeicherterServer>
        get() = lesenListe()

    val aktiv: GespeicherterServer?
        get() {
            val id = ablage.lesen(AKTIV) ?: return null
            return server.firstOrNull { it.id == id }
        }

    fun finde(id: String): GespeicherterServer? = server.firstOrNull { it.id == id }

    fun findeAdresse(adresse: String): GespeicherterServer? =
        server.firstOrNull { gleicheAdresse(it.adresse, adresse) }

    /** Das Geräte-Token für diesen Server — nur für den Kern selbst. */
    internal fun tokenVon(id: String): String? = ablage.lesen(tokenSchluessel(id))

    /** Ob dieses Gerät auf dem Server noch angemeldet ist. */
    fun angemeldet(id: String): Boolean = !ablage.lesen(tokenSchluessel(id)).isNullOrEmpty()

    // ---------------------------------------------------------- Ändern

    /**
     * Einen Server wählen. Er gilt ab jetzt als der zuletzt benutzte —
     * beim nächsten Start verbindet die App ihn von selbst.
     */
    @Synchronized
    fun waehlen(id: String): GespeicherterServer? {
        val liste = lesenListe()
        val i = liste.indexOfFirst { it.id == id }
        if (i < 0) return null
        val neu = liste[i].copy(zuletzt = uhr())
        schreibenListe(liste.toMutableList().also { it[i] = neu })
        ablage.schreiben(AKTIV, id)
        return neu
    }

    /**
     * Einen Server merken und wählen. Kennt die Liste die Adresse schon,
     * bleibt es derselbe Eintrag — samt Token, Namen und, wenn kein neuer
     * kommt, Fingerabdruck.
     */
    @Synchronized
    fun merken(adresse: String, fingerabdruck: String? = null, name: String? = null): GespeicherterServer {
        val liste = lesenListe().toMutableList()
        val i = liste.indexOfFirst { gleicheAdresse(it.adresse, adresse) }
        val eintrag = if (i >= 0) {
            val alt = liste[i]
            alt.copy(
                fingerabdruck = fingerabdruck ?: alt.fingerabdruck,
                name = if (alt.umbenannt || name.isNullOrBlank()) alt.name else name.trim().take(NAME_LAENGE),
                zuletzt = uhr(),
            ).also { liste[i] = it }
        } else {
            GespeicherterServer(
                id = UUID.randomUUID().toString().replace("-", "").take(12),
                name = name?.trim()?.takeIf { it.isNotEmpty() }?.take(NAME_LAENGE) ?: standardName(adresse),
                adresse = adresse,
                fingerabdruck = fingerabdruck,
                zuletzt = uhr(),
            ).also { liste.add(it) }
        }
        schreibenListe(liste)
        ablage.schreiben(AKTIV, eintrag.id)
        return eintrag
    }

    /** Den Namen von Hand setzen. Leer heisst: wieder den vom Server. */
    @Synchronized
    fun umbenennen(id: String, name: String) {
        aendern(id) {
            val t = name.trim().take(NAME_LAENGE)
            if (t.isEmpty()) it.copy(name = standardName(it.adresse), umbenannt = false)
            else it.copy(name = t, umbenannt = true)
        }
    }

    /** Den Namen übernehmen, den der Server nennt — ausser er wurde umbenannt. */
    @Synchronized
    fun nameVomServer(id: String, name: String) {
        aendern(id) { if (it.umbenannt || name.isBlank()) it else it.copy(name = name.take(NAME_LAENGE)) }
    }

    /**
     * Einen Server vergessen — samt Token. War er gewählt, ist danach
     * keiner mehr gewählt; welcher nun drankommt, entscheidet, wer fragt.
     */
    @Synchronized
    fun entfernen(id: String) {
        schreibenListe(lesenListe().filter { it.id != id })
        ablage.schreiben(tokenSchluessel(id), null)
        if (ablage.lesen(AKTIV) == id) ablage.schreiben(AKTIV, null)
    }

    private fun aendern(id: String, wie: (GespeicherterServer) -> GespeicherterServer) {
        val liste = lesenListe()
        val i = liste.indexOfFirst { it.id == id }
        if (i < 0) return
        schreibenListe(liste.toMutableList().also { it[i] = wie(liste[i]) })
    }

    // ---------------------------------------------------------- TokenStore

    override var serverUrl: String?
        get() = aktiv?.adresse
        set(value) {
            if (value == null) ablage.schreiben(AKTIV, null) else merken(value)
        }

    override var token: String?
        get() = aktiv?.let { ablage.lesen(tokenSchluessel(it.id)) }
        set(value) {
            val id = aktiv?.id ?: return
            ablage.schreiben(tokenSchluessel(id), value?.takeIf { it.isNotEmpty() })
        }

    override var fingerabdruck: String?
        get() = aktiv?.fingerabdruck
        set(value) {
            val id = aktiv?.id ?: return
            synchronized(this) { aendern(id) { it.copy(fingerabdruck = value) } }
        }

    // ---------------------------------------------------------- Ablage

    private fun lesenListe(): List<GespeicherterServer> {
        val text = ablage.lesen(LISTE) ?: return emptyList()
        return try {
            Json.parse(text)["server"].list.mapNotNull { j ->
                val id = j["id"].text ?: return@mapNotNull null
                val adresse = j["adresse"].text ?: return@mapNotNull null
                GespeicherterServer(
                    id = id,
                    name = j["name"].text ?: standardName(adresse),
                    adresse = adresse,
                    fingerabdruck = j["fingerabdruck"].text,
                    umbenannt = j["umbenannt"].bool ?: false,
                    zuletzt = j["zuletzt"].long ?: 0,
                )
            }
        } catch (e: JsonException) {
            // Kaputt gespeichert — lieber neu anfangen als nicht starten.
            emptyList()
        }
    }

    private fun schreibenListe(liste: List<GespeicherterServer>) {
        ablage.schreiben(LISTE, JsonWriter.obj("server" to liste.map {
            mapOf(
                "id" to it.id, "name" to it.name, "adresse" to it.adresse,
                "fingerabdruck" to it.fingerabdruck, "umbenannt" to it.umbenannt,
                "zuletzt" to it.zuletzt,
            )
        }))
    }

    /**
     * Die Daten aus der Zeit mit nur einem Server übernehmen — genau einmal.
     * Danach sind die alten Schlüssel weg; vor allem das Token soll nicht
     * doppelt herumliegen, schon gar nicht unverschlüsselt.
     */
    @Synchronized
    private fun uebernehmen(alt: Schluesselablage) {
        val adresse = alt.lesen(ALT_SERVER)
        if (ablage.lesen(LISTE) == null) {
            if (!adresse.isNullOrBlank()) {
                val eintrag = merken(adresse, alt.lesen(ALT_FINGERABDRUCK)?.takeIf { it.isNotBlank() })
                alt.lesen(ALT_TOKEN)?.takeIf { it.isNotEmpty() }?.let {
                    ablage.schreiben(tokenSchluessel(eintrag.id), it)
                }
            } else {
                schreibenListe(emptyList())
            }
        }
        if (adresse != null || alt.lesen(ALT_TOKEN) != null || alt.lesen(ALT_FINGERABDRUCK) != null) {
            alt.schreiben(ALT_SERVER, null)
            alt.schreiben(ALT_TOKEN, null)
            alt.schreiben(ALT_FINGERABDRUCK, null)
        }
    }
}
