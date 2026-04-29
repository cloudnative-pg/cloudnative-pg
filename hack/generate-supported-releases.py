#!/usr/bin/env python3
# Copyright © contributors to CloudNativePG, established as
# CloudNativePG a Series of LF Projects, LLC.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
#
# SPDX-License-Identifier: Apache-2.0
"""Regenerate the release tables in docs/src/supported_releases.md from docs/releases.json.

Run from the repo root:
    python3 hack/generate-supported-releases.py
"""

import json
import re
import sys
from datetime import date
from pathlib import Path

REPO_ROOT = Path(__file__).parent.parent
DATA_FILE = REPO_ROOT / "docs" / "releases.json"
OUTPUT_FILE = REPO_ROOT / "docs" / "src" / "supported_releases.md"

MONTHS = ["Jan", "Feb", "Mar", "Apr", "May", "Jun",
          "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"]


def parse_date(s):
    return date.fromisoformat(s) if s else None


def fmt_date(d, approximate=False):
    if d is None:
        return ""
    if approximate:
        return f"~ {MONTHS[d.month - 1]} {d.year}"
    return f"{MONTHS[d.month - 1]} {d.day}, {d.year}"


def minor_key(r):
    """Sort key: parse '1.29' → (1, 29); 'main' sorts lowest."""
    m = r["minor"]
    if m == "main":
        return (0, 0)
    major, minor = m.split(".")
    return (int(major), int(minor))


def status(r, today):
    if r.get("development"):
        return "development"
    rd = parse_date(r.get("release_date"))
    eol = parse_date(r.get("end_of_life"))
    if rd is None or rd > today:
        return "upcoming"
    if eol is None or eol > today:
        return "active"
    return "eol"


def fmt_k8s(versions):
    return ", ".join(versions) if versions else ""


def fmt_pg(r):
    pg = r.get("postgres")
    if not pg:
        return ""
    return f"{pg['min']} - {pg['max']}"


def active_table(releases, today):
    rows = sorted(
        [r for r in releases if status(r, today) in ("active", "development")],
        key=minor_key,
        reverse=True,
    )
    # development (main) always last
    rows.sort(key=lambda r: 1 if r.get("development") else 0)

    lines = [
        "| Version | Currently supported | Release date | End of life | Supported Kubernetes versions | Tested, but not supported | Supported Postgres versions |",
        "|---------|---------------------|--------------|-------------|-------------------------------|---------------------------|-----------------------------|",
    ]
    for r in rows:
        if r.get("development"):
            lines.append(
                f"| {r['minor']} | No, development only |  |  |  |  | {fmt_pg(r)} |"
            )
            continue
        k8s = r.get("kubernetes", {})
        lines.append(
            f"| {r['minor']}.x"
            f" | Yes"
            f" | {fmt_date(parse_date(r.get('release_date')), r.get('release_date_approximate', False))}"
            f" | {fmt_date(parse_date(r.get('end_of_life')), r.get('end_of_life_approximate', False))}"
            f" | {fmt_k8s(k8s.get('supported', []))}"
            f" | {fmt_k8s(k8s.get('tested', []))}"
            f" | {fmt_pg(r)}"
            f" |"
        )
    return "\n".join(lines)


def upcoming_table(releases, today):
    rows = sorted(
        [r for r in releases if status(r, today) == "upcoming"],
        key=minor_key,
    )
    lines = [
        "| Version | Release date | End of life |",
        "|---------|--------------|-------------|",
    ]
    for r in rows:
        lines.append(
            f"| {r['minor']}.0"
            f" | {fmt_date(parse_date(r.get('release_date')), r.get('release_date_approximate', True))}"
            f" | {fmt_date(parse_date(r.get('end_of_life')), r.get('end_of_life_approximate', True))}"
            f" |"
        )
    return "\n".join(lines)


def old_table(releases, today):
    rows = sorted(
        [r for r in releases if status(r, today) == "eol"],
        key=minor_key,
        reverse=True,
    )
    lines = [
        "| Version | Release date | End of life | Supported Kubernetes versions |",
        "|---------|--------------|-------------|-------------------------------|",
    ]
    for r in rows:
        k8s = r.get("kubernetes", {})
        lines.append(
            f"| {r['minor']}.x"
            f" | {fmt_date(parse_date(r.get('release_date')), r.get('release_date_approximate', False))}"
            f" | {fmt_date(parse_date(r.get('end_of_life')), r.get('end_of_life_approximate', False))}"
            f" | {fmt_k8s(k8s.get('supported', []))}"
            f" |"
        )
    return "\n".join(lines)


def replace_section(content, marker, new_table):
    begin = f"<!-- BEGIN {marker} -->"
    end = f"<!-- END {marker} -->"
    pattern = re.compile(
        re.escape(begin) + r".*?" + re.escape(end),
        re.DOTALL,
    )
    replacement = f"{begin}\n{new_table}\n{end}"
    updated, count = pattern.subn(replacement, content)
    if count == 0:
        print(f"ERROR: marker {marker!r} not found in {OUTPUT_FILE}", file=sys.stderr)
        sys.exit(1)
    return updated


def main():
    today = date.today()

    with open(DATA_FILE) as f:
        data = json.load(f)

    releases = data["releases"]

    content = OUTPUT_FILE.read_text()
    content = replace_section(content, "ACTIVE_RELEASES_TABLE", active_table(releases, today))
    content = replace_section(content, "UPCOMING_RELEASES_TABLE", upcoming_table(releases, today))
    content = replace_section(content, "OLD_RELEASES_TABLE", old_table(releases, today))
    OUTPUT_FILE.write_text(content)

    print(f"Updated {OUTPUT_FILE}")


if __name__ == "__main__":
    main()
