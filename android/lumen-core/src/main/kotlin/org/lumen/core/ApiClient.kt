// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package org.lumen.core

import java.io.IOException
import java.net.HttpURLConnection
import java.net.URI
import java.net.URLEncoder
import javax.net.ssl.HttpsURLConnection

/**
 * Die Verbindung zum Server.
 *
 * Gebaut auf `java.net.HttpURLConnection`. Das ist nicht die schönste
 * Schnittstelle im JDK — aber die einzige, die es auf Android wirklich
 * gibt.
 *
 * Hier stand erst `java.net.http.HttpClient`, mit dem Kommentar, den gebe
 * es „seit Android 13 auch dort". Das ist falsch: Android enthält das
 * Paket `java.net.http` in keiner Fassung, und die Entzuckerung bringt es
 * auch nicht mit. Übersetzt hätte es trotzdem — `lumen-core` ist ein
 * gewöhnliches JVM-Modul und wird gegen das JDK gebaut. Erst der erste
 * Netzaufruf auf dem Gerät wäre mit `NoClassDefFoundError` umgefallen.
 *
 * Darum HttpURLConnection: steckt in jedem Android, braucht keine fremde
 * Bibliothek, und `lumen-core` bleibt ohne Android übersetzbar und ohne
 * Gerät testbar.
 */
class ApiClient(
    baseUrl: String,
    token: String? = null,
) {
    var baseUrl: String = baseUrl.trimEnd('/')
        set(value) {
            val neu = value.trimEnd('/')
            // Ein Medienschlüssel gilt nur bei dem Server, der ihn ausgab.
            if (neu != field) medienSchluesselVergessen()
            field = neu
        }

    /** Schützt das Setzen des Tokens und das Vergleichen-und-Löschen nach
     *  einer 401 (siehe [senden]) — beides darf sich nicht überholen. */
    private val tokenSperre = Any()

    /**
     * Das Token dieses Geräts. Es reist nur im Kopf „Authorization", nie in
     * einer Adresse — siehe [medienSchluessel].
     */
    // @Volatile: gesetzt wird im Hintergrund (Anmelden, 401), gelesen
    // überall — auch beim Bauen von Bildadressen mitten im Zeichnen.
    @Volatile var token: String? = token
        set(value) = synchronized(tokenSperre) {
            // Neues Token, neue Person oder abgemeldet: der Schlüssel des
            // alten Tokens öffnet dann nichts mehr, was uns gehört.
            if (value != field) medienSchluesselVergessen()
            field = value
        }

    val isSignedIn: Boolean get() = !token.isNullOrEmpty()

    /**
     * Wird gerufen, wenn der Server das Token zurückweist (401) — die
     * Anmeldung gilt nicht mehr. Wie setAbmeldeHandler auf Apple: Wer die
     * Oberfläche führt, meldet dann ab und zeigt wieder die Profilwahl.
     *
     * Läuft im Faden der Anfrage, also im Hintergrund. Wer die Oberfläche
     * anfasst, muss selbst auf den Hauptfaden wechseln. Gerufen wird nur,
     * wenn das zurückgewiesene Token noch das aktuelle war — siehe [senden].
     */
    @Volatile var beiAbmeldung: (() -> Unit)? = null

    /**
     * Das Zertifikat des Servers festnageln.
     *
     * Von unterwegs läuft alles über TLS, und ein Heimserver stellt sein
     * Zertifikat meist selbst aus — dem traut kein Gerät von sich aus.
     * Statt die Prüfung abzuschalten wird genau eines zugelassen; den
     * Fingerabdruck zeigt der Server in der Verwaltung an.
     *
     * Null oder unbrauchbar heisst: zurück zur gewöhnlichen Prüfung.
     */
    fun pinnen(fingerabdruck: String?) {
        gepinnt = Pinnen.pruefer(fingerabdruck)
    }

    /** Die Prüfung fürs Zertifikat, falls eine eingerichtet ist. */
    private var gepinnt: Pinnen.Pruefer? = null

    // ---------------------------------------------------------- Medienschlüssel

    /**
     * Der Medienschlüssel: was in Bild-, Video- und Untertiteladressen
     * steht, statt des Tokens.
     *
     * Ein Abspieler oder Bildlader holt seine Adresse selbst und schickt
     * keinen eigenen Kopf mit — darum muss etwas in der Adresse stehen.
     * Früher war das das Token. Nur schreibt jeder Reverse-Proxy vor dem
     * NAS jede Adresse in sein Protokoll, und mit dem Token daraus stünde
     * die ganze Schnittstelle offen, für immer. Der Schlüssel öffnet nur
     * Bilder, Videos und Untertitel, lebt nur im Arbeitsspeicher des
     * Servers und läuft ab: 6 Stunden nach der letzten Benutzung,
     * spätestens nach 48 Stunden, und nach einem Neustart des Servers
     * sofort.
     *
     * Hier liegt er deshalb auch nur im Speicher, nie in der Ablage — nach
     * einem Neustart der App wird ein neuer geholt. Die Adressen werden
     * synchron gebaut (die Oberfläche fragt mitten im Zeichnen danach) und
     * lesen nur, was hier liegt; geholt wird vorher, im Hintergrund:
     * [medienSchluesselSichern] nach der Anmeldung und beim Start,
     * [wiedergabeVorbereiten] vor jedem Film, [nachAbgelaufenemSchluessel]
     * nach einer 401.
     */
    @Volatile var medienSchluessel: String? = null
        private set

    /** Bis wann der Schlüssel nach unserer Rechnung gilt (Millisekunden). */
    @Volatile var medienSchluesselBis: Long = 0
        private set

    /**
     * Ein eigener, frischer Schlüssel für die laufende Wiedergabe.
     *
     * Getrennt vom Schlüssel für die Bilder: Ein neuer Schlüssel ändert die
     * Adresse jedes Bildes, und der Bildlader hielte sie für neue Bilder —
     * nach jedem Film würden alle Poster noch einmal geladen. So bleibt der
     * Bildschlüssel, wie er ist, und der Film bekommt trotzdem einen, der
     * gerade erst ausgegeben wurde.
     */
    @Volatile private var wiedergabeSchluessel: String? = null

    /** Die Uhr — austauschbar, damit sich das Erneuern prüfen lässt. */
    var uhr: () -> Long = { System.currentTimeMillis() }

    /** Ob die Adressbauer einen bald ablaufenden Schlüssel selbst im
     *  Hintergrund erneuern. Nur Tests schalten das ab, um mitzuzählen. */
    var imHintergrundErneuern = true

    private val schluesselSperre = Any()

    /** Läuft gerade ein Erneuern im Hintergrund? Dann nicht noch eins. */
    private val erneuertImHintergrund = java.util.concurrent.atomic.AtomicBoolean(false)

    /**
     * Wann das Holen zuletzt scheiterte. Ist der Server weg, baut die
     * Oberfläche trotzdem weiter Adressen, und jedes Bild bekommt seine
     * 401 — ohne Pause fragte dann jedes einzelne noch einmal beim Server
     * an, der gar nicht antwortet.
     */
    @Volatile private var letzterFehlschlag = 0L

    private fun kurzNachFehlschlag(): Boolean = uhr() - letzterFehlschlag < PAUSE_NACH_FEHLSCHLAG

    private fun medienSchluesselVergessen() {
        medienSchluessel = null
        medienSchluesselBis = 0
        wiedergabeSchluessel = null
    }

    /** Für Tests: einen Schlüssel setzen, ohne den Server zu fragen. */
    fun medienSchluesselSetzen(schluessel: String?, gueltigMs: Long = MEDIEN_FRISCH) {
        medienSchluessel = schluessel
        medienSchluesselBis = if (schluessel == null) 0 else uhr() + gueltigMs
    }

    /**
     * Einen neuen Schlüssel beim Server holen — nur mit dem Token im Kopf.
     * Er gilt danach für die Bilder; zurück kommt er auch.
     */
    fun medienSchluesselHolen(): Medienschluessel {
        val m = try {
            Medienschluessel.from(getJson("/me/media-key"))
        } catch (e: LumenException) {
            letzterFehlschlag = uhr()
            throw e
        }
        synchronized(schluesselSperre) {
            medienSchluessel = m.key
            // Mit der Dauer rechnen, nicht mit dem Zeitpunkt: Die Uhr eines
            // Fernsehers geht gern ein paar Minuten falsch, und dann wäre
            // „gültig bis 14:00" hier etwas anderes als beim Server.
            medienSchluesselBis = uhr() + m.gueltigSek * 1000L
        }
        return m
    }

    /**
     * Dafür sorgen, dass ein Schlüssel da ist, der noch eine Weile hält —
     * nach der Anmeldung, beim Start und immer dann, wenn weniger als eine
     * Stunde übrig ist. Blockiert; auf Android im Hintergrund aufrufen.
     *
     * Gibt an, ob danach ein Schlüssel da ist. Klappt das Holen nicht,
     * bleibt der alte — ein Schlüssel, der vielleicht noch gilt, ist
     * besser als keiner.
     */
    fun medienSchluesselSichern(): Boolean {
        if (!isSignedIn) return false
        if (medienSchluessel != null && medienSchluesselBis - uhr() > MEDIEN_ERNEUERN) return true
        return try {
            medienSchluesselHolen()
            true
        } catch (e: LumenException) {
            medienSchluessel != null
        }
    }

    /**
     * Läuft der Schlüssel bald ab, im Hintergrund einen neuen holen. Aus den
     * Adressbauern heraus: die blockieren nie, und wer sie aufruft, merkt
     * davon nichts — die nächste Adresse trägt dann schon den neuen.
     */
    private fun baldErneuern() {
        if (!imHintergrundErneuern || !isSignedIn) return
        if (medienSchluessel != null && medienSchluesselBis - uhr() > MEDIEN_ERNEUERN) return
        if (kurzNachFehlschlag()) return
        if (!erneuertImHintergrund.compareAndSet(false, true)) return
        val t = Thread {
            try { medienSchluesselSichern() } finally { erneuertImHintergrund.set(false) }
        }
        t.isDaemon = true
        t.name = "lumen-medienschluessel"
        t.start()
    }

    /**
     * Vor jedem Film: den Plan erfragen und einen frischen Schlüssel für
     * genau diese Wiedergabe holen. Ein Film läuft Stunden, und der
     * Abspieler lädt die ganze Zeit Stücke nach — mit der Adresse, die er
     * am Anfang bekam. Ein Schlüssel, der in zehn Minuten abliefe, risse
     * ihm mittendrin den Strom ab. Ein frischer hält: jedes Stück verlängert
     * ihn wieder um 6 Stunden, bis zu den 48 des Servers.
     *
     * Klappt das Holen nicht, nimmt die Wiedergabe den Bildschlüssel.
     */
    fun wiedergabeVorbereiten(fileId: String): PlaybackPlan {
        val p = plan(fileId)
        wiedergabeSchluesselErneuern()
        return p
    }

    /**
     * Einen neuen Schlüssel für die Wiedergabe — vor dem Start und, falls
     * der Abspieler eine 401 bekommt, einmal danach. Gibt an, ob es klappte.
     */
    fun wiedergabeSchluesselErneuern(): Boolean {
        if (!isSignedIn) return false
        return try {
            val m = Medienschluessel.from(getJson("/me/media-key"))
            wiedergabeSchluessel = m.key
            // Kein Bildschlüssel da (oder ein abgelaufener)? Dann gilt der
            // neue gleich auch für die Bilder.
            synchronized(schluesselSperre) {
                if (medienSchluessel == null || medienSchluesselBis <= uhr()) {
                    medienSchluessel = m.key
                    medienSchluesselBis = uhr() + m.gueltigSek * 1000L
                }
            }
            true
        } catch (e: LumenException) {
            false
        }
    }

    /**
     * Nach einer 401 auf eine Medienadresse: einmal einen neuen Schlüssel
     * holen und die Adresse damit neu bauen. Für den Bildlader, der genau
     * einmal wiederholt.
     *
     * Der Server kann neu gestartet sein — dann kennt er keinen Schlüssel
     * mehr, auch wenn er nach unserer Rechnung noch Stunden gälte.
     *
     * Viele Bilder scheitern meist gleichzeitig. Geholt wird trotzdem nur
     * einmal: Trägt die Adresse schon einen anderen als den aktuellen
     * Schlüssel, hat ein anderer Faden erneuert, und die Adresse bekommt
     * einfach den aktuellen. Null heisst: nicht unsere Adresse, oder es
     * gibt keinen neuen Schlüssel — dann bleibt es bei der 401.
     */
    fun nachAbgelaufenemSchluessel(adresse: String): String? {
        if (!isSignedIn || !istMedienAdresse(adresse)) return null
        val alt = schluesselIn(adresse)
        val neu = synchronized(schluesselSperre) {
            val jetzt = medienSchluessel
            if (jetzt != null && jetzt != alt) {
                jetzt
            } else if (kurzNachFehlschlag()) {
                null
            } else {
                try { medienSchluesselHolen().key } catch (e: LumenException) { null }
            }
        } ?: return null
        return mitSchluessel(adresse, neu)
    }

    /** Eine Adresse dieses Servers, die einen Medienschlüssel annimmt? */
    fun istMedienAdresse(adresse: String): Boolean {
        val api = "$baseUrl/api/v1/"
        if (!adresse.startsWith(api)) return false
        val rest = adresse.substring(api.length)
        return rest.startsWith("art/") || rest.startsWith("avatar/") || rest.startsWith("stream/")
    }

    /** Der Schlüssel, der in einer Adresse steht — oder null. */
    private fun schluesselIn(adresse: String): String? =
        adresse.substringAfter('?', "").split('&')
            .firstOrNull { it.startsWith("key=") }
            ?.removePrefix("key=")
            ?.let { java.net.URLDecoder.decode(it, Charsets.UTF_8) }

    /** Die Adresse mit [schluessel] statt des alten (oder überhaupt mit einem). */
    private fun mitSchluessel(adresse: String, schluessel: String): String {
        val basis = adresse.substringBefore('?')
        val teile = adresse.substringAfter('?', "").split('&')
            .filter { it.isNotEmpty() && !it.startsWith("key=") } + "key=${enc(schluessel)}"
        return basis + "?" + teile.sorted().joinToString("&")
    }

    /** Der Schlüssel für Bildadressen — und ein Anstoss, falls er bald abläuft. */
    private fun bildSchluessel(): String? {
        baldErneuern()
        return medienSchluessel
    }

    companion object {
        /** Wie lange auf das Zustandekommen der Verbindung gewartet wird. */
        const val VERBINDUNGSFRIST = 8_000

        /** Wie lange auf die Antwort. Grosszügiger: der Server muss beim
         *  Einlesen einer Bibliothek schon mal nachdenken. */
        const val ANTWORTFRIST = 30_000

        /** Weniger als eine Stunde übrig: dann einen neuen Medienschlüssel holen. */
        const val MEDIEN_ERNEUERN = 60 * 60 * 1000L

        /** So lange gilt ein frischer Medienschlüssel beim Server (6 Stunden). */
        const val MEDIEN_FRISCH = 6 * 60 * 60 * 1000L

        /** Nach einem gescheiterten Holen so lange nicht wieder fragen. */
        const val PAUSE_NACH_FEHLSCHLAG = 15_000L

        /** „192.168.1.20", „lumen.local:8097", „http://…" — alles soll gehen. */
        fun normalizeAddress(input: String): String? {
            var text = input.trim()
            if (text.isEmpty()) return null
            if (!text.contains("://")) text = "http://$text"
            return try {
                val uri = URI(text)
                val host = uri.host ?: return null
                if (host.isEmpty()) return null
                val scheme = uri.scheme ?: "http"
                val port = if (uri.port > 0) uri.port else if (scheme == "http") 8097 else -1
                if (port > 0) "$scheme://$host:$port" else "$scheme://$host"
            } catch (e: Exception) {
                null
            }
        }
    }

    // ---------------------------------------------------------- Grundlage

    private fun url(path: String, query: Map<String, String> = emptyMap()): String {
        val base = "$baseUrl/api/v1$path"
        if (query.isEmpty()) return base
        val q = query.entries.sortedBy { it.key }.joinToString("&") {
            "${enc(it.key)}=${enc(it.value)}"
        }
        return "$base?$q"
    }

    private fun enc(s: String) = URLEncoder.encode(s, Charsets.UTF_8)

    /**
     * Ein Stück eines Pfads, etwa ein Name mit Leerzeichen. URLEncoder
     * schreibt für Formulare und macht aus dem Leerzeichen ein „+" — im
     * Pfad bliebe das ein Plus, und aus „Tom Hanks" würde „Tom+Hanks".
     */
    private fun pfadTeil(s: String) = enc(s).replace("+", "%20")

    private fun send(
        method: String,
        path: String,
        query: Map<String, String> = emptyMap(),
        body: String? = null,
        // Roh statt JSON — etwa ein Bild fürs Profil. Dann zählt [typ].
        roh: ByteArray? = null,
        typ: String = "application/octet-stream",
    ): String = senden(method, path, query, body, roh, typ).text

    /**
     * Wie [send], aber mit dem Statuscode — für die wenigen Stellen, an
     * denen 200 und 202 Verschiedenes heissen (Trickplay: fertig oder „wird
     * gerade erstellt").
     */
    private fun senden(
        method: String,
        path: String,
        query: Map<String, String> = emptyMap(),
        body: String? = null,
        roh: ByteArray? = null,
        typ: String = "application/octet-stream",
    ): Paar {
        // Das Token, mit dem diese Anfrage losgeht. Kommt die 401 erst
        // später — nach einem Profilwechsel —, darf sie nicht das Token des
        // neuen Profils löschen. Swift macht dasselbe mit `gesendet`.
        val gesendet = token
        val hadToken = !gesendet.isNullOrEmpty()

        // PATCH kennt HttpURLConnection nicht — `setRequestMethod("PATCH")`
        // wirft eine ProtocolException. Der Server nimmt für denselben
        // Griff auch POST entgegen; siehe api.go.
        val methode = if (method == "PATCH") "POST" else method

        val c = try {
            oeffnen(url(path, query))
        } catch (e: IOException) {
            throw LumenException.Unreachable(e)
        }

        val antwort: Paar = try {
            c.requestMethod = methode
            c.connectTimeout = VERBINDUNGSFRIST
            c.readTimeout = ANTWORTFRIST
            c.instanceFollowRedirects = true
            c.setRequestProperty("Accept", "application/json")
            // Damit Meldungen des Servers schon vor der Anmeldung in der
            // Sprache der App kommen; danach gilt die Sprache des Profils.
            c.setRequestProperty("Accept-Language", Texte.sprache)
            gesendet?.let { c.setRequestProperty("Authorization", "Bearer $it") }

            if (body != null || roh != null) {
                c.doOutput = true
                c.setRequestProperty("Content-Type", if (roh != null) typ else "application/json")
                val daten = roh ?: body!!.toByteArray(Charsets.UTF_8)
                // Nur rohe Daten (ein Profilbild, bis 16 MB) am Stück
                // durchreichen. Im Streaming-Modus gibt die JDK-Verbindung
                // nach einer 401 keinen Fehlerstrom heraus — und dann käme
                // statt „das stimmt nicht" beim falschen PIN nur „Das hat
                // nicht geklappt.". Ein JSON-Rumpf ist klein genug zum Puffern.
                if (roh != null) c.setFixedLengthStreamingMode(daten.size)
                c.outputStream.use { it.write(daten) }
            }

            val code = c.responseCode
            // Ab 400 liegt der Text im Fehlerstrom, nicht im normalen —
            // und genau dort stehen die Meldungen des Servers, die wir
            // der Person zeigen wollen.
            val strom = if (code >= 400) c.errorStream else c.inputStream
            val text = strom?.bufferedReader(Charsets.UTF_8)?.use { it.readText() } ?: ""
            Paar(code, text)
        } catch (e: javax.net.ssl.SSLException) {
            // Muss vor IOException stehen — SSLException ist eine.
            throw LumenException.Zertifikat(zertifikatsHinweis(e), e)
        } catch (e: IOException) {
            throw LumenException.Unreachable(e)
        } finally {
            c.disconnect()
        }

        return when (val code = antwort.code) {
            in 200..299 -> antwort
            // 401 mit Token heisst: die Anmeldung gilt nicht mehr.
            // 401 ohne Token heisst: der Server weist gerade das zurück, was
            // wir ihm eben gegeben haben — einen falschen PIN etwa. Wer das
            // vermengt, sagt beim Vertippen „Deine Anmeldung ist abgelaufen".
            401 -> if (hadToken) {
                val nochAktuell = synchronized(tokenSperre) {
                    if (token == gesendet) { token = null; true } else false
                }
                if (!nochAktuell) {
                    // Die Antwort gilt einem Token, das schon ersetzt ist —
                    // ein anderes Profil, eine neue Anmeldung. Kein Abmelden:
                    // das träfe die Person, die jetzt angemeldet ist.
                    throw LumenException.Server(messageOf(antwort.text), code)
                }
                beiAbmeldung?.invoke()
                throw LumenException.SignedOut
            } else {
                throw LumenException.Server(messageOf(antwort.text), code)
            }
            // Von unterwegs: keine Profilauswahl, sondern Name und PIN.
            403 -> if (codeOf(antwort.text) == "unterwegs") {
                throw LumenException.Unterwegs(messageOf(antwort.text))
            } else {
                throw LumenException.Server(messageOf(antwort.text), code, codeOf(antwort.text))
            }
            else -> throw LumenException.Server(messageOf(antwort.text), code, codeOf(antwort.text))
        }
    }

    /** Der maschinenlesbare Code einer Fehlerantwort, falls es einen gibt. */
    private fun codeOf(body: String): String? =
        try {
            Json.parse(body)["code"].text
        } catch (e: JsonException) {
            null
        }

    /** Antwortcode und Text — mehr braucht niemand von einer Antwort. */
    internal data class Paar(val code: Int, val text: String)

    /**
     * Die Verbindung öffnen und, falls ein Zertifikat festgenagelt ist,
     * die Prüfung dafür einhängen.
     *
     * Zur Namensprüfung: ist ein Fingerabdruck hinterlegt, wird sie
     * übergangen — und das ist hier richtig herum gedacht. Ein Heimserver
     * heisst „nas" und ist von unterwegs unter der Adresse des Vermittlers
     * erreichbar; sein Zertifikat kann diesen Namen gar nicht enthalten.
     * Der Fingerabdruck beweist die Gegenstelle genauer, als ein Name es
     * je könnte: er gilt für genau dieses eine Zertifikat. Beides zusammen
     * wäre strenger als nötig und würde die Verbindung grundlos verhindern.
     *
     * Ohne Fingerabdruck bleibt alles beim Üblichen — Prüfung gegen die
     * Wurzelzertifikate des Geräts samt Namensprüfung.
     */
    private fun oeffnen(adresse: String): HttpURLConnection {
        // URI statt URL: der URL-Erbauer gilt als überholt, weil er
        // nicht prüft, was man ihm gibt.
        val c = URI(adresse).toURL().openConnection() as HttpURLConnection
        val pruefer = gepinnt
        if (pruefer != null && c is HttpsURLConnection) {
            c.sslSocketFactory = pruefer.fabrik
            c.hostnameVerifier = pruefer.namenspruefung
        }
        return c
    }

    /** Aus einer TLS-Meldung etwas machen, mit dem jemand etwas anfangen
     *  kann. „PKIX path building failed" hilft niemandem weiter. */
    private fun zertifikatsHinweis(e: Throwable): String {
        val text = generateSequence(e) { it.cause }.mapNotNull { it.message }.joinToString(" ")
        return when {
            generateSequence(e) { it.cause }.any { it is AnderesZertifikat } -> Texte.t(Tx.ZERTIFIKAT_ANDERS)
            else -> Texte.t(Tx.ZERTIFIKAT_UNBEKANNT)
        }
    }

    /** Der Server schreibt seine Meldungen für Menschen — unverändert zeigen. */
    private fun messageOf(body: String): String =
        try {
            Json.parse(body)["error"].text ?: Texte.t(Tx.FEHLER_ALLGEMEIN)
        } catch (e: JsonException) {
            Texte.t(Tx.FEHLER_ALLGEMEIN)
        }

    private fun getJson(path: String, query: Map<String, String> = emptyMap()): JsonValue =
        parseOrThrow(send("GET", path, query), path)

    private fun parseOrThrow(body: String, path: String): JsonValue =
        try {
            Json.parse(body)
        } catch (e: JsonException) {
            throw LumenException.Mismatch(path, e)
        }

    // ---------------------------------------------------------- Vor der Anmeldung

    fun hello(): Hello = Hello.from(getJson("/hello"))

    /**
     * Die Profilauswahl. Von unterwegs gibt es keine — der Server antwortet
     * 403 mit dem Code „unterwegs", und das wird hier zu
     * [LumenException.Unterwegs]: die App zeigt dann „Von unterwegs
     * anmelden" mit Name und PIN.
     */
    fun profiles(): List<ProfileCard> = getJson("/profiles").list.map { ProfileCard.from(it) }

    fun setUpOwner(name: String, password: String, deviceName: String): Account {
        val body = JsonWriter.obj(
            "name" to name, "password" to password,
            "deviceName" to deviceName, "deviceKind" to DeviceProfile.current.kind)
        val j = parseOrThrow(send("POST", "/setup", body = body), "/setup")
        token = j.requiredText("token")
        return Account.from(j.required("account"))
    }

    fun unlock(profileId: String, secret: String, deviceName: String): Account {
        val body = JsonWriter.obj(
            "profileId" to profileId, "secret" to secret,
            "deviceName" to deviceName, "deviceKind" to DeviceProfile.current.kind)
        val j = parseOrThrow(send("POST", "/unlock", body = body), "/unlock")
        token = j.requiredText("token")
        return Account.from(j.required("account"))
    }

    /**
     * Von unterwegs anmelden: mit dem Namen des Profils statt seiner
     * Kennung. Eine Profilauswahl gibt es von dort nicht — sie verriete die
     * Namen im Haushalt —, also tippt man den Namen ein. Nur Profile mit PIN
     * oder Passwort lassen sich so öffnen; nach zu vielen Fehlversuchen
     * antwortet der Server 429 mit einer Meldung, die gezeigt werden soll.
     */
    fun unlockByName(name: String, secret: String, deviceName: String): Account {
        val body = JsonWriter.obj(
            "name" to name.trim(), "secret" to secret,
            "deviceName" to deviceName, "deviceKind" to DeviceProfile.current.kind)
        val j = parseOrThrow(send("POST", "/unlock", body = body), "/unlock")
        token = j.requiredText("token")
        return Account.from(j.required("account"))
    }

    fun redeemInvite(code: String, name: String, lockKind: String, secret: String): Account {
        val body = JsonWriter.obj(
            "code" to code, "name" to name, "lockKind" to lockKind, "secret" to secret)
        val j = parseOrThrow(send("POST", "/invite/redeem", body = body), "/invite/redeem")
        token = j.requiredText("token")
        return Account.from(j.required("account"))
    }

    fun signOut() { token = null }

    // ---------------------------------------------------------- Anmelden mit Code

    /**
     * Einen Code anfragen, den jemand an einem anderen Gerät freigibt.
     * Ohne Anmeldung. Siehe [AnmeldeCode].
     */
    fun codeStarten(deviceName: String = DeviceProfile.current.name): AnmeldeCode {
        val body = JsonWriter.obj(
            "deviceName" to deviceName, "deviceKind" to DeviceProfile.current.kind)
        return AnmeldeCode.from(parseOrThrow(send("POST", "/code/start", body = body), "/code/start"))
    }

    /**
     * Nachfragen, ob der Code freigegeben ist. `null` heisst: noch nicht
     * (der Server antwortet 202). Ist er freigegeben, hält der Client ab
     * jetzt das neue Token, und zurück kommt das Konto. Abgelaufen gibt
     * [LumenException.Server] mit Status 410.
     */
    fun codeAbfragen(geheim: String): Account? {
        val j = parseOrThrow(send("GET", "/code/${enc(geheim)}"), "/code")
        val neu = j["token"].text ?: return null
        token = neu
        return Account.from(j.required("account"))
    }

    /**
     * Einen Code freigeben, der an einem anderen Gerät steht — für das
     * eigene Profil. Zurück kommt der Name des Geräts.
     */
    fun codeFreigeben(code: String): String {
        val body = JsonWriter.obj("code" to Anmeldecode.saubern(code))
        val j = parseOrThrow(send("POST", "/code/approve", body = body), "/code/approve")
        return j["geraet"].text ?: ""
    }

    // ---------------------------------------------------------- Ich

    fun me(): Me = Me.from(getJson("/me"))

    fun updatePrefs(prefs: Map<String, String>): Me {
        val body = JsonWriter.obj("prefs" to prefs)
        return Me.from(parseOrThrow(send("PATCH", "/me", body = body), "/me"))
    }

    /** Das Profilbild auf eine Farbe („av-b") oder eine Figur („fig-eule") setzen. */
    fun setAvatar(avatar: String): Me {
        val body = JsonWriter.obj("avatar" to avatar)
        return Me.from(parseOrThrow(send("PATCH", "/me", body = body), "/me"))
    }

    /**
     * Ein eigenes Bild als Profilbild hochladen — roh, wie es ist. Der
     * Server schneidet die Mitte quadratisch aus und verkleinert auf 256
     * Pixel; JPEG, PNG oder GIF, höchstens 16 MB.
     */
    fun uploadAvatar(bild: ByteArray, typ: String): Me =
        Me.from(parseOrThrow(send("POST", "/me/avatar", roh = bild, typ = typ), "/me/avatar"))

    /**
     * Die Adresse eines eigenen Profilbilds. Zu Hause zeigt die Profilauswahl
     * es, bevor jemand angemeldet ist — dann ohne Schlüssel. Angemeldet kommt
     * der Medienschlüssel dazu: von unterwegs gibt der Server Profilbilder
     * nur damit heraus. [stand] hängt als ?v= dran, damit nach dem Hochladen
     * kein altes Bild aus dem Zwischenspeicher kommt.
     */
    fun avatarUrl(accountId: String, stand: Long = 0): String {
        val q = buildMap {
            if (stand > 0) put("v", stand.toString())
            if (isSignedIn) bildSchluessel()?.let { put("key", it) }
        }
        return url("/avatar/$accountId", q)
    }

    /** Das Bild zu einem Profil, falls es ein eigenes hat — sonst null. */
    fun profilbildUrl(accountId: String?, avatar: String?, stand: Long = 0): String? =
        if (Profilbild.istBild(avatar) && !accountId.isNullOrBlank()) avatarUrl(accountId, stand) else null

    /**
     * Den eigenen Schutz ändern: PIN, Passwort oder keinen. [alt] ist, was
     * das Profil bisher öffnet (leer, wenn es offen war). Der Server
     * antwortet 403 „falsch", wenn [alt] nicht stimmt, und 409
     * „verwaltung_braucht_sperre", wenn ein Profil mit Vollzugriff den
     * Schutz ganz entfernen will. Gibt es die Schnittstelle (noch) nicht,
     * kommt 404 — die Oberfläche blendet den Punkt dann aus.
     */
    fun sperreAendern(alt: String, art: String, neu: String) {
        send("PUT", "/me/sperre", body = JsonWriter.obj("alt" to alt, "art" to art, "neu" to neu))
    }

    // ---------------------------------------------------------- Neuigkeiten

    /** Die eigenen Neuigkeiten — nur, was dieses Konto selbst betrifft. */
    fun news(): Neuigkeiten = Neuigkeiten.from(getJson("/news"))

    /** Alles bis jetzt gilt als gelesen. */
    fun newsSeen() { send("POST", "/news/seen") }

    fun clearHistory() { send("DELETE", "/me/progress") }

    /** Die Geräte, auf denen dieses Konto angemeldet ist — nur die eigenen. */
    fun devices(): List<Device> = getJson("/me/devices").list.map { Device.from(it) }

    fun signOutEverywhere() { send("POST", "/me/sign-out-everywhere") }

    /**
     * Was gerade im eigenen Profil läuft, auf welchem Gerät — damit man einen
     * vergessenen Fernseher selbst beenden kann. Nur die eigenen.
     */
    fun meineWiedergaben(): List<EigeneWiedergabe> =
        getJson("/me/playbacks").list.map { EigeneWiedergabe.from(it) }

    /** Eine eigene Wiedergabe beenden. Läuft sie schon nicht mehr: 404. */
    fun wiedergabeBeenden(id: String) { send("DELETE", "/me/playbacks/${enc(id)}") }

    /**
     * Genau dieses Gerät beim Server abmelden: sein Token gilt danach
     * nicht mehr. Die übrigen Geräte des Kontos bleiben angemeldet.
     */
    fun geraetAbmelden() { send("DELETE", "/me/device") }

    // ---------------------------------------------------------- Mediathek

    fun home(): List<HomeRow> = getJson("/home")["rows"].list.map { HomeRow.from(it) }

    fun items(
        kind: String? = null,
        search: String? = null,
        genre: String? = null,
        sort: String? = null,
        limit: Int = 120,
        // Alles mit einer Person aus der Besetzung — die Seite hinter dem
        // Porträt in der Suche.
        person: String? = null,
    ): List<Item> {
        val q = buildMap {
            put("limit", limit.toString())
            kind?.let { put("kind", it) }
            search?.takeIf { it.isNotBlank() }?.let { put("q", it) }
            genre?.let { put("genre", it) }
            sort?.let { put("sort", it) }
            person?.takeIf { it.isNotBlank() }?.let { put("person", it) }
        }
        return getJson("/items", q).list.map { Item.from(it) }
    }

    /**
     * Eine Seite eines Rasters: Filme oder Serien, wahlweise nur ein Genre,
     * ab [offset]. Dazu die Gesamtzahl, ob es weitergeht, und die Genres
     * für die Filterknöpfe — genau das, was der Browser für sein Raster holt.
     */
    fun seite(
        kind: String,
        genre: String? = null,
        sort: String = "title",
        limit: Int = 60,
        offset: Int = 0,
        // Nur Titel mit diesem Anfangsbuchstaben — „#“ für Zahlen und
        // Zeichen. Der Server kennt es nur zusammen mit A–Z.
        buchstabe: String? = null,
        // asc oder desc; null: die übliche Richtung der Sortierung.
        dir: String? = null,
        // Bei sort=random der Samen aus der ersten Seite — das Nachladen
        // setzt dieselbe Mischung fort.
        seed: Long? = null,
    ): Seite {
        val q = buildMap {
            put("seite", "1")
            put("kind", kind)
            put("sort", sort)
            dir?.let { put("dir", it) }
            seed?.takeIf { it > 0 }?.let { put("seed", it.toString()) }
            put("limit", limit.toString())
            put("offset", offset.toString())
            genre?.takeIf { it.isNotBlank() }?.let { put("genre", it) }
            buchstabe?.takeIf { it.isNotBlank() }?.let { put("buchstabe", it) }
        }
        return Seite.from(getJson("/items", q))
    }

    /**
     * Eine Seite der eigenen Merkliste oder Favoriten — [liste] ist
     * „merkliste" oder „favoriten". Der Server filtert und ordnet (zuletzt
     * Gemerktes zuerst). Vorher holte die App die ersten paar hundert Titel
     * und siebte selbst; wer mehr hatte, sah gemerkte Titel weiter hinten nie.
     */
    fun liste(
        liste: String,
        kind: String? = null,
        limit: Int = 200,
        offset: Int = 0,
    ): Seite {
        val q = buildMap {
            put("seite", "1")
            put("liste", liste)
            put("limit", limit.toString())
            put("offset", offset.toString())
            kind?.takeIf { it.isNotBlank() }?.let { put("kind", it) }
        }
        return Seite.from(getJson("/items", q))
    }

    /**
     * Die ganze Liste, Seite um Seite — höchstens [hoechstens] Titel, damit
     * eine verirrte Antwort mit „weiter" nicht endlos fragt.
     */
    fun listeGanz(liste: String, kind: String? = null, hoechstens: Int = 2000): Seite {
        var s = liste(liste, kind)
        while (s.weiter && s.items.size < hoechstens) {
            val n = liste(liste, kind, offset = s.naechsterOffset)
            if (n.items.isEmpty()) break
            s = s.mit(n)
        }
        return s
    }

    /**
     * Wie viele Titel unter jedem Anfangsbuchstaben stehen — für die
     * Buchstabenleiste über dem Raster: {"A":12, …}, die Raute für Zahlen und Zeichen.
     */
    fun buchstaben(kind: String, genre: String? = null): Buchstaben {
        val q = buildMap {
            put("kind", kind)
            genre?.takeIf { it.isNotBlank() }?.let { put("genre", it) }
        }
        return Buchstaben.from(getJson("/items/letters", q))
    }

    /**
     * Die Angaben zu einer Person. Null, wenn der Server sie
     * nicht kennt (404) — dann zeigt die Seite nur die Titel. Andere Fehler
     * gehen weiter wie überall.
     */
    fun person(name: String): PersonInfo? =
        try {
            PersonInfo.from(getJson("/persons/${pfadTeil(name)}"))
        } catch (e: LumenException.Server) {
            if (e.status == 404) null else throw e
        }

    /**
     * Die Suche: Titel, Personen, Genres und wie der Server die Eingabe
     * gedeutet hat. Mit leerer Eingabe kommt ein leeres Ergebnis zurück.
     */
    fun search(q: String, limit: Int = 60): Suchergebnis =
        Suchergebnis.from(getJson("/search", mapOf("q" to q.trim(), "limit" to limit.toString())))

    fun item(id: String): ItemDetail = ItemDetail.from(getJson("/items/$id"))

    fun children(id: String): List<Item> =
        getJson("/items/$id/children").list.map { Item.from(it) }

    // ---------------------------------------------------------- Filmreihen

    fun collections(): List<Reihe> = getJson("/collections").list.map { Reihe.from(it) }

    fun collection(id: String): Reihe = Reihe.from(getJson("/collections/$id"))

    // ---------------------------------------------------------- Listen

    /**
     * Alle Listen, die diese Person sieht: die eigenen und die geteilten.
     * Mit [itemId] steht bei jeder dabei, ob der Titel schon drin ist —
     * das braucht „Zur Liste".
     */
    fun playlists(itemId: String? = null): List<Liste> =
        getJson("/playlists", if (itemId != null) mapOf("item" to itemId) else emptyMap())
            .list.map { Liste.from(it) }

    fun playlist(id: String): Liste = Liste.from(getJson("/playlists/$id"))

    fun createPlaylist(name: String): Liste =
        Liste.from(parseOrThrow(
            send("POST", "/playlists", body = JsonWriter.obj("name" to name)),
            "/playlists"))

    /**
     * Name und Freigabe ändern; nur, was nicht null ist, geht mit. Darf
     * nur, wer die Liste angelegt hat. POST statt PATCH — siehe [send].
     */
    fun updatePlaylist(
        id: String,
        name: String? = null,
        geteilt: Boolean? = null,
        gemeinsam: Boolean? = null,
    ) {
        val felder = buildList<Pair<String, Any?>> {
            name?.let { add("name" to it) }
            geteilt?.let { add("geteilt" to it) }
            gemeinsam?.let { add("gemeinsam" to it) }
        }
        send("POST", "/playlists/$id", body = JsonWriter.obj(*felder.toTypedArray()))
    }

    fun deletePlaylist(id: String) { send("DELETE", "/playlists/$id") }

    fun addToPlaylist(id: String, itemId: String) {
        send("POST", "/playlists/$id/items", body = JsonWriter.obj("itemId" to itemId))
    }

    fun removeFromPlaylist(id: String, itemId: String) {
        send("DELETE", "/playlists/$id/items/$itemId")
    }

    fun orderPlaylist(id: String, itemIds: List<String>) {
        send("POST", "/playlists/$id/order", body = JsonWriter.obj("itemIds" to itemIds))
    }

    // ---------------------------------------------------------- Mein Stand

    fun saveProgress(itemId: String, positionSeconds: Double, durationSeconds: Double) {
        send("POST", "/items/$itemId/progress", body = JsonWriter.obj(
            "positionMs" to (positionSeconds * 1000).toLong(),
            "durationMs" to (durationSeconds * 1000).toLong()))
    }

    /**
     * Als gesehen markieren. Bei einem Film oder einer Folge genau diese;
     * bei einer Serie alle Folgen — mit [staffel] nur die dieser Staffel.
     */
    fun markFinished(id: String, staffel: Int? = null) {
        send("POST", "/items/$id/finished", query = staffelAbfrage(staffel))
    }

    /** Den Haken wieder wegnehmen — wie [markFinished], nur umgekehrt. */
    fun clearFinished(id: String, staffel: Int? = null) {
        send("DELETE", "/items/$id/finished", query = staffelAbfrage(staffel))
    }

    private fun staffelAbfrage(staffel: Int?): Map<String, String> =
        if (staffel != null) mapOf("staffel" to staffel.toString()) else emptyMap()

    fun rate(id: String, stars: Int, shared: Boolean) {
        send("PUT", "/items/$id/rating",
            body = JsonWriter.obj("stars" to stars, "shared" to shared))
    }

    fun addToWatchlist(id: String) { send("PUT", "/watchlist/$id") }

    fun removeFromWatchlist(id: String) { send("DELETE", "/watchlist/$id") }

    /** Das Herz: nur für das eigene Profil, wie die Merkliste. */
    fun addFavorite(id: String) { send("PUT", "/favorites/$id") }

    fun removeFavorite(id: String) { send("DELETE", "/favorites/$id") }

    // ---------------------------------------------------------- Wünsche

    fun wishes(): List<Wish> = getJson("/wishes").list.map { Wish.from(it) }

    fun wish(title: String, year: Int = 0): Wish =
        Wish.from(parseOrThrow(
            send("POST", "/wishes", body = JsonWriter.obj("title" to title, "year" to year)),
            "/wishes"))

    // ---------------------------------------------------------- Wiedergabe

    /**
     * Nur der Plan. Wer gleich abspielt, nimmt [wiedergabeVorbereiten] —
     * das holt dazu den frischen Schlüssel.
     */
    fun plan(fileId: String, profil: DeviceProfile = DeviceProfile.current): PlaybackPlan =
        PlaybackPlan.from(getJson("/stream/$fileId/plan", profil.query())["plan"])

    /** Der Schlüssel für Strom und Untertitel: der eigene der Wiedergabe, sonst der fürs Bild. */
    private fun stromSchluessel(): String? =
        if (!isSignedIn) null else wiedergabeSchluessel ?: bildSchluessel()

    /**
     * Die Adresse für den Abspieler.
     *
     * Der Medienschlüssel hängt als Abfrageparameter dran: ExoPlayer lädt
     * HLS-Segmente selbst nach, und eigene Kopfzeilen dafür durchzureichen
     * ist umständlich. Das Token gehört nie in eine Adresse — siehe
     * [medienSchluessel].
     */
    fun streamUrl(file: MediaFile, plan: PlaybackPlan, spur: Int = -1,
                  profil: DeviceProfile = DeviceProfile.current): String {
        val q = profil.query().toMutableMap()
        stromSchluessel()?.let { q["key"] = it }
        // Die gewünschte Tonspur: beim Umpacken mappt der Server genau sie.
        // Läuft die Datei direkt, kommen alle Spuren mit, und der Abspieler
        // wählt selbst — der Parameter stört dann nicht.
        if (spur >= 0) q["spur"] = spur.toString()
        return if (istDirekt(file, plan, profil)) {
            url("/stream/${file.id}", q)
        } else {
            // HLS kann ExoPlayer von Haus aus, samt Springen.
            url("/stream/${file.id}/hls/master.m3u8", q)
        }
    }

    /** Ob die Datei unverändert läuft — dann stecken alle Tonspuren im Strom. */
    fun istDirekt(file: MediaFile, plan: PlaybackPlan, profil: DeviceProfile = DeviceProfile.current): Boolean =
        plan.direct && DeviceProfile.playsDirectly(file.container) &&
            // Ein Gerät, das den Behälter gar nicht kennt (der Chromecast und
            // MKV), bekommt HLS — auch wenn der Inhalt passen würde.
            (profil === DeviceProfile.current || file.container?.lowercase().let { c ->
                c != null && (c in profil.containers || (c == "m4v" && "mp4" in profil.containers))
            })

    /**
     * Die Adresse einer Untertitelspur als WebVTT — zum seitlichen Laden in
     * den Abspieler. Der Schlüssel hängt dran wie beim Strom: der Abspieler
     * holt die Datei selbst.
     */
    fun subtitleUrl(fileId: String, index: Int): String {
        val q = buildMap { stromSchluessel()?.let { put("key", it) } }
        return url("/stream/$fileId/subtitles/$index.vtt", q)
    }

    /**
     * Ob eine Untertitelspur fertig ist. Eingebettete Spuren zieht der
     * Server erst aus der Datei — das kann bei grossen Dateien Minuten
     * dauern. Solange antwortet er 202 (mit [kurz]: 503) und dem Code
     * „wird_vorbereitet". Ein Abspieler, der die Spur so bekäme, hielte
     * das JSON für einen Untertitel oder wartete beim Start bis zu 20
     * Sekunden; darum fragt die App erst hier und gibt die Spur danach weiter.
     *
     * [kurz]: nur eine Sekunde warten lassen — für die Frage vor dem Start.
     * Sonst wartet der Server bis zu 20 Sekunden, ehe er „noch nicht" sagt.
     * Wahr heisst fertig, falsch heisst später noch einmal fragen. Andere
     * Fehler (die Spur gibt es nicht, lässt sich nicht lesen) gehen weiter.
     */
    fun untertitelBereit(fileId: String, index: Int, kurz: Boolean = false): Boolean {
        val q = if (kurz) mapOf("warten" to "1") else emptyMap()
        return try {
            senden("GET", "/stream/$fileId/subtitles/$index.vtt", q).code != 202
        } catch (e: LumenException.Server) {
            if (e.status == 503 && e.code == "wird_vorbereitet") false else throw e
        }
    }

    /**
     * Eine HLS-Sitzung beim Server beenden — beim Schliessen des Players.
     * Sonst rechnet der Server noch zwei Minuten weiter, und die Wiedergabe
     * zählt so lange bei „auf wie vielen Geräten" mit. Die Kennung steht in
     * der Adresse der Medienliste, siehe [HlsSitzung.kennung].
     */
    fun hlsBeenden(fileId: String, sitzung: String) {
        send("DELETE", "/stream/$fileId/hls/$sitzung")
    }

    // ---------------------------------------------------------- Herunterladen

    /**
     * Ob und was sich zu einem Titel herunterladen lässt. Null, wenn der
     * Server das nicht kennt (404) — dann gibt es keinen Knopf.
     */
    fun downloadInfo(itemId: String): DownloadInfo? =
        try {
            DownloadInfo.from(getJson("/items/$itemId/download/info"))
        } catch (e: LumenException.Server) {
            if (e.status == 404) null else throw e
        }

    /**
     * Eine Datei herunterladen — nach [ziel], ab dem, was dort schon liegt
     * (Range). Die Anmeldung reist wie bei jedem Strom als Medienschlüssel
     * in der Adresse; vorher wird ein frischer geholt, denn ein grosser
     * Film braucht seine Zeit. [fortschritt] bekommt Geladenes und Gesamt
     * (Gesamt 0, wenn der Server es nicht nennt); gibt [weiter] false
     * zurück, wird abgebrochen. Blockiert.
     *
     * 403 „kein_download" und 429 „zu_viele_downloads" kommen als
     * [LumenException.Server] mit diesem Code.
     */
    fun herunterladen(
        fileId: String,
        ziel: java.io.File,
        fortschritt: (geladen: Long, gesamt: Long) -> Unit = { _, _ -> },
        weiter: () -> Boolean = { true },
    ) {
        wiedergabeSchluesselErneuern()
        val q = buildMap { stromSchluessel()?.let { put("key", it) } }
        val schon = if (ziel.exists()) ziel.length() else 0L
        val c = try {
            oeffnen(url("/stream/$fileId/download", q))
        } catch (e: IOException) {
            throw LumenException.Unreachable(e)
        }
        try {
            c.connectTimeout = VERBINDUNGSFRIST
            c.readTimeout = ANTWORTFRIST
            if (schon > 0) c.setRequestProperty("Range", "bytes=$schon-")
            val code = c.responseCode
            if (code >= 400) {
                val text = c.errorStream?.bufferedReader(Charsets.UTF_8)?.use { it.readText() } ?: ""
                throw LumenException.Server(messageOf(text), code, codeOf(text))
            }
            // 206: der Server setzt fort. 200: er schickt alles — dann von vorn.
            val anhaengen = code == 206 && schon > 0
            val gesamt = Herunterladen.gesamtGroesse(
                c.getHeaderField("Content-Range"), c.contentLengthLong, if (anhaengen) schon else 0L)
            var geladen = if (anhaengen) schon else 0L
            c.inputStream.use { ein ->
                java.io.FileOutputStream(ziel, anhaengen).use { aus ->
                    val puffer = ByteArray(64 * 1024)
                    var zuletzt = 0L
                    while (true) {
                        if (!weiter()) return
                        val n = ein.read(puffer)
                        if (n < 0) break
                        aus.write(puffer, 0, n)
                        geladen += n
                        // Nicht bei jedem Puffer melden — die Benachrichtigung
                        // käme sonst nicht hinterher.
                        if (geladen - zuletzt >= 1_000_000) { fortschritt(geladen, gesamt); zuletzt = geladen }
                    }
                }
            }
            fortschritt(geladen, gesamt)
        } catch (e: javax.net.ssl.SSLException) {
            throw LumenException.Zertifikat(zertifikatsHinweis(e), e)
        } catch (e: IOException) {
            throw LumenException.Unreachable(e)
        } finally {
            c.disconnect()
        }
    }

    // ---------------------------------------------------------- Trickplay

    /**
     * Die Vorschaubilder fürs Spulen. [TrickplayStand.Bereit] mit den
     * Angaben, [TrickplayStand.WirdErstellt] bei 202 (später noch einmal
     * fragen), [TrickplayStand.Keins] bei 404 — dann gibt es eben keine.
     */
    fun trickplay(fileId: String): TrickplayStand {
        val antwort = try {
            senden("GET", "/stream/$fileId/trickplay")
        } catch (e: LumenException.Server) {
            if (e.status == 404) return TrickplayStand.Keins else throw e
        }
        if (antwort.code == 202) return TrickplayStand.WirdErstellt
        val t = Trickplay.from(parseOrThrow(antwort.text, "/stream/$fileId/trickplay"))
        if (!t.brauchbar) return TrickplayStand.Keins
        // Die Blätter lädt der Bildlader über eine Adresse — dafür muss ein
        // Schlüssel da sein, bevor die erste gebaut wird.
        if (stromSchluessel() == null) medienSchluesselSichern()
        return TrickplayStand.Bereit(t)
    }

    /**
     * Die Adresse eines Blatts, mit Medienschlüssel — der Server nennt sie
     * als Pfad („/api/v1/stream/…/trickplay/0.jpg") oder ganz.
     */
    fun trickplayBlattUrl(blatt: String): String? {
        if (!isSignedIn) return null
        val k = stromSchluessel()
        val voll = if (blatt.startsWith("http://") || blatt.startsWith("https://")) blatt
                   else baseUrl + (if (blatt.startsWith("/")) blatt else "/$blatt")
        if (k == null) return voll
        val trenner = if ('?' in voll) '&' else '?'
        return "$voll${trenner}key=${enc(k)}"
    }

    /**
     * Ein Bild zum Titel. Ohne Anmeldung keines. Ist (noch) kein Schlüssel
     * da, kommt die Adresse ohne — der Server antwortet dann 401, und der
     * Bildlader holt über [nachAbgelaufenemSchluessel] einen und versucht
     * es noch einmal.
     */
    fun artworkUrl(itemId: String, kind: String = "poster"): String? {
        if (!isSignedIn) return null
        val q = buildMap { bildSchluessel()?.let { put("key", it) } }
        return url("/art/$itemId/$kind", q)
    }

    /**
     * Die bewegte Vorschau: zwölf stumme Sekunden aus dem Film als MP4. Der
     * Server schneidet sie beim ersten Abruf; ohne FFmpeg gibt es keine.
     */
    fun vorschauUrl(itemId: String): String? = artworkUrl(itemId, "vorschau")

    /** Ein Standbild aus dem Titel — für „Als Nächstes". */
    fun frameUrl(itemId: String): String? = artworkUrl(itemId, "frame")

    /**
     * Eine Bildadresse, wie der Server sie nennt („/api/v1/art/it_1/cast00"),
     * vollständig und mit Medienschlüssel — etwa das Porträt einer Person
     * in der Suche.
     */
    fun bildUrl(pfad: String?): String? {
        if (!isSignedIn) return null
        if (pfad.isNullOrBlank()) return null
        val k = bildSchluessel() ?: return "$baseUrl$pfad"
        val trenner = if ('?' in pfad) '&' else '?'
        return "$baseUrl$pfad${trenner}key=${enc(k)}"
    }

    /** Das Foto einer Person — mit Medienschlüssel, nie mit dem Token. */
    fun personBildUrl(person: PersonInfo): String? = bildUrl(person.bild)

    /**
     * Das Bild einer Filmreihe: ihr eigenes Poster (oder mit [breit] ihr
     * Hintergrund), sonst das des ersten Films. Null, wenn es keines gibt.
     */
    fun reiheBildUrl(reihe: Reihe, breit: Boolean = false): String? {
        val eigenes = if (breit) reihe.backdrop else reihe.poster
        if (eigenes != null) return bildUrl(eigenes)
        val erster = reihe.items.firstOrNull() ?: return null
        return if (breit) wideUrl(erster) else posterUrl(erster)
    }

    /** Das Bild hochkant: Poster, sonst ein Standbild aus dem Film. */
    fun posterUrl(item: Item): String? = artworkUrl(item.id, item.posterKind)

    /** Das Bild in die Breite: Hintergrund, Standbild, zur Not das Poster. */
    fun wideUrl(item: Item): String? = artworkUrl(item.id, item.wideKind)
}

// ---------------------------------------------------------------- Fehler

/**
 * Eine Abfrage, deren Ausbleiben nicht schlimm ist („Beiwerk") — null,
 * wenn der Server nicht will oder nicht da ist.
 *
 * Bewusst nicht `runCatching`: das fängt *alles*, auch den Abbruch einer
 * Koroutine (CancellationException) und echte Programmfehler. Ein
 * verschluckter Abbruch lässt eine längst verlassene Ansicht weiterlaufen;
 * ein verschluckter Programmfehler fällt nie auf. Hier gilt nur, was der
 * Server sagt oder dass er fehlt.
 */
inline fun <T> oderNull(block: () -> T): T? =
    try { block() } catch (e: LumenException) { null }

sealed class LumenException(message: String, cause: Throwable? = null) :
    RuntimeException(message, cause) {

    class Unreachable(cause: Throwable) : LumenException(
        Texte.t(Tx.SERVER_NICHT_ERREICHBAR), cause)

    data object SignedOut : LumenException(Tx.ANMELDUNG_UNGUELTIG.vorgabe) {
        // Ein Objekt entsteht einmal; die Meldung folgt trotzdem der
        // Sprache, die gerade gilt.
        override val message: String get() = Texte.t(Tx.ANMELDUNG_UNGUELTIG)

        private fun readResolve(): Any = SignedOut
    }

    /**
     * Der Server hat abgelehnt. [code] ist sein maschinenlesbarer Grund
     * („falsch", „kein_download" …), falls er einen nennt — daran hängt
     * die App ihre eigenen, übersetzbaren Sätze.
     */
    open class Server(message: String, val status: Int, val code: String? = null) : LumenException(message)

    /**
     * 403 „unterwegs": Der Server zeigt von aussen keine Profilauswahl. Kein
     * Fehler im eigentlichen Sinn — die App bietet dann die Anmeldung mit
     * Profilname und PIN oder Passwort an (und den Code vom anderen Gerät).
     * Eine [Server]-Ausnahme mit Status 403, damit bisherige Behandlungen
     * weiter greifen.
     */
    class Unterwegs(message: String) : Server(message, 403)

    /** Das Zertifikat des Servers passt nicht zum gespeicherten — oder es
     *  ist eines, dem dieses Gerät nicht von sich aus traut. Beides braucht
     *  eine andere Antwort als „nicht erreichbar": hier hilft kein Warten,
     *  sondern der Fingerabdruck aus der Verwaltung. */
    class Zertifikat(message: String, cause: Throwable? = null) : LumenException(message, cause)

    class Mismatch(path: String, cause: Throwable) : LumenException(
        Texte.t(Tx.ANTWORT_UNLESBAR, path), cause)
}
