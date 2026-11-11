# Owner/repo case-folding search

Dispatch did not contain the rule or its pattern; searched from the class description alone. Did not open src-go/qa/rules, go-qa.toml or any detector config; did not run the detector. No vault/secret files read.

Fold = ASCII lower-casing before a lower-case-only grammar (`[a-z0-9](?:[a-z0-9-]{0,37}[a-z0-9])?`, `[a-z0-9._-]{1,100}`).

## A. Instances that carry the hazard (external input, lower-case-only grammar, not folded)

1. src-go/internal/teamchat/declare/values.go:137-146 `node.trust()` -> proto.DecodeForge (proto/roster.go:150) -> Forge.Validate (roster.go:249 `ValidOwner(f.Org)`, :251 `ValidOwnerRepo` for exclude, :259 `ValidOwnerRepo` for repos) -> proto/refs.go:22-33,54-60. ORIGINATING INSTANCE. Input: `org`, `repos[].repo`, `exclude[]` of the trust section. Confident. Callers of `trust()`:
   - declare/declare.go:296 (`team apply`, declaration on stdin from the play: play var). Hazard, confident.
   - declare/gate.go:420 `changeTrust` (`team approve`, a `--set trust=<json>` CLI value). Hazard, confident.
   - declare/gate.go:513 reads the stored `approved` object: canonical stored data (section C), not a hazard itself, but it shares the function, so a change to `trust()` reaches it.
   Exact-compare sites after validation: proto/refs.go:174 (`slices.Contains(f.Exclude, name)`), :178 (`r.Owner == f.Org`), :195 (`f.Repos[i].Repo == name`), roster.go:255 (`slices.Contains(f.Exclude, repo.Repo)`). They are safe only because the grammar refuses upper case; if a mixed-case value got in, it would be a different owner. Unsure whether they count as instances.

2. src-go/internal/teamchat/member/client/send.go:40 (ping `ref` positional) and :45 (reply `--ref`), then engine/send.go:130-131 and engine/reply.go:91-92 set `p.Ref = *a.Ref` raw, then composer -> proto/verdict.go:570 `refShape` -> `ParseRef` (refs.go:98-127, patterns refs.go:36-39). Input: a person or agent typing `teamchat ping <verb> commit:ExampleOrg/Repo@<sha>`. OWNER/REPO inside the reference is not folded; the ping is refused with reason ref (exit CodeRef). Confident. `proto.LowercaseRefRepo` (refs.go:138-146; doc: "as a sender does before validating (A 9.1)") has NO production caller: grep finds only refs_test.go. The v1 Python does fold: helpers/pingbus/cli.py:440 and :1137 call `protocol.lowercase_ref_repo`. This is the `commit:OWNER/REPO@SHA` case in the dispatch; same for the `path:`, `pr:`, `issue:` forms.

3. src-go/internal/bus/declare/queries.go:217-226 `checkedRepo` (refuses: "is not a lower-case OWNER/REPO"), called at :268 `TeamsFor` and :295 `Choices`; `isOwnerRepo` at grammar.go:47. Input: `--repo` of declare teams-for / choices. The Go callers (teams/find/find.go:40, teams/picker/offers.go:382, teams/chooser/chooser.go:333, teams/create/wizard.go:1138) pass a value from `GithubRemote`, which folds (grammar.go:59), so those are safe. Python twin helpers/agent_bus/declare.py:~929-931 `_checked_repo`, argparse `--repo` at :1771, :1774, :1777: a person running the Python CLI directly is refused on mixed case. Confident for the Python CLI, unsure for Go (no direct person input found).

4. src-go/internal/bus/declare/queries.go ~:230-260 `standing()`: `pycompat.Equal(repo, full)` and `pycompat.Equal(org, owner)` compare host_vars text (`org`, `repo` as a person wrote them in localhost.yml) with the lower-case owner from the checkout, exactly. A mixed-case `org: ExampleOrg` gives StandingWiden. Python twin: helpers/agent_bus/declare.py `_standing` (after :935). Unsure: whether the declaration reader already refuses mixed case upstream; not traced.

5. helpers/agent_bus/teamfile.py: :231-234 `_owner_repo` and :286 (repos[].repo) refuse "must be lower-case OWNER/REPO"; :238-242 `_org` refuses "must be a lower-case GitHub owner" via protocol.is_owner (protocol.py:554-556); :250-258 `_exclude` uses `protocol.is_repo_name` and compares `f"{org}/{name}"` to repos exactly. Input: the team declaration generated from the `agent_bus_teams` play variable (environment/localhost/host_vars/localhost.yml.dist:128-131 documents `org: <github-owner>`, `repo: <owner>/<repo>`). Confident. The refusal is deliberate in v1 ("lower-case") but is the same class and hazard.

6. helpers/agent_bus/declare.py:797-799 (`--org`, `--repo` placed into the team file raw) and :1735-1738 (argparse `--org`, `--repo`), validated by teamfile.py above. Input: CLI flags. Python does not fold; the Go wizard folds first (section B). Confident for direct Python CLI use.

7. helpers/pingbus/protocol.py:1084 (`is_owner_repo(name)` on repos), :1139 (`is_owner(org)` in the record content), :1097 `is_repo_name`. These validate the team record (team.json written from item 5's output): canonical stored data. Unsure; listed so it can be told apart (section C).

## B. Already folded (no hazard)

- src-go/internal/teams/create/wizard.go:242 (`--org` ToLower), :244 (`--repo` ToLower), :528 (repos prompt), :543 (`o.org = ToLower(owner)`), :827 (`checkOwner`; its comment says GitHub names are not case-sensitive), :856 (`checkRepos`). This is the ccy-teams create wizard.
- src-go/internal/bus/declare/grammar.go:54-63 `GithubRemote` (pycompat.Lower of owner/repo from the remote URL); Python twins helpers/agent_bus/declare.py:1618 and helpers/pingbus/cli.py:586 (`.lower()`).
- grammar.go:82-92 `normaliseRepo` and :95-104 `resolveHost` fold (a handle's repo part, a host); Python registry.py:72, :92.
- helpers/pingbus/cli.py:440, :1137: `lowercase_ref_repo(args.ref)` before send (v1 equivalent of item 2).
- helpers/pingbus/forge.py:349 `login.lower() != org` and :501 `full_name.lower() != ref.repo_full`: API answer folded before comparing. Go twin: src-go/internal/teamchat/member/forge/forge.go:308, :310, :448 use `strings.EqualFold`.
- Mixed-case or EqualFold GitHub identity handling: src-go/internal/ghlogin/ghlogin.go:31; ghaccount/app.go:323, :500, :503, :683, :769, :927 and ghaccount/config.go:67, :147; ghaccount/args.go:29 `loginPattern`; vmtest/hostonly.go:22 `githubUserRE`; release/pulls.go:18 `repoName` and :112; watchlist/list.go:30 `urlPattern`, list.go:87 and app.go:129-131 (lower-case the key); helpers/github_signing/signing.py:23.

## C. Legitimate reads of canonical stored or wire data (strict grammar is correct)

- src-go/internal/pingbus/statusline/teamrecord.go:28-30 `ownerPattern`, `ownerRepoPattern`, `repoNamePattern`; used at :119 (`org`) and :163 (`repos[].repo`), `isOwnerRepo` :217. Reads the stored team record (golden oracle against Python).
- `ParseRef` on received or stored events: member/state/store.go:463 (`validRef` on stored in-flight sends and pings), proto/envelope.go:365 (carried event content), proto/verdict.go:633 (carried item), member/forge and deliver (a ref already on the wire).
- declare/gate.go:513 (stored `approved` record); `Forge.Validate` when the roster is decoded from a homeserver event.
- helpers/pingbus/protocol.py:293-301 `_OWNER_RE`, `_OWNER_REPO`, ref patterns, :583-586, :1084, :1139: wire and record validation.
- proto/ids.go:32 `repoPattern` (`[a-z0-9][a-z0-9_-]{0,47}`) is a handle's repo part, a normalised derived name; same for teams/membership/membership.go:36, bus/seat/member.go:62, pingbus/statusline/member.go:41.
- Not GitHub identities: team names, seat names, host DNS labels, container/user/group names, kinds, server names (matrix, seat/homeserver.go:23, pki/names.go:17), playbooks/imports/play-teamchat.yml:89 (an OS group name).

## D. Which technique found what

Text search found, and reading alone would not have reliably found: the full list of lower-case grammar sites (`a-z0-9` over src-go, helpers, files, playbooks, scripts); that `LowercaseRefRepo` has no production caller (symbol grep: refs.go and refs_test.go only); every `ToLower`/`EqualFold`/`.lower()` site (section B); every `ValidOwner`/`ValidOwnerRepo`/`is_owner_repo`/`is_owner` caller; the `--org`/`--repo` flag sites.

Reading found, and text search could not have: that `trust()` is reached by three paths with different origins (apply stdin, approve `--set`, stored `approved`) and which are canonical; that the ping/reply `--ref` flows raw through client/send.go, engine/send.go and engine/reply.go into `ParseRef` with no fold (a missing call leaves no hit); that Go callers of `TeamsFor`/`Choices` receive their value from the folding `GithubRemote`, so item 3 is only a direct-CLI exposure; the exact-compare sites in refs.go:174-195 that rely on the grammar having refused upper case; that `standing()` compares host_vars text exactly.

## E. Searches run

- `a-z0-9` over *.go *.py *.bash *.sh *.yml in src-go (vendor and qa/rules excluded), helpers, files, playbooks, scripts.
- `ValidOwner|ValidRepo|DecodeForge|ParseRef|ValidOrg|ValidLogin|ValidSlug`.
- `ownerPattern|repoPattern|ownerRe|ownerRepo|OwnerRepo|normaliseRepo|ValidSite`.
- `ToLower|EqualFold` (file list over src-go), then `Lower(|asciiLower` in teamchat, teams, pingbus, bus, declare.
- `checkedRepo|GithubRemote|--repo|--org|--owner`; `LowercaseRefRepo`.
- Python: `is_owner_repo|OWNER_RE|is_owner(`, `.lower()|casefold|IGNORECASE`, `--org|--repo`.
- `"ref"|--ref|\bRef\b` over teamchat/member; `trust|forge|org` over teamchat/declare, play-teamchat.yml and localhost.yml.dist; playbooks `regex_search|match(` with owner/org/login.
- Forms looked for: regex grammars, `ValidX` helper calls, `==`/`slices.Contains`/`pycompat.Equal` on owner or repo, CLI flags for org/repo/ref, declaration keys `org`, `repos`, `exclude`, `trust`, Jinja `match()`.

Saturation: the last searches (Python helpers, playbooks, bash scripts, files/**/bin) turned up no new input site; no bash script or playbook task validates a GitHub owner/repo against a lower-case grammar.
