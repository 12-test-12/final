package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestRunReturnsConfigurationErrors verifies that a configuration the loader
// rejects stops the process with an error instead of starting a service with
// defaults the operator did not choose.
//
// The successful path of run blocks until a signal arrives, so it is covered
// through the composition root in internal/app, where Run is exercisable with a
// cancellable context.
func TestRunReturnsConfigurationErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env.local")
	if err := os.WriteFile(path, []byte("AUTH_MODE=kerberos\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("BACKEND_CONFIG_FILE", path)

	err := run()
	if err == nil {
		t.Fatal("an unsupported authentication mode started the service")
	}
}
