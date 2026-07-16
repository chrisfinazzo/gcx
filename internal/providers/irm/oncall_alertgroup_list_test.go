//nolint:testpackage // white-box tests require access to unexported IRM types and helpers
package irm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	cmdio "github.com/grafana/gcx/internal/output"
)

// fakeRichListAPI drives the rich alert-groups list path (internal API).
type fakeRichListAPI struct {
	OnCallAPI

	items    []json.RawMessage
	hasMore  bool
	gotLimit int
}

func (f *fakeRichListAPI) ListAlertGroupsRaw(_ context.Context, _ alertGroupListFilters, limit int) ([]json.RawMessage, bool, error) {
	f.gotLimit = limit
	return f.items, f.hasMore, nil
}

func (f *fakeRichListAPI) GetAlertGroupRich(context.Context, string) (*AlertGroupRich, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeRichListAPI) ListAlertIDs(context.Context, string, int) ([]string, int, error) {
	return nil, 0, nil
}

func (f *fakeRichListAPI) GetAlertRich(context.Context, string) (*alertAPI, *AlertRich, error) {
	return nil, nil, errors.New("not implemented")
}

func (f *fakeRichListAPI) ResolveTeams(context.Context) (map[string]string, error) {
	return map[string]string{}, nil
}

// fakeLegacyListAPI drives the SA-token fallback list path. It intentionally
// does NOT implement RichAlertGroupReader, so the command takes the legacy
// branch. The applied ListConfig is recorded to observe the wire limit.
type fakeLegacyListAPI struct {
	OnCallAPI

	items  []AlertGroup
	gotCfg ListConfig
}

func (f *fakeLegacyListAPI) ListAlertGroups(_ context.Context, opts ...ListOption) ([]AlertGroup, error) {
	for _, o := range opts {
		o(&f.gotCfg)
	}
	if f.gotCfg.Limit > 0 && len(f.items) > f.gotCfg.Limit {
		return f.items[:f.gotCfg.Limit], nil
	}
	return f.items, nil
}

// alertGroupListPayload is the decoded stdout shape asserted by these tests.
type alertGroupListPayload struct {
	Items    []map[string]any `json:"items"`
	ListMeta *cmdio.ListMeta  `json:"list_meta"`
}

func runAlertGroupList(t *testing.T, client OnCallAPI, args ...string) (alertGroupListPayload, string) {
	t.Helper()
	resetAgentMode(t)

	cmd := newAlertGroupListCommand(&fakeLoader{client: client})
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(append([]string{"-o", "json"}, args...))
	if err := cmd.Execute(); err != nil {
		t.Fatalf("alert-groups list: %v\nstderr=%s", err, stderr.String())
	}

	var payload alertGroupListPayload
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid JSON on stdout: %v\nraw=%s", err, stdout.String())
	}
	return payload, stderr.String()
}

func rawAlertGroups(n int) []json.RawMessage {
	items := make([]json.RawMessage, 0, n)
	for i := range n {
		items = append(items, json.RawMessage(
			`{"pk":"AG`+string(rune('0'+i))+`","alerts_count":1,"started_at":"2026-01-01T00:00:00Z"}`))
	}
	return items
}

func TestAlertGroupList_RichPath_ServerHasMore(t *testing.T) {
	fake := &fakeRichListAPI{items: rawAlertGroups(2), hasMore: true}
	payload, stderr := runAlertGroupList(t, fake, "--limit", "2")

	if fake.gotLimit != 2 {
		t.Errorf("wire limit = %d, want 2 (rich path passes the limit server-side)", fake.gotLimit)
	}
	if len(payload.Items) != 2 {
		t.Fatalf("items = %d, want 2", len(payload.Items))
	}
	meta := payload.ListMeta
	if meta == nil || !meta.Truncated || meta.Returned != 2 {
		t.Fatalf("list_meta = %+v, want truncated page of 2", meta)
	}
	if meta.Total != nil {
		t.Errorf("list_meta.total = %d, want absent (source not drained)", *meta.Total)
	}
	if meta.Continue != "gcx irm oncall alert-groups list --limit 0" {
		t.Errorf("list_meta.continue = %q", meta.Continue)
	}
	if !strings.Contains(stderr, "showing first 2 (more available)") {
		t.Errorf("stderr missing truncation hint:\n%s", stderr)
	}
}

func TestAlertGroupList_RichPath_Complete(t *testing.T) {
	fake := &fakeRichListAPI{items: rawAlertGroups(2), hasMore: false}
	payload, stderr := runAlertGroupList(t, fake, "--limit", "50")

	if payload.ListMeta != nil {
		t.Errorf("list_meta = %+v, want absent for a complete result set", payload.ListMeta)
	}
	if strings.Contains(stderr, "showing first") {
		t.Errorf("unexpected truncation hint on stderr:\n%s", stderr)
	}
}

func TestAlertGroupList_LegacyPath_OverFetchDetectsTruncation(t *testing.T) {
	items := []AlertGroup{
		{PK: "AG1", AlertsCount: 1},
		{PK: "AG2", AlertsCount: 1},
		{PK: "AG3", AlertsCount: 1},
		{PK: "AG4", AlertsCount: 1},
	}
	fake := &fakeLegacyListAPI{items: items}
	payload, stderr := runAlertGroupList(t, fake, "--limit", "3")

	if fake.gotCfg.Limit != 4 {
		t.Errorf("wire limit = %d, want 4 (over-fetch by one)", fake.gotCfg.Limit)
	}
	if len(payload.Items) != 3 {
		t.Fatalf("items = %d, want 3 (display limit)", len(payload.Items))
	}
	meta := payload.ListMeta
	if meta == nil || !meta.Truncated || meta.Returned != 3 || meta.Total != nil {
		t.Fatalf("list_meta = %+v, want truncated page of 3 with unknown total", meta)
	}
	if !strings.Contains(stderr, "showing first 3 (more available)") {
		t.Errorf("stderr missing truncation hint:\n%s", stderr)
	}
}

func TestAlertGroupList_LegacyPath_LimitZeroReturnsAll(t *testing.T) {
	items := []AlertGroup{{PK: "AG1"}, {PK: "AG2"}, {PK: "AG3"}}
	fake := &fakeLegacyListAPI{items: items}
	payload, stderr := runAlertGroupList(t, fake, "--limit", "0")

	if fake.gotCfg.Limit != 0 {
		t.Errorf("wire limit = %d, want 0 (unlimited)", fake.gotCfg.Limit)
	}
	if len(payload.Items) != 3 {
		t.Fatalf("items = %d, want all 3", len(payload.Items))
	}
	if payload.ListMeta != nil {
		t.Errorf("list_meta = %+v, want absent for --limit 0", payload.ListMeta)
	}
	if strings.Contains(stderr, "showing first") {
		t.Errorf("unexpected truncation hint on stderr:\n%s", stderr)
	}
}

func TestAlertGroupList_LegacyPath_ShortPageIsComplete(t *testing.T) {
	items := []AlertGroup{{PK: "AG1"}, {PK: "AG2"}}
	fake := &fakeLegacyListAPI{items: items}
	payload, _ := runAlertGroupList(t, fake, "--limit", "3")

	if len(payload.Items) != 2 {
		t.Fatalf("items = %d, want 2", len(payload.Items))
	}
	if payload.ListMeta != nil {
		t.Errorf("list_meta = %+v, want absent: no spare row means no more data", payload.ListMeta)
	}
}
