// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

plugins {
    alias(libs.plugins.android.application)
    alias(libs.plugins.kotlin.compose)
}

android {
    namespace = "io.github.fqazzazee.shuttle"
    compileSdk = 37
    defaultConfig {
        applicationId = "io.github.fqazzazee.shuttle"
        // Android 11: where "All files access" arrived (DECISIONS 2026-10-14).
        minSdk = 30
        targetSdk = 37
        versionCode = 1
        versionName = "0.0.0-dev"
        testInstrumentationRunner = "androidx.test.runner.AndroidJUnitRunner"
    }
    buildFeatures {
        compose = true
    }
    lint {
        warningsAsErrors = true
        // A newer AndroidX or SDK is a pull request of its own, not a lint failure.
        disable += setOf("GradleDependency", "NewerVersionAvailable", "AndroidGradlePluginVersion", "OldTargetApi")
    }
}

dependencies {
    // The Go core: `make aar` in the repository's root builds it.
    implementation(files("../../bin/android/shuttle.aar"))
    implementation(platform(libs.compose.bom))
    implementation(libs.compose.material3)
    implementation(libs.compose.ui)
    implementation(libs.activity.compose)
    implementation(libs.work.runtime)
    testImplementation(libs.junit)
    testImplementation(libs.json)
    androidTestImplementation(libs.androidx.test.junit)
    androidTestImplementation(libs.androidx.test.runner)
}
