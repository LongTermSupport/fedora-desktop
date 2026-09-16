# Report a daemon defect upstream

Drive the whole procedure for filing a defect against the hooks daemon's own
repository — the checks that come before filing, the generator that builds the
body, and the filing itself.

**📖 SINGLE SOURCE OF TRUTH: `BUG_REPORTING.md` at the daemon's root** — in a
client install that is `.claude/hooks-daemon/BUG_REPORTING.md`; on a
self-install it is `BUG_REPORTING.md` in the repository root; online it is
<https://github.com/Edmonds-Commerce-Limited/claude-code-hooks-daemon/blob/main/BUG_REPORTING.md>.
Read it and follow it. What is here is the procedure in the order you execute
it; the reasoning lives there.

(The path is spelled out rather than linked because this file is COPIED into
every install, and no single relative link is correct from both the daemon
repository and a client's `.claude/skills/`.)

## Usage

```claude-code
/hooks-daemon issue-report
```

## The one thing to internalise first

**That tracker is PUBLIC and an issue cannot be retracted.** Editing leaves the
original in the edit history; deleting does not reach what GitHub already
served, emailed and indexed. The cost of disclosing something private is
permanent and it is the USER'S, while the cost of leaving something out is one
comment asking for it. Those are not comparable.

So never put into a report: the config file or `.env` in full, daemon logs,
hook payloads, transcript excerpts, absolute paths carrying a username or
client name, git remote URLs, or any credential.

## Step 1 — establish there is a defect

Do all three. Each one eliminates a class of report that would otherwise cost
the user a round trip and the maintainers a triage slot.

1. **Rule out configuration.** Run `bin/hooks-daemon explain-handler <name>`
   for the handler involved and read what it honours. Most reported bugs are a
   handler doing exactly what its configuration asks. You will have to name the
   options you ruled out and say why each is insufficient — a report without
   that is refused by the generator, so do it now rather than twice.

2. **Read the daemon source.** It is on disk at
   `src/claude_code_hooks_daemon/` (client install:
   `.claude/hooks-daemon/src/...`). Find the code that produces the behaviour
   and note it as `file.py:123`. If reading it explains the behaviour as
   intended, stop — that is a documentation gap or a configuration question,
   not a defect, and it is a different conversation.

3. **Check version currency.** `bin/hooks-daemon release-notes --latest`, and
   `--from <installed> --to <latest>` for everything in between. Reporting from
   an older version is legitimate **provided nothing in those notes touched the
   subsystem involved**. If something did, upgrade first — the fix may already
   be out.

## Step 2 — write the fields file

A JSON object under `untracked/scratch/`. Every key is checked:

| Key                 | Required | Note                                                       |
| ------------------- | -------- | ---------------------------------------------------------- |
| `summary`           | yes      | The behaviour, not a theory about the cause                |
| `expected`          | yes      | And what states it — a doc, a deny message, `explain-rule` |
| `observed`          | yes      | What actually happened                                     |
| `reproduction`      | yes      | Steps from a CLEAN checkout, paths invented under scratch  |
| `config_considered` | yes      | List of `{option, why_insufficient}` — both halves needed  |
| `source_citation`   | yes      | `src/claude_code_hooks_daemon/...py:123`, must resolve     |
| `handler`           | no       | Config key. Omit it when the defect is not in a handler    |

The reproduction is the field most often written wrong. It is refused outright
— not scrubbed — when it names a path from the user's own tree, because that is
a report written the wrong way rather than one needing cleanup. Build a
synthetic reproduction under `untracked/scratch/`. If the behaviour genuinely
cannot be reproduced, say `CANNOT REPRODUCE` and describe what was seen; that
is a weaker report, not an invalid one.

## Step 3 — generate

```bash
bin/hooks-daemon issue-report --fields untracked/scratch/fields.json --latest <version>
```

It collects the declared fields plus version, platform and install mode, and it
**never collects** the hostname, git remote, `.env`, config dump or logs —
those are not scrubbed out later, they are never gathered.

It refuses before writing anything, and returns every reason at once. On
refusal no file exists, deliberately: a file that exists is a file that can be
filed by mistake. Fix everything it names and re-run; do not work around a
refusal.

## Step 4 — read it, then file it

**Read the generated document before filing.** The generator proves the body is
the one it built. It cannot prove the prose the user wrote inside it is safe to
publish, and that judgement does not belong to a tool.

```bash
gh issue create --repo Edmonds-Commerce-Limited/claude-code-hooks-daemon \
  --title "<summary>" --body-file untracked/issue-reports/<report>.md
```

**Do not edit the generated file.** It carries a digest of its own body, so
`issue_filing_gate` refuses an edited one (`R-UPSTREAM-ISSUE-UNVERIFIED-BODY`).
That is the failure this catches: a clean report edited to paste in "just the
relevant bit of the log", then filed. Extra detail goes in `reproduction`,
where the checks still run over it.

**If the generator genuinely cannot run** — a defect that stops the CLI, a
machine without the install — `--web` is allowed and is the fallback:

```bash
gh issue create --repo Edmonds-Commerce-Limited/claude-code-hooks-daemon --web
```

It files nothing; it opens GitHub's issue chooser. Pick **Daemon defect**: it
asks for the generator's fields and cannot be submitted without ticking two
acknowledgements. (**Something else** states the same rule but asks for no
structure and has no acknowledgements.) Do not reach for `--web` to skip steps
1–3 — the checks are then yours to do by hand, and they are the part that
decides whether there is a defect at all.

## Related

- `/hooks-daemon bug-report` — a LOCAL diagnostic for reading yourself. It is
  not a filing artefact and its output must never be pasted into an issue.
- `/hooks-daemon report` — an investigation report with a timeline, also local.
- `docs/guides/TROUBLESHOOTING.md` at the daemon's root — read this first if
  the daemon will not start or hooks are not firing.

## Version

Introduced in: Plan 00403
