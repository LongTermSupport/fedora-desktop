"""The installer's source digest: what agent-bus-install is, now that it is a Go binary.

The play bakes this value into the binary at build (`-X main.SourceDigest=`), `team` stamps
it, and `declare` recomputes it from the checkout, so "the checkout moved on, the play has not
run" stays visible without a Go toolchain at status time. The Go side of the agreement is
src-go/internal/businstall/digest_agree_test.go, which compares this module's answer with the
closure `go list` reports.
"""

import hashlib
import io
import pathlib
import tempfile
import unittest

from helpers.agent_bus import installer_digest

REPO_ROOT = pathlib.Path(__file__).resolve().parents[3]
MODULE = "example.test/mod"


def sha(data: bytes) -> str:
    return hashlib.sha256(data).hexdigest()


def write(root: pathlib.Path, name: str, text: str) -> None:
    path = root / "src-go" / name
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(text, encoding="utf-8")


class ToyTreeTest(unittest.TestCase):
    """A module whose command imports one package that imports another; a third is unused."""

    def setUp(self) -> None:
        self.root = pathlib.Path(self.enterContext(tempfile.TemporaryDirectory()))
        write(self.root, "go.mod", f"module {MODULE}\n\ngo 1.26\n")
        write(self.root, "cmd/agent-bus-install/main.go", f"""package main

import (
\t"fmt"

\t"{MODULE}/internal/a"
)

func main() {{ fmt.Println(a.A) }}
""")
        write(self.root, "internal/a/a.go", f"""// Package a. import "{MODULE}/internal/unused"
package a

import (
\t// "{MODULE}/internal/unused"
\tb "{MODULE}/internal/b"
\t_ "embed"
)

var A = b.B
""")
        write(self.root, "internal/b/b.go", 'package b\n\nimport "strings"\n\nvar B = strings.ToUpper("b")\n')
        write(self.root, "internal/b/b_test.go", "package b\n")
        write(self.root, "internal/b/usage.txt", "text the package embeds\n")
        write(self.root, "internal/unused/u.go", "package unused\n")

    def digest(self) -> str:
        return installer_digest.source_digest(self.root)

    def test_the_closure_is_the_command_and_what_it_imports_however_deep(self) -> None:
        self.assertEqual(installer_digest.closure(self.root),
                         ["cmd/agent-bus-install", "internal/a", "internal/b"])

    def test_the_files_are_go_mod_and_every_file_of_a_closure_package_but_tests(self) -> None:
        self.assertEqual(installer_digest.files(self.root), [
            "cmd/agent-bus-install/main.go", "go.mod", "internal/a/a.go", "internal/b/b.go",
            "internal/b/usage.txt"])

    def test_the_digest_is_the_sha256_of_path_nul_filehash_newline_in_path_order(self) -> None:
        listing = b"".join(
            f"{name}\0{sha((self.root / 'src-go' / name).read_bytes())}\n".encode()
            for name in installer_digest.files(self.root))
        self.assertEqual(self.digest(), sha(listing))

    def test_a_change_to_a_file_the_binary_is_built_from_changes_the_digest(self) -> None:
        for name, text in (("internal/b/b.go", 'package b\n\nvar B = "other"\n'),
                           ("internal/b/usage.txt", "other text\n"),
                           ("go.mod", f"module {MODULE}\n\ngo 1.27\n"),
                           ("cmd/agent-bus-install/main.go", "package main\n\nfunc main() {}\n")):
            before = self.digest()
            write(self.root, name, text)
            self.assertNotEqual(self.digest(), before, name)

    def test_a_change_the_binary_does_not_see_leaves_the_digest(self) -> None:
        before = self.digest()
        write(self.root, "internal/unused/u.go", "package unused\n\nvar X = 1\n")
        write(self.root, "internal/b/b_test.go", "package b\n\nvar T = 1\n")
        write(self.root, "internal/b/testdata/deep/f.go", "package deep\n")
        self.assertEqual(self.digest(), before)

    def test_a_package_that_stops_being_imported_leaves_the_digest(self) -> None:
        write(self.root, "internal/a/a.go", "package a\n\nvar A = 1\n")
        self.assertEqual(installer_digest.closure(self.root), ["cmd/agent-bus-install", "internal/a"])

    def test_an_import_of_a_package_that_is_not_there_is_an_error_naming_it(self) -> None:
        write(self.root, "internal/a/a.go", f'package a\n\nimport _ "{MODULE}/internal/gone"\n')
        with self.assertRaisesRegex(installer_digest.DigestError, "internal/gone"):
            self.digest()

    def test_a_checkout_with_no_module_file_is_an_error(self) -> None:
        (self.root / "src-go" / "go.mod").unlink()
        with self.assertRaisesRegex(installer_digest.DigestError, "go.mod"):
            self.digest()

    def test_the_command_line_prints_the_digest_alone(self) -> None:
        out, err = io.StringIO(), io.StringIO()
        code = installer_digest.main(["--source", str(self.root)], stdout=out, stderr=err)
        self.assertEqual((code, out.getvalue(), err.getvalue()), (0, self.digest() + "\n", ""))

    def test_the_command_line_names_a_failure_on_stderr_and_prints_nothing(self) -> None:
        (self.root / "src-go" / "go.mod").unlink()
        out, err = io.StringIO(), io.StringIO()
        code = installer_digest.main(["--source", str(self.root)], stdout=out, stderr=err)
        self.assertEqual((code, out.getvalue()), (1, ""))
        self.assertIn("go.mod", err.getvalue())


class ThisCheckoutTest(unittest.TestCase):
    def test_the_closure_holds_the_installer_and_not_the_tools_it_is_not_built_from(self) -> None:
        closure = installer_digest.closure(REPO_ROOT)
        self.assertIn("cmd/agent-bus-install", closure)
        self.assertIn("internal/businstall", closure)
        self.assertNotIn("internal/teams", closure)
        self.assertNotIn("cmd/ccy-teams", closure)

    def test_the_digest_is_a_sha256(self) -> None:
        digest = installer_digest.source_digest(REPO_ROOT)
        self.assertRegex(digest, r"^[0-9a-f]{64}$")


if __name__ == "__main__":
    unittest.main()
