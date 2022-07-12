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
import sys

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
    parser.add_argument(
        "-o",
        "--outfile",
        type=str,
        default="",
        help="file to write the output to",
    )

    args = parser.parse_args()

    output = sys.stdout
    if args.outfile != "":
        output = args.outfile

    print("out file:")
    print(output)

    hits_by_test = {}

    dir_listing = os.listdir(args.dir)
    for f in dir_listing:
        path = os.path.join(args.dir, f)
        with open(path) as json_file:
            testResults = json.load(json_file)
            name = testResults["name"]
            if name not in hits_by_test:
                hits_by_test[name] = 0
            hits_by_test[name] = 1 + hits_by_test[name]

    row = Template("|{{ name }} | {{ hits }}|")

    with open(output, mode="a") as fout:
        print("""# E2E Test summary

## total hits by test
| test | hits |
|------|------|
        """, file=fout)

        for testname in hits_by_test:
            print(row.render(name=testname, hits=hits_by_test[testname]), file=fout)
