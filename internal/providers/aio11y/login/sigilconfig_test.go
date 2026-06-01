package login

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteSigilConfig_CreatesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sigil", "config.env")

	err := writeSigilConfig(path, map[string]string{
		"SIGIL_ENDPOINT":       "https://sigil-prod-eu-west-2.grafana.net",
		"SIGIL_AUTH_TENANT_ID": "42",
		"SIGIL_AUTH_TOKEN":     "glc_xxx",
	})
	if err != nil {
		t.Fatalf("writeSigilConfig: %v", err)
	}

	got := readEnv(t, path)
	if got["SIGIL_ENDPOINT"] != "https://sigil-prod-eu-west-2.grafana.net" {
		t.Errorf("SIGIL_ENDPOINT = %q", got["SIGIL_ENDPOINT"])
	}
	if got["SIGIL_AUTH_TENANT_ID"] != "42" {
		t.Errorf("SIGIL_AUTH_TENANT_ID = %q", got["SIGIL_AUTH_TENANT_ID"])
	}
	if got["SIGIL_AUTH_TOKEN"] != "glc_xxx" {
		t.Errorf("SIGIL_AUTH_TOKEN = %q", got["SIGIL_AUTH_TOKEN"])
	}

	// File must be 0600 because it holds credentials.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("perm = %o, want 600", perm)
	}
}

func TestWriteSigilConfig_PreservesUnrelatedAndComments(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.env")
	seed := "# my config\n" +
		"SIGIL_TAGS=team=infra\n" +
		"SIGIL_ENDPOINT=https://old.example.com\n" +
		"\n" +
		"export SIGIL_CONTENT_CAPTURE_MODE=metadata_only\n"
	if err := os.WriteFile(path, []byte(seed), 0o600); err != nil {
		t.Fatal(err)
	}

	err := writeSigilConfig(path, map[string]string{
		"SIGIL_ENDPOINT":   "https://new.example.com",
		"SIGIL_AUTH_TOKEN": "glc_new",
	})
	if err != nil {
		t.Fatalf("writeSigilConfig: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(raw)

	got := readEnv(t, path)
	if got["SIGIL_ENDPOINT"] != "https://new.example.com" {
		t.Errorf("SIGIL_ENDPOINT not updated: %q", got["SIGIL_ENDPOINT"])
	}
	if got["SIGIL_AUTH_TOKEN"] != "glc_new" {
		t.Errorf("SIGIL_AUTH_TOKEN not appended: %q", got["SIGIL_AUTH_TOKEN"])
	}
	// Unrelated keys and comments preserved.
	if got["SIGIL_TAGS"] != "team=infra" {
		t.Errorf("SIGIL_TAGS not preserved: %q", got["SIGIL_TAGS"])
	}
	if got["SIGIL_CONTENT_CAPTURE_MODE"] != "metadata_only" {
		t.Errorf("SIGIL_CONTENT_CAPTURE_MODE not preserved: %q", got["SIGIL_CONTENT_CAPTURE_MODE"])
	}
	if !containsLine(content, "# my config") {
		t.Errorf("comment line lost:\n%s", content)
	}
}

func TestWriteSigilConfig_EmptyValueDeletes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.env")
	if err := os.WriteFile(path, []byte("SIGIL_ENDPOINT=https://x\nSIGIL_OTEL_EXPORTER_OTLP_ENDPOINT=https://otlp\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	err := writeSigilConfig(path, map[string]string{
		"SIGIL_OTEL_EXPORTER_OTLP_ENDPOINT": "", // delete
	})
	if err != nil {
		t.Fatalf("writeSigilConfig: %v", err)
	}

	got := readEnv(t, path)
	if _, ok := got["SIGIL_OTEL_EXPORTER_OTLP_ENDPOINT"]; ok {
		t.Errorf("expected key to be deleted, still present")
	}
	if got["SIGIL_ENDPOINT"] != "https://x" {
		t.Errorf("SIGIL_ENDPOINT clobbered: %q", got["SIGIL_ENDPOINT"])
	}
}

// readEnv parses a dotenv file into a map for assertions.
func readEnv(t *testing.T, path string) map[string]string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	out := map[string]string{}
	for _, line := range splitLines(string(raw)) {
		key := lineKey(line)
		if key == "" {
			continue
		}
		_, after, _ := cut(line, "=")
		out[key] = after
	}
	return out
}

func splitLines(s string) []string {
	var lines []string
	cur := ""
	for _, r := range s {
		if r == '\n' {
			lines = append(lines, cur)
			cur = ""
			continue
		}
		cur += string(r)
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	return lines
}

func cut(s, sep string) (before, after string, found bool) {
	for i := 0; i+len(sep) <= len(s); i++ {
		if s[i:i+len(sep)] == sep {
			return s[:i], s[i+len(sep):], true
		}
	}
	return s, "", false
}

func containsLine(content, want string) bool {
	for _, line := range splitLines(content) {
		if line == want {
			return true
		}
	}
	return false
}
