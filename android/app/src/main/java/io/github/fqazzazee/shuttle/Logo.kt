// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package io.github.fqazzazee.shuttle

import androidx.compose.foundation.Canvas
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.CornerRadius
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.drawscope.DrawScope
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.graphics.lerp
import androidx.compose.ui.graphics.luminance
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics

/** The TUI's colours for its logo (internal/tui/theme.go), dark or light. */
private class LogoColors(
    val sky: Color, val teal: Color, val mint: Color, val amber: Color,
    val yellow: Color, val sign: Color, val glass: Color, val dim: Color,
)

private val darkLogo = LogoColors(
    Color(0xFF61AFEF), Color(0xFF56C2CF), Color(0xFF98C379), Color(0xFFF5C451),
    Color(0xFFF5C451), Color(0xFF13151B), Color(0xFFCDE3FB), Color(0xFF8E97A9),
)
private val lightLogo = LogoColors(
    Color(0xFF1C64C8), Color(0xFF00788A), Color(0xFF2A7430), Color(0xFF8A5B00),
    Color(0xFFF6D32D), Color(0xFF241F31), Color(0xFF4787D6), Color(0xFF646B7A),
)

/** The TUI's wordmark, "shuttle" in blocks (internal/tui/header.go): each row is two of the logo's pixel rows. */
private val wordmark = listOf(
    "█▀▀ █ █ █ █ ▀█▀ ▀█▀ █   █▀▀",
    "▀▀█ █▀█ █ █  █   █  █   █▀▀",
    "▀▀▀ ▀ ▀ ▀▀▀  ▀   ▀  ▀▀▀ ▀▀▀",
)

// The TUI's header in its cells: the bus, 11 columns by 5 rows, a gap of 3, then the wordmark, 27
// columns by 3 rows, a row down: centred on the bus, as nothing follows it here, where the TUI has
// its summary and tabs under it. A cell is twice as high as it is wide, as a terminal's.
private const val BUS_COLUMNS = 11
private const val GAP = 3
private val columns = BUS_COLUMNS + GAP + wordmark[0].length
private const val ROWS = 5

/**
 * shuttle's logo as the TUI draws it at the top of its screen: the bus (smallBus) beside the
 * wordmark, in the TUI's gradient from sky through teal and mint to amber, a colour a column. Drawn
 * cell for cell, in its dark or its light colours as the screen is.
 */
@Composable
fun ShuttleLogo(modifier: Modifier = Modifier) {
    val c = if (MaterialTheme.colorScheme.background.luminance() < 0.5f) darkLogo else lightLogo
    Canvas(modifier.aspectRatio(columns / (ROWS * 2f)).semantics { contentDescription = "shuttle" }) {
        val w = size.width / columns
        bus(c, w)
        wordmark(c, w, (BUS_COLUMNS + GAP) * w, (ROWS - wordmark.size) * w)
    }
}

/** The bus, w a cell's width: its outline and mirrors in sky, its sign with the sync arrows, glass, lights and wheels. */
private fun DrawScope.bus(c: LogoColors, w: Float) {
    val h = 2 * w
    val line = 0.28f * w
    drawRoundRect(c.sky, Offset(1.5f * w, 0.5f * h), Size(8 * w, 4 * h), CornerRadius(0.8f * w), style = Stroke(line))
    for ((from, to) in listOf(0f to 1.5f, 9.5f to 11f)) {
        drawLine(c.sky, Offset(from * w, 2.5f * h), Offset(to * w, 2.5f * h), strokeWidth = 1.6f * line)
    }
    drawRect(c.sign, Offset(4 * w, h), Size(3 * w, h))
    arrows(c.yellow, Offset(4.75f * w, 1.32f * h), 1.5f * w, 0.36f * h, line)
    drawRect(c.glass, Offset(3.5f * w, 2 * h), Size(4 * w, h))
    for (x in listOf(3.5f, 7.5f)) drawCircle(c.yellow, 0.42f * w, Offset(x * w, 3.5f * h))
    for (y in listOf(3.4f, 3.6f)) drawLine(c.dim, Offset(4.8f * w, y * h), Offset(6.2f * w, y * h), strokeWidth = line)
    for (x in listOf(3f, 7f)) drawRect(c.dim, Offset(x * w, 4.5f * h), Size(w, 0.5f * h))
}

/** ⇄: an arrow right above an arrow left, from at their top left, length long, gap apart. */
private fun DrawScope.arrows(color: Color, at: Offset, long: Float, gap: Float, line: Float) {
    val head = 0.3f * long
    for ((y, right) in listOf(at.y to true, at.y + gap to false)) {
        drawLine(color, Offset(at.x, y), Offset(at.x + long, y), strokeWidth = line)
        val tip = if (right) at.x + long else at.x
        val back = if (right) tip - head else tip + head
        val p = Path().apply {
            moveTo(tip, y)
            lineTo(back, y - head * 0.6f)
            lineTo(back, y + head * 0.6f)
            close()
        }
        drawPath(p, color)
    }
}

/** The wordmark from left and top, w a cell's width: a block's halves are its pixels, coloured by column. */
private fun DrawScope.wordmark(c: LogoColors, w: Float, left: Float, top: Float) {
    val stops = listOf(c.sky, c.teal, c.mint, c.amber)
    val last = wordmark[0].length - 1
    for ((row, text) in wordmark.withIndex()) {
        for ((col, ch) in text.withIndex()) {
            val halves = when (ch) {
                '█' -> listOf(0, 1)
                '▀' -> listOf(0)
                '▄' -> listOf(1)
                else -> emptyList()
            }
            if (halves.isEmpty()) continue
            val color = gradient(stops, col.toFloat() / last)
            for (half in halves) drawRect(color, Offset(left + col * w, top + (2 * row + half) * w), Size(w, w))
        }
    }
}

/** The colour at t, from 0 to 1, along stops evenly spaced, as the TUI's Blend1D. */
private fun gradient(stops: List<Color>, t: Float): Color {
    val span = t * (stops.size - 1)
    val i = span.toInt().coerceAtMost(stops.size - 2)
    return lerp(stops[i], stops[i + 1], span - i)
}
