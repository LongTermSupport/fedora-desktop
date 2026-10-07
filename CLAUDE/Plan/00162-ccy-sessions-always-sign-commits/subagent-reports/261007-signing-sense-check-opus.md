# Plan 00162: sense check of ccy commit signing

Sense check of the owner's request that every ccy session signs its commits with the
session's GitHub SSH key. Evidence was read in the main checkout at F44 `7b3220a5`, from
inside a ccy session. Nothing was launched or deployed. Identifiers specific to this
install (account names, emails, key fingerprints, the repository slug) are left out on
purpose.

## Verdict

**Mostly already true, and already owned by Plan 00139.** Plan 00139 ("commit signing
everywhere", In Progress) has "every commit and tag signed by default ... inside ccy" as
its first goal. It shipped ccy signing in Phase 2 and redid it for the login keys in D5 /
Task 5.2. The owner confirmed a ccy commit Verified on GitHub (Task 4.3, `907704f6`). On
the default path, a commit made in a ccy session is signed with the session's SSH key and
GitHub shows it Verified. What is left is three narrower gaps, listed below. They are
refinements inside Plan 00139's scope, so **fold them into Plan 00139 as a new phase; the
owner can then delete Plan 00162.**

## 1. Overlap

| Plan  | What it covers                                                                                                                                                                                                                                                                                                                                    | Status                                                                                                                    |
| ----- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------- |
| 00139 | Goal 1: "Every commit and tag signed by default: on the host, in `cc` sessions, and inside ccy" (PLAN.md Goals). Task 2.1/2.2, then D5 + Task 5.2: ccy signs through an agent with the session's identity, and refuses a launch that cannot sign. Task 4.3 HOST: a ccy commit Verified on GitHub. Task 4.2: F\* ruleset requiring signed commits. | In Progress. Open: Task 3.1 owner step (server trust list), and a success criterion awaiting the owner's own confirmation |
| 00137 | The self-update server's trust in signers (D3, overruled by 00139 D4; `allowedSignersFile` holds a list of the owner's keys)                                                                                                                                                                                                                      | In Progress; its signing half is now 00139's                                                                              |
| 00116 | Deploy keys and `--ssh-agent` in ccy; says nothing about signing                                                                                                                                                                                                                                                                                  | In Progress                                                                                                               |
| 00161 | The reason for CCY 3.85.2: its U20 acceptance checkouts set `commit.gpgsign false` locally and launch with no identity (`4f7d5707`)                                                                                                                                                                                                               | In Progress                                                                                                               |

No other live plan touches commit signing (`grep -il sign CLAUDE/Plan/*/PLAN.md` matches
are incidental elsewhere).

## 2. Current behaviour, per launch route

The mechanism is `configure_git_signing`
(`files/var/local/claude-yolo/lib/ssh-handling.bash:1614-1726`), called once per launch
from `files/var/local/claude-yolo/claude-yolo:2170-2178` with `SSH_KEYS[0]` as the primary
identity. It reads `commit.gpgsign`/`tag.gpgsign` from the project's local config, falling
back to the `~/.gitconfig` copy (`:1627-1649`). With both off it returns and leaves the copy
alone (`:1650`). With either on, it requires `gpg.format ssh` (`:1657`), picks the key
(`:1663-1679`), and checks that a forwarded agent holds it (`:1682-1718`). It then appends a
`[user] signingkey` to the copy (`:1721`), and that last value overrides the host key and
every account `includeIf`. The include files sit at host paths the container cannot read
anyway.

| Route                                                                                                | Signed?                                                                                                                                                              | Key                                                                                    | Verified on GitHub?                                                            |
| ---------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------ |
| Interactive pick of a `github_<alias>` key                                                           | Yes                                                                                                                                                                  | The picked key, at its container mount (`key_0`), signed through the container's agent | Only if the commit's email is verified on that key's account (gap G1)          |
| Quick Launch                                                                                         | Same as the saved pick (`claude-yolo:1101-1103` replays `LAST_SSH_KEYS`)                                                                                             | Saved key                                                                              | As above                                                                       |
| `--ssh-key <path>`                                                                                   | Yes                                                                                                                                                                  | That key, whatever it is                                                               | Only if it is registered as a signing key on the account owning the email (G1) |
| The remote's ssh-config alias key (a deploy key)                                                     | Yes, when the host signs                                                                                                                                             | The deploy key                                                                         | **Never**: GitHub cannot hold a deploy key as a user signing key (G1)          |
| `--ssh-agent` (forwarded agent only)                                                                 | Yes, if the agent holds the key the host's git picks for the project; else launch refused                                                                            | `key::` literal of the host-picked key (`:1715-1716`)                                  | Same as on the host: yes on the default setup                                  |
| `--no-ssh`, or picker `0`, or no key found                                                           | Launch **refused** while signing is on (`:1675-1678`)                                                                                                                | n/a                                                                                    | n/a                                                                            |
| `--headless`                                                                                         | Same as the route its key arrived by; no signing-specific branch                                                                                                     |                                                                                        |                                                                                |
| Restart / restore relaunch                                                                           | Same key as before (`lib/restart-request.bash:259,361-365` carries `--ssh-key`/`--ssh-agent`/`--no-ssh`; the registry records a picked key, `claude-yolo:1141-1146`) | Same                                                                                   | Same                                                                           |
| Project with `commit.gpgsign false` locally                                                          | **Unsigned**, by design since CCY 3.85.2                                                                                                                             | none                                                                                   | n/a (G3)                                                                       |
| Host whose `~/.gitconfig` does not sign (a box with no GitHub identity, Plan 00139 Task 5.5, PR #56) | **Unsigned**, by the owner's decision on PR #56                                                                                                                      | none                                                                                   | n/a                                                                            |
| Per-account `includeIf` from `play-github-cli-multi.yml:701-740`                                     | Ignored in the container: the appended key wins, and the include paths are host paths                                                                                | The launch key                                                                         | See G1                                                                         |

Measured in this session:

- `git config --show-origin` in the container: `gpg.format ssh`, `commit.gpgsign true`,
  `tag.gpgsign true`, and `user.signingkey` twice, the host path first and then the
  appended `/root/.ssh/ccy-keys/key_0`, which wins.
- Commits since 2026-09-26 (all ancestry, 434): 415 SSH-signed, 17 PGP-signed (GitHub's
  web-flow key on PR merges), 2 unsigned (`3f50cc62`, `b4b1deb5`, 2026-09-29, before the
  F\* ruleset went live on 2026-10-02 per `f92bb624`). None are unsigned since the ruleset.
- 225 of the 254 non-merge commits since 2026-10-02 verify against this session's agent key
  (checked with a throwaway `allowedSignersFile` in `untracked/scratch/`, since deleted). The
  rest are other sessions' and the host's keys.
- GitHub API, the last 15 non-merge commits on `origin/F44`: every one
  `verification.verified = true`, reason `valid`.

Other observations:

- **No local verification inside ccy.** The container's git has no
  `gpg.ssh.allowedSignersFile`, so `git log --format=%G?` prints `N` and an error for every
  commit, signed or not. An agent cannot check its own signatures without a hand-made
  signers file. This is cosmetic; GitHub is the verifier that matters.
- **`podman exec` shells.** The container's agent is started in the entrypoint's process
  tree (`entrypoint.sh:162`), so a shell opened with `podman exec` has no `SSH_AUTH_SOCK`.
  A commit there signs by loading `key_0` directly. That works for a passphrase-free key
  and fails loudly for a passphrase-protected one. Either way it never produces an unsigned
  commit. Nothing in the repo commits that way today.

### G1: signed but Unverified when the key's account does not own the commit email

GitHub marks an SSH-signed commit Verified only when the key is a signing key on an account
that has the commit's email verified (`docs/configuration.md:243-257`;
`helpers/github_signing/signing.py:6-7,66-88`). On the host this is arranged: `~/.ssh/id`
signs by default and is registered on the account owning `user_email`, and an account's
key signs only in that account's repositories. In ccy the key **picked at launch** signs
every commit, whatever the repository's remote:

- The picker (`discover_and_select_ssh_keys`, `ssh-handling.bash:423-709`) ranks keys by
  **push access**, not by signing. Its default is the first `github_*` key (sorted by file
  name) whose account can push (`:571-598`). In a repository on the primary account where a
  second account can also push, the default can be that second account's key. Its commits
  then carry the primary account's email with the other account's signature, so they are
  Unverified. The host's git would have signed the same commit with `~/.ssh/id`, Verified.
- An alias repository on account B signs with B's key. The email is still the global
  `user_email` until `gh-switch <alias> --update-git` has been run in it, so the commit is
  Unverified. This is the same as on the host, and documented there.
- `--ssh-key` with any key, and the remote's deploy key, sign with a key GitHub cannot
  attribute to a user.

The launch line names the key (`✓ Commit signing: <key>, the session's SSH identity`) but
not whether GitHub will accept it. On a repository whose ruleset requires signed commits
(this one's F\* branches), the push is refused, which is loud. Everywhere else the commit
lands silently Unverified.

### G2: a server's trust list (already tracked)

A self-updating server deploys only commits signed by a key in
`self_update_signing_public_keys`. Plan 00139 Task 3.1's owner step is still 🚫: it adds
each `github_<alias>.pub` that ccy sessions sign with. Until then, a ccy-signed commit to
the deploy branch waits for one signed by a trusted key. It is not refused at push time;
the server just does not take it. This is already tracked, and nothing new is needed.

### G3: no client-side guarantee

`commit.gpgsign`, `git commit --no-gpg-sign` and the project's `.git/config` are all under
the container's control, and CCY 3.85.2 honours a local `commit.gpgsign false` on purpose.
So the launcher can make signing the default, but it cannot make it certain. The only
enforcement is server-side, through a GitHub ruleset requiring signed (Verified) commits.
Today that is on this repository's F\* branches only (Plan 00139 Task 4.2). For other
repositories ccy works in, that is an outward-facing setting and the owner's call.

## 3. Is there a reason not to sign?

No reason against it as the default. These are the cases where signing is off, or costs
something:

- **Throwaway acceptance checkouts** (Plan 00161 U20) launch with no SSH identity and never
  commit. They set signing off locally, and CCY 3.85.2 honours that. This is legitimate, and
  it is also the hole described in G3.
- **A box with no GitHub identity** (headless `RUN_BASH_GITHUB_ACCOUNTS=none`) does not
  sign. That was the owner's decision on PR #56 (Plan 00139 Task 5.5). Such a box has no
  key GitHub could verify anyway, except a forwarded agent's.
- **Deploy-key sessions** cannot produce a Verified commit by construction. A Verified
  commit needs a user key in the container, which reaches every repository the account can
  reach. That is a scope-versus-verification trade-off the owner has to make.
- **The self-update server** trusts listed keys only (G2). An untrusted signature is no
  worse than none: unsigned commits were never deployable either.
- **Tests** that create throwaway repositories isolate themselves from the machine's git
  config: `GIT_CONFIG_GLOBAL` or `--no-gpg-sign` in `scripts/test-self-update-cycle.bash`,
  `scripts/test-release.bash`, `scripts/test-ccy-git-signing.bash`,
  `scripts/qa-helper-tests.bash` and the `tests/helpers/*` suites. Plan 00139's first review
  fixed the one that did not.
- **Automation commits.** The only scripted commit is `scripts/release.bash:232`, which
  signs (`-S`). Nothing else in `playbooks/`, `files/`, `helpers/` or `scripts/` commits.
- **Cost.** One `ssh-keygen -Y sign` per commit through the agent, and a passphrase-protected
  key file has to be unlocked once at launch (or by askpass on a restore). Both are already
  in place.

## 4. Recommendation

All of this goes into Plan 00139 as one new phase. No new play is needed.

1. **Launch-time Verified check (closes G1).** In `configure_git_signing`
   (`files/var/local/claude-yolo/lib/ssh-handling.bash`), when signing is on and the key is
   a file, ask GitHub, with the session's account token that ccy already holds and checks
   against the key, two things:

   - is the key's public half in that account's `user/ssh_signing_keys`? (scope
     `admin:ssh_signing_key`, already in `vars/github-required-scopes.yml`)
   - is the commit email the container will use (the project's local `user.email`, else the
     copy's) in that account's verified `user/emails` (scope `user:email`), or its noreply
     address?

   If either answer is no, refuse the launch and give the remedy: pick the key of the
   account that owns that email, or run `gh-switch <alias> --update-git` in the project, or
   register the key (re-run `play-github-cli-multi.yml`). The noreply rule already exists in
   `helpers/github_signing/signing.py:21`; reuse it, do not copy it. The forwarded-agent
   route needs no new check: it signs with the key the host's git picks, which the play has
   already proven. Owner decision: the deploy-key route can never pass, so either it refuses
   (consistent with fail fast) or it gets an explicit acknowledgement. Pick one; do not
   warn and continue.

   - **Picker.** Among keys that can push, rank first the one whose account owns the commit
     email, so the default passes the check above.
   - **Test.** New cases in `scripts/test-ccy-git-signing.bash` (gate `ccy-git-signing`),
     with `gh` stubbed on `PATH`: a key registered with the email verified passes; a key on
     the wrong account, an unregistered key, an unverified email, and a deploy key each
     refuse and name the remedy. The cases must fail red against the current launcher.
     Bump the CCY version and add a `docs/ccy-changelog.md` entry. `docs/ccy.md` and
     `docs/configuration.md` "Commit Signing" describe the check.
   - **Acceptance (owner, at a desk).** Launch ccy in a repository where a second account
     has push access, accept the default key, make a commit, push it to a scratch branch,
     and read GitHub's `verification.verified` for it. The meta-deploy only re-runs
     `play-claude-yolo.yml` to install the launcher.

2. **Optional: verification inside the session.** The launcher writes the session key's
   public half to an allowed-signers file beside the gitconfig copy and sets
   `gpg.ssh.allowedSignersFile` to it, so `git log --format=%G?` reads `G` in the
   container. It costs a few lines in the same function. Include it only if the owner wants
   agents to be able to check their own signatures.

3. **Owner decision, not code (G3):** whether repositories other than this one get a
   "require signed commits" ruleset. This is the only real guarantee. Plan 00139 Task 4.2
   did it for this repository's F\* branches.

4. **Already tracked:** Plan 00139 Task 3.1's owner step (G2).

Leave alone: CCY 3.85.2's local-off handling (Plan 00161 needs it), and the no-identity box
(the owner's PR #56 decision).
