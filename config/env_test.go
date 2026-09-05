package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ShanilKoshitha/goforge/config"
)

func TestReaderAccumulatesConfigurationErrors(t *testing.T) {
	reader := config.FromMap(map[string]string{
		"PORT":    "many",
		"ENABLED": "perhaps",
		"TIMEOUT": "eventually",
	})
	reader.Required("SECRET")
	reader.Int("PORT", 8080)
	reader.Bool("ENABLED", false)
	reader.Duration("TIMEOUT", time.Second)
	err := reader.Err()
	if err == nil {
		t.Fatal("expected a joined configuration error")
	}
	for _, key := range []string{"SECRET", "PORT", "ENABLED", "TIMEOUT"} {
		if !strings.Contains(err.Error(), key) {
			t.Errorf("expected error to mention %s: %v", key, err)
		}
	}
}

func TestReaderUsesTypedDefaults(t *testing.T) {
	reader := config.FromMap(nil)
	if got := reader.String("NAME", "forge"); got != "forge" {
		t.Fatalf("got %q", got)
	}
	if got := reader.Int("PORT", 8080); got != 8080 {
		t.Fatalf("got %d", got)
	}
	if err := reader.Err(); err != nil {
		t.Fatal(err)
	}
}

func TestLoadFilePreservesSnapshotAndLoadsQuotedDefaults(t *testing.T) {
	values := map[string]string{"NAME": "environment"}
	reader := config.FromMap(values)
	values["NAME"] = "modified"
	path := filepath.Join(t.TempDir(), ".env")
	contents := "# defaults\nNAME=file\nGREETING='hello world'\nURL=\"https://example.test/?a=b\"\nEMPTY=\n"
	if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
	if err := reader.LoadFile(path); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{
		"NAME": "environment", "GREETING": "hello world", "URL": "https://example.test/?a=b", "EMPTY": "",
	} {
		if got := reader.String(key, "missing"); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
}

func TestLoadFileReportsPathForInvalidInput(t *testing.T) {
	for name, contents := range map[string]string{
		"malformed": "# comment\ninvalid\n",
		"too-long":  "KEY=" + strings.Repeat("x", 70_000),
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), ".env")
			if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
				t.Fatal(err)
			}
			err := config.FromMap(nil).LoadFile(path)
			if err == nil || !strings.Contains(err.Error(), path) {
				t.Fatalf("expected error containing file path, got %v", err)
			}
		})
	}
}

func TestLoadFileIgnoresMissingOptionalFile(t *testing.T) {
	if err := config.FromMap(nil).LoadFile(filepath.Join(t.TempDir(), ".env")); err != nil {
		t.Fatal(err)
	}
}
