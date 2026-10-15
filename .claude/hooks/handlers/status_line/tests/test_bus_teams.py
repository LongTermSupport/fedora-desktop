"""BusTeamsHandler - the status line names the agent-bus teams this session is on (Plan 00201 S-C6)."""

import sys
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).parent.parent))

from bus_teams import BusTeamsHandler


@pytest.fixture
def handler():
    return BusTeamsHandler()


def segment(handler):
    return handler.handle({}).context


def test_name_and_priority(handler):
    assert handler.name == "bus-teams"
    assert handler.priority == 25


def test_it_always_matches(handler):
    assert handler.matches({}) is True


def test_one_team_is_shown(monkeypatch, handler):
    monkeypatch.setenv("PINGBUS_TEAMS", "alpha")
    assert [text for text in segment(handler) if "bus: alpha" in text and "," not in text]


def test_several_teams_are_shown_in_order(monkeypatch, handler):
    monkeypatch.setenv("PINGBUS_TEAMS", "alpha,beta")
    (text,) = segment(handler)
    assert text.startswith("| ")
    assert "bus: alpha,beta" in text


def test_nothing_is_shown_when_unset(monkeypatch, handler):
    monkeypatch.delenv("PINGBUS_TEAMS", raising=False)
    assert segment(handler) == []


def test_nothing_is_shown_when_empty(monkeypatch, handler):
    monkeypatch.setenv("PINGBUS_TEAMS", "")
    assert segment(handler) == []


@pytest.mark.parametrize("bad", ["alpha,", "Al pha", "a\x1b[31m,b", "alpha,alpha", "x" * 25, ",", "alpha,,beta"])
def test_a_value_that_is_not_a_team_list_is_never_shown(monkeypatch, handler, bad):
    """The variable is the session's own, but it reaches a terminal: only team names get through."""
    monkeypatch.setenv("PINGBUS_TEAMS", bad)
    assert segment(handler) == []


def test_it_explains_itself(monkeypatch, handler):
    monkeypatch.setenv("PINGBUS_TEAMS", "alpha,beta")
    explained = handler.explain_segment()
    assert "bus:" in explained.glyphs
    assert "alpha,beta" in explained.current_value
