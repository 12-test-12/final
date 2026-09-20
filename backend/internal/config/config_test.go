package config_test

import (
	"strings"
	"testing"
	"time"

	"github.com/BobcGn/final/backend/internal/config"
)

// setEnv sets the environment for one test and restores it afterwards.
func setEnv(t *testing.T, values map[string]string) {
	t.Helper()

	for key, value := range values {
		if err := setenv(key, value); err != nil {
			t.Fatalf("set %s: %v", key, err)
		}
	}
}

// TestLoadDefaults verifies the values a bare environment produces.
func TestLoadDefaults(t *testing.T) {
	clearAll(t)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if cfg.Addr != ":8080" {
		t.Fatalf("Addr = %q", cfg.Addr)
	}
	if !cfg.UsesMemoryStore() {
		t.Fatal("a bare environment selected a database")
	}
	if cfg.UsesBroker() {
		t.Fatal("a bare environment selected a broker")
	}
	if cfg.AuthMode != "none" {
		t.Fatalf("AuthMode = %q", cfg.AuthMode)
	}
	if cfg.MQTTClientID != "lab-backend" {
		t.Fatalf("MQTTClientID = %q", cfg.MQTTClientID)
	}
	if cfg.MQTTKeepAlive != 30*time.Second {
		t.Fatalf("MQTTKeepAlive = %s", cfg.MQTTKeepAlive)
	}
	if cfg.OfflineAfter != 15*time.Second {
		t.Fatalf("OfflineAfter = %s", cfg.OfflineAfter)
	}
	if cfg.CommandTTL != 60*time.Second {
		t.Fatalf("CommandTTL = %s", cfg.CommandTTL)
	}
	// The liveness relationship the contract fixes must hold in the defaults.
	if err := cfg.Liveness().Validate(); err != nil {
		t.Fatalf("the default liveness configuration is invalid: %v", err)
	}
	if err := cfg.Alert.Validate(); err != nil {
		t.Fatalf("the default alert configuration is invalid: %v", err)
	}
}

// TestLoadFromEnvironment verifies that every documented variable is read.
func TestLoadFromEnvironment(t *testing.T) {
	clearAll(t)
	setEnv(t, map[string]string{
		"BACKEND_ADDR":                ":9999",
		"DATABASE_URL":                "postgres://localhost/lab",
		"MQTT_BROKER_URL":             "emqx.lab:1883",
		"MQTT_USERNAME":               "backend",
		"MQTT_PASSWORD":               "secret",
		"MQTT_CLIENT_ID":              "lab-backend-2",
		"MQTT_TLS":                    "true",
		"MQTT_KEEPALIVE_SECONDS":      "45",
		"AUTH_MODE":                   "bearer",
		"AUTH_TOKENS":                 "tok1:alice,tok2:bob",
		"DEVICE_ALLOWLIST":            "MCU001, MCU002",
		"OFFLINE_AFTER_SECONDS":       "30",
		"COMMAND_TTL_SECONDS":         "90",
		"SWEEP_INTERVAL_SECONDS":      "10",
		"ALERT_WINDOW_SECONDS":        "120",
		"ALERT_MIN_SAMPLES":           "8",
		"ALERT_MIN_DURATION_SECONDS":  "30",
		"ALERT_GAS_RISE_ADC":          "200",
		"ALERT_TEMP_RATE_C_PER_MIN":   "4.5",
		"ALERT_RECOVERY_HOLD_SECONDS": "60",
		"MAX_WS_CLIENTS":              "16",
		"LOG_LEVEL":                   "debug",
	})

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if cfg.Addr != ":9999" || cfg.DatabaseURL != "postgres://localhost/lab" {
		t.Fatalf("addr or database not read: %+v", cfg)
	}
	if !cfg.UsesBroker() || !cfg.BrokerTLS {
		t.Fatal("the broker settings were not read")
	}
	if cfg.MQTTKeepAlive != 45*time.Second {
		t.Fatalf("MQTTKeepAlive = %s", cfg.MQTTKeepAlive)
	}
	if cfg.AuthMode != "bearer" || len(cfg.AuthTokens) != 2 {
		t.Fatalf("auth settings = %q / %v", cfg.AuthMode, cfg.AuthTokens)
	}
	if cfg.AuthTokens["tok1"] != "alice" {
		t.Fatalf("token mapping = %v", cfg.AuthTokens)
	}
	if len(cfg.AllowedDevices) != 2 || cfg.AllowedDevices[1] != "MCU002" {
		t.Fatalf("allowlist = %v", cfg.AllowedDevices)
	}
	if cfg.OfflineAfter != 30*time.Second || cfg.CommandTTL != 90*time.Second {
		t.Fatalf("timings = %s / %s", cfg.OfflineAfter, cfg.CommandTTL)
	}
	if cfg.Alert.Window != 120*time.Second || cfg.Alert.MinSamples != 8 || cfg.Alert.MinDuration != 30*time.Second {
		t.Fatalf("alert window = %+v", cfg.Alert)
	}
	if cfg.Alert.GasAdcRiseThreshold != 200 || cfg.Alert.TemperatureRateThresholdCPerMinute != 4.5 {
		t.Fatalf("alert thresholds = %+v", cfg.Alert)
	}
	if cfg.MaxWebSocketClients != 16 {
		t.Fatalf("MaxWebSocketClients = %d", cfg.MaxWebSocketClients)
	}
	// The offline window moved but the report interval did not, so the
	// three-missed-report relationship still holds at the device's cadence.
	if cfg.Liveness().ReportInterval != 5*time.Second {
		t.Fatalf("report interval = %s, want the device cadence", cfg.Liveness().ReportInterval)
	}
}

// TestLoadRejectsUnsafeAndMalformedValues covers the validation rules.
func TestLoadRejectsUnsafeAndMalformedValues(t *testing.T) {
	cases := map[string]map[string]string{
		"unknown auth mode":           {"AUTH_MODE": "kerberos"},
		"bearer without tokens":       {"AUTH_MODE": "bearer"},
		"token without an actor":      {"AUTH_MODE": "bearer", "AUTH_TOKENS": "tok1"},
		"empty actor":                 {"AUTH_MODE": "bearer", "AUTH_TOKENS": "tok1:"},
		"offline below three reports": {"OFFLINE_AFTER_SECONDS": "10"},
		"zero command ttl":            {"COMMAND_TTL_SECONDS": "0"},
		"zero sweep interval":         {"SWEEP_INTERVAL_SECONDS": "0"},
		"non-numeric limit":           {"MAX_WS_CLIENTS": "many"},
		"non-boolean tls":             {"MQTT_TLS": "maybe"},
		"non-numeric duration":        {"OFFLINE_AFTER_SECONDS": "soon"},
		"alert min samples below two": {"ALERT_MIN_SAMPLES": "1"},
		"alert window of zero":        {"ALERT_WINDOW_SECONDS": "0"},
		"alert slope of zero":         {"ALERT_TEMP_RATE_C_PER_MIN": "0"},
		"alert gas rise above range":  {"ALERT_GAS_RISE_ADC": "5000"},
		"unknown log level":           {"LOG_LEVEL": "verbose"},
		"client id too long":          {"MQTT_CLIENT_ID": "0123456789012345678901234"},
	}
	for name, values := range cases {
		t.Run(name, func(t *testing.T) {
			clearAll(t)
			setEnv(t, values)
			if _, err := config.Load(); err == nil {
				t.Fatalf("%s was accepted", name)
			}
		})
	}
}

// TestRedactedHidesSecrets verifies that the log line cannot leak a credential.
func TestRedactedHidesSecrets(t *testing.T) {
	clearAll(t)
	setEnv(t, map[string]string{
		"DATABASE_URL":    "postgres://user:hunter2@localhost/lab",
		"MQTT_BROKER_URL": "emqx.lab:1883",
		"MQTT_USERNAME":   "backend",
		"MQTT_PASSWORD":   "hunter2",
		"AUTH_MODE":       "bearer",
		"AUTH_TOKENS":     "supersecret:alice",
	})

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	rendered := sprint(cfg.Redacted())

	for _, secret := range []string{"hunter2", "supersecret", "user:", "postgres://"} {
		if strings.Contains(rendered, secret) {
			t.Fatalf("the redacted configuration leaks %q: %s", secret, rendered)
		}
	}
	// Presence is still reported, so an operator can tell whether a credential
	// was configured without seeing it.
	if !strings.Contains(rendered, "configured") {
		t.Fatalf("the redacted configuration hides presence too: %s", rendered)
	}
}

// TestAllowlistAndTokensIgnoreBlankEntries verifies the list parsers.
func TestAllowlistAndTokensIgnoreBlankEntries(t *testing.T) {
	clearAll(t)
	setEnv(t, map[string]string{
		"DEVICE_ALLOWLIST": " MCU001 , , MCU002 ,",
		"AUTH_MODE":        "bearer",
		"AUTH_TOKENS":      " tok1:alice , , tok2:bob ",
	})

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(cfg.AllowedDevices) != 2 {
		t.Fatalf("allowlist = %v", cfg.AllowedDevices)
	}
	if len(cfg.AuthTokens) != 2 {
		t.Fatalf("tokens = %v", cfg.AuthTokens)
	}
	if cfg.AuthTokens["tok2"] != "bob" {
		t.Fatalf("a token with surrounding spaces was not trimmed: %v", cfg.AuthTokens)
	}
}
