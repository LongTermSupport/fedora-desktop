"""Tests for the `qa_gate_detail` call-site parser.

The bash version of this was three greps that had to agree with each other: a
strict extraction, a loose denominator, and the loop that re-split each match.
Every defect found in it was one of the three drifting from the other two — a
spelling the extraction took and the splitter mis-read, a spelling both greps
missed in lockstep, a prose mention only one of them counted.

A parser that reports what it COULD NOT parse has no second count to disagree
with. The guard is `unparsed`, not arithmetic.
"""

import os
import sys
import unittest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "..", ".."))

from helpers.qa_environment import gate_call_sites


class TestStripComment(unittest.TestCase):
    """A trailing comment is not code, and a `#` inside quotes is not a comment.

    Both directions were live defects: a comment mentioning the function
    inflated the old denominator and failed the suite with a message blaming the
    extraction regex, while the header comment escaped only because it happened
    to write the name in backticks.
    """

    def test_a_trailing_comment_is_removed(self):
        self.assertEqual(gate_call_sites.strip_comment("x=1  # hi"), "x=1")

    def test_a_whole_line_comment_becomes_empty(self):
        self.assertEqual(gate_call_sites.strip_comment("   # qa_gate_detail"), "")

    def test_a_hash_inside_single_quotes_is_kept(self):
        self.assertEqual(gate_call_sites.strip_comment("x='a#b'"), "x='a#b'")

    def test_a_hash_inside_double_quotes_is_kept(self):
        self.assertEqual(gate_call_sites.strip_comment('x="a#b"'), 'x="a#b"')

    def test_a_hash_with_no_whitespace_before_it_is_not_a_comment(self):
        """`x=1#two` is a literal in bash, not a comment."""
        self.assertEqual(gate_call_sites.strip_comment("x=1#two"), "x=1#two")

    def test_a_line_with_no_comment_is_unchanged(self):
        self.assertEqual(gate_call_sites.strip_comment("x=$(f 'a')"), "x=$(f 'a')")


class TestEverySpellingIsParsed(unittest.TestCase):
    """One case per spelling the extraction accepts.

    The regex was widened to five spellings and the code that split a match was
    not, so `${braces}` reached the gate lookup carrying its braces and a
    double-quoted pattern handed the whole call-site text over as the pattern.
    Both FAILED with a confident, wrong diagnosis. No case covered any of them,
    which is why widening the regex read as finishing the job.
    """

    def parse_one(self, line):
        sites, unparsed = gate_call_sites.call_sites(line)
        self.assertEqual(unparsed, [], f"did not parse: {line!r}")
        self.assertEqual(len(sites), 1, sites)
        return sites[0]

    def test_plain_variable_and_single_quoted_pattern(self):
        site = self.parse_one("""out=$(qa_gate_detail "$nokill_out" '[0-9]+ clean')""")
        self.assertEqual(site["var"], "nokill_out")
        self.assertEqual(site["pattern"], "[0-9]+ clean")

    def test_braced_variable(self):
        site = self.parse_one("""out=$(qa_gate_detail "${nokill_out}" '[0-9]+ clean')""")
        self.assertEqual(site["var"], "nokill_out")

    def test_uppercase_and_digits_in_the_name(self):
        site = self.parse_one("""out=$(qa_gate_detail "$nokillOut2" '[0-9]+ clean')""")
        self.assertEqual(site["var"], "nokillOut2")

    def test_double_quoted_pattern(self):
        site = self.parse_one('''out=$(qa_gate_detail "$nokill_out" "[0-9]+ clean")''')
        self.assertEqual(site["pattern"], "[0-9]+ clean")

    def test_a_tab_between_the_name_and_its_first_argument(self):
        """Missed by BOTH old greps, which keyed on one literal space."""
        site = self.parse_one("""out=$(qa_gate_detail\t"$nokill_out" '[0-9]+ clean')""")
        self.assertEqual(site["var"], "nokill_out")

    def test_a_call_at_column_zero(self):
        site = self.parse_one("""qa_gate_detail "$nokill_out" '[0-9]+ clean'""")
        self.assertEqual(site["var"], "nokill_out")


class TestCounting(unittest.TestCase):
    def test_two_calls_on_one_line_are_two_sites(self):
        """`grep -c` counts LINES, so the old denominator read this as one."""
        line = ("""a=$(qa_gate_detail "$x_out" 'one') """
                """b=$(qa_gate_detail "$y_out" 'two')""")
        sites, unparsed = gate_call_sites.call_sites(line)
        self.assertEqual(unparsed, [])
        self.assertEqual([s["var"] for s in sites], ["x_out", "y_out"])

    def test_a_mention_in_a_comment_is_not_a_call_site(self):
        text = "# call qa_gate_detail with a pattern\nx=1\n"
        sites, unparsed = gate_call_sites.call_sites(text)
        self.assertEqual((sites, unparsed), ([], []))

    def test_a_mention_in_a_trailing_comment_is_not_a_call_site(self):
        text = """x=1  # see qa_gate_detail "$foo" 'bar'\n"""
        sites, unparsed = gate_call_sites.call_sites(text)
        self.assertEqual((sites, unparsed), ([], []))

    def test_a_longer_identifier_containing_the_name_is_not_a_call_site(self):
        text = """out=$(_qa_gate_detail "$x_out" 'one')\n"""
        sites, unparsed = gate_call_sites.call_sites(text)
        self.assertEqual((sites, unparsed), ([], []))

    def test_the_name_as_a_suffix_is_not_a_call_site(self):
        text = """out=$(qa_gate_detail_v2 "$x_out" 'one')\n"""
        sites, unparsed = gate_call_sites.call_sites(text)
        self.assertEqual((sites, unparsed), ([], []))


class TestUnparsedIsTheGuard(unittest.TestCase):
    """The one thing this design must never do is drop a call site silently.

    The old pair could: a spelling neither grep matched left numerator and
    denominator equal and the suite printed a pass. Here anything recognised as
    a call and not parsed lands in `unparsed`, which the caller fails on, and it
    carries the text so the reader can see what it was.
    """

    def test_an_unrecognised_argument_shape_is_reported_not_dropped(self):
        text = """out=$(qa_gate_detail $bare_word 'one')\n"""
        sites, unparsed = gate_call_sites.call_sites(text)
        self.assertEqual(sites, [])
        self.assertEqual(len(unparsed), 1)
        self.assertIn("bare_word", unparsed[0]["text"])

    def test_a_missing_pattern_is_reported(self):
        text = """out=$(qa_gate_detail "$x_out")\n"""
        sites, unparsed = gate_call_sites.call_sites(text)
        self.assertEqual(sites, [])
        self.assertEqual(len(unparsed), 1)

    def test_the_report_carries_the_line_number(self):
        text = "x=1\ny=2\n" + """out=$(qa_gate_detail $bare 'one')\n"""
        _, unparsed = gate_call_sites.call_sites(text)
        self.assertEqual(unparsed[0]["line"], 3)

    def test_a_parsed_site_also_carries_its_line_number(self):
        text = "x=1\n" + """out=$(qa_gate_detail "$x_out" 'one')\n"""
        sites, _ = gate_call_sites.call_sites(text)
        self.assertEqual(sites[0]["line"], 2)


class TestAgainstTheRealScript(unittest.TestCase):
    """The parser is pointed at the file it exists to read.

    A fixture cannot notice the thing it copies changing, which is the defect
    the coupling section was written to close — so the parser is run against the
    real `scripts/qa-all.bash` here as well as against synthetic lines.
    """

    def setUp(self):
        root = os.path.join(os.path.dirname(__file__), "..", "..", "..")
        self.qa_all = os.path.abspath(os.path.join(root, "scripts", "qa-all.bash"))

    def test_the_real_qa_all_parses_with_nothing_left_over(self):
        with open(self.qa_all, encoding="utf-8") as handle:
            sites, unparsed = gate_call_sites.call_sites(handle.read())
        self.assertEqual(unparsed, [], "a real call site did not parse")
        self.assertGreater(len(sites), 0, "no call sites found — the parser broke")

    def test_every_real_site_names_a_variable_and_a_non_empty_pattern(self):
        with open(self.qa_all, encoding="utf-8") as handle:
            sites, _ = gate_call_sites.call_sites(handle.read())
        for site in sites:
            self.assertRegex(site["var"], r"^[A-Za-z_][A-Za-z0-9_]*$")
            self.assertTrue(site["pattern"], site)


if __name__ == "__main__":
    unittest.main()
