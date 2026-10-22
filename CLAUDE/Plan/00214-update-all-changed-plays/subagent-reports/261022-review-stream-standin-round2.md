# Review: stream-stand-in-not-a-text-stream, round 2 (22e599b3d..8f283480f)

Verdict: **FIX-FIRST**

## Round 1 findings

1. **meta-deploy.list exactness: resolved.** The 00213 entry now says that
   play-agent-bus.yml installs the rebuilt pingbus on the host, in the kit and in every
   running LXC member. The changelog says the same.
2. **Rule gaps: NOT resolved.** The new regex alternatives do not do what the commit, the
   rule comment, QA.md and the DBF report say they do. See finding A.
3. **Fixture branches: resolved.** The fixture now has `ruleid:` lines for `sys.stdout`,
   `redirect_stdout`/`redirect_stderr`, the 3- and 4-element tuples, the optional parameter
   and `out_`/`err_` names. However, the `_stream`/`_sink` alternatives and the wider
   annotation regex have no fixture line that only they can satisfy, which is why finding A
   went unseen. The fixture's `out_stream` is matched by `^out_`, not by `_stream`.
4. **QA.md comma and blank line: resolved.**
5. **print(file=) interleaving: resolved.** QA.md now has a sentence on it.

Checks run: `./scripts/qa-speech-to-text-rules.bash` passes (rc 0, 396/0). The
`test_login_report` and pingbus `test_cli` tests pass (153).

## Findings

**A. The widened rule's regexes are partly dead, because semgrep anchors `metavariable-regex`
at the start of the value (re.match semantics).** I ran semgrep 1.177.0 on probe files in
untracked/scratch/probe/:

- **Keyword names.** In `(?i)(^(...)$|_stream|_sink|^out_|^err_)`, the bare `_stream` and
  `_sink` alternatives only match a name that *starts* with `_stream`/`_sink`. Both
  `run(log_stream=_S())` and `run(event_sink=_S())` stay silent. QA.md says "ending
  `_stream` or `_sink`", and the commit message says "\*\_stream, \*\_sink". Both are false.
  There are 20 first-party `*_stream=` / `*_sink=` sites, so the gap is real.
- **Annotations.** `'\b(TextIO|TextIOBase|IO\[str\])\b'` matches only an annotation that
  begins with `TextIO`:
  - `typing.TextIO`, `Optional[TextIO]` and `"TextIO"` all stay silent.
  - `IO[str]` can never match, because `\]\b` needs a word character after the `]`.
  - `TextIO | None` works only because it starts with `TextIO`. "Whose annotation names
    TextIO" overstates what the rule checks.

Fix:

- Keyword regex: `.*(_stream|_sink)$`.
- Annotation regex: `.*\b(TextIO|TextIOBase)\b|.*\bIO\[str\]`.
- Add a fixture `ruleid:` line for each new branch (`log_stream=`, `event_sink=`,
  `Optional[TextIO]`, `typing.TextIO`).
- Re-run the sweep, because the corrected rule may find new sites. Then correct the hit
  count in the DBF report and the rule's wording in QA.md.

This is not covered by the DBF referral, which names only the positional and cross-module
gaps.

**B. Cosmetic.** Two lines in this diff are not rewrapped to the file width:
docs/ccy-changelog.md ("The same pingbus reaches...") and CLAUDE/QA.md ("Where several
threads share..."). Fix them in the same pass.

## No new issues

- The login_report `io.StringIO` change and its test against the real check_freshness.
- `_LockedStream` as `io.TextIOBase`.
- The CCY 3.100.9 / container 2.79.4 bump: Dockerfile, script and changelog agree.
- The meta-deploy.list entry ordering and header rules.
