package com.hugalafutro.bellhop.data

import kotlinx.coroutines.flow.take
import kotlinx.coroutines.flow.toList
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withTimeout
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test

class FrontDeskClientTest {
    private lateinit var server: MockWebServer
    private val client = FrontDeskClient()

    @Before
    fun setUp() {
        server = MockWebServer()
        server.start()
    }

    @After
    fun tearDown() {
        server.shutdown()
    }

    @Test
    fun pairSuccessParsesTokenAndPostsCode() =
        runBlocking {
            server.enqueue(
                MockResponse().setBody(
                    """{"token":"tok-123","device":{"id":"i","label":"Pixel","role":"operator","created_at":"t"}}""",
                ),
            )

            // Trailing slash on the base URL must be normalized away.
            val result = client.pair(server.url("/").toString(), "CODE1", "Pixel")

            assertTrue(result is PairResult.Success)
            result as PairResult.Success
            assertEquals("tok-123", result.response.token)
            assertEquals("operator", result.response.device.role)

            val request = server.takeRequest()
            assertEquals("POST", request.method)
            assertEquals("/api/pair", request.path)
            val body = request.body.readUtf8()
            assertTrue(body.contains("\"code\":\"CODE1\""))
            assertTrue(body.contains("\"label\":\"Pixel\""))
        }

    @Test
    fun pairMapsUnauthorizedToInvalidCode() =
        runBlocking {
            server.enqueue(
                MockResponse().setResponseCode(401).setBody(
                    """{"error":{"code":"invalid_pairing_code","message":"nope"}}""",
                ),
            )

            val result = client.pair(server.url("/").toString(), "BAD", "Pixel")
            assertEquals(PairResult.InvalidCode, result)
        }

    @Test
    fun pairMapsServerErrorToFailureWithMessage() =
        runBlocking {
            server.enqueue(
                MockResponse().setResponseCode(500).setBody(
                    """{"error":{"code":"boom","message":"kaboom"}}""",
                ),
            )

            val result = client.pair(server.url("/").toString(), "X", "Pixel")
            assertTrue(result is PairResult.Failure)
            assertEquals("kaboom", (result as PairResult.Failure).message)
        }

    @Test
    fun pairMalformedUrlIsFailureNotThrow() =
        runBlocking {
            // A non-blank but invalid fd_url throws while building the request; it
            // must surface as Failure, not escape and strand the busy spinner.
            val result = client.pair("not a url", "CODE", "label")
            assertTrue(result is PairResult.Failure)
        }

    @Test
    fun pairUnexpectedSuccessBodyIsFailure() =
        runBlocking {
            // 2xx with a body that is not a PairResponse must not throw out of the
            // modeled result set.
            server.enqueue(MockResponse().setBody("<html>not json</html>"))
            val result = client.pair(server.url("/").toString(), "CODE", "label")
            assertTrue(result is PairResult.Failure)
        }

    @Test
    fun membersParsesFleetAndSendsBearer() =
        runBlocking {
            server.enqueue(
                MockResponse().setBody(
                    """[{"id":"m1","name":"hotel-1","url":"http://h1:8080","state":"drained","has_token":true,""" +
                        """"status":{"health":{"known":true,"healthy":true,"latency_ms":12,"checked_at":"t"},""" +
                        """"traefik_status":"UP","version":"0.31.0"}}]""",
                ),
            )

            val result = client.members(server.url("/").toString(), "tok-1")

            assertTrue(result is FetchResult.Success)
            result as FetchResult.Success
            val member = result.data.single()
            assertEquals("m1", member.id)
            assertTrue(member.drained)
            assertTrue(member.status.health.healthy)
            assertEquals(12L, member.status.health.latencyMs)
            assertEquals("UP", member.status.traefikStatus)
            assertEquals("0.31.0", member.status.version)

            val request = server.takeRequest()
            assertEquals("GET", request.method)
            assertEquals("/api/members", request.path)
            assertEquals("Bearer tok-1", request.getHeader("Authorization"))
        }

    @Test
    fun membersMapsUnauthorizedToItsOwnArm() =
        runBlocking {
            // 401 means the device token is dead (revoked), not a transient
            // failure; it must be distinguishable so the dashboard can say so.
            server.enqueue(
                MockResponse().setResponseCode(401).setBody(
                    """{"error":{"code":"unauthorized","message":"bad token"}}""",
                ),
            )
            val result = client.members(server.url("/").toString(), "dead")
            assertEquals(FetchResult.Unauthorized, result)
        }

    @Test
    fun membersUnexpectedSuccessBodyIsFailure() =
        runBlocking {
            server.enqueue(MockResponse().setBody("<html>not json</html>"))
            val result = client.members(server.url("/").toString(), "tok")
            assertTrue(result is FetchResult.Failure)
        }

    @Test
    fun membersMalformedUrlIsFailureNotThrow() =
        runBlocking {
            val result = client.members("not a url", "tok")
            assertTrue(result is FetchResult.Failure)
        }

    @Test
    fun memberTrafficParsesSeries() =
        runBlocking {
            server.enqueue(
                MockResponse().setBody(
                    """{"member_id":"m1","reachable":true,"window_minutes":60,"total_requests":42,""" +
                        """"total_errors":3,"points":[{"bucket":"2026-07-11T10:00:00Z","requests":40,"errors":3},""" +
                        """{"bucket":"2026-07-11T10:05:00Z","requests":2,"errors":0}]}""",
                ),
            )

            val result = client.memberTraffic(server.url("/").toString(), "tok-1", "m1")

            assertTrue(result is FetchResult.Success)
            result as FetchResult.Success
            assertTrue(result.data.reachable)
            assertEquals(42, result.data.totalRequests)
            assertEquals(3, result.data.totalErrors)
            assertEquals(2, result.data.points.size)
            assertEquals(40, result.data.points[0].requests)

            val request = server.takeRequest()
            assertEquals("GET", request.method)
            // The default window (one hour) is sent even when the caller omits it.
            assertEquals("/api/members/m1/traffic?window=60", request.path)
            assertEquals("Bearer tok-1", request.getHeader("Authorization"))
        }

    @Test
    fun memberTrafficSendsRequestedWindow() =
        runBlocking {
            server.enqueue(
                MockResponse().setBody(
                    """{"member_id":"m1","reachable":true,"window_minutes":360,""" +
                        """"total_requests":0,"total_errors":0,"points":[]}""",
                ),
            )

            client.memberTraffic(server.url("/").toString(), "tok-1", "m1", windowMinutes = 360)

            val request = server.takeRequest()
            assertEquals("/api/members/m1/traffic?window=360", request.path)
        }

    @Test
    fun memberTrafficUnreachableIsStillSuccess() =
        runBlocking {
            // No stored admin token on the FD side answers 200 with
            // reachable=false; that's a renderable state, not a Failure.
            server.enqueue(
                MockResponse().setBody(
                    """{"member_id":"m1","reachable":false,"window_minutes":60,"total_requests":0,""" +
                        """"total_errors":0,"points":[]}""",
                ),
            )
            val result = client.memberTraffic(server.url("/").toString(), "tok-1", "m1")
            assertTrue(result is FetchResult.Success)
            result as FetchResult.Success
            assertFalse(result.data.reachable)
            assertTrue(result.data.points.isEmpty())
        }

    @Test
    fun memberTrafficMapsUnauthorizedToItsOwnArm() =
        runBlocking {
            server.enqueue(
                MockResponse().setResponseCode(401).setBody(
                    """{"error":{"code":"unauthorized","message":"bad token"}}""",
                ),
            )
            val result = client.memberTraffic(server.url("/").toString(), "dead", "m1")
            assertEquals(FetchResult.Unauthorized, result)
        }

    @Test
    fun eventsParsesPageAndSendsBearer() =
        runBlocking {
            server.enqueue(
                MockResponse().setBody(
                    """{"events":[{"id":"e1","type":"health.down","severity":"error","source":"poller",""" +
                        """"message":"hotel-2 unreachable","member_id":"m2","created_at":"2026-07-12T10:00:00Z"},""" +
                        """{"id":"e2","type":"config.synced","severity":"success","source":"autosync",""" +
                        """"message":"synced","created_at":"2026-07-12T09:00:00Z"}],"total":40}""",
                ),
            )

            val result = client.events(server.url("/").toString(), "tok-1")

            assertTrue(result is FetchResult.Success)
            result as FetchResult.Success
            assertEquals(40, result.data.total)
            val events = result.data.events.orEmpty()
            assertEquals(2, events.size)
            assertEquals("health.down", events[0].type)
            assertEquals("m2", events[0].memberId)
            assertEquals("", events[1].memberId)

            val request = server.takeRequest()
            assertEquals("GET", request.method)
            assertEquals("/api/events", request.path)
            assertEquals("Bearer tok-1", request.getHeader("Authorization"))
        }

    @Test
    fun eventsSendsFilterQueryParams() =
        runBlocking {
            server.enqueue(MockResponse().setBody("""{"events":[],"total":0}"""))

            val result =
                client.events(
                    server.url("/").toString(),
                    "tok-1",
                    EventQuery(
                        severity = "error",
                        since = "2026-07-12T09:00:00Z",
                        limit = 25,
                        offset = 50,
                    ),
                )
            assertTrue(result is FetchResult.Success)

            val request = server.takeRequest()
            val url = request.requestUrl!!
            assertEquals("/api/events", url.encodedPath)
            assertEquals("error", url.queryParameter("severity"))
            assertEquals("2026-07-12T09:00:00Z", url.queryParameter("since"))
            assertEquals("25", url.queryParameter("limit"))
            assertEquals("50", url.queryParameter("offset"))
            // Unset filters must be omitted, not sent empty: the server filters
            // by equality, so an empty member_id would match nothing.
            assertEquals(null, url.queryParameter("member_id"))
            assertEquals(null, url.queryParameter("type"))
        }

    @Test
    fun eventsNullListIsSuccessWithEmptyPage() =
        runBlocking {
            // Go marshals an empty result as "events": null; that's a
            // renderable empty log, not a Failure.
            server.enqueue(MockResponse().setBody("""{"events":null,"total":0}"""))
            val result = client.events(server.url("/").toString(), "tok-1")
            assertTrue(result is FetchResult.Success)
            result as FetchResult.Success
            assertTrue(result.data.events.orEmpty().isEmpty())
        }

    @Test
    fun eventsMapsUnauthorizedToItsOwnArm() =
        runBlocking {
            server.enqueue(
                MockResponse().setResponseCode(401).setBody(
                    """{"error":{"code":"unauthorized","message":"bad token"}}""",
                ),
            )
            val result = client.events(server.url("/").toString(), "dead")
            assertEquals(FetchResult.Unauthorized, result)
        }

    @Test
    fun eventQueryStringOmitsUnsetAndEncodesValues() {
        assertEquals("", FrontDeskClient.eventQueryString(EventQuery()))
        assertEquals(
            "?member_id=m+1&severity=error&since=2026-07-12T09%3A00%3A00Z&limit=25&offset=50",
            FrontDeskClient.eventQueryString(
                EventQuery(
                    memberId = "m 1",
                    severity = "error",
                    since = "2026-07-12T09:00:00Z",
                    limit = 25,
                    offset = 50,
                ),
            ),
        )
        // The calendar range's upper bound rides along as until.
        assertEquals(
            "?since=2026-07-01T00%3A00%3A00Z&until=2026-07-15T00%3A00%3A00Z",
            FrontDeskClient.eventQueryString(
                EventQuery(since = "2026-07-01T00:00:00Z", until = "2026-07-15T00:00:00Z"),
            ),
        )
    }

    @Test
    fun autoSyncParsesPrimary() =
        runBlocking {
            server.enqueue(MockResponse().setBody("""{"enabled":true,"primary_id":"m2"}"""))

            val result = client.autoSync(server.url("/").toString(), "tok-1")

            assertTrue(result is FetchResult.Success)
            result as FetchResult.Success
            assertTrue(result.data.enabled)
            assertEquals("m2", result.data.primaryId)
            assertEquals("/api/fleet/autosync", server.takeRequest().path)
        }

    @Test
    fun autoSyncParsesFleetState() =
        runBlocking {
            server.enqueue(
                MockResponse().setBody(
                    """{"enabled":true,"primary_id":"p1","stale":false,""" +
                        """"fleet_state":"degraded","fleet_state_reasons":["member_down","sync_held"]}""",
                ),
            )

            val result = client.autoSync(server.url("/").toString(), "tok-1")

            assertTrue(result is FetchResult.Success)
            result as FetchResult.Success
            assertEquals("degraded", result.data.fleetState)
            assertEquals(listOf("member_down", "sync_held"), result.data.fleetStateReasons)
        }

    @Test
    fun autoSyncFleetStateDefaultsOnLegacyPayload() =
        runBlocking {
            // A Front Desk that predates the state machine omits both fields.
            server.enqueue(MockResponse().setBody("""{"enabled":true,"primary_id":"p1","stale":false}"""))

            val result = client.autoSync(server.url("/").toString(), "tok-1")

            assertTrue(result is FetchResult.Success)
            result as FetchResult.Success
            assertEquals("", result.data.fleetState)
            assertEquals(emptyList<String>(), result.data.fleetStateReasons)
        }

    @Test
    fun unlinkMalformedUrlIsFalseNotThrow() =
        runBlocking {
            assertFalse(client.unlink("not a url", "tok"))
        }

    @Test
    fun unlinkSendsBearerDeleteToSelf() =
        runBlocking {
            server.enqueue(MockResponse().setResponseCode(200))

            val ok = client.unlink(server.url("/").toString(), "tok-123")
            assertTrue(ok)

            val request = server.takeRequest()
            assertEquals("DELETE", request.method)
            assertEquals("/api/devices/self", request.path)
            assertEquals("Bearer tok-123", request.getHeader("Authorization"))
        }

    @Test
    fun streamEmitsOpenThenEventWithBearer() =
        runBlocking {
            server.enqueue(
                MockResponse()
                    .setHeader("Content-Type", "text/event-stream")
                    .setBody(
                        "data: {\"id\":\"e1\",\"type\":\"health.down\"," +
                            "\"severity\":\"error\",\"message\":\"down\"}\n\n",
                    ),
            )

            val messages =
                withTimeout(5_000) {
                    client.streamEvents(server.url("/").toString(), "tok-1").take(2).toList()
                }

            assertEquals(SseMessage.Open, messages[0])
            val event = messages[1] as SseMessage.Event
            assertEquals("health.down", event.event.type)
            assertEquals("down", event.event.message)

            val request = server.takeRequest()
            assertEquals("GET", request.method)
            assertEquals("/api/sse", request.path)
            assertEquals("Bearer tok-1", request.getHeader("Authorization"))
        }

    @Test
    fun streamMapsUnauthorizedToItsOwnMessage() =
        runBlocking {
            // A dead token on the stream must be distinguishable so the caller can
            // stop reconnecting instead of looping on a token that will never work.
            server.enqueue(
                MockResponse().setResponseCode(401).setBody(
                    """{"error":{"code":"unauthorized","message":"bad token"}}""",
                ),
            )

            val messages =
                withTimeout(5_000) {
                    client.streamEvents(server.url("/").toString(), "dead").take(1).toList()
                }
            assertEquals(listOf(SseMessage.Unauthorized), messages)
        }

    @Test
    fun streamMalformedUrlCompletesEmpty() =
        runBlocking {
            // A bad fd_url can't stream; it must complete quietly, not throw, so
            // the caller's reconnect loop just backs off.
            val messages = client.streamEvents("not a url", "tok").toList()
            assertTrue(messages.isEmpty())
        }

    @Test
    fun alertStatusParsesAndSendsBearer() =
        runBlocking {
            server.enqueue(
                MockResponse().setBody(
                    """{"configured":true,"reachable":true,"healthy":false,""" +
                        """"detail":"apprise-api returned status 417"}""",
                ),
            )

            val result = client.alertStatus(server.url("/").toString(), "tok-1")

            assertTrue(result is FetchResult.Success)
            result as FetchResult.Success
            assertTrue(result.data.configured)
            assertTrue(result.data.reachable)
            assertFalse(result.data.healthy)
            assertEquals("apprise-api returned status 417", result.data.detail)

            val request = server.takeRequest()
            assertEquals("GET", request.method)
            assertEquals("/api/alert/status", request.path)
            assertEquals("Bearer tok-1", request.getHeader("Authorization"))
        }

    @Test
    fun alertStatusMissingDetailDefaultsEmpty() =
        runBlocking {
            // A healthy notifier omits detail (omitempty on the server); it must
            // decode to "" not fail on the missing field.
            server.enqueue(
                MockResponse().setBody("""{"configured":true,"reachable":true,"healthy":true}"""),
            )
            val result = client.alertStatus(server.url("/").toString(), "tok-1")
            assertTrue(result is FetchResult.Success)
            assertEquals("", (result as FetchResult.Success).data.detail)
        }

    @Test
    fun alertStatusMapsUnauthorizedToItsOwnArm() =
        runBlocking {
            server.enqueue(
                MockResponse().setResponseCode(401).setBody(
                    """{"error":{"code":"unauthorized","message":"bad token"}}""",
                ),
            )
            val result = client.alertStatus(server.url("/").toString(), "dead")
            assertEquals(FetchResult.Unauthorized, result)
        }

    @Test
    fun alertSelectionUnwrapsEnvelopeAndSendsBearer() =
        runBlocking {
            // GET /api/alert/selection returns {"events":[...]} with each event's
            // live enabled state; the client unwraps to the bare list.
            server.enqueue(
                MockResponse().setBody(
                    """{"events":[{"type":"health.down","category":"Health",""" +
                        """"severity":"error","defaultOn":true,"enabled":true},""" +
                        """{"type":"config.synced","category":"Config Sync",""" +
                        """"severity":"info","defaultOn":false,"enabled":false}]}""",
                ),
            )

            val result = client.alertSelection(server.url("/").toString(), "tok-1")

            assertTrue(result is FetchResult.Success)
            result as FetchResult.Success
            assertEquals(2, result.data.size)
            assertEquals("health.down", result.data[0].type)
            assertTrue(result.data[0].enabled)
            assertFalse(result.data[1].enabled)

            val request = server.takeRequest()
            assertEquals("GET", request.method)
            assertEquals("/api/alert/selection", request.path)
            assertEquals("Bearer tok-1", request.getHeader("Authorization"))
        }

    @Test
    fun alertSelectionMapsUnauthorizedToItsOwnArm() =
        runBlocking {
            server.enqueue(
                MockResponse().setResponseCode(401).setBody(
                    """{"error":{"code":"unauthorized","message":"bad token"}}""",
                ),
            )
            val result = client.alertSelection(server.url("/").toString(), "dead")
            assertEquals(FetchResult.Unauthorized, result)
        }

    @Test
    fun setAlertEventPostsBodyAndAdoptsEchoedSelection() =
        runBlocking {
            // The POST echoes the whole refreshed selection: that echo is the ack
            // the phone reconciles from, so a dropped request is never half-applied.
            server.enqueue(
                MockResponse().setBody(
                    """{"events":[{"type":"health.down","category":"Health",""" +
                        """"severity":"error","defaultOn":true,"enabled":false}]}""",
                ),
            )

            val result = client.setAlertEvent(server.url("/").toString(), "tok-1", "health.down", false)

            assertTrue(result is ActionResult.Success)
            result as ActionResult.Success
            assertEquals(1, result.data.size)
            assertFalse(result.data[0].enabled)

            val request = server.takeRequest()
            assertEquals("POST", request.method)
            assertEquals("/api/alert/selection", request.path)
            assertEquals("Bearer tok-1", request.getHeader("Authorization"))
            val body = request.body.readUtf8()
            assertTrue(body.contains("\"type\":\"health.down\""))
            assertTrue(body.contains("\"enabled\":false"))
        }

    @Test
    fun setAlertEventMapsForbiddenToItsOwnArm() =
        runBlocking {
            // 403 device_role_forbidden: a monitor device may not flip an alert.
            server.enqueue(
                MockResponse().setResponseCode(403).setBody(
                    """{"code":"device_role_forbidden","error":"nope"}""",
                ),
            )
            val result = client.setAlertEvent(server.url("/").toString(), "dead", "health.down", true)
            assertEquals(ActionResult.Forbidden, result)
        }

    @Test
    fun setAlertEventMapsUnauthorizedToItsOwnArm() =
        runBlocking {
            server.enqueue(
                MockResponse().setResponseCode(401).setBody(
                    """{"error":{"code":"unauthorized","message":"bad token"}}""",
                ),
            )
            val result = client.setAlertEvent(server.url("/").toString(), "dead", "health.down", true)
            assertEquals(ActionResult.Unauthorized, result)
        }

    @Test
    fun setMemberStateParsesRecordedStateAndPostsBody() =
        runBlocking {
            // Front Desk answers with the member carrying its recorded new state:
            // that is the ack the phone waits on.
            server.enqueue(
                MockResponse().setBody(
                    """{"id":"m1","name":"hotel-1","url":"http://h1:8080","state":"drained","has_token":true}""",
                ),
            )

            val result = client.setMemberState(server.url("/").toString(), "tok-1", "m1", "drained")

            assertTrue(result is ActionResult.Success)
            assertEquals("drained", (result as ActionResult.Success).data.state)

            val request = server.takeRequest()
            assertEquals("POST", request.method)
            assertEquals("/api/members/m1/state", request.path)
            assertEquals("Bearer tok-1", request.getHeader("Authorization"))
            assertTrue(request.body.readUtf8().contains("\"state\":\"drained\""))
        }

    @Test
    fun setMemberStateMapsForbiddenToItsOwnArm() =
        runBlocking {
            // 403 device_role_forbidden: a monitor-role device may never mutate.
            // Distinct from a dead token so the UI can say "wrong role", not "relink".
            server.enqueue(
                MockResponse().setResponseCode(403).setBody(
                    """{"error":{"code":"device_role_forbidden","message":"nope"}}""",
                ),
            )
            val result = client.setMemberState(server.url("/").toString(), "tok-1", "m1", "drained")
            assertEquals(ActionResult.Forbidden, result)
        }

    @Test
    fun setMemberStateMapsUnauthorizedToItsOwnArm() =
        runBlocking {
            server.enqueue(
                MockResponse().setResponseCode(401).setBody(
                    """{"error":{"code":"unauthorized","message":"bad token"}}""",
                ),
            )
            val result = client.setMemberState(server.url("/").toString(), "dead", "m1", "active")
            assertEquals(ActionResult.Unauthorized, result)
        }

    @Test
    fun setMemberStateServerErrorIsFailure() =
        runBlocking {
            server.enqueue(MockResponse().setResponseCode(500).setBody("boom"))
            val result = client.setMemberState(server.url("/").toString(), "tok-1", "m1", "active")
            assertTrue(result is ActionResult.Failure)
        }

    @Test
    fun setMemberStateMalformedUrlIsFailureNotThrow() =
        runBlocking {
            val result = client.setMemberState("not a url", "tok", "m1", "active")
            assertTrue(result is ActionResult.Failure)
        }

    @Test
    fun syncFleetParsesResultsAndPostsPrimary() =
        runBlocking {
            server.enqueue(
                MockResponse().setBody(
                    """{"primary_id":"m1","results":[{"member_id":"m2","name":"hotel-2","ok":true},""" +
                        """{"member_id":"m3","name":"hotel-3","ok":false,"error":"unreachable"}]}""",
                ),
            )

            val result = client.syncFleet(server.url("/").toString(), "tok-1", "m1")

            assertTrue(result is ActionResult.Success)
            val data = (result as ActionResult.Success).data
            assertEquals("m1", data.primaryId)
            assertEquals(2, data.results.size)
            assertTrue(data.results[0].ok)
            assertFalse(data.results[1].ok)

            val request = server.takeRequest()
            assertEquals("POST", request.method)
            assertEquals("/api/config/sync", request.path)
            assertEquals("Bearer tok-1", request.getHeader("Authorization"))
            assertTrue(request.body.readUtf8().contains("\"primary_id\":\"m1\""))
        }

    @Test
    fun syncFleetMapsForbiddenToItsOwnArm() =
        runBlocking {
            server.enqueue(
                MockResponse().setResponseCode(403).setBody(
                    """{"error":{"code":"device_role_forbidden","message":"nope"}}""",
                ),
            )
            val result = client.syncFleet(server.url("/").toString(), "tok-1", "m1")
            assertEquals(ActionResult.Forbidden, result)
        }

    @Test
    fun syncFleetMapsUnauthorizedToItsOwnArm() =
        runBlocking {
            // A dead device token on sync is a revoke, distinct from the role 403.
            server.enqueue(
                MockResponse().setResponseCode(401).setBody(
                    """{"error":{"code":"unauthorized","message":"bad token"}}""",
                ),
            )
            val result = client.syncFleet(server.url("/").toString(), "dead", "m1")
            assertEquals(ActionResult.Unauthorized, result)
        }

    @Test
    fun syncFleetServerErrorIsFailure() =
        runBlocking {
            server.enqueue(MockResponse().setResponseCode(500).setBody("boom"))
            val result = client.syncFleet(server.url("/").toString(), "tok-1", "m1")
            assertTrue(result is ActionResult.Failure)
        }

    @Test
    fun syncFleetMalformedUrlIsFailureNotThrow() =
        runBlocking {
            val result = client.syncFleet("not a url", "tok", "m1")
            assertTrue(result is ActionResult.Failure)
        }

    @Test
    fun syncFleetEmptyResultsIsSuccessWithEmptyTally() =
        runBlocking {
            // A single-node fleet has nobody to propagate to: Front Desk still
            // answers 200 with an empty results list, which must parse cleanly.
            server.enqueue(MockResponse().setBody("""{"primary_id":"m1","results":[]}"""))
            val result = client.syncFleet(server.url("/").toString(), "tok-1", "m1")
            assertTrue(result is ActionResult.Success)
            assertTrue((result as ActionResult.Success).data.results.isEmpty())
        }

    @Test
    fun setAutoSyncPutsBodyAndParsesEcho() =
        runBlocking {
            // Front Desk echoes the applied config back; that 200 is the ack.
            server.enqueue(MockResponse().setBody("""{"enabled":false,"primary_id":"m1","stale":false}"""))

            val result = client.setAutoSync(server.url("/").toString(), "tok-1", enabled = false, primaryId = "m1")

            assertTrue(result is ActionResult.Success)
            assertEquals(false, (result as ActionResult.Success).data.enabled)

            val request = server.takeRequest()
            assertEquals("PUT", request.method)
            assertEquals("/api/fleet/autosync", request.path)
            assertEquals("Bearer tok-1", request.getHeader("Authorization"))
            val body = request.body.readUtf8()
            assertTrue(body.contains("\"enabled\":false"))
            assertTrue(body.contains("\"primary_id\":\"m1\""))
            // Toggling an unchanged primary carries no confirm token: the empty
            // default is dropped from the body and Front Desk treats absence as "".
            assertTrue(!body.contains("confirm_token"))
        }

    @Test
    fun setAutoSyncMapsForbiddenToItsOwnArm() =
        runBlocking {
            server.enqueue(
                MockResponse().setResponseCode(403).setBody(
                    """{"error":{"code":"device_role_forbidden","message":"nope"}}""",
                ),
            )
            val result = client.setAutoSync(server.url("/").toString(), "tok-1", enabled = true, primaryId = "m1")
            assertEquals(ActionResult.Forbidden, result)
        }

    @Test
    fun setAutoSyncMapsUnauthorizedToItsOwnArm() =
        runBlocking {
            server.enqueue(
                MockResponse().setResponseCode(401).setBody(
                    """{"error":{"code":"unauthorized","message":"bad token"}}""",
                ),
            )
            val result = client.setAutoSync(server.url("/").toString(), "dead", enabled = true, primaryId = "m1")
            assertEquals(ActionResult.Unauthorized, result)
        }

    @Test
    fun setAutoSyncServerErrorIsFailure() =
        runBlocking {
            server.enqueue(MockResponse().setResponseCode(500).setBody("boom"))
            val result = client.setAutoSync(server.url("/").toString(), "tok-1", enabled = true, primaryId = "m1")
            assertTrue(result is ActionResult.Failure)
        }

    @Test
    fun setAutoSyncMalformedUrlIsFailureNotThrow() =
        runBlocking {
            val result = client.setAutoSync("not a url", "tok", enabled = true, primaryId = "m1")
            assertTrue(result is ActionResult.Failure)
        }
}
