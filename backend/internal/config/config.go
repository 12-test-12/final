// Package config loads the backend configuration from the environment.
//
// Configuration is environment-only. Secrets never come from checked-in files,
// and the process refuses to start on a value it cannot interpret rather than
// silently falling back to a default that would change safety behaviour.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/BobcGn/final/backend/internal/alert"
	"github.com/BobcGn/final/backend/internal/liveness"
)

// Config is the fully validated process configuration.
type Config struct {
	// Addr is the HTTP listen address.
	Addr string
	// DatabaseURL enables PostgreSQL persistence. Empty selects the in-memory
	// store, which is useful for local development and tests but loses all state
	// on restart and must not be used for a real deployment.
	DatabaseURL string

	// BrokerURL is the MQTT broker address, without a scheme. BrokerTLS enables
	// TLS. An empty BrokerURL runs the HTTP API without MQTT ingress, which is
	// how the API can be exercised before a broker exists.
	BrokerURL     string
	BrokerTLS     bool
	BrokerUser    string
	BrokerPass    string
	MQTTClientID  string
	MQTTKeepAlive time.Duration

	// AuthMode and AuthTokens configure request authentication.
	AuthMode   string
	AuthTokens map[string]string

	// AllowedDevices restricts which device identifiers may publish. Empty means
	// every well-formed identifier is accepted.
	AllowedDevices []string

	// OfflineAfter is the telemetry silence that marks a device offline.
	OfflineAfter time.Duration
	// CommandTTL is how long a control command stays valid.
	CommandTTL time.Duration
	// SweepInterval is how often offline detection and command expiry run.
	SweepInterval time.Duration

	// Alert is the composite fire warning configuration.
	Alert alert.Config

	// MaxWebSocketClients bounds concurrent realtime connections.
	MaxWebSocketClients int

	// LogLevel is the minimum level the process logs at.
	LogLevel slog.Level
}

// Load reads and validates the configuration from the process environment.
func Load() (Config, error) {
	cfg := Config{
		Addr:         envString("BACKEND_ADDR", ":8080"),
		DatabaseURL:  strings.TrimSpace(os.Getenv("DATABASE_URL")),
		BrokerURL:    strings.TrimSpace(os.Getenv("MQTT_BROKER_URL")),
		BrokerUser:   os.Getenv("MQTT_USERNAME"),
		BrokerPass:   os.Getenv("MQTT_PASSWORD"),
		MQTTClientID: envString("MQTT_CLIENT_ID", "lab-backend"),
		Alert:        alert.DefaultConfig(),
		AuthMode:     envString("AUTH_MODE", "none"),
	}

	var err error
	if cfg.MaxWebSocketClients, err = envIntErr("MAX_WS_CLIENTS", 128); err != nil {
		return Config{}, err
	}
	if cfg.BrokerTLS, err = envBool("MQTT_TLS", false); err != nil {
		return Config{}, err
	}
	if cfg.MQTTKeepAlive, err = envDuration("MQTT_KEEPALIVE_SECONDS", 30*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.OfflineAfter, err = envDuration("OFFLINE_AFTER_SECONDS", liveness.DefaultConfig().OfflineAfter); err != nil {
		return Config{}, err
	}
	if cfg.CommandTTL, err = envDuration("COMMAND_TTL_SECONDS", 60*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.SweepInterval, err = envDuration("SWEEP_INTERVAL_SECONDS", 5*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.Alert.Window, err = envDuration("ALERT_WINDOW_SECONDS", cfg.Alert.Window); err != nil {
		return Config{}, err
	}
	if cfg.Alert.MinDuration, err = envDuration("ALERT_MIN_DURATION_SECONDS", cfg.Alert.MinDuration); err != nil {
		return Config{}, err
	}
	if cfg.Alert.RecoveryHold, err = envDuration("ALERT_RECOVERY_HOLD_SECONDS", cfg.Alert.RecoveryHold); err != nil {
		return Config{}, err
	}
	if cfg.Alert.MinSamples, err = envIntErr("ALERT_MIN_SAMPLES", cfg.Alert.MinSamples); err != nil {
		return Config{}, err
	}
	if cfg.Alert.GasAdcRiseThreshold, err = envIntErr("ALERT_GAS_RISE_ADC", cfg.Alert.GasAdcRiseThreshold); err != nil {
		return Config{}, err
	}
	if cfg.Alert.TemperatureRateThresholdCPerMinute, err = envFloat("ALERT_TEMP_RATE_C_PER_MIN", cfg.Alert.TemperatureRateThresholdCPerMinute); err != nil {
		return Config{}, err
	}
	if cfg.LogLevel, err = parseLevel(envString("LOG_LEVEL", "info")); err != nil {
		return Config{}, err
	}
	if cfg.AuthTokens, err = parseTokens(os.Getenv("AUTH_TOKENS")); err != nil {
		return Config{}, err
	}
	cfg.AllowedDevices = splitList(os.Getenv("DEVICE_ALLOWLIST"))

	return cfg, cfg.Validate()
}

// Validate rejects combinations that would run in an unsafe configuration.
func (c Config) Validate() error {
	if c.Addr == "" {
		return errors.New("config: BACKEND_ADDR must not be empty")
	}
	switch c.AuthMode {
	case "none", "bearer":
	default:
		return fmt.Errorf("config: AUTH_MODE %q must be none or bearer", c.AuthMode)
	}
	if c.AuthMode == "bearer" && len(c.AuthTokens) == 0 {
		return errors.New("config: AUTH_MODE=bearer requires at least one AUTH_TOKENS entry")
	}
	if c.MQTTKeepAlive <= 0 {
		return fmt.Errorf("config: MQTT_KEEPALIVE_SECONDS must be positive, got %s", c.MQTTKeepAlive)
	}
	if c.MQTTClientID == "" || len(c.MQTTClientID) > 23 {
		return fmt.Errorf("config: MQTT_CLIENT_ID must be 1 to 23 characters, got %q", c.MQTTClientID)
	}
	if c.MaxWebSocketClients < 0 {
		return fmt.Errorf("config: MAX_WS_CLIENTS must not be negative, got %d", c.MaxWebSocketClients)
	}
	if c.SweepInterval <= 0 {
		return fmt.Errorf("config: SWEEP_INTERVAL_SECONDS must be positive, got %s", c.SweepInterval)
	}
	if c.CommandTTL <= 0 {
		return fmt.Errorf("config: COMMAND_TTL_SECONDS must be positive, got %s", c.CommandTTL)
	}
	if err := c.Liveness().Validate(); err != nil {
		return fmt.Errorf("config: %w", err)
	}
	if err := c.Alert.Validate(); err != nil {
		return fmt.Errorf("config: %w", err)
	}
	return nil
}

// Liveness derives the tracker configuration from the offline window.
//
// The report interval stays at the device contract's five seconds rather than
// being derived from OfflineAfter, so that a deployment which raises the offline
// window does not silently also stretch what counts as one missed report.
func (c Config) Liveness() liveness.Config {
	return liveness.Config{ReportInterval: liveness.DefaultConfig().ReportInterval, OfflineAfter: c.OfflineAfter}
}

// UsesMemoryStore reports whether the process will run without persistence.
func (c Config) UsesMemoryStore() bool { return c.DatabaseURL == "" }

// UsesBroker reports whether MQTT ingress is enabled.
func (c Config) UsesBroker() bool { return c.BrokerURL != "" }

// Redacted returns the configuration with secrets removed, for logging.
func (c Config) Redacted() map[string]any {
	return map[string]any{
		"addr":                  c.Addr,
		"database":              describePresence(c.DatabaseURL != ""),
		"broker":                describePresence(c.BrokerURL != ""),
		"broker_url":            redactUserInfo(c.BrokerURL),
		"broker_tls":            c.BrokerTLS,
		"broker_credentials":    describePresence(c.BrokerUser != "" || c.BrokerPass != ""),
		"mqtt_client_id":        c.MQTTClientID,
		"auth_mode":             c.AuthMode,
		"auth_token_count":      len(c.AuthTokens),
		"allowed_devices":       c.AllowedDevices,
		"offline_after_seconds": c.OfflineAfter.Seconds(),
		"command_ttl_seconds":   c.CommandTTL.Seconds(),
		"alert_window_seconds":  c.Alert.Window.Seconds(),
		"alert_min_samples":     c.Alert.MinSamples,
		"log_level":             c.LogLevel.String(),
	}
}

// redactUserInfo removes any credentials embedded in a URL-shaped value. A
// broker URL is not secret by itself, but `user:password@host` inside one is.
func redactUserInfo(raw string) string {
	at := strings.LastIndex(raw, "@")
	if at < 0 {
		return raw
	}
	return "***@" + raw[at+1:]
}

// describePresence reports whether a secret is configured without revealing it.
func describePresence(present bool) string {
	if present {
		return "configured"
	}
	return "not configured"
}

// envString reads a string variable with a default.
func envString(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

// envIntErr reads an integer variable, reporting a malformed value rather than
// falling back to a default: a typo in a numeric setting must not silently change
// behaviour.
func envIntErr(key string, fallback int) (int, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("config: %s must be an integer, got %q", key, raw)
	}
	return value, nil
}

// envFloat reads a float variable with a default.
func envFloat(key string, fallback float64) (float64, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, fmt.Errorf("config: %s must be a number, got %q", key, raw)
	}
	return value, nil
}

// envBool reads a boolean variable with a default.
func envBool(key string, fallback bool) (bool, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("config: %s must be a boolean, got %q", key, raw)
	}
	return value, nil
}

// envDuration reads a whole-second duration variable with a default.
func envDuration(key string, fallback time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback, nil
	}
	seconds, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("config: %s must be a whole number of seconds, got %q", key, raw)
	}
	return time.Duration(seconds) * time.Second, nil
}

// parseTokens parses the comma-separated `token:actor` credential list.
func parseTokens(raw string) (map[string]string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, nil
	}
	tokens := make(map[string]string)
	for _, entry := range strings.Split(trimmed, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		token, actor, found := strings.Cut(entry, ":")
		token = strings.TrimSpace(token)
		actor = strings.TrimSpace(actor)
		if !found || token == "" || actor == "" {
			// The actor is required, not decorative: it is what the command audit
			// trail records, and an anonymous controller is not auditable.
			return nil, fmt.Errorf("config: AUTH_TOKENS entries must be token:actor, got %q", entry)
		}
		tokens[token] = actor
	}
	return tokens, nil
}

// splitList parses a comma-separated list, dropping empty entries.
func splitList(raw string) []string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil
	}
	var values []string
	for _, entry := range strings.Split(trimmed, ",") {
		if entry = strings.TrimSpace(entry); entry != "" {
			values = append(values, entry)
		}
	}
	return values
}

// parseLevel converts a log level name.
func parseLevel(raw string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "debug":
		return slog.LevelDebug, nil
	case "info", "":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("config: LOG_LEVEL %q must be debug, info, warn or error", raw)
	}
}
