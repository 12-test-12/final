package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealth(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	response := httptest.NewRecorder()

	newRouter().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if contentType := response.Header().Get("Content-Type"); contentType != "application/json; charset=utf-8" {
		t.Fatalf("Content-Type = %q", contentType)
	}

	var body map[string]string
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["status"] != "ok" {
		t.Fatalf("status body = %q, want ok", body["status"])
	}
}

func TestDocumentedRouteIsRegistered(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/v1/devices/MCU001/status", nil)
	response := httptest.NewRecorder()

	newRouter().ServeHTTP(response, request)

	if response.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNotImplemented)
	}
}

func TestRouteRejectsWrongMethod(t *testing.T) {
	request := httptest.NewRequest(http.MethodDelete, "/api/v1/devices/MCU001/thresholds", nil)
	response := httptest.NewRecorder()

	newRouter().ServeHTTP(response, request)

	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusMethodNotAllowed)
	}
}

func TestWriteJSONHandlesMarshalFailure(t *testing.T) {
	response := httptest.NewRecorder()

	writeJSON(response, http.StatusOK, make(chan int))

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
	}
}
