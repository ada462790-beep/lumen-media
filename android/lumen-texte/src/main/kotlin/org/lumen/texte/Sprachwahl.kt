// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package org.lumen.texte

import android.app.Activity
import android.app.LocaleManager
import android.content.Context
import android.content.res.Configuration
import android.content.res.Resources
import android.os.Build
import android.os.LocaleList
import org.lumen.core.Sprachen
import org.lumen.core.Texte
import java.util.Locale

/**
 * Welche Sprache die App spricht — und wie sie dazu kommt.
 *
 * Die Sprache gehört zum Profil auf dem Server, nicht zum Gerät: wer sich
 * anmeldet, bekommt seine (GET /me „sprache"), vorher gilt die Vorgabe des
 * Servers (GET /hello „sprache"). Schickt ein älterer Server nichts davon,
 * bleibt die zuletzt gültige Sprache — anfangs die des Geräts.
 *
 * Umgesetzt mit einer eigenen Konfiguration für jede Activity
 * ([kontext] in attachBaseContext) und für die Application ([ressourcen],
 * für Benachrichtigungen und lumen-core). Das geht auf jeder Android-
 * Fassung ab 8 und braucht kein AppCompat.
 *
 * Ab Android 13 kennt das System eine Sprache je App (Einstellungen › Apps
 * › Sprache, dafür res/xml/locales_config.xml). Die Wahl wird dorthin
 * gespiegelt, damit die Einstellungen dasselbe zeigen. Ändert jemand sie
 * dort, übernimmt die App das und schreibt es beim nächsten Abgleich ins
 * Profil ([offenerWunsch]) — sonst stellte der Server sie gleich zurück.
 *
 * Die Rückfallkette der Ressourcen: values-de-rCH (= values, Schweiz),
 * values-de (Deutschland), values-de-rAT (nur was in Österreich anders
 * heisst), values-en (USA), values-en-rGB (nur was im Vereinigten
 * Königreich anders heisst); jede andere Sprache landet bei values, der
 * Quelle in Schweizer Hochdeutsch.
 */
object Sprachwahl {
    private const val DATEI = "lumen_sprache"
    private const val TAG = "tag"
    private const val GESPIEGELT = "gespiegelt"
    private const val WUNSCH = "wunsch"

    /**
     * Die Ressourcen der Application in der gewählten Sprache. Null, bevor
     * [einrichten] lief — dann übersetzt lumen-core mit seinen Vorgaben.
     */
    @Volatile var ressourcen: Resources? = null
        private set

    /** Beim Start der Application, vor allem anderen, das Text braucht. */
    fun einrichten(app: Context) {
        Texte.uebersetzer = KernUebersetzer
        auffrischen(app.applicationContext)
    }

    /** Nach einer Änderung der Gerätesprache (Application.onConfigurationChanged). */
    fun konfigurationGeaendert(app: Context) = auffrischen(app.applicationContext)

    /** Für Activity.attachBaseContext: der Kontext in der gewählten Sprache. */
    fun kontext(basis: Context): Context {
        systemWahlUebernehmen(basis)
        val tag = gespeichert(basis) ?: return basis
        val locale = Locale.forLanguageTag(tag)
        val konf = Configuration(basis.resources.configuration)
        konf.setLocales(LocaleList(locale))
        return basis.createConfigurationContext(konf)
    }

    /** Die gespeicherte Wahl („en-GB") — oder null: wie das Gerät. */
    fun gespeichert(ctx: Context): String? =
        Sprachen.normalisieren(prefs(ctx).getString(TAG, null))

    /**
     * Die Sprache, die tatsächlich gilt — auch ohne eigene Wahl. So, wie
     * Android die Ressourcen auswählt: ein Deutsch, das nicht Schweiz oder
     * Österreich ist, ist values-de; ein anderes Englisch values-en; alles
     * andere die Quelle in values.
     */
    fun wirksam(): String {
        val locale = ressourcen?.configuration?.locales?.get(0) ?: return Texte.VORGABE
        return Sprachen.normalisieren(locale.toLanguageTag()) ?: when (locale.language) {
            "de" -> "de-DE"
            "en" -> "en-US"
            else -> Texte.VORGABE
        }
    }

    /**
     * Den Wunsch aus den System-Einstellungen, der noch ins Profil muss:
     * eine Kennung, "" für „wie der Server" — oder null, wenn keiner offen ist.
     */
    fun offenerWunsch(ctx: Context): String? = prefs(ctx).getString(WUNSCH, null)

    fun wunschErledigt(ctx: Context) {
        prefs(ctx).edit().remove(WUNSCH).apply()
    }

    /**
     * Die App auf [sprache] stellen — die des Profils oder des Servers.
     * Ändert sich etwas, baut die Activity sich neu auf. Gibt zurück, ob
     * sich etwas geändert hat.
     *
     * Null oder eine Sprache, die die App nicht kann, ändert nichts: Beim
     * Start ist noch nichts vom Server da, und ein älterer Server schickt
     * nie etwas. Dann bleibt, was zuletzt galt — beim allerersten Start
     * die Sprache des Geräts. Sonst spränge die App bei jedem Start erst
     * auf die Gerätesprache und gleich wieder zurück.
     */
    fun anwenden(activity: Activity, sprache: String?): Boolean {
        val neu = Sprachen.normalisieren(sprache) ?: return false
        // Hat jemand in den System-Einstellungen gewählt, gilt das, bis es
        // im Profil steht — sonst spränge die Sprache zurück.
        if (offenerWunsch(activity) != null) return false
        if (neu == gespeichert(activity)) return false
        prefs(activity).edit().putString(TAG, neu).apply()
        auffrischen(activity.applicationContext)
        if (!spiegeln(activity, neu)) activity.recreate()
        return true
    }

    /**
     * Nach jeder Änderung an Anmeldung oder Profil aufrufen. Steht ein
     * Wunsch aus den System-Einstellungen offen und ist jemand angemeldet,
     * geht er mit [insProfil] an den Server (PATCH /me, Pref „sprache");
     * sonst gilt [sprache].
     */
    fun abgleichen(activity: Activity, sprache: String?, angemeldet: Boolean, insProfil: (String) -> Unit) {
        val wunsch = offenerWunsch(activity)
        if (wunsch != null) {
            if (angemeldet) {
                wunschErledigt(activity)
                insProfil(wunsch)
            }
            return
        }
        anwenden(activity, sprache)
    }

    /**
     * Was in den Einstellungen zur Wahl steht: zuerst „Wie der Server"
     * (leer), dann die Sprachen — jede in ihrer eigenen Sprache benannt.
     */
    fun wahl(ctx: Context, vomServer: List<org.lumen.core.Sprache>, vorgabe: String?): List<Pair<String, String>> {
        val res = ressourcen ?: ctx.resources
        val name = Sprachen.normalisieren(vorgabe)?.let { v ->
            vomServer.firstOrNull { it.id.equals(v, ignoreCase = true) }?.name ?: Sprachen.NAMEN[v]
        }
        val erste = if (name != null) res.getString(R.string.sprache_wie_server_mit, name)
                    else res.getString(R.string.sprache_wie_server)
        return listOf("" to erste) + Sprachen.wahl(vomServer).map { it.id to it.name }
    }

    /** Der Name der eigenen Wahl aus den Prefs, für die Zeile in den Einstellungen. */
    fun wahlText(ctx: Context, prefs: Map<String, String>, vomServer: List<org.lumen.core.Sprache>, vorgabe: String?): String {
        val eigene = prefs[Sprachen.PREF]?.trim().orEmpty()
        val alle = wahl(ctx, vomServer, vorgabe)
        return alle.firstOrNull { it.first.equals(eigene, ignoreCase = true) }?.second ?: alle.first().second
    }

    /** „Sprache" — die Überschrift der Einstellung, für beide Apps. */
    fun titel(ctx: Context): String = (ressourcen ?: ctx.resources).getString(R.string.sprache_titel)

    /** Der Satz unter der Einstellung. */
    fun hinweis(ctx: Context): String = (ressourcen ?: ctx.resources).getString(R.string.sprache_hinweis)

    // ------------------------------------------------------------ intern

    private fun prefs(ctx: Context) =
        ctx.applicationContext.getSharedPreferences(DATEI, Context.MODE_PRIVATE)

    private fun auffrischen(app: Context) {
        systemWahlUebernehmen(app)
        ressourcen = kontext(app).resources
        Texte.sprache = wirksam()
    }

    /**
     * Ab Android 13 die Wahl ins System schreiben. Das System baut die
     * Activity dann selbst neu auf — true heisst: das übernimmt es.
     */
    private fun spiegeln(ctx: Context, tag: String?): Boolean {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.TIRAMISU) return false
        val lm = ctx.getSystemService(LocaleManager::class.java) ?: return false
        val liste = if (tag == null) LocaleList.getEmptyLocaleList() else LocaleList.forLanguageTags(tag)
        val vorher = lm.applicationLocales.toLanguageTags()
        prefs(ctx).edit().putString(GESPIEGELT, liste.toLanguageTags()).apply()
        if (vorher == liste.toLanguageTags()) return false
        lm.applicationLocales = liste
        return true
    }

    /** Hat jemand die Sprache in den System-Einstellungen geändert? Dann gilt sie. */
    private fun systemWahlUebernehmen(ctx: Context) {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.TIRAMISU) return
        val lm = ctx.getSystemService(LocaleManager::class.java) ?: return
        val p = prefs(ctx)
        val system = lm.applicationLocales.toLanguageTags()
        val gespiegelt = p.getString(GESPIEGELT, null)
        if (gespiegelt == null) {
            // Erster Start mit dieser Fassung: nichts übernehmen, nur merken.
            p.edit().putString(GESPIEGELT, system).apply()
            return
        }
        if (system == gespiegelt) return
        val neu = Sprachen.normalisieren(system.substringBefore(','))
        p.edit().apply {
            if (neu == null) remove(TAG) else putString(TAG, neu)
            putString(GESPIEGELT, system)
            putString(WUNSCH, neu ?: "")
        }.apply()
    }
}
