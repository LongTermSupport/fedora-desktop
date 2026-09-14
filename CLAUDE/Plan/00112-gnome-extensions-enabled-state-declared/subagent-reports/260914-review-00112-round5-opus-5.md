# QA Review — Plan 00112, commit `49717c3f` (round 5, narrow verification)

Reviewer: qa-reviewer (Opus 5). Read-only — nothing in the reviewed tree was modified. Every
measurement below ran in memory through process substitution, with no file written; this report
is the only file this review created.
Scope: verification of the five items round 4 raised, plus anything the edits broke. Rounds 1–4
are closed.

**Verdict**: FIX-BEFORE-MERGE.

**The decision asked for: Task 2.3 is not safe to tick as it stands — but everything round 4
raised is genuinely landed and independently verified.** Two new text-only over-claims remain,
both one-clause edits in files already in this commit, neither needing a host. Fix those and 2.3
is clear. Task 2.2's Plan 00117 blocker and Task 2.1's operator step did not enter this
judgement.

---

## Should fix

### 1. "the four shapes `validate_uuid` rejects" is five

`PLAN.md:118`, and the same undercount in `JOURNAL/00112-Journal-26-09-14.md:374-378` and in the
commit message.

`helpers/gnome/enabled_extensions.py:135` is
`_FORBIDDEN_IN_UUID = {",": …, "\n": …, "\r": …, " ": …}` — four characters — plus the empty
check at `:149-150`. That is **five** rejected shapes. Empty, space, comma and newline were
measured; **carriage return was not**, and the sentence is written as an exhaustive sweep
("Measured across the four shapes… and **only** a newline-bearing one fails").

I measured all five, driving the real builder body (extracted from
`scripts/git-hooks/lib/secret-scan.bash` with `awk`, not retyped) and then the real
`hook_filter_match_lines`, with `grep` resolved to GNU grep 3.8:

| `uuid:` shape       | builder | emitted entry                   | filter                                |
| ------------------- | ------- | ------------------------------- | ------------------------------------- |
| empty               | rc 0    | (skipped, `:141-142`)           | rc 0                                  |
| space               | rc 0    | `^Vitals@Core\ Coding\.com$`    | rc 0                                  |
| comma               | rc 0    | `^Vitals,@CoreCoding\.com$`     | rc 0                                  |
| **carriage return** | rc 0    | `^Vitals@Core\<CR>Coding\.com$` | rc 0                                  |
| newline             | rc 0    | two broken lines                | **rc 1** — `grep: Trailing backslash` |

The conclusion survives intact: the CR case behaves exactly as space and comma do, and in every
non-failing shape the real address was still reported, so nothing widens the exemption.

**Fix**: say *five*, and add that a carriage return behaves as the space and comma do.

Everything else in that sentence is now exactly true, including the distinction round 4 did not
state in those terms: the builder returns 0 in all five, and the failure is at the filter.
`grep: Trailing backslash` is GNU grep 3.8's verbatim wording, which is what the hook gets
(`env -i bash -c 'type -a grep'` → `/usr/bin/grep`); an interactive shell here has a `grep`
function shim that runs ugrep and says `invalid escape` instead, so anyone re-measuring must
`unset -f grep` first or they will get a different string and conclude the parenthetical is wrong.

### 2. The EXT-13 closure claims an equivalence the finding itself contradicts

`CLAUDE/Plan/00049-full-repo-audit/research/extensions.md:170-175`.

The note says the chosen form is "the same outcome" as the recommended file loop "without
enumerating filenames a third time". It is the same outcome for modes and ownership. It is **not**
the same outcome for the other half of EXT-13, two paragraphs below at `:177`: *"The
whole-directory copy also deploys `README.md` unnecessarily and would deploy any stray file added
to the source dir."* Ansible's own `_walk_dirs` over the source confirms all four files still
ship, `README.md` included:

```
files: stylesheet.css, extension.js, metadata.json, README.md
directories: []   symlinks: []
```

So an unqualified **CLOSED** plus "same outcome" tells a future reader a recorded concern is
resolved when it is untouched. The note was careful enough to flag its own stale line and
task-name citations; it should be equally precise here.

**Fix**: one clause — same outcome for mode and ownership; `README.md` and any stray file in the
source dir still deploy, so that half of EXT-13 stays open.

---

## Verified — round 4's findings, re-measured independently

- **Should-fix 1 (deferral reason)** — accurate now apart from the count above. Measured as
  tabulated. `"hard-fails, confirmed"` is gone and the replacement matches the code's actual
  behaviour, including "fails at the filter, not the builder".
- **Should-fix 2 (rename consequence)** — accurate in both `PLAN.md:90-96` and
  `playbooks/imports/play-gnome-shell-extensions.yml:110-115`, and the over-claim is gone.
  Driving the real applier for the vars-only-rename scenario:

  ```
  rc = 1
  stdout: GNOME-EXT-FAIL declared-extension-not-deployed
  stderr: declared but not found under /workspace/extensions: …-RENAMED. …
  ```

  The old task (`git show d13c10cf:…:110-116`) did spell the UUID in `src`/`dest` with
  `mode: '0755'` and no owner/group, so a vars-only rename deployed the **old** directory and the
  applier then failed the play. "Loud, not silent, but a failure whose cause sat two files from
  its symptom" is exactly right.
- **Should-fix 3 (file-task rule) — fixed, and the mode semantics are correct.** All four source
  files are `0644` in the repo and none needs an exec bit: `extension.js` is imported by GJS,
  `metadata.json` is parsed, `stylesheet.css` and `README.md` are data. The directory needs only
  the search bit and gets it — `directories: []` from the walk means the dest directory is created
  by the copy *module's* `os.makedirs` path (`ansible/modules/copy.py:551-566`), which applies
  `directory_args` (owner/group from `load_file_common_arguments`, mode overridden to
  `directory_mode`) through `adjust_recursive_directory_permissions`. So on a fresh install the
  extension dir lands `0755` owned by `user_login` and the shell can traverse it; on an existing
  host `directory_mode` leaves the already-0755 dir alone by documented design, and only the four
  files flip `0755 → 0644` — the one-time change that needs the Task 2.1 deploy. `owner`/`group`
  set to `user_login` under `become_user: "{{ user_login }}"` is a chown-to-self, which needs no
  privilege. Idempotent on the second run. The form matches
  `playbooks/imports/optional/common/play-container-watch.yml:101-108` exactly, and `0644` for
  `extension.js`/`metadata.json` matches `play-remote-desktop-toggle.yml:105-113` too.
  `CLAUDE/AnsibleStyle.md:102-104` is satisfied.
- **EXT-13 marking, otherwise** — the code facts in the note are all true (task name, the four
  params, the loop over `gnome_shell_extensions.custom`), the heading and its anchor are untouched
  so `triage.md:142`'s link still resolves, and `triage.md`'s `Verified` column records
  triage-time confirmation rather than fix status, so leaving it blank is not a contradiction. The
  audit record is otherwise undamaged.
- **Nit 1** — `Deploy Declared Custom Extensions` at `:120`; no longer overclaims, and no `: -x`
  pattern to trip the ansible-core 2.19 task-name parser.
- **Nit 2 — re-measured with my own baseline control, and the comment is exact.** Mutating the
  emitted pattern through a shim over the real builder, running the real suite each time, with the
  emitted pattern printed from inside the harness (the control the journal says the first attempt
  lacked):

  | variant             | emits                     | result                                    |
  | ------------------- | ------------------------- | ----------------------------------------- |
  | baseline (verbatim) | `^Vitals@CoreCoding\.com$`| 29 of 29 green, 0 failures                |
  | `^` dropped         | `Vitals@CoreCoding\.com$` | 1 failure — `UUID_WITH_HEAD`              |
  | `$` dropped         | `^Vitals@CoreCoding\.com` | 2 failures — `UUID_WITH_TAIL`, `EMBEDDED` |
  | both dropped        | `Vitals@CoreCoding\.com`  | 3 failures, 26 green                      |
  | `re.escape` dropped | `^Vitals@CoreCoding.com$` | 1 failure — the wildcard near-miss        |

  `scripts/test-secret-scan.bash:196-198` matches this exactly. My own first attempt at the
  both-anchors variant broke in the same `$$`-through-two-quoting-layers way the journal
  describes and produced seven failures — the baseline is what caught it.
- **Nit 3** — the deferral is now a plain `- **Deferred…**` bullet (`PLAN.md:116`), not a
  `- [ ] ⬜` inside a ticked task. Addressed.
- **Nit 4** — the new entry carries `## 14:22 · correction · T1.5/T1.8 — …`, matching the file's
  first nine. The 12:36 and 13:12 entries still lack the fields and cannot be retro-fixed
  (`journal-append-only`). Conforming new entries was the only available fix.

---

## Checked and clean

- **Cross-file contradiction sweep.** Only `00049/research/extensions.md:177` still carries the
  old task name, and the note above it declares that citation pre-fix. `docs/playbooks.md:534-541`
  makes no mode or permission claim and its "Nothing keeps a second copy" is true. No other file
  in the repo cites the renamed task.
- **Remaining "measured"/"confirmed" claims.** `PLAN.md:73` (Task 1.4), `:137` and `:142` ("eight
  shapes" — the list enumerates 3 pass + 5 fail = 8, self-consistent), and the play's own
  `verified`/`PROVES` wording are unchanged by this commit and hold. The one stale figure is a nit
  below.
- **Public-repo safety, non-vacuously.** 255 added lines, **0** match the email pattern at all
  (positive control: the same probe sees a planted address and a UUID on adjacent lines), so
  nothing needed the exemption; no `/home/…` paths, IPs or `.local`/`.lan` hostnames in added
  lines.
- **Fail-fast.** No `failed_when` / `ignore_errors` / `FAIL-FAST-OK` / `|| true` added anywhere
  (the only matching added line is prose inside the round-4 report). No `shell:` block, no
  skip-and-continue logic. The applier task still fails the play on the applier's rc 1.
- **Version bumps.** Not applicable — no path under `files/var/local/claude-yolo/`, and no
  Dockerfile, entrypoint, patch script or deployed skill in the diff. The play keeps its shebang
  and is tracked `100755`.
- **Plan Commit Rule.** Plan, journal, research note and code landed in one commit; the journal
  entry is appended, not rewritten; Task 2.3 correctly left unticked. `## F44...origin/F44` —
  nothing unpushed. The modified/untracked files in `git status` are another session's
  `helpers/play_ledger/` work and were not swept in.

---

## Nits

1. `scripts/test-secret-scan.bash:192` — "measured: a variant with the anchors dropped ships
   `passed: 24`". Today that variant ships 26 green, not 24; 24 was the suite's total before the
   three anchor cases and two escape cases were added (24 + 3 + 2 = 29). True as history, stale as
   a number a reader would reproduce. The sentence it supports — "every case above STILL PASSES" —
   is still true today.
2. `playbooks/imports/play-gnome-shell-extensions.yml:117` — "an extension is JS, JSON and CSS"
   enumerates three file types for a directory that also ships `README.md`: the same file that
   should-fix 2 above is about.

---

## Mechanical gates

| Gate                                                              | Result                                                                                                                                                                                     |
| ----------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `./scripts/qa-all.bash`                                           | PASS, exit 0 — 837 files; `version-pins: VERSION-PINS-OK 9 pin(s)` green; `secret-scan-tests: passed: 29`; `ansible-syntax: 79 playbooks OK`; `helper-tests: Ran 1057 tests`; `extension-compat: All 4 extension(s)` |
| `scripts/test-secret-scan.bash`                                   | triggered (the suite changed) — standalone `passed: 29   failed: 0`, exit 0                                                                                                                |
| `ansible-playbook --syntax-check play-gnome-shell-extensions.yml` | PASS, exit 0                                                                                                                                                                               |
| `hooks-daemon plan-qa --sweep`                                    | exit 1, 0 block / 2 advise — Plan 00046 stale path, journal-freshness on 12 older plans. 00112 in neither; identical to rounds 1–4                                                          |
| `hooks-daemon docs-qa --sweep`                                    | exit 1, 0 block / 37 advise — module-doc budgets and duplicate blocks, all repo-wide and pre-existing. No finding names any file in this commit                                            |
| `scripts/qa-helper-tests.bash`                                    | not triggered — no `helpers/` or `tests/helpers/` file in the diff; `qa-all.bash` ran it anyway, 1057 tests                                                                                 |
| `python3 -m helpers.gnome.check_extension_compat`                 | not triggered — no `extensions/**/metadata.json` in the diff; `qa-all.bash` ran it anyway, 4 extensions OK                                                                                  |
| `extensions` ESLint                                               | not triggered — no extension JS in the diff                                                                                                                                                |

No playbook was run — `/workspace` is a CCY container. Every claim came from the code, from
ansible-core's own action plugin and module source, or from the real scanner and applier functions
executed in memory.

---

## The minimum remaining

Two clauses, both text, both in files this commit already touches, neither needing a host:

1. `PLAN.md:118` — "four" → "five", and name the carriage return as behaving like the space and
   comma. (`JOURNAL/00112-Journal-26-09-14.md:374-378` is append-only; correct it in the next
   entry, not in place.)
2. `CLAUDE/Plan/00049-full-repo-audit/research/extensions.md:170-175` — qualify "the same
   outcome": same for mode and ownership, and `README.md`/stray files still deploy, so that half
   of EXT-13 stays open.
