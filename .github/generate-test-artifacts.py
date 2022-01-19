#
# This file is part of Cloud Native PostgreSQL.
#
# Copyright (C) 2019-2021 EnterpriseDB Corporation.
#
import argparse
import json
import re
import os
import hashlib

def flatten(arr):
    """flatten an array of arrays"""
    out = []
    for l in arr:
        for item in l:
            out.append(item)
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
        "repo": , # EnterpriseDB/cloud-native-postgresql -> you get this from GH with ${{github.repository}}
        "branch": , # dev/cnp-1666 -> you get this with "${{github.head_ref}}" ... EXCEPT
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
    if state == "failed" and "ContainerHierarchyLabels" in t and "ignore-fails" in flatten(t["ContainerHierarchyLabels"]):
        state = "ignoreFailed"

    kind = "PostgreSQL"

    branch = matrix["branch"]
    if branch == "":
        branch = matrix["refname"]

    x = {
        "name": " - ".join(t["ContainerHierarchyTexts"]) + " -- "  + t["LeafNodeText"],
        "state": state,
        "start_time": t["StartTime"],
        "end_time": t["EndTime"], # NOTE: Grafana will need a default timestamp field. This is a good candidate
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
    return x


if __name__ == "__main__":

    parser = argparse.ArgumentParser(description="Create JSON artifacts from E2E JSON report")
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
        help="directory where we write the artifiacts",
    )
    parser.add_argument(
        "-m",
        "--matrix",
        type=str,
        help="the matrix with GH execution variables"
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
            print("Directory " , dir ,  " Created ")

    # MAIN LOOP: go over each `SpecReport` in the Ginkgo JSON output, convert
    # each to the normalized JSON format: https://enterprisedb.atlassian.net/wiki/spaces/PD/pages/2927591425/Generic+JSON+format+for+Test+Errors+in+Dashboard
    # And create a JSON file for each of those
    whitespace = re.compile("\s")
    with open(args.file) as json_file:
        testResults = json.load(json_file)
        for t in testResults[0]["SpecReports"]:
            if (t["State"] != "skipped") and (t["LeafNodeText"] != ""):
                test1 = convert_ginkgo_test(t, matrix)
                # the filename will be used as the ID to introduce the payload into
                # Elastic. Should be unique across the current GH run. So: MatrixID + Test
                # But, because we may run this on MSFT Azure, where filename length limits still
                # exist, we HASH the test name.
                # The platform team's scraping script will add the GH Run ID to this, and the
                # Repository, and with Repo + Run ID + MatrixID + Test Hash, gives a unique
                # ID in Elastic to each object.
                slug = whitespace.sub("_", test1["name"])
                h = hashlib.sha224(slug.encode('utf-8')).hexdigest()
                filename = matrix["id"] + "_" + h + ".json"
                if dir != "":
                    filename = dir + "/" + filename
                with open(filename, "w") as f:
                    f.write(json.dumps(test1))
