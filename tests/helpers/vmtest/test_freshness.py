"""Unit tests for helpers/vmtest/freshness.py — the DESIGN.md §4.4 policy.

Run from the repo root with no third-party deps:

    python3 -m unittest tests.helpers.vmtest.test_freshness

The test population is the WHOLE readable/unreadable matrix, not a set of cases
someone thought of. An earlier draft of this plan listed three hand-picked
assertions, and the most important of them — "unknown can never collapse into
current" — would have passed against a policy that did exactly that, because
the fail-open lived in the partial-outage cell it never exercised. So every
cell of

    artefact_identity ∈ {matches, differs, unreadable}
  × package_revision ∈ {unchanged, advanced, unreadable}
  × TTL-U           ∈ {expired, unexpired}
  × TTL-R           ∈ {expired, unexpired}

has a named verdict below, the table is asserted to cover the product exactly,
and the function is asserted total over it. The named guards that follow are
each the one that goes red when its line of the policy is removed.
"""

from __future__ import annotations

import itertools
import pathlib
import sys
import unittest

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

from helpers.vmtest import freshness

IDENTITY = "a" * 64
OTHER_IDENTITY = "b" * 64
RECIPE = "c" * 64
BASE_SHA = "d" * 64

REVISION = 1789172543
NEWER_REVISION = REVISION + 86400
OLDER_REVISION = REVISION - 86400

DAY = 86400
TTL_U = 7 * DAY
TTL_R = 90 * DAY
NOW = 1_800_000_000

UNREADABLE = freshness.Unreadable(
    url="https://dl.fedoraproject.org/pub/fedora/linux/releases/44/COMPOSE_ID",
    error="ConnectionResetError: [Errno 104] Connection reset by peer",
)

# The whole matrix, one row per cell. `degraded` is part of the named verdict,
# not an afterthought: §4.4 says the TTL-backstopped cells carry it and nothing
# else does.
MATRIX = """\
identity    revision    ttl_u      ttl_r      verdict    degraded
unreadable  unchanged   unexpired  unexpired  unknown    no
unreadable  unchanged   unexpired  expired    unknown    no
unreadable  unchanged   expired    unexpired  unknown    no
unreadable  unchanged   expired    expired    unknown    no
unreadable  advanced    unexpired  unexpired  unknown    no
unreadable  advanced    unexpired  expired    unknown    no
unreadable  advanced    expired    unexpired  unknown    no
unreadable  advanced    expired    expired    unknown    no
unreadable  unreadable  unexpired  unexpired  unknown    no
unreadable  unreadable  unexpired  expired    unknown    no
unreadable  unreadable  expired    unexpired  unknown    no
unreadable  unreadable  expired    expired    unknown    no
differs     unchanged   unexpired  unexpired  reinstall  no
differs     unchanged   unexpired  expired    reinstall  no
differs     unchanged   expired    unexpired  reinstall  no
differs     unchanged   expired    expired    reinstall  no
differs     advanced    unexpired  unexpired  reinstall  no
differs     advanced    unexpired  expired    reinstall  no
differs     advanced    expired    unexpired  reinstall  no
differs     advanced    expired    expired    reinstall  no
differs     unreadable  unexpired  unexpired  reinstall  no
differs     unreadable  unexpired  expired    reinstall  no
differs     unreadable  expired    unexpired  reinstall  no
differs     unreadable  expired    expired    reinstall  no
matches     unchanged   unexpired  unexpired  current    no
matches     unchanged   unexpired  expired    reinstall  no
matches     unchanged   expired    unexpired  current    no
matches     unchanged   expired    expired    reinstall  no
matches     advanced    unexpired  unexpired  refresh    no
matches     advanced    unexpired  expired    reinstall  no
matches     advanced    expired    unexpired  refresh    no
matches     advanced    expired    expired    reinstall  no
matches     unreadable  unexpired  unexpired  current    yes
matches     unreadable  unexpired  expired    reinstall  no
matches     unreadable  expired    unexpired  refresh    yes
matches     unreadable  expired    expired    reinstall  no
"""

AXES = {
    "identity": ("matches", "differs", "unreadable"),
    "revision": ("unchanged", "advanced", "unreadable"),
    "ttl_u": ("expired", "unexpired"),
    "ttl_r": ("expired", "unexpired"),
}


def _parse_matrix():
    rows = [line.split() for line in MATRIX.splitlines()[1:] if line.strip()]
    table = {}
    for identity, revision, ttl_u, ttl_r, verdict, degraded in rows:
        key = (identity, revision, ttl_u, ttl_r)
        assert key not in table, f"duplicate matrix row {key}"
        table[key] = (verdict, degraded == "yes")
    return table


def _inputs(identity="matches", revision="unchanged", ttl_u="unexpired", ttl_r="unexpired", **over):
    """Inputs for one matrix cell, with per-test overrides on top."""
    live_identity = {
        "matches": IDENTITY,
        "differs": OTHER_IDENTITY,
        "unreadable": UNREADABLE,
    }[identity]
    probe_revision = {
        "unchanged": REVISION,
        "advanced": NEWER_REVISION,
        "unreadable": UNREADABLE,
    }[revision]
    last_upgraded_at = NOW - (TTL_U + 1 if ttl_u == "expired" else 1)
    installed_at = NOW - (TTL_R + 1 if ttl_r == "expired" else 1)
    fields = {
        "stored_identity": IDENTITY,
        "live_identity": live_identity,
        "stored_recipe_digest": RECIPE,
        "current_recipe_digest": RECIPE,
        "stored_base_sha256": BASE_SHA,
        "actual_base_sha256": BASE_SHA,
        "last_upgraded_revision": REVISION,
        "probe_revision": probe_revision,
        "installed_at": installed_at,
        "last_upgraded_at": last_upgraded_at,
        "now": NOW,
        "ttl_upgrade_seconds": TTL_U,
        "ttl_rebuild_seconds": TTL_R,
        "bodhi_state": "current",
    }
    fields.update(over)
    return freshness.FreshnessInputs(**fields)


class TestMatrixIsTheWholeInputSpace(unittest.TestCase):
    def test_table_covers_the_product_exactly(self):
        # Derived, not typed: the set of cells is the cartesian product of the
        # axes, and the table must equal it. Add an axis value without a row and
        # this goes red; add a row that names no real cell and it goes red.
        table = _parse_matrix()
        expected = set(itertools.product(*AXES.values()))
        self.assertEqual(set(table), expected)

    def test_policy_is_total_over_the_matrix(self):
        for cell in itertools.product(*AXES.values()):
            with self.subTest(cell=cell):
                verdict = freshness.decide(_inputs(*cell))
                self.assertIn(verdict.decision, freshness.DECISIONS)

    def test_every_cell_has_its_named_verdict(self):
        for cell, (decision, degraded) in _parse_matrix().items():
            with self.subTest(cell=cell):
                verdict = freshness.decide(_inputs(*cell))
                self.assertEqual(verdict.decision, decision)
                self.assertEqual(verdict.degraded, degraded)

    def test_every_verdict_names_a_reason(self):
        for cell in itertools.product(*AXES.values()):
            with self.subTest(cell=cell):
                self.assertTrue(freshness.decide(_inputs(*cell)).reason.strip())


class TestNamedGuards(unittest.TestCase):
    """Each of these goes red when its line of §4.4 is removed."""

    def test_unreadable_identity_is_unknown_on_every_other_axis(self):
        # B6: the partial outage. Identity unreadable but the revision readable
        # must NOT resolve through the TTL backstops into `current`.
        for revision, ttl_u, ttl_r in itertools.product(*list(AXES.values())[1:]):
            with self.subTest(revision=revision, ttl_u=ttl_u, ttl_r=ttl_r):
                verdict = freshness.decide(_inputs("unreadable", revision, ttl_u, ttl_r))
                self.assertEqual(verdict.decision, "unknown")

    def test_unknown_reason_names_the_url_and_the_error(self):
        # §4.5: `lab-status` prints the failing URL and the transport error.
        verdict = freshness.decide(_inputs("unreadable"))
        self.assertIn(UNREADABLE.url, verdict.reason)
        self.assertIn(UNREADABLE.error, verdict.reason)

    def test_advanced_revision_refreshes_inside_an_unexpired_ttl(self):
        # The round-1 inversion: the revision is the trigger, the TTL is a
        # backstop. A test that only exercised the expired case would pass
        # against the wrong policy.
        verdict = freshness.decide(_inputs("matches", "advanced", "unexpired", "unexpired"))
        self.assertEqual(verdict.decision, "refresh")
        self.assertFalse(verdict.degraded)

    def test_unchanged_revision_is_current_even_when_ttl_u_expired(self):
        # A refresh when nothing upstream moved is a pointless dnf cycle. The
        # readable, unchanged revision proves the refresh unnecessary.
        verdict = freshness.decide(_inputs("matches", "unchanged", "expired", "unexpired"))
        self.assertEqual(verdict.decision, "current")
        self.assertFalse(verdict.degraded)

    def test_unreadable_revision_inside_ttl_u_is_current_but_degraded(self):
        verdict = freshness.decide(_inputs("matches", "unreadable", "unexpired", "unexpired"))
        self.assertEqual(verdict.decision, "current")
        self.assertTrue(verdict.degraded)
        self.assertIn("package-revision-unreadable", verdict.divergences)

    def test_unreadable_revision_past_ttl_u_is_refresh_and_degraded(self):
        verdict = freshness.decide(_inputs("matches", "unreadable", "expired", "unexpired"))
        self.assertEqual(verdict.decision, "refresh")
        self.assertTrue(verdict.degraded)
        self.assertIn("package-revision-unreadable", verdict.divergences)

    def test_degraded_is_never_set_on_a_readable_revision(self):
        for identity, revision, ttl_u, ttl_r in itertools.product(*AXES.values()):
            if revision == "unreadable":
                continue
            with self.subTest(identity=identity, revision=revision, ttl_u=ttl_u, ttl_r=ttl_r):
                verdict = freshness.decide(_inputs(identity, revision, ttl_u, ttl_r))
                self.assertFalse(verdict.degraded)

    def test_backwards_probe_revision_is_unknown_not_a_retry(self):
        # §4.4a: the probe reads the canonical host, not a mirror. A revision
        # that went backwards there is not mirror lag; it blocks.
        verdict = freshness.decide(_inputs(probe_revision=OLDER_REVISION))
        self.assertEqual(verdict.decision, "unknown")
        self.assertIn(str(OLDER_REVISION), verdict.reason)
        self.assertIn(str(REVISION), verdict.reason)

    def test_bodhi_leaving_current_warns_and_diverges_but_does_not_rebuild(self):
        for state in ("archived", "pending", "frozen"):
            with self.subTest(state=state):
                verdict = freshness.decide(_inputs(bodhi_state=state))
                self.assertEqual(verdict.decision, "current")
                self.assertIn("release-not-current", verdict.divergences)
                self.assertTrue(any(state in warning for warning in verdict.warnings))

    def test_bodhi_current_adds_no_warning_and_no_divergence(self):
        verdict = freshness.decide(_inputs(bodhi_state="current"))
        self.assertEqual(verdict.warnings, ())
        self.assertEqual(verdict.divergences, ())

    def test_bodhi_unreadable_warns_and_diverges_but_does_not_block(self):
        # Bodhi is not an identity input (§4.4), so an outage there cannot be
        # `unknown`; but a green verdict must still say the release state was
        # not checked.
        verdict = freshness.decide(_inputs(bodhi_state=UNREADABLE))
        self.assertEqual(verdict.decision, "current")
        self.assertIn("release-state-unreadable", verdict.divergences)
        self.assertTrue(any(UNREADABLE.url in warning for warning in verdict.warnings))

    def test_bodhi_state_never_changes_the_decision(self):
        for cell in itertools.product(*AXES.values()):
            baseline = freshness.decide(_inputs(*cell, bodhi_state="current")).decision
            for state in ("archived", UNREADABLE):
                with self.subTest(cell=cell, state=state):
                    self.assertEqual(
                        freshness.decide(_inputs(*cell, bodhi_state=state)).decision, baseline
                    )


class TestLocalReinstallTriggers(unittest.TestCase):
    """§4.4: reinstall also fires on recipe or base-disk change, no network needed."""

    def test_changed_recipe_digest_reinstalls(self):
        verdict = freshness.decide(_inputs(current_recipe_digest="e" * 64))
        self.assertEqual(verdict.decision, "reinstall")
        self.assertIn("recipe", verdict.reason)

    def test_base_sha256_mismatch_reinstalls(self):
        verdict = freshness.decide(_inputs(actual_base_sha256="f" * 64))
        self.assertEqual(verdict.decision, "reinstall")
        self.assertIn("base.qcow2", verdict.reason)

    def test_expired_ttl_r_reinstalls_even_when_everything_else_is_current(self):
        verdict = freshness.decide(_inputs("matches", "unchanged", "unexpired", "expired"))
        self.assertEqual(verdict.decision, "reinstall")
        self.assertIn("TTL-R", verdict.reason)

    def test_unreadable_identity_still_blocks_when_the_recipe_changed(self):
        # A reinstall fetches media whose identity must be established first.
        # An unreadable identity cannot be, so the local trigger does not
        # promote `unknown` to `reinstall`.
        verdict = freshness.decide(_inputs("unreadable", current_recipe_digest="e" * 64))
        self.assertEqual(verdict.decision, "unknown")


class TestInputValidation(unittest.TestCase):
    def test_now_before_installed_at_is_an_error(self):
        with self.assertRaises(freshness.FreshnessInputError):
            freshness.decide(_inputs(installed_at=NOW + 1))

    def test_now_before_last_upgraded_at_is_an_error(self):
        with self.assertRaises(freshness.FreshnessInputError):
            freshness.decide(_inputs(last_upgraded_at=NOW + 1))

    def test_non_positive_ttl_is_an_error(self):
        for field in ("ttl_upgrade_seconds", "ttl_rebuild_seconds"):
            for value in (0, -1):
                with self.subTest(field=field, value=value):
                    with self.assertRaises(freshness.FreshnessInputError):
                        freshness.decide(_inputs(**{field: value}))

    def test_malformed_stored_identity_is_an_error(self):
        with self.assertRaises(freshness.FreshnessInputError):
            freshness.decide(_inputs(stored_identity="not-a-digest"))

    def test_verdict_is_immutable(self):
        verdict = freshness.decide(_inputs())
        with self.assertRaises(AttributeError):
            verdict.decision = "current"


if __name__ == "__main__":
    unittest.main()
