#!/usr/bin/env python3
"""Plan 00234 — check-fit.py: one acceptance check of the INSTALLED model manager and
resolver, run as a leg of acceptance.bash. Read-only: nothing is downloaded or removed.

    deployed    ~/.local/bin/wsi-resolve-model and wsi-model-manager are the checkout's
    fit         wsi-resolve-model --fit answers; on a machine with NVIDIA device nodes it
                names the GPU and its memory (device cuda), never a CPU fallback
    preselect   the installed manager, given that answer, preselects an English-only model
                that fits, and refuses every model that does not with a reason
    screen      the installed manager's real Textual screen, driven headless (App.run_test):
                the cursor starts on that model, the too-big row is marked, and choosing a
                too-big model (Enter, and the Download key) shows the refusal and leaves
                the list open; the download itself is replaced, so nothing is fetched

The report goes to stdout, for the run log; failures say why on stderr, exit 1.
Usage: check-fit.py deployed|fit|preselect|screen
"""

import asyncio
import glob
import importlib.machinery
import importlib.util
import json
import os
import pathlib
import subprocess
import sys

BIN = pathlib.Path.home() / ".local" / "bin"
# PLAN_REPO_ROOT is exported by acceptance.bash's plan_init
CHECKOUT_BIN = pathlib.Path(os.environ.get("PLAN_REPO_ROOT", "/nonexistent")) / "files" / "home" / ".local" / "bin"
TOOLS = ("wsi-resolve-model", "wsi-model-manager")


def fail(reason):
    print(f"FAIL: {reason}", file=sys.stderr)
    return 1


def fit_report():
    result = subprocess.run([str(BIN / "wsi-resolve-model"), "--fit"], capture_output=True,
                            text=True, timeout=60, check=False)
    if result.returncode != 0:
        raise RuntimeError(f"wsi-resolve-model --fit exited {result.returncode}: "
                           f"{result.stderr.strip()}")
    return json.loads(result.stdout)


def check_deployed():
    for tool in TOOLS:
        if (BIN / tool).read_bytes() != (CHECKOUT_BIN / tool).read_bytes():
            return fail(f"{BIN / tool} differs from the checkout's; run play-speech-to-text.yml")
        print(f"{tool}: installed copy is the checkout's")
    return 0


def check_fit():
    report = fit_report()
    print(json.dumps(report, indent=2))
    if glob.glob("/dev/nvidia[0-9]*"):
        if report["device"] != "cuda" or not report["gpu_memory_mb"]:
            return fail("this machine has NVIDIA device nodes, but --fit did not judge a GPU")
    elif report["device"] != "cpu":
        return fail("no NVIDIA device nodes, but --fit judged a GPU")
    return 0


def load_manager():
    path = str(BIN / "wsi-model-manager")
    loader = importlib.machinery.SourceFileLoader("wsi_model_manager_installed", path)
    manager = importlib.util.module_from_spec(importlib.util.spec_from_loader(loader.name, loader))
    loader.exec_module(manager)
    return manager


def check_preselect():
    manager = load_manager()
    report = fit_report()
    print(manager.machine_line(report, ""))
    best = manager.best_english_fit(report)
    if best is None:
        return fail("no English-only model fits this machine, so none is preselected")
    print(f"preselected: {best} ({manager.fit_cell(best, report)})")
    for model_id, *_ in manager.MODELS:
        refusal = manager.download_refusal(model_id, report, "")
        fits = report["models"][model_id]["fits"]
        if fits == bool(refusal):
            return fail(f"{model_id}: fits={fits} but refusal={refusal!r}")
        if refusal:
            print(f"refused: {refusal}")
    return 0


def check_screen():
    manager = load_manager()
    report = fit_report()
    best = manager.best_english_fit(report)
    installed = manager.get_installed()
    too_big = [model_id for model_id, *_ in manager.MODELS
               if not report["models"][model_id]["fits"] and model_id not in installed]
    if best is None or not too_big:
        return fail("this machine has no English-only model that fits, or no model too big "
                    "that is not downloaded, so the preselection and refusal cannot be shown")
    problems = asyncio.run(drive_screen(manager, report, best, too_big[0]))
    for problem in problems:
        print(f"FAIL: {problem}", file=sys.stderr)
    return 1 if problems else 0


async def drive_screen(manager, report, best, too_big):
    """The installed manager's real Textual screen, run headless: the cursor starts on
    `best`, and choosing `too_big` (Enter, then the Download key) is refused with the
    reason while the list stays open. A download that starts is recorded, not run."""

    class Watched(manager.ModelManagerApp):
        def __init__(self):
            super().__init__()
            self.notes = []
            self.downloads = []

        def notify(self, message, **kwargs):
            self.notes.append(str(message))
            super().notify(message, **kwargs)

        def _do_download(self, model_id, repo):
            self.downloads.append(model_id)

        def _poll_progress(self, model_id):
            self.downloads.append(model_id)

    problems = []
    app = Watched()
    async with app.run_test(size=(160, 40)) as pilot:
        await pilot.pause()
        table = app.query_one("#table", manager.DataTable)
        start = app._selected_model_id()
        print(f"cursor starts on: {start}")
        if start != best:
            problems.append(f"the cursor starts on {start}, not {best}")
        cell = str(table.get_row(too_big)[3])
        print(f"{too_big} GPU memory cell: {cell}")
        if not cell.startswith("✗"):
            problems.append(f"{too_big} is too big but its GPU memory cell says {cell!r}")
        refusal = manager.download_refusal(too_big, report, "")
        table.move_cursor(row=app._visible_model_ids.index(too_big))
        for key in ("enter", "d"):
            app.notes.clear()
            await pilot.press(key)
            await pilot.pause()
            print(f"{key} on {too_big}: {app.notes}")
            if refusal not in app.notes:
                problems.append(f"{key} on {too_big} did not show the refusal {refusal!r}")
        if app.downloads or app._downloading:
            problems.append(f"a download started: {app.downloads or sorted(app._downloading)}")
        if len(app.screen_stack) != 1 or app._selected_model_id() != too_big:
            problems.append("after the refusal the model list is not left open on that row")
    return problems


CHECKS = {"deployed": check_deployed, "fit": check_fit, "preselect": check_preselect,
          "screen": check_screen}

if __name__ == "__main__":
    if len(sys.argv) != 2 or sys.argv[1] not in CHECKS:
        print(f"usage: check-fit.py {'|'.join(CHECKS)}", file=sys.stderr)
        sys.exit(64)
    try:
        sys.exit(CHECKS[sys.argv[1]]())
    except (RuntimeError, OSError, ValueError, KeyError) as e:
        sys.exit(fail(str(e)))
