#
# This file is part of Cloud Native PostgreSQL.
#
# Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
#

import json
import sys

POSTGRES_REPO = "quay.io/enterprisedb/postgresql"

# Kubernetes versions to use during the tests
K8S_VERSIONS = [
    "v1.19.1",
    "v1.18.8",
    "v1.17.11",
    "v1.16.9",
    "v1.15.12",
]

# PostgreSQL versions to use during the tests
# MAJOR: [VERSION, PRE_ROLLING_UPDATE_VERSION]
POSTGRES_VERSION_LISTS = {
    "13": ["13.1", "13.0"],
    "12": ["12.5", "12.4"],
    "11": ["11.9", "11.8"],
    "10": ["10.15", "10.14"],
}


def build_job(k8s_version, postgres_version_list):
    """Build a single job of the matrix"""
    postgres_version = postgres_version_list[0]
    postgres_version_pre = postgres_version_list[1]

    name = f"{k8s_version}-PostgreSQL-{postgres_version}"
    repo = POSTGRES_REPO

    print(f"Generating: {name}", file=sys.stderr)

    return {
        "id": name,
        "k8s_version": k8s_version,
        "postgres_version": postgres_version,
        "postgres_img": f"{repo}:{postgres_version}",
        "postgres_pre_img": f"{repo}:{postgres_version_pre}",
    }


def build_include():
    """Build include Job list"""
    include = []

    # Sorted keys
    postgres_versions = list(sorted(POSTGRES_VERSION_LISTS.keys(), reverse=True))

    # Default versions
    default_postgres_version = postgres_versions[0]
    default_k8s_version = K8S_VERSIONS[0]

    # Iterate over K8S versions
    for k8s_version in K8S_VERSIONS:
        include.append(
            build_job(
                k8s_version,
                POSTGRES_VERSION_LISTS[default_postgres_version],
            )
        )

    # Iterate over PostgreSQL versions except the first one
    for postgres_version in postgres_versions:
        include.append(
            build_job(
                default_k8s_version,
                POSTGRES_VERSION_LISTS[postgres_version],
            )
        )

    return include


print("::set-output name=matrix::" + json.dumps({"include": build_include()}))
