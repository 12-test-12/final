// Package config loads the backend configuration from a local dotenv file.
package config

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/BobcGn/final/backend/internal/alert"
	"github.com/BobcGn/final/backend/internal/liveness"
)

const (
	// DefaultFile is the local, git-ignored configuration read at startup.
	DefaultFile = ".env.local"
	// FilePathEnv selects another dotenv file without overriding its values.
	FilePathEnv = "BACKEND_CONFIG_FILE"
)

type valueSource func(string) string

var knownKeys = map[string]struct{}{
	"BACKEND_ADDR": {}, "DATABASE_URL": {}, "MQTT_BROKER_URL": {},
	"MQTT_USERNAME": {}, "MQTT_PASSWORD": {}, "MQTT_CLIENT_ID": {},
	"MQTT_TLS": {}, "MQTT_KEEPALIVE_SECONDS": {}, "AUTH_MODE": {},
	"AUTH_TOKENS": {}, "DEVICE_ALLOWLIST": {}, "OFFLINE_AFTER_SECONDS": {},
	"COMMAND_TTL_SECONDS": {}, "SWEEP_INTERVAL_SECONDS": {},
	"ALERT_WINDOW_SECONDS": {}, "ALERT_MIN_SAMPLES": {},
	"ALERT_MIN_DURATION_SECONDS": {}, "ALERT_GAS_RISE_ADC": {},
	"ALERT_TEMP_RATE_C_PER_MIN": {}, "ALERT_RECOVERY_HOLD_SECONDS": {},
	"MAX_WS_CLIENTS": {}, "LOG_LEVEL": {},
}

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

// Load reads and validates .env.local, or the path selected by
// BACKEND_CONFIG_FILE. Configuration values come from the file, not from the
// surrounding shell environment.
func Load() (Config, error) {
	path := strings.TrimSpace(os.Getenv(FilePathEnv))
	if path == "" {
		path = DefaultFile
	}
	return LoadFile(path)
}

// LoadFile reads and validates one dotenv configuration file.
func LoadFile(path string) (Config, error) {
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Config{}, fmt.Errorf("config: %s not found; copy .env.example to .env.local", path)
		}
		return Config{}, fmt.Errorf("config: open %s: %w", path, err)
	}
	defer file.Close()

	values, err := parseDotEnv(file)
	if err != nil {
		return Config{}, fmt.Errorf("config: parse %s: %w", path, err)
	}
	return loadFrom(func(key string) string { return values[key] })
}

// LoadEnvironment is retained for tests and embedding callers that explicitly
// provide process environment configuration. The service executable uses Load.
func LoadEnvironment() (Config, error) {
	return loadFrom(os.Getenv)
}

func loadFrom(source valueSource) (Config, error) {
	cfg := Config{
		Addr:         sourceString(source, "BACKEND_ADDR", ":8080"),
		DatabaseURL:  strings.TrimSpace(source("DATABASE_URL")),
		BrokerURL:    strings.TrimSpace(source("MQTT_BROKER_URL")),
		BrokerUser:   source("MQTT_USERNAME"),
		BrokerPass:   source("MQTT_PASSWORD"),
		MQTTClientID: sourceString(source, "MQTT_CLIENT_ID", "lab-backend"),
		Alert:        alert.DefaultConfig(),
		AuthMode:     sourceString(source, "AUTH_MODE", "none"),
	}

	var err error
	if cfg.MaxWebSocketClients, err = sourceInt(source, "MAX_WS_CLIENTS", 128); err != nil {
		return Config{}, err
	}
	if cfg.BrokerTLS, err = sourceBool(source, "MQTT_TLS", false); err != nil {
		return Config{}, err
	}
	if cfg.MQTTKeepAlive, err = sourceDuration(source, "MQTT_KEEPALIVE_SECONDS", 30*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.OfflineAfter, err = sourceDuration(source, "OFFLINE_AFTER_SECONDS", liveness.DefaultConfig().OfflineAfter); err != nil {
		return Config{}, err
	}
	if cfg.CommandTTL, err = sourceDuration(source, "COMMAND_TTL_SECONDS", 60*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.SweepInterval, err = sourceDuration(source, "SWEEP_INTERVAL_SECONDS", 5*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.Alert.Window, err = sourceDuration(source, "ALERT_WINDOW_SECONDS", cfg.Alert.Window); err != nil {
		return Config{}, err
	}
	if cfg.Alert.MinDuration, err = sourceDuration(source, "ALERT_MIN_DURATION_SECONDS", cfg.Alert.MinDuration); err != nil {
		return Config{}, err
	}
	if cfg.Alert.RecoveryHold, err = sourceDuration(source, "ALERT_RECOVERY_HOLD_SECONDS", cfg.Alert.RecoveryHold); err != nil {
		return Config{}, err
	}
	if cfg.Alert.MinSamples, err = sourceInt(source, "ALERT_MIN_SAMPLES", cfg.Alert.MinSamples); err != nil {
		return Config{}, err
	}
	if cfg.Alert.GasAdcRiseThreshold, err = sourceInt(source, "ALERT_GAS_RISE_ADC", cfg.Alert.GasAdcRiseThreshold); err != nil {
		return Config{}, err
	}
	if cfg.Alert.TemperatureRateThresholdCPerMinute, err = sourceFloat(source, "ALERT_TEMP_RATE_C_PER_MIN", cfg.Alert.TemperatureRateThresholdCPerMinute); err != nil {
		return Config{}, err
	}
	if cfg.LogLevel, err = parseLevel(sourceString(source, "LOG_LEVEL", "info")); err != nil {
		return Config{}, err
	}
	if cfg.AuthTokens, err = parseTokens(source("AUTH_TOKENS")); err != nil {
		return Config{}, err
	}
	cfg.AllowedDevices = splitList(source("DEVICE_ALLOWLIST"))

	return cfg, cfg.Validate()
}

func parseDotEnv(reader io.Reader) (map[string]string, error) {
	values := make(map[string]string)
	scanner := bufio.NewScanner(reader)
	for lineNumber := 1; scanner.Scan(); lineNumber++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		key, raw, found := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if !found || key == "" {
			return nil, fmt.Errorf("line %d must be KEY=VALUE", lineNumber)
		}
		if _, duplicate := values[key]; duplicate {
			return nil, fmt.Errorf("line %d duplicates %s", lineNumber, key)
		}
		if _, known := knownKeys[key]; !known {
			return nil, fmt.Errorf("line %d contains unknown key %s", lineNumber, key)
		}
		value, err := parseDotEnvValue(strings.TrimSpace(raw))
		if err != nil {
			return nil, fmt.Errorf("line %d %s: %w", lineNumber, key, err)
		}
		values[key] = value
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return values, nil
}

func parseDotEnvValue(raw string) (string, error) {
	if raw == "" {
		return "", nil
	}
	if raw[0] == '\'' {
		if len(raw) < 2 || raw[len(raw)-1] != '\'' {
			return "", errors.New("unterminated single-quoted value")
		}
		return raw[1 : len(raw)-1], nil
	}
	if raw[0] == '"' {
		value, err := strconv.Unquote(raw)
		if err != nil {
			return "", fmt.Errorf("invalid quoted value: %w", err)
		}
		return value, nil
	}
	return raw, nil
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

// sourceString reads a string setting with a default.
func sourceString(source valueSource, key, fallback string) string {
	if value := strings.TrimSpace(source(key)); value != "" {
		return value
	}
	return fallback
}

// sourceInt reads an integer setting, reporting a malformed value rather than
// falling back to a default: a typo in a numeric setting must not silently change
// behaviour.
func sourceInt(source valueSource, key string, fallback int) (int, error) {
	raw := strings.TrimSpace(source(key))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("config: %s must be an integer, got %q", key, raw)
	}
	return value, nil
}

// sourceFloat reads a float setting with a default.
func sourceFloat(source valueSource, key string, fallback float64) (float64, error) {
	raw := strings.TrimSpace(source(key))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, fmt.Errorf("config: %s must be a number, got %q", key, raw)
	}
	return value, nil
}

// sourceBool reads a boolean setting with a default.
func sourceBool(source valueSource, key string, fallback bool) (bool, error) {
	raw := strings.TrimSpace(source(key))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("config: %s must be a boolean, got %q", key, raw)
	}
	return value, nil
}

// sourceDuration reads a whole-second duration setting with a default.
func sourceDuration(source valueSource, key string, fallback time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(source(key))
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
