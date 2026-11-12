package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad(t *testing.T) {
	for _, name := range []string{
		"GITHUB_CLIENT_ID",
		"GITHUB_APP_PRIVATE_KEY",
		"GITHUB_APP_PRIVATE_KEY_FILE",
		"GITHUB_WEBHOOK_SECRET",
		"LISTEN_ADDR",
		"DATA_DIR",
	} {
		t.Setenv(name, "")
	}
	if _, err := Load(); err == nil {
		t.Fatal("missing credentials accepted")
	}
	file := filepath.Join(t.TempDir(), "app.pem")
	if err := os.WriteFile(file, []byte("test-private-key"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GITHUB_CLIENT_ID", "client")
	t.Setenv("GITHUB_APP_PRIVATE_KEY_FILE", file)
	t.Setenv("GITHUB_WEBHOOK_SECRET", "secret")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.PrivateKey != "test-private-key" || c.ListenAddr != ":8080" || c.DataDir != "./data" {
		t.Fatal("incorrect defaults")
	}
	t.Setenv("GITHUB_APP_PRIVATE_KEY", "another-key")
	if _, err := Load(); err == nil {
		t.Fatal("ambiguous private keys accepted")
	}
}
