# SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0
#
# R8 keeps what it sees used from Kotlin. The Go core reaches into Java through JNI, by name,
# where R8 cannot see it: gomobile's runtime (go.*) and the binding's classes, with every member,
# and what implements the binding's interfaces in Kotlin (the listener, the password store).
-keep class go.** { *; }
-keep class io.github.fqazzazee.shuttle.mobile.** { *; }
-keep class * implements io.github.fqazzazee.shuttle.mobile.** { *; }
