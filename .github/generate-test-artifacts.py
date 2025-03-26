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

import argparse
import json
import re
import os
import hashlib
from datetime import *


def flatten(arr):
    """flatten an array of arrays"""
    out = []
    for l in arr:
        if isinstance(l, list):
            for item in l:
                out.append(item)
        else:
            print("unexpected hierarchy labels")
            print(arr)
    return out


def env_to_json():
    """Convert a set of environment variables into a valid JSON with the following format:
    {
        "runner": , # e.g. local, aks, eks, gke
        "id": , # the matrix ID e.g. local-v1.22.2-PostgreSQL-13.5
        "postgres": , # version of PostgreSQL e.g. 13.5
        "postgres_kind": , # flavor of PostgreSQL
        "kubernetes": , # version of K8s e.g. v1.22.2
        "runid": , # the GH Action run-id -> ${{ github.run_id }}
        "branch": , # dev/xxxx-1666 -> you get this with "${{ github.head_ref }}" ... EXCEPT
        "refname": , # it may be blank, and then we want: "${{ github.ref_name }}"
        "repo": , # cloudnative-pg/cloudnative-pg -> you get this from GH with ${{ github.repository }}
    }
    """

    runner = os.getenv("RUNNER")
    postgres = os.getenv("POSTGRES_VERSION")
    postgres_kind = os.getenv("POSTGRES_KIND")
    kubernetes_version = os.getenv("K8S_VERSION")
    runid = os.getenv("RUN_ID")
    id = os.getenv("MATRIX")
    repo = os.getenv("REPOSITORY")
    branch = os.getenv("BRANCH_NAME")
    refname = os.getenv("GIT_REF")

    matrix = f"""
    {{
    "runner": "{runner}",
    "postgres": "{postgres}",
    "postgres_kind": "{postgres_kind}",
    "kubernetes": "{kubernetes_version}",
    "runid": "{runid}",
    "id": "{id}",
    "repo": "{repo}",
    "branch": "{branch}",
    "refname": "{refname}"
    }}
    """
    return matrix


def is_user_spec(spec):
    """Checks if the spec contains the fields used to build the test name.
    The JSON report produced by Ginkgo may contain
    SpecReports entries that are for internal Ginkgo purposes and will not
    reflect user-defined Specs. For these entries, ContainerHierarchyTexts may
    be null or the LeafNodeText may be blank
    """
    if spec["LeafNodeText"] == "":
        return False

    try:
        _ = " - ".join(spec["ContainerHierarchyTexts"])
        return True
    except TypeError:
        return False


def convert_ginkgo_test(test, matrix):
    """Converts a test spec in ginkgo JSON format into a normalized JSON object.
    The matrix arg will be passed from the GH Actions, and is expected to be
    a JSON of the form:
    {
        "runner": , # e.g. local, aks, eks, gke
        "id": , # the matrix ID e.g. local-v1.22.2-PostgreSQL-13.5
        "postgres": , # version of PostgreSQL e.g. 13.5
        "postgres_kind": , # flavor of PostgreSQL
        "kubernetes": , # version of K8s e.g. v1.22.2
        "runid": , # the GH Action run-id -> ${{ github.run_id }}
        "branch": , # dev/xxxx-1666 -> you get this with "${{ github.head_ref }}" ... EXCEPT
        "refname": , # it may be blank, and then we want: "${{ github.ref_name }}"
        "repo": , # cloudnative-pg/cloudnative-pg -> you get this from GH with ${{ github.repository }}
    }
    """
    err = ""
    err_file = ""
    err_line = 0
    if "Failure" in test:
        err = test["Failure"]["Message"]
        err_file = test["Failure"]["Location"]["FileName"]
        err_line = test["Failure"]["Location"]["LineNumber"]

    state = test["State"]

    branch = matrix["branch"]
    if branch == "":
        branch = matrix["refname"]

    ginkgo_format = {
        "name": " - ".join(test["ContainerHierarchyTexts"])
        + " -- "
        + test["LeafNodeText"],
        "state": state,
        "start_time": test["StartTime"],
        "end_time": test[
            "EndTime"
        ],  # NOTE: Grafana will need a default timestamp field. This is a good candidate
        "error": err,
        "error_file": err_file,
        "error_line": err_line,
        "platform": matrix["runner"],
        "postgres_kind": matrix["postgres_kind"],
        "matrix_id": matrix["id"],
        "postgres_version": matrix["postgres"],
        "k8s_version": matrix["kubernetes"],
        "workflow_id": matrix["runid"],
        "repo": matrix["repo"],
        "branch": branch,
    }
    return ginkgo_format


def write_artifact(artifact, artifact_dir, matrix):
    """writes an artifact to local storage as a JSON file

    The computed filename will be used as the ID to introduce the payload into
    Elastic for the E2E Test . Should be unique across the current GH run.
    So: MatrixID + Test
    Because we may run this on MSFT Azure, where filename length limits still
    exist, we HASH the test name.
    The platform team's scraping script will add the GH Run ID to this, and the
    Repository, and with Repo + Run ID + MatrixID + Test Hash, gives a unique
    ID in Elastic to each object.
    """
    whitespace = re.compile(r"\s")
    slug = whitespace.sub("_", artifact["name"])
    h = hashlib.sha224(slug.encode("utf-8")).hexdigest()
    filename = matrix["id"] + "_" + h + ".json"
    if artifact_dir != "":
        filename = artifact_dir + "/" + filename
    try:
        with open(filename, "w") as f:
            f.write(json.dumps(artifact))
    except (FileNotFoundError, PermissionError) as e:
        print(f"Error: {e}")


def create_artifact(matrix, name, state, error):
    """creates an artifact with a given name, state and error,
    with the metadata provided by the `matrix` argument.
    Useful to generate artifacts that signal failures outside the Test Suite,
    for example if the suite never executed
    """
    branch = matrix["branch"]
    if branch == "":
        branch = matrix["refname"]

    return {
        "name": name,
        "state": state,
        "start_time": datetime.now().isoformat(),
        "end_time": datetime.now().isoformat(),  # NOTE: Grafana will need a default timestamp field. This is a good candidate
        "error": error,
        "error_file": "no-file",
        "error_line": 0,
        "platform": matrix["runner"],
        "matrix_id": matrix["id"],
        "postgres_kind": matrix["postgres_kind"],
        "postgres_version": matrix["postgres"],
        "k8s_version": matrix["kubernetes"],
        "workflow_id": matrix["runid"],
        "repo": matrix["repo"],
        "branch": branch,
    }


if __name__ == "__main__":
    parser = argparse.ArgumentParser(
        description="Create JSON artifacts from E2E JSON report"
    )
    parser.add_argument(
        "-f",
        "--file",
        type=str,
        help="report JSON file with test run, as produce by ginkgo",
    )
    parser.add_argument(
        "-o",
        "--outdir",
        type=str,
        default="",
        help="directory where we write the artifacts",
    )
    parser.add_argument(
        "-m", "--matrix", type=str, help="the matrix with GH execution variables"
    )
    parser.add_argument(
        "-e",
        "--environment",
        type=bool,
        help="get the matrix arguments from environment variables. "
        "Variables defined with -m/--matrix take priority",
    )

    args = parser.parse_args()

    print("test matrix: ")
    matrix = {}
    # First, try to gather the matrix from env variables
    if args.environment:
        matrix = json.loads(env_to_json())
    # If defined, user provided arguments will take priority
    if args.matrix:
        args_matrix = json.loads(args.matrix)
        matrix.update(args_matrix)

    print(matrix)

    outputDir = ""
    if args.outdir:
        outputDir = args.outdir
        if not os.path.exists(outputDir):
            os.makedirs(outputDir)
            print("Directory ", outputDir, " Created ")

    # If the ginkgo report file is not found, produce a "failed" artifact
    if not os.path.exists(args.file):
        print("Report ", args.file, " not found ")
        # we still want to get an entry in the E2E Dashboard for workflows that even
        # failed to run the ginkgo suite or failed to produce a JSON report.
        # We create a custom Artifact with a `failed` status for the Dashboard
        artifact = create_artifact(
            matrix,
            "Open Ginkgo report",
            "failed",
            "ginkgo Report Not Found: " + args.file,
        )
        write_artifact(artifact, outputDir, matrix)
        exit(0)

    # MAIN LOOP: go over each `SpecReport` in the Ginkgo JSON output, convert
    # each to the normalized JSON format and create a JSON file for each of those
    try:
        with open(args.file) as json_file:
            testResults = json.load(json_file)
            for t in testResults[0]["SpecReports"]:
                if (t["State"] != "skipped") and is_user_spec(t):
                    test1 = convert_ginkgo_test(t, matrix)
                    write_artifact(test1, outputDir, matrix)
    except Exception as e:
        # Reflect any unexpected failure in an artifact
        artifact = create_artifact(
            matrix, "Generate artifacts from Ginkgo report", "failed", f"{e}"
        )
        write_artifact(artifact, outputDir, matrix)
