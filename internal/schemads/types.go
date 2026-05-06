// Package schemads provides a client for the schemads schema-discovery
// protocol exposed by Grafana datasource plugins under
// /api/datasources/uid/{uid}/resources/abstractionSchema/...
//
// Plugins that implement this protocol publish their tables, columns,
// table hints, and aggregation capabilities so that callers (UIs, query
// engines, this CLI) can introspect what they can be asked.
package schemads

// FullSchemaResponse is the response body of the fullSchema endpoint.
type FullSchemaResponse struct {
	FullSchema *Schema `json:"fullSchema,omitempty"`
	Error      string  `json:"error,omitempty"`
}

// Schema is the complete schema reported by a datasource.
type Schema struct {
	Tables       []Table                 `json:"tables,omitempty"`
	Functions    []string                `json:"functions,omitempty"`
	Capabilities *DatasourceCapabilities `json:"capabilities,omitempty"`
}

// Table describes a single virtual table the datasource exposes.
type Table struct {
	Name            string           `json:"name"`
	TableParameters []TableParameter `json:"tableParameters,omitempty"`
	Columns         []Column         `json:"columns,omitempty"`
	TableHints      []TableHint      `json:"tableHints,omitempty"`
}

// Column is a single column in a Table.
type Column struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
}

// TableParameter is a parameter accepted by a parameterised table.
type TableParameter struct {
	Name      string   `json:"name"`
	DependsOn []string `json:"dependsOn,omitempty"`
	Root      bool     `json:"root,omitempty"`
	Required  bool     `json:"required,omitempty"`
}

// TableHint is a free-form hint a caller can attach to a FROM-clause
// table reference (e.g. `rate('5m')`).
type TableHint struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	HasValue    bool   `json:"hasValue,omitempty"`
}

// DatasourceCapabilities is the datasource-level capability advertisement.
type DatasourceCapabilities struct {
	AggregateFunctions []string `json:"aggregateFunctions,omitempty"`
	OrderBy            bool     `json:"orderBy,omitempty"`
	Limit              bool     `json:"limit,omitempty"`
}
