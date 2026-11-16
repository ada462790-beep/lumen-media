// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package org.lumen.tv

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.graphics.vector.addPathNodes
import androidx.compose.ui.semantics.clearAndSetSemantics
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import org.lumen.core.Titelseite

/**
 * Die Wertungen in einer eigenen Reihe unter dem Titel — wie im Browser
 * (wertungsReihe): IMDb als gelber Schriftzug mit Stern, Note und Stimmen,
 * Rotten Tomatoes mit Tomate (frisch, ab 60 %) oder Klecks (faul) nach dem
 * echten Prozentwert, TMDb mit Prozent. Vorher standen sie am Ende der
 * langen grauen Eckdatenzeile und gingen darin unter.
 */
@OptIn(ExperimentalLayoutApi::class)
@Composable
internal fun WertungenZeile(wertungen: List<Titelseite.Wertung>, gross: Boolean = false, modifier: Modifier = Modifier) {
    if (wertungen.isEmpty()) return
    val schrift = if (gross) 20.sp else 15.sp
    val klein = if (gross) 15.sp else 12.sp
    FlowRow(modifier, horizontalArrangement = Arrangement.spacedBy(if (gross) 26.dp else 16.dp),
            verticalArrangement = Arrangement.spacedBy(6.dp)) {
        wertungen.forEach { w ->
            Row(Modifier.clearAndSetSemantics { contentDescription = w.beschreibung },
                verticalAlignment = Alignment.CenterVertically) {
                when (w.art) {
                    Titelseite.WertungArt.IMDB -> {
                        Text("IMDb", color = Color.Black, fontSize = klein, fontWeight = FontWeight.Black, // sprache: fest
                             modifier = Modifier.clip(RoundedCornerShape(4.dp)).background(IMDB_GELB)
                                 .padding(horizontal = 5.dp, vertical = 1.dp))
                        Spacer(Modifier.width(7.dp))
                        Text("★ " + w.wert, color = Color.White, fontSize = schrift, fontWeight = FontWeight.SemiBold)
                    }
                    Titelseite.WertungArt.NOTE ->
                        Text("★ " + w.wert, color = Color.White, fontSize = schrift, fontWeight = FontWeight.SemiBold)
                    Titelseite.WertungArt.TOMATE_FRISCH -> {
                        Text("🍅", fontSize = schrift) // sprache: fest
                        Spacer(Modifier.width(5.dp))
                        Text(w.wert, color = Color.White, fontSize = schrift, fontWeight = FontWeight.SemiBold)
                    }
                    Titelseite.WertungArt.TOMATE_FAUL -> {
                        Icon(KLECKS, null, tint = KLECKS_GRUEN, modifier = Modifier.size(if (gross) 22.dp else 17.dp))
                        Spacer(Modifier.width(5.dp))
                        Text(w.wert, color = Color.White, fontSize = schrift, fontWeight = FontWeight.SemiBold)
                    }
                    Titelseite.WertungArt.TMDB -> {
                        Text("TMDb", color = TMDB_TUERKIS, fontSize = klein, fontWeight = FontWeight.Black) // sprache: fest
                        Spacer(Modifier.width(6.dp))
                        Text(w.wert, color = Color.White, fontSize = schrift, fontWeight = FontWeight.SemiBold)
                    }
                }
                w.zusatz?.let {
                    Spacer(Modifier.width(6.dp))
                    Text(it, color = Color(0x9EFFFFFF), fontSize = klein)
                }
            }
        }
    }
}

private val IMDB_GELB = Color(0xFFF5C518)
private val TMDB_TUERKIS = Color(0xFF01B4E4)
private val KLECKS_GRUEN = Color(0xFF8DC63F)

/** Der Klecks für „faul" — dieselbe Form wie im Browser (rt-faul). */
private val KLECKS: ImageVector = ImageVector.Builder("klecks", 24.dp, 24.dp, 24f, 24f).apply {
    addPath(
        pathData = addPathNodes("M12 3c1.2 2.4 3.6 1.6 4.6 3.4 1 1.8-.6 3 .8 4.6 1.4 1.6 3.6 1.4 3.6 3.6 0 2.2-2.6 2.2-3.6 3.8-1 1.6.2 3.6-2.2 3.6-2.4 0-2-2-3.2-2s-1.2 2-3.4 1.6C6.4 21.2 7.6 19 6.2 18c-1.4-1-4.2-.6-4.2-2.8 0-2.2 2.6-2 3-3.8.4-1.8-2.2-3.2-.8-4.8 1.4-1.6 3.4.2 4.8-.8C10.4 4.8 10.8 3 12 3z"),
        fill = SolidColor(Color.Black),
    )
}.build()
