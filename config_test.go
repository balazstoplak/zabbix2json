package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadAppliesDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "c.yaml")
	if err := os.WriteFile(path, []byte("url: https://zbx/api_jsonrpc.php\ntoken: abc\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.URL != "https://zbx/api_jsonrpc.php" || cfg.Token != "abc" {
		t.Fatalf("bad url/token: %+v", cfg)
	}
	if cfg.Listen != ":8080" || cfg.Timeout != 10*time.Second || cfg.Version != 20110619 {
		t.Fatalf("defaults not applied: %+v", cfg)
	}
}

func TestLoadParsesTimeoutAndEnvOverride(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "c.yaml")
	body := "url: https://a\ntoken: t\nlisten: \":9000\"\ntimeout: 3s\nversion: 42\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ZABBIX_URL", "https://override")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.URL != "https://override" {
		t.Fatalf("env override failed: %q", cfg.URL)
	}
	if cfg.Listen != ":9000" || cfg.Timeout != 3*time.Second || cfg.Version != 42 {
		t.Fatalf("yaml values not parsed: %+v", cfg)
	}
}
