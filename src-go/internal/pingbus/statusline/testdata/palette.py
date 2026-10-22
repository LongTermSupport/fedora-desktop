"""Print the Python palette and auto colours as JSON, for colour_test.go (SPEC-team-colours C6).

Usage: python3 -I palette.py <checkout> <team> [<team> ...]

Prints {"palette": [[name, hex], ...], "auto": {team: hex}, "sgr": {team: escape}} from
helpers/pingbus/colours.py in <checkout>.
"""

import json
import sys


def main(checkout: str, teams: list[str]) -> None:
    sys.path.insert(0, checkout)
    from helpers.pingbus import colours

    auto = {team: colours.auto_colour(team) for team in teams}
    print(json.dumps({
        "palette": [list(entry) for entry in colours.PALETTE],
        "backgrounds": list(colours.DARK_BACKGROUNDS),
        "auto": auto,
        "sgr": {team: colours.sgr(hex_colour) for team, hex_colour in auto.items()},
    }))


if __name__ == "__main__":
    main(sys.argv[1], sys.argv[2:])
