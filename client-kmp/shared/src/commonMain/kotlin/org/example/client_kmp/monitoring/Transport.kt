package org.example.client_kmp.monitoring

/** HTTP request kept deliberately small so Android and MiniApp can share the same client. */
data class HttpRequest(
    val url: String,
    val method: String = "GET",
    val headers: Map<String, String> = emptyMap(),
    val body: String? = null,
)

/** Completed HTTP response. Non-2xx status codes remain responses and are mapped by the client. */
data class HttpResponse(val statusCode: Int, val body: String)

/** Platform boundary for network I/O, idempotency-key generation and the clock. */
interface MonitoringPlatform {
    suspend fun request(request: HttpRequest): HttpResponse

    /** Must return a fresh value for every control request. */
    fun newIdempotencyKey(): String

    /**
     * Wall-clock milliseconds since the Unix epoch.
     *
     * The shared layer needs it to turn a trend window into the contract's
     * absolute `from`/`to` bounds. It lives on the platform boundary rather than
     * being read from a global so that a test can pin the clock and assert the
     * exact query the shared layer builds, instead of asserting only its shape.
     */
    fun nowMillis(): Long
}

/** Structured failure exposed consistently to Android and MiniApp hosts. */
class MonitoringException(
    val code: String,
    override val message: String,
    val statusCode: Int = 0,
) : Exception(message)
