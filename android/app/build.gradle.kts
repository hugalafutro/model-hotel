plugins {
    alias(libs.plugins.android.application)
    alias(libs.plugins.kotlin.android)
    alias(libs.plugins.kotlin.compose)
    alias(libs.plugins.kotlin.serialization)
    alias(libs.plugins.ktlint)
}

android {
    namespace = "com.hugalafutro.bellhop"
    compileSdk = 35

    defaultConfig {
        applicationId = "com.hugalafutro.bellhop"
        minSdk = 26
        targetSdk = 35
        versionCode = 1
        versionName = "0.1.0"
    }

    buildTypes {
        release {
            isMinifyEnabled = true
            isShrinkResources = true
            proguardFiles(
                getDefaultProguardFile("proguard-android-optimize.txt"),
                "proguard-rules.pro",
            )
        }
    }

    buildFeatures {
        compose = true
    }

    testOptions {
        unitTests {
            isIncludeAndroidResources = true
            all { test ->
                // Print each test as it starts, not just failures, so a future
                // wedge names its culprit in the CI log instead of going silent.
                test.testLogging.events("started", "failed", "skipped")
            }
        }
    }
}

androidComponents {
    // Compose UI tests launch ui-test-manifest's ComponentActivity, which is a
    // debugImplementation dependency, so the release-variant unit tests can
    // never pass (CI runs testDebugUnitTest only). Disable them so a plain
    // `./gradlew build` stays green instead of failing on a variant nobody runs.
    beforeVariants(selector().withBuildType("release")) { variant ->
        variant.hostTests[com.android.build.api.variant.HostTestBuilder.UNIT_TEST_TYPE]?.enable = false
    }
}

kotlin {
    jvmToolchain(21)
}

dependencies {
    implementation(platform(libs.androidx.compose.bom))
    implementation(libs.androidx.core.ktx)
    implementation(libs.androidx.activity.compose)
    implementation(libs.androidx.lifecycle.runtime.compose)
    implementation(libs.androidx.lifecycle.viewmodel.compose)
    // ProcessLifecycleOwner drives the app-lock idle timer off the whole
    // process's foreground, not any single Activity's (plan section 3.1).
    implementation(libs.androidx.lifecycle.process)
    implementation(libs.androidx.compose.ui)
    implementation(libs.androidx.compose.material3)
    // The back-arrow vector. material3 currently drags this in transitively,
    // but the icons the app uses must not ride on someone else's dependency.
    implementation(libs.androidx.compose.material.icons.core)
    implementation(libs.androidx.compose.ui.tooling.preview)
    implementation(libs.okhttp)
    implementation(libs.okhttp.sse)
    implementation(libs.kotlinx.serialization.json)
    implementation(libs.androidx.datastore.preferences)
    // WorkManager runs the Layer-2 background poll (plan section 5.2): a periodic
    // worker that diffs fleet health while Bellhop is backgrounded and posts a
    // local notification on a member going down or recovering. No push infra.
    implementation(libs.androidx.work.runtime)
    // BiometricPrompt gates local access to the stored token; its device-credential
    // fallback (pattern/PIN) needs no fingerprint sensor. It requires a
    // FragmentActivity host, hence the explicit fragment dependency (plan 3.1/5.4).
    implementation(libs.androidx.biometric)
    implementation(libs.androidx.fragment)
    // QR scan for pairing: ZXing (Apache-2.0, no Google Play Services / Firebase),
    // in keeping with the plan's FOSS stance. Its CaptureActivity requests the
    // CAMERA permission at runtime, so nothing is asked until Scan is tapped.
    implementation(libs.zxing.android.embedded)
    debugImplementation(libs.androidx.compose.ui.tooling)
    // Provides the empty ComponentActivity that createComposeRule() launches;
    // must be on the debug manifest (Robolectric merges the app's debug
    // manifest), not just the test classpath.
    debugImplementation(libs.androidx.compose.ui.test.manifest)

    testImplementation(libs.junit)
    testImplementation(libs.robolectric)
    testImplementation(libs.okhttp.mockwebserver)
    testImplementation(libs.kotlinx.coroutines.test)
    testImplementation(platform(libs.androidx.compose.bom))
    testImplementation(libs.androidx.compose.ui.test.junit4)
    testImplementation(libs.androidx.compose.ui.test.manifest)
}
