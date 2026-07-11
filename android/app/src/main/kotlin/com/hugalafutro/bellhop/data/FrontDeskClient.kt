package com.hugalafutro.bellhop.data

import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody

/**
 * PairResult distinguishes the outcomes the pairing screen reacts to: a bad or
 * expired code (the operator re-generates one) is a different message from a
 * network/host failure (the operator likely mistyped the URL).
 */
sealed interface PairResult {
    data class Success(val response: PairResponse) : PairResult

    data object InvalidCode : PairResult

    data class Failure(val message: String) : PairResult
}

/**
 * FetchResult is the outcome of an authenticated read. Unauthorized is its own
 * arm because it means the device token itself no longer works (revoked on
 * Front Desk, most likely), which the dashboard surfaces very differently from
 * a transient network failure.
 */
sealed interface FetchResult<out T> {
    data class Success<T>(
        val data: T,
    ) : FetchResult<T>

    data object Unauthorized : FetchResult<Nothing>

    data class Failure(
        val message: String,
    ) : FetchResult<Nothing>
}

/**
 * FrontDeskClient is Bellhop's one HTTP entry point to a Front Desk: the public
 * pairing exchange, self-unlink, and the authenticated read tier (members,
 * auto-sync). The SSE client arrives in a later slice on the same OkHttp stack.
 */
open class FrontDeskClient(
    private val http: OkHttpClient = OkHttpClient(),
    private val json: Json = Json { ignoreUnknownKeys = true },
) {
    /**
     * pair exchanges a one-time code for a device token at
     * POST {fdUrl}/api/pair. On success the token is returned exactly once.
     */
    open suspend fun pair(
        fdUrl: String,
        code: String,
        label: String,
    ): PairResult =
        withContext(Dispatchers.IO) {
            // Everything that can throw on bad input lives inside runCatching: a
            // malformed fdUrl (IllegalArgumentException from url()) and a 2xx body
            // that is not a PairResponse (SerializationException) must both become
            // a Failure, not an escaped throwable that strands the busy spinner.
            runCatching {
                val body =
                    json
                        .encodeToString(PairRequest(code = code, label = label))
                        .toRequestBody(JSON_MEDIA)
                val request =
                    Request
                        .Builder()
                        .url("${base(fdUrl)}/api/pair")
                        .post(body)
                        .build()
                http.newCall(request).execute().use { resp ->
                    val text = resp.body?.string().orEmpty()
                    when {
                        resp.isSuccessful -> PairResult.Success(json.decodeFromString(text))
                        resp.code == 401 -> PairResult.InvalidCode
                        else -> PairResult.Failure(errorMessage(text, resp.code))
                    }
                }
            }.getOrElse { e ->
                if (e is CancellationException) throw e
                PairResult.Failure(e.message ?: "could not reach the Front Desk")
            }
        }

    /**
     * unlink revokes this device's own token via DELETE {fdUrl}/api/devices/self.
     * Returns true on success; a failure still lets the caller clear local state
     * (a lost link is better cleared than stuck).
     */
    open suspend fun unlink(
        fdUrl: String,
        token: String,
    ): Boolean =
        withContext(Dispatchers.IO) {
            runCatching {
                val request =
                    Request
                        .Builder()
                        .url("${base(fdUrl)}/api/devices/self")
                        .header("Authorization", "Bearer $token")
                        .delete()
                        .build()
                http.newCall(request).execute().use { it.isSuccessful }
            }.getOrElse { e ->
                if (e is CancellationException) throw e
                false
            }
        }

    /** members fetches the fleet: every member plus its live poller status. */
    open suspend fun members(
        fdUrl: String,
        token: String,
    ): FetchResult<List<FleetMember>> = get(fdUrl, "/api/members", token)

    /** autoSync fetches the auto-sync config; the dashboard badges its primary. */
    open suspend fun autoSync(
        fdUrl: String,
        token: String,
    ): FetchResult<AutoSyncConfig> = get(fdUrl, "/api/fleet/autosync", token)

    // get is the shared authenticated GET: bearer token, decode the 2xx body as
    // T, map 401 to Unauthorized (dead token), and keep every other throwable
    // inside the modeled result set exactly like pair() does.
    private suspend inline fun <reified T> get(
        fdUrl: String,
        path: String,
        token: String,
    ): FetchResult<T> =
        withContext(Dispatchers.IO) {
            runCatching {
                val request =
                    Request
                        .Builder()
                        .url("${base(fdUrl)}$path")
                        .header("Authorization", "Bearer $token")
                        .build()
                http.newCall(request).execute().use { resp ->
                    val text = resp.body?.string().orEmpty()
                    when {
                        resp.isSuccessful -> FetchResult.Success(json.decodeFromString<T>(text))
                        resp.code == 401 -> FetchResult.Unauthorized
                        else -> FetchResult.Failure(errorMessage(text, resp.code))
                    }
                }
            }.getOrElse { e ->
                if (e is CancellationException) throw e
                FetchResult.Failure(e.message ?: "could not reach the Front Desk")
            }
        }

    private fun errorMessage(
        text: String,
        code: Int,
    ): String =
        runCatching { json.decodeFromString<ApiError>(text).error?.message }
            .getOrNull()
            ?.takeIf { it.isNotBlank() }
            ?: "request failed ($code)"

    private fun base(fdUrl: String): String = fdUrl.trim().trimEnd('/')

    companion object {
        private val JSON_MEDIA = "application/json; charset=utf-8".toMediaType()
    }
}
