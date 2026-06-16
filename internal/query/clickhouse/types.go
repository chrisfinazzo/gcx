// Package clickhouse provides a client for executing ClickHouse SQL queries
// against a Grafana ClickHouse datasource via Grafana's /api/ds/query proxy.
package clickhouse

import (
	"encoding/json"
	"time"
)

// QueryRequest is the input for a ClickHouse query.
type QueryRequest struct {
	// SQL is the rawSql payload forwarded to the ClickHouse datasource plugin.
	SQL string
	// Start and End set the inclusive time range advertised to the plugin so
	// macros like $__timeFilter() can expand against the request window. They
	// may both be zero, in which case a small default window (now-5m..now) is
	// used — sufficient for queries that don't reference time macros.
	Start time.Time
	End   time.Time
}

// IsRange reports whether the caller supplied an explicit time window.
func (r QueryRequest) IsRange() bool {
	return !r.Start.IsZero() && !r.End.IsZero()
}

// QueryResponse is a flattened ClickHouse query result. ClickHouse via the
// Grafana proxy always returns a single column-major data frame; we expose
// that shape directly so callers can iterate rows and columns without
// translating an intermediate format.
type QueryResponse struct {
	Schema FrameSchema `json:"schema"`
	Data   FrameData   `json:"data"`
}

// FrameSchema describes the columns of the returned frame.
type FrameSchema struct {
	Name   string     `json:"name,omitempty"`
	RefID  string     `json:"refId,omitempty"`
	Meta   *FrameMeta `json:"meta,omitempty"`
	Fields []Field    `json:"fields,omitempty"`
}

// FrameMeta carries the metadata Grafana attaches to a frame (executed SQL,
// preferred visualisation type, etc.).
type FrameMeta struct {
	ExecutedQueryString        string          `json:"executedQueryString,omitempty"`
	PreferredVisualisationType string          `json:"preferredVisualisationType,omitempty"`
	TypeVersion                []int           `json:"typeVersion,omitempty"`
	Custom                     json.RawMessage `json:"custom,omitempty"`
}

// Field describes a single column.
type Field struct {
	Name     string            `json:"name,omitempty"`
	Type     string            `json:"type,omitempty"`
	TypeInfo *FieldTypeInfo    `json:"typeInfo,omitempty"`
	Labels   map[string]string `json:"labels,omitempty"`
}

// FieldTypeInfo describes the wire-level type of a column's values.
type FieldTypeInfo struct {
	Frame    string `json:"frame,omitempty"`
	Nullable bool   `json:"nullable,omitempty"`
}

// FrameData holds the column-major values of the frame.
// Values[col][row] is the cell at the given column and row.
type FrameData struct {
	Values [][]any `json:"values,omitempty"`
}

// RowCount returns the number of rows in the response, derived from the
// longest column. A nil receiver or empty frame returns 0.
func (r *QueryResponse) RowCount() int {
	if r == nil {
		return 0
	}
	longest := 0
	for _, col := range r.Data.Values {
		if len(col) > longest {
			longest = len(col)
		}
	}
	return longest
}

// grafanaQueryResponse mirrors the envelope returned by /api/ds/query (and
// /apis/query.grafana.app). The ClickHouse plugin emits a single frame under
// refId "A".
type grafanaQueryResponse struct {
	Results map[string]grafanaResult `json:"results"`
}

type grafanaResult struct {
	Frames      []QueryResponse `json:"frames,omitempty"`
	Error       string          `json:"error,omitempty"`
	ErrorSource string          `json:"errorSource,omitempty"`
	Status      int             `json:"status,omitempty"`
}
