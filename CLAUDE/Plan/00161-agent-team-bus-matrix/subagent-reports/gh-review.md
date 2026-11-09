# Review: pingbus, gh and "require, don't provide" (Plan 00161, D66)

Read-only review of the F44 deployed state versus the D66 branch staged in the in-progress
merge. No install-specific names.

## 1. The claims, verified

**Does the deployed (pre-D66) pingbus ignore gh inside ccy? Yes.**
`git show HEAD:helpers/pingbus/forge.py:67` — `CREDENTIAL_SOURCES = ("PINGBUS_FORGE_TOKEN_FILE", "PINGBUS_FORGE_TOKEN", "GH_TOKEN", "GITHUB_TOKEN")`; `resolve_credential` (lines 103-120) reads
only environment variables and returns `Credential("none")` when none is set; `_request`
(line 442) sends `Authorization` only when a token exists. It never runs `gh`.

**Does the ccy entrypoint unset GH_TOKEN? Yes — but it logs gh in first.**
`files/var/local/claude-yolo/entrypoint.sh:66-73`: `mkdir -p ~/.config/gh`; `TEMP_TOKEN="$GH_TOKEN"; unset GH_TOKEN`; `echo "$TEMP_TOKEN" | gh auth login --with-token` (exit 1 on failure); then
`gh auth status` must pass (line 128) or the container does not start. The host side exports
`GH_TOKEN` into the container only for this (`files/var/local/claude-yolo/claude-yolo:3611,3891`;
`lib/token-management.bash:1248-1261`), and `files/opt/claude-yolo/optional/child-claude/bin/ccy-claude:13`
already documents that the entrypoint unsets it after `gh auth login`. gh is in the image
(`files/var/local/claude-yolo/Dockerfile:137`).

So inside every ccy session: `GH_TOKEN` is absent, but gh holds a stored login in
`~/.config/gh/hosts.yml`. The deployed pingbus therefore called GitHub anonymously, 60/h per IP,
while a logged-in gh sat beside it. That is exactly the owner's point: the provision of gh is
already solved; pingbus simply never asked it. The quota failure at M2.8 is the direct result.

**Would `gh auth token` work inside a ccy session as pingbus runs it? Yes.** pingbus runs as the
Claude hooks (`files/opt/claude-yolo/optional/agent-bus/agent-bus-claude:15-18`: the kit's
pingbus on PATH, `pingbus config check`, then `exec claude …`), in the same user, HOME and
environment the entrypoint used for `gh auth login`. The staged `forge.py` `gh_token()` runs
`gh auth token --hostname github.com` with the inherited environ (`GH_ARGV`, line ~71 of the
staged file) — the same call `lib/ssh-handling.bash:1780` and `play-github-cli-multi.yml:945`
already rely on. Nothing new has to be provided in ccy.

## 2. Does D66 match "require, don't provide"?

**The production code path does, cleanly.** One source (`gh auth token --hostname github.com`),
no env-var reads of its own, no token file, no anonymous mode, exit 78 naming the fix when gh is
missing or logged out (staged `helpers/pingbus/forge.py` `gh_token`/`resolve_credential`;
`helpers/pingbus/cli.py` `forge_credential_source` reduced to one call). The LXC members play
only *checks* gh (`helpers/agent_bus_lxc/members.py` `_check_gh`, "never provided here"). The
kit READMEs state the requirement and say "nothing here does it". This is "require".

**Every place gh is provided / installed / logged in** (all in the staged diff):

| Where                                                              | What                                                                                   | Verdict                                                               |
| ------------------------------------------------------------------ | -------------------------------------------------------------------------------------- | --------------------------------------------------------------------- |
| `_acceptance-steps.inc.bash` `read_gh_login`                       | reads the desktop user's token via `gh auth token` into a shell variable               | test scaffolding; the owner has explicitly OK'd using their own login |
| `_acceptance-steps.inc.bash` (M1 members, `gh_login_member` block) | `gh auth login --with-token` as each DynamicUser host member, `HOME=/var/lib/<member>` | provides, for throwaway members the harness itself creates            |
| `_acceptance-u23.inc.bash` lxc/vm                                  | `dnf install … gh`                                                                     | **installs the gh package** in throwaway guests                       |
| `_acceptance-u23.inc.bash` docker                                  | `apt-get update && apt-get install gh` on a Debian image                               | **installs gh**, unmeasured package availability (sub-agent report)   |
| `_acceptance-u23.inc.bash` `u23_join`                              | `gh auth login --with-token` as the agent user                                         | provides, throwaway                                                   |
| `scripts/test-ccy-agent-bus.bash` stub `gh`                        | a stand-in printing a fake token                                                       | fine: an offline unit-style test                                      |
| `tests/helpers/pingbus/fake_gh.py`                                 | stand-in gh for unit tests                                                             | fine                                                                  |

Nothing in production (ccy image, entrypoint, the installer, the plays) provides gh beyond
what already existed. The "providing" is confined to the acceptance harness's own throwaway
members, which is defensible: the harness *is* "whoever sets the member up". But it is also
where the confusion the owner senses lives — about 40 lines plus three package installs, two
`gh auth login` paths, a Debian `gh` package nobody has measured, and the owner's full-scope
token copied into five throwaway gh configs (the sub-agent report records this as a risk).

**Is there something simpler? Yes — the stub gh the owner suggests.** pingbus's whole contract
with gh is one argv (`gh auth token --hostname github.com`) and one line on stdout. A tiny
executable named `gh` first on each member's PATH that prints the token from a run-scoped 0600
file satisfies pingbus exactly, and M1.5/M1.6 still get real GitHub answers because the token is
the owner's real one. It removes the `dnf`/`apt` installs, the `gh auth login` steps and the
Debian package risk; the owner's token then sits in one 0600 file per member instead of a gh
config, both removed at teardown. `scripts/test-ccy-agent-bus.bash` already does exactly this
(staged lines ~297-303), so the pattern exists in the repo. Trade-off: the acceptance no longer
exercises a real gh in LXC/docker/VM; but what D66 changed is pingbus's call, not gh, and real
gh is exercised where it matters, in ccy (M2) and on the host.

**Recommendation:** stub gh for the non-ccy members (M1 host members and U23 lxc/docker/vm),
reading the token from a per-member 0600 file written by the harness and deleted at teardown.
Keep `read_gh_login` (the owner's real login is the source). Drop the three package installs and
the five `gh auth login --with-token` steps. Alternatively — and even simpler given "the tests
are not the priority" — defer U23 entirely: the owner's acceptance is ccy-teams with agents in
ccy sessions, which is M2 and needs none of this.

## 3. Anything else contrary or overbuilt in D66?

- `helpers/agent_bus_lxc/members.py` `GH_CHECK_SH` sources the member's env file so "a GH_TOKEN
  there counts". Harmless (gh honours it itself), but it is a second thought about credentials
  in a design whose point is one path. Not blocking.
- `helpers/agent_bus/teamfile.py` now refuses any `forge_api` other than GitHub's. Consistent
  with "single code path"; it does make the `forge_api` field a constant, which could be deleted
  under YAGNI later. Not blocking.
- The 15 s gh timeout, the "no oauth token" stderr match, and the error-message curation in
  `gh_token` are more than strictly needed but all fail fast. Fine.
- Version bump: the sub-agent left the ccy bump to the owner. The rule's paths
  (`files/var/local/claude-yolo/**`) are not touched by D66, and play-claude-yolo.yml rebuilds
  the image every run, so no bump is owed by the rule as written.
- The merge in progress has two unresolved files (`JOURNAL/00161-Journal-26-10-13.md`,
  `tests/helpers/agent_bus_lxc/test_members.py`); the production code is staged clean.

## 4. The way forward

1. **Merge D66's production path as staged** (forge.py, cli.py, teamfile.py, members.py check,
   READMEs, docs). It is the fix for the owner's point: pingbus asks gh; ccy already has gh
   logged in; nothing is provided.
2. **Swap the acceptance's provisioning for a stub gh** (or defer U23 and the M1 gh-login
   steps): remove the `dnf`/`apt` gh installs and the `gh auth login --with-token` legs; have the
   harness write a per-member 0600 token file and a two-line `gh` stand-in on the member's
   PATH, both torn down with the member. This can land as a follow-up commit; it does not block
   the owner's own testing, which is in ccy.
3. **Host run, via meta-deploy as already added (uncommitted):** `00161-agent-team-bus-matrix/ deploy.bash` — leg 3 reinstalls the host kit and pingbus zipapp, leg 11 runs
   play-claude-yolo.yml to rebuild the ccy image with the new pingbus. Then `ccy --teams …`
   sessions check references with the session's own gh login. Precondition the owner already
   meets: their gh logged in to github.com on the host (the launcher derives the container's
   `GH_TOKEN` from it).
4. Then the owner can start acceptance testing ccy-teams with agents in ccy sessions. The
   measured anonymous-quota failure (M2.8) cannot recur: there is no anonymous mode left.
