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

from jinja2 import Template

"""
{
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
        "postgres_kind": kind,
        "matrix_id": matrix["id"],
        "postgres_version": matrix["postgres"],
        "k8s_version": matrix["kubernetes"],
        "workflow_id": matrix["runid"],
        "repo": matrix["repo"],
        "branch": branch,
}
"""

def is_failed(e2e_test):
    return e2e_test["state"] != "passed" and e2e_test["state"] != "skipped"

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

    total_by_test = {}
    fails_by_test = {}
    total_runs = 0
    total_fails = 0
    failed_k8s_by_test = {}
    failed_pg_by_test = {}
    total_by_matrix = {}
    failed_by_matrix = {}

    dir_listing = os.listdir(args.dir)
    for f in dir_listing:
        path = os.path.join(args.dir, f)
        with open(path) as json_file:
            testResults = json.load(json_file)
            name = testResults["name"]
            if name not in total_by_test:
                total_by_test[name] = 0
            if name not in fails_by_test:
                fails_by_test[name] = 0
            if name not in failed_k8s_by_test:
                failed_k8s_by_test[name] = []
            if name not in failed_pg_by_test:
                failed_pg_by_test[name] = []

            total_runs = 1 + total_runs
            total_by_test[name] = 1 + total_by_test[name]
            if is_failed(testResults):
                fails_by_test[name] = 1 + fails_by_test[name]
                total_fails = 1 + total_fails
                failed_k8s_by_test[name].append(testResults["k8s_version"])
                failed_pg_by_test[name].append(testResults["postgres_version"])

            matrix = testResults["matrix_id"]
            if matrix not in total_by_matrix:
                total_by_matrix[matrix] = 0
            if matrix not in failed_by_matrix:
                failed_by_matrix[matrix] = 0

            total_by_matrix[matrix] = 1 + total_by_matrix[matrix]
            if is_failed(testResults):
                failed_by_matrix[matrix] = 1 + failed_by_matrix[matrix]

    summary = {
        "total_run": total_runs,
        "total_failed": total_fails,
        "failed_by_test": fails_by_test,
        "total_by_test": total_by_test,
        "failed_by_matrix": failed_by_matrix,
        "total_by_matrix": total_by_matrix,
        "failed_k8s_by_test": failed_k8s_by_test,
        "failed_pg_by_test": failed_pg_by_test,
    }


    tpl = """E2E Test summary

Total test combinations failed: {{ summary.total_failed }} out of {{ summary.total_run }} run.

## Failures by matrix branch

| matrix branch | failed | runs |
|------|------|-------|
{%- for matrix in summary.total_by_matrix %}
| {{ matrix }} | {{ summary.failed_by_matrix[matrix] }} | {{ summary.total_by_matrix[matrix] }} |
{%- endfor %}

## Failures by test

| fails | runs | failed K8s | failed PG | test |
|------|------|-------|---------|-------|
{%- for t in summary.total_by_test %}
| {{ summary.failed_by_test[t] }} | {{ summary.total_by_test[t] }} | {{ summary.failed_k8s_by_test[t] }} | {{ summary.failed_pg_by_test[t] }} | {{ t }} |
{%- endfor %}"""

    out = Template(tpl)
    print(out.render(summary=summary))
