#
# This file is part of Cloud Native PostgreSQL.
#
# Copyright (C) 2019-2022 EnterpriseDB Corporation.
#

import re
import urllib.request
import json

min_supported_major = 10

pg_repo_url = "https://quay.io/api/v1/repository/enterprisedb/postgresql"
pg_version_re = re.compile(r"^(\d+)(?:\.\d+)(-\d+)?$")
pg_versions_file = ".github/pg_versions.json"


def get_json(repo_url):
    req = urllib.request.Request(repo_url)
    data = urllib.request.urlopen(req).read()
    repo_json = json.loads(data.decode("utf-8"))
    return repo_json


def version_sort_key(version):
    """
    This function works by returning an int array containing the version parts.
    It returns an empty array if it is a non numeric version
    """
    try:
        return [int(u) for u in re.split(r"[.-]", version)]
    except ValueError:
        return []


def write_json(repo_url, version_re, output_file):
    repo_json = get_json(repo_url)

    tags = list(repo_json["tags"].keys())
    tags.sort(key=version_sort_key, reverse=True)

    results = {}
    extra_results = {}
    for item in tags:
        match = version_re.search(item)
        if not match:
            continue

        major = match.group(1)

        # Skip too old versions
        if int(major) < min_supported_major:
            continue

        # We normally want to handle only versions without the '-' inside
        extra = match.group(2)
        if not extra:
            if major not in results:
                results[major] = [item]
            elif len(results[major]) < 2:
                results[major].append(item)
        # But we keep the highest version with the '-' in case we have not enough other versions
        else:
            if major not in extra_results:
                extra_results[major] = item

    # If there are not enough version without '-` inside we add the one we kept
    for major in results:
        if len(results[major]) < 2:
            results[major].append(extra_results[major])

    with open(output_file, "w") as json_file:
        json.dump(results, json_file, indent=2)


if __name__ == "__main__":
    # PostgreSQL JSON file generator with Versions like x.y
    write_json(pg_repo_url, pg_version_re, pg_versions_file)
