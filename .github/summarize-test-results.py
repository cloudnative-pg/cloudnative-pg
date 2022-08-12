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
are finished, all the artifacts are downloaded to a local directory.

The code in this file iterates over all the collected JSON artifacts to produce
a summary in Markdown, which can then be rendered in GitHub using
[GitHub Job Summaries](https://github.blog/2022-05-09-supercharging-github-actions-with-job-summaries/)
"""

import argparse
import json
import os
from datetime import datetime

def is_failed(e2e_test):
    """checks if the test failed. In ginkgo, the passing states are well defined
    but ginkgo 1 -> 2 added new failure kinds. So, check for non-pass
    """
    return (
        e2e_test["state"] != "passed"
        and e2e_test["state"] != "skipped"
        and e2e_test["state"] != "ignoreFailed"
    )

def track_time_taken(test_results, test_times):
    """computes the running shortest and longest duration of running each kind of test
    """
    name = test_results["name"]
    if (test_results["start_time"] == "0001-01-01T00:00:00Z" or
        test_results["start_time"] == "0001-01-01T00:00:00Z"):
        return
    # chop off the nanoseconds part, which is too much for Python `fromisoformat`
    start_frags = test_results["start_time"].split(".")
    if len(start_frags) != 2:
        return
    end_frags = test_results["end_time"].split(".")
    if len(end_frags) != 2:
        return

    start_time = datetime.fromisoformat(start_frags[0])
    end_time = datetime.fromisoformat(end_frags[0])
    duration = end_time - start_time
    matrix_id = test_results["matrix_id"]
    if name not in test_times["max"]:
        test_times["max"][name] = duration
    if name not in test_times["min"]:
        test_times["min"][name] = duration
    if name not in test_times["slowest_branch"]:
        test_times["slowest_branch"][name] = matrix_id

    if duration > test_times["max"][name]:
        test_times["max"][name] = duration
        test_times["slowest_branch"][name] = matrix_id
    if duration < test_times["min"][name]:
        test_times["min"][name] = duration

def count_bucketized_stats(test_results, parameter_buckets, field_id):
    """counts the success/failures onto a bucket. This means there are two
    dictionaries: one for `total` tests, one for `failed` tests.
    """
    bucket_id = test_results[field_id]
    if bucket_id not in parameter_buckets["total"]:
        parameter_buckets["total"][bucket_id] = 0
    parameter_buckets["total"][bucket_id] = 1 + parameter_buckets["total"][bucket_id]
    if is_failed(test_results):
        if bucket_id not in parameter_buckets["failed"]:
            parameter_buckets["failed"][bucket_id] = 0
        parameter_buckets["failed"][bucket_id] = 1 + parameter_buckets["failed"][bucket_id]

def compute_bucketized_summary(parameter_buckets):
    """counts the number of buckets with failures and the total number of buckets

    returns (num-failed-buckets, num-total-buckets)
    """
    failed_buckets_count = 0
    total_buckets_count = 0
    for _ in parameter_buckets["total"]:
        total_buckets_count = 1 + total_buckets_count
    for _ in parameter_buckets["failed"]:
        failed_buckets_count = 1 + failed_buckets_count
    return failed_buckets_count, total_buckets_count

def compute_test_summary(test_dir):
    """iterate over the JSON artifact files in `test_dir`, and
    bucket them for comprehension.

    Produces a dictionary of dictionaries:

    {
        "total_run": 0,
        "total_failed": 0,
        "by_test": by_test,
        "by_matrix": by_matrix,
        "by_k8s": by_k8s,
        "by_platform": by_platform,
        "by_postgres": by_postgres,
        "test_durations": test_dutrations,
    }
    """
    total_runs = 0
    total_fails = 0
    unique_test = {
        "total": {},
        "failed": {}                
    }
    by_test = {
        "total": {},
        "failed": {},
        "k8s_versions_failed": {},
        "pg_versions_failed": {}
    }
    by_matrix = {
        "total": {},
        "failed": {}        
    }
    by_k8s = {
        "total": {},
        "failed": {}        
    }
    by_postgres = {
        "total": {},
        "failed": {}
    }
    by_platform = {
        "total": {},
        "failed": {}
    }

    test_dutrations = {
        "max": {},
        "min": {},
        "slowest_branch": {}
    }

    dir_listing = os.listdir(test_dir)
    for f in dir_listing:
        path = os.path.join(test_dir, f)
        with open(path) as json_file:
            test_results = json.load(json_file)

            ## bucketing by test name
            name = test_results["name"]
            if name not in by_test["total"]:
                by_test["total"][name] = 0
            unique_test["total"][name] = True
            total_runs = 1 + total_runs
            by_test["total"][name] = 1 + by_test["total"][name]
            if is_failed(test_results):
                if name not in by_test["failed"]:
                    by_test["failed"][name] = 0
                if name not in by_test["k8s_versions_failed"]:
                    by_test["k8s_versions_failed"][name] = {}
                if name not in by_test["pg_versions_failed"]:
                    by_test["pg_versions_failed"][name] = {}
                unique_test["failed"][name] = True
                by_test["failed"][name] = 1 + by_test["failed"][name]
                total_fails = 1 + total_fails
                by_test["k8s_versions_failed"][name][test_results["k8s_version"]] = True
                by_test["pg_versions_failed"][name][test_results["postgres_version"]] = True

            ## bucketing by matrix ID
            count_bucketized_stats(test_results, by_matrix, "matrix_id")

            ## bucketing by k8s version
            count_bucketized_stats(test_results, by_k8s, "k8s_version")

            ## bucketing by postgres version
            count_bucketized_stats(test_results, by_postgres, "postgres_version")

            ## bucketing by platform
            count_bucketized_stats(test_results, by_platform, "platform")

            track_time_taken(test_results, test_dutrations)



    return {
        "total_run": total_runs,
        "total_failed": total_fails,
        "by_test": by_test,
        "by_matrix": by_matrix,
        "by_k8s": by_k8s,
        "by_platform": by_platform,
        "by_postgres": by_postgres,
        "test_durations": test_dutrations,
    }

def compile_overview(summary):
    """computes the failed vs total count for different buckets
    """
    unique_failed, unique_run = compute_bucketized_summary(summary["by_test"])
    k8s_failed, k8s_run = compute_bucketized_summary(summary["by_k8s"])
    postgres_failed, postgres_run = compute_bucketized_summary(summary["by_postgres"])
    matrix_failed, matrix_run = compute_bucketized_summary(summary["by_matrix"])
    platform_failed, platform_run = compute_bucketized_summary(summary["by_platform"])
    return {
        "total_run": summary["total_run"],
        "total_failed": summary["total_failed"],
        "unique_run": unique_run,
        "unique_failed": unique_failed,
        "k8s_run": k8s_run,
        "k8s_failed": k8s_failed,
        "postgres_run": postgres_run,
        "postgres_failed": postgres_failed,
        "matrix_failed": matrix_failed,
        "matrix_run": matrix_run,
        "platform_failed": platform_failed,
        "platform_run": platform_run
    }

def format_overview(summary, structure):
    """print unbucketed test metrics"""
    print("## " + structure["title"])
    print()
    print("|" + " | ".join(structure["header"]) + "|")
    print("|" + "|".join(["---"] * len(structure["header"])) + "|")
    for row in structure["rows"]:
        print(
            "| {failed} | {total} | {name} |".format(
                name=row[0],
                failed=summary[row[1]],
                total=summary[row[2]],
            )
        )
    print()

def format_bucket_table(buckets, structure):
    """print table with bucketed metrics, sorted by decreasing amount of faiulres.

    The structure argument contains the layout directives. E.g.
    {
        "title": "Failures by platform",
        "header": ["failed tests", "total tests", "platform"],
    }
    """
    print("<h2><a name={anchor}>{title}</a></h2>".format(
        title=structure["title"],
        anchor=structure["anchor"]))
    print()
    print("|" + " | ".join(structure["header"]) + "|")
    print("|" + "|".join(["---"] * len(structure["header"])) + "|")
    sorted_by_fail = dict(
        sorted(
            buckets["failed"].items(), key=lambda item: item[1], reverse=True
        )
    )

    for bucket in sorted_by_fail:
        print(
            "| {failed} | {total} | {name} |".format(
                name=bucket,
                failed=buckets["failed"][bucket],
                total=buckets["total"][bucket],
            )
        )
    print()

def format_by_test(summary, structure):
    """print metrics bucketed by test class
    """
    print("<h2><a name={anchor}>{title}</a></h2>".format(
        title=structure["title"],
        anchor=structure["anchor"]))
    print()
    print("|" + " | ".join(structure["header"]) + "|")
    print("|" + "|".join(["---"] * len(structure["header"])) + "|")
    sorted_by_fail = dict(
        sorted(
            summary["by_test"]["failed"].items(), key=lambda item: item[1], reverse=True
        )
    )

    for bucket in sorted_by_fail:
        print(
            "| {failed} | {total} | {failed_k8s} | {failed_pg} | {name} |".format(
                name=bucket,
                failed=summary["by_test"]["failed"][bucket],
                total=summary["by_test"]["failed"][bucket],
                failed_k8s=", ".join(summary["by_test"]["k8s_versions_failed"][bucket].keys()),
                failed_pg=", ".join(summary["by_test"]["pg_versions_failed"][bucket].keys()),
            )
        )
    print()

def format_duration(d):
    "pretty-print duration"
    return "{minutes} min {seconds} sec".format(
        minutes = d.seconds // 60,
        seconds = d.seconds % 60,
    )

def format_durations_table(test_times, structure):
    """print the table of durations per test
    """
    print("<h2><a name={anchor}>{title}</a></h2>".format(
        title=structure["title"],
        anchor=structure["anchor"]))
    print()
    print("|" + " | ".join(structure["header"]) + "|")
    print("|" + "|".join(["---"] * len(structure["header"])) + "|")
    sorted_by_longest = dict(
        sorted(
            test_times["max"].items(), key=lambda item: item[1], reverse=True
        )
    )

    for bucket in sorted_by_longest:
        print(
            "| {longest} | {shortest} | {branch} | {name} |".format(
                name=bucket,
                longest=format_duration(test_times["max"][bucket]),
                shortest=format_duration(test_times["min"][bucket]),
                branch=test_times["slowest_branch"][bucket]
            )
        )
    print()

def format_test_failures(summary):
    """creates the part of the test report that drills into the failures
    """
    by_test_section = {
        "title": "Failures by test",
        "anchor": "by_test",
        "header": ["failed runs", "total runs", "failed K8s", "failed PG", "test"],
    }

    format_by_test(summary, by_test_section)

    by_matrix_section = {
        "title": "Failures by matrix branch",
        "anchor": "by_matrix",
        "header": ["failed tests", "total tests", "matrix branch"],
    }

    format_bucket_table(summary["by_matrix"], by_matrix_section)

    by_k8s_section = {
        "title": "Failures by kubernetes version",
        "anchor": "by_k8s",
        "header": ["failed tests", "total tests", "kubernetes version"],
    }

    format_bucket_table(summary["by_k8s"], by_k8s_section)

    by_postgres_section = {
        "title": "Failures by postgres version",
        "anchor": "by_postgres",
        "header": ["failed tests", "total tests", "postgres version"],
    }

    format_bucket_table(summary["by_postgres"], by_postgres_section)

    by_platform_section = {
        "title": "Failures by platform",
        "anchor": "by_platform",
        "header": ["failed tests", "total tests", "platform"],
    }

    format_bucket_table(summary["by_platform"], by_platform_section)

def format_test_summary(summary):
    """creates a Markdown document with several tables rendering test results.
    Outputs to stdout like a good 12-factor-app citizen
    """

    print(
        """Note that there are several tables below: overview, bucketed
by several parameters, timings.
""")
    if summary["total_failed"] != 0:
        print(
            """Index: [timing table](#user-content-timing) | [by test](#user-content-by_test) |
  [by k8s](#user-content-by_k8s) | [by postgres](#user-content-by_postgres) |  [by platform](#user-content-by_platform)
""")

    overview = compile_overview(summary)

    overview_section = {
        "title": "Overview",
        "header": ["failed", "out of", ""],
        "rows": [["test combinations", "total_failed", "total_run"],
                ["unique tests", "unique_failed", "unique_run"],
                ["matrix branches", "matrix_failed", "matrix_run"],
                ["k8s versions", "k8s_failed", "k8s_run"],
                ["postgres versions", "postgres_failed", "postgres_run"],
                ["platforms", "platform_failed", "platform_run"]],
    }

    format_overview(overview, overview_section)

    if summary["total_failed"] == 0:
        print("No failures, no failure stats shown. It's not easy being green.")
        print()
    else:
        format_test_failures(summary)

    timing_section = {
        "title": "Test times",
        "anchor": "timing",
        "header": ["longest taken", "shortest taken", "slowest branch", "test"],
    }

    format_durations_table(summary["test_durations"], timing_section)


if __name__ == "__main__":

    parser = argparse.ArgumentParser(description="Summarize the E2E Suite results")
    parser.add_argument(
        "-d",
        "--dir",
        type=str,
        help="directory with the JSON artifacts",
    )

    args = parser.parse_args()

    summary = compute_test_summary(args.dir)
    format_test_summary(summary)
