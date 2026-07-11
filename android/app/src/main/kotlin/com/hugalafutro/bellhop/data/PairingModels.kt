package com.hugalafutro.bellhop.data

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

// Wire models for the Front Desk pairing exchange (plan section 3.2/3.3). These
// mirror the FD contract exactly: internal/frontdesk/server_devices.go and
// frontdesk/web/src/api/types.ts. Do not rename JSON fields.

/**
 * PairPayload is the JSON both the FD "Paired devices" QR and its copyable
 * pairing string carry. Bellhop parses it to learn where to send the code.
 */
@Serializable
data class PairPayload(
    @SerialName("fd_url") val fdUrl: String = "",
    @SerialName("pairing_code") val pairingCode: String = "",
    @SerialName("fd_name") val fdName: String = "",
)

/** PairRequest is the body of POST {fd_url}/api/pair. */
@Serializable
data class PairRequest(
    val code: String,
    val label: String,
)

/**
 * PairResponse is the 200 body: the device bearer token (returned exactly once)
 * and the created device record.
 */
@Serializable
data class PairResponse(
    val token: String,
    val device: PairedDevice,
)

/** PairedDevice mirrors the FD PairedDevice struct. */
@Serializable
data class PairedDevice(
    val id: String,
    val label: String,
    val role: String,
    @SerialName("created_at") val createdAt: String = "",
    @SerialName("last_seen_at") val lastSeenAt: String? = null,
)

/** ApiError is the FD coded-error envelope ({"error": {code, message}}). */
@Serializable
data class ApiError(
    val error: ApiErrorBody? = null,
)

@Serializable
data class ApiErrorBody(
    val code: String = "",
    val message: String = "",
)
