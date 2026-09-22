package org.example.client_kmp

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import org.example.client_kmp.monitoring.HttpRequest
import org.example.client_kmp.monitoring.HttpResponse
import org.example.client_kmp.monitoring.MonitoringPlatform
import java.net.HttpURLConnection
import java.net.URL
import java.util.UUID

/** Android transport adapter. All blocking URLConnection work stays on Dispatchers.IO. */
class AndroidMonitoringPlatform : MonitoringPlatform {
    override suspend fun request(request: HttpRequest): HttpResponse = withContext(Dispatchers.IO) {
        val connection = (URL(request.url).openConnection() as HttpURLConnection).apply {
            requestMethod = request.method
            connectTimeout = 8_000
            readTimeout = 8_000
            request.headers.forEach { (name, value) -> setRequestProperty(name, value) }
            if (request.body != null) {
                doOutput = true
                outputStream.bufferedWriter(Charsets.UTF_8).use { it.write(request.body) }
            }
        }
        try {
            val code = connection.responseCode
            val stream = if (code in 200..299) connection.inputStream else connection.errorStream
            HttpResponse(code, stream?.bufferedReader(Charsets.UTF_8)?.use { it.readText() }.orEmpty())
        } finally {
            connection.disconnect()
        }
    }

    override fun newIdempotencyKey(): String = UUID.randomUUID().toString()

    override fun nowMillis(): Long = System.currentTimeMillis()
}
