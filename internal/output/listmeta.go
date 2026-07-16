package output

import (
	"fmt"
	"io"
)

// ListMeta is machine-readable truncation metadata for list envelopes.
// List commands attach it to their items envelope as
//
//	ListMeta *cmdio.ListMeta `json:"list_meta,omitempty" yaml:"list_meta,omitempty"`
//
// so that a truncated page is impossible to mistake for the complete set in
// structured output (json / yaml / agents). A nil ListMeta (field absent)
// means the output IS the complete result set. Table codecs ignore the field;
// the paired stderr hint comes from [EmitListTruncationHint].
type ListMeta struct {
	// Truncated is always true — a ListMeta is only attached when the
	// output is a partial page. It is serialized explicitly (no omitempty)
	// so agents can key on it.
	Truncated bool `json:"truncated" yaml:"truncated"`
	// Returned is the number of items in this page.
	Returned int `json:"returned" yaml:"returned"`
	// Total is the size of the complete result set, present only when the
	// source was actually drained or reported it. Absent (nil) means
	// unknown — never a guess.
	Total *int `json:"total,omitempty" yaml:"total,omitempty"`
	// Continue is the runnable command that retrieves the complete set.
	Continue string `json:"continue,omitempty" yaml:"continue,omitempty"`
}

// TruncateCompleteList applies a client-side display limit to a fully-fetched
// list (a "cheaply complete" source: the API has no server-side limit, so the
// command already holds everything). Returns the page and, when items were
// dropped, a ListMeta with the observed total. limit <= 0 means unlimited:
// the input is returned unchanged with nil metadata.
//
// listCmd is the runnable list command without a limit flag (filters
// included), e.g. "gcx datasources list --type prometheus".
func TruncateCompleteList[T any](items []T, limit int, listCmd string) ([]T, *ListMeta) {
	if limit <= 0 || len(items) <= limit {
		return items, nil
	}
	total := len(items)
	return items[:limit], &ListMeta{
		Truncated: true,
		Returned:  limit,
		Total:     &total,
		Continue:  continueCommand(listCmd),
	}
}

// TruncatePagedList applies a display limit to items over-fetched by one from
// a paginated source. Callers request limit+1 from the API and pass the full
// response: a spare item proves more data exists without draining the source,
// so Total stays unknown. limit <= 0 means unlimited: the input is returned
// unchanged with nil metadata.
func TruncatePagedList[T any](items []T, limit int, listCmd string) ([]T, *ListMeta) {
	if limit <= 0 || len(items) <= limit {
		return items, nil
	}
	return items[:limit], &ListMeta{
		Truncated: true,
		Returned:  limit,
		Continue:  continueCommand(listCmd),
	}
}

// PagedListMeta builds truncation metadata for a paginated source whose API
// reports directly whether more pages exist (a continue token / next cursor).
// Returns nil when the page is complete (limit <= 0 or the server reported no
// further pages). Total stays unknown — the source was not drained.
func PagedListMeta(returned, limit int, serverHasMore bool, listCmd string) *ListMeta {
	if limit <= 0 || !serverHasMore {
		return nil
	}
	return &ListMeta{
		Truncated: true,
		Returned:  returned,
		Continue:  continueCommand(listCmd),
	}
}

// EmitListTruncationHint writes the standardized truncation hint for a list
// page to w (stderr). No-op when meta is nil (complete result set). The line
// routes through [EmitHint], so agent mode emits the JSONL class:"hint" form.
//
//	TTY, total known:   "hint: showing first 10 of 87: gcx datasources list --limit 0"
//	TTY, total unknown: "hint: showing first 50 (more available): gcx irm oncall alert-groups list --limit 0"
func EmitListTruncationHint(w io.Writer, meta *ListMeta) {
	if meta == nil || !meta.Truncated {
		return
	}
	summary := fmt.Sprintf("showing first %d (more available)", meta.Returned)
	if meta.Total != nil {
		summary = fmt.Sprintf("showing first %d of %d", meta.Returned, *meta.Total)
	}
	EmitHint(w, summary, meta.Continue)
}

// continueCommand renders the uniform full-retrieval suggestion: --limit 0 is
// the documented "unlimited" spelling across all gcx list commands.
func continueCommand(listCmd string) string {
	if listCmd == "" {
		return ""
	}
	return listCmd + " --limit 0"
}
