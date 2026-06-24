package datasources

import (
	"log/slog"

	dsclient "github.com/grafana/gcx/internal/datasources"
)

// secretRequiringTypes lists datasource types that typically need credentials
// (database passwords, API keys) supplied via secureJsonData. It is a
// best-effort heuristic used only to warn on push — never to block it.
//
//nolint:gochecknoglobals // Static lookup table for the warn-only secret heuristic.
var secretRequiringTypes = map[string]bool{
	"mysql":                         true,
	"postgres":                      true,
	"grafana-postgresql-datasource": true,
	"mssql":                         true,
	"elasticsearch":                 true,
	"influxdb":                      true,
	"cloudwatch":                    true,
	"grafana-athena-datasource":     true,
	"grafana-redshift-datasource":   true,
	"grafana-x-ray-datasource":      true,
	"grafana-bigquery-datasource":   true,
	"grafana-snowflake-datasource":  true,
	"grafana-mongodb-datasource":    true,
	"grafana-clickhouse-datasource": true,
	"grafana-databricks-datasource": true,
}

// secretLikelyRequired reports whether ds is being pushed without any
// secureJsonData even though its configuration suggests credentials are needed:
// either basic auth is enabled (and thus needs a password) or its type is one
// that conventionally requires a secret. Secrets are write-only and absent from
// pulled manifests, so this catches the common pull→push round-trip that would
// silently drop credentials.
func secretLikelyRequired(ds *dsclient.Datasource) bool {
	if len(ds.SecureJSONData) > 0 {
		return false
	}
	return ds.BasicAuth || secretRequiringTypes[ds.Type]
}

// warnIfSecretMissing emits a warning when a push is unlikely to carry the
// credentials the datasource needs. It does not prevent the push.
func warnIfSecretMissing(ds *dsclient.Datasource) {
	if !secretLikelyRequired(ds) {
		return
	}
	slog.Warn(
		"datasource pushed without secureJsonData; credentials such as passwords or API keys are write-only and will not be set — add spec.secureJsonData to configure them",
		"name", ds.Name,
		"type", ds.Type,
		"uid", ds.UID,
	)
}
