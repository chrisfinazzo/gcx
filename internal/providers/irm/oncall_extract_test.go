package irm

import (
	"encoding/json"
	"testing"
)

// TestStripTitleTargetSuffix asserts that OnCall's render_for_web titles —
// which always include a trailing "(cluster, namespace)" target block —
// have that block removed when the contents look like cluster/service/
// namespace identifiers. Alert names that legitimately contain parens
// (e.g. "FailedDeploys (canary)" where the qualifier is human-readable
// prose) are left alone.
func TestStripTitleTargetSuffix(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"single namespace", "KubePodNotReady (grafana-apps)", "KubePodNotReady"},
		{"cluster + namespace", "CloudSQLSlowQueries (prod-eu-west-2, machine-learning)", "CloudSQLSlowQueries"},
		{"three identifiers", "Foo (a, b, c)", "Foo"},
		{"no parens", "AlwaysFiringAlert", "AlwaysFiringAlert"},
		{"compound name with hyphen and parens", "GrafanaRulerWriteTimeSeries - FastErrorBudgetBurn (prod-eu-west-2, grafana-ruler)", "GrafanaRulerWriteTimeSeries - FastErrorBudgetBurn"},
		{"empty", "", ""},
		// Conservative: don't strip parentheticals that contain prose
		// (spaces inside the parens, or 4+ comma-separated segments).
		{"prose left intact", "FailedDeploys (canary build)", "FailedDeploys (canary build)"},
		{"too many commas left intact", "Wide (a, b, c, d)", "Wide (a, b, c, d)"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := stripTitleTargetSuffix(tc.in)
			if got != tc.want {
				t.Errorf("stripTitleTargetSuffix(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestExtractTitleFromRenderForWeb asserts the title extraction strips the
// target suffix even when the render_for_web blob contains noise.
func TestExtractTitleFromRenderForWeb(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   json.RawMessage
		want string
	}{
		{"with target", json.RawMessage(`{"title": "PyroscopeRingHasUnhealthyMembers (prod-au-southeast-1, profiles-prod-016)"}`), "PyroscopeRingHasUnhealthyMembers"},
		{"no target", json.RawMessage(`{"title": "k6CloudIngestLagOverThreshold"}`), "k6CloudIngestLagOverThreshold"},
		{"empty input", nil, ""},
		{"null", json.RawMessage(`null`), ""},
		{"missing title", json.RawMessage(`{"message": "..."}`), ""},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := extractTitleFromRenderForWeb(tc.in)
			if got != tc.want {
				t.Errorf("extractTitleFromRenderForWeb(%q) = %q, want %q", string(tc.in), got, tc.want)
			}
		})
	}
}

// TestExtractSeverityFromRenderForWeb asserts the HTML-fallback severity
// extractor pulls "warning" / "critical" / etc. out of the OnCall server-
// rendered message body so the SEVERITY column on the list path renders
// real values when last_alert.raw_request_data is absent.
func TestExtractSeverityFromRenderForWeb(t *testing.T) {
	t.Parallel()
	const sample = `{"message": "<p>Status: firing</p><ul><li>cluster: prod  </li><li>severity: warning  </li><li>alertname: Foo  </li></ul>"}`
	cases := []struct {
		name string
		in   json.RawMessage
		want string
	}{
		{"warning from CommonLabels", json.RawMessage(sample), "warning"},
		{"critical", json.RawMessage(`{"message": "<li>severity: critical</li>"}`), "critical"},
		{"none", json.RawMessage(`{"message": "<li>severity: none</li>"}`), "none"},
		{"missing severity", json.RawMessage(`{"message": "<li>foo: bar</li>"}`), ""},
		{"empty", nil, ""},
		{"no message field", json.RawMessage(`{"title": "x"}`), ""},
		{"malformed JSON", json.RawMessage(`{not json`), ""},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := extractSeverityFromRenderForWeb(tc.in)
			if got != tc.want {
				t.Errorf("extractSeverityFromRenderForWeb(%q) = %q, want %q", string(tc.in), got, tc.want)
			}
		})
	}
}

// TestTruncateRunes covers the table-codec rune-aware truncation: ASCII +
// multibyte content, the single-rune-budget edge case, and the "no
// truncation" sentinel.
func TestTruncateRunes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		s     string
		width int
		want  string
	}{
		{"shorter than width", "abc", 10, "abc"},
		{"exactly width", "abcdef", 6, "abcdef"},
		{"truncate ASCII", "abcdefghij", 5, "abcd…"},
		{"truncate width=1", "abc", 1, "…"},
		{"no truncation w=0", "verylongtitle", 0, "verylongtitle"},
		{"no truncation w<0", "verylongtitle", -1, "verylongtitle"},
		{"empty", "", 5, ""},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := truncateRunes(tc.s, tc.width)
			if got != tc.want {
				t.Errorf("truncateRunes(%q, %d) = %q, want %q", tc.s, tc.width, got, tc.want)
			}
		})
	}
}
