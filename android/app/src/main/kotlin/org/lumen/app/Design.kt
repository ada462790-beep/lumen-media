// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package org.lumen.app

import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Favorite
import androidx.compose.material.icons.outlined.FavoriteBorder
import androidx.compose.material3.*
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.StrokeJoin
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.graphics.drawscope.scale
import androidx.compose.ui.graphics.drawscope.translate
import androidx.compose.ui.graphics.vector.PathParser
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import coil.compose.AsyncImage
import org.lumen.core.Figur
import org.lumen.core.Item
import org.lumen.core.Profilbild
import org.lumen.core.TechAbzeichen

/**
 * Das Aussehen — dieselben Werte wie in der Web-App und auf Apple-Geräten,
 * damit sich Lumen überall gleich anfühlt.
 *
 * Die Schrift kommt vom System. Nichts wird nachgeladen; die App braucht
 * dafür kein Internet.
 */
object Lumen {
    val background = Color(0xFF050507)
    val surface = Color(0x0FFFFFFF)
    val fill = Color(0x17FFFFFF)
    val hairline = Color(0x14FFFFFF)

    val label = Color.White
    val label2 = Color(0x9EFFFFFF)
    val label3 = Color(0x61FFFFFF)

    /** Der Akzent färbt genau eine Hauptaktion je Ansicht. */
    val accent = Color(0xFF0A84FF)
    val good = Color(0xFF30D158)
    val warn = Color(0xFFFF9F0A)
    val bad = Color(0xFFFF6961)
    val star = Color(0xFFFFD60A)

    val posterWidth = 150.dp
    val posterHeight = 225.dp
    val corner = 12.dp

    fun avatarColors(key: String): List<Color> = when (key) {
        "av-b" -> listOf(Color(0xFFE67D54), Color(0xFFC75A4A))
        "av-c" -> listOf(Color(0xFFA885E8), Color(0xFF8261D1))
        "av-d" -> listOf(Color(0xFF9FD4D8), Color(0xFF3F8F99))
        "av-k" -> listOf(Color(0xFF7DC788), Color(0xFF54A66B))
        else -> listOf(Color(0xFF6B9EED), Color(0xFF4278DB))
    }
}

@Composable
fun LumenTheme(content: @Composable () -> Unit) {
    MaterialTheme(
        colorScheme = darkColorScheme(
            primary = Lumen.accent,
            background = Lumen.background,
            surface = Lumen.background,
            onBackground = Lumen.label,
            onSurface = Lumen.label,
            error = Lumen.bad,
        ),
        content = content,
    )
}

// ---------------------------------------------------------------- Bausteine

/**
 * Das Profilbild: eine Farbe mit dem Anfangsbuchstaben, eine der
 * eingebauten Figuren oder — mit [bildUrl] — das eigene Bild. Lädt das
 * Bild nicht, bleibt der Buchstabe darunter stehen.
 */
@Composable
fun Avatar(
    initial: String,
    colorKey: String,
    size: androidx.compose.ui.unit.Dp = 40.dp,
    bildUrl: String? = null,
) {
    val figur = Profilbild.figur(colorKey)
    Box(
        modifier = Modifier
            .size(size)
            .clip(CircleShape)
            .then(
                if (figur != null) Modifier.background(Color(figur.farbe))
                else Modifier.background(Brush.linearGradient(Lumen.avatarColors(colorKey)))),
        contentAlignment = Alignment.Center,
    ) {
        if (figur != null) {
            FigurZeichnung(figur, Modifier.fillMaxSize(0.62f))
        } else {
            Text(initial, fontSize = (size.value * 0.42f).sp,
                 fontWeight = FontWeight.Medium, color = Color.White)
        }
        if (bildUrl != null) {
            AsyncImage(model = bildUrl, contentDescription = null,
                       contentScale = ContentScale.Crop, modifier = Modifier.fillMaxSize())
        }
    }
}

/** Eine eingebaute Figur, weiss gezogen — die Pfade kommen aus lumen-core. */
@Composable
fun FigurZeichnung(figur: Figur, modifier: Modifier = Modifier) {
    val pfade = remember(figur.name) {
        figur.formen.map { it to PathParser().parsePathString(it.pfad).toPath() }
    }
    Canvas(modifier) {
        val f = size.minDimension / 24f
        translate((size.width - 24f * f) / 2, (size.height - 24f * f) / 2) {
            scale(f, f, pivot = Offset.Zero) {
                pfade.forEach { (form, pfad) ->
                    if (form.gefuellt) drawPath(pfad, Color.White)
                    drawPath(pfad, Color.White,
                             style = Stroke(1.6f, cap = StrokeCap.Round, join = StrokeJoin.Round))
                }
            }
        }
    }
}

/**
 * Die Abzeichen auf einer Karte: links „Neu" oder „3 neue Folgen" in Rot,
 * rechts untereinander 4K und HDR — wie im Browser. Nur, was die Daten
 * hergeben.
 */
@Composable
fun KartenAbzeichen(item: Item, modifier: Modifier = Modifier, gross: Boolean = false) {
    val links = item.abzeichenLinks
    val rechts = item.abzeichenRechts
    if (links == null && rechts.isEmpty()) return
    Row(
        modifier.fillMaxWidth().padding(if (gross) 10.dp else 7.dp),
        horizontalArrangement = Arrangement.SpaceBetween,
        verticalAlignment = Alignment.Top,
    ) {
        if (links != null) AbzeichenPille(links, Color(0xFFFF453A), gross) else Spacer(Modifier.width(1.dp))
        Column(horizontalAlignment = Alignment.End, verticalArrangement = Arrangement.spacedBy(4.dp)) {
            rechts.forEach { AbzeichenPille(it, Color(0x99000000), gross) }
        }
    }
}

@Composable
private fun AbzeichenPille(text: String, farbe: Color, gross: Boolean) {
    Text(
        text,
        color = Color.White,
        fontSize = if (gross) 14.sp else 10.sp,
        fontWeight = FontWeight.Bold,
        letterSpacing = 0.3.sp,
        maxLines = 1,
        modifier = Modifier
            .clip(RoundedCornerShape(if (gross) 7.dp else 5.dp))
            .background(farbe)
            .padding(horizontal = if (gross) 8.dp else 6.dp, vertical = if (gross) 3.dp else 2.dp),
    )
}

/**
 * Die Technik-Zeile der Titelseite: kleine umrandete Abzeichen —
 * Auflösung, HDR, Ton, Sprachen, Untertitel; „Direkt abspielbar" grün,
 * „Wird umgewandelt" blau.
 */
@OptIn(ExperimentalLayoutApi::class)
@Composable
fun TechnikZeile(abzeichen: List<TechAbzeichen>, modifier: Modifier = Modifier) {
    if (abzeichen.isEmpty()) return
    FlowRow(
        modifier,
        horizontalArrangement = Arrangement.spacedBy(8.dp),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        abzeichen.forEach { a ->
            val (rand, grund, schrift) = when (a.art) {
                "gut" -> Triple(Color.Transparent, Color(0x2930D158), Lumen.good)
                "blau" -> Triple(Color.Transparent, Color(0x2E0A84FF), Color(0xFF4AA3FF))
                else -> Triple(Color(0x33FFFFFF), Color.Transparent, Lumen.label)
            }
            Row(
                Modifier
                    .height(24.dp)
                    .clip(RoundedCornerShape(6.dp))
                    .background(grund)
                    .border(1.dp, rand, RoundedCornerShape(6.dp))
                    .padding(horizontal = 8.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                if (a.untertitel) {
                    SprechblaseSymbol(schrift, Modifier.size(13.dp))
                    Spacer(Modifier.width(5.dp))
                }
                Text(a.text, color = schrift, fontSize = 12.sp, fontWeight = FontWeight.SemiBold,
                     letterSpacing = 0.3.sp, maxLines = 1)
            }
        }
    }
}

/**
 * Das Bild eines Titels. Gibt es keins, steht der Titel selbst da — das
 * ist ehrlicher als ein graues Rechteck und hilft beim Wiederfinden.
 */
@Composable
fun Artwork(
    url: String?,
    title: String,
    modifier: Modifier = Modifier,
    corner: androidx.compose.ui.unit.Dp = Lumen.corner,
) {
    Box(
        modifier = modifier
            .clip(RoundedCornerShape(corner))
            .background(Brush.linearGradient(
                listOf(Color(0xFF1C1C21), Color(0xFF0D0D10)))),
        contentAlignment = Alignment.Center,
    ) {
        if (url == null) {
            Text(
                title,
                color = Lumen.label3,
                fontSize = 13.sp,
                fontWeight = FontWeight.SemiBold,
                textAlign = TextAlign.Center,
                maxLines = 4,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.padding(8.dp),
            )
        } else {
            AsyncImage(
                model = url,
                contentDescription = null,
                contentScale = ContentScale.Crop,
                modifier = Modifier.fillMaxSize(),
            )
        }
    }
}

/** Der Fortschrittsbalken unten auf einem Poster. */
@Composable
fun ProgressStripe(percent: Double, modifier: Modifier = Modifier) {
    Box(modifier.fillMaxWidth().height(3.dp).background(Color(0x73000000))) {
        Box(
            Modifier
                .fillMaxWidth((percent / 100).coerceIn(0.0, 1.0).toFloat())
                .fillMaxHeight()
                .background(Color.White)
        )
    }
}

/** Der Haken auf einem gesehenen Titel. */
@Composable
fun SeenBadge(modifier: Modifier = Modifier) {
    Box(
        modifier
            .padding(6.dp)
            .size(20.dp)
            .clip(CircleShape)
            .background(Color.White),
        contentAlignment = Alignment.Center,
    ) {
        Text("✓", color = Color.Black, fontSize = 11.sp, fontWeight = FontWeight.Bold)
    }
}

/** Die eine Hauptaktion einer Ansicht. */
@Composable
fun PrimaryButton(text: String, enabled: Boolean = true, onClick: () -> Unit) {
    Button(
        onClick = onClick,
        enabled = enabled,
        shape = CircleShape,
        colors = ButtonDefaults.buttonColors(
            containerColor = Color.White, contentColor = Color.Black,
            disabledContainerColor = Lumen.fill, disabledContentColor = Lumen.label3),
        contentPadding = PaddingValues(horizontal = 22.dp, vertical = 12.dp),
    ) { Text(text, fontWeight = FontWeight.SemiBold) }
}

/**
 * Das Herz neben „Merken": rund wie die übrigen Knöpfe, rot,
 * wenn der Titel zu den eigenen Favoriten gehört. Die Merkliste ist „will
 * ich noch sehen", das Herz „mag ich" — darum zwei Knöpfe. Mit Wort wie im
 * Browser („Zu Favoriten" / „Favorit"): ein Herz allein lässt offen, ob
 * ein Tipp es setzt oder schon gesetzt ist.
 */
@Composable
fun HerzKnopf(an: Boolean, onClick: () -> Unit) {
    Button(
        onClick = onClick,
        shape = CircleShape,
        colors = ButtonDefaults.buttonColors(
            containerColor = Lumen.fill, contentColor = if (an) Color(0xFFFF453A) else Lumen.label),
        contentPadding = PaddingValues(horizontal = 18.dp, vertical = 12.dp),
    ) {
        Icon(
            if (an) Icons.Filled.Favorite else Icons.Outlined.FavoriteBorder,
            contentDescription = null,
            modifier = Modifier.size(18.dp))
        Spacer(Modifier.width(6.dp))
        Text(if (an) tx(R.string.favorit) else tx(R.string.zu_favoriten), fontWeight = FontWeight.SemiBold)
    }
}

/** Alles Übrige — zurückhaltend, damit die Hauptaktion steht. */
@Composable
fun QuietButton(text: String, enabled: Boolean = true, onClick: () -> Unit) {
    Button(
        onClick = onClick,
        enabled = enabled,
        shape = CircleShape,
        colors = ButtonDefaults.buttonColors(
            containerColor = Lumen.fill, contentColor = Lumen.label),
        contentPadding = PaddingValues(horizontal = 18.dp, vertical = 10.dp),
    ) { Text(text, fontSize = 15.sp) }
}

@Composable
fun Field(
    label: String,
    value: String,
    onValueChange: (String) -> Unit,
    // modifier steht vor den übrigen wahlfreien Parametern — so ist es in
    // Compose üblich, und wer sich daran hält, kann jede Funktion gleich
    // aufrufen, ohne nachzusehen.
    modifier: Modifier = Modifier,
    hint: String? = null,
    secret: Boolean = false,
) {
    Column(modifier.fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(6.dp)) {
        Text(label, color = Lumen.label2, fontSize = 13.sp)
        OutlinedTextField(
            value = value,
            onValueChange = onValueChange,
            singleLine = true,
            visualTransformation = if (secret)
                androidx.compose.ui.text.input.PasswordVisualTransformation()
            else androidx.compose.ui.text.input.VisualTransformation.None,
            shape = RoundedCornerShape(12.dp),
            colors = OutlinedTextFieldDefaults.colors(
                focusedContainerColor = Lumen.fill,
                unfocusedContainerColor = Lumen.fill,
                focusedBorderColor = Lumen.accent,
                unfocusedBorderColor = Color.Transparent,
            ),
            modifier = Modifier.fillMaxWidth(),
        )
        if (hint != null) {
            Text(hint, color = Lumen.label3, fontSize = 12.sp)
        }
    }
}

/** Lädt, Fehler oder nichts gefunden — an einer Stelle statt überall. */
@Composable
fun StateOverlay(
    loading: Boolean,
    error: String? = null,
    empty: Boolean = false,
    emptyText: String = tx(R.string.hier_ist_noch_nichts),
    onRetry: (() -> Unit)? = null,
) {
    when {
        error != null -> Column(
            Modifier.fillMaxSize().padding(32.dp),
            verticalArrangement = Arrangement.Center,
            horizontalAlignment = Alignment.CenterHorizontally,
        ) {
            Text(tx(R.string.das_hat_nicht_geklappt_2), fontSize = 19.sp, fontWeight = FontWeight.SemiBold)
            Spacer(Modifier.height(8.dp))
            Text(error, color = Lumen.label2, textAlign = TextAlign.Center)
            if (onRetry != null) {
                Spacer(Modifier.height(18.dp))
                QuietButton(tx(R.string.nochmal_versuchen), onClick = onRetry)
            }
        }
        loading -> Box(Modifier.fillMaxSize(), Alignment.Center) {
            CircularProgressIndicator(color = Lumen.label2)
        }
        empty -> Box(Modifier.fillMaxSize(), Alignment.Center) {
            Text(emptyText, color = Lumen.label2)
        }
    }
}


// ---------------------------------------------------------------- Kürzel

/** Scrollbar machen, ohne an jeder Stelle einen Zustand zu merken. */
@Composable
fun Modifier.verticalScrollable(): Modifier = verticalScroll(rememberScrollState())

@Composable
fun Modifier.horizontalScrollable(): Modifier = horizontalScroll(rememberScrollState())

/**
 * Anklickbar, ohne das Wellen-Aufleuchten von Material: in einer dunklen,
 * bildlastigen Oberfläche wirkt es wie ein Fehler.
 */
fun Modifier.clickableRow(onClick: () -> Unit): Modifier =
    // Als Knopf ansagen: TalkBack liest sonst nur den Text vor, ohne zu
    // sagen, dass man ihn antippen kann.
    clickable(role = androidx.compose.ui.semantics.Role.Button, onClick = onClick)
