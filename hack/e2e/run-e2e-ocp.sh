#!/usr/bin/env bash

##
## Copyright © contributors to CloudNativePG, established as
## CloudNativePG a Series of LF Projects, LLC.
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
## SPDX-License-Identifier: Apache-2.0
##

set -eEuo pipefail

# wait_for(type, namespace, name, interval, retries)
# Parameters:
#   type: the k8s object type
#   namespace: the namespace the object resides in
#   name: the object name
#   interval: the time to expect between each retry
#   retries: the total number of retries
function wait_for() {
  # We wait later for the deployment to be available, but if it doesn't exist if fails. Let's wait again.
  ITER=0
  while ! oc get -n "$2" "$1" "$3" && [ $ITER -lt "$5" ]; do
    ITER=$((ITER + 1))
    echo "$3 $1 doesn't exist yet. Waiting $4 seconds ($ITER of $5)."
    sleep "$4"
  done
  [[ $ITER -lt $5 ]]
}

# Retry a command up to a specific numer of times until it exits successfully,
# with exponential back off.
#
#  $ retry 5 echo Hello
#  Hello
#
#  $ retry 5 false
#  Retry 1/5 exited 1, retrying in 1 seconds...
#  Retry 2/5 exited 1, retrying in 2 seconds...
#  Retry 3/5 exited 1, retrying in 4 seconds...
#  Retry 4/5 exited 1, retrying in 8 seconds...
#  Retry 5/5 exited 1, no more retries left.
#
# Inspired from https://gist.github.com/sj26/88e1c6584397bb7c13bd11108a579746
function retry {
  local retries=$1
  shift

  local count=0
  until "$@"; do
    local exit=$?
    local wait=$((2 ** count))
    count=$((count + 1))
    if [ $count -lt "$retries" ]; then
      echo "Retry $count/$retries exited $exit, retrying in $wait seconds..." >&2
      sleep $wait
    else
      echo "Retry $count/$retries exited $exit, no more retries left." >&2
      return $exit
    fi
  done
  return 0
}

ROOT_DIR=$(realpath "$(dirname "$0")/../../")
# we need to export ENVs defined in the workflow and used in run-e2e.sh script
export POSTGRES_IMG=${POSTGRES_IMG:-$(grep 'DefaultImageName.*=' "${ROOT_DIR}/pkg/versions/versions.go" | cut -f 2 -d \")}
export PGBOUNCER_IMG=${PGBOUNCER_IMG:-$(grep 'DefaultPgbouncerImage.*=' "${ROOT_DIR}/pkg/versions/versions.go" | cut -f 2 -d \")}

# Override pgbouncer image repository if PGBOUNCER_IMG_REPOSITORY is set
if [ -n "${PGBOUNCER_IMG_REPOSITORY:-}" ]; then
  PGBOUNCER_VERSION=$(echo "${PGBOUNCER_IMG}" | cut -d: -f2)
  PGBOUNCER_IMG="${PGBOUNCER_IMG_REPOSITORY}:${PGBOUNCER_VERSION}"
fi

export E2E_PRE_ROLLING_UPDATE_IMG=${E2E_PRE_ROLLING_UPDATE_IMG:-${POSTGRES_IMG%.*}}
export E2E_DEFAULT_STORAGE_CLASS=${E2E_DEFAULT_STORAGE_CLASS:-standard}
export E2E_CSI_STORAGE_CLASS=${E2E_CSI_STORAGE_CLASS:-}
export TEST_CLOUD_VENDOR="ocp"

# create the catalog source
oc apply -f cloudnative-pg-catalog.yaml

# create the secret for the index to be pulled in the marketplace
oc create secret docker-registry -n openshift-marketplace --docker-server="${REGISTRY}" --docker-username="${REGISTRY_USER}" --docker-password="${REGISTRY_PASSWORD}" cnpg-pull-secret || true

# Install the operator
oc apply -f - <<EOF
apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  name: cloudnative-pg
  namespace: openshift-operators
spec:
  channel: stable-v1
  name: cloudnative-pg
  source: cloudnative-pg-catalog
  sourceNamespace: openshift-marketplace
  config:
    env:
    - name: POSTGRES_IMAGE_NAME
      value: ${POSTGRES_IMG}
    - name: PGBOUNCER_IMAGE_NAME
      value: ${PGBOUNCER_IMG}
    - name: STANDBY_TCP_USER_TIMEOUT
      value: "5000"
EOF

# The subscription will install the operator, but the service account used
# requires a secret. When the sa is available, define the secret.
wait_for sa openshift-operators cnpg-manager 10 60
oc create secret docker-registry -n openshift-operators --docker-server="${REGISTRY}" --docker-username="${REGISTRY_USER}" --docker-password="${REGISTRY_PASSWORD}" cnpg-pull-secret || true
retry 5 oc secrets link -n openshift-operators cnpg-manager cnpg-pull-secret --for=pull

# We wait 60 seconds for the operator deployment to be created
echo "Waiting 60s for the operator deployment to be ready"
sleep 60

# We wait later for the deployment to be available, but if it doesn't exist if fails. Let's wait again.
CSV_NAME=$(oc get csv -n openshift-operators -l 'operators.coreos.com/cloudnative-pg.openshift-operators=' -o jsonpath='{.items[0].metadata.name}')
DEPLOYMENT_NAME=$(oc get csv -n openshift-operators "$CSV_NAME" -o jsonpath='{.spec.install.spec.deployments[0].name}')
wait_for deployment openshift-operators "$DEPLOYMENT_NAME" 5 60

# After creating Subscription, OLM needs time to create and start the operator pod with the configured environment
ITER=0
while true; do
  ITER=$((ITER + 1))
  sleep 5
  if [[ $ITER -gt 60 ]]; then
    echo "OLM did not create operator pod with correct environment, exiting"
    oc get -n openshift-operators "$(oc get -n openshift-operators deployments -o name)" -o yaml || true
    oc get -n openshift-operators "$(oc get -n openshift-operators pods -o name)" -o yaml || true
    oc logs -n openshift-operators "$(oc get -n openshift-operators pods -o name)" || true
    exit 1
  fi
  # There should be only one pod
  pod_count=$(oc get -n openshift-operators pods -o name -l app.kubernetes.io/name=cloudnative-pg | wc -l)
  if [[ $pod_count -ne 1 ]]; then
    echo "[$ITER] Expected pod count to be 1, got $pod_count instead"
    continue
  fi
  # The pod should be ready
  if ! oc wait --for=condition=Ready -n openshift-operators pods -l app.kubernetes.io/name=cloudnative-pg --timeout=0; then
    echo "[$ITER] Waiting pod to be ready"
    continue
  fi
  # Check the pod env is correct
  pod_postgres_img=$(oc get -n openshift-operators pods -l app.kubernetes.io/name=cloudnative-pg -o jsonpath="{.items[0].spec.containers[0].env[?(@.name=='POSTGRES_IMAGE_NAME')].value}" || true)
  if [[ "${pod_postgres_img}" != "${POSTGRES_IMG}" ]]; then
    echo "[$ITER] Expected POSTGRES_IMG to be $POSTGRES_IMG, got $pod_postgres_img instead"
    continue
  fi
  pod_pgbouncer_img=$(oc get -n openshift-operators pods -l app.kubernetes.io/name=cloudnative-pg -o jsonpath="{.items[0].spec.containers[0].env[?(@.name=='PGBOUNCER_IMAGE_NAME')].value}" || true)
  if [[ "${pod_pgbouncer_img}" != "${PGBOUNCER_IMG}" ]]; then
    echo "[$ITER] Expected PGBOUNCER_IMG to be $PGBOUNCER_IMG, got $pod_pgbouncer_img instead"
    continue
  fi
  # All checks passed, proceeding
  echo "[$ITER] Everything ready to run e2e tests."
  break
done

# Move OCP ingress routers off worker nodes for the duration of this test
# job, so the drain_node e2e does not strand router-default's PDB. The
# placement is non-production but the OCP cluster is destroyed at end-of-job.
# OCP still applies the legacy control-plane taint by default; the key is
# kept in a shell variable so the JSON literal does not contain a woke
# trigger word.
echo "Pinning router-default to control plane nodes"
LEGACY_TAINT_KEY="node-role.kubernetes.io/master" # wokeignore:rule=master
CTRL_PLANE_KEY="node-role.kubernetes.io/control-plane"
PATCH=$(cat <<EOF
{
  "spec": {
    "nodePlacement": {
      "nodeSelector": { "matchLabels": { "${CTRL_PLANE_KEY}": "" } },
      "tolerations": [
        { "key": "${LEGACY_TAINT_KEY}", "operator": "Exists", "effect": "NoSchedule" },
        { "key": "${CTRL_PLANE_KEY}",   "operator": "Exists", "effect": "NoSchedule" }
      ]
    }
  }
}
EOF
)
NEW_GEN=$(oc patch ingresscontroller.operator.openshift.io/default \
  -n openshift-ingress-operator --type merge -p "$PATCH" \
  -o jsonpath='{.metadata.generation}')
oc wait ingresscontroller.operator.openshift.io/default \
  -n openshift-ingress-operator \
  --for=jsonpath="{.status.observedGeneration}=${NEW_GEN}" --timeout=2m
oc rollout status -n openshift-ingress deployment/router-default --timeout=5m
oc wait ingresscontroller.operator.openshift.io/default \
  -n openshift-ingress-operator \
  --for=condition=Available=True --timeout=2m

echo "Running the e2e tests"
"${ROOT_DIR}/hack/e2e/run-e2e.sh"
