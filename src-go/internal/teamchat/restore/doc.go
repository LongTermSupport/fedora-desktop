// Package restore is a team's restore from one of its backups (Plan 00209 SPEC-n2g.md, C
// 3.12.2): the record `restore.json` and its resume and discard, the merged roster, the
// validation, the engine over its seams, the staging and secure units' argv, the fd-relative
// renames and removals in the homeserver's state directory, and the guard every lock-taking verb
// runs (RG1-RG3), which refuses while a restore is recorded.
//
// It must not import admin: the child's restore ops are the Child interface declared here, which
// admin implements. Production refuses the restore until host checks H-C5 and H-C15 pass (DC8,
// n2g-D8); the guard is live everywhere.
package restore
