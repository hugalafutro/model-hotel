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
 * FrontDeskClient is Bellhop's one HTTP entry point to a Front Desk. This slice
 * only needs the public pairing exchange and self-unlink; the SSE/members
 * client arrives in a later slice on the same OkHttp stack.
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
