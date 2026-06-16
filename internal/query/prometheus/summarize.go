package prometheus

import "fmt"

// SummarizeResult returns a concise, one-line description of a query result's
// shape (kind, series count, and points-per-series for ranges) so that
// empty-but-ran, instant, and range results are distinguishable at a glance
// without inspecting the JSON. It pairs with the request-intent result type:
// an empty range reads as an empty matrix, never an empty vector.
func SummarizeResult(resp *QueryResponse) string {
	if resp == nil {
		return ""
	}

	switch resp.Data.ResultType {
	case "matrix":
		return summarizeMatrix(resp.Data.Result)
	case "vector":
		if len(resp.Data.Result) == 0 {
			return "vector: 0 series (empty instant result)"
		}
		return fmt.Sprintf("vector: %d series", len(resp.Data.Result))
	case "scalar":
		return "scalar"
	default:
		return resp.Data.ResultType
	}
}

// summarizeMatrix reports series count and points-per-series, collapsing to a
// single "N × P" figure when every series carries the same number of points and
// falling back to a total when they differ.
func summarizeMatrix(samples []Sample) string {
	if len(samples) == 0 {
		return "matrix: 0 series (empty range result)"
	}

	pointsPerSeries := len(samples[0].Values)
	total := 0
	uniform := true
	for _, s := range samples {
		total += len(s.Values)
		if len(s.Values) != pointsPerSeries {
			uniform = false
		}
	}

	if uniform {
		return fmt.Sprintf("matrix: %d series × %d points", len(samples), pointsPerSeries)
	}
	return fmt.Sprintf("matrix: %d series, %d points total", len(samples), total)
}
