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

import re
import pprint
import urllib.request
import json
from packaging import version
from subprocess import check_output

min_supported_major = 13
os_name = "trixie"
image_type = "system"

pg_repo_name = "cloudnative-pg/postgresql"
pg_version_re = r"(\d+)(?:\.\d+|beta\d+|rc\d+|alpha\d+)(-\d{12})?"
pg_versions_file = ".github/pg_versions.json"


def get_json(repo_name):
    data = check_output(
        [
            "docker",
            "run",
            "--rm",
            "quay.io/skopeo/stable",
            "list-tags",
            "docker://ghcr.io/{}".format(repo_name),
        ]
    )
    repo_json = json.loads(data.decode("utf-8"))
    return repo_json


def parse_version(v):
    return version.Version(v.removesuffix(f"-{image_type}-{os_name}"))


def is_pre_release(v):
    return parse_version(v).is_prerelease


def write_json(repo_url, version_re, output_file):
    repo_json = get_json(repo_url)
    tags = repo_json["Tags"]

    version_re = re.compile(rf"^{version_re}-{image_type}-{os_name}$")

    # Filter out all the tags which do not match the version regexp
    tags = [item for item in tags if version_re.search(item)]

    # Sort the tags according to semantic versioning
    tags.sort(key=parse_version, reverse=True)

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

        # We normally want to handle only versions without the timestamp inside
        extra = match.group(2)
        if not extra:
            if major not in results:
                results[major] = [item]
            elif len(results[major]) < 2:
                results[major].append(item)
        # But we keep the highest version with the timestamp in case we have not enough other versions
        else:
            if major not in extra_results:
                extra_results[major] = item

    # If there are not enough versions without the timestamp inside, we add the one we kept
    for major in results:
        if len(results[major]) < 2:
            results[major].append(extra_results[major])
        # You cannot update between pre-release versions. If one of the two values is a pre-release
        # make sure to update between two different names of the most recent version (it might be a release)
        elif is_pre_release(results[major][0]) or is_pre_release(results[major][1]):
            results[major] = [results[major][0], extra_results[major]]

    with open(output_file, "w") as json_file:
        json.dump(results, json_file, indent=2)


if __name__ == "__main__":
    # PostgreSQL JSON file generator with Versions like x.y
    write_json(pg_repo_name, pg_version_re, pg_versions_file)
