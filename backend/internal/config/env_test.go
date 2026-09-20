package config_test

import (
	"encoding/json"
	"os"
	"testing"
)

// envKeys lists every variable the loader reads, so a test can start from a
// known-empty environment instead of inheriting whatever the developer has
// exported.
var envKeys = []string{
	"BACKEND_ADDR",
	"DATABASE_URL",
	"MQTT_BROKER_URL",
	"MQTT_USERNAME",
	"MQTT_PASSWORD",
	"MQTT_CLIENT_ID",
	"MQTT_TLS",
	"MQTT_KEEPALIVE_SECONDS",
	"AUTH_MODE",
	"AUTH_TOKENS",
	"DEVICE_ALLOWLIST",
	"OFFLINE_AFTER_SECONDS",
	"COMMAND_TTL_SECONDS",
	"SWEEP_INTERVAL_SECONDS",
	"ALERT_WINDOW_SECONDS",
	"ALERT_MIN_SAMPLES",
	"ALERT_MIN_DURATION_SECONDS",
	"ALERT_GAS_RISE_ADC",
	"ALERT_TEMP_RATE_C_PER_MIN",
	"ALERT_RECOVERY_HOLD_SECONDS",
	"MAX_WS_CLIENTS",
	"LOG_LEVEL",
}

// clearAll unsets every variable the loader reads and restores them afterwards.
func clearAll(t *testing.T) {
	t.Helper()

	for _, key := range envKeys {
		if original, present := os.LookupEnv(key); present {
			value := original
			t.Cleanup(func() { _ = os.Setenv(key, value) })
		}
		if err := os.Unsetenv(key); err != nil {
			t.Fatalf("unset %s: %v", key, err)
		}
	}
}

// setenv sets one variable for the remainder of the test.
func setenv(key, value string) error { return os.Setenv(key, value) }

// sprint renders a value for a substring assertion.
func sprint(value any) string {
	raw, err := json.Marshal(value)
	if err != nil {
		// The value holds only JSON-safe primitives.
		panic(err)
	}
	return string(raw)
}
