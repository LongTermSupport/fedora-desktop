"""The shared protocol corpus, walked against the Python (Plan 00209, SPEC-proto.md).

`tests/fixtures/agent-bus-protocol/` holds one file per suite and the contexts files
(`contexts*.json`, each `{"v": 1, "contexts": {name: context}}`, a name used once). The Go
package `src-go/internal/bus/proto` walks the same files (`corpus_test.go`); neither side is
the other's oracle, every case is written from the SPEC or a found bug.

A suite file is `{"v": 1, "suite": <name>, "cases": [...]}`. A case is `id`, `spec` (the P
number), `fn` (an adapter name), `args`, an optional `ctx` (a context's name), and
`expect`, the verdict as JSON: a value, `{"refused": <reason>}` (a Refusal),
`{"untrusted": <reason text>}` (Untrusted) or `{"error": true}` (any other ValueError, a
caller error). Where the Python answers differently a case adds one of:

- `expect_python`: `{"departure": "D<n>", "raises": <exception class>}`. The Python is known
  to crash where `expect` is right; the case runs as an expected failure naming the departure,
  and turns into an unexpected success (a failed run) once the Python is fixed.
- `expect_python`: `{"departure": "D<n>", "value": <verdict>}`. The Python's own answer.
- `expect_python_by_unicode`: `{"14": <verdict>, "15": <verdict>}`, keyed on the major
  version of `unicodedata.unidata_version` (15 for 15 or later): a case that depends on the
  Unicode tables (SPEC P84, D4, D5).

Adapters live in the `corpus_*` modules beside this file, one per group of suites.
"""

from __future__ import annotations

import json
import os
import pathlib
import re
import sys
import unicodedata
import unittest
from collections.abc import Callable

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "..", ".."))

from helpers.pingbus import protocol as p
from tests.helpers.pingbus import (
    corpus_envelope,
    corpus_grammar,
    corpus_limits,
    corpus_ping,
    corpus_rooms,
)

CORPUS = pathlib.Path(__file__).resolve().parents[3] / "tests" / "fixtures" / "agent-bus-protocol"
UNICODE_MAJOR = "14" if int(unicodedata.unidata_version.split(".")[0]) < 15 else "15"

Adapter = Callable[[dict, "p.Context | None"], object]
ADAPTERS: dict[str, Adapter] = {}
for _module in (corpus_grammar, corpus_ping, corpus_rooms, corpus_limits, corpus_envelope):
    for _name, _adapter in _module.ADAPTERS.items():
        if _name in ADAPTERS:
            raise RuntimeError(f"adapter {_name!r} is defined twice")
        ADAPTERS[_name] = _adapter


def load_contexts() -> dict[str, p.Context]:
    contexts: dict[str, p.Context] = {}
    for path in sorted(CORPUS.glob("contexts*.json")):
        for name, c in json.loads(path.read_text(encoding="utf-8"))["contexts"].items():
            if name in contexts:
                raise RuntimeError(f"{path.name}: context {name!r} is defined twice")
            contexts[name] = p.Context(
                c["server_name"],
                frozenset(c["humans"]),
                dict(c["roles"]),
                {repo: tuple(branches) for repo, branches in c["repos"].items()},
                tuple(c["path_prefixes"]),
                c["org"],
                tuple(c["exclude"]),
            )
    return contexts


def verdict(adapter: Adapter, args: dict, ctx: p.Context | None) -> object:
    """The adapter's result, or the refusal it raised, as the corpus spells verdicts."""
    try:
        return adapter(args, ctx)
    except p.Refusal as exc:
        return {"refused": exc.reason}
    except p.Untrusted as exc:
        return {"untrusted": str(exc)}
    except UnicodeError:
        raise  # a ValueError, but a crash (D1), never a refusal
    except ValueError:
        return {"error": True}


def canonical(value: object) -> str:
    return json.dumps(value, sort_keys=True, ensure_ascii=True)


def _make_test(case: dict, contexts: dict[str, p.Context]) -> Callable[[unittest.TestCase], None]:
    adapter = ADAPTERS[case["fn"]]
    ctx = contexts[case["ctx"]] if "ctx" in case else None
    expect = case["expect"]
    by_unicode = case.get("expect_python_by_unicode")
    if by_unicode is not None:
        expect = by_unicode[UNICODE_MAJOR]
    known = case.get("expect_python")
    if known is not None and "value" in known:
        expect = known["value"]

    def test(self: unittest.TestCase) -> None:
        if known is not None and "raises" in known:
            try:
                got = verdict(adapter, case["args"], ctx)
            except Exception as exc:
                if type(exc).__name__ != known["raises"]:
                    raise
                self.fail(f"{known['departure']}: the Python raises {known['raises']}; "
                          f"the right verdict is {canonical(expect)}")
        else:
            got = verdict(adapter, case["args"], ctx)
        self.assertEqual(canonical(got), canonical(expect), f"{case['spec']} {case['id']}")

    if known is not None and "raises" in known:
        test = unittest.expectedFailure(test)
    return test


def _method_name(case: dict) -> str:
    suffix = f"_{case['expect_python']['departure']}" if "expect_python" in case else ""
    return "test_" + re.sub(r"[^0-9A-Za-z]", "_", case["id"]) + suffix


def build_suites() -> dict[str, type[unittest.TestCase]]:
    contexts = load_contexts()
    classes: dict[str, type[unittest.TestCase]] = {}
    for path in sorted(CORPUS.glob("*.json")):
        if path.name.startswith("contexts"):
            continue
        data = json.loads(path.read_text(encoding="utf-8"))
        if data.get("v") != 1 or data.get("suite") != path.stem:
            raise RuntimeError(f"{path.name}: not a v1 suite named {path.stem!r}")
        methods: dict[str, object] = {}
        for case in data["cases"]:
            name = _method_name(case)
            if name in methods:
                raise RuntimeError(f"{path.name}: case id {case['id']!r} is given twice")
            methods[name] = _make_test(case, contexts)
        class_name = "TestCorpus_" + re.sub(r"[^0-9A-Za-z]", "_", path.stem)
        classes[class_name] = type(class_name, (unittest.TestCase,), methods)
    return classes


globals().update(build_suites())


class TestCorpusShape(unittest.TestCase):
    def test_every_suite_has_cases(self) -> None:
        suites = [name for name in globals() if name.startswith("TestCorpus_")]
        self.assertTrue(suites)
        for name in suites:
            self.assertTrue([m for m in dir(globals()[name]) if m.startswith("test_")], name)

    def test_every_case_cites_a_spec_expectation(self) -> None:
        for path in CORPUS.glob("*.json"):
            if path.name.startswith("contexts"):
                continue
            for case in json.loads(path.read_text(encoding="utf-8"))["cases"]:
                self.assertRegex(case["spec"], r"^([PD][1-9][0-9]?)( [PD][1-9][0-9]?)*$",
                                 case["id"])


if __name__ == "__main__":
    unittest.main()
