#
# Copyright The CloudNativePG Contributors
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

import argparse
import json
import os
import re
import sys
from operator import itemgetter
from typing import Dict, List

POSTGRES_REPO = "ghcr.io/cloudnative-pg/postgresql"
PG_VERSIONS_FILE = ".github/pg_versions.json"


class VersionList(list):
    """List of versions"""

    def __init__(self, versions: List[str]):
        super().__init__(versions)

    @property
    def latest(self):
        return self[0]

    @property
    def oldest(self):
        return self[-1]


class MajorVersionList(dict):
    """List of major versions, with multiple patch levels"""

    def __init__(self, version_lists: Dict[str, List[str]]):
        sorted_versions = {
            k: VersionList(version_lists[k]) for k in version_lists.keys()
        }
        super().__init__(sorted_versions)
        self.versions = list(self.keys())

    @property
    def latest(self):
        if "beta" in self[self.versions[0]][0]:
            return self.get(self.versions[1])
        return self.get(self.versions[0])

    @property
    def oldest(self):
        return self.get(self.versions[-1])


# Kubernetes versions to use during the tests
# Based on the images available at https://hub.docker.com/r/kindest/node/tags
# and the supported releases https://kubernetes.io/releases/
K8S = VersionList(
    [
        "v1.25.0",
        "v1.24.4",
        "v1.23.10",
        "v1.22.13",
    ]
)

# PostgreSQL versions to use during the tests
# Entries are expected to be ordered from newest to oldest
# First entry is used as default testing version
# Entries format:
# MAJOR: [VERSION, PRE_ROLLING_UPDATE_VERSION],

with open(PG_VERSIONS_FILE, "r") as json_file:
    postgres_versions = json.load(json_file)
POSTGRES = MajorVersionList(postgres_versions)


class E2EJob(dict):
    """Build a single job of the matrix"""

    def __init__(self, k8s_version, postgres_version_list):
        postgres_version = postgres_version_list.latest
        postgres_version_pre = postgres_version_list.oldest

        name = f"{k8s_version}-PostgreSQL-{postgres_version}"
        repo = POSTGRES_REPO

        super().__init__(
            {
                "id": name,
                "k8s_version": k8s_version,
                "postgres_version": postgres_version,
                "postgres_img": f"{repo}:{postgres_version}",
                "postgres_pre_img": f"{repo}:{postgres_version_pre}",
            }
        )

    def __hash__(self):
        return hash(self["id"])


def build_push_include_local():
    """Build the list of tests running on push"""
    return {
        E2EJob(K8S.latest, POSTGRES.latest),
        E2EJob(K8S.oldest, POSTGRES.oldest),
    }


def build_pull_request_target_include_local():
    result = build_push_include_local()
    # Iterate over K8S versions
    for k8s_version in K8S:
        result |= {
            E2EJob(k8s_version, POSTGRES.latest),
        }

    # Iterate over PostgreSQL versions
    for postgres_version in POSTGRES.values():
        result |= {E2EJob(K8S.latest, postgres_version)}

    return result


def build_pull_request_include_local():
    """Build the list of tests running on pull request"""
    result = build_push_include_local()

    # Iterate over K8S versions
    for k8s_version in K8S:
        result |= {
            E2EJob(k8s_version, POSTGRES.latest),
        }

    # Iterate over PostgreSQL versions
    for postgres_version in POSTGRES.values():
        result |= {E2EJob(K8S.latest, postgres_version)}

    return result


def build_main_include_local():
    """Build the list tests running on main"""
    result = build_pull_request_include_local()

    # Iterate over K8S versions
    for k8s_version in K8S:
        result |= {
            E2EJob(k8s_version, POSTGRES.latest),
        }

    # Iterate over PostgreSQL versions
    for postgres_version in POSTGRES.values():
        result |= {E2EJob(K8S.latest, postgres_version)}

    return result


def build_schedule_include_local():
    """Build the list of tests running on schedule"""
    # For the moment scheduled tests are identical to main
    return build_main_include_local()


def build_push_include_cloud(engine_version_list):
    return {}


def build_pull_request_include_cloud(engine_version_list):
    return {}


def build_main_include_cloud(engine_version_list):
    return {
        E2EJob(engine_version_list.latest, POSTGRES.latest),
    }


def build_schedule_include_cloud(engine_version_list):
    """Build the list of tests running on schedule"""
    result = set()
    # Iterate over K8S versions
    for k8s_version in engine_version_list:
        result |= {
            E2EJob(k8s_version, POSTGRES.latest),
        }

    # Iterate over PostgreSQL versions
    for postgres_version in POSTGRES.values():
        result |= {E2EJob(engine_version_list.latest, postgres_version)}

    return result


ENGINE_MODES = {
    "local": {
        "push": build_push_include_local,
        "pull_request": build_pull_request_include_local,
        "pull_request_target": build_pull_request_target_include_local,
        "main": build_main_include_local,
        "schedule": build_schedule_include_local,
    },
}


if __name__ == "__main__":

    parser = argparse.ArgumentParser(description="Create the job matrix")
    parser.add_argument(
        "-m",
        "--mode",
        type=str,
        choices={"push", "pull_request", "main", "schedule", "pull_request_target"},
        default="push",
        help="set of tests to run",
    )
    parser.add_argument(
        "-l",
        "--limit",
        type=str,
        default="",
        help="limit to a list of engines",
    )
    args = parser.parse_args()

    engines = set(ENGINE_MODES.keys())

    if args.limit:
        required_engines = set(re.split(r"[, ]+", args.limit.strip()))
        if len(wrong_engines := required_engines - engines):
            raise SystemExit(
                f"Limit contains unknown engines {wrong_engines}. Available engines: {engines}"
            )
        engines = required_engines

    matrix = {}
    for engine in ENGINE_MODES:
        include = {}
        if engine in engines:
            include = list(
                sorted(ENGINE_MODES[engine][args.mode](), key=itemgetter("id"))
            )
        for job in include:
            job["id"] = engine + "-" + job["id"]
            print(f"Generating {engine}: {job['id']}", file=sys.stderr)
        with open(os.getenv("GITHUB_OUTPUT"), 'a') as env:
            print(f"{engine}Matrix=" + json.dumps({"include": include}), file=env)
            print(f"{engine}Enabled=" + str(len(include) > 0), file=env)
