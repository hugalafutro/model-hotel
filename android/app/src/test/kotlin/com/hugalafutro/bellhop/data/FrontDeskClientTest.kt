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
}
