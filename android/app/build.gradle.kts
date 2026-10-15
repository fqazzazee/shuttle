// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

plugins {
    alias(libs.plugins.android.application)
    alias(libs.plugins.kotlin.compose)
}

// A release's version, from its tag: -PshuttleVersion=1.2.3 (make apk passes it). Its versionCode is
// major·10000 + minor·100 + patch (DECISIONS 2026-10-14), so each release installs over the one
// before; a version that is not plain X.Y.Z, under 100 each but the major, is refused.
val shuttleVersion = providers.gradleProperty("shuttleVersion").orNull
val shuttleVersionCode = shuttleVersion?.let { v ->
    val parts = Regex("""(\d+)\.(\d+)\.(\d+)""").matchEntire(v)?.groupValues?.drop(1)?.map(String::toInt)
    require(parts != null && parts[1] < 100 && parts[2] < 100) { "shuttleVersion $v: want X.Y.Z, minor and patch under 100" }
    parts[0] * 10000 + parts[1] * 100 + parts[2]
}

// The release key, from the environment: the release workflow writes it from its secrets
// (SHUTTLE_SIGNING_KEYSTORE, a PKCS12 file; SHUTTLE_SIGNING_PASSWORD; SHUTTLE_SIGNING_ALIAS). It is
// never in the repository. Without it, a release build is left unsigned.
val signingKeystore = providers.environmentVariable("SHUTTLE_SIGNING_KEYSTORE").orNull

android {
    namespace = "io.github.fqazzazee.shuttle"
    compileSdk = 37
    defaultConfig {
        applicationId = "io.github.fqazzazee.shuttle"
        // Android 11: where "All files access" arrived (DECISIONS 2026-10-14).
        minSdk = 30
        targetSdk = 37
        versionCode = shuttleVersionCode ?: 1
        versionName = shuttleVersion ?: "0.0.0-dev"
        testInstrumentationRunner = "androidx.test.runner.AndroidJUnitRunner"
    }
    signingConfigs {
        if (signingKeystore != null) {
            create("release") {
                storeFile = file(signingKeystore)
                storeType = "pkcs12"
                storePassword = providers.environmentVariable("SHUTTLE_SIGNING_PASSWORD").get()
                keyAlias = providers.environmentVariable("SHUTTLE_SIGNING_ALIAS").get()
                // A PKCS12 file has one password, for the file and the key.
                keyPassword = storePassword
            }
        }
    }
    buildTypes {
        release {
            signingConfig = signingConfigs.findByName("release")
            // Shrunk: unshrunk, Compose and its icons were 24 MB of the APK's 35.
            isMinifyEnabled = true
            isShrinkResources = true
            proguardFiles(getDefaultProguardFile("proguard-android-optimize.txt"), "proguard-rules.pro")
            // Releases carry arm64 only (DECISIONS 2026-10-14): the .aar's one ABI, said here too.
            ndk { abiFilters += "arm64-v8a" }
        }
    }
    buildFeatures {
        compose = true
    }
    testOptions {
        // Robolectric reads the app's resources: the screens' strings.
        unitTests.isIncludeAndroidResources = true
    }
    lint {
        warningsAsErrors = true
        // A newer AndroidX or SDK is a pull request of its own, not a lint failure.
        disable += setOf("GradleDependency", "NewerVersionAvailable", "AndroidGradlePluginVersion", "OldTargetApi")
        // Releases are arm64 only (DECISIONS 2026-10-14): x86_64 ChromeOS is left out on purpose.
        disable += "ChromeOsAbiSupport"
    }
}

dependencies {
    // The Go core: `make aar` in the repository's root builds it.
    implementation(files("../../bin/android/shuttle.aar"))
    implementation(platform(libs.compose.bom))
    implementation(libs.compose.material3)
    implementation(libs.compose.ui)
    implementation(libs.compose.icons)
    implementation(libs.activity.compose)
    implementation(libs.work.runtime)
    testImplementation(libs.junit)
    testImplementation(libs.json)
    testImplementation(libs.robolectric)
    testImplementation(platform(libs.compose.bom))
    testImplementation(libs.compose.ui.test)
    testImplementation(libs.espresso.core)
    testImplementation(libs.androidx.test.junit)
    androidTestImplementation(platform(libs.compose.bom))
    androidTestImplementation(libs.compose.ui.test)
    androidTestImplementation(libs.espresso.core)
    debugImplementation(libs.compose.ui.test.manifest)
    androidTestImplementation(libs.androidx.test.junit)
    androidTestImplementation(libs.androidx.test.runner)
}

// Robolectric reaches into the JDK for file descriptors (jdk.internal.access.SharedSecrets),
// which JDKs from 22 on keep closed (CI runs 21).
tasks.withType<Test>().configureEach {
    jvmArgs("--add-exports=java.base/jdk.internal.access=ALL-UNNAMED", "--add-opens=java.base/java.io=ALL-UNNAMED")
}
