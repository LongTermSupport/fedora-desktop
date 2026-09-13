"""Unit tests for helpers/vmtest/upstream.py — pure Fedora upstream-signal parsing.

Run from the repo root with no third-party deps:

    python3 -m unittest tests.helpers.vmtest.test_upstream
    # or via the runner that finds every helper test:
    ./scripts/qa-helper-tests.bash

Plan 00110 DESIGN.md §4.1-§4.3. Every function under test is pure: it takes the
TEXT of an upstream document and returns data. No network here, by construction —
the HTTP lives in probe_upstream.py (T1.4), which is the thin executor.

The fixtures below are trimmed from documents fetched live while the design was
written, so the values are real rather than invented.
"""

from __future__ import annotations

import dataclasses
import json
import pathlib
import sys
import unittest

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

from helpers.vmtest import upstream

COMPOSE_ID_TEXT = "Fedora-44-20260422.1\n"

# Trimmed from releases/44/Everything/x86_64/os/.treeinfo. The sections other
# than [checksums]/[tree] are kept so the parser is exercised against the real
# shape rather than a two-section stub.
TREEINFO_TEXT = """\
[checksums]
images/boot.iso = sha256:bd285201494dd0ba09b54d05ac707de1401668b8512a573edb5922dcf9d7067e
images/eltorito.img = sha256:968be6cb726599011985b8f40ba47c16ae84f1cf91ed12b0a44d2c4563d7d92e
images/install.img = sha256:c2571f26c8d46411f8700388f7ab61d8e27356f960430dcc476325b7157ac8b0
images/pxeboot/initrd.img = sha256:ab26d5270b8aa5df60ea86cfdff76716531c095aff235e33d911974f35216d4a
images/pxeboot/vmlinuz = sha256:4b37e4e542a62c580c751787848be6c99e6f908f6712c8c6da85516b8d541de2

[general]
arch = x86_64
family = Fedora
name = Fedora 44
version = 44

[header]
type = productmd.treeinfo
version = 1.2

[release]
name = Fedora
short = Fedora
version = 44

[tree]
arch = x86_64
build_timestamp = 1776865868
platforms = x86_64,xen
variants = Everything

[variant-Everything]
id = Everything
name = Everything
packages = Packages
repository = .
type = variant
uid = Everything
"""

# A top-level JSON array, which is what fedoraproject.org/releases.json serves.
# Four entries: the ones the three bases actually name, per DESIGN.md §4.3.
RELEASES_JSON_TEXT = json.dumps(
    [
        {
            "version": "44",
            "arch": "x86_64",
            "variant": "Cloud",
            "subvariant": "Cloud_Base",
            "link": "https://download.fedoraproject.org/pub/fedora/linux/releases/44/Cloud/x86_64/images/Fedora-Cloud-Base-Generic-44-1.7.x86_64.qcow2",
            "sha256": "28680fe5" + "0" * 50 + "f90b7f",
            "size": "583729152",
        },
        {
            "version": "44",
            "arch": "x86_64",
            "variant": "Server",
            "subvariant": "Server",
            "link": "https://download.fedoraproject.org/pub/fedora/linux/releases/44/Server/x86_64/images/Fedora-Server-Guest-Generic-44-1.7.x86_64.qcow2",
            "sha256": "446c01f7" + "0" * 50 + "dd3f0e",
            "size": "952762368",
        },
        {
            "version": "44",
            "arch": "x86_64",
            "variant": "Everything",
            "subvariant": "Everything",
            "link": "https://download.fedoraproject.org/pub/fedora/linux/releases/44/Everything/x86_64/iso/Fedora-Everything-netinst-x86_64-44-1.7.iso",
            "sha256": "bd285201494dd0ba09b54d05ac707de1401668b8512a573edb5922dcf9d7067e",
            "size": "1217329152",
        },
        {
            "version": "44",
            "arch": "x86_64",
            "variant": "Workstation",
            "subvariant": "Workstation",
            "link": "https://download.fedoraproject.org/pub/fedora/linux/releases/44/Workstation/x86_64/iso/Fedora-Workstation-Live-44-1.7.x86_64.iso",
            "sha256": "1620295f" + "0" * 50 + "426ddf",
            "size": "2851612672",
        },
    ]
)

# Bodhi's `name` space is wider than "F<version>": F44F is the Flatpak release
# and sits alongside F44, so the version lookup has to be an exact match. Both
# are present here for that reason. The pagination fields are real and load-
# bearing — see TestParseBodhiReleases.test_truncated_page_raises.
BODHI_TEXT = json.dumps(
    {
        "releases": [
            {"name": "F44", "version": "44", "branch": "f44", "state": "current"},
            {"name": "F44F", "version": "44F", "branch": "f44", "state": "current"},
            {"name": "F45", "version": "45", "branch": "f45", "state": "pending"},
            {"name": "F46", "version": "46", "branch": "rawhide", "state": "pending"},
            {"name": "F43", "version": "43", "branch": "f43", "state": "archived"},
            {"name": "EPEL-10.0", "version": "10.0", "branch": "epel10.0", "state": "current"},
        ],
        "page": 1,
        "pages": 1,
        "total": 6,
        "rows_per_page": 100,
    }
)

REPOMD_TEXT = """\
<?xml version="1.0" encoding="UTF-8"?>
<repomd xmlns="http://linux.duke.edu/metadata/repo" xmlns:rpm="http://linux.duke.edu/metadata/rpm">
  <revision>1789172543</revision>
  <data type="primary">
    <checksum type="sha256">deadbeef</checksum>
    <location href="repodata/primary.xml.zst"/>
    <size>1234</size>
  </data>
</repomd>
"""

CLOUD_QCOW2 = "Fedora-Cloud-Base-Generic-44-1.7.x86_64.qcow2"
NETINST_ISO = "Fedora-Everything-netinst-x86_64-44-1.7.iso"
LIVE_ISO = "Fedora-Workstation-Live-44-1.7.x86_64.iso"


def _fingerprint(**overrides):
    """A valid `full`-kind fingerprint, with per-test field overrides."""
    treeinfo = upstream.parse_treeinfo(TREEINFO_TEXT)
    index = upstream.parse_releases_json(RELEASES_JSON_TEXT)
    netinst = upstream.find_artefact(index, NETINST_ISO)
    live = upstream.find_artefact(index, LIVE_ISO)
    defaults = {
        "base_name": "desktop-44",
        "base_kind": "full",
        "compose_label": upstream.compose_label_from_link(netinst.link),
        "artefacts": (
            upstream.ArtefactRef(netinst.filename, netinst.sha256),
            upstream.ArtefactRef(live.filename, live.sha256),
        ),
        "treeinfo_checksums": treeinfo.checksums,
        "recipe_digest": "a" * 64,
    }
    defaults.update(overrides)
    return upstream.BaseFingerprint(**defaults)


class TestParseComposeId(unittest.TestCase):
    def test_returns_the_stripped_label(self):
        self.assertEqual(upstream.parse_compose_id(COMPOSE_ID_TEXT), "Fedora-44-20260422.1")

    def test_development_compose_id_is_accepted(self):
        # Branched/Rawhide carry a date and a nightly counter, and DESIGN.md §4.1
        # names them as the signal that actually moves day to day.
        self.assertEqual(
            upstream.parse_compose_id("Fedora-45-20260913.n.0\n"), "Fedora-45-20260913.n.0"
        )

    def test_empty_is_an_error_not_an_empty_label(self):
        for text in ("", "\n", "   \n\t\n"):
            with self.subTest(text=text), self.assertRaises(upstream.UpstreamParseError):
                upstream.parse_compose_id(text)

    def test_multiple_lines_is_an_error(self):
        # A COMPOSE_ID is a single label. More than one non-blank line means the
        # fetch returned something else — an error page, a directory index — and
        # taking the first line would launder that into a plausible-looking value.
        with self.assertRaises(upstream.UpstreamParseError):
            upstream.parse_compose_id("Fedora-44-20260422.1\nFedora-45-20260913.n.0\n")

    def test_html_error_page_is_rejected(self):
        with self.assertRaises(upstream.UpstreamParseError):
            upstream.parse_compose_id("<!DOCTYPE html><html><body>404</body></html>")


class TestParseTreeinfo(unittest.TestCase):
    def test_extracts_every_checksum_with_the_algorithm_stripped(self):
        treeinfo = upstream.parse_treeinfo(TREEINFO_TEXT)
        self.assertEqual(
            sorted(treeinfo.checksums),
            [
                "images/boot.iso",
                "images/eltorito.img",
                "images/install.img",
                "images/pxeboot/initrd.img",
                "images/pxeboot/vmlinuz",
            ],
        )
        self.assertEqual(
            treeinfo.checksums["images/install.img"],
            "c2571f26c8d46411f8700388f7ab61d8e27356f960430dcc476325b7157ac8b0",
        )

    def test_keys_keep_their_case_and_path_separators(self):
        # configparser lowercases option names by default, which would silently
        # rewrite the artefact paths this identity is built from.
        treeinfo = upstream.parse_treeinfo(TREEINFO_TEXT.replace("images/boot.iso", "images/BOOT.iso"))
        self.assertIn("images/BOOT.iso", treeinfo.checksums)

    def test_build_timestamp_is_an_int(self):
        self.assertEqual(upstream.parse_treeinfo(TREEINFO_TEXT).build_timestamp, 1776865868)

    def test_missing_checksums_section_is_an_error(self):
        text = TREEINFO_TEXT.split("[general]", 1)[1]
        with self.assertRaises(upstream.UpstreamParseError):
            upstream.parse_treeinfo("[general]" + text)

    def test_empty_checksums_section_is_an_error(self):
        # An empty section yields an empty mapping, which would hash to a stable
        # value and make the identity vacuous — the exact "check that cannot
        # fail" class this plan's review kept finding.
        with self.assertRaises(upstream.UpstreamParseError):
            upstream.parse_treeinfo("[checksums]\n\n[tree]\nbuild_timestamp = 1\n")

    def test_missing_build_timestamp_is_an_error(self):
        text = TREEINFO_TEXT.replace("build_timestamp = 1776865868\n", "")
        with self.assertRaises(upstream.UpstreamParseError):
            upstream.parse_treeinfo(text)

    def test_non_numeric_build_timestamp_is_an_error(self):
        text = TREEINFO_TEXT.replace("build_timestamp = 1776865868", "build_timestamp = soon")
        with self.assertRaises(upstream.UpstreamParseError):
            upstream.parse_treeinfo(text)

    def test_non_sha256_algorithm_is_an_error(self):
        # The whole identity rests on sha256. A tree that offered md5 would be
        # accepted with a 32-hex value and compared happily forever after.
        text = TREEINFO_TEXT.replace(
            "images/boot.iso = sha256:", "images/boot.iso = md5:"
        )
        with self.assertRaises(upstream.UpstreamParseError):
            upstream.parse_treeinfo(text)

    def test_malformed_hex_is_an_error(self):
        text = TREEINFO_TEXT.replace(
            "sha256:bd285201494dd0ba09b54d05ac707de1401668b8512a573edb5922dcf9d7067e",
            "sha256:notavalidhash",
        )
        with self.assertRaises(upstream.UpstreamParseError):
            upstream.parse_treeinfo(text)

    def test_unparseable_text_is_an_error(self):
        with self.assertRaises(upstream.UpstreamParseError):
            upstream.parse_treeinfo("<html>503 Service Unavailable</html>")


class TestParseReleasesJson(unittest.TestCase):
    def test_indexes_by_filename(self):
        index = upstream.parse_releases_json(RELEASES_JSON_TEXT)
        self.assertIn(CLOUD_QCOW2, index)
        self.assertIn(NETINST_ISO, index)

    def test_carries_link_sha256_and_size(self):
        entry = upstream.find_artefact(upstream.parse_releases_json(RELEASES_JSON_TEXT), NETINST_ISO)
        self.assertEqual(
            entry.sha256, "bd285201494dd0ba09b54d05ac707de1401668b8512a573edb5922dcf9d7067e"
        )
        self.assertEqual(entry.size, 1217329152)
        self.assertTrue(entry.link.endswith(NETINST_ISO))

    def test_netinst_hash_equals_treeinfo_boot_iso(self):
        # DESIGN.md §4.1 S3: the two signals cross-confirm each other, and that
        # is what establishes the netinst ISO *is* boot.iso. If a future parser
        # change broke either side, this is where it shows.
        treeinfo = upstream.parse_treeinfo(TREEINFO_TEXT)
        entry = upstream.find_artefact(upstream.parse_releases_json(RELEASES_JSON_TEXT), NETINST_ISO)
        self.assertEqual(entry.sha256, treeinfo.checksums["images/boot.iso"])

    def test_unknown_filename_raises_rather_than_returning_none(self):
        index = upstream.parse_releases_json(RELEASES_JSON_TEXT)
        with self.assertRaises(upstream.UpstreamParseError):
            upstream.find_artefact(index, "Fedora-Cloud-Base-Generic-45-1.1.x86_64.qcow2")

    def test_entry_with_empty_sha256_raises_on_lookup(self):
        doc = json.loads(RELEASES_JSON_TEXT)
        doc[0]["sha256"] = ""
        index = upstream.parse_releases_json(json.dumps(doc))
        with self.assertRaises(upstream.UpstreamParseError):
            upstream.find_artefact(index, CLOUD_QCOW2)

    def test_duplicate_filename_with_conflicting_hash_raises(self):
        doc = json.loads(RELEASES_JSON_TEXT)
        clash = dict(doc[0])
        clash["sha256"] = "f" * 64
        doc.append(clash)
        with self.assertRaises(upstream.UpstreamParseError):
            upstream.parse_releases_json(json.dumps(doc))

    def test_duplicate_filename_with_identical_hash_is_fine(self):
        doc = json.loads(RELEASES_JSON_TEXT)
        doc.append(dict(doc[0]))
        index = upstream.parse_releases_json(json.dumps(doc))
        self.assertIn(CLOUD_QCOW2, index)

    def test_entry_without_a_link_raises(self):
        doc = json.loads(RELEASES_JSON_TEXT)
        del doc[0]["link"]
        with self.assertRaises(upstream.UpstreamParseError):
            upstream.parse_releases_json(json.dumps(doc))

    def test_non_list_document_raises(self):
        with self.assertRaises(upstream.UpstreamParseError):
            upstream.parse_releases_json(json.dumps({"releases": []}))

    def test_invalid_json_raises_the_module_error(self):
        with self.assertRaises(upstream.UpstreamParseError):
            upstream.parse_releases_json("<html>502</html>")


class TestSelectArtefact(unittest.TestCase):
    """The base's artefact is chosen by releases.json's structured fields, never by
    a filename that embeds a compose label nobody knows in advance."""

    SELECTOR = upstream.ArtefactSelector(
        variant="Cloud", subvariant="Cloud_Base", prefix="Fedora-Cloud-Base-Generic-", suffix=".qcow2"
    )

    def test_selects_by_version_arch_variant_subvariant_and_suffix(self):
        index = upstream.parse_releases_json(RELEASES_JSON_TEXT)
        entry = upstream.select_artefact(index, 44, "x86_64", self.SELECTOR)
        self.assertEqual(entry.filename, CLOUD_QCOW2)

    def test_carries_the_structured_fields(self):
        entry = upstream.find_artefact(upstream.parse_releases_json(RELEASES_JSON_TEXT), CLOUD_QCOW2)
        self.assertEqual((entry.version, entry.arch, entry.variant, entry.subvariant), ("44", "x86_64", "Cloud", "Cloud_Base"))

    def test_no_match_raises(self):
        index = upstream.parse_releases_json(RELEASES_JSON_TEXT)
        with self.assertRaises(upstream.UpstreamParseError):
            upstream.select_artefact(index, 45, "x86_64", self.SELECTOR)
        with self.assertRaises(upstream.UpstreamParseError):
            upstream.select_artefact(index, 44, "aarch64", self.SELECTOR)
        with self.assertRaises(upstream.UpstreamParseError):
            upstream.select_artefact(index, 44, "x86_64", dataclasses.replace(self.SELECTOR, suffix=".raw.xz"))
        with self.assertRaises(upstream.UpstreamParseError):
            upstream.select_artefact(
                index, 44, "x86_64", dataclasses.replace(self.SELECTOR, prefix="Fedora-Cloud-Base-UEFI-UKI-")
            )

    def test_prefix_separates_siblings_that_share_every_structured_field(self):
        # Measured live: Fedora-Server-dvd-… and Fedora-Server-netinst-… are both
        # variant=Server subvariant=Server .iso. Only the filename prefix tells
        # them apart, which is why the selector carries one.
        doc = json.loads(RELEASES_JSON_TEXT)
        dvd = dict(doc[2])
        dvd["variant"] = dvd["subvariant"] = "Server"
        dvd["link"] = dvd["link"].replace("Fedora-Everything-netinst", "Fedora-Server-dvd")
        dvd["sha256"] = "1" * 64
        netinst = dict(dvd)
        netinst["link"] = dvd["link"].replace("Fedora-Server-dvd", "Fedora-Server-netinst")
        netinst["sha256"] = "2" * 64
        doc += [dvd, netinst]
        index = upstream.parse_releases_json(json.dumps(doc))
        chosen = upstream.select_artefact(
            index,
            44,
            "x86_64",
            upstream.ArtefactSelector("Server", "Server", "Fedora-Server-netinst-", ".iso"),
        )
        self.assertEqual(chosen.sha256, "2" * 64)

    def test_ambiguous_match_raises(self):
        # Two respins published side by side would both match; picking the
        # "newest" by string comparison of labels is a guess the design does not
        # make. It is an error until the selector is narrowed.
        doc = json.loads(RELEASES_JSON_TEXT)
        respin = dict(doc[0])
        respin["link"] = respin["link"].replace("44-1.7", "44-1.8")
        respin["sha256"] = "e" * 64
        doc.append(respin)
        index = upstream.parse_releases_json(json.dumps(doc))
        with self.assertRaises(upstream.UpstreamParseError):
            upstream.select_artefact(index, 44, "x86_64", self.SELECTOR)

    def test_entry_missing_a_structured_field_raises_at_parse(self):
        for field in ("version", "arch", "variant", "subvariant"):
            with self.subTest(field=field):
                doc = json.loads(RELEASES_JSON_TEXT)
                del doc[0][field]
                with self.assertRaises(upstream.UpstreamParseError):
                    upstream.parse_releases_json(json.dumps(doc))

    def test_selector_fields_must_be_non_empty(self):
        for field in ("variant", "subvariant", "prefix", "suffix"):
            with self.subTest(field=field):
                with self.assertRaises(upstream.UpstreamParseError):
                    upstream.select_artefact(
                        upstream.parse_releases_json(RELEASES_JSON_TEXT),
                        44,
                        "x86_64",
                        dataclasses.replace(self.SELECTOR, **{field: ""}),
                    )


class TestComposeLabelFromLink(unittest.TestCase):
    def test_reads_the_label_from_the_qcow2_filename(self):
        entry = upstream.find_artefact(upstream.parse_releases_json(RELEASES_JSON_TEXT), CLOUD_QCOW2)
        self.assertEqual(upstream.compose_label_from_link(entry.link), "44-1.7")

    def test_reads_the_label_from_both_iso_shapes(self):
        # The version/label pair sits in a different position in these two:
        #   Fedora-Everything-netinst-x86_64-44-1.7.iso   (arch before version)
        #   Fedora-Workstation-Live-44-1.7.x86_64.iso     (arch after version)
        index = upstream.parse_releases_json(RELEASES_JSON_TEXT)
        for filename in (NETINST_ISO, LIVE_ISO):
            with self.subTest(filename=filename):
                entry = upstream.find_artefact(index, filename)
                self.assertEqual(upstream.compose_label_from_link(entry.link), "44-1.7")

    def test_query_string_does_not_confuse_the_basename(self):
        link = "https://example.com/pub/" + CLOUD_QCOW2 + "?mirror=1"
        self.assertEqual(upstream.compose_label_from_link(link), "44-1.7")

    def test_link_without_a_label_raises(self):
        with self.assertRaises(upstream.UpstreamParseError):
            upstream.compose_label_from_link("https://example.com/pub/Fedora-Rawhide.iso")

    def test_empty_link_raises(self):
        with self.assertRaises(upstream.UpstreamParseError):
            upstream.compose_label_from_link("")


class TestParseBodhiReleases(unittest.TestCase):
    def test_maps_release_name_to_state(self):
        states = upstream.parse_bodhi_releases(BODHI_TEXT)
        self.assertEqual(states["F44"], "current")
        self.assertEqual(states["F45"], "pending")
        self.assertEqual(states["F43"], "archived")

    def test_state_for_version_uses_the_numeric_version(self):
        states = upstream.parse_bodhi_releases(BODHI_TEXT)
        self.assertEqual(upstream.bodhi_state_for(states, 44), "current")
        self.assertEqual(upstream.bodhi_state_for(states, 43), "archived")

    def test_unknown_version_raises_rather_than_defaulting(self):
        states = upstream.parse_bodhi_releases(BODHI_TEXT)
        with self.assertRaises(upstream.UpstreamParseError):
            upstream.bodhi_state_for(states, 99)

    def test_flatpak_neighbour_does_not_satisfy_the_lookup(self):
        # Bodhi ships F44 and F44F side by side. A prefix match would read the
        # Flatpak release's state as the Fedora release's, and the two can
        # diverge. Verified live: both exist for 44 and for 45.
        states = upstream.parse_bodhi_releases(
            json.dumps(
                {
                    "releases": [
                        {"name": "F44F", "version": "44F", "branch": "f44", "state": "pending"}
                    ],
                    "page": 1,
                    "pages": 1,
                    "total": 1,
                }
            )
        )
        with self.assertRaises(upstream.UpstreamParseError):
            upstream.bodhi_state_for(states, 44)

    def test_truncated_page_raises(self):
        # Measured live: 85 releases against rows_per_page=100. That is one page
        # today and will silently become two, at which point a single fetch
        # returns a PARTIAL release list that looks exactly like a complete one —
        # and a missing F<version> would read as "unknown release". Detecting the
        # truncation is the difference between a loud error and a wrong answer.
        doc = json.loads(BODHI_TEXT)
        doc["pages"] = 2
        doc["total"] = 120
        with self.assertRaises(upstream.UpstreamParseError):
            upstream.parse_bodhi_releases(json.dumps(doc))

    def test_missing_pagination_fields_raise(self):
        # Absent `pages` cannot be assumed to mean "one page".
        doc = json.loads(BODHI_TEXT)
        del doc["pages"]
        with self.assertRaises(upstream.UpstreamParseError):
            upstream.parse_bodhi_releases(json.dumps(doc))

    def test_missing_releases_key_raises(self):
        with self.assertRaises(upstream.UpstreamParseError):
            upstream.parse_bodhi_releases(json.dumps({"page": 1}))

    def test_empty_releases_list_raises(self):
        # Pagination fields are valid here, so this can only fail on emptiness —
        # otherwise it would pass on the wrong error and vouch for nothing.
        with self.assertRaises(upstream.UpstreamParseError):
            upstream.parse_bodhi_releases(
                json.dumps({"releases": [], "page": 1, "pages": 1, "total": 0})
            )

    def test_entry_without_a_state_raises(self):
        doc = json.loads(BODHI_TEXT)
        del doc["releases"][0]["state"]
        with self.assertRaises(upstream.UpstreamParseError):
            upstream.parse_bodhi_releases(json.dumps(doc))

    def test_entry_without_a_name_raises(self):
        doc = json.loads(BODHI_TEXT)
        del doc["releases"][0]["name"]
        with self.assertRaises(upstream.UpstreamParseError):
            upstream.parse_bodhi_releases(json.dumps(doc))


class TestParseRepomdRevision(unittest.TestCase):
    def test_returns_the_revision_as_an_int(self):
        self.assertEqual(upstream.parse_repomd_revision(REPOMD_TEXT), 1789172543)

    def test_missing_revision_raises(self):
        text = REPOMD_TEXT.replace("<revision>1789172543</revision>", "")
        with self.assertRaises(upstream.UpstreamParseError):
            upstream.parse_repomd_revision(text)

    def test_non_numeric_revision_raises(self):
        text = REPOMD_TEXT.replace("1789172543", "not-a-revision")
        with self.assertRaises(upstream.UpstreamParseError):
            upstream.parse_repomd_revision(text)

    def test_more_than_one_revision_raises(self):
        # repomd.xml carries exactly one. Two means the document is not what we
        # think it is, and picking either would be a guess.
        text = REPOMD_TEXT.replace(
            "<revision>1789172543</revision>",
            "<revision>1789172543</revision>\n  <revision>1776864872</revision>",
        )
        with self.assertRaises(upstream.UpstreamParseError):
            upstream.parse_repomd_revision(text)

    def test_html_error_page_raises(self):
        with self.assertRaises(upstream.UpstreamParseError):
            upstream.parse_repomd_revision("<html>503</html>")


class TestArtefactIdentity(unittest.TestCase):
    def test_is_a_sha256_hex_digest(self):
        digest = upstream.artefact_identity(_fingerprint())
        self.assertRegex(digest, r"^[0-9a-f]{64}$")

    def test_is_deterministic(self):
        self.assertEqual(
            upstream.artefact_identity(_fingerprint()),
            upstream.artefact_identity(_fingerprint()),
        )

    def test_artefact_order_does_not_change_the_identity(self):
        forward = _fingerprint()
        reversed_order = _fingerprint(artefacts=tuple(reversed(forward.artefacts)))
        self.assertEqual(
            upstream.artefact_identity(forward), upstream.artefact_identity(reversed_order)
        )

    def test_treeinfo_key_order_does_not_change_the_identity(self):
        base = _fingerprint()
        shuffled = dict(reversed(list(base.treeinfo_checksums.items())))
        self.assertEqual(
            upstream.artefact_identity(base),
            upstream.artefact_identity(_fingerprint(treeinfo_checksums=shuffled)),
        )

    # --- the load-bearing one -------------------------------------------------
    #
    # A field silently dropped from the hash makes the reinstall trigger vacuous
    # for that field, and nothing else in the system would notice. So the set of
    # fields under test is DERIVED from the dataclass rather than typed out: add
    # a field without a mutator here and this goes red, which is the property a
    # hand-written list cannot give (AgentNotes row 9b).

    MUTATORS = {
        "base_name": lambda fp: {"base_name": "server-full-44"},
        "base_kind": lambda fp: {"base_kind": "fast", "treeinfo_checksums": None},
        "compose_label": lambda fp: {"compose_label": "44-1.8"},
        "artefacts": lambda fp: {
            "artefacts": (upstream.ArtefactRef(fp.artefacts[0].name, "9" * 64),)
            + fp.artefacts[1:]
        },
        "treeinfo_checksums": lambda fp: {
            "treeinfo_checksums": dict(fp.treeinfo_checksums, **{"images/install.img": "b" * 64})
        },
        "recipe_digest": lambda fp: {"recipe_digest": "c" * 64},
    }

    def test_every_fingerprint_field_has_a_mutator(self):
        declared = {f.name for f in dataclasses.fields(upstream.BaseFingerprint)}
        self.assertEqual(
            declared,
            set(self.MUTATORS),
            "BaseFingerprint gained or lost a field without this test being updated — "
            "an unmutated field is one the identity may silently ignore.",
        )

    def test_changing_any_field_changes_the_identity(self):
        base = _fingerprint()
        baseline = upstream.artefact_identity(base)
        for field_name, mutate in self.MUTATORS.items():
            with self.subTest(field=field_name):
                mutated = upstream.artefact_identity(_fingerprint(**mutate(base)))
                self.assertNotEqual(
                    baseline,
                    mutated,
                    f"{field_name} does not contribute to artefact_identity",
                )

    def test_bodhi_state_is_not_an_input(self):
        # DESIGN.md §4.4: "Bodhi leaving `current` warns; it does not rebuild."
        # §4.3's formula listed bodhi_state as an identity input, which would
        # have made it a reinstall trigger and contradicted §4.4 outright. The
        # structural guard is that no such field exists; this test is what keeps
        # it from being reintroduced.
        declared = {f.name for f in dataclasses.fields(upstream.BaseFingerprint)}
        self.assertFalse(
            [name for name in declared if "bodhi" in name],
            "Bodhi release state must never feed artefact_identity — it warns, it does not rebuild.",
        )

    # --- fail-closed validation ----------------------------------------------

    def test_fast_kind_with_treeinfo_checksums_raises(self):
        # A `fast` base is imported from a qcow2 and never runs Anaconda, so it
        # has no install tree. Checksums here mean the caller built the wrong
        # fingerprint, and hashing them anyway would bind the identity to a tree
        # the base was not built from.
        with self.assertRaises(upstream.UpstreamParseError):
            upstream.artefact_identity(_fingerprint(base_kind="fast"))

    def test_full_kind_without_treeinfo_checksums_raises(self):
        with self.assertRaises(upstream.UpstreamParseError):
            upstream.artefact_identity(_fingerprint(treeinfo_checksums=None))

    def test_unknown_kind_raises(self):
        with self.assertRaises(upstream.UpstreamParseError):
            upstream.artefact_identity(_fingerprint(base_kind="medium"))

    def test_no_artefacts_raises(self):
        with self.assertRaises(upstream.UpstreamParseError):
            upstream.artefact_identity(_fingerprint(artefacts=()))

    def test_empty_recipe_digest_raises(self):
        with self.assertRaises(upstream.UpstreamParseError):
            upstream.artefact_identity(_fingerprint(recipe_digest=""))

    def test_malformed_artefact_hash_raises(self):
        bad = (upstream.ArtefactRef(NETINST_ISO, "nothex"),)
        with self.assertRaises(upstream.UpstreamParseError):
            upstream.artefact_identity(_fingerprint(artefacts=bad))

    def test_fast_base_identity_is_computable(self):
        # The positive control for the `fast` branch: server-fast has one
        # artefact and no tree, and must still produce a digest.
        index = upstream.parse_releases_json(RELEASES_JSON_TEXT)
        cloud = upstream.find_artefact(index, CLOUD_QCOW2)
        digest = upstream.artefact_identity(
            upstream.BaseFingerprint(
                base_name="server-fast-44",
                base_kind="fast",
                compose_label=upstream.compose_label_from_link(cloud.link),
                artefacts=(upstream.ArtefactRef(cloud.filename, cloud.sha256),),
                treeinfo_checksums=None,
                recipe_digest="d" * 64,
            )
        )
        self.assertRegex(digest, r"^[0-9a-f]{64}$")


if __name__ == "__main__":
    unittest.main()
