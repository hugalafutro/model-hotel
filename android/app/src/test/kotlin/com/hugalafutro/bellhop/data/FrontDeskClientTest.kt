package com.hugalafutro.bellhop.data

import kotlinx.coroutines.runBlocking
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.After
import org.junit.Assert.assertEquals
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
}
