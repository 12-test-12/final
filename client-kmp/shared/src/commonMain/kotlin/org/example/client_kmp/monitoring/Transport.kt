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

/** Platform boundary for network I/O and idempotency-key generation. */
interface MonitoringPlatform {
    suspend fun request(request: HttpRequest): HttpResponse

    /** Must return a fresh value for every control request. */
    fun newIdempotencyKey(): String
}

/** Structured failure exposed consistently to Android and MiniApp hosts. */
class MonitoringException(
    val code: String,
    override val message: String,
    val statusCode: Int = 0,
) : Exception(message)
