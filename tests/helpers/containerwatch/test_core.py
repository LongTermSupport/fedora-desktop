"""Unit tests for helpers/containerwatch/core.py — pure container-watchdog logic.

Run from the repo root with no third-party deps (stdlib unittest only):

    python3 -m unittest tests.helpers.containerwatch.test_core

containerwatch is a namespace package (no __init__.py); we put the repo root on
sys.path so `from helpers.containerwatch import core` resolves. The sys.path edit
before the import is why ruff E402 is ignored for tests/** in ruff.toml.

These tests are the L1 layer of CLAUDE/Plan/00055 testing.md: the full
attribution matrix (podman rootless/rootful, docker systemd+cgroupfs drivers, lxc,
host, unknown), detection logic, schema, and the comm/NSpid/allowlist/PID-reuse
regression cases — all driven from in-memory fixture content, no /proc, no
containers, no privileges.

Public-repo fixture hygiene (testing.md §3 / CLAUDE/ExampleValues.md): every
fixture uses reserved placeholders — synthetic hex container ids, `<project-a>`
style names, no real workspace paths or captured argv.
"""

from __future__ import annotations

import os
import pathlib
import sys
import tempfile
import unittest

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

from helpers.containerwatch import core

# A synthetic 64-hex container id (NOT a real container) — reserved-placeholder style.
FAKE_ID = "abcdef0123456789" * 4
SHORT_ID = FAKE_ID[:12]


def make_stat(pid: int, comm: str, *, utime: int, stime: int, starttime: int) -> str:
    """Build a /proc/<pid>/stat line.

    Real layout (1-based): 1 pid, 2 (comm), 3 state, ... 14 utime, 15 stime,
    ... 22 starttime. We emit fields 3..22 (20 values) which is all the parser
    needs; the `comm` value deliberately contains no parens so the last-')' scan
    is exercised without edge complexity (a separate test covers parens-in-comm).
    """
    after_comm = ["0"] * 20
    after_comm[0] = "R"          # field 3: state
    after_comm[11] = str(utime)  # field 14
    after_comm[12] = str(stime)  # field 15
    after_comm[19] = str(starttime)  # field 22
    return f"{pid} ({comm}) " + " ".join(after_comm) + "\n"


class TestParseCgroup(unittest.TestCase):
    def test_podman_rootless(self):
        text = (
            "0::/user.slice/user-1000.slice/user@1000.service/user.slice/"
            f"libpod-{FAKE_ID}.scope/container\n"
        )
        attr = core.parse_cgroup(text)
        self.assertEqual(attr.engine, "podman")
        self.assertTrue(attr.rootless)
        self.assertEqual(attr.owner_uid, 1000)
        self.assertEqual(attr.container_token, FAKE_ID)

    def test_podman_rootful(self):
        text = f"0::/machine.slice/libpod-{FAKE_ID}.scope/container\n"
        attr = core.parse_cgroup(text)
        self.assertEqual(attr.engine, "podman")
        self.assertFalse(attr.rootless)
        self.assertEqual(attr.owner_uid, 0)
        self.assertEqual(attr.container_token, FAKE_ID)

    def test_docker_systemd_driver(self):
        text = f"0::/system.slice/docker-{FAKE_ID}.scope\n"
        attr = core.parse_cgroup(text)
        self.assertEqual(attr.engine, "docker")
        self.assertFalse(attr.rootless)
        self.assertEqual(attr.owner_uid, 0)
        self.assertEqual(attr.container_token, FAKE_ID)

    def test_docker_cgroupfs_driver(self):
        text = f"0::/docker/{FAKE_ID}\n"
        attr = core.parse_cgroup(text)
        self.assertEqual(attr.engine, "docker")
        self.assertEqual(attr.container_token, FAKE_ID)

    def test_lxc_payload(self):
        text = "0::/lxc.payload.devbox/system.slice/cron.service\n"
        attr = core.parse_cgroup(text)
        self.assertEqual(attr.engine, "lxc")
        self.assertFalse(attr.rootless)
        self.assertEqual(attr.container_token, "devbox")

    def test_lxc_plain(self):
        text = "0::/lxc/devbox\n"
        attr = core.parse_cgroup(text)
        self.assertEqual(attr.engine, "lxc")
        self.assertEqual(attr.container_token, "devbox")

    def test_host_process_returns_none(self):
        text = (
            "0::/user.slice/user-1000.slice/user@1000.service/"
            "app.slice/app-foo.scope\n"
        )
        self.assertIsNone(core.parse_cgroup(text))

    def test_unknown_engine_bucket(self):
        # A container runtime we can see in the cgroup but can't engine-resolve
        # (e.g. kubepods/cri-o) → 'unknown', logged but never treated as host.
        text = f"0::/kubepods/besteffort/pod123/crio-{FAKE_ID}\n"
        attr = core.parse_cgroup(text)
        self.assertEqual(attr.engine, "unknown")

    def test_cgroup_v1_fails_fast(self):
        # v1 shows controller hierarchy lines, not a single 0:: line. Pure v1
        # (controllers, no usable 0:: container path) must raise rather than be
        # silently mis-attributed.
        text_pure_v1 = (
            "12:cpuset:/\n"
            f"11:memory:/docker/{FAKE_ID}\n"
        )
        with self.assertRaises(core.CgroupV1Error):
            core.parse_cgroup(text_pure_v1)


class TestCmdline(unittest.TestCase):
    def test_argv0_basename_from_path(self):
        cmdline = b"/usr/bin/ugrep\x00-rl\x00pattern\x00/\x00"
        self.assertEqual(core.argv0_basename(cmdline), "ugrep")

    def test_cmd_joined_human_readable(self):
        cmdline = b"ugrep\x00-rl\x00pattern\x00/\x00"
        self.assertEqual(core.cmdline_to_cmd(cmdline), "ugrep -rl pattern /")

    def test_empty_cmdline_kernel_thread(self):
        # Kernel threads have an empty cmdline; argv0 falls back to "".
        self.assertEqual(core.argv0_basename(b""), "")
        self.assertEqual(core.cmdline_to_cmd(b""), "")


class TestNspid(unittest.TestCase):
    def test_two_levels_returns_container_pid(self):
        status = "Name:\tugrep\nNSpid:\t2124472\t42\nUid:\t1000\n"
        self.assertEqual(core.parse_nspid(status), 42)

    def test_single_level_returns_none(self):
        # A host process (not namespaced) has one NSpid field → no container pid.
        status = "Name:\tbash\nNSpid:\t2124472\n"
        self.assertIsNone(core.parse_nspid(status))

    def test_three_levels_takes_first_nested(self):
        # Nested namespaces: take the 2nd field (first nested level), per §2c.
        status = "NSpid:\t100\t50\t7\n"
        self.assertEqual(core.parse_nspid(status), 50)

    def test_missing_nspid_returns_none(self):
        self.assertIsNone(core.parse_nspid("Name:\tfoo\n"))


class TestStatParsing(unittest.TestCase):
    def test_starttime_and_cpu_fields(self):
        stat = make_stat(99, "ugrep", utime=500, stime=100, starttime=777)
        self.assertEqual(core.parse_starttime(stat), 777)
        self.assertEqual(core.parse_utime_stime(stat), (500, 100))

    def test_comm_with_spaces_and_parens(self):
        # comm can contain spaces and parens; parser must split on the LAST ')'.
        after_comm = ["0"] * 20
        after_comm[0] = "S"
        after_comm[11] = "12"
        after_comm[12] = "3"
        after_comm[19] = "555"
        stat = "1234 (Web Content (tab)) " + " ".join(after_comm) + "\n"
        self.assertEqual(core.parse_starttime(stat), 555)
        self.assertEqual(core.parse_utime_stime(stat), (12, 3))


class TestAgeAndCpu(unittest.TestCase):
    def test_age_from_starttime_btime(self):
        # starttime in ticks; age = now - (btime + starttime/HZ).
        clock = 100
        # process started 600s after boot; boot at unix 1000; now 1000+1500.
        age = core.compute_age_s(
            starttime_ticks=600 * clock, btime=1000, now_unix=1000 + 1500, clock_ticks=clock
        )
        self.assertEqual(age, 900)

    def test_cpu_pct_per_core(self):
        # 300 ticks consumed over a 1.5s interval at 100 HZ → 3.0 core-seconds /
        # 1.5s = 200% of a single core.
        pct = core.compute_cpu_pct(ticks_delta=300, interval_s=1.5, clock_ticks=100)
        self.assertAlmostEqual(pct, 200.0)

    def test_is_flagged_requires_both_thresholds(self):
        self.assertTrue(core.is_flagged(age_s=1000, cpu_pct=80, age_threshold=900, cpu_threshold=50))
        self.assertFalse(core.is_flagged(age_s=10, cpu_pct=80, age_threshold=900, cpu_threshold=50))
        self.assertFalse(core.is_flagged(age_s=1000, cpu_pct=10, age_threshold=900, cpu_threshold=50))


class TestCpuDelta(unittest.TestCase):
    def test_delta_pct_normal(self):
        before = make_stat(7, "x", utime=100, stime=50, starttime=999)
        after = make_stat(7, "x", utime=250, stime=200, starttime=999)
        # delta = (250+200)-(100+50)=300 ticks over 1.5s at 100HZ → 200%.
        pct = core.cpu_delta_pct(before, after, interval_s=1.5, clock_ticks=100)
        self.assertAlmostEqual(pct, 200.0)

    def test_pid_reuse_returns_none(self):
        before = make_stat(7, "x", utime=100, stime=50, starttime=999)
        after = make_stat(7, "y", utime=10, stime=5, starttime=12345)  # starttime changed
        self.assertIsNone(core.cpu_delta_pct(before, after, interval_s=1.5, clock_ticks=100))

    def test_negative_delta_returns_none(self):
        before = make_stat(7, "x", utime=100, stime=50, starttime=999)
        after = make_stat(7, "x", utime=10, stime=5, starttime=999)
        self.assertIsNone(core.cpu_delta_pct(before, after, interval_s=1.5, clock_ticks=100))


class TestExecHint(unittest.TestCase):
    def test_podman_rootless_hint(self):
        attr = core.Attribution(engine="podman", rootless=True, owner_uid=1000, container_token=FAKE_ID)
        hint = core.build_exec_hint(attr, container_name="proj_a_yolo", container_pid=42, host_pid=999)
        self.assertIn("podman exec", hint)
        self.assertNotIn("sudo", hint)
        self.assertIn("proj_a_yolo", hint)
        self.assertIn("42", hint)

    def test_podman_rootful_hint_uses_sudo(self):
        attr = core.Attribution(engine="podman", rootless=False, owner_uid=0, container_token=FAKE_ID)
        hint = core.build_exec_hint(attr, container_name="proj_a", container_pid=7, host_pid=999)
        self.assertTrue(hint.startswith("sudo podman exec"))

    def test_docker_hint(self):
        attr = core.Attribution(engine="docker", rootless=False, owner_uid=0, container_token=FAKE_ID)
        hint = core.build_exec_hint(attr, container_name="web", container_pid=7, host_pid=999)
        self.assertIn("docker exec", hint)
        self.assertIn("web", hint)

    def test_lxc_hint_uses_attach(self):
        attr = core.Attribution(engine="lxc", rootless=False, owner_uid=0, container_token="devbox")
        hint = core.build_exec_hint(attr, container_name="devbox", container_pid=7, host_pid=999)
        self.assertIn("lxc-attach", hint)
        self.assertIn("devbox", hint)

    def test_hint_without_container_pid_omits_pid_flag(self):
        attr = core.Attribution(engine="podman", rootless=True, owner_uid=1000, container_token=FAKE_ID)
        hint = core.build_exec_hint(attr, container_name="proj_a", container_pid=None, host_pid=999)
        self.assertNotIn(" -p ", hint)


class TestMakeFindingAndSchema(unittest.TestCase):
    def test_finding_schema_v1(self):
        attr = core.Attribution(engine="podman", rootless=True, owner_uid=1000, container_token=FAKE_ID)
        finding = core.make_finding(
            host_pid=2124472,
            container_pid=42,
            attr=attr,
            argv0="ugrep",
            cmd="ugrep -rl pattern /",
            age_s=6916,
            cpu_pct=1116.4,
            rss_kb=35784,
            container_name="proj_a_yolo",
            exec_hint="podman exec -it proj_a_yolo ps -o pid,%cpu,args -p 42",
        )
        self.assertEqual(finding["host_pid"], 2124472)
        self.assertEqual(finding["container_pid"], 42)
        self.assertEqual(finding["engine"], "podman")
        self.assertTrue(finding["rootless"])
        self.assertEqual(finding["owner_uid"], 1000)
        self.assertEqual(finding["container_name"], "proj_a_yolo")
        self.assertEqual(finding["argv0"], "ugrep")
        self.assertEqual(finding["cpu_pct"], 1116)  # rounded to int
        self.assertIn("exec_hint", finding)

    def test_container_pid_null_when_untranslated(self):
        attr = core.Attribution(engine="docker", rootless=False, owner_uid=0, container_token=FAKE_ID)
        finding = core.make_finding(
            host_pid=10, container_pid=None, attr=attr, argv0="x", cmd="x",
            age_s=1000, cpu_pct=80, rss_kb=None, container_name="web",
            exec_hint="docker exec -it web ps",
        )
        # Never 0 — explicitly null when untranslated.
        self.assertIsNone(finding["container_pid"])


class TestAllowlist(unittest.TestCase):
    def _finding(self, name, cmd):
        return {"container_name": name, "cmd": cmd}

    def test_match_by_container_name(self):
        allow = [{"container_name": "build-box"}]
        self.assertTrue(core.matches_allowlist(self._finding("build-box", "make -j"), allow))
        self.assertFalse(core.matches_allowlist(self._finding("other", "make -j"), allow))

    def test_match_by_cmd_glob(self):
        allow = [{"cmd_pattern": "*ffmpeg*"}]
        self.assertTrue(core.matches_allowlist(self._finding("any", "/usr/bin/ffmpeg -i x"), allow))
        self.assertFalse(core.matches_allowlist(self._finding("any", "ugrep -rl x /"), allow))

    def test_match_requires_both_when_both_present(self):
        allow = [{"container_name": "ci", "cmd_pattern": "*pytest*"}]
        self.assertTrue(core.matches_allowlist(self._finding("ci", "python -m pytest"), allow))
        self.assertFalse(core.matches_allowlist(self._finding("ci", "make"), allow))
        self.assertFalse(core.matches_allowlist(self._finding("other", "pytest"), allow))


class TestScanProcRoot(unittest.TestCase):
    """End-to-end detection over a fixture proc_root with injected sampler/resolver."""

    def setUp(self):
        self.tmp = tempfile.mkdtemp(prefix="cw-proc-")
        self.clock = 100
        self.btime = 1000
        self.now = 1000 + 10000  # 10000s since boot

    def tearDown(self):
        for root, dirs, files in os.walk(self.tmp, topdown=False):
            for f in files:
                os.unlink(os.path.join(root, f))
            for d in dirs:
                os.rmdir(os.path.join(root, d))
        os.rmdir(self.tmp)

    def _write_proc(self, pid, *, cgroup, comm, argv0_cmdline, starttime, status_nspid=None, vmrss=None):
        d = os.path.join(self.tmp, str(pid))
        os.makedirs(d)
        with open(os.path.join(d, "cgroup"), "w", encoding="utf-8") as fh:
            fh.write(cgroup)
        with open(os.path.join(d, "stat"), "w", encoding="utf-8") as fh:
            fh.write(make_stat(pid, comm, utime=0, stime=0, starttime=starttime))
        with open(os.path.join(d, "cmdline"), "wb") as fh:
            fh.write(argv0_cmdline)
        status = f"Name:\t{comm}\n"
        if status_nspid is not None:
            status += "NSpid:\t" + "\t".join(str(x) for x in status_nspid) + "\n"
        if vmrss is not None:
            status += f"VmRSS:\t{vmrss} kB\n"
        with open(os.path.join(d, "status"), "w", encoding="utf-8") as fh:
            fh.write(status)

    def _scan(self, *, cpu_by_pid, allowlist=None, age_threshold=900, cpu_threshold=50):
        return core.scan_proc_root(
            self.tmp,
            now=self.now,
            btime=self.btime,
            clock_ticks=self.clock,
            age_threshold=age_threshold,
            cpu_threshold=cpu_threshold,
            cpu_sampler=lambda pid: cpu_by_pid.get(pid, 0.0),
            name_resolver=lambda attr: f"name-{attr.container_token[:6]}",
            allowlist=allowlist or [],
        )

    def test_old_hot_container_process_flagged(self):
        # started 100s after boot → age = 10000-100 = 9900s (> 900); hot via sampler.
        self._write_proc(
            500, cgroup=f"0::/user.slice/user-1000.slice/libpod-{FAKE_ID}.scope/container\n",
            comm="ugrep", argv0_cmdline=b"ugrep\x00-rl\x00x\x00/\x00",
            starttime=100 * self.clock, status_nspid=[500, 42], vmrss=35784,
        )
        findings = self._scan(cpu_by_pid={500: 800.0})
        self.assertEqual(len(findings), 1)
        f = findings[0]
        self.assertEqual(f["engine"], "podman")
        self.assertEqual(f["container_pid"], 42)
        self.assertEqual(f["argv0"], "ugrep")
        self.assertEqual(f["cpu_pct"], 800)
        self.assertEqual(f["rss_kb"], 35784)

    def test_comm_gotcha_still_flagged(self):
        # The pinning bug: comm lies ("claude.exe") but argv[0] is ugrep. Matching
        # is cmdline-based, so this MUST still be enumerated and flagged.
        self._write_proc(
            501, cgroup=f"0::/machine.slice/libpod-{FAKE_ID}.scope/container\n",
            comm="claude.exe", argv0_cmdline=b"/usr/bin/ugrep\x00-rl\x00x\x00/\x00",
            starttime=50 * self.clock, status_nspid=[501, 9],
        )
        findings = self._scan(cpu_by_pid={501: 1116.0})
        self.assertEqual(len(findings), 1)
        self.assertEqual(findings[0]["argv0"], "ugrep")

    def test_old_idle_not_flagged(self):
        self._write_proc(
            502, cgroup=f"0::/docker/{FAKE_ID}\n", comm="sleep",
            argv0_cmdline=b"sleep\x00infinity\x00", starttime=10 * self.clock,
        )
        self.assertEqual(self._scan(cpu_by_pid={502: 1.0}), [])

    def test_hot_young_not_flagged(self):
        self._write_proc(
            503, cgroup=f"0::/docker/{FAKE_ID}\n", comm="make",
            argv0_cmdline=b"make\x00-j\x00", starttime=9990 * self.clock,  # age ~10s
        )
        self.assertEqual(self._scan(cpu_by_pid={503: 900.0}), [])

    def test_host_old_hot_not_flagged(self):
        # Not in a container → never flagged even if old and hot.
        self._write_proc(
            504, cgroup="0::/user.slice/user-1000.slice/user@1000.service/app.slice/x.scope\n",
            comm="ugrep", argv0_cmdline=b"ugrep\x00-rl\x00x\x00/\x00", starttime=10 * self.clock,
        )
        self.assertEqual(self._scan(cpu_by_pid={504: 900.0}), [])

    def test_allowlist_suppresses(self):
        self._write_proc(
            505, cgroup=f"0::/docker/{FAKE_ID}\n", comm="ffmpeg",
            argv0_cmdline=b"ffmpeg\x00-i\x00movie\x00", starttime=10 * self.clock,
        )
        allow = [{"cmd_pattern": "*ffmpeg*"}]
        self.assertEqual(len(self._scan(cpu_by_pid={505: 900.0})), 1)
        self.assertEqual(self._scan(cpu_by_pid={505: 900.0}, allowlist=allow), [])

    def test_vanished_pid_skipped(self):
        # A pid dir that disappears mid-scan (sampler returns None) must not crash.
        self._write_proc(
            506, cgroup=f"0::/docker/{FAKE_ID}\n", comm="x",
            argv0_cmdline=b"x\x00", starttime=10 * self.clock,
        )
        findings = core.scan_proc_root(
            self.tmp, now=self.now, btime=self.btime, clock_ticks=self.clock,
            age_threshold=900, cpu_threshold=50,
            cpu_sampler=lambda pid: None,  # process vanished between samples
            name_resolver=lambda attr: "n", allowlist=[],
        )
        self.assertEqual(findings, [])


if __name__ == "__main__":
    unittest.main()
