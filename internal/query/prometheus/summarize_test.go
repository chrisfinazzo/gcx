package prometheus_test

import (
	"testing"

	"github.com/grafana/gcx/internal/query/prometheus"
	"github.com/stretchr/testify/assert"
)

func TestSummarizeResult(t *testing.T) {
	matrixSample := func(points int) prometheus.Sample {
		s := prometheus.Sample{Metric: map[string]string{"__name__": "up"}}
		for i := range points {
			s.Values = append(s.Values, []any{float64(i), "1"})
		}
		return s
	}

	tests := []struct {
		name string
		resp *prometheus.QueryResponse
		want string
	}{
		{
			name: "nil response",
			resp: nil,
			want: "",
		},
		{
			name: "empty range result",
			resp: &prometheus.QueryResponse{Data: prometheus.ResultData{ResultType: "matrix"}},
			want: "matrix: 0 series (empty range result)",
		},
		{
			name: "empty instant result",
			resp: &prometheus.QueryResponse{Data: prometheus.ResultData{ResultType: "vector"}},
			want: "vector: 0 series (empty instant result)",
		},
		{
			name: "vector with series",
			resp: &prometheus.QueryResponse{Data: prometheus.ResultData{
				ResultType: "vector",
				Result: []prometheus.Sample{
					{Value: []any{1.0, "1"}},
					{Value: []any{1.0, "2"}},
					{Value: []any{1.0, "3"}},
				},
			}},
			want: "vector: 3 series",
		},
		{
			name: "uniform matrix",
			resp: &prometheus.QueryResponse{Data: prometheus.ResultData{
				ResultType: "matrix",
				Result:     []prometheus.Sample{matrixSample(3), matrixSample(3)},
			}},
			want: "matrix: 2 series × 3 points",
		},
		{
			name: "ragged matrix reports total",
			resp: &prometheus.QueryResponse{Data: prometheus.ResultData{
				ResultType: "matrix",
				Result:     []prometheus.Sample{matrixSample(3), matrixSample(2)},
			}},
			want: "matrix: 2 series, 5 points total",
		},
		{
			name: "scalar",
			resp: &prometheus.QueryResponse{Data: prometheus.ResultData{ResultType: "scalar"}},
			want: "scalar",
		},
		{
			name: "unknown type falls back to type name",
			resp: &prometheus.QueryResponse{Data: prometheus.ResultData{ResultType: "string"}},
			want: "string",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, prometheus.SummarizeResult(tt.resp))
		})
	}
}
