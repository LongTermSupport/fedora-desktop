"""Pytest configuration: load clip-scan as an importable module.

The deployable script lives at files/home/.local/bin/clip-scan with no .py
extension (matches the raw-prune neighbour convention). We load it via
importlib so its functions are importable in tests without packaging.
"""

import importlib.util
import pathlib
import sys
from importlib.machinery import SourceFileLoader

_REPO_ROOT = pathlib.Path(__file__).resolve().parents[2]
_SCRIPT_PATH = _REPO_ROOT / "files" / "home" / ".local" / "bin" / "clip-scan"

if not _SCRIPT_PATH.exists():
    raise FileNotFoundError(
        f"clip-scan source not found at {_SCRIPT_PATH}; "
        "tests cannot run until the script exists."
    )

# clip-scan has no .py extension (matches raw-prune sibling convention).
# spec_from_file_location's default loader-by-extension lookup fails;
# use SourceFileLoader explicitly.
_loader = SourceFileLoader("clip_scan", str(_SCRIPT_PATH))
_spec = importlib.util.spec_from_loader("clip_scan", _loader)
if _spec is None or _spec.loader is None:
    raise ImportError(f"Could not build module spec for {_SCRIPT_PATH}")

clip_scan = importlib.util.module_from_spec(_spec)
sys.modules["clip_scan"] = clip_scan
_spec.loader.exec_module(clip_scan)
