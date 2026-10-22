# Defence Before Fix report: stream-stand-in-not-a-text-stream

## Specification followed

Method specification 1.1.0 (cached copy), detector specification 1.0.0 and toolchain
specification 0.2.0 (cached), as printed by the dbf skill's refresh script.

## Toolchain

There is no manifest declaration. No `defenceBeforeFix` or `extra.defence-before-fix` key
exists; `.claude/settings.json` only enables the plugin. No toolchain in the register is
declared. The project's own DBF practice is used instead: semgrep rules in `.semgrep/`, each
proven against an annotated fixture on every run by a Go gate. Each rule's documentation is
in `CLAUDE/QA.md` under its identifier.

- **Gate:** `scripts/qa-speech-to-text-rules.bash` (`src-go/qa/sttrules`). It is the Python
  and JavaScript semgrep gate that already holds non-speech rules, the two test-timing rules.
- **Single-rule harness:** `semgrep --test --config <rule.yml> <fixture>`.
- **Listing:** the `id:` lines of `.semgrep/*.yml`, plus `scripts/qa-gates.tsv`.
- **Identifier resolution:** the `CLAUDE/QA.md` heading of the same name.
- **Calibration assumed:** blocking, no baseline, no exceptions.

## Defect and class

**Originating instance.** `helpers/host_health/login_report.py:378` defined `class _Sink`
with `write()` only. At `:469` it was passed as `stdout`, `stderr` and `unchecked` to
`check_freshness.run`, whose parameters are typed `TextIO`. Since 8d423099e,
`check_freshness.py:150` and `:238` call `writelines`, so the collection crashed with
`'_Sink' object has no attribute 'writelines'` whenever a play was stale or no answer could
be given.

**Class.** A hand-rolled text-stream stand-in passed where a real text stream is expected.

**Hazard.** Python accepts the object, and no type checker runs here, so every test that only
writes passes. The first consumer call outside `write()` (`writelines`, `flush`,
`print(file=)`) then raises.

## Rule

**Detector.** semgrep, in `.semgrep/speech-to-text.yml`.

**Identifier.** `stream-stand-in-not-a-text-stream`.

**Message.** "an object with only write() is handed over as a text stream; the first
writelines(), flush() or print(file=) on it raises. Use io.StringIO, or derive from
io.TextIOBase."

**Documentation.** `CLAUDE/QA.md#stream-stand-in-not-a-text-stream`.

**Width.** The rule matches a class defined in the same file that has `write(self, text, ...)`
and no `io.*`, `TextIOBase`, `StringIO`, `IOBase`, `TextIOWrapper` or `Protocol` base. It
reports when an instance of that class is handed over as a stream:

- as a stream-named keyword argument, directly or through a variable (tuple assignments
  included);
- assigned to `sys.stdout` or `sys.stderr`;
- given to `redirect_stdout` or `redirect_stderr`;
- assigned over a parameter whose annotation names `TextIO` (`TextIO | None` included).

After the adversarial review (round 1) the stream-name list was widened to the names this
repo uses (`log`, `diagnostics`, `errors`, `*_stream`, `*_sink`, `out_*`, `err_*`) and the
parameter branch to optional annotations. The widened rule finds the same 3 sites on the
pre-fix code and nothing else in the tree.

**Excluded, as not carrying the hazard.** Classes that are real streams, and Protocols, which
are interfaces rather than stand-ins.

**Next wider rule considered.** Every class with a one-argument `write` and no io base. It was
not built because it matches `DecisionLog` in `.claude/ccy/claude-supervise.py` and `Report`
in the 00161 triage probe. Both are append-to-file loggers that are never handed over as
streams, so they do not carry the hazard.

**What the rule does not see.** A stand-in defined in one module and handed over in another,
or one passed positionally. Semgrep OSS has no cross-file type flow. Reading found no such
case.

## Proof

**Fixture.** `.semgrep/speech-to-text.py` has 11 `ruleid` and 3 `ok` lines, one or more for every branch of the rule (keyword, variable, 2-, 3- and 4-element tuples, `sys.stdout`/`sys.stderr`, `redirect_stdout`/`redirect_stderr`, a `TextIO` parameter); the branches beyond the first six were added after the conformance review.
`semgrep --test` reported "All tests passed".

**Red commit.** 804178386 contains the rule alone, before any fix.
`./scripts/qa-speech-to-text-rules.bash` reported the following and exited 1:

```
helpers/host_health/login_report.py:470, helpers/pingbus/cli.py:1638, :2168
```

## Independent search

The `defence-before-fix:independent-searcher` was dispatched before the rule existed and kept
out of `.semgrep/`. Its report is saved under `untracked/agent-reports/auto/`.

**Text search** found:

- every `def write(self` (14 hits outside `.semgrep/`; the searcher's report names the ones in `helpers/` and `tests/`, and the four in `.claude/ccy/claude-supervise.py`, the 00161 triage probe and its test, and the 00201 `test_m7_check.py` were read by the remediator: none is handed over as a stream);
- `write=` lambdas;
- the stream methods consumers call: `writelines` in `agent_bus/declare.py` and
  `check_freshness`, `flush` and `isatty` in `agent_bus/cli.py` and `agent_bus_lxc/members.py`.

**Reading** decided which hits are streams. It found that `_LockedStream` is assigned over
`err: TextIO` and fanned out to helpers, which no grep of call arguments would show. It also
found that the test fakes `_DeadTerminal` and `Full` already derive from `io.TextIOBase`.

**Result.** Both techniques found the same two classes, `_Sink` and `_LockedStream`, and the
rule matched both. Nothing was found beyond them, so the rule was not widened.

## Sweep and count

**Scope.** Every Python file git would publish, as the gate's default: `helpers/`, `tests/`,
`scripts/`, `.claude/`, plan folders and shebang executables. It is recorded here as the
project's decision. Vendored and daemon-owned trees are excluded by the gate's trees table.

**Count.** 3 findings, 2 instances (classes). No narrowing.

## Fixes

Each instance was examined individually.

1. **`login_report._Sink`** was replaced with `io.StringIO`. The lines are read by
   `_lines(sink)`, which takes the non-blank lines of `getvalue()`.

2. **`pingbus.cli._LockedStream`** now derives from `io.TextIOBase`:

   - `write` returns the length written;
   - `writelines` writes under one lock;
   - `flush` and `isatty` delegate to the wrapped stream;
   - `close` closes the wrapper only, so garbage collection never flushes or closes the
     wrapped stream.

   It is latent: nothing calls those methods today. The change bumps CCY to 3.100.9 and the
   container to 2.79.4, because the image's pingbus zipapp is built from `helpers/pingbus/`.

No instance was suppressed and no hazard was left in place.

## Decisions referred to the owner

Nothing was baselined, suppressed or left unfixed: 2 instances fixed, 0 remaining.

One coverage gap is referred, not decided: the rule does not see a stand-in passed
positionally, or defined in one module and handed over in another. Cross-module flow is a
semgrep OSS limit. A positional form within one file could be written (any call taking an
instance as an argument), but it would also match `DecisionLog` and `Report`, which carry no
hazard, so it was not built. Both forms were covered by reading in this sweep (none found).
Closing the gap for good would take a type checker (mypy or pyright with `TextIO`
annotations), which helpers/CLAUDE.md's stdlib-only rule does not forbid for QA but which is
a toolchain decision for the owner.

## Permanence

The rule is part of `qa-speech-to-text-rules.bash`, which is in `qa-all` and `qa-changed`
(`stt-rules-files`, and `stt-rules` as a whole). It is blocking. After the fix, the green run
through that entry point reported `passed: 396 failed: 0` and exited 0.

## Toolchain and detector gaps

- The project declares no DBF toolchain or manifest entry, and there is no listing command
  beyond reading the rule files.
- semgrep OSS cannot follow types across modules, which is why the rule has the cross-module
  blind spot named above.
- The gate's name (`speech-to-text-rules`) no longer describes its contents. A rename belongs
  to the gate's owner.

## The original defect

**Fix.** `_Sink` was replaced with `io.StringIO`.

**Test.** `tests/helpers/host_health/test_login_report.py::TestTheRealFreshnessCheckThroughTheSeam`
drives the real `check_freshness.run` (with only the fetch and the judge replaced) through
`login_report.freshness_findings`. It covers:

- a stale play, which must give one finding that carries its changed path;
- a `NoAnswer`, which must give an unchecked finding that carries its reason.

Both were red with `AttributeError: '_Sink' object has no attribute 'writelines'` before the
fix and pass after it.

**Wrapper test.** `tests/helpers/pingbus/test_cli.py::LockedStreamTest` holds the wrapper to
the stream interface.
