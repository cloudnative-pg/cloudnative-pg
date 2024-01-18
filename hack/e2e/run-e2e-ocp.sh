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

ROOT_DIR=$(realpath "$(dirname "$0")/../../")
SUBPROJECT_ROOT_DIR="${ROOT_DIR}/cloudnative-pg"
# we need export defined in workflow and used in run-e2e.sh script
export POSTGRES_IMG=${POSTGRES_IMG:-$(grep 'DefaultImageName.*=' "${SUBPROJECT_ROOT_DIR}/pkg/versions/versions.go" | cut -f 2 -d \")}
export E2E_PRE_ROLLING_UPDATE_IMG=${E2E_PRE_ROLLING_UPDATE_IMG:-${POSTGRES_IMG}.0}
export E2E_ENABLE_REDWOOD=${E2E_ENABLE_REDWOOD:-}
export E2E_RUN_UPGRADE_TESTS=${E2E_RUN_UPGRADE_TESTS:-false}
OCP_VERSION=${OCP_VERSION:-latest}
export E2E_DEFAULT_STORAGE_CLASS=${E2E_DEFAULT_STORAGE_CLASS:-standard}
export TEST_TIMEOUTS=${TEST_TIMEOUTS:-}
export FEATURE_TYPE=${FEATURE_TYPE:-}

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
EOF

# The subscription will install the operator, but the service account used
# requires a secret. When the sa is available, define the secret.
wait_for sa openshift-operators cnpg-manager 10 60
oc create secret docker-registry -n openshift-operators --docker-server="${REGISTRY}" --docker-username="${REGISTRY_USER}" --docker-password="${REGISTRY_PASSWORD}" cnpg-pull-secret || true
oc secrets link -n openshift-operators cnpg-manager cnpg-pull-secret --for=pull

# We wait 30 seconds for the operator deployment to be created
echo "Waiting 30s for the operator deployment to be ready"
sleep 60

# We wait later for the deployment to be available, but if it doesn't exist if fails. Let's wait again.
DEPLOYMENT_NAME=$(oc get csv -n openshift-operators -o yaml | awk '/deploymentName/{print $2; exit}')
wait_for deployment openshift-operators "$DEPLOYMENT_NAME" 5 60

# Force a default postgresql image in the running operator
oc patch -n openshift-operators "$(oc get csv -n openshift-operators -o name)" --type='json' -p \
"[
  {\"op\": \"add\", \"path\": \"/spec/install/spec/deployments/0/spec/template/spec/containers/0/env/0\", \"value\": { \"name\": \"POSTGRES_IMAGE_NAME\", \"value\": \"${POSTGRES_IMG}\"}}
]"

if [ -z "${TESTING_LICENSE+x}" ]; then
  echo "no license set, skipping step"
else
  oc patch -n openshift-operators "$(oc get csv -n openshift-operators -o name)" --type='json' -p \
"[
  {\"op\": \"add\", \"path\": \"/spec/install/spec/deployments/0/spec/template/spec/containers/0/env/0\", \"value\": { \"name\": \"EDB_LICENSE_KEY\", \"value\": \"${TESTING_LICENSE}\"}},
  {\"op\": \"add\", \"path\": \"/spec/install/spec/deployments/0/spec/template/spec/containers/0/env/0\", \"value\": { \"name\": \"ENABLE_REDWOOD_BY_DEFAULT\", \"value\": \"${E2E_ENABLE_REDWOOD}\"}}
]"
fi

# After patching, we need some time to propagate the change to the deployment and the pod.
ITER=0
while true; do
  ITER=$((ITER + 1))
  sleep 5
  if [[ $ITER -gt 60 ]]; then
    echo "Patch not propagated to pod, exiting"
    oc get -n openshift-operators "$(oc get -n openshift-operators deployments -o name)" -o yaml || true
    oc get -n openshift-operators "$(oc get -n openshift-operators pods -o name)" -o yaml || true
    oc logs -n openshift-operators "$(oc get -n openshift-operators pods -o name)" || true
    exit 1
  fi
  # There should be only one pod
  pod_count=$(oc get -n openshift-operators pods -o name | wc -l)
  if [[ $pod_count -ne 1 ]]; then
    echo "[$ITER] Expected pod count to be 1, got $pod_count instead"
    continue
  fi
  # The pod should be ready
  if ! oc wait --for=condition=Ready -n openshift-operators "$(oc get -n openshift-operators pods -o name)" --timeout=0; then
    echo "[$ITER] Waiting pod to be ready"
    continue
  fi
  # Check the pod env is correct
  pod_postgres_img=$(oc get -n openshift-operators "$(oc get -n openshift-operators pods -o name)" -o jsonpath="{.spec.containers[0].env[?(@.name=='POSTGRES_IMAGE_NAME')].value}" || true)
  if [[ "${pod_postgres_img}" != "${POSTGRES_IMG}" ]]; then
    echo "[$ITER] Expected POSTGRES_IMG to be $POSTGRES_IMG, got $pod_postgres_img instead"
    continue
  fi
  # All checks passed, proceeding
  echo "[$ITER] Everything ready to run e2e tests."
  break
done


E2E_DEFAULT_VOLUMESNAPSHOT_CLASS=$(kubectl get vsclass -o=jsonpath='{.items[?(@.metadata.annotations.snapshot\.storage\.kubernetes\.io/is-default-class=="true")].metadata.name}')
export E2E_DEFAULT_VOLUMESNAPSHOT_CLASS
export OPENSHIFT="true"
echo "Running the e2e tests"
"${ROOT_DIR}/hack/e2e/run-e2e.sh"
