#!/usr/bin/env bash
##
## Copyright The CloudNativePG Contributors
##
## Licensed under the Apache License, Version 2.0 (the "License");
## you may not use this file except in compliance with the License.
## You may obtain a copy of the License at
##
##     http://www.apache.org/licenses/LICENSE-2.0
##
## Unless required by applicable law or agreed to in writing, software
## distributed under the License is distributed on an "AS IS" BASIS,
## WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
## See the License for the specific language governing permissions and
## limitations under the License.
##

echo '::echo::off'

colorBoldRed='\033[1;31m'
colorBoldYellow='\033[1;33m'
colorWhite='\033[37m'
colorBoldWhite='\033[1;37m'
colorGreen='\033[0;32m'

indent() {
  local indent=1
  if [ -n "$1" ]; then indent=$1; fi
  pr -to "${indent}"
}

function failure_summary {
  # Number of failures
  cnt=0
  # whether the failures are ignorable
  ignorable=false
  if [ "$1" = "ignorable" ]; then
    ignorable=true
  fi

  if ! ${ignorable}; then
    highlight_color=${colorBoldRed}
    printf "${highlight_color}%s\n\n" "Summarizing Non-Ignorable Failure(s):"
    filter_file="hack/e2e/filter-failures.jq"
    summary="Non Ignorable Failure(s) Found!"
  else
    highlight_color=${colorBoldYellow}
    printf "${highlight_color}%s\n\n" "Summarizing Ignorable Failure(s):"
    filter_file="hack/e2e/filter-ignorable-failures.jq"
    summary="Ignorable Failure(s) Found!"
  fi

  for ff in "tests/e2e/out/upgrade_report.json" "tests/e2e/out/report.json"
  do
    # the upgrade_report.json file may not exist depending on the test level
    if [ ! -f $ff ] && [ $ff = "tests/e2e/out/upgrade_report.json" ]; then
      continue
    fi
    while read -rs failure; do
      desc=$(printf "%s" "${failure}" | jq -r -C '. | .Test')
      err=$(printf "%s" "${failure}" | jq -r -C '. | .Error')
      indented_err=$(echo "${err}" | indent 20)
      location=$(printf "%s" "${failure}" | jq -r -C '. | (.File + ":" + .Line)')
      stack=$(printf "%s" "${failure}" | jq -r .Stack)
      indented_stack=$(echo "${stack}" | indent 18)

      printf "${colorGreen}%-20s" "Spec Description: "
      printf "${colorBoldWhite}%s\n" "${desc}"
      printf "${colorGreen}%-20s\n" "Error Description:"
      printf "${highlight_color}%s${highlight_color}\n" "${indented_err}"
      printf "${colorGreen}%-20s" "Code Location:"
      printf "${colorWhite}%s\n" "${location}"
      echo
      ## The below line will print an annotation
      ## on the relevant source code line of the
      ## test that has failed. The annotation will
      ## be printed in the "changed files" tab of
      ## the Pull Request. We are commenting this
      ## to avoid generating noise when tests fail
      ## during workflows of PRs unrelated to that
      ## specific test.
      #  echo "$failure" | jq -r '. | "::notice file=" + .File + ",line=" + .Line + "::" + (.Error | @json )'
      echo "::group::Stack Trace:"
      echo "${indented_stack}"
      echo "::endgroup::"
      (( cnt+=1 ))
      echo
      echo "-----"
    done < <(jq -c -f "${filter_file}" $ff)
  done
  printf "${highlight_color}%d ${summary}\n\n" ${cnt}
  echo "------------------------------"
  echo
}

failure_summary "non-ignorable"
failure_summary "ignorable"