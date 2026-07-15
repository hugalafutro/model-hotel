# Bellhop, the Model Hotel Android companion app

Bellhop (BH) is a native Android app that monitors and operates a Model Hotel fleet
from your phone. It links to exactly one Front Desk (FD) instance and only ever talks
to that FD; it never holds a member Model Hotel token. The full design lives in
[`plans/android-companion-app.md`](../plans/android-companion-app.md).

Current status: in active use. Pairing (QR/code), the live dashboard and member
detail, background monitoring, push notifications, operator actions, an app lock,
alerts, an event log, a settings screen, and full internationalisation have shipped.

## Stack

- Kotlin + Jetpack Compose (Material 3), single module, MVVM.
- minSdk 26 (Android 8), target/compile SDK 35.
- Brand theming (plan section 5.5), no Material You dynamic color: dark "night lobby"
  (ink blue + brass, matching Model Hotel's copper theme) and light "day shift" (warm
  paper), following the system setting. A green-phosphor terminal theme is planned for
  the Phase A4 polish pass. Tokens live in `ui/theme/Color.kt` and `Type.kt`.
- Bundled fonts (OFL, texts in `licenses/`): Zilla Slab (display), IBM Plex Sans (body,
  variable), IBM Plex Mono (metrics, and the future phosphor theme).
- JVM-only unit tests: JUnit 4 + Robolectric + Compose UI test.
- ktlint (via the jlleitschuh Gradle plugin) and Android Lint gate CI.

## Building (CLI only, no Android Studio)

Prerequisites: JDK 21 and the Android SDK (`ANDROID_HOME`, platform 35,
build-tools 35.0.0). Gradle itself comes from the checked-in wrapper.

Gradle requires JDK 21; if your system default `java` is newer, builds fail.
The repo Makefile targets pin `JAVA_HOME` for you:

```bash
make android-build     # assembleDebug -> android/app/build/outputs/apk/debug/app-debug.apk
make android-test      # ktlintCheck + testDebugUnitTest
make android-lint      # Android Lint
make android-install   # build + adb install -r to the connected device/emulator
```

Or invoke the wrapper directly from `android/`:

```bash
JAVA_HOME=/usr/lib/jvm/java-21-openjdk ./gradlew assembleDebug
```

## Testing

Unit tests are JVM-only (Robolectric renders Compose without a device) and run in
CI on every PR that touches `android/`. Tests assert on Compose `testTag`s, never
on display text, so localization cannot break them. Instrumented tests (emulator)
are reserved for what Robolectric cannot do and are not part of the per-push CI.

## CI

`.github/workflows/android.yml` is path-filtered to `android/**`: ktlint,
Android Lint, unit tests, debug APK assembly, and an APK artifact upload.
Release signing and distribution (GitHub Releases + Obtainium) come in Phase A4.
