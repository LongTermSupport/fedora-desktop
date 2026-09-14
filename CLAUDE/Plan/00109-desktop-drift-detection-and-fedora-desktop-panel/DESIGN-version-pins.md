# The installed-vs-pinned check — design

Task 2.2's design output. Referenced from `PLAN.md`; this file owns the detail.

The third drift axis, and the only one that was not watched.
`scripts/check-pinned-versions.bash` compares the repo pin against **upstream latest** and
correctly said "up to date". `scripts/qa-deployed-drift.bash` compares repo scripts against their
**deployed copies** and correctly said "in sync". Neither asks whether the host has what the repo
says it should — which is the axis `evdi` drifted on, a minor version behind a pin set months
earlier, with every gate green and both monitors dark after a reboot.

## 1. Why the manifest had to move first

The pin manifest was a heredoc inside `check-pinned-versions.bash`. A second consumer could only
have copied it, and two checks asking different questions of populations that can silently differ
is how one of them ends up narrower than the other without anyone noticing.

It is `vars/version-pins.yml` now — the shape `vars/gnome-shell-extensions.yml` took in Plan
00112 — and both consumers read it. The extraction was **verified, not asserted**: the heredoc
taken straight out of `git show HEAD:` diffs clean against the rows the real validator emits from
the YAML.

`scripts/qa-version-pins.bash` validates it on every `qa-all` run. That gate has to exist because
**neither consumer runs there**: the review tool needs an authenticated `gh`, and this check needs
a real host. Without it, a row that had drifted away from the playbooks would surface only when
somebody happened to run a review tool — and the specific rot is quiet, because a row naming a var
that was renamed reports the old pinned value for ever, which reads as "up to date".

## 2. Resolution is declared per pin, and guessing is worse than declining

| `installed:` kind   | Meaning                                            |
| ------------------- | -------------------------------------------------- |
| `dkms` + `name`     | the module's version from `dkms status`            |
| `rpm` + `name`      | the package's version from the rpm database        |
| `command` + `name`  | the first version-shaped run in `<name> --version` |
| `untracked` + `why` | a recorded decision not to compare this pin        |
| *absent*            | **rejected** — "nobody decided" is not a state     |

The worked example is in the manifest itself. `displaylink_version` pins the displaylink-rpm
**release tag** (`v6.3.0-1`), while the installed rpm's own version tracks *evdi*
(`displaylink-1.14.16-2` during the incident). A plausible-looking `rpm` resolver there would
report a permanent false finding — and a check that cries wolf gets muted just as surely as one
that never speaks.

So most rows are `untracked` with *"not yet established"*. That is a to-do **declared in a tracked
file and counted on every run** — the gate prints `N with install state tracked, M declared untracked` — rather than an absence that reads like a clean result. One of nine is tracked today,
and the report says so out loud.

## 3. The gate, in both directions

`PLAN.md` demanded a check that fails against the 2026-09-11 state. Both halves are asserted
against the strings this plan's own `JOURNAL/` recorded:

| Input                                     | Pin      | Verdict                   |
| ----------------------------------------- | -------- | ------------------------- |
| `evdi/1.14.16, …: installed`              | `1.15.0` | **BEHIND** — a finding    |
| `evdi/1.15.0-1.github_evdi, …: installed` | `1.15.0` | MATCH — clean             |
| no `evdi` entry at all                    | `1.15.0` | **ABSENT** — a finding    |
| `dkms` not installed                      | `1.15.0` | **a finding**, not a pass |

The release suffix matters: the post-fix version really is `1.15.0-1.github_evdi`, and a
comparison that could not see past it would report drift on a healthy host for ever. Ordering is
numeric and canonical — as strings, `1.14.16` sorts *before* `1.14.9`.

## 4. Nothing here has a "cannot tell" that passes

`compare.UNDETERMINED` is a finding. `parse_version` **raises** rather than returning a sentinel,
because two sentinels compare equal and two unreadable versions would therefore report MATCH — a
silent pass on precisely the axis this exists to watch.

`check()` catches broadly and turns any failure into a finding naming the pin. That breadth is
deliberate: this runs at the end of a login, so an escaping exception takes the whole surface down,
and a user who sees nothing cannot distinguish that from a healthy host. It is the same rule as
[DESIGN-host-health.md](DESIGN-host-health.md) §3 and the ledger's `BROKEN` sentinel.

The one thing deliberately **not** probed is an untracked pin — not even to fail. Probing it would
produce a finding about a question nobody decided to ask, on a host that may legitimately have no
`dkms` at all.

## 5. Where it runs

Not in `qa-all.bash` — see [DESIGN-play-ledger.md](DESIGN-play-ledger.md) §7 for why neither drift
check belongs there. Its **tests** are in `qa-all` via `qa-helper-tests.bash`, and its **manifest**
is validated there by `qa-version-pins.bash`; the check itself belongs to Phase 3's login surface,
where its findings merge into the one report in `login_report.collect` — the layer that guards
each check separately, so this one raising cannot silence the other two.
