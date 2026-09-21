import org.jetbrains.kotlin.gradle.dsl.JvmTarget
import org.jetbrains.kotlin.gradle.plugin.KotlinPlatformType

plugins {
    alias(libs.plugins.kotlinMultiplatform)
    alias(libs.plugins.kotlinSerialization)
    alias(libs.plugins.kover)
    id("io.github.bobcgn.miniapp") version "0.1.1"
    alias(libs.plugins.androidMultiplatformLibrary)
    alias(libs.plugins.composeMultiplatform)
    alias(libs.plugins.composeCompiler)
}

kotlin {
    listOf(
        iosArm64(),
        iosSimulatorArm64()
    ).forEach { iosTarget ->
        iosTarget.binaries.framework {
            baseName = "Shared"
            isStatic = true
        }
    }

    android {
        namespace = "org.example.client_kmp.shared"
        compileSdk = libs.versions.android.compileSdk.get().toInt()
        minSdk = libs.versions.android.minSdk.get().toInt()

        compilerOptions {
            jvmTarget = JvmTarget.JVM_11
        }
        androidResources {
            enable = true
        }
        withHostTest {
            isIncludeAndroidResources = true
        }
        withDeviceTestBuilder {
            sourceSetTreeName = "test"
        }.configure {
            instrumentationRunner = "androidx.test.runner.AndroidJUnitRunner"
        }
    }

    sourceSets {
        androidMain.dependencies {
            implementation(libs.kotlinx.coroutines.core)
            implementation(libs.compose.uiToolingPreview)
            implementation(libs.compose.uiTooling)
            implementation(libs.compose.runtime)
            implementation(libs.compose.foundation)
            implementation(libs.compose.material3)
            implementation(libs.compose.ui)
            implementation(libs.compose.components.resources)
            implementation(libs.androidx.lifecycle.viewmodelCompose)
            implementation(libs.androidx.lifecycle.runtimeCompose)
        }
        commonMain.dependencies {
            implementation(libs.kotlinx.coroutines.core)
            implementation(libs.kotlinx.serialization.json)
        }
        commonTest.dependencies {
            implementation(libs.kotlin.test)
            implementation(libs.kotlinx.coroutines.test)
        }
        iosMain.dependencies {
            implementation(libs.compose.runtime)
            implementation(libs.compose.foundation)
            implementation(libs.compose.material3)
            implementation(libs.compose.ui)
            implementation(libs.compose.components.resources)
            implementation(libs.androidx.lifecycle.viewmodelCompose)
            implementation(libs.androidx.lifecycle.runtimeCompose)
        }
    }
}

dependencies {
    androidRuntimeClasspath(libs.compose.uiTooling)
}

// MiniApp is a Kotlin/JS host with native WXML/WXSS UI. Applying the Compose
// compiler to JS would require the forbidden browser renderer runtime even
// though miniappMain contains no composables.
composeCompiler {
    targetKotlinPlatforms.set(KotlinPlatformType.values().filterNot { it == KotlinPlatformType.js })
}

/**
 * Coverage for the shared business rules that Android and the MiniApp both
 * consume.
 *
 * Scoped to `monitoring` on purpose: Compose screens, the WXML host and the
 * generated MiniApp bundle are host code with no shared rules to cover, and
 * including them would report a number that says nothing about the contract
 * logic. Compiler-generated nested classes (serializers, lambdas, data-class
 * helpers) are excluded so the figure reflects hand-written rule coverage.
 */
kover {
    reports {
        filters {
            includes {
                classes("org.example.client_kmp.monitoring.*")
            }
            excludes {
                classes("org.example.client_kmp.monitoring.*\$*")
            }
        }
        verify {
            rule("shared business logic line coverage") {
                bound {
                    minValue = 80
                }
            }
        }
    }
}
