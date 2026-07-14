package mysql_test

import (
	"testing"

	"github.com/grafana/gcx/internal/query/mysql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEscapeSQLString(t *testing.T) {
	assert.Equal(t, "it''s", mysql.EscapeSQLString("it's"))
	assert.Equal(t, "plain", mysql.EscapeSQLString("plain"))
}

func TestValidateIdentifier(t *testing.T) {
	t.Run("accepts", func(t *testing.T) {
		for _, name := range []string{"", "orders", "testdb.orders", "my_table_2"} {
			assert.NoError(t, mysql.ValidateIdentifier(name, "table"), name)
		}
	})

	t.Run("rejects", func(t *testing.T) {
		for _, name := range []string{"bad-name", "x; DROP TABLE y", "1table", "a'b", "a`b"} {
			err := mysql.ValidateIdentifier(name, "table")
			require.Error(t, err, name)
			assert.Contains(t, err.Error(), "invalid table")
		}
	})
}

func TestEnforceLimit(t *testing.T) {
	tests := []struct {
		name  string
		sql   string
		limit int
		want  string
	}{
		{"appends LIMIT when missing", "SELECT 1", 100, "SELECT 1 LIMIT 100"},
		{"caps existing LIMIT exceeding max", "SELECT 1 LIMIT 5000", 100, "SELECT 1 LIMIT 1000"},
		{"keeps existing LIMIT if under max", "SELECT 1 LIMIT 50", 100, "SELECT 1 LIMIT 50"},
		{"limit 0 disables enforcement", "SELECT 1", 0, "SELECT 1"},
		{"bail on LIMIT OFFSET", "SELECT id FROM t LIMIT 10 OFFSET 20", 100, "SELECT id FROM t LIMIT 10 OFFSET 20"},
		{"bail on LIMIT comma syntax", "SELECT id FROM t LIMIT 10, 20", 100, "SELECT id FROM t LIMIT 10, 20"},
		{"bail on OFFSET", "SELECT id FROM t OFFSET 20", 100, "SELECT id FROM t OFFSET 20"},
		{"bail on INTO OUTFILE", "SELECT id FROM t INTO OUTFILE '/tmp/x'", 100, "SELECT id FROM t INTO OUTFILE '/tmp/x'"},
		{"bail on FOR UPDATE", "SELECT id FROM t FOR UPDATE", 100, "SELECT id FROM t FOR UPDATE"},
		{"bail on LOCK IN SHARE MODE", "SELECT id FROM t LOCK IN SHARE MODE", 100, "SELECT id FROM t LOCK IN SHARE MODE"},
		{"bail on EXPLAIN", "EXPLAIN SELECT id FROM t", 100, "EXPLAIN SELECT id FROM t"},
		{"bail on SHOW", "SHOW TABLES", 100, "SHOW TABLES"},
		{"bail on DESCRIBE", "DESCRIBE orders", 100, "DESCRIBE orders"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, mysql.EnforceLimit(tt.sql, tt.limit, 1000))
		})
	}
}
