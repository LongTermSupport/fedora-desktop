"""Unit tests for helpers/vmtest/basejson.py — the base.json record (DESIGN.md §3.2).

Run from the repo root with no third-party deps:

    python3 -m unittest tests.helpers.vmtest.test_basejson

base.json is the fingerprint and provenance of one base. It is written once by
the base builder and read by every run, so its shape is validated on both
sides: a record that parses is a record every field of which has been checked.
"""

from __future__ import annotations

import dataclasses
import json
import pathlib
import sys
import unittest

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

from helpers.vmtest import basejson, upstream

SHA_A = "a" * 64
SHA_B = "b" * 64
SHA_C = "c" * 64
NOW = 1_800_000_000

FAST_FIELDS = {
    "fedora_version": 44,
    "profile": "server",
    "kind": "fast",
    "name": "server-fast-44",
    "compose_id": "Fedora-44-20260422.1",
    "compose_label": "44-1.7",
    "artefacts": ({"name": "Fedora-Cloud-Base-Generic-44-1.7.x86_64.qcow2", "sha256": SHA_A},),
    "treeinfo_checksums": None,
    "recipe_digest": SHA_B,
    "installed_at": NOW,
    "last_upgraded_at": NOW,
    "last_upgraded_revision": 1789172543,
    "last_upgraded_mirror": "https://mirror.example.net/fedora/linux/updates/44/Everything/x86_64/",
    "refresh_state": "complete",
    "base_sha256": SHA_C,
    "base_size": 5_368_709_120,
    "base_mtime": NOW,
}

FULL_FIELDS = {
    **FAST_FIELDS,
    "kind": "full",
    "name": "server-full-44",
    "artefacts": ({"name": "Fedora-Server-netinst-x86_64-44-1.7.iso", "sha256": SHA_A},),
    "treeinfo_checksums": {"images/install.img": SHA_B, "images/pxeboot/vmlinuz": SHA_C},
}


def _record(**overrides):
    fields = {**FAST_FIELDS, **overrides}
    return basejson.build_record(**fields)


class TestBuildRecord(unittest.TestCase):
    def test_carries_every_field_and_a_schema(self):
        record = _record()
        self.assertEqual(record.schema, basejson.SCHEMA)
        self.assertEqual(record.name, "server-fast-44")
        self.assertEqual(record.last_upgraded_revision, 1789172543)
        self.assertEqual(record.refresh_state, "complete")

    def test_artefact_identity_is_computed_from_the_same_fingerprint_as_upstream(self):
        record = _record()
        expected = upstream.artefact_identity(
            upstream.BaseFingerprint(
                base_name="server-fast-44",
                base_kind="fast",
                compose_label="44-1.7",
                artefacts=(upstream.ArtefactRef("Fedora-Cloud-Base-Generic-44-1.7.x86_64.qcow2", SHA_A),),
                treeinfo_checksums=None,
                recipe_digest=SHA_B,
            )
        )
        self.assertEqual(record.artefact_identity, expected)

    def test_fingerprint_of_round_trips(self):
        # lab-status recomputes the identity from live upstream data plus the
        # record's own name/kind/recipe; the fingerprint it starts from must be
        # exactly the one the identity was built from.
        record = _record()
        fingerprint = basejson.fingerprint_of(record)
        self.assertEqual(upstream.artefact_identity(fingerprint), record.artefact_identity)

    def test_full_kind_carries_treeinfo_checksums(self):
        record = basejson.build_record(**FULL_FIELDS)
        self.assertEqual(record.treeinfo_checksums["images/install.img"], SHA_B)

    def test_fast_kind_with_treeinfo_is_rejected(self):
        with self.assertRaises(basejson.BaseRecordError):
            _record(treeinfo_checksums={"images/install.img": SHA_B})

    def test_full_kind_without_treeinfo_is_rejected(self):
        with self.assertRaises(basejson.BaseRecordError):
            basejson.build_record(**{**FULL_FIELDS, "treeinfo_checksums": None})

    def test_refresh_state_must_be_named(self):
        for state in ("complete", "incomplete", "degraded"):
            with self.subTest(state=state):
                self.assertEqual(_record(refresh_state=state).refresh_state, state)
        with self.assertRaises(basejson.BaseRecordError):
            _record(refresh_state="done")

    def test_name_must_carry_the_fedora_version(self):
        # `server-fast-44` on a record claiming fedora_version 45 is a copied
        # record, and the name/kind binding is the anti-substitution property.
        with self.assertRaises(basejson.BaseRecordError):
            _record(fedora_version=45)

    def test_profile_and_kind_are_validated(self):
        with self.assertRaises(basejson.BaseRecordError):
            _record(profile="laptop")
        with self.assertRaises(basejson.BaseRecordError):
            _record(kind="medium")

    def test_times_and_sizes_must_be_positive_integers(self):
        for field in ("installed_at", "last_upgraded_at", "last_upgraded_revision", "base_size", "base_mtime"):
            for value in (0, -1, "1", 1.5, None):
                with self.subTest(field=field, value=value):
                    with self.assertRaises(basejson.BaseRecordError):
                        _record(**{field: value})

    def test_upgrade_cannot_predate_install(self):
        with self.assertRaises(basejson.BaseRecordError):
            _record(installed_at=NOW, last_upgraded_at=NOW - 1)

    def test_digests_must_be_sha256(self):
        for field in ("recipe_digest", "base_sha256"):
            with self.subTest(field=field):
                with self.assertRaises(basejson.BaseRecordError):
                    _record(**{field: "nothex"})
        with self.assertRaises(basejson.BaseRecordError):
            _record(artefacts=({"name": "x.qcow2", "sha256": "short"},))

    def test_mirror_must_be_an_http_url(self):
        for value in ("", "mirror.example.net", "ftp://x/y"):
            with self.subTest(value=value):
                with self.assertRaises(basejson.BaseRecordError):
                    _record(last_upgraded_mirror=value)

    def test_compose_id_and_label_are_validated(self):
        with self.assertRaises(basejson.BaseRecordError):
            _record(compose_id="latest")
        with self.assertRaises(basejson.BaseRecordError):
            _record(compose_label="1.7")


class TestRenderAndParse(unittest.TestCase):
    def test_render_parse_round_trip_is_identity(self):
        record = _record()
        self.assertEqual(basejson.parse_record(basejson.render_record(record)), record)

    def test_full_record_round_trips_too(self):
        record = basejson.build_record(**FULL_FIELDS)
        self.assertEqual(basejson.parse_record(basejson.render_record(record)), record)

    def test_rendered_json_is_sorted_and_newline_terminated(self):
        text = basejson.render_record(_record())
        self.assertTrue(text.endswith("\n"))
        keys = list(json.loads(text))
        self.assertEqual(keys, sorted(keys))

    def test_parse_rejects_a_stale_schema(self):
        document = json.loads(basejson.render_record(_record()))
        document["schema"] = basejson.SCHEMA + 1
        with self.assertRaises(basejson.BaseRecordError):
            basejson.parse_record(json.dumps(document))

    def test_parse_rejects_a_tampered_identity(self):
        # The identity is recomputed on parse, so a record whose stored digest
        # does not match its own fields cannot be read as a valid base.
        document = json.loads(basejson.render_record(_record()))
        document["artefact_identity"] = "f" * 64
        with self.assertRaises(basejson.BaseRecordError):
            basejson.parse_record(json.dumps(document))

    def test_parse_rejects_unknown_and_missing_keys(self):
        document = json.loads(basejson.render_record(_record()))
        document["extra"] = 1
        with self.assertRaises(basejson.BaseRecordError):
            basejson.parse_record(json.dumps(document))
        del document["extra"]
        del document["base_sha256"]
        with self.assertRaises(basejson.BaseRecordError):
            basejson.parse_record(json.dumps(document))

    def test_parse_rejects_invalid_json_and_non_objects(self):
        with self.assertRaises(basejson.BaseRecordError):
            basejson.parse_record("{not json")
        with self.assertRaises(basejson.BaseRecordError):
            basejson.parse_record("[]")

    def test_every_record_field_survives_the_round_trip(self):
        # Derived from the dataclass, so a field added to the record without a
        # place in render/parse goes red here rather than silently dropping.
        record = basejson.build_record(**FULL_FIELDS)
        parsed = basejson.parse_record(basejson.render_record(record))
        for field in dataclasses.fields(basejson.BaseRecord):
            with self.subTest(field=field.name):
                self.assertEqual(getattr(parsed, field.name), getattr(record, field.name))


if __name__ == "__main__":
    unittest.main()
