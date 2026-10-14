# qa-discovery port (Plan 00192 Task 3.3), branch go-port/qa-discovery

Commit d1c4989a on go-port/qa-discovery (from integration/261014), pushed, not merged.

- Spec: SPEC-qa-discovery.md, expectations D1-D14, departures B1-B11.
- Go: `src-go/qa/discovery` (run, list, trees, shebang) and `qa/cmd/qa-discovery`; tests first
  (red was a failed build), then code; the wrapper and real-tree tests came after and are checked
  by the live-tree comparison.
- Cut-over: `scripts/qa-discovery.bash` is a build-and-run wrapper (`qa_discover LIST ROOT` fills
  `QA_FILES`; `qa_is_excluded` and `qa_py_is_excluded` stay in bash over `qa-trees.tsv` because
  qa-js and qa-ansible-syntax ask once per file). Allowlist row removed. Callers changed: qa-bash,
  qa-python, qa-ansible-syntax, qa-helper-tests, qa-ready-wait-rules. Routing row added for the
  wrapper and `lib/qa-trees.bash` (run the Go gate). gosec G304 exclusion for list.go and trees.go.
- Live tree: old library and Go tool list identical files for all seven lists (shell 433, python
  311, templates 1, tracked-playbooks 82, tracked-helper-tests 131). qa-bash, qa-python,
  qa-ansible-syntax run green through the wrapper.
- Gates: `go test ./...` in src-go/qa, qa-go.bash, qa-changed.bash all green.
- Conflict note: the parallel qa-python port edits qa-python.bash, which this branch also edits at
  three call sites (qa_discover python / tracked-python / python-templates); take the port's file
  and keep those calls (or use the Go package directly).
