#
# This file is part of Cloud Native PostgreSQL.
#
# Copyright (C) 2019-2021 EnterpriseDB Corporation.
#

import argparse
import json
import re
import sys
from operator import itemgetter
from typing import Dict, List

POSTGRES_REPO = "quay.io/enterprisedb/postgresql"
AKS_VERSIONS_FILE = ".github/aks_versions.json"
EKS_VERSIONS_FILE = ".github/eks_versions.json"
GKE_VERSIONS_FILE = ".github/gke_versions.json"
RKE_VERSIONS_FILE = ".github/rke_versions.json"
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
        return self.get(self.versions[0])

    @property
    def oldest(self):
        return self.get(self.versions[-1])


# Kubernetes versions to use during the tests
K8S = VersionList(
    [
        "v1.22.2",
        "v1.21.2",
        "v1.20.7",
        "v1.19.11",
        "v1.18.19",
    ]
)

# Kubernetes versions on EKS to use during the tests
with open(EKS_VERSIONS_FILE) as json_file:
    eks_versions = json.load(json_file)
EKS_K8S = VersionList(eks_versions)

# Kubernetes versions on AKS to use during the tests
with open(AKS_VERSIONS_FILE) as json_file:
    aks_versions = json.load(json_file)
AKS_K8S = VersionList(aks_versions)

# Kubernetes versions on GKE to use during the tests
with open(GKE_VERSIONS_FILE) as json_file:
    gke_versions = json.load(json_file)
GKE_K8S = VersionList(gke_versions)

# Kubernetes versions on RKE to use during the tests
with open(RKE_VERSIONS_FILE) as json_file:
    rke_versions = json.load(json_file)
RKE_K8S = VersionList(rke_versions)

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
        "main": build_main_include_local,
        "schedule": build_schedule_include_local,
    },
    "eks": {
        "push": lambda: build_push_include_cloud(EKS_K8S),
        "pull_request": lambda: build_pull_request_include_cloud(EKS_K8S),
        "main": lambda: build_main_include_cloud(EKS_K8S),
        "schedule": lambda: build_schedule_include_cloud(EKS_K8S),
    },
    "aks": {
        "push": lambda: build_push_include_cloud(AKS_K8S),
        "pull_request": lambda: build_pull_request_include_cloud(AKS_K8S),
        "main": lambda: build_main_include_cloud(AKS_K8S),
        "schedule": lambda: build_schedule_include_cloud(AKS_K8S),
    },
    "gke": {
        "push": lambda: build_push_include_cloud(GKE_K8S),
        "pull_request": lambda: build_pull_request_include_cloud(GKE_K8S),
        "main": lambda: build_main_include_cloud(GKE_K8S),
        "schedule": lambda: build_schedule_include_cloud(GKE_K8S),
    },
    "rke": {
        "push": lambda: build_push_include_cloud(RKE_K8S),
        "pull_request": lambda: build_pull_request_include_cloud(RKE_K8S),
        "main": lambda: build_main_include_cloud(RKE_K8S),
        "schedule": lambda: build_schedule_include_cloud(RKE_K8S),
    },
}


if __name__ == "__main__":

    parser = argparse.ArgumentParser(description="Create the job matrix")
    parser.add_argument(
        "-m",
        "--mode",
        type=str,
        choices={"push", "pull_request", "main", "schedule"},
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
    else:
        # Do not run `gke` and `rke` by default
        engines = engines - {"gke", "rke"}

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
        print(f"::set-output name={engine}Matrix::" + json.dumps({"include": include}))
        print(f"::set-output name={engine}Enabled::" + str(len(include) > 0))
