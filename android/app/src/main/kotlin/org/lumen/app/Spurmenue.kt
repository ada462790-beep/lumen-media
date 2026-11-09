// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package org.lumen.app

import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.geometry.CornerRadius
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import coil.compose.AsyncImage
import kotlinx.coroutines.delay
import org.lumen.core.AmStueck
import org.lumen.core.Item

/**
 * Die Bausteine über dem Bild: die zwei runden Symbole rechts unten, das
 * Feld mit Tonspuren und Untertiteln, der Knopf „Vorspann überspringen"
 * und die Karte „Als Nächstes".
 *
 * Gebaut wie im Player von Apple TV: ein dunkles, halbdurchsichtiges Feld
 * am rechten Rand, Abschnitte in gedämpften Grossbuchstaben, Einträge mit
 * Luft, ein Haken rechts am gewählten. Kein Dialog des Systems — der sähe
 * nach einer anderen App aus. Auf dem Telefon tippt man; der Fernseher hat
 * dieselben Bausteine mit Fokus (TvActivity.kt).
 */

/** Welches Feld offen ist. */
enum class Spurfeld { TON, UNTERTITEL, FOLGEN }

/** Ein Eintrag im Feld: Text, gewählt oder nicht, und was beim Tippen passiert. */
data class Spureintrag(
    val text: String,
    val gewaehlt: Boolean = false,
    val gedaempft: Boolean = false,
    val onClick: (() -> Unit)? = null,
)

/** Ein Abschnitt im Feld: Überschrift in Grossbuchstaben, darunter die Einträge. */
data class Spurabschnitt(val titel: String, val eintraege: List<Spureintrag>)

/** Das Symbol für Untertitel: eine Sprechblase mit zwei Zeilen. */
@Composable
fun SprechblaseSymbol(farbe: Color, modifier: Modifier = Modifier) {
    Canvas(modifier) {
        val s = size.minDimension
        val strich = s * 0.085f
        val breite = s * 0.84f
        val hoehe = s * 0.62f
        val links = (size.width - breite) / 2
        val oben = s * 0.08f
        drawRoundRect(
            color = farbe,
            topLeft = Offset(links, oben),
            size = Size(breite, hoehe),
            cornerRadius = CornerRadius(s * 0.16f),
            style = Stroke(strich),
        )
        // Der Zipfel unten links.
        val zipfel = Path().apply {
            moveTo(links + breite * 0.22f, oben + hoehe - strich / 2)
            lineTo(links + breite * 0.16f, oben + hoehe + s * 0.2f)
            lineTo(links + breite * 0.42f, oben + hoehe - strich / 2)
        }
        drawPath(zipfel, farbe, style = Stroke(strich, cap = StrokeCap.Round))
        // Zwei Zeilen Text.
        for ((i, laenge) in listOf(0.52f, 0.36f).withIndex()) {
            val y = oben + hoehe * (0.38f + i * 0.26f)
            drawLine(farbe, Offset(links + breite * 0.2f, y),
                     Offset(links + breite * (0.2f + laenge), y), strich, StrokeCap.Round)
        }
    }
}

/** Das Symbol für den Ton: drei Balken wie ein Pegel. */
@Composable
fun TonSymbol(farbe: Color, modifier: Modifier = Modifier) {
    Canvas(modifier) {
        val s = size.minDimension
        val balken = s * 0.16f
        val abstand = s * 0.1f
        val gesamt = 3 * balken + 2 * abstand
        val links = (size.width - gesamt) / 2
        for ((i, anteil) in listOf(0.45f, 0.9f, 0.65f).withIndex()) {
            val h = s * anteil
            drawRoundRect(
                color = farbe,
                topLeft = Offset(links + i * (balken + abstand), (size.height - h) / 2),
                size = Size(balken, h),
                cornerRadius = CornerRadius(balken / 2),
            )
        }
    }
}

/** Das Symbol für die Folgen: drei Zeilen, die unterste kürzer. */
@Composable
fun FolgenSymbol(farbe: Color, modifier: Modifier = Modifier) {
    Canvas(modifier) {
        val s = size.minDimension
        val strich = s * 0.09f
        val links = s * 0.16f
        val rechts = s * 0.84f
        listOf(0.27f to rechts, 0.5f to rechts, 0.73f to s * 0.58f).forEach { (y, ende) ->
            drawLine(farbe, Offset(links, s * y), Offset(ende, s * y), strich, StrokeCap.Round)
        }
    }
}

/** Das Symbol für „Nächste Folge": ein Dreieck mit Strich dahinter. */
@Composable
fun NaechsteSymbol(farbe: Color, modifier: Modifier = Modifier) {
    Canvas(modifier) {
        val s = size.minDimension
        val dreieck = Path().apply {
            moveTo(s * 0.2f, s * 0.2f)
            lineTo(s * 0.62f, s * 0.5f)
            lineTo(s * 0.2f, s * 0.8f)
            close()
        }
        drawPath(dreieck, farbe)
        drawLine(farbe, Offset(s * 0.76f, s * 0.2f), Offset(s * 0.76f, s * 0.8f), s * 0.1f, StrokeCap.Round)
    }
}

/** „Nächste Folge" — eine Pille neben den runden Knöpfen. */
@Composable
fun NaechsteFolgeKnopf(onClick: () -> Unit) {
    Row(
        Modifier.height(44.dp).clip(CircleShape).background(Color(0x8C000000))
            .clickableRow(onClick).padding(horizontal = 16.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        NaechsteSymbol(Color.White, Modifier.size(16.dp))
        Spacer(Modifier.width(8.dp))
        Text(tx(R.string.naechste_folge), fontSize = 15.sp, fontWeight = FontWeight.SemiBold)
    }
}

/** Ein runder Knopf mit einem der Symbole. */
@Composable
fun SpurKnopf(feld: Spurfeld, aktiv: Boolean, onClick: () -> Unit) {
    val hintergrund = if (aktiv) Color.White else Color(0x8C000000)
    val glyph = if (aktiv) Color.Black else Color.White
    Box(
        Modifier.size(44.dp).clip(CircleShape).background(hintergrund).clickableRow(onClick),
        contentAlignment = Alignment.Center,
    ) {
        when (feld) {
            Spurfeld.UNTERTITEL -> SprechblaseSymbol(glyph, Modifier.size(22.dp))
            Spurfeld.TON -> TonSymbol(glyph, Modifier.size(22.dp))
            Spurfeld.FOLGEN -> FolgenSymbol(glyph, Modifier.size(22.dp))
        }
    }
}

/**
 * Das Feld am rechten Rand. Ein Tipp daneben schliesst es; ein Tipp auf
 * einen Eintrag wählt ihn und lässt das Feld stehen, damit man das Ergebnis
 * gleich sieht.
 */
@Composable
fun SpurfeldAnsicht(abschnitte: List<Spurabschnitt>, onClose: () -> Unit) {
    Box(Modifier.fillMaxSize().clickableRow(onClose)) {
        Column(
            Modifier
                .align(Alignment.CenterEnd)
                .padding(end = 16.dp, top = 24.dp, bottom = 24.dp)
                .width(320.dp)
                .clip(RoundedCornerShape(20.dp))
                .background(Color(0xD912121A))
                // Tippen im Feld soll es nicht schliessen.
                .clickableRow {}
                .padding(vertical = 14.dp, horizontal = 10.dp)
                .verticalScroll(rememberScrollState()),
        ) {
            abschnitte.forEachIndexed { i, a ->
                if (i > 0) Spacer(Modifier.height(16.dp))
                Text(a.titel.uppercase(), color = Lumen.label3, fontSize = 12.sp,
                     fontWeight = FontWeight.SemiBold, letterSpacing = 1.sp,
                     modifier = Modifier.padding(start = 14.dp, bottom = 6.dp))
                a.eintraege.forEach { e -> SpurZeile(e) }
            }
        }
    }
}

@Composable
private fun SpurZeile(e: Spureintrag) {
    Row(
        Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(12.dp))
            .then(if (e.onClick != null) Modifier.clickableRow(e.onClick) else Modifier)
            .padding(horizontal = 14.dp, vertical = if (e.gedaempft) 8.dp else 14.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(
            e.text,
            color = if (e.gedaempft) Lumen.label3 else Lumen.label,
            fontSize = if (e.gedaempft) 13.sp else 16.sp,
            maxLines = 2, overflow = TextOverflow.Ellipsis,
            modifier = Modifier.weight(1f),
        )
        if (e.gewaehlt) {
            Spacer(Modifier.width(10.dp))
            Text("✓", color = Color.White, fontSize = 17.sp, fontWeight = FontWeight.Bold)
        }
    }
}

/**
 * Das Folgen-Feld: oben die Staffeln als Pillen, darunter die Folgen mit
 * Bild, Nummer, Haken und Balken; die laufende hell. Gebaut wie das Feld
 * für Ton und Untertitel — derselbe dunkle Rand rechts, dieselbe Schrift.
 *
 * [folgen] ist null, solange die Staffel lädt.
 */
@Composable
fun FolgenfeldAnsicht(
    staffeln: List<Item>,
    staffelId: String?,
    folgen: List<Item>?,
    aktuellId: String,
    bild: (Item) -> String?,
    onStaffel: (Item) -> Unit,
    onFolge: (Item) -> Unit,
    onClose: () -> Unit,
) {
    val liste = rememberLazyListState()
    // Zur laufenden Folge, sobald die Staffel da ist.
    LaunchedEffect(folgen) {
        val i = folgen?.indexOfFirst { it.id == aktuellId } ?: -1
        if (i > 0) liste.scrollToItem(i)
    }
    Box(Modifier.fillMaxSize().clickableRow(onClose)) {
        Column(
            Modifier
                .align(Alignment.CenterEnd)
                .padding(end = 16.dp, top = 24.dp, bottom = 24.dp)
                .width(380.dp)
                .clip(RoundedCornerShape(20.dp))
                .background(Color(0xD912121A))
                .clickableRow {}
                .padding(vertical = 14.dp, horizontal = 10.dp),
        ) {
            Text(tx(R.string.folgen), color = Lumen.label3, fontSize = 12.sp,
                 fontWeight = FontWeight.SemiBold, letterSpacing = 1.sp,
                 modifier = Modifier.padding(start = 14.dp, bottom = 8.dp))
            if (staffeln.size > 1) {
                LazyRow(
                    contentPadding = PaddingValues(horizontal = 10.dp),
                    horizontalArrangement = Arrangement.spacedBy(6.dp),
                    modifier = Modifier.padding(bottom = 10.dp),
                ) {
                    items(staffeln, key = { it.id }) { st ->
                        val an = st.id == staffelId
                        Text(AmStueck.staffelName(st), fontSize = 13.sp,
                             color = if (an) Color.Black else Lumen.label,
                             fontWeight = if (an) FontWeight.SemiBold else FontWeight.Normal,
                             modifier = Modifier.clip(CircleShape)
                                 .background(if (an) Color.White else Lumen.fill)
                                 .clickableRow { onStaffel(st) }
                                 .padding(horizontal = 12.dp, vertical = 6.dp))
                    }
                }
            }
            if (folgen == null) {
                Box(Modifier.fillMaxWidth().height(80.dp), Alignment.Center) {
                    androidx.compose.material3.CircularProgressIndicator(color = Lumen.label2)
                }
            } else if (folgen.isEmpty()) {
                Text(tx(R.string.keine_folgen), color = Lumen.label3, fontSize = 14.sp,
                     modifier = Modifier.padding(14.dp))
            } else {
                LazyColumn(state = liste) {
                    items(folgen, key = { it.id }) { f ->
                        FolgenZeile(f, f.id == aktuellId, bild(f)) { onFolge(f) }
                    }
                }
            }
        }
    }
}

@Composable
private fun FolgenZeile(f: Item, aktuell: Boolean, bild: String?, onClick: () -> Unit) {
    Row(
        Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(12.dp))
            .background(if (aktuell) Color(0x29FFFFFF) else Color.Transparent)
            .clickableRow(onClick)
            .padding(horizontal = 8.dp, vertical = 8.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Box(Modifier.width(104.dp).height(58.dp).clip(RoundedCornerShape(8.dp))
                .background(Color(0xFF1C1C21))) {
            if (bild != null) {
                AsyncImage(model = bild, contentDescription = null,
                           contentScale = androidx.compose.ui.layout.ContentScale.Crop,
                           modifier = Modifier.fillMaxSize())
            }
            if (f.progress > 1 && !f.finished) {
                ProgressStripe(f.progress, Modifier.align(Alignment.BottomCenter))
            }
        }
        Spacer(Modifier.width(12.dp))
        Column(Modifier.weight(1f)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text("${f.episodeNo ?: ""}", color = Lumen.label3, fontSize = 14.sp,
                     modifier = Modifier.width(22.dp))
                Text(f.title, fontSize = 15.sp, maxLines = 1, overflow = TextOverflow.Ellipsis,
                     fontWeight = if (aktuell) FontWeight.SemiBold else FontWeight.Normal,
                     color = if (aktuell) Color.White else Lumen.label,
                     modifier = Modifier.weight(1f, fill = false))
                if (f.finished) {
                    Spacer(Modifier.width(6.dp))
                    Text("✓", color = Color.White, fontSize = 13.sp, fontWeight = FontWeight.Bold)
                }
            }
            Text(
                if (aktuell) tx(R.string.laeuft) else f.runtimeMs?.takeIf { it > 0 }?.let { Item.duration(it) } ?: "",
                color = if (aktuell) Lumen.accent else Lumen.label3, fontSize = 12.sp,
                modifier = Modifier.padding(start = 22.dp, top = 2.dp))
        }
    }
}

/** „Vorspann überspringen" — steht, solange die Stelle im Vorspann liegt. */
@Composable
fun VorspannKnopf(onClick: () -> Unit, modifier: Modifier = Modifier) {
    Row(
        modifier
            .clip(RoundedCornerShape(22.dp))
            .background(Color(0xD912121A))
            .clickableRow(onClick)
            .padding(horizontal = 18.dp, vertical = 12.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(tx(R.string.vorspann_ueberspringen), fontSize = 15.sp, fontWeight = FontWeight.SemiBold)
        Spacer(Modifier.width(8.dp))
        Text("›", fontSize = 18.sp, fontWeight = FontWeight.Bold)
    }
}

/**
 * „Als Nächstes" mit Bild und Countdown.
 *
 * [restImFilm] ist die Zeit bis zum Ende des laufenden Titels; der
 * Countdown folgt ihr — wer pausiert, bekommt mehr Zeit, wer springt,
 * weniger. Dazu gilt eine Mindestdauer [mindest]: so kurz vor dem Ende
 * erschienen, zählt die Karte trotzdem bis dahin herunter. Bei null geht
 * es los; „Abbrechen" lässt den Titel zu Ende laufen und sonst nichts.
 */
@Composable
fun AlsNaechstes(
    bild: String?,
    beschriftung: String,
    restImFilm: Int,
    mindest: Int,
    onCancel: () -> Unit,
    onNow: () -> Unit,
    modifier: Modifier = Modifier,
) {
    var vergangen by remember { mutableStateOf(0) }
    val jetzt by rememberUpdatedState(onNow)
    val rest = maxOf(restImFilm, mindest - vergangen).coerceAtLeast(0)

    LaunchedEffect(Unit) {
        while (true) {
            delay(1000)
            vergangen++
        }
    }
    LaunchedEffect(rest) { if (rest <= 0) jetzt() }

    Row(
        modifier
            .widthIn(max = 400.dp)
            .clip(RoundedCornerShape(16.dp))
            .background(Color(0xE612121A))
            .padding(12.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        if (bild != null) {
            AsyncImage(
                model = bild, contentDescription = null,
                contentScale = androidx.compose.ui.layout.ContentScale.Crop,
                modifier = Modifier.width(96.dp).height(54.dp).clip(RoundedCornerShape(8.dp))
                    .background(Color(0xFF1C1C21)),
            )
            Spacer(Modifier.width(12.dp))
        }
        Column(Modifier.weight(1f)) {
            Text(tx(R.string.als_naechstes_in_x_s, rest), color = Lumen.label2, fontSize = 12.sp)
            Text(beschriftung, fontSize = 15.sp, fontWeight = FontWeight.SemiBold,
                 maxLines = 2, overflow = TextOverflow.Ellipsis)
            Spacer(Modifier.height(10.dp))
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                PrimaryButton(tx(R.string.jetzt)) { onNow() }
                QuietButton(tx(R.string.abbrechen)) { onCancel() }
            }
        }
    }
}
