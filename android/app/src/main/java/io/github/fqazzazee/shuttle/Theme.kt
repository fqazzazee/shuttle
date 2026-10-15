// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package io.github.fqazzazee.shuttle

import android.os.Build
import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.material3.ColorScheme
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.dynamicDarkColorScheme
import androidx.compose.material3.dynamicLightColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext

// shuttle's palette: the TUI's (internal/tui/theme.go) and the project page's (docs/site.css). Bus
// yellow is what is chosen, as the TUI's chosen tab, with ink on it; sky, its headings and the bus;
// orange, what waits for the user; coral, errors.
private val ink = Color(0xFF1B1E26)

/** shuttle's dark palette: the terminal's look. */
val shuttleDark: ColorScheme = darkColorScheme(
    primary = Color(0xFFF5C451),
    onPrimary = ink,
    primaryContainer = Color(0xFF3A4252),
    onPrimaryContainer = Color(0xFFEEF1F6),
    secondary = Color(0xFF61AFEF),
    onSecondary = ink,
    secondaryContainer = Color(0xFF2B303B),
    onSecondaryContainer = Color(0xFFEEF1F6),
    tertiary = Color(0xFFF0975A),
    onTertiary = ink,
    error = Color(0xFFF7848D),
    onError = ink,
    background = ink,
    onBackground = Color(0xFFC9CED8),
    surface = ink,
    onSurface = Color(0xFFC9CED8),
    surfaceVariant = Color(0xFF2B303B),
    onSurfaceVariant = Color(0xFF8E97A9),
    surfaceContainerLowest = Color(0xFF13151B),
    surfaceContainerLow = Color(0xFF1F222A),
    surfaceContainer = Color(0xFF22252D),
    surfaceContainerHigh = Color(0xFF262A33),
    surfaceContainerHighest = Color(0xFF2B303B),
    outline = Color(0xFF8E97A9),
    outlineVariant = Color(0xFF3A4252),
)

/** shuttle's light palette: the project page's, in a light browser. */
val shuttleLight: ColorScheme = lightColorScheme(
    primary = Color(0xFF8A5B00),
    onPrimary = Color.White,
    primaryContainer = Color(0xFFF6D32D),
    onPrimaryContainer = ink,
    secondary = Color(0xFF1C64C8),
    onSecondary = Color.White,
    secondaryContainer = Color(0xFFDFE6F0),
    onSecondaryContainer = ink,
    tertiary = Color(0xFFB25000),
    onTertiary = Color.White,
    error = Color(0xFFC8303F),
    onError = Color.White,
    background = Color(0xFFF6F7F9),
    onBackground = ink,
    surface = Color(0xFFF6F7F9),
    onSurface = ink,
    surfaceVariant = Color(0xFFEEF1F6),
    onSurfaceVariant = Color(0xFF646B7A),
    surfaceContainerLowest = Color.White,
    surfaceContainerLow = Color.White,
    surfaceContainer = Color(0xFFEEF1F6),
    surfaceContainerHigh = Color.White,
    surfaceContainerHighest = Color.White,
    outline = Color(0xFF646B7A),
    outlineVariant = Color(0xFFDFE6F0),
)

/** Whether this Android has wallpaper colours (Material You) to offer: Android 12 and later. */
val hasWallpaperColors get() = Build.VERSION.SDK_INT >= Build.VERSION_CODES.S

/** Light or dark as the phone is set, in shuttle's palette, or in the wallpaper's colours when asked and offered. */
@Composable
fun ShuttleTheme(wallpaper: Boolean = false, content: @Composable () -> Unit) {
    val dark = isSystemInDarkTheme()
    val colors = if (wallpaper && hasWallpaperColors) {
        val context = LocalContext.current
        if (dark) dynamicDarkColorScheme(context) else dynamicLightColorScheme(context)
    } else {
        if (dark) shuttleDark else shuttleLight
    }
    MaterialTheme(colorScheme = colors, content = content)
}
