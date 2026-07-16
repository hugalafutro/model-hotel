plugins {
    alias(libs.plugins.android.application)
    alias(libs.plugins.kotlin.android)
    alias(libs.plugins.kotlin.compose)
    alias(libs.plugins.kotlin.serialization)
    alias(libs.plugins.ktlint)
}

// gitCommit stamps the build with the short HEAD sha (plus a "-dirty" marker when
// the tree has uncommitted changes), mirroring the Go binaries' `-X …buildCommit`
// ldflag. Uses providers.exec so it stays compatible with the configuration cache;
// falls back to "unknown" when git isn't available so a source-tarball build still
// succeeds. The footer deep-links to this commit on GitHub.
fun gitOutput(vararg args: String): String? =
    try {
        providers
            .exec { commandLine("git", "-C", rootDir.absolutePath, *args) }
            .standardOutput.asText.get().trim()
    } catch (e: Exception) {
        null
    }

fun gitCommit(): String {
    val sha = gitOutput("rev-parse", "--short", "HEAD")?.takeIf { it.isNotEmpty() } ?: return "unknown"
    val dirty = gitOutput("status", "--porcelain")?.isNotBlank() ?: false
    return if (dirty) "$sha-dirty" else sha
}

// Bellhop's version lives in android/.version, mirroring the root .version that
// releases the Go services: bumping the file on master triggers the Bellhop
// Release workflow. versionCode is derived from it (major*10000 + minor*100 +
// patch) so it stays monotonic without a second field to bump; minor and patch
// must therefore stay below 100.
val bellhopVersion = rootProject.file(".version").readText().trim().removePrefix("v")
val bellhopVersionCode =
    bellhopVersion.split(".").map { it.toInt() }.let { (major, minor, patch) ->
        require(minor < 100 && patch < 100) { "minor/patch must be < 100 to keep versionCode monotonic" }
        major * 10000 + minor * 100 + patch
    }

// Release signing is CI-only: android-release.yml decodes the keystore from a
// repo secret and hands its path plus credentials over in these env vars. When
// BELLHOP_KEYSTORE is unset (every local build) the release APK is produced
// unsigned, exactly as before.
val releaseKeystore: String? = System.getenv("BELLHOP_KEYSTORE")

android {
    namespace = "com.hugalafutro.bellhop"
    compileSdk = 35

    defaultConfig {
        applicationId = "com.hugalafutro.bellhop"
        minSdk = 26
        targetSdk = 35
        versionCode = bellhopVersionCode
        versionName = bellhopVersion
        buildConfigField("String", "GIT_COMMIT", "\"${gitCommit()}\"")
    }

    signingConfigs {
        if (releaseKeystore != null) {
            create("release") {
                storeFile = file(releaseKeystore)
                storePassword = System.getenv("BELLHOP_KEYSTORE_PASSWORD")
                keyAlias = System.getenv("BELLHOP_KEY_ALIAS")
                keyPassword = System.getenv("BELLHOP_KEY_PASSWORD")
            }
        }
    }

    buildTypes {
        release {
            isMinifyEnabled = true
            isShrinkResources = true
            proguardFiles(
                getDefaultProguardFile("proguard-android-optimize.txt"),
                "proguard-rules.pro",
            )
            if (releaseKeystore != null) {
                signingConfig = signingConfigs.getByName("release")
            }
        }
    }

    buildFeatures {
        compose = true
        buildConfig = true
    }

    lint {
        // Locale parity is enforced here, in the existing Bellhop lint job: the
        // repo's web "i18n Check" only covers the JS/JSON frontends, not Android
        // resources. A values-<lang>/ that is missing a base key (MissingTranslation)
        // or supplies one the base lacks (ExtraTranslation) fails the build, so a
        // half-translated locale can't merge.
        error += listOf("MissingTranslation", "ExtraTranslation")
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
    // UnifiedPush is the Layer-3 real-time wake (plan section 5.2): an opt-in,
    // Google-free replacement for FCM. A distributor (ntfy) holds the socket and
    // wakes Bellhop the moment Front Desk's Apprise pipeline pushes to its topic;
    // the push is only a trigger, the fleet truth is re-fetched from Front Desk.
    implementation(libs.unifiedpush.connector)
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
