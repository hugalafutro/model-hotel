package com.hugalafutro.bellhop.data

/**
 * LinkState is the top-level app gate: Bellhop is either not yet linked to a
 * Front Desk (show the pairing screen) or linked to exactly one (show the
 * dashboard). Loading is the transient state before the persisted link has been
 * read back from disk, so the UI shows neither screen prematurely.
 */
sealed interface LinkState {
    data object Loading : LinkState

    data object Unlinked : LinkState

    /**
     * Linked carries the non-secret link metadata for display. The device token
     * itself is never held here; it stays encrypted in [LinkStore] and is read
     * only when a request needs it.
     */
    data class Linked(
        val fdUrl: String,
        val fdName: String,
        val role: String,
        val deviceId: String,
        val label: String,
    ) : LinkState
}
