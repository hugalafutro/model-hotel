package com.hugalafutro.bellhop.data

import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.channels.awaitClose
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.buffer
import kotlinx.coroutines.flow.callbackFlow
import kotlinx.coroutines.withContext
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import okhttp3.Response
import okhttp3.sse.EventSource
import okhttp3.sse.EventSourceListener
import okhttp3.sse.EventSources
import java.net.URLEncoder
import java.util.concurrent.TimeUnit

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
 * SseMessage is what [FrontDeskClient.streamEvents] emits: a connection opened,
 * a decoded control-plane event, or a 401 meaning the device token is dead. The
 * flow completes on any disconnect (heartbeat gap, network drop, clean close),
 * so reconnection with backoff is the caller's job.
 */
sealed interface SseMessage {
    data object Open : SseMessage

    data class Event(
        val event: FleetEvent,
    ) : SseMessage

    data object Unauthorized : SseMessage
}

/**
 * FrontDeskClient is Bellhop's one HTTP entry point to a Front Desk: the public
 * pairing exchange, self-unlink, the authenticated read tier (members,
 * auto-sync), and the SSE event stream, all on the same OkHttp stack.
 */
open class FrontDeskClient(
    private val http: OkHttpClient = OkHttpClient(),
    private val json: Json = Json { ignoreUnknownKeys = true },
) {
    // Front Desk sends a comment heartbeat every 25s, so the SSE read timeout has
    // to clear that interval (the default 10s would tear a quiet connection down
    // before the first heartbeat). It stays finite and above the heartbeat rather
    // than 0: a silently half-open socket (a proxy or cellular drop that never
    // sends a FIN) would make a 0 read wait forever, so onFailure never fires and
    // streamLoop never reconnects; a finite timeout trips it and reconnects.
    // Derived from the injected client so it inherits any test/prod wiring.
    private val sseFactory: EventSource.Factory by lazy {
        EventSources.createFactory(
            http.newBuilder().readTimeout(SSE_READ_TIMEOUT_SECONDS, TimeUnit.SECONDS).build(),
        )
    }

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

    /**
     * memberTraffic fetches one member's last-hour request/error series (the
     * member-detail chart). Front Desk answers 200 with reachable=false when
     * the series can't be read, so failures here are FD-transport ones.
     */
    open suspend fun memberTraffic(
        fdUrl: String,
        token: String,
        memberId: String,
    ): FetchResult<MemberTraffic> = get(fdUrl, "/api/members/$memberId/traffic", token)

    /**
     * events fetches one page of the Front Desk event log (newest first) with
     * the query's filters applied server-side. Monitor tier, like [members].
     */
    open suspend fun events(
        fdUrl: String,
        token: String,
        query: EventQuery = EventQuery(),
    ): FetchResult<EventsResponse> = get(fdUrl, "/api/events${eventQueryString(query)}", token)

    /** autoSync fetches the auto-sync config; the dashboard badges its primary. */
    open suspend fun autoSync(
        fdUrl: String,
        token: String,
    ): FetchResult<AutoSyncConfig> = get(fdUrl, "/api/fleet/autosync", token)

    /**
     * alertStatus reports whether Front Desk's outbound notifier is reachable
     * and delivering. Monitor tier, like [members]; the Alerts screen renders it
     * as a delivery-health pill.
     */
    open suspend fun alertStatus(
        fdUrl: String,
        token: String,
    ): FetchResult<AlertStatus> = get(fdUrl, "/api/alert/status", token)

    /**
     * alertCatalog fetches Front Desk's alertable-event catalog (what it can
     * notify on, grouped by category). Monitor tier; the enabled subset lives in
     * admin settings and is not readable here, so the screen shows this read-only.
     */
    open suspend fun alertCatalog(
        fdUrl: String,
        token: String,
    ): FetchResult<List<AlertEventDef>> = get(fdUrl, "/api/alert/events", token)

    /**
     * streamEvents subscribes to GET {fdUrl}/api/sse and emits each frame as an
     * [SseMessage]. Comment heartbeats are swallowed by the SSE parser, so only
     * real events surface. The flow completes on disconnect; a 401 first emits
     * [SseMessage.Unauthorized] so the caller can stop reconnecting on a dead
     * token instead of looping. A malformed fdUrl completes immediately.
     */
    open fun streamEvents(
        fdUrl: String,
        token: String,
    ): Flow<SseMessage> =
        callbackFlow {
            val request =
                runCatching {
                    Request
                        .Builder()
                        .url("${base(fdUrl)}/api/sse")
                        .header("Authorization", "Bearer $token")
                        .build()
                }.getOrElse {
                    // A bad URL can never stream; complete so the reconnect loop
                    // backs off rather than tight-looping on an unbuildable request.
                    close()
                    return@callbackFlow
                }
            val listener =
                object : EventSourceListener() {
                    override fun onOpen(
                        eventSource: EventSource,
                        response: Response,
                    ) {
                        trySend(SseMessage.Open)
                    }

                    override fun onEvent(
                        eventSource: EventSource,
                        id: String?,
                        type: String?,
                        data: String,
                    ) {
                        // A malformed frame is dropped, not fatal: the stream stays
                        // open for the next good event.
                        runCatching { json.decodeFromString<FleetEvent>(data) }
                            .getOrNull()
                            ?.let { trySend(SseMessage.Event(it)) }
                    }

                    override fun onClosed(eventSource: EventSource) {
                        close()
                    }

                    override fun onFailure(
                        eventSource: EventSource,
                        t: Throwable?,
                        response: Response?,
                    ) {
                        if (response?.code == 401) trySend(SseMessage.Unauthorized)
                        // Complete normally (not close(t)): a network drop is
                        // expected and handled by the caller's reconnect, not an
                        // error that should crash the collector.
                        close()
                    }
                }
            val source = sseFactory.newEventSource(request, listener)
            awaitClose { source.cancel() }
        }
            // UNLIMITED, fused into the callbackFlow's own channel, so a burst of
            // events can never backpressure trySend into silently dropping a frame
            // (in the worst case the lone Unauthorized that stops the reconnect).
            .buffer(Channel.UNLIMITED)

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

        // Above Front Desk's 25s SSE heartbeat, so a healthy quiet stream never
        // times out but a half-open socket is detected within a heartbeat or two.
        private const val SSE_READ_TIMEOUT_SECONDS = 60L

        // eventQueryString renders an EventQuery as an encoded query string
        // ("" when every field is unset). Internal so the omission and encoding
        // rules can be unit-tested without a server round-trip.
        internal fun eventQueryString(q: EventQuery): String {
            val params =
                buildList {
                    if (q.memberId.isNotEmpty()) add("member_id" to q.memberId)
                    if (q.type.isNotEmpty()) add("type" to q.type)
                    if (q.severity.isNotEmpty()) add("severity" to q.severity)
                    if (q.since.isNotEmpty()) add("since" to q.since)
                    if (q.limit > 0) add("limit" to q.limit.toString())
                    if (q.offset > 0) add("offset" to q.offset.toString())
                }
            if (params.isEmpty()) return ""
            return "?" +
                params.joinToString("&") { (k, v) ->
                    "$k=${URLEncoder.encode(v, "UTF-8")}"
                }
        }
    }
}
