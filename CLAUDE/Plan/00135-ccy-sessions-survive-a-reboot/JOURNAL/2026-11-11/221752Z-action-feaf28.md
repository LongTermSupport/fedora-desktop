## 22:17 · action · —   — Phase 13 review fixes

Phase 13 review fixes (QA review of 6405e2c7c). Its should-fix 1, that provisioning had no way to write the marked record, is Task 13.5, `ccy-sessions declare`.

Doc drift: the GoingNone comment in internal/restore/manifest.go now covers a fresh restore; docs/ccy.md's "a session that recorded no conversation is not restored" now says "unless provisioning marked its record to start fresh"; Plan 00203 Decision 10 notes that Phase 13 revised it (the owner, 2026-11-11); Plan 00227 Task 3.3 says a rewritten record keeps FreshIfNoConversation.

Nits: in lib/session-registry.bash, ccy_registry_write's option loop no longer uses ${2?}, which ended the shell. An option with no word after it is not taken as an option, so a missing value reaches the record's own checks and the function returns 1. The restore/fresh refusal now prints both values with their allowed ranges, in the reader's "or" form, and no longer claims the mark is wrong. A message for each value would cost three statements, and the file is held at its bash-limits record, so it was not done. meta-deploy.list no longer says "No image rebuild" for Phase 13: the container version is unchanged, but the builder stage re-runs at the new commit. The stray backslash in Task 13.2 is gone; the line was rewrapped so mdformat does not escape it again.
