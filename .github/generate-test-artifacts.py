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


def convert_ginkgo_test(t, matrix):
    """converts a test spec in ginkgo JSON format into a normalized JSON object.
    The matrix arg will be passed from the GH Actions, and is expected to be
    a JSON of the form:
    {
        "runner": , # eg. local, aks
        "id": , # the matrix ID eg. local-v1.22.2-PostgreSQL-13.5
        "postgres": , # version of PostgreSQL eg. 13.5
        "kubernetes": , # version of K8s eg. v1.22.2
        "runid": , # the GH Action run-id -> ${{ github.run_id }}
        "repo": , # cloudnative-pg/cloudnative-pg -> you get this from GH with ${{github.repository}}
        "branch": , # dev/cnpg-1666 -> you get this with "${{github.head_ref}}" ... EXCEPT
        "refname": , # depending on how the job was triggered, the above may be blank, and then we want: "${{github.ref_name}}"
    }
    """
    err = ""
    errFile = ""
    errLine = 0
    if "Failure" in t:
        err = t["Failure"]["Message"]
        errFile = t["Failure"]["Location"]["FileName"]
        errLine = t["Failure"]["Location"]["LineNumber"]

    state = t["State"]
    # if the test failed but it had an Ignore label, mark it as ignoreFailed
    # so it doesn't count as FAILED but we can still see how much it's failing
    if (
        state == "failed"
        and "ContainerHierarchyLabels" in t
        and "ignore-fails" in flatten(t["ContainerHierarchyLabels"])
    ):
        state = "ignoreFailed"

    branch = matrix["branch"]
    if branch == "":
        branch = matrix["refname"]

    x = {
        "name": " - ".join(t["ContainerHierarchyTexts"]) + " -- " + t["LeafNodeText"],
        "state": state,
        "start_time": t["StartTime"],
        "end_time": t[
            "EndTime"
        ],  # NOTE: Grafana will need a default timestamp field. This is a good candidate
        "error": err,
        "error_file": errFile,
        "error_line": errLine,
        "platform": matrix["runner"],
        "postgres_kind": matrix["postgres_kind"],
        "matrix_id": matrix["id"],
        "postgres_version": matrix["postgres"],
        "k8s_version": matrix["kubernetes"],
        "workflow_id": matrix["runid"],
        "repo": matrix["repo"],
        "branch": branch,
    }
    return x


def write_artifact(artifact, matrix):
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
    whitespace = re.compile("\s")
    slug = whitespace.sub("_", artifact["name"])
    h = hashlib.sha224(slug.encode("utf-8")).hexdigest()
    filename = matrix["id"] + "_" + h + ".json"
    if dir != "":
        filename = dir + "/" + filename
    with open(filename, "w") as f:
        f.write(json.dumps(artifact))


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

    args = parser.parse_args()

    print("test matrix: ")
    print(args.matrix)
    if args.matrix:
        matrix = json.loads(args.matrix)

    dir = ""
    if args.outdir:
        dir = args.outdir
        if not os.path.exists(dir):
            os.makedirs(dir)
            print("Directory ", dir, " Created ")

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
        write_artifact(artifact, matrix)
        exit(0)

    # MAIN LOOP: go over each `SpecReport` in the Ginkgo JSON output, convert
    # each to the normalized JSON format and create a JSON file for each of those
    try:
        with open(args.file) as json_file:
            testResults = json.load(json_file)
            for t in testResults[0]["SpecReports"]:
                if (t["State"] != "skipped") and (t["LeafNodeText"] != ""):
                    test1 = convert_ginkgo_test(t, matrix)
                    write_artifact(test1, matrix)
    except Exception as e:
        # Reflect any unexpected failure in an artifact
        artifact = create_artifact(
            matrix, "Generate artifacts from Ginkgo report", "failed", f"{e}"
        )
        write_artifact(artifact, matrix)
