# Rescued 2026-11-06 from an agent worktree's untracked scratch (Plan 00250 Task 2.3); it was held by no ref.
"""Writes the parity fixtures of src-go/internal/bus/declare (Plan 00209 Task 2.2, scratch).

Placeholder values only (CLAUDE/ExampleValues.md). Run from the worktree root.
"""

import pathlib

ROOT = pathlib.Path("src-go/internal/bus/declare/testdata/parity")
VAULT = "!vault |\n      $ANSIBLE_VAULT;1.1;AES256\n      30303030303030303030303030303030\n"


def w(rel: str, text: str | bytes) -> None:
    path = ROOT / rel
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_bytes(text if isinstance(text, bytes) else text.encode("utf-8"))


BASIC = """---
# Example host_vars for the parity test: placeholder values only.
user_login: someone
agent_bus_address: 192.0.2.10   # the bus address
agent_bus_teams:
  - team: alpha
    port: 6167
    description: "Builds the example site"
    listen: ["192.0.2.10"]
    allow_from: ["192.0.2.0/24"]
    humans: ["owner"]
    org: example-org
    exclude: ["legacy"]
    repos:
      - repo: other-org/tool
        branches: ["main"]
    path_prefixes: ["CLAUDE/Plan/", "docs/"]
    forge_api: https://api.github.com
  - team: beta
    port: 6168
    humans: ["owner", "helper"]
    repos:
      - repo: example-org/proj
      - repo: example-org/legacy
  - team: old
    state: absent
    purge: false
agent_bus_element_teams:
  - team: alpha
    base_url: "http://192.0.2.10:6167"
agent_bus_lxc_members:
  - team: alpha
    container: web-1
    user: agent
    checkout: /srv/agent/proj
other_setting: true
"""

w("basic.yml", BASIC)
w("crlf.yml", BASIC.replace("\n", "\r\n"))
w("lone-cr.yml", BASIC.replace("\n", "\r"))
w("mixed-newlines.yml", BASIC.replace("\n", "\r\n", 10).replace("\n", "\r", 5))
w("bom.yml", "\ufeff" + BASIC)
w("empty.yml", "")
w("no-teams.yml", "---\nuser_login: someone\nagent_bus_address: 2001:db8::1\n")
w("teams-empty.yml", "agent_bus_teams:\nother: 1\n  - team: alpha\n    port: 6167\n")
w(
    "teams-comments-only.yml",
    "agent_bus_teams:  # none yet\n  # - team: alpha\n\n# end\n",
)
w("teams-quoted-empty.yml", "agent_bus_teams: ''\n  - team: alpha\n    port: 6167\n")

w(
    "unicode-space-key.yml",
    "agent_bus_address\u3000: 192.0.2.7\nagent_bus_teams\u00a0:\n  - team: alpha\n    port: 6167\n"
    "    org:\u00a0example-org\u2003\n    description: \u3000Padded\u3000\n",
)
w(
    "unicode-line-breaks.yml",
    "agent_bus_teams:\n  - team: alpha\n    port: 6167\n    org: example-org\u2028    description: x\n"
    '  - team: beta\x85    port: 6168\n    description: a\x0bb\n    humans: ["a"]\x0c\n'
    "  - team: gamma\x1c    port: 6169\n    server_name: g\u2029.example.com\n",
)
w(
    "trailing-space.yml",
    "agent_bus_teams:  \t\n  - team: alpha   \n    port: 6167\t\n    org: example-org \t \n"
    '    description: "Spaced"  # c  \n    server_name: alpha.example.com\t\n',
)
w(
    "comment-whitespace.yml",
    "agent_bus_address: 192.0.2.10\t# tab before the hash\nagent_bus_teams:\t# tab\n"
    "  - team: alpha # nbsp\n    port: 6167　# ideographic space\n"
    "    org: example-org\t#tab comment\n    description: Words #em space\n"
    '    server_name: s.example.com\x1f# unit separator\n    humans: ["a"] # nbsp\n'
    '    path_prefixes: ["docs/"] #narrow nbsp\n',
)
w(
    "tab-in-value.yml",
    "agent_bus_address: 192.0.2.10\nagent_bus_teams:\n  - team: alpha\n    port: 6167\n"
    '    server_name: \'alpha\tbus\'\n    org: example\torg\n    path_prefixes: ["a\\tb", "c"]\n'
    '    description: "a\\tb"\n  - team: beta\n    port: 6168\n    listen: ["192.0.2.1\\t"]\n'
    '    repos: [{"repo": "x\\ty"}]\n',
)

w(
    "anchors-aliases.yml",
    "agent_bus_teams:\n  - team: alpha\n    port: 6167\n    org: &owner example-org\n"
    '    humans: &h ["owner"]\n  - team: beta\n    port: 6168\n    org: *owner\n    humans: *h\n'
    "    description: &d Anchored text\n    path_prefixes: *p\n",
)
w(
    "odd-indent.yml",
    "agent_bus_teams:\n- team: alpha\n  port: 6167\n  org: example-org\n-   team: beta\n    port: 6168\n"
    '    repos:\n        - repo: example-org/proj\n          branches: ["main"]\n',
)
w(
    "comments-inside.yml",
    "agent_bus_teams:\n  - team: alpha   # the first\n# a column-0 comment inside the list\n\n"
    "    port: 6167 # port\n    # a comment line\n    org: example#org\n"
    "    description: 'x # y'\n    server_name: \"s#n\" # c\n"
    "  - team: beta\n    port: '6168'\n    org: \"example-org\" # quoted\n"
    '    description: "hash # inside" # and a comment\n',
)
w(
    "quotes.yml",
    'agent_bus_teams:\n  - team: "alpha"\n    port: "6167"\n    state: \'present\'\n'
    "    server_name: \"alpha.example.com\"\n    org: 'example-org'\n"
    '    description: "a \\"quoted\\" word"\n  - team: \'beta\'\n    port: 6168\n'
    "    description: 'single'\n    org: \"unterminated\n  - team: gamma\n    port: 6169\n"
    "    description: \"\"\n    org: ''\n    humans: '[\"a\"]'\n",
)
w(
    "ints.yml",
    "agent_bus_teams:\n  - team: alpha\n    port: 007\n    allow_from: -0\n    humans: 12345678901234567890\n"
    "    forge_api: 1.5\n    removed_humans: "
    + "9"
    * 4300
    + "\n  - team: beta\n    port: \u0661\u0662\n"
    "    allow_from: -" + "1" * 4300 + "\n",
)
w(
    "int-too-long.yml",
    "agent_bus_teams:\n  - team: alpha\n    port: 6167\n    allow_from: "
    + "9" * 4301
    + "\n",
)
w(
    "json-specials.yml",
    "agent_bus_teams:\n  - team: alpha\n    port: 6167\n"
    "    allow_from: [NaN, Infinity, -Infinity, 1e400, -0.0, 1E5, 1e16, 1e-5, 0.0001, 123456789012345678901234567890]\n"
    '    humans: ["\\u00e9", "\\ud83d\\ude00", "\\u2028", "\\\\", "\\/", "\\b\\f\\n\\r\\t", "\\u0001"]\n'
    '    exclude: {"a": 1, "a": 2, "b": {"c": [true, false, null]}}\n'
    "    removed_humans: [ 1 , 2 ]\n    server_name: caf\u00e9.example.com\n"
    '    description: "caf\\u00e9 \\ud83d\\ude00"\n',
)
w(
    "json-bad.yml",
    "agent_bus_teams:\n  - team: alpha\n    port: 6167\n    org: example-org\n    exclude: [legacy]\n"
    '    allow_from: ["a", ]\n    humans: {"a": 1}x\n    path_prefixes: ["docs #x"]\n'
    '    description: ["not", "text"]\n    removed_humans: ["\\ud800"]\n'
    '  - team: beta\n    port: 6168\n    org: example-org\n    exclude: "legacy-and-proj"\n'
    '  - team: gamma\n    port: 6169\n    org: example-org\n    exclude: {"proj": 1}\n',
)
w(
    "json-deep.yml",
    "agent_bus_teams:\n  - team: alpha\n    port: 6167\n    allow_from: "
    + "[" * 60
    + "]" * 60
    + "\n",
)

w(
    "descriptions.yml",
    "agent_bus_teams:\n"
    "  - team: alpha\n    port: 6167\n    description: Plain words\n"
    '  - team: beta\n    port: 6168\n    description: "' + "e" * 121 + '"\n'
    '  - team: gamma\n    port: 6169\n    description: " padded "\n'
    '  - team: delta\n    port: 6170\n    description: "tab\\there"\n'
    "  - team: eps\n    port: 6171\n    description: "
    + VAULT
    + "  - team: zeta\n    port: 6172\n    description: -\n"
    '  - team: eta\n    port: 6173\n    description: "-"\n'
    '  - team: theta\n    port: 6174\n    description: "zero\\u200bwidth"\n'
    "  - team: iota\n    port: 6175\n    description: 42\n"
    "  - team: kappa\n    port: 6176\n    description: true\n"
    '  - team: lambda\n    port: 6177\n    description: "\\u00e9'
    + "\\u00e9"
    * 119
    + '"\n'
    "  - team: mu\n    port: 6178\n    description:\n      - a\n"
    "  - team: nu\n    port: 6179\n    description:\n",
)
w(
    "description-surrogate.yml",
    'agent_bus_teams:\n  - team: alpha\n    port: 6167\n    description: "bad \\ud800 half"\n',
)
w(
    "listen.yml",
    "agent_bus_address: 2001:DB8:0:0:0:0:0:10\nagent_bus_teams:\n"
    '  - team: alpha\n    port: 6167\n    listen: ["2001:db8::5", "192.0.2.5"]\n'
    "  - team: beta\n    port: 6168\n    listen: '[\"192.0.2.6\"]'\n"
    '  - team: gamma\n    port: 6169\n    listen: [""]\n'
    "  - team: delta\n    port: 6170\n    listen: [1]\n"
    '  - team: eps\n    port: 6171\n    listen: "192.0.2.7"\n'
    "  - team: zeta\n    port: 6172\n    listen: "
    + VAULT
    + "  - team: eta\n    port: 6173\n    listen: [broken\n"
    '  - team: theta\n    listen: ["192.0.2.8"]\n'
    '  - team: iota\n    port: 6174\n    listen: ["not an address"]\n',
)
w(
    "listen-surrogate.yml",
    'agent_bus_teams:\n  - team: alpha\n    port: 6167\n    listen: ["\\udbff"]\n',
)
w(
    "address-scope.yml",
    'agent_bus_address: "fe80::1%eth0"\nagent_bus_teams:\n  - team: alpha\n    port: 6167\n',
)
w(
    "address-invalid.yml",
    "agent_bus_address: 192.0.2.256\nagent_bus_teams:\n  - team: alpha\n    port: 6167\n",
)
w("address-leading-zero.yml", "agent_bus_address: 192.0.2.01\n")
w(
    "address-vault.yml",
    "agent_bus_address: "
    + VAULT
    + "agent_bus_teams:\n  - team: alpha\n    port: 6167\n",
)
w(
    "address-indented.yml",
    "  agent_bus_address: 192.0.2.1\nagent_bus_teams:\n  - team: alpha\n    port: 6167\n",
)
w(
    "address-v6-compress.yml",
    "agent_bus_address: 2001:0db8:0000:0000:0001:0000:0000:0001 # c\n",
)

w(
    "repos-forms.yml",
    "agent_bus_teams:\n"
    '  - team: alpha\n    port: 6167\n    repos: example-org/proj\n    path_prefixes: ["x"]\n'
    '  - team: beta\n    port: 6168\n    repos: {"repo": "example-org/proj"}\n'
    '  - team: gamma\n    port: 6169\n    repos: [{"branches": []}, "example-org/proj", {"repo": "example-org/proj"}]\n'
    '  - team: delta\n    port: 6170\n    repos: []\n    org: ""\n',
)
w(
    "repos-int.yml",
    'agent_bus_teams:\n  - team: alpha\n    port: 6167\n    repos: 5\n    path_prefixes: ["x"]\n',
)
w(
    "repos-true.yml",
    "agent_bus_teams:\n  - team: alpha\n    port: 6167\n    description: d\n    repos: true\n",
)
w(
    "repos-nonstring.yml",
    'agent_bus_teams:\n  - team: alpha\n    port: 6167\n    repos: [{"repo": 5}, {"repo": "example-org/proj"}]\n',
)
w(
    "exclude-int.yml",
    "agent_bus_teams:\n  - team: alpha\n    port: 6167\n    org: example-org\n    exclude: 7\n",
)
w(
    "exclude-forms.yml",
    "agent_bus_teams:\n"
    "  - team: alpha\n    port: 6167\n    org: example-org\n    exclude: legacy\n"
    '  - team: beta\n    port: 6168\n    org: example-org\n    exclude: ["proj", 1]\n'
    "  - team: gamma\n    port: 6169\n    org: example-org\n    exclude: []\n"
    "  - team: delta\n    port: 6170\n    org: other-org\n    exclude: 7\n"
    "  - team: eps\n    port: 6171\n    org: example-org\n    exclude: "
    + VAULT
    + "  - team: zeta\n    port: 6172\n    org: example-org\n    exclude:\n      - legacy\n      - tool\n",
)
w(
    "org-forms.yml",
    "agent_bus_teams:\n"
    "  - team: alpha\n    port: 6167\n    org: true\n"
    "  - team: beta\n    port: 6168\n    org: 0\n"
    "  - team: gamma\n    port: 6169\n    org: Example-Org\n"
    '  - team: delta\n    port: 6170\n    org: ["example-org"]\n'
    "  - team: eps\n    port: 6171\n    org: [example-org\n"
    "  - team: zeta\n    port: 6172\n    org: "
    + VAULT
    + '  - team: eta\n    port: 6173\n    org: example-org\n    repos: [{"repo": "example-org/proj"}]\n'
    '  - team: theta\n    port: 6174\n    org: "example-org"\n',
)
w(
    "prefixes-forms.yml",
    "agent_bus_teams:\n"
    "  - team: alpha\n    port: 6167\n    path_prefixes: docs\n"
    '  - team: beta\n    port: 6168\n    path_prefixes: {"docs/": 1, "src/": 2}\n'
    "  - team: gamma\n    port: 6169\n    path_prefixes: []\n"
    '  - team: delta\n    port: 6170\n    path_prefixes: [1, true, null, 1.5, ["a"], {"k": "v"}]\n'
    "  - team: eps\n    state: absent\n    path_prefixes: 5\n"
    '  - team: zeta\n    port: 6171\n    path_prefixes: ""\n'
    "  - team: eta\n    port: 6172\n    path_prefixes:\n      - docs/\n      - 'src/'\n",
)
w(
    "prefixes-int.yml",
    "agent_bus_teams:\n  - team: alpha\n    port: 6167\n    path_prefixes: 5\n",
)
w(
    "prefixes-vault.yml",
    "agent_bus_teams:\n  - team: alpha\n    port: 6167\n    path_prefixes: " + VAULT,
)
w(
    "prefixes-scope-vault.yml",
    'agent_bus_teams:\n  - team: alpha\n    port: 6167\n    path_prefixes: ["docs/"]\n    org: '
    + VAULT,
)
w(
    "block-lists.yml",
    "agent_bus_teams:\n  - team: alpha\n    port: 6167\n    humans:\n      - owner\n      - 'helper' # c\n"
    '      # skipped\n\n      - "third"\n    allow_from:\n      - 192.0.2.0/24\n      - 7\n      - true\n'
    '      - ["nested"]\n    repos:\n      - repo: example-org/proj\n        branches: ["main", "dev"]\n'
    '      - repo: "example-org/legacy"\n      - repo: other-org/tool\n        branches:   [] # none\n'
    "    exclude:\n    - a\n    - b\n",
)
w(
    "block-dup-key.yml",
    "agent_bus_teams:\n  - team: alpha\n    port: 6167\n    repos:\n      - repo: example-org/proj\n"
    "        repo: example-org/other\n",
)
w(
    "block-bad-indent.yml",
    "agent_bus_teams:\n  - team: alpha\n    port: 6167\n    humans:\n      - a\n     - b\n",
)
w(
    "block-scalar-continuation.yml",
    "agent_bus_teams:\n  - team: alpha\n    port: 6167\n    org: example-org\n    repos:\n      - example-org/proj\n"
    "        more: text\n",
)
w(
    "block-mapping-no-value.yml",
    "agent_bus_teams:\n  - team: alpha\n    port: 6167\n    repos:\n      - repo:\n      - repo: example-org/proj\n",
)
w(
    "block-flow-error.yml",
    "agent_bus_teams:\n  - team: alpha\n    port: 6167\n    repos:\n      - repo: x\n      - [broken\n",
)
w(
    "value-and-nested.yml",
    "agent_bus_teams:\n  - team: alpha\n    port: 6167\n    org: example-org\n      nested: line\n",
)
w(
    "no-value.yml",
    "agent_bus_teams:\n  - team: alpha\n    port: 6167\n    org:\n    description: Fine\n  - team: beta\n    port: 6168\n    org: # only a comment\n",
)
w(
    "multiline-scalar.yml",
    "agent_bus_teams:\n  - team: alpha\n    port: 6167\n    description: >\n      folded text\n    org: |\n      literal\n",
)
w(
    "states.yml",
    "agent_bus_teams:\n  - team: alpha\n    port: 6167\n    state: absent\n    org: example-org\n"
    "  - team: beta\n    state: present\n    org: example-org\n  - team: gamma\n    port: 6169\n    org: example-org\n"
    '  - team: old\n    port: 6170\n    state: "absent"\n',
)
w(
    "teams-for-many.yml",
    "agent_bus_teams:\n"
    '  - team: alpha\n    port: 6167\n    org: example-org\n    description: "Org wide"\n'
    '  - team: beta\n    port: 6168\n    org: example-org\n    exclude: ["legacy", "proj"]\n'
    '  - team: gamma\n    port: 6169\n    repos: [{"repo": "example-org/legacy"}, {"repo": "other-org/tool"}]\n'
    "  - team: delta\n    port: 6170\n    org: other-org\n    repos:\n      - repo: example-org/proj\n"
    "  - team: eps\n    port: 6171\n    org: other-org\n    exclude: tool\n"
    "  - team: old\n    port: 6172\n    state: absent\n    org: example-org\n",
)
w("merge-key.yml", "agent_bus_teams:\n  - team: alpha\n    port: 6167\n    <<: *base\n")
w(
    "second-list-first.yml",
    "agent_bus_element_teams:\n  - team: beta\n    base_url: http://192.0.2.10:6168\n"
    "agent_bus_teams:\n  - team: beta\n    port: 6168\nagent_bus_lxc_members:\n",
)
w(
    "element-forms.yml",
    "agent_bus_element_teams:\n"
    '  - team: alpha\n    base_url: "http://[2001:db8::1]:6167"\n'
    "  - team: beta\n    base_url: http://192.0.2.10:6168 # c\n"
    "  - team: gamma\n"
    '  - team: delta\n    base_url: ["a", 1, true, null, 1.5]\n'
    "  - team: eps\n    base_url: 6170\n"
    "  - team: zeta\n    base_url: false\n"
    '  - team: eta\n    base_url: {"k": "v\\u00e9"}\n'
    "  - team: theta\n    port: x1\n",
)
w(
    "element-vault.yml",
    "agent_bus_element_teams:\n  - team: alpha\n    base_url: " + VAULT,
)
w(
    "element-surrogate.yml",
    'agent_bus_element_teams:\n  - team: alpha\n    base_url: "http://\\ud800"\n',
)
w(
    "element-dup.yml",
    "agent_bus_element_teams:\n  - team: alpha\n    base_url: x\n  - team: alpha\n    base_url: y\n",
)
w(
    "lxc-forms.yml",
    "agent_bus_teams:\n  - team: alpha\n    port: 6167\nagent_bus_lxc_members:\n"
    "  - team: alpha\n    container: web-1\n    user: agent\n    checkout: /srv/agent/proj\n    host: Work Laptop\n"
    "    seat: 2\n    role: orchestrator\n    service: true\n"
    "  - team: alpha\n    container: web-2\n    user: agent\n    repo: Proj.Name\n    state: absent\n"
    '  - team: beta\n    container: web-1\n    user: agent\n    checkout: "/srv/agent/proj"\n    human_text: false\n'
    '    service: true\n    seat: worker\n    host: "\\u0130x"\n'
    "  - team: alpha\n    container: Web.3_x\n    user: _a-b\n    checkout: /srv/x\n    role: worker\n    port: 1\n",
)
w(
    "next-steps.yml",
    'agent_bus_teams:\n  - team: alpha\n    port: 6167\n    org: example-org\n    repos: [{"repo": "other-org/tool"}]\n'
    '  - team: beta\n    port: 6168\n  - team: gamma\n    port: 6169\n    org: ["a"]\n'
    "  - team: old\n    org: example-org\n",
)

# The whole file refused: every query gives the same refusal, so few are asked.
REFUSED = {
    "indented-key.yml": "  agent_bus_teams:\n  - team: alpha\n",
    "key-in-trailing-comment.yml": "user_login: x # agent_bus_teams\n",
    "key-suffix.yml": "agent_bus_teams_extra: 1\n",
    "key-twice.yml": "agent_bus_teams:\n  - team: a\nagent_bus_teams :\n",
    "address-twice.yml": "agent_bus_address: 192.0.2.1\nagent_bus_address: 192.0.2.2\nagent_bus_teams:\n  - team: X\n",
    "inline-list.yml": 'agent_bus_teams: [{"team": "alpha"}]\n',
    "vault-list.yml": "agent_bus_teams: " + VAULT,
    "tab-indent.yml": "agent_bus_teams:\n  - team: alpha\n\tport: 6167\n",
    "tab-blank-line.yml": "agent_bus_teams:\n  - team: alpha\n \t\nother: 1\n",
    "items-not-alike.yml": "agent_bus_teams:\n  - team: alpha\n  -   team: beta\n",
    "not-shape.yml": "agent_bus_teams:\n  team: alpha\n",
    "not-shape-deep.yml": "agent_bus_teams:\n  - team: alpha\n port: 1\n",
    "item-not-mapping.yml": "agent_bus_teams:\n  - [alpha]\n",
    "item-dash-only.yml": "agent_bus_teams:\n  -\n",
    "key-no-space.yml": "agent_bus_teams:\n  - team:alpha\n",
    "key-twice-in-item.yml": "agent_bus_teams:\n  - team: alpha\n    port: 1\n    port: 2\n",
    "no-team.yml": "agent_bus_teams:\n  - team: alpha\n  - port: 6168\n    org: x\n",
    "team-twice.yml": "agent_bus_teams:\n  - team: alpha\n  - team: 'alpha'\n",
    "bad-team-name.yml": "agent_bus_teams:\n  - team: Alpha\n",
    "long-team-name.yml": "agent_bus_teams:\n  - team: " + "a" * 25 + "\n",
    "port-not-number.yml": "agent_bus_teams:\n  - team: alpha\n    port: 61x67\n",
    "port-negative.yml": "agent_bus_teams:\n  - team: alpha\n    port: -1\n",
    "port-superscript.yml": "agent_bus_teams:\n  - team: alpha\n    port: 6\u00b2\n",
    "port-circled.yml": "agent_bus_teams:\n  - team: alpha\n    port: \u2460\n",
    "port-vault.yml": "agent_bus_teams:\n  - team: alpha\n    port: " + VAULT,
    "state-bad.yml": "agent_bus_teams:\n  - team: alpha\n    state: gone\n",
    "state-vault.yml": "agent_bus_teams:\n  - team: alpha\n    state: " + VAULT,
    "team-vault.yml": "agent_bus_teams:\n  - team: " + VAULT,
    "server-name-vault.yml": "agent_bus_teams:\n  - team: alpha\n    server_name: "
    + VAULT,
    "bom-before-key.yml": "\ufeffagent_bus_teams:\n  - team: alpha\n",
    "errors-in-order.yml": "agent_bus_teams:\n  - team: Bad\n  - port: 1\n  - team: ok\n -x\n",
    "team-before-port.yml": "agent_bus_teams:\n  - team: alpha\n    port: x\n  - team: alpha\n",
    "latin1.yml": b"agent_bus_teams:\n  - team: caf\xe9\n",
    "encoded-surrogate.yml": b"agent_bus_teams:\n  - team: \xed\xa0\x80\n",
}
for name, text in REFUSED.items():
    w("refused/" + name, text)

HEAD = "agent_bus_teams:\n  - team: alpha\n    port: 6167\nagent_bus_lxc_members:\n"
M = "  - team: alpha\n    container: c\n    user: u\n"
MC = M + "    checkout: /x\n"
LXC = {
    "missing-keys.yml": "  - team: alpha\n    port: 1\n",
    "unknown-keys.yml": MC + "    zed: 1\n    listen: []\n",
    "state-quoted-bad.yml": MC + "    state: '\"absent\"'\n",
    "state-json-list.yml": MC + '    state: ["absent"]\n',
    "team-json.yml": '  - team: "alpha"\n    container: c\n    user: u\n    checkout: /x\n',
    "container-bad.yml": "  - team: alpha\n    container: -c\n    user: u\n",
    "container-int.yml": "  - team: alpha\n    container: 5\n    user: u\n",
    "user-bad.yml": "  - team: alpha\n    container: c\n    user: U\n",
    "host-int.yml": MC + "    host: 5\n",
    "host-empty.yml": MC + '    host: ""\n',
    "host-bad.yml": MC + "    host: -bad-\n",
    "checkout-relative.yml": M + "    checkout: x\n",
    "checkout-list.yml": M + '    checkout: ["/x"]\n',
    "repo-int.yml": MC + "    repo: 5\n",
    "repo-empty.yml": MC + "    repo: '__'\n",
    "repo-long.yml": M + "    state: absent\n    repo: " + "R" * 60 + "\n",
    "present-no-checkout.yml": M,
    "absent-no-place.yml": M + "    state: absent\n",
    "seat-zero.yml": MC + "    seat: 0\n",
    "seat-bool.yml": MC + "    seat: true\n",
    "seat-big.yml": MC + "    seat: 1234567\n",
    "seat-name.yml": MC + "    seat: abc123\n",
    "role-bad.yml": MC + "    role: boss\n",
    "role-int.yml": MC + "    role: 1\n",
    "human-text-string.yml": MC + '    human_text: "no"\n',
    "service-int.yml": MC + "    service: 1\n",
    "role-flow-error.yml": MC + "    role: [x\n",
    "value-vault.yml": MC + "    host: " + VAULT,
    "user-twice.yml": MC + "    user: v\n",
    "entries-twice.yml": MC + M + "    checkout: /y\n",
    "two-hosts.yml": MC
    + "    host: a\n"
    + M.replace("alpha", "beta")
    + "    checkout: /x\n    host: b\n",
    "seat-clash.yml": MC
    + "    host: a\n    seat: 1\n"
    + M.replace("c\n", "d\n", 1)
    + "    checkout: /x\n    host: a\n    seat: 1\n",
    "seat-two-repos.yml": MC
    + "    host: a\n    seat: 1\n    repo: one\n"
    + M.replace("c\n", "d\n", 1)
    + "    checkout: /x\n    host: a\n    seat: 1\n    repo: two\n",
    "two-coordinators.yml": MC
    + "    role: coordinator\n"
    + M.replace("c\n", "d\n", 1)
    + "    checkout: /x\n    role: orchestrator\n",
    "absent-coordinators.yml": MC
    + "    role: coordinator\n    state: absent\n"
    + M.replace("c\n", "d\n", 1)
    + "    checkout: /x\n    role: coordinator\n",
    "service-differs.yml": MC
    + "    service: true\n"
    + M.replace("alpha", "beta")
    + "    checkout: /x\n",
    "service-checkouts.yml": MC
    + "    service: true\n"
    + M.replace("alpha", "beta")
    + "    checkout: /y\n    service: true\n",
    "checkout-surrogate.yml": M + '    checkout: "/x\\ud800"\n',
    "lxc-state-bad.yml": MC + "    state: gone\n",
    "lxc-bad-team.yml": "  - team: Bad\n    container: c\n    user: u\n",
}
for name, text in LXC.items():
    w("lxc/" + name, HEAD + text)
