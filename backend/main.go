package main

import (
	"log"
	"net/http"
	"os"
	"time"
)

func main() {
	addr := backendAddress()
	server := newServer(addr)

	log.Printf("backend listening on %s", addr)
	log.Fatal(server.ListenAndServe())
}

func backendAddress() string {
	addr := os.Getenv("BACKEND_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	return addr
}

func newServer(addr string) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           newRouter(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
}
