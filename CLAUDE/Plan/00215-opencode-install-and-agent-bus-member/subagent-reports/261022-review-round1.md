VERDICT: FIX-FIRST

The play is sound and every upstream fact I checked holds. Two claims about v2 in the design and plan are wrong and should be corrected before merge.

**Checks I ran myself**

- **sha256:** the release API digest for `opencode-linux-x64.tar.gz` at v1.19.3 is `sha256:13a8fb29…6c9b`, which matches `opencodeSha256`.
- **Gates:** `ansible-playbook --syntax-check` passes. `./scripts/qa-version-pins.bash` prints `VERSION-PINS-OK 14 pin(s) … 14 of 14 resolve`.
- **File:** mode 100755, with the same shebang as the other plays.
- **Upstream facts at the tag:** all of these match the fact sheet:
  - `managed.ts` resolves the managed config dir to `/etc/opencode` on Linux.
  - `upgrade.ts` reads `cfg.getGlobal()` only, as the facts file says.
  - `server.mdx`: port 4096, `OPENCODE_SERVER_PASSWORD`, `/global/health` returns `{healthy, version}`, `prompt_async` returns 204, `noReply`, `/event`.
  - `config.mdx`: precedence, Linux `/etc/opencode/`, the `share` and `autoupdate` keys.
  - `cli.mdx`: `--auto`, `--attach`, `--major`, `OPENCODE_DISABLE_CLAUDE_CODE*`.
  - `prompt.ts`: `createUserMessage` runs, then `noReply` returns early, else the call joins the running loop through `ensureRunning`.
  - `index.ts`: yargs `.version(InstallationVersion)` prints the bare version, so the assert on `1.19.3` is right.
- **Play behaviour:** become/become_user is set per task. `get_url` skips the download when the checksum matches, `unarchive` has `creates`, and the symlink uses `force`. The managed JSON renders through `to_nice_json`, and `$schema` sorts first. It is fail-fast, `scope: general`, and writes no credential.
- **Own play, not `play-cli-tools.yml`:** justified, because it writes config under `/etc`.
- **Public-repo hygiene:** the diff has no install-specific names, paths or hostnames.
- **Skill and plan files:** the update-versions skill edit fixes text that was already stale (the script reads `vars/version-pins.yml`, not a heredoc). The README row and meta-deploy entry are in the right format and the wording is accurate.

**Findings**

1. **FIX-FIRST** — `PLAN.md:81-82` (Task 3.3) and `research/opencode-bus-member-design.md:253`. LXC members are Plan 00209 **N3 Task 3.3** ("LXC members: guest operations (B 2.13)…", `00209/PLAN.md:205`). They are not N4: Tasks 4.1-4.4 are the ccy kit, the launcher, ccy-teams and the pilot. Fix: make the LXC option of Task 3.3 depend on "Plan 00209 Task 3.3 (N3)", change "v2 N4's LXC member kind" to "v2 N3's LXC member kind (Task 3.3)", and leave N4 Task 4.1 only on the ccy-image option.

2. **FIX-FIRST** — `research/opencode-bus-member-design.md:68-71` and `:103`. The design records the opencode session id only in `session.json` and resumes from it. In v2, `session.json` lives in `<v2-run>` (tmpfs, removed at the end; B 2.3.1 table and 2.4.2 "removes `<v2-run>`"). Resume across runs reads `<v2-state>/conversation.json` (B 2.4.1 step 5, B43). Fix: say the link writes the opencode session id to `conversation.json`, which `--resume-recorded` reads as a hint. Keep `session.json` for the live run and link restarts only.

3. **NOTE** — `docs/playbooks.md:1254`. "never with `/update` or `opencode upgrade`, which the next run of the play undoes" is not established. At v1.19.3, `Installation.method()` returns `curl` when execPath contains `.local/bin`. `upgradeCurl` then pipes `https://opencode.ai/install` to a shell, which installs into `~/.opencode/bin`. The play never touches that path, and its assert runs the absolute `opencodeBin`, so it would not see the drift. If execPath resolves to `~/.local/lib/…`, the method is probably `unknown` and the upgrade just fails. Fix: drop "which the next run of the play undoes", or add a host check that `opencode upgrade` is refused (or what it writes). The same applies to the "puts the pinned binary back" wording in `vars/version-pins.yml:84`.

4. **NOTE** — `play-opencode.yml:104-109`. `patterns: "opencode-*"` deletes any directory in `~/.local/lib` whose name starts with `opencode-`. Fix: use `use_regex: true` with `patterns: '^opencode-[0-9]+\.[0-9]+\.[0-9]+$'`.

5. **NOTE** — design §5.2 `type Kind`. This clashes with v2's existing `--kind ccy|lxc|host|other` (member kind). v2 calls the agent program `launch` (`session.pid` `"launch":"claude|plain"`). Fix: rename it `Launch` or `Agent` to match.

6. **NOTE** — design §3.1/§5.2. Two things are left unsaid:

   - What `inbound: refuse` means for an opencode launch. Fix: say that refuse means no `prompt_async` push, so delivery mode `wait`.
   - Where the HTTP/SSE client runs. The link's state goroutine "never makes a network call" (B 2.4.3), so the opencode `Channel` must run in its own goroutine, as the delivery goroutine does.

7. **NOTE** — design §3.1 step 2. "The keeper draws … a loopback port" is a race between choosing the port and opencode binding it. Fix: state the retry rule (a bind failure is a launch failure, retried with a new port), or have it learn the port another way.

8. **NOTE** — `PLAN.md:116`. The success criterion "`check-pinned-versions.bash` lists `opencodeVersion`" can be checked in the container now. Run it and tick the criterion, or note that it is checked at Task 1.4.

**Design against v2, apart from the above**

- **Gate A:** untouched as claimed. The only new owner-visible thing is the `teamchat run` launch-mode spelling, which is put to the owner as O4.
- **Seam:** coherent. Envelope rendering, item states, the ledger, receipts, the flood budget and seats stay outside the adapter, and hook ops are refused for a `client`-facts kind, so authority stays in the link.
- **Proof:** the API needle check is the same proof the Claude Code path uses (B 2.7.4 counts a busy session's `queued_command` as proof), so an opencode delivery is proven on the same terms.
- **Other v2 citations:** the ones I checked are accurate (`--plain` as a second launch mode, `TEAMCHAT_RUNTIME` set at step 7, `wait` mode in 2.7.1, 2 s pacing, end judgement in 2.4.2).
- **Phase 3:** depends on v2, not v1, apart from the wrong task numbers in finding 1.

I changed no tracked files and committed nothing. Upstream sources and the v2 B design were fetched into `untracked/scratch/` (`oc-*.ts`, `oc-*.mdx`, `v2B.md`) and can be deleted.
