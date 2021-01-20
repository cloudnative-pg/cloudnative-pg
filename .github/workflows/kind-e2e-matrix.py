#
# This file is part of Cloud Native PostgreSQL.
#
# Copyright (C) 2019-2021 EnterpriseDB Corporation.
#

import json
import os
import sys
from operator import itemgetter
from typing import Dict, List

POSTGRES_REPO = "quay.io/enterprisedb/postgresql"


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
            k: VersionList(version_lists[k])
            for k in version_lists.keys()
        }
        super().__init__(sorted_versions)
        self.versions = list(self.keys())

    @property
    def latest(self):
        return self.get(self.versions[0])

    @property
    def oldest(self):
        return self.get(self.versions[-1])


# Kubernetes versions to use during the tests
K8S = VersionList(
    [
        "v1.20.0",
        "v1.19.4",
        "v1.18.8",
        "v1.17.11",
        "v1.16.15",
    ]
)

# PostgreSQL versions to use during the tests
# Entries are expected to be ordered from newest to oldest
# First entry is used as default testing version
# Entries format:
# MAJOR: [VERSION, PRE_ROLLING_UPDATE_VERSION],
POSTGRES = MajorVersionList(
    {
        # We cannot use PostgreSQL 13 as default testing version due to the bug
        # https://postgr.es/m/20201209.174314.282492377848029776.horikyota.ntt%40gmail.com
        # TODO: Reorder the versions when the bug will be fixed
        "12": ["12.5", "12.4"],
        "13": ["13.1", "13.0"],
        "11": ["11.9", "11.8"],
        "10": ["10.15", "10.14"],
    }
)


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


def build_push_include():
    """Build the list of tests running on push"""
    return {
        E2EJob(K8S.latest, POSTGRES.latest),
        E2EJob(K8S.oldest, POSTGRES.oldest),
    }


def build_pull_request_include():
    """Build the list of tests running on pull request"""
    result = build_push_include()

    # Iterate over K8S versions
    for k8s_version in K8S:
        result |= {
            E2EJob(k8s_version, POSTGRES.latest),
        }

    # Iterate over PostgreSQL versions
    for postgres_version in POSTGRES.values():
        print(postgres_version)
        result |= {E2EJob(K8S.latest, postgres_version)}

    return result


def build_main_include():
    """Build the list tests running on main"""
    result = build_pull_request_include()

    # Iterate over K8S versions
    for k8s_version in K8S:
        result |= {
            E2EJob(k8s_version, POSTGRES.latest),
        }

    # Iterate over PostgreSQL versions
    for postgres_version in POSTGRES.values():
        result |= {E2EJob(K8S.latest, postgres_version)}

    return result


def build_schedule_include():
    """Build the list of tests running on schedule"""
    # For the moment scheduled tests are identical to main
    return build_main_include()


MODES = {
    "push": build_push_include,
    "pull_request": build_pull_request_include,
    "main": build_main_include,
    "schedule": build_schedule_include,
}


if __name__ == "__main__":
    mode = os.getenv("E2E_DEPTH", "push")

    if mode not in MODES:
        raise SystemExit(
            f"GITHUB_EVENT_NAME='{mode}' is not supported. Possible values are: {', '.join(MODES)}"
        )

    include = list(sorted(MODES[mode](), key=itemgetter("id")))
    for job in include:
        print(f"Generating: {job['id']}", file=sys.stderr)

    print("::set-output name=matrix::" + json.dumps({"include": include}))
