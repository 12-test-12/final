package org.example.client_kmp

interface Platform {
    val name: String
}

expect fun getPlatform(): Platform