// Package hooks is `pingbus hook <event>` (Plan 00209 stage 2, Task 3.1), the Claude Code
// hooks of the agent bus: stdin hook JSON, stdout hook JSON.
//
// This file and hooks.go are a stand-in so the parity oracle (hooks/golden) compiles before the
// port lands: Run answers nothing and fails, and Placeholder says so. The port replaces the
// whole package, Placeholder included; hooks/golden/pending_test.go goes with it.
package hooks
