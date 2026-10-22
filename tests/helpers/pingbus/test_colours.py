"""Unit tests for helpers/pingbus/colours.py, the team colours of the status line.

Run from the repo root:

    python3 -m unittest tests.helpers.pingbus.test_colours

Plan 00217 SPEC-team-colours.md C1 to C7: a palette legible on dark terminals, no hue the status
line already uses for a state, colours told apart, a stable automatic colour per team name, and
the override grammar the team file and the syncer share.
"""

from __future__ import annotations

import colorsys
import itertools
import math
import pathlib
import sys
import unittest

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

from helpers.pingbus import colours


def _channels(hex_colour: str) -> tuple[int, int, int]:
    return int(hex_colour[1:3], 16), int(hex_colour[3:5], 16), int(hex_colour[5:7], 16)


def _lab(hex_colour: str) -> tuple[float, float, float]:
    """CIELAB (D65) of an sRGB colour, for the CIE76 distance of C4."""
    r, g, b = (colours.linear(c) for c in _channels(hex_colour))
    x = (0.4124 * r + 0.3576 * g + 0.1805 * b) / 0.95047
    y = 0.2126 * r + 0.7152 * g + 0.0722 * b
    z = (0.0193 * r + 0.1192 * g + 0.9505 * b) / 1.08883

    def f(t: float) -> float:
        return t ** (1 / 3) if t > 0.008856 else 7.787 * t + 16 / 116

    return 116 * f(y) - 16, 500 * (f(x) - f(y)), 200 * (f(y) - f(z))


class PaletteTest(unittest.TestCase):
    def test_the_palette_is_the_ten_named_colours_in_order(self) -> None:
        self.assertEqual(colours.PALETTE, (
            ("teal", "#2dd4bf"), ("cyan", "#22d3ee"), ("sky", "#7dd3fc"), ("blue", "#60a5fa"),
            ("indigo", "#a5b4fc"), ("violet", "#a78bfa"), ("purple", "#d8b4fe"),
            ("magenta", "#e879f9"), ("pink", "#f9a8d4"), ("mint", "#99f6e4"),
        ))

    def test_every_colour_meets_wcag_aa_on_every_reference_dark_background(self) -> None:
        self.assertEqual(colours.MIN_CONTRAST, 4.5)
        self.assertEqual(colours.DARK_BACKGROUNDS, ("#000000", "#1e1e1e", "#242424", "#282c34", "#2e3440"))
        for name, hex_colour in colours.PALETTE:
            for background in colours.DARK_BACKGROUNDS:
                with self.subTest(name=name, background=background):
                    self.assertGreaterEqual(colours.contrast_ratio(hex_colour, background), colours.MIN_CONTRAST)

    def test_contrast_ratio_is_the_wcag_formula(self) -> None:
        self.assertAlmostEqual(colours.contrast_ratio("#ffffff", "#000000"), 21.0)
        self.assertAlmostEqual(colours.contrast_ratio("#000000", "#ffffff"), 21.0)
        self.assertAlmostEqual(colours.contrast_ratio("#777777", "#777777"), 1.0)
        # WebAIM's checker gives 4.48:1 for #777777 on white.
        self.assertAlmostEqual(colours.contrast_ratio("#777777", "#ffffff"), 4.48, places=2)

    def test_no_hue_of_red_orange_yellow_or_green(self) -> None:
        for name, hex_colour in colours.PALETTE:
            r, g, b = (c / 255 for c in _channels(hex_colour))
            hue = colorsys.rgb_to_hsv(r, g, b)[0] * 360
            with self.subTest(name=name, hue=hue):
                self.assertTrue(160 <= hue <= 335)

    def test_every_pair_is_told_apart(self) -> None:
        for (a, x), (b, y) in itertools.combinations(colours.PALETTE, 2):
            with self.subTest(pair=(a, b)):
                self.assertGreaterEqual(math.dist(_lab(x), _lab(y)), 15)

    def test_names_and_hexes_are_distinct(self) -> None:
        names = [name for name, _ in colours.PALETTE]
        hexes = [hex_colour for _, hex_colour in colours.PALETTE]
        self.assertEqual(len(set(names)), len(names))
        self.assertEqual(len(set(hexes)), len(hexes))


class AutoColourTest(unittest.TestCase):
    def test_fnv1a32_matches_the_published_vectors(self) -> None:
        self.assertEqual(colours.fnv1a32(b""), 0x811C9DC5)
        self.assertEqual(colours.fnv1a32(b"a"), 0xE40C292C)
        self.assertEqual(colours.fnv1a32(b"foobar"), 0xBF9CF968)

    def test_auto_colour_is_the_palette_entry_the_hash_picks(self) -> None:
        for team in ("alpha", "beta", "gamma", "dev-team", "a"):
            with self.subTest(team=team):
                index = colours.fnv1a32(team.encode()) % len(colours.PALETTE)
                self.assertEqual(colours.auto_colour(team), colours.PALETTE[index][1])

    def test_auto_colour_is_pinned_for_known_names(self) -> None:
        # Stable across releases: changing the palette or the hash re-colours every team.
        self.assertEqual(colours.auto_colour("alpha"), "#e879f9")  # magenta
        self.assertEqual(colours.auto_colour("beta"), "#22d3ee")  # cyan
        self.assertEqual(colours.auto_colour("gamma"), "#a5b4fc")  # indigo

    def test_sgr_is_a_24_bit_foreground(self) -> None:
        self.assertEqual(colours.sgr("#2dd4bf"), "\033[38;2;45;212;191m")


class ParseColourTest(unittest.TestCase):
    def test_a_palette_name_is_its_hex(self) -> None:
        for name, hex_colour in colours.PALETTE:
            with self.subTest(name=name):
                self.assertEqual(colours.parse_colour(name), hex_colour)

    def test_a_hex_is_stored_lower_case(self) -> None:
        self.assertEqual(colours.parse_colour("#FFD7AF"), "#ffd7af")
        self.assertEqual(colours.parse_colour("#a5b4fc"), "#a5b4fc")

    def test_an_unknown_name_or_bad_hex_is_refused_naming_the_choices(self) -> None:
        names = ", ".join(name for name, _ in colours.PALETTE)
        for value in ("red", "Teal", "", "#fff", "#gggggg", "2dd4bf", "#2dd4bf0", " #2dd4bf", 7, None, ["teal"]):
            with self.subTest(value=value), self.assertRaises(colours.ColourError) as caught:
                colours.parse_colour(value)
            self.assertEqual(str(caught.exception), f"must be one of {names}, or #rrggbb")

    def test_a_dark_hex_is_refused_with_its_worst_contrast(self) -> None:
        with self.assertRaises(colours.ColourError) as caught:
            colours.parse_colour("#1e3a8a")
        ratio = min(colours.contrast_ratio("#1e3a8a", bg) for bg in colours.DARK_BACKGROUNDS)
        self.assertEqual(str(caught.exception),
                         f"#1e3a8a is too dark to read on a dark terminal (contrast {ratio:.2f}:1 against "
                         "#2e3440, at least 4.5:1 needed)")

    def test_a_stored_colour_is_lower_case_hex_alone(self) -> None:
        self.assertTrue(colours.is_stored_colour("#2dd4bf"))
        for value in ("#2DD4BF", "teal", "#2dd4b", "#2dd4bf ", 1, None):
            with self.subTest(value=value):
                self.assertFalse(colours.is_stored_colour(value))


if __name__ == "__main__":
    unittest.main()
