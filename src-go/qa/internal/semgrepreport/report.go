// Package semgrepreport is the part of semgrep's JSON report that every gate reading it shares:
// one finding. semgrep names its keys in snake_case, which is why this is the one place the
// camelCase lint rule is excused for them.
package semgrepreport

// Result is one match of a rule.
type Result struct {
	CheckID string `json:"check_id"`
	Path    string `json:"path"`
	Start   struct {
		Line int `json:"line"`
	} `json:"start"`
	Extra struct {
		Message string `json:"message"`
	} `json:"extra"`
}
