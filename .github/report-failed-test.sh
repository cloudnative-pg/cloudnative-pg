#!/usr/bin/env bash
##
## This file is part of Cloud Native PostgreSQL.
##
## Copyright (C) 2019-2021 EnterpriseDB Corporation.
##

echo '::echo::off'
for ff in "tests/e2e/out/upgrade_report.json" "tests/e2e/out/report.json"
do
  # the upgrade_report.json file may not exist depending on the test level
  if [ ! -f $ff ] && [ $ff = "tests/e2e/out/upgrade_report.json" ]; then
    continue
  fi
  jq -c -f "hack/e2e/filter-failures.jq" $ff | while read -rs failure; do
    echo "$failure" | jq -r '. | { Test: .Test, Error: .Error, Location: (.File + ":" + .Line) } | to_entries[] | "\(.key): \(.value )"'
    ## The below line will print an annotation
    ## on the relevant source code line of the
    ## test that has failed. The annotation will
    ## be printed in the "changed files" tab of
    ## the Pull Request. We are commenting this
    ## to avoid generating noise when tests fail
    ## during workflows of PRs unrelated to that
    ## specific test.
    #  echo "$failure" | jq -r '. | "::notice file=" + .File + ",line=" + .Line + "::" + (.Error | @json )'
    echo "::group::Stack trace"
    echo "$failure" | jq -r .Stack
    echo "::endgroup::"
    echo "-----"
  done
done
