# Plan 00161 pingbus forge work: report

Three commits on the branch, from F44 at 124d465a. Not merged, not pushed.

1. **Repository answer in the cache file** (`helpers/pingbus/forge.py`). `ForgeCache` keeps
   a `repos` section (owner, private, fork, default branch, expiry), REACH_TTL_S, pruned with
   the other entries, strict on load; a file with no `repos` still loads (CACHE_VERSION
   unchanged). `Forge._repo` reads the process map, then the file, then the forge. A 404 is
   never kept. Measured with the stub transport, one new process per ping: 2 calls before
   (repository, compare), 1 for a new commit in a known repository, 0 for a repeat.
2. **`forge-rate` drops as `rate`**. `ForgeError.drop_reason` returns the existing `rate`
   code (already in `DROP_REASONS`, used by the flood limit), so no new code was added.
   Tests in test_forge.py and test_syncer.py; protocol doc §6 and §9 10p updated.
3. **`exclude`**. Team file and record key (needs `org`, 1 to 32 names, none also in
   `repos`), `Context.exclude`, `protocol.is_excluded`, new drop reason `excluded` (offline,
   before trust, on send and receive, any verb or form, not external either). Written by
   admin.py into the team record. declare.py needs no code (it digests the entry through the
   team file; a test shows a hand-written `exclude` changes the digest); `create` has no
   flag for it. Docs: protocol §1, §8, §9, §11; agent-bus.md; localhost.yml.dist; DESIGN D64
   and 3.4. Every member must run the new pingbus before a team uses `exclude`.

Container 2.53 and CCY 3.95.3 (pingbus is built into the image), changelog entry added.

Checks: `tests/helpers/pingbus` 988 passed, `tests/helpers/agent_bus` 385 passed, ruff clean.
`scripts/test-agent-bus-install.bash` not run (not touched).
