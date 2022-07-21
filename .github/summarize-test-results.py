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

"""Creates a summary of all the "strategy matrix" branches in GH actions that
are running the E2E Test Suite.

Each test execution in each "matrix branch" in GH is uploading a JSON artifact
with the test results.
The test artifacts are normalized to avoid some of ginkgo's idiosyncrasies.

This is the JSON format of the test artifacts:

{
        "name": "my test",
        "state": "passed",
        "start_time": "timestamp",
        "end_time": "timestamp",
        "error": error,
        "error_file": errFile,
        "error_line": errLine,
        "platform": e.g. local / gke / aks…,
        "postgres_kind": postgresql / epas,
        "matrix_id": GH actions "matrix branch" id,
        "postgres_version": semver,
        "k8s_version": semver,
        "workflow_id": GH actions workflow id,
        "repo": git repo,
        "branch": git branch,
}

In a final GH action, after all the matrix branches running the E2E Test Suite
are finished, all the artifacts are downloaded to a local folder.

The code in this file iterates over all the collected JSON artifacts to produce
a summary in Markdown, which can then be rendered in GitHub using
[GitHub Job Summaries](https://github.blog/2022-05-09-supercharging-github-actions-with-job-summaries/)
"""

import argparse
import json
import os

def is_failed(e2e_test):
    """checks if the test failed. In ginkgo, the passing states are well defined
    but ginkgo 1 -> 2 added new failure kinds. So, check for non-pass    
    """
    return (e2e_test["state"] != "passed" and e2e_test["state"] != "skipped"
        and e2e_test["state"] != "ignoreFailed")

def compute_test_summary(test_dir):
    """iterate over the JSON artifact files in `test_dir`, and
    bucket them for comprehension.
    
    Produces a dictionary of dictionaries:

    {
        "total_run": 0,
        "total_failed": 0,
        "unique_run": 0,
        "unique_failed": 0,
        "failed_by_test": {},
        "total_by_test": {},
        "failed_by_matrix": {},
        "total_by_matrix": {},
        "failed_k8s_by_test": {},
        "failed_pg_by_test": {},
    }
    """
    total_runs = 0
    total_fails = 0
    unique_test_run = {}
    unique_test_failed = {}
    total_by_test = {}
    fails_by_test = {}
    failed_k8s_by_test = {}
    failed_pg_by_test = {}
    total_by_matrix = {}
    failed_by_matrix = {}

    dir_listing = os.listdir(test_dir)
    for f in dir_listing:
        path = os.path.join(test_dir, f)
        with open(path) as json_file:
            test_results = json.load(json_file)
            name = test_results["name"]
            if name not in total_by_test:
                total_by_test[name] = 0
            unique_test_run[name] = True
            total_runs = 1 + total_runs
            total_by_test[name] = 1 + total_by_test[name]

            if is_failed(test_results):
                if name not in fails_by_test:
                    fails_by_test[name] = 0
                if name not in failed_k8s_by_test:
                    failed_k8s_by_test[name] = []
                if name not in failed_pg_by_test:
                    failed_pg_by_test[name] = []

                unique_test_failed[name] = True
                fails_by_test[name] = 1 + fails_by_test[name]
                total_fails = 1 + total_fails
                failed_k8s_by_test[name].append(test_results["k8s_version"])
                failed_pg_by_test[name].append(test_results["postgres_version"])

            matrix = test_results["matrix_id"]
            if matrix not in total_by_matrix:
                total_by_matrix[matrix] = 0

            total_by_matrix[matrix] = 1 + total_by_matrix[matrix]
            if is_failed(test_results):
                if matrix not in failed_by_matrix:
                    failed_by_matrix[matrix] = 0
                failed_by_matrix[matrix] = 1 + failed_by_matrix[matrix]

    unique_failed = 0
    unique_run = 0
    for name in unique_test_failed:
        unique_failed = 1 + unique_failed
    for name in unique_test_run:
        unique_run = 1 + unique_run

    return {
        "total_run": total_runs,
        "total_failed": total_fails,
        "unique_run": unique_run,
        "unique_failed": unique_failed,
        "failed_by_test": fails_by_test,
        "total_by_test": total_by_test,
        "failed_by_matrix": failed_by_matrix,
        "total_by_matrix": total_by_matrix,
        "failed_k8s_by_test": failed_k8s_by_test,
        "failed_pg_by_test": failed_pg_by_test,
    }

def format_overview(summary, structure):
    """print unbucketed test metrics
    """
    print("## " + structure["title"])
    print()
    print("|" + " | ".join(structure["header"]) + "|")
    print("|" + "|".join(["---"] * len(structure["header"])) + "|")
    print("| {failed} | {total} | {name} |".format(
        name = structure["row1"][0],
        failed = summary[structure["row1"][1]],
        total = summary[structure["row1"][2]]))
    print("| {failed} | {total} | {name} |".format(
        name = structure["row2"][0],
        failed = summary[structure["row2"][1]],
        total = summary[structure["row2"][2]]))
    print()

def format_by_matrix(summary, structure):
    """print metrics bucketed by matrix branch
    """
    print("## " + structure["title"])
    print()
    print("|" + " | ".join(structure["header"]) + "|")
    print("|" + "|".join(["---"] * len(structure["header"])) + "|")
    sorted_by_fail = dict(sorted(summary["failed_by_matrix"].items(),
        key=lambda item: item[1], reverse=True))

    for bucket in sorted_by_fail:
        print("| {failed} | {total} | {name} |".format(
            name = bucket,
            failed = summary["failed_by_matrix"][bucket],
            total = summary["total_by_matrix"][bucket]))
    print()

def format_by_test(summary, structure):
    """print metrics bucketed by test class
    """
    print("## " + structure["title"])
    print()
    print("|" + " | ".join(structure["header"]) + "|")
    print("|" + "|".join(["---"] * len(structure["header"])) + "|")
    sorted_by_fail = dict(sorted(summary["failed_by_test"].items(),
        key=lambda item: item[1], reverse=True))

    for bucket in sorted_by_fail:
        print("| {failed} | {total} | {failed_k8s} | {failed_pg} | {name} |".format(
            name = bucket,
            failed = summary["failed_by_test"][bucket],
            total = summary["total_by_test"][bucket],
            failed_k8s = ", ".join(summary["failed_k8s_by_test"][bucket]),
            failed_pg = ", ".join(summary["failed_pg_by_test"][bucket])))
    print()

def format_test_summary(summary):
    """creates a Markdown document with several tables rendering test results.
    Outputs to stdout like a good 12-factor-app citizen
    """

    print("Note that there are three tables below: overview, bucketed " +
        "by test, bucketed by matrix branch.")
    print()

    overview_section = {
        "title": "Overview",
        "header": ["failed", "total", ""],
        "row1": [ "test combinations", "total_failed", "total_run"],
        "row2": [ "unique tests", "unique_failed", "unique_run"],
    }

    format_overview(summary, overview_section)

    if summary["total_failed"] == 0:
        print("No failures, no more stats shown. It's not easy being green.")
        return

    by_test_section = {
        "title": "Failures by test",
        "header": ["failed", "total", "failed K8s", "failed PG", "test"],
    }

    format_by_test(summary, by_test_section)

    by_matrix_section = {
        "title": "Failures by matrix branch",
        "header": ["failed", "total", "matrix branch"],
    }

    format_by_matrix(summary, by_matrix_section)

if __name__ == "__main__":

    parser = argparse.ArgumentParser(
        description="Summarize the E2E Suite results"
    )
    parser.add_argument(
        "-d",
        "--dir",
        type=str,
        help="folder with the JSON artifacts",
    )

    args = parser.parse_args()

    summary = compute_test_summary(args.dir)
    format_test_summary(summary)
