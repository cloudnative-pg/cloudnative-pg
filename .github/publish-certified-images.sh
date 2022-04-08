#!/usr/bin/env bash

##
## Copyright 2019-2022 The CloudNativePG Contributors
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

# Standard bash error handling
set -eEuo pipefail

# Default variables
RH_API=${RH_API:-}
RH_PROJ_ID=${RH_PROJ_ID:-}
RH_URL="${RH_API}/${RH_PROJ_ID}"
RH_CONNECT_API_KEY=${RH_CONNECT_API_KEY:-api_key}
DIGEST=${DIGEST:-}
QUAY_JSON_AUTH=${QUAY_JSON_AUTH:-}

function executeCurl() {
    local url=$1; shift
    local type=$1; shift
    local data=$1; shift

    curl -s -X  "$type" "$url" \
      -H 'Content-Type: application/json' \
      -H 'X-API-KEY: '"${RH_CONNECT_API_KEY}" \
      -d "$data"
}

# generate_data(tag)
# Parameters:
#   tag: the certified container image

# Generate data to publish image
function generate_data() {
  local TAG=$1; shift

  # We need to make sure that Red Hat has the secret to pull.
  data="{\"container\":{\"kube_objects\":\"$QUAY_JSON_AUTH\"}}"

  # Getting the _id from the certified image to publish in Red Hat catalog
  jsonOutput=$(executeCurl "${RH_URL}/images" 'GET' '{}')
  ID=$(echo "$jsonOutput" | jq -C -r '.data[] | select (.docker_image_digest == "'"$DIGEST"'") | ._id')

  # Creating the data
  jq -c -n --arg id "$ID" --arg tag "$TAG" '{image_id:$id, tag: $tag}'
}

# publish_image(url, data, tag)
# Parameters:
#   url: the url to consume from API
#   data: the data sent in the request

function publish_image() {
  local data=$1; shift
  local url="${RH_URL}/requests/tags"

  ## We start the publish process with the released version tag
  response=$(executeCurl "$url" 'POST' "$data")

  status=$(echo "${response}" | jq '.status' )
  case "${status}" in
      400 | 404 )
          # detail is only available in case of error
          detail=$(echo "${response}" | jq '.detail')
          echo "ERROR there was an error while calling the Red Hat API: ${detail}"
          exit 1
      ;;
      409)
          printf "WARNING: there is a request conflict with current state of the target resource.\nPlease check %s/images for more info." "$RH_URL"
          exit 1
          ;;
  esac

  # Saving tag request id to use later
  tagReqID=$(echo "${response}"  | jq -C -r ._id)

  echo "${tagReqID}"
}

# check_publish_status(tag_req_id)
# Parameters:
#   tag_req_id: publish tag request id

function check_publish_status() {
  local tag_req_id=$1; shift
  local url="${RH_URL}/requests/tags?page_size=10&page=0&sort_by=creation_date%5Bdesc%5D"

  # Wait until the status of the image being published is completed
  echo "Starting to check image publish status"
  status=""
  ITER=0
  while [ "$status" != "completed" ] && [ $ITER -lt 30 ]; do
    ITER=$((ITER + 1))
    jsonStatus=$(executeCurl "${url}" 'GET' '{}')
    status=$(echo "${jsonStatus}" | jq -r '.data[] | select (._id == "'"$tag_req_id"'") | .status')
    echo "The current publish status is ${status}. Waiting 30 seconds and then retry (Iteration #$ITER)."
    sleep 30
  done

  # If the publish status of the image is not completed, script execution will stop.
  if [ "$status" != "completed" ]; then
    jsonStatus=$(executeCurl "$url" 'GET' '{}')
    status=$(echo "$jsonStatus" | jq -r '.data[] | select (._id == "'"$tag_req_id"'")')
    echo "The publish status is not completed. The actual status is $status, script will stop"
    exit 1
  fi

}

# Generate data to be used in the publish request
data=$(generate_data "${CNP_VERSION}")

# Publishing certified operator image with released version tag
# and mark it as latest tag
echo "Starting the publish process"
tagReqID=$(publish_image "${data}" "${CNP_VERSION}")

# Checking publication status
check_publish_status "${tagReqID}"
