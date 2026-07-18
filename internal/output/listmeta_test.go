package output_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/grafana/gcx/internal/agent"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/stretchr/testify/assert"
)

func TestTruncateCompleteList(t *testing.T) {
	items := []string{"a", "b", "c", "d"}

	tests := []struct {
		name      string
		items     []string
		limit     int
		wantItems []string
		wantMeta  *cmdio.ListMeta
	}{
		{
			name:      "limit zero returns all with nil meta",
			items:     items,
			limit:     0,
			wantItems: items,
			wantMeta:  nil,
		},
		{
			name:      "under limit returns all with nil meta",
			items:     items,
			limit:     10,
			wantItems: items,
			wantMeta:  nil,
		},
		{
			name:      "exactly at limit returns all with nil meta",
			items:     items,
			limit:     4,
			wantItems: items,
			wantMeta:  nil,
		},
		{
			name:      "over limit truncates with observed total",
			items:     items,
			limit:     2,
			wantItems: []string{"a", "b"},
			wantMeta: &cmdio.ListMeta{
				Truncated: true,
				Returned:  2,
				Total:     new(4),
				Continue:  "gcx datasources list --limit 0",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, meta := cmdio.TruncateCompleteList(tc.items, tc.limit, "gcx datasources list")
			assert.Equal(t, tc.wantItems, got)
			assert.Equal(t, tc.wantMeta, meta)
		})
	}
}

func TestTruncatePagedList(t *testing.T) {
	// Over-fetch-by-one: the caller requested limit+1 items.
	tests := []struct {
		name      string
		items     []string
		limit     int
		wantItems []string
		wantMeta  *cmdio.ListMeta
	}{
		{
			name:      "limit zero returns all with nil meta",
			items:     []string{"a", "b", "c"},
			limit:     0,
			wantItems: []string{"a", "b", "c"},
			wantMeta:  nil,
		},
		{
			name:      "short page proves source drained",
			items:     []string{"a", "b"},
			limit:     3,
			wantItems: []string{"a", "b"},
			wantMeta:  nil,
		},
		{
			name:      "exactly limit items means no spare row, no truncation",
			items:     []string{"a", "b", "c"},
			limit:     3,
			wantItems: []string{"a", "b", "c"},
			wantMeta:  nil,
		},
		{
			name:      "spare row proves more exist, total stays unknown",
			items:     []string{"a", "b", "c", "d"},
			limit:     3,
			wantItems: []string{"a", "b", "c"},
			wantMeta: &cmdio.ListMeta{
				Truncated: true,
				Returned:  3,
				Continue:  "gcx things list --limit 0",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, meta := cmdio.TruncatePagedList(tc.items, tc.limit, "gcx things list")
			assert.Equal(t, tc.wantItems, got)
			assert.Equal(t, tc.wantMeta, meta)
		})
	}
}

func TestPagedListMeta(t *testing.T) {
	tests := []struct {
		name          string
		returned      int
		limit         int
		serverHasMore bool
		want          *cmdio.ListMeta
	}{
		{
			name:          "limit zero means unlimited, nil meta",
			returned:      120,
			limit:         0,
			serverHasMore: true,
			want:          nil,
		},
		{
			name:          "server reports no more pages, nil meta",
			returned:      50,
			limit:         50,
			serverHasMore: false,
			want:          nil,
		},
		{
			name:          "server reports more pages, total unknown",
			returned:      50,
			limit:         50,
			serverHasMore: true,
			want: &cmdio.ListMeta{
				Truncated: true,
				Returned:  50,
				Continue:  "gcx irm oncall alert-groups list --limit 0",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := cmdio.PagedListMeta(tc.returned, tc.limit, tc.serverHasMore, "gcx irm oncall alert-groups list")
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestEmitListTruncationHint(t *testing.T) {
	tests := []struct {
		name      string
		meta      *cmdio.ListMeta
		agentMode bool
		want      string
	}{
		{
			name: "nil meta emits nothing",
			meta: nil,
			want: "",
		},
		{
			name: "total known TTY",
			meta: &cmdio.ListMeta{Truncated: true, Returned: 10, Total: new(87), Continue: "gcx datasources list --limit 0"},
			want: "hint: showing first 10 of 87: gcx datasources list --limit 0\n",
		},
		{
			name: "total unknown TTY",
			meta: &cmdio.ListMeta{Truncated: true, Returned: 50, Continue: "gcx things list --limit 0"},
			want: "hint: showing first 50 (more available): gcx things list --limit 0\n",
		},
		{
			name:      "agent mode emits JSONL hint",
			meta:      &cmdio.ListMeta{Truncated: true, Returned: 10, Total: new(87), Continue: "gcx datasources list --limit 0"},
			agentMode: true,
			want:      `{"class":"hint","summary":"showing first 10 of 87","command":"gcx datasources list --limit 0"}` + "\n",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			agent.SetFlag(tc.agentMode)
			t.Cleanup(func() { agent.SetFlag(false) })

			var buf bytes.Buffer
			cmdio.EmitListTruncationHint(&buf, tc.meta)
			if buf.String() != tc.want {
				t.Errorf("hint output = %q, want %q", buf.String(), tc.want)
			}
		})
	}
}

// TestListMetaJSONShape locks the wire shape agents key on: truncated is
// always explicit, total is omitted (never guessed) when unknown.
func TestListMetaJSONShape(t *testing.T) {
	tests := []struct {
		name string
		meta *cmdio.ListMeta
		want string
	}{
		{
			name: "total known",
			meta: &cmdio.ListMeta{Truncated: true, Returned: 2, Total: new(4), Continue: "gcx datasources list --limit 0"},
			want: `{"truncated":true,"returned":2,"total":4,"continue":"gcx datasources list --limit 0"}`,
		},
		{
			name: "total unknown is omitted",
			meta: &cmdio.ListMeta{Truncated: true, Returned: 50, Continue: "gcx things list --limit 0"},
			want: `{"truncated":true,"returned":50,"continue":"gcx things list --limit 0"}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b, err := json.Marshal(tc.meta)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if string(b) != tc.want {
				t.Errorf("json = %s, want %s", b, tc.want)
			}
		})
	}
}
