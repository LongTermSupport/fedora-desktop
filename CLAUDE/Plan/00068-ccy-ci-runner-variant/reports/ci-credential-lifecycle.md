# Plan 00068 — the CI credential: ccy already solves this

**This document previously specified a vaulted variable plus a `gh secret set` play pushing the token
into GitHub Actions secrets, with an expiry-metadata scheme on top. All of that is retracted as
wheel-reinvention.** It is recorded rather than deleted because it is the third instance of one
mistake in this plan, and the pattern is more useful than the design was.

## What actually exists — and it is already solved

The CI runner is a **fedora-desktop-provisioned VM**. It therefore already has ccy, and ccy already
has host-level token persistence:

| Piece                   | Where                                                                                    |
| ----------------------- | ---------------------------------------------------------------------------------------- |
| Token store             | `~/.claude-tokens/ccy/tokens/*.token` (`play-claude-yolo.yml:311-327`)                   |
| Creation                | `ccy --create-token` — interactive OAuth device flow                                     |
| Listing / selection     | `ccy --list-tokens`, `ccy --token <name>`; `select_token` in `lib/token-management.bash` |
| Naming, carrying expiry | `${token_name}.${expiry_date}.token` (`token-management.bash:199`)                       |
| Delivery into container | `-e CLAUDE_CODE_OAUTH_TOKEN`, **by name** (`claude-yolo:2777`)                           |

**Updating the token on the runner is the same operation as updating it on any other
fedora-desktop box**: `ccy --create-token`. Nothing in this plan needs to add a mechanism, a
variable, or a distribution path.

## What was wrong with the retracted design

- **GitHub has no need for the token.** The runner is self-hosted and provisioned by this estate;
  the credential never has to leave the host it was created on. A GitHub secret would be a *second*
  copy of a secret that already has a home, with a wider blast radius and a sync problem.
- **No vaulted variable is needed.** The token already persists on the host in ccy's store. Vaulting
  it would create a second source of truth for something that is not currently duplicated.
- **The expiry-metadata scheme solved a problem the retraction removes.** It existed only because a
  GitHub secret is an opaque string with no expiry. ccy's own store keeps expiry **in the filename**,
  which is why `ccy --list-tokens` and the token-expiry work can see it at all.

## A correction I owe the owner separately

The retracted version claimed the assumed process was *"runner runs ansible, ansible decrypts the
vaulted string"*, and refuted it on the grounds that the vault passphrase must never reach the runner.

**The owner never suggested that.** They said *they* would update the vaulted string and run ansible
— the ordinary host workflow. I invented a position, attributed it, and then corrected it. The
vault-passphrase constraint is real and still holds; it was simply not a rebuttal to anything anyone
had proposed.

## The pattern — third instance in this plan

| #   | I specified                                       | What already existed                                      |
| --- | ------------------------------------------------- | --------------------------------------------------------- |
| 1   | A `LABEL` identity convention for image staleness | `podman build` — it *is* the staleness check              |
| 2   | Ansible-built CI images, never per-job            | the project's own Dockerfile, the seam ccy already has    |
| 3   | Vault + `gh secret set` + expiry metadata         | ccy's host-level token store, with expiry in the filename |

Each time the specified thing was coherent, cited, and unnecessary. The common cause is reaching for
the industry-standard answer to a sub-problem before asking what this estate already does — and the
owner's steer has been the same in all three cases: **use ccy the normal way.**

The check that would have caught all three, and which belongs in the plan's working method: *before
specifying a mechanism, name the existing thing it replaces, and state why that thing cannot do the
job.* If no such statement can be written, the mechanism is not needed.

## Open

Whether the CI job invokes `ccy` itself, or a separate mechanism reuses ccy's token store, is not
settled here — see the plan's open decisions. It materially changes Phase 2's status: if CI runs the
launcher, then `--non-interactive` is a **CI enabler**, not the desktop-only hardening this plan
currently records.
