rootProject.name = "client-kmp"

pluginManagement {
    // The Mini App Gradle plugin is not published yet, so resolve its plugin id from the local SDK
    // build. The settings-level includeBuild below separately substitutes the runtime coordinate.
    includeBuild("../../../My_SDKs/kmp-miniapp-sdk")

    repositories {
        google {
            mavenContent {
                includeGroupAndSubgroups("androidx")
                includeGroupAndSubgroups("com.android")
                includeGroupAndSubgroups("com.google")
            }
        }
        mavenCentral()
        gradlePluginPortal()
    }
}

dependencyResolutionManagement {
    repositories {
        google {
            mavenContent {
                includeGroupAndSubgroups("androidx")
                includeGroupAndSubgroups("com.android")
                includeGroupAndSubgroups("com.google")
            }
        }
        mavenCentral()
    }
}

// The SDK is not published yet, so its public Maven coordinate is resolved from the local
// composite build. After publication this include can be replaced by the artifact repository.
includeBuild("../../../My_SDKs/kmp-miniapp-sdk")

include(":androidApp")
include(":shared")
