package main

import (
	"encoding/json"
	"net/http"
)

type errorEnvelope struct {
	Error apiError `json:"error"`
}

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func newRouter() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", handleHealth)
	mux.HandleFunc("GET /api/v1/devices/{deviceId}/status", notImplemented)
	mux.HandleFunc("GET /api/v1/devices/{deviceId}/telemetry/latest", notImplemented)
	mux.HandleFunc("GET /api/v1/devices/{deviceId}/telemetry", notImplemented)
	mux.HandleFunc("GET /api/v1/devices/{deviceId}/alerts", notImplemented)
	mux.HandleFunc("GET /api/v1/devices/{deviceId}/thresholds", notImplemented)
	mux.HandleFunc("PUT /api/v1/devices/{deviceId}/thresholds", notImplemented)
	mux.HandleFunc("POST /api/v1/devices/{deviceId}/commands/mute", notImplemented)
	mux.HandleFunc("GET /ws/v1/devices/{deviceId}/telemetry", notImplemented)

	return mux
}

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func notImplemented(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusNotImplemented, errorEnvelope{
		Error: apiError{
			Code:    "not_implemented",
			Message: "route contract exists; implementation is scheduled for a later phase",
		},
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	payload, err := json.Marshal(value)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(append(payload, '\n'))
}
