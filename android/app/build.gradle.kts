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
    testOptions {
        // Robolectric reads the app's resources: the screens' strings.
        unitTests.isIncludeAndroidResources = true
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
// which newer JDKs than CI's 17 keep closed.
tasks.withType<Test>().configureEach {
    jvmArgs("--add-exports=java.base/jdk.internal.access=ALL-UNNAMED", "--add-opens=java.base/java.io=ALL-UNNAMED")
}
