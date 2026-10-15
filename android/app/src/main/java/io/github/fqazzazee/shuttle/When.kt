// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package io.github.fqazzazee.shuttle

import android.text.format.DateUtils
import java.time.Instant

/** How long ago, as Android words it ("5 minutes ago"), and "just now" for the last minute, where it says "0 minutes ago". */
fun ago(t: Instant, justNow: String, now: Long = System.currentTimeMillis()): String =
    if (now - t.toEpochMilli() < DateUtils.MINUTE_IN_MILLIS) {
        justNow
    } else {
        DateUtils.getRelativeTimeSpanString(t.toEpochMilli(), now, DateUtils.MINUTE_IN_MILLIS).toString()
    }
