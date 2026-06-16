package clickhouse_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/query/clickhouse"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatTable_Basic(t *testing.T) {
	resp := &clickhouse.QueryResponse{
		Schema: clickhouse.FrameSchema{
			Fields: []clickhouse.Field{
				{Name: "n", Type: "number"},
				{Name: "s", Type: "string"},
			},
		},
		Data: clickhouse.FrameData{
			Values: [][]any{
				{float64(1), float64(2)},
				{"a", "b"},
			},
		},
	}

	var buf bytes.Buffer
	require.NoError(t, clickhouse.FormatTable(&buf, resp))

	out := buf.String()
	assert.Contains(t, out, "n")
	assert.Contains(t, out, "s")
	assert.Contains(t, out, "1")
	assert.Contains(t, out, "a")
	assert.Contains(t, out, "b")
}

func TestFormatTable_RendersTimeColumnAsRFC3339(t *testing.T) {
	resp := &clickhouse.QueryResponse{
		Schema: clickhouse.FrameSchema{
			Fields: []clickhouse.Field{
				{Name: "t", Type: "time"},
				{Name: "v", Type: "number"},
			},
		},
		Data: clickhouse.FrameData{
			// 1700000000000 ms = 2023-11-14T22:13:20Z
			Values: [][]any{
				{float64(1700000000000)},
				{float64(42)},
			},
		},
	}

	var buf bytes.Buffer
	require.NoError(t, clickhouse.FormatTable(&buf, resp))
	assert.Contains(t, buf.String(), "2023-11-14T22:13:20Z")
}

func TestFormatTable_EmptyResponse(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, clickhouse.FormatTable(&buf, &clickhouse.QueryResponse{}))
	assert.Equal(t, "No data\n", buf.String())
}

func TestFormatTable_NilResponse(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, clickhouse.FormatTable(&buf, nil))
	assert.True(t, strings.HasPrefix(buf.String(), "No data"))
}
