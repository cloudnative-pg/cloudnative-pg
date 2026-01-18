#!/usr/bin/env python3
#
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
#

"""
Fetch supported OpenShift Container Platform 4.x versions from Red Hat API.

This script queries the Red Hat Product Life Cycles API to retrieve all
OpenShift versions that are still under active support (Full Support,
Maintenance Support, Extended Update Support Term 1 or Term 2).

Output: Human-readable table showing OpenShift supported releases and their
lifecycle information including current status and expiration dates for each
support phase.

Options:
  -o FILE  Write list of supported versions as JSON array to specified file
"""

import argparse
import json
import re
import sys
from datetime import datetime
from urllib.request import Request, urlopen


# ANSI color codes
class Colors:
    """ANSI color codes for terminal output."""

    RESET = "\033[0m"
    BOLD = "\033[1m"
    DIM = "\033[2m"

    # Status colors
    GREEN = "\033[32m"
    YELLOW = "\033[33m"
    CYAN = "\033[36m"
    GRAY = "\033[90m"


# Support phase names (in priority order)
PHASE_FULL_SUPPORT = "Full support"
PHASE_MAINTENANCE = "Maintenance support"
PHASE_EUS_TERM1 = "Extended update support"
PHASE_EUS_TERM2 = "Extended update support Term 2"
PHASE_GA = "General availability"

SUPPORT_PHASES = [
    PHASE_FULL_SUPPORT,
    PHASE_MAINTENANCE,
    PHASE_EUS_TERM1,
    PHASE_EUS_TERM2,
]

# Phase display names
PHASE_ABBREVIATIONS = {
    PHASE_FULL_SUPPORT: "Full",
    PHASE_MAINTENANCE: "Maintenance",
    PHASE_EUS_TERM1: "EUS",
    PHASE_EUS_TERM2: "EUS",
}

# Status colors mapping
STATUS_COLORS = {
    PHASE_FULL_SUPPORT: Colors.CYAN + Colors.BOLD,
    PHASE_MAINTENANCE: Colors.GREEN + Colors.BOLD,
    PHASE_EUS_TERM1: Colors.YELLOW + Colors.BOLD,
    PHASE_EUS_TERM2: Colors.YELLOW + Colors.BOLD,
}


def colorize(text, color, use_color=True):
    """Wrap text with ANSI color codes."""
    if not use_color:
        return text
    return f"{color}{text}{Colors.RESET}"


def strip_ansi(text):
    """Remove ANSI color codes from text for length calculation."""
    ansi_escape = re.compile(r"\033\[[0-9;]+m")
    return ansi_escape.sub("", text)


def get_status_display(phase_name, use_color=True):
    """Get colored status display text for a phase."""
    status_text = PHASE_ABBREVIATIONS.get(phase_name, phase_name)
    color = STATUS_COLORS.get(phase_name, Colors.CYAN + Colors.BOLD)
    return colorize(status_text, color, use_color)


def get_combined_eus_date(phases):
    """Get the latest EUS end date (combining Term 1 and Term 2)."""
    eus_term2 = phases.get(PHASE_EUS_TERM2)
    if eus_term2:
        return eus_term2
    return phases.get(PHASE_EUS_TERM1)


def format_date_cell(
    date_val, today, is_current_phase, has_string_date=False, use_color=True
):
    """Format a date cell with appropriate indicators and colors."""
    if date_val is None and not has_string_date:
        return colorize("  N/A", Colors.GRAY, use_color)
    elif date_val is None and has_string_date:
        # Phase with TBD end date (e.g., "GA of 4.21 + 3 Months")
        if is_current_phase:
            arrow = colorize("→", Colors.GREEN + Colors.BOLD, use_color)
            tbd = colorize("TBD", Colors.CYAN, use_color)
            return f"{arrow} {tbd}"
        else:
            return colorize("  TBD", Colors.CYAN, use_color)

    date_str = date_val.strftime("%Y-%m-%d")
    if is_current_phase:
        arrow = colorize("→", Colors.GREEN + Colors.BOLD, use_color)
        return f"{arrow} {date_str}"
    elif date_val < today:
        check = colorize("✓", Colors.GRAY + Colors.DIM, use_color)
        date_colored = colorize(date_str, Colors.GRAY + Colors.DIM, use_color)
        return f"{check} {date_colored}"
    else:
        return f"  {date_str}"


def get_openshift_versions_data():
    """
    Query Red Hat API and return detailed lifecycle data for OpenShift versions.

    The function retrieves all support phases for each version and includes it
    if any phase (Full, Maintenance, EUS Term 1, or EUS Term 2) hasn't expired.

    Returns:
        list: List of dicts containing version name and phase information,
              sorted by version in descending order
    """
    api_url = "https://access.redhat.com/product-life-cycles/api/v1/products"
    params = "?name=OpenShift+Container+Platform+4"

    try:
        # User-Agent header required to avoid 403 Forbidden
        headers = {
            "User-Agent": "CloudNativePG-CI/1.0 (openshift-versions-update; +https://github.com/cloudnative-pg/cloudnative-pg)"
        }
        req = Request(api_url + params, headers=headers)
        with urlopen(req, timeout=10) as response:
            data = json.loads(response.read())

        today = datetime.now().date()
        versions_data = []

        # Navigate API structure: data[0].versions[]
        products = data.get("data", [])
        if not products:
            print("No product data found in API response", file=sys.stderr)
            return []

        versions = products[0].get("versions", [])

        for version in versions:
            version_name = version.get("name", "")
            phases = version.get("phases", [])

            # Extract phase information
            phase_info = {}
            phase_has_string_date = {}
            is_supported = False

            for phase_name in SUPPORT_PHASES:
                phase = next((p for p in phases if p.get("name") == phase_name), None)

                if not phase:
                    phase_info[phase_name] = None
                    phase_has_string_date[phase_name] = False
                    continue

                date_str = phase.get("date")
                date_format = phase.get("date_format")

                # Handle phases with actual dates
                if date_format == "date" and date_str:
                    try:
                        # Parse ISO 8601 date (e.g., "2026-12-17T00:00:00.000Z")
                        end_date = datetime.fromisoformat(
                            date_str.replace("Z", "+00:00")
                        ).date()
                        phase_info[phase_name] = end_date
                        phase_has_string_date[phase_name] = False

                        if end_date >= today:
                            is_supported = True
                    except (ValueError, AttributeError) as e:
                        print(
                            f"Warning: Failed to parse date for {version_name} "
                            f"phase '{phase_name}': {date_str} ({e})",
                            file=sys.stderr,
                        )
                        phase_info[phase_name] = None
                        phase_has_string_date[phase_name] = False
                # Handle phases with TBD end dates (e.g., "GA of 4.21 + 3 Months")
                elif date_format == "string" and date_str and date_str != "N/A":
                    phase_info[phase_name] = None
                    phase_has_string_date[phase_name] = True
                else:
                    phase_info[phase_name] = None
                    phase_has_string_date[phase_name] = False

            # Check if Full support has a TBD date and GA is in the past
            if phase_has_string_date.get(PHASE_FULL_SUPPORT):
                ga_phase = next((p for p in phases if p.get("name") == PHASE_GA), None)
                if ga_phase:
                    ga_date_str = ga_phase.get("date")
                    ga_date_format = ga_phase.get("date_format")
                    if ga_date_format == "date" and ga_date_str:
                        try:
                            ga_date = datetime.fromisoformat(
                                ga_date_str.replace("Z", "+00:00")
                            ).date()
                            # If GA is in the past, version is in Full support
                            if ga_date <= today:
                                is_supported = True
                        except (ValueError, AttributeError):
                            pass

            if is_supported:
                versions_data.append(
                    {
                        "version": version_name,
                        "phases": phase_info,
                        "phase_has_string_date": phase_has_string_date,
                        "today": today,
                    }
                )

        # Sort by semantic version (e.g., 4.20 > 4.19 > 4.2)
        versions_data.sort(
            key=lambda v: [int(x) for x in v["version"].split(".")], reverse=True
        )

        return versions_data

    except Exception as e:
        print(f"Error fetching data from Red Hat API: {e}", file=sys.stderr)
        return []


def get_current_phase(version_data):
    """
    Determine the current support phase for a version.

    Returns:
        tuple: (phase_name, end_date) for the active phase, or (None, None)
    """
    today = version_data["today"]
    phases = version_data["phases"]
    phase_has_string_date = version_data.get("phase_has_string_date", {})

    # First pass: check if we're in a phase with a TBD end date
    # If a phase has a TBD date, we assume we're still in it unless we can
    # confirm we've moved to a later phase (by checking if later phases have expired)
    for phase_name in SUPPORT_PHASES:
        if phase_has_string_date.get(phase_name):
            # This phase has unknown end date (TBD)
            # We're in this phase if we haven't definitively moved past it
            # Check if we've entered a later phase by looking for completed phases
            current_idx = SUPPORT_PHASES.index(phase_name)
            in_later_phase = False

            # Check each subsequent phase
            for i in range(current_idx + 1, len(SUPPORT_PHASES)):
                later_phase_name = SUPPORT_PHASES[i]
                later_phase_date = phases.get(later_phase_name)

                # If a later phase exists with a date, check if we're past it
                # If we're past a later phase's END date, then we must have
                # passed through this TBD phase already
                if later_phase_date and later_phase_date < today:
                    in_later_phase = True
                    break

            # If we haven't passed any later phase, we're still in this TBD phase
            if not in_later_phase:
                return (phase_name, None)

    # Second pass: normal phases with concrete dates
    for phase_name in SUPPORT_PHASES:
        end_date = phases.get(phase_name)
        if end_date and end_date >= today:
            return (phase_name, end_date)

    return (None, None)


def get_eol_date(version_data):
    """
    Get the final EOL date for a version (last date of any support phase).

    Returns:
        date: The EOL date, or None if no phases have dates
    """
    phases = version_data["phases"]
    eol_date = None

    for phase_date in phases.values():
        if phase_date:
            if eol_date is None or phase_date > eol_date:
                eol_date = phase_date

    return eol_date


def format_table(versions_data, use_color=True):
    """
    Format version data as a human-readable table.

    Args:
        versions_data: List of version data dictionaries
        use_color: Whether to use ANSI colors in output

    Returns:
        str: Formatted table string
    """
    if not versions_data:
        return "No supported OpenShift versions found."

    # Table headers
    headers = ["Version", "Status", "Full Support", "Maintenance", "EUS", "EOL Date"]
    rows = []

    for version_data in versions_data:
        version = version_data["version"]
        today = version_data["today"]
        phases = version_data["phases"]

        current_phase, current_end = get_current_phase(version_data)

        if not current_phase:
            continue

        # Get status display with color
        status = get_status_display(current_phase, use_color)

        # Get phase end dates
        full_end = phases.get(PHASE_FULL_SUPPORT)
        maint_end = phases.get(PHASE_MAINTENANCE)
        eus_end = get_combined_eus_date(phases)
        eol_date = get_eol_date(version_data)

        # Check which phase is current
        is_full = current_phase == PHASE_FULL_SUPPORT
        is_maint = current_phase == PHASE_MAINTENANCE
        is_eus = current_phase in [PHASE_EUS_TERM1, PHASE_EUS_TERM2]

        # Check if phases have TBD dates
        phase_has_string_date = version_data.get("phase_has_string_date", {})
        full_is_tbd = phase_has_string_date.get(PHASE_FULL_SUPPORT, False)
        maint_is_tbd = phase_has_string_date.get(PHASE_MAINTENANCE, False)

        # Format EOL date
        if eol_date:
            eol_str = eol_date.strftime("%Y-%m-%d")
        else:
            eol_str = colorize("TBD", Colors.CYAN, use_color)

        rows.append(
            [
                version,
                status,
                format_date_cell(full_end, today, is_full, full_is_tbd, use_color),
                format_date_cell(maint_end, today, is_maint, maint_is_tbd, use_color),
                format_date_cell(eus_end, today, is_eus, use_color=use_color),
                eol_str,
            ]
        )

    # Calculate column widths (strip ANSI codes for accurate width)
    col_widths = [len(h) for h in headers]
    for row in rows:
        for i, cell in enumerate(row):
            col_widths[i] = max(col_widths[i], len(strip_ansi(str(cell))))

    # Format table
    separator = "+" + "+".join("-" * (w + 2) for w in col_widths) + "+"
    header_row = (
        "|" + "|".join(f" {h:<{col_widths[i]}} " for i, h in enumerate(headers)) + "|"
    )

    lines = [separator, header_row, separator]
    for row in rows:
        # Format each cell with proper padding accounting for ANSI codes
        formatted_cells = []
        for i, cell in enumerate(row):
            cell_str = str(cell)
            visible_len = len(strip_ansi(cell_str))
            padding = col_widths[i] - visible_len
            # Add padding to the right
            padded_cell = cell_str + " " * padding
            formatted_cells.append(f" {padded_cell} ")
        row_str = "|" + "|".join(formatted_cells) + "|"
        lines.append(row_str)
    lines.append(separator)

    # Add legend with colors
    lines.append("")
    legend_parts = [
        "Legend:",
        colorize("→", Colors.GREEN + Colors.BOLD, use_color) + " Current phase",
        colorize("✓", Colors.GRAY + Colors.DIM, use_color) + " Completed",
        colorize("TBD", Colors.CYAN, use_color) + " To be determined",
    ]
    lines.append("  ".join(legend_parts))

    return "\n".join(lines)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(
        description="Fetch supported OpenShift versions from Red Hat API"
    )
    parser.add_argument(
        "-o",
        "--output",
        metavar="FILE",
        help="Write JSON array of supported versions to specified file",
    )
    parser.add_argument(
        "--no-color",
        action="store_true",
        help="Disable colored output",
    )
    args = parser.parse_args()

    # Auto-detect color support: disable if stdout is not a TTY or if explicitly disabled
    use_color = not args.no_color and sys.stdout.isatty()

    versions_data = get_openshift_versions_data()

    if not versions_data:
        sys.exit(1)

    # Print human-readable table to stdout
    print(format_table(versions_data, use_color=use_color))

    # Optionally write JSON to file
    if args.output:
        version_list = [v["version"] for v in versions_data]
        try:
            with open(args.output, "w") as f:
                json.dump(version_list, f, indent=2)
                f.write("\n")
            print(f"\nJSON written to {args.output}", file=sys.stderr)
        except Exception as e:
            print(f"Error writing to {args.output}: {e}", file=sys.stderr)
            sys.exit(1)
