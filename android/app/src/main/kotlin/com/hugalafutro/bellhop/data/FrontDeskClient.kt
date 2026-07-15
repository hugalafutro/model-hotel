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
 * ActionResult is the outcome of an operator mutation (drain/activate, config
 * sync). It has one arm the reads don't: [Forbidden] is Front Desk's 403
 * device_role_forbidden, meaning this paired device holds the monitor role and
 * may never mutate. That is the authoritative guard (Bellhop also hides operator
 * controls on a monitor device, but that is only UX); it is distinct from
 * [Unauthorized], which is a dead token that even reads can't use, and from a
 * transient [Failure]. Modeled separately from [FetchResult] so the read call
 * sites don't have to grow a role branch they can never hit.
 */
sealed interface ActionResult<out T> {
    data class Success<T>(
        val data: T,
    ) : ActionResult<T>

    data object Forbidden : ActionResult<Nothing>

    data object Unauthorized : ActionResult<Nothing>

    data class Failure(
        val message: String,
    ) : ActionResult<Nothing>
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

    // Front Desk runs the whole fleet sync (export, per-member dry-run, backup,
    // import with member-side model discovery) before answering POST
    // /api/config/sync, which routinely outlives OkHttp's default 10s read
    // timeout. A premature client hang-up used to abort the run mid-flight
    // server-side, so syncFleet gets its own patient client. Derived from the
    // injected client so it inherits any test/prod wiring.
    private val slowSyncHttp: OkHttpClient by lazy {
        http.newBuilder().readTimeout(SYNC_READ_TIMEOUT_SECONDS, TimeUnit.SECONDS).build()
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
     * memberTraffic fetches one member's request/error series for the charts.
     * [windowMinutes] is the graph range: Front Desk trims the member series to
     * that span (default one hour). FD answers 200 with reachable=false when the
     * series can't be read, so failures here are FD-transport ones.
     */
    open suspend fun memberTraffic(
        fdUrl: String,
        token: String,
        memberId: String,
        windowMinutes: Int = PrefsStore.DEFAULT_GRAPH_RANGE_MINUTES,
    ): FetchResult<MemberTraffic> = get(fdUrl, "/api/members/$memberId/traffic?window=$windowMinutes", token)

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
     * alertSelection fetches Front Desk's alertable-event catalog enriched with
     * each event's current enabled state (GET /api/alert/selection). Readable by
     * any paired device (monitor included), so the Alerts screen can show what
     * Front Desk alerts on; flipping an event needs the operator role
     * ([setAlertEvent]). The wire envelope is {"events": [...]}; callers get the
     * unwrapped list.
     */
    open suspend fun alertSelection(
        fdUrl: String,
        token: String,
    ): FetchResult<List<AlertEventDef>> =
        when (val r = get<AlertSelectionResponse>(fdUrl, "/api/alert/selection", token)) {
            is FetchResult.Success -> FetchResult.Success(r.data.events)
            FetchResult.Unauthorized -> FetchResult.Unauthorized
            is FetchResult.Failure -> r
        }

    /**
     * setAlertEvent flips one alert event on or off via POST /api/alert/selection.
     * Operator tier: Front Desk enforces the role and echoes back the whole
     * refreshed selection (the ack doubles as the re-read), so the phone reconciles
     * from Front Desk's own truth rather than an optimistic guess. A per-event
     * toggle is atomic on Front Desk, so a dropped request never leaves the
     * selection half-applied; a retry is a safe no-op. A 403 is [ActionResult.Forbidden]
     * (a monitor device may not flip it), a 401 is [ActionResult.Unauthorized].
     */
    open suspend fun setAlertEvent(
        fdUrl: String,
        token: String,
        type: String,
        enabled: Boolean,
    ): ActionResult<List<AlertEventDef>> =
        when (
            val r =
                post<AlertSelectionRequest, AlertSelectionResponse>(
                    fdUrl,
                    "/api/alert/selection",
                    token,
                    AlertSelectionRequest(type, enabled),
                )
        ) {
            is ActionResult.Success -> ActionResult.Success(r.data.events)
            ActionResult.Forbidden -> ActionResult.Forbidden
            ActionResult.Unauthorized -> ActionResult.Unauthorized
            is ActionResult.Failure -> r
        }

    /**
     * setMemberState drains or activates a member via POST
     * /api/members/{id}/state. Operator tier: Front Desk enforces the role and
     * returns the member with its recorded new state (the ack), so a 200 means
     * the intent is recorded; the physical drain converges asynchronously and the
     * dashboard reconciles it. Set-state, not toggle, so a retry is a safe no-op.
     */
    open suspend fun setMemberState(
        fdUrl: String,
        token: String,
        memberId: String,
        state: String,
    ): ActionResult<FleetMember> = post(fdUrl, "/api/members/$memberId/state", token, MemberStateRequest(state))

    /**
     * syncFleet propagates [primaryId]'s config to the rest of the fleet via POST
     * /api/config/sync. Operator tier. Front Desk runs the whole sync before it
     * answers 200, so a success carries the per-member [SyncResponse.results]; the
     * phone summarizes them rather than blocking on convergence.
     */
    open suspend fun syncFleet(
        fdUrl: String,
        token: String,
        primaryId: String,
    ): ActionResult<SyncResponse> = post(fdUrl, "/api/config/sync", token, ConfigSyncRequest(primaryId), slowSyncHttp)

    /**
     * setAutoSync pauses or resumes auto-sync via PUT /api/fleet/autosync.
     * Operator tier: Front Desk enforces the role. Bellhop only ever toggles
     * [enabled] on the unchanged [primaryId] (empty confirm token), which Front
     * Desk applies without an admin confirmation; repointing the primary stays a
     * web-only action. Front Desk echoes the applied config back, so a 200 is the
     * ack the dashboard reconciles against.
     */
    open suspend fun setAutoSync(
        fdUrl: String,
        token: String,
        enabled: Boolean,
        primaryId: String,
    ): ActionResult<AutoSyncConfig> = put(fdUrl, "/api/fleet/autosync", token, AutoSyncRequest(enabled, primaryId))

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

    // post/put are the shared authenticated operator mutations (POST for actions,
    // PUT for the auto-sync toggle); both delegate to mutate with the HTTP method.
    private suspend inline fun <reified B, reified T> post(
        fdUrl: String,
        path: String,
        token: String,
        body: B,
        client: OkHttpClient = http,
    ): ActionResult<T> = mutate(fdUrl, path, token, body, "POST", client)

    private suspend inline fun <reified B, reified T> put(
        fdUrl: String,
        path: String,
        token: String,
        body: B,
    ): ActionResult<T> = mutate(fdUrl, path, token, body, "PUT")

    // mutate sends a JSON body with a bearer token and decodes the 2xx body as T.
    // It maps 403 to Forbidden (this device's role may not mutate) and 401 to
    // Unauthorized (dead token) as distinct arms, and keeps every other throwable
    // inside the modeled result set exactly like get() does.
    private suspend inline fun <reified B, reified T> mutate(
        fdUrl: String,
        path: String,
        token: String,
        body: B,
        method: String,
        client: OkHttpClient = http,
    ): ActionResult<T> =
        withContext(Dispatchers.IO) {
            runCatching {
                val requestBody = json.encodeToString(body).toRequestBody(JSON_MEDIA)
                val request =
                    Request
                        .Builder()
                        .url("${base(fdUrl)}$path")
                        .header("Authorization", "Bearer $token")
                        .method(method, requestBody)
                        .build()
                client.newCall(request).execute().use { resp ->
                    val text = resp.body?.string().orEmpty()
                    when {
                        resp.isSuccessful -> ActionResult.Success(json.decodeFromString<T>(text))
                        resp.code == 403 -> ActionResult.Forbidden
                        resp.code == 401 -> ActionResult.Unauthorized
                        else -> ActionResult.Failure(errorMessage(text, resp.code))
                    }
                }
            }.getOrElse { e ->
                if (e is CancellationException) throw e
                ActionResult.Failure(e.message ?: "could not reach the Front Desk")
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

        // Generous ceiling for the synchronous fleet sync: per-member imports
        // run model discovery and Front Desk answers only after every member is
        // done, so this must comfortably cover a few slow members. Front Desk
        // also detaches the run from the connection, so even tripping this
        // no longer aborts the sync — it only stops the phone waiting.
        private const val SYNC_READ_TIMEOUT_SECONDS = 180L

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
                    if (q.until.isNotEmpty()) add("until" to q.until)
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
