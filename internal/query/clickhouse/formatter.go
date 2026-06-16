package clickhouse

import (
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/grafana/gcx/internal/style"
)

// FormatTable renders a QueryResponse as a column-major table. Each schema
// field becomes a column; rows are interleaved across the column-major Values.
func FormatTable(w io.Writer, resp *QueryResponse) error {
	if resp == nil || len(resp.Schema.Fields) == 0 {
		fmt.Fprintln(w, "No data")
		return nil
	}

	headers := make([]string, len(resp.Schema.Fields))
	for i, f := range resp.Schema.Fields {
		headers[i] = f.Name
	}
	t := style.NewTable(headers...)

	rowCount := resp.RowCount()
	for r := range rowCount {
		row := make([]string, len(resp.Schema.Fields))
		for c, field := range resp.Schema.Fields {
			var v any
			if c < len(resp.Data.Values) && r < len(resp.Data.Values[c]) {
				v = resp.Data.Values[c][r]
			}
			row[c] = formatCell(v, field.Type)
		}
		t.Row(row...)
	}

	return t.Render(w)
}

// formatCell renders a single cell. ClickHouse + the Grafana plugin emit
// "time" typed columns as epoch milliseconds; render those as RFC3339 for
// readability. Other types stringify with sensible numeric defaults.
func formatCell(v any, fieldType string) string {
	if v == nil {
		return ""
	}
	if fieldType == "time" {
		if ms, ok := toInt64(v); ok {
			return time.UnixMilli(ms).UTC().Format(time.RFC3339)
		}
	}
	switch x := v.(type) {
	case string:
		return x
	case bool:
		return strconv.FormatBool(x)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(x), 'f', -1, 32)
	case int:
		return strconv.FormatInt(int64(x), 10)
	case int32:
		return strconv.FormatInt(int64(x), 10)
	case int64:
		return strconv.FormatInt(x, 10)
	default:
		return fmt.Sprintf("%v", v)
	}
}

func toInt64(v any) (int64, bool) {
	switch x := v.(type) {
	case float64:
		return int64(x), true
	case float32:
		return int64(x), true
	case int:
		return int64(x), true
	case int32:
		return int64(x), true
	case int64:
		return x, true
	case string:
		if n, err := strconv.ParseInt(x, 10, 64); err == nil {
			return n, true
		}
		if f, err := strconv.ParseFloat(x, 64); err == nil {
			return int64(f), true
		}
	}
	return 0, false
}
