// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package org.lumen.core

/**
 * Ein kleiner JSON-Leser.
 *
 * Warum selbst geschrieben statt eine Bibliothek: lumen-core soll ohne jede
 * Abhängigkeit auskommen. Dann lässt es sich hier übersetzen und testen,
 * ohne dass erst ein halbes Maven-Verzeichnis heruntergeladen werden muss —
 * und auf dem Gerät ist es ein paar Kilobyte statt ein paar hundert.
 *
 * Gelesen wird nur, nicht geschrieben: was die App zum Server schickt, sind
 * eine Handvoll flacher Objekte, die [JsonWriter] von Hand baut.
 *
 * Der Leser ist streng, wo Strenge hilft (ungültiges JSON gibt einen Fehler
 * mit Position), und nachsichtig, wo es nichts kostet: ein fehlendes Feld
 * ist null, kein Absturz.
 */

sealed interface JsonValue {
    data object Null : JsonValue
    data class Bool(val value: Boolean) : JsonValue
    data class Num(val value: Double) : JsonValue
    data class Text(val value: String) : JsonValue
    data class Arr(val items: List<JsonValue>) : JsonValue
    data class Obj(val fields: Map<String, JsonValue>) : JsonValue
}

class JsonException(message: String) : RuntimeException(message)

/**
 * Ein Teil der Antwort, der fehlen darf: passt er nicht (Pflichtfeld weg),
 * null statt eines Fehlers, der die ganze Antwort mitnähme. Fängt nur
 * [JsonException] — wie [oderNull] nur Fehler des Servers.
 */
inline fun <T> oderNullJson(block: () -> T): T? =
    try { block() } catch (e: JsonException) { null }

object Json {

    fun parse(text: String): JsonValue {
        val reader = Reader(text)
        reader.skipSpace()
        val value = reader.readValue()
        reader.skipSpace()
        if (!reader.atEnd) reader.fail("unerwartete Zeichen am Ende")
        return value
    }

    private class Reader(private val src: String) {
        private var pos = 0
        val atEnd get() = pos >= src.length

        fun fail(what: String): Nothing =
            throw JsonException("$what (Position $pos)")

        fun skipSpace() {
            while (pos < src.length && src[pos].isWhitespace()) pos++
        }

        fun readValue(): JsonValue {
            if (atEnd) fail("Wert erwartet")
            return when (val c = src[pos]) {
                '{' -> readObject()
                '[' -> readArray()
                '"' -> JsonValue.Text(readString())
                't' -> { expect("true"); JsonValue.Bool(true) }
                'f' -> { expect("false"); JsonValue.Bool(false) }
                'n' -> { expect("null"); JsonValue.Null }
                else ->
                    if (c == '-' || c in '0'..'9') readNumber()
                    else fail("unerwartetes Zeichen '$c'")
            }
        }

        private fun expect(word: String) {
            if (!src.startsWith(word, pos)) fail("'$word' erwartet")
            pos += word.length
        }

        private fun readObject(): JsonValue.Obj {
            pos++ // {
            val fields = LinkedHashMap<String, JsonValue>()
            skipSpace()
            if (!atEnd && src[pos] == '}') { pos++; return JsonValue.Obj(fields) }
            while (true) {
                skipSpace()
                if (atEnd || src[pos] != '"') fail("Feldname erwartet")
                val key = readString()
                skipSpace()
                if (atEnd || src[pos] != ':') fail("':' erwartet")
                pos++
                skipSpace()
                fields[key] = readValue()
                skipSpace()
                if (atEnd) fail("'}' erwartet")
                when (src[pos]) {
                    ',' -> pos++
                    '}' -> { pos++; return JsonValue.Obj(fields) }
                    else -> fail("',' oder '}' erwartet")
                }
            }
        }

        private fun readArray(): JsonValue.Arr {
            pos++ // [
            val items = ArrayList<JsonValue>()
            skipSpace()
            if (!atEnd && src[pos] == ']') { pos++; return JsonValue.Arr(items) }
            while (true) {
                skipSpace()
                items.add(readValue())
                skipSpace()
                if (atEnd) fail("']' erwartet")
                when (src[pos]) {
                    ',' -> pos++
                    ']' -> { pos++; return JsonValue.Arr(items) }
                    else -> fail("',' oder ']' erwartet")
                }
            }
        }

        private fun readString(): String {
            pos++ // "
            val out = StringBuilder()
            while (true) {
                if (atEnd) fail("Zeichenkette nicht beendet")
                when (val c = src[pos]) {
                    '"' -> { pos++; return out.toString() }
                    '\\' -> {
                        pos++
                        if (atEnd) fail("Fluchtzeichen nicht beendet")
                        when (val e = src[pos]) {
                            '"' -> out.append('"')
                            '\\' -> out.append('\\')
                            '/' -> out.append('/')
                            'b' -> out.append('\b')
                            'f' -> out.append('\u000C')
                            'n' -> out.append('\n')
                            'r' -> out.append('\r')
                            't' -> out.append('\t')
                            'u' -> {
                                if (pos + 4 >= src.length) fail("\\u braucht vier Ziffern")
                                val hex = src.substring(pos + 1, pos + 5)
                                val code = hex.toIntOrNull(16) ?: fail("ungültiges \\u$hex")
                                out.append(code.toChar())
                                pos += 4
                            }
                            else -> fail("unbekanntes Fluchtzeichen '\\$e'")
                        }
                        pos++
                    }
                    else -> { out.append(c); pos++ }
                }
            }
        }

        private fun readNumber(): JsonValue.Num {
            val start = pos
            if (src[pos] == '-') pos++
            while (pos < src.length && src[pos] in '0'..'9') pos++
            if (pos < src.length && src[pos] == '.') {
                pos++
                while (pos < src.length && src[pos] in '0'..'9') pos++
            }
            if (pos < src.length && (src[pos] == 'e' || src[pos] == 'E')) {
                pos++
                if (pos < src.length && (src[pos] == '+' || src[pos] == '-')) pos++
                while (pos < src.length && src[pos] in '0'..'9') pos++
            }
            val text = src.substring(start, pos)
            return JsonValue.Num(text.toDoubleOrNull() ?: fail("ungültige Zahl '$text'"))
        }
    }
}

// ---------------------------------------------------------------- Zugriff
//
// Ein fehlendes Feld ist null, kein Absturz: der Server lässt leere Felder
// weg, und jedes davon einzeln abzufangen wäre unlesbar.

operator fun JsonValue?.get(key: String): JsonValue? =
    (this as? JsonValue.Obj)?.fields?.get(key)?.takeIf { it != JsonValue.Null }

val JsonValue?.list: List<JsonValue>
    get() = (this as? JsonValue.Arr)?.items ?: emptyList()

val JsonValue?.text: String?
    get() = (this as? JsonValue.Text)?.value

val JsonValue?.int: Int?
    get() = (this as? JsonValue.Num)?.value?.toInt()

val JsonValue?.long: Long?
    get() = (this as? JsonValue.Num)?.value?.toLong()

val JsonValue?.double: Double?
    get() = (this as? JsonValue.Num)?.value

val JsonValue?.bool: Boolean?
    get() = (this as? JsonValue.Bool)?.value

/** Pflichtfeld: fehlt es, passen App und Server nicht zusammen. */
fun JsonValue?.required(key: String): JsonValue =
    this[key] ?: throw JsonException("Feld '$key' fehlt in der Antwort")

fun JsonValue?.requiredText(key: String): String =
    required(key).text ?: throw JsonException("Feld '$key' ist keine Zeichenkette")

fun JsonValue?.map(): Map<String, String> =
    (this as? JsonValue.Obj)?.fields
        ?.mapNotNull { (k, v) -> v.text?.let { k to it } }
        ?.toMap() ?: emptyMap()

fun JsonValue?.strings(): List<String> = list.mapNotNull { it.text }

// ---------------------------------------------------------------- Schreiben

/** Baut die wenigen flachen Objekte, die zum Server gehen. */
object JsonWriter {

    fun obj(vararg pairs: Pair<String, Any?>): String =
        pairs.joinToString(",", "{", "}") { (k, v) -> "${quote(k)}:${value(v)}" }

    private fun value(v: Any?): String = when (v) {
        null -> "null"
        is String -> quote(v)
        is Boolean -> v.toString()
        is Int, is Long -> v.toString()
        is Double -> if (v == v.toLong().toDouble()) v.toLong().toString() else v.toString()
        is List<*> -> v.joinToString(",", "[", "]") { value(it) }
        is Map<*, *> -> v.entries.joinToString(",", "{", "}") {
            "${quote(it.key.toString())}:${value(it.value)}"
        }
        else -> quote(v.toString())
    }

    fun quote(s: String): String {
        val out = StringBuilder(s.length + 2)
        out.append('"')
        for (c in s) when {
            c == '"' -> out.append("\\\"")
            c == '\\' -> out.append("\\\\")
            c == '\n' -> out.append("\\n")
            c == '\r' -> out.append("\\r")
            c == '\t' -> out.append("\\t")
            c < ' ' -> out.append("\\u%04x".format(c.code))
            else -> out.append(c)
        }
        out.append('"')
        return out.toString()
    }
}
