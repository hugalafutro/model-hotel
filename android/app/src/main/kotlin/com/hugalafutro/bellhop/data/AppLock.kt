package com.hugalafutro.bellhop.data

/**
 * LockConfig is the app-lock policy the user controls in Settings: whether the
 * lock is on at all, and the idle grace window (how long Bellhop may sit in the
 * background before the next foreground demands a fresh unlock). The window is
 * measured from when Bellhop last held the foreground, not from device-screen
 * lock, so unrelated screen unlocks never extend it (plan section 3.1).
 */
data class LockConfig(
    val enabled: Boolean,
    val timeoutMs: Long,
)

/**
 * LockTimeout is the fixed set of grace windows the Settings picker offers. The
 * millis are the on-disk representation, so an unknown stored value (e.g. a
 * future build's option this build doesn't know) falls back to [DEFAULT] rather
 * than crashing the picker.
 */
enum class LockTimeout(
    val millis: Long,
) {
    IMMEDIATELY(0L),
    ONE_MINUTE(60_000L),
    FIVE_MINUTES(5 * 60_000L),
    FIFTEEN_MINUTES(15 * 60_000L),
    THIRTY_MINUTES(30 * 60_000L),
    ONE_HOUR(60 * 60_000L),
    ;

    companion object {
        val DEFAULT = THIRTY_MINUTES

        fun fromMillis(millis: Long): LockTimeout = entries.firstOrNull { it.millis == millis } ?: DEFAULT
    }
}

/**
 * shouldLock is the pure gate decision: an enabled lock trips once more than
 * [LockConfig.timeoutMs] has elapsed since Bellhop last held the foreground
 * ([lastForegroundExit]). A disabled lock never trips. Kept side-effect-free so
 * the timing rule is unit-testable without a real clock or lifecycle.
 */
fun shouldLock(
    config: LockConfig,
    lastForegroundExit: Long,
    now: Long,
): Boolean = config.enabled && now - lastForegroundExit > config.timeoutMs

// shouldLockOnEntry is the gate when Bellhop is (re)entered and evaluates the lock.
// A cold start (a fresh process, e.g. after a force-kill) always re-locks when the
// lock is enabled: restarting the app must require re-auth, not silently reuse a
// still-open session. Otherwise it falls back to the idle-window rule [shouldLock].
fun shouldLockOnEntry(
    config: LockConfig,
    lastForegroundExit: Long,
    now: Long,
    coldStart: Boolean,
): Boolean = config.enabled && (coldStart || shouldLock(config, lastForegroundExit, now))
