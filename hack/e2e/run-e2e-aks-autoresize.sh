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

# run-e2e-aks-autoresize.sh
#
# Focused E2E runner for PVC auto-resize tests on AKS.
# Handles: build, push, deploy, pre-flight checks, test execution.
#
# Usage:
#   hack/e2e/run-e2e-aks-autoresize.sh [--skip-build] [--skip-deploy] [--diagnose-only]
#   hack/e2e/run-e2e-aks-autoresize.sh --focus "archive health"
#   hack/e2e/run-e2e-aks-autoresize.sh --focus "inactive slot" --skip-build --skip-deploy
#
# Required environment:
#   CONTROLLER_IMG  — image tag (default: ghcr.io/jmealo/cloudnative-pg-testing:feat-pvc-autoresizing)
#
# Optional environment:
#   E2E_DEFAULT_STORAGE_CLASS  — StorageClass to use (auto-detected if unset)
#   GINKGO_NODES               — parallelism (default: 1, sequential for auto-resize)
#   GINKGO_TIMEOUT             — overall timeout (default: 3h)
#   GINKGO_FOCUS               — regex to filter which tests run (e.g., "archive health|inactive slot")
#   TEST_TIMEOUTS              — JSON override for per-test timeouts
#
# Examples — iterating on a single failing test:
#   # Re-run only the archive health test (skip build/deploy for speed):
#   hack/e2e/run-e2e-aks-autoresize.sh --focus "archive health" --skip-build --skip-deploy
#
#   # Re-run only webhook tests:
#   hack/e2e/run-e2e-aks-autoresize.sh --focus "webhook" --skip-build --skip-deploy
#
#   # Re-run two specific tests:
#   hack/e2e/run-e2e-aks-autoresize.sh --focus "rate-limit|minStep" --skip-build --skip-deploy
#
#   # Final verification — run ALL auto-resize tests:
#   hack/e2e/run-e2e-aks-autoresize.sh --skip-build --skip-deploy
#
# Test names (for --focus regex matching):
#   "basic auto-resize"          — data PVC resize
#   "separate WAL volume"        — WAL PVC resize
#   "expansion limit"            — maxSize/limit enforcement
#   "webhook"                    — acknowledgeWALRisk validation
#   "rate-limit"                 — maxActionsPerDay enforcement
#   "minStep"                    — minimum step clamping
#   "maxStep"                    — maximum step webhook validation
#   "metrics"                    — Prometheus metric exposure
#   "tablespace"                 — tablespace PVC resize
#   "archive health"             — WAL archive blocks resize
#   "inactive slot"              — slot retention blocks resize

set -eEuo pipefail

if [ "${DEBUG-}" = true ]; then
  set -x
fi

ROOT_DIR=$(realpath "$(dirname "$0")/../../")
readonly ROOT_DIR

# Image tagging strategy:
# Use a unique tag per build (git short SHA) to avoid stale image cache issues.
# The init container (bootstrap-controller) copies the instance manager binary
# from the operator image into each PostgreSQL pod. With imagePullPolicy=IfNotPresent
# (the CNPG default), reusing the same tag across builds means nodes that already
# pulled the image will use the stale cached version, causing the instance manager
# to run old code even after rebuilding. A SHA-based tag ensures every build
# produces a unique, pullable image.
CONTROLLER_IMG_BASE=${CONTROLLER_IMG_BASE:-ghcr.io/jmealo/cloudnative-pg-testing}
CONTROLLER_IMG_TAG=${CONTROLLER_IMG_TAG:-feat-pvc-autoresizing-$(git -C "$(dirname "$0")/../../" rev-parse --short HEAD)}
CONTROLLER_IMG=${CONTROLLER_IMG:-${CONTROLLER_IMG_BASE}:${CONTROLLER_IMG_TAG}}
GINKGO_NODES=${GINKGO_NODES:-3}
GINKGO_TIMEOUT=${GINKGO_TIMEOUT:-3h}
SKIP_BUILD=${SKIP_BUILD:-false}
SKIP_DEPLOY=${SKIP_DEPLOY:-false}
DIAGNOSE_ONLY=${DIAGNOSE_ONLY:-false}
GINKGO_FOCUS=${GINKGO_FOCUS:-}

# Parse flags
while [[ $# -gt 0 ]]; do
  case "$1" in
    --skip-build)  SKIP_BUILD=true; shift ;;
    --skip-deploy) SKIP_DEPLOY=true; shift ;;
    --diagnose-only) DIAGNOSE_ONLY=true; shift ;;
    --focus)
      if [[ -n "${2:-}" ]]; then
        GINKGO_FOCUS="$2"; shift 2
      else
        fail "--focus requires a regex argument"
        exit 1
      fi
      ;;
    --focus=*) GINKGO_FOCUS="${1#--focus=}"; shift ;;
    *) warn "Unknown flag: $1"; shift ;;
  esac
done

POSTGRES_IMG=${POSTGRES_IMG:-$(grep 'DefaultImageName.*=' "${ROOT_DIR}/pkg/versions/versions.go" | cut -f 2 -d \")}
PGBOUNCER_IMG=${PGBOUNCER_IMG:-$(grep 'DefaultPgbouncerImage.*=' "${ROOT_DIR}/pkg/versions/versions.go" | cut -f 2 -d \")}

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
CYAN='\033[0;36m'
NC='\033[0m'

info()  { echo -e "${CYAN}[INFO]${NC}  $*"; }
warn()  { echo -e "${YELLOW}[WARN]${NC}  $*"; }
ok()    { echo -e "${GREEN}[OK]${NC}    $*"; }
fail()  { echo -e "${RED}[FAIL]${NC}  $*"; }

# ────────────────────────────────────────────────────────────
# Diagnostic functions (all read-only)
# ────────────────────────────────────────────────────────────

diagnose_volume_attachments() {
  info "=== Volume Attachment Diagnostics ==="

  info "Checking for FailedAttachVolume events (last 30 min)..."
  kubectl get events --all-namespaces \
    --field-selector reason=FailedAttachVolume \
    --sort-by='.lastTimestamp' 2>/dev/null | tail -20 || true

  info "Checking for FailedMount events (last 30 min)..."
  kubectl get events --all-namespaces \
    --field-selector reason=FailedMount \
    --sort-by='.lastTimestamp' 2>/dev/null | tail -20 || true

  info "Checking VolumeAttachment objects..."
  kubectl get volumeattachments -o custom-columns=\
'NAME:.metadata.name,ATTACHER:.spec.attacher,NODE:.spec.nodeName,ATTACHED:.status.attached,AGE:.metadata.creationTimestamp' \
    2>/dev/null | head -30 || true

  info "Checking for Pending PVCs..."
  kubectl get pvc --all-namespaces --field-selector status.phase=Pending \
    -o custom-columns='NAMESPACE:.metadata.namespace,NAME:.metadata.name,STATUS:.status.phase,SC:.spec.storageClassName' \
    2>/dev/null || true

  info "Checking for pods stuck in ContainerCreating..."
  kubectl get pods --all-namespaces --field-selector status.phase=Pending \
    -o custom-columns='NAMESPACE:.metadata.namespace,NAME:.metadata.name,STATUS:.status.phase,NODE:.spec.nodeName' \
    2>/dev/null | head -20 || true

  info "Node conditions..."
  kubectl get nodes -o custom-columns=\
'NAME:.metadata.name,STATUS:.status.conditions[-1].type,READY:.status.conditions[-1].status,DISK_PRESSURE:.status.conditions[?(@.type=="DiskPressure")].status' \
    2>/dev/null || true

  info "Checking Azure Disk CSI driver pods..."
  kubectl get pods -n kube-system -l app=csi-azuredisk-node \
    -o custom-columns='NAME:.metadata.name,STATUS:.status.phase,NODE:.spec.nodeName,RESTARTS:.status.containerStatuses[0].restartCount' \
    2>/dev/null || true

  info "StorageClass details..."
  kubectl get storageclass -o custom-columns=\
'NAME:.metadata.name,PROVISIONER:.provisioner,RECLAIM:.reclaimPolicy,BINDING:.volumeBindingMode,EXPANSION:.allowVolumeExpansion' \
    2>/dev/null || true

  info "=== End Volume Attachment Diagnostics ==="
}

print_victorialogs_hint() {
  info "=== VictoriaLogs Query Hint ==="
  info "To view all logs relevant to auto-resize E2E tests in VictoriaLogs, use the victorialogs-infra MCP:"
  info ""
  info '  {namespace=~"autoresize-.*|cnpg-system"}'
  info ""
  info "To further filter for volume/resize issues:"
  info '  {namespace=~"autoresize-.*|cnpg-system"} AND (_msg:~"resize|volume|attach|disk|pvc")'
  info ""
  info "For Azure CSI driver logs specifically:"
  info '  {namespace="kube-system"} AND (_msg:~"csi|azuredisk|attach|detach")'
  info ""
  info "You can also check metrics using the victoriametrics-infra MCP."
  info "=== End VictoriaLogs Hint ==="
}

diagnose_autoresize_namespaces() {
  info "=== Auto-Resize Namespace Diagnostics ==="

  for ns in $(kubectl get namespaces -o name 2>/dev/null | grep -E 'autoresize' | cut -d/ -f2); do
    info "--- Namespace: ${ns} ---"

    info "Pods:"
    kubectl get pods -n "${ns}" -o wide 2>/dev/null || true

    info "PVCs:"
    kubectl get pvc -n "${ns}" -o custom-columns=\
'NAME:.metadata.name,STATUS:.status.phase,SIZE:.spec.resources.requests.storage,SC:.spec.storageClassName,ROLE:.metadata.labels.cnpg\.io/pvcRole' \
      2>/dev/null || true

    info "Events (last 20):"
    kubectl get events -n "${ns}" --sort-by='.lastTimestamp' 2>/dev/null | tail -20 || true

    info "Cluster status:"
    kubectl get clusters.postgresql.cnpg.io -n "${ns}" -o json 2>/dev/null | \
      jq -r '.items[] | "Cluster: \(.metadata.name) Phase: \(.status.phase // "unknown") Instances: \(.status.instances // 0)/\(.spec.instances)"' \
      2>/dev/null || true
  done

  info "=== End Auto-Resize Namespace Diagnostics ==="
}

if [[ "${DIAGNOSE_ONLY}" == "true" ]]; then
  diagnose_volume_attachments
  diagnose_autoresize_namespaces
  print_victorialogs_hint
  exit 0
fi

# ────────────────────────────────────────────────────────────
# Pre-flight checks
# ────────────────────────────────────────────────────────────

info "=== Pre-flight Checks ==="

# 1. kubectl connectivity
info "Checking kubectl connectivity..."
if ! kubectl cluster-info >/dev/null 2>&1; then
  fail "Cannot connect to Kubernetes cluster. Check kubeconfig."
  exit 1
fi
KUBE_CONTEXT=$(kubectl config current-context)
ok "Connected to cluster via context: ${KUBE_CONTEXT}"

# 2. Node count
NODE_COUNT=$(kubectl get nodes --no-headers 2>/dev/null | wc -l)
info "Cluster has ${NODE_COUNT} nodes"
if [[ "${NODE_COUNT}" -lt 1 ]]; then
  fail "No nodes available"
  exit 1
fi

# 3. StorageClass with volume expansion
if [ -z "${E2E_DEFAULT_STORAGE_CLASS:-}" ]; then
  # Auto-detect default StorageClass
  E2E_DEFAULT_STORAGE_CLASS=$(kubectl get storageclass -o json | \
    jq -r 'first(.items[] | select(.metadata.annotations["storageclass.kubernetes.io/is-default-class"] == "true") | .metadata.name)' 2>/dev/null || echo "")
  if [ -z "${E2E_DEFAULT_STORAGE_CLASS}" ]; then
    # Fallback: try managed-csi (AKS default)
    if kubectl get storageclass managed-csi >/dev/null 2>&1; then
      E2E_DEFAULT_STORAGE_CLASS="managed-csi"
    else
      fail "No default StorageClass found. Set E2E_DEFAULT_STORAGE_CLASS."
      exit 1
    fi
  fi
fi
export E2E_DEFAULT_STORAGE_CLASS

EXPANSION_SUPPORT=$(kubectl get storageclass "${E2E_DEFAULT_STORAGE_CLASS}" -o jsonpath='{.allowVolumeExpansion}' 2>/dev/null || echo "false")
if [[ "${EXPANSION_SUPPORT}" != "true" ]]; then
  fail "StorageClass '${E2E_DEFAULT_STORAGE_CLASS}' does not support volume expansion (allowVolumeExpansion=${EXPANSION_SUPPORT})"
  warn "Fix with: kubectl patch storageclass ${E2E_DEFAULT_STORAGE_CLASS} -p '{\"allowVolumeExpansion\": true}'"
  exit 1
fi
ok "StorageClass '${E2E_DEFAULT_STORAGE_CLASS}' supports volume expansion"

# 4. Check binding mode (WaitForFirstConsumer can affect attach timing)
BINDING_MODE=$(kubectl get storageclass "${E2E_DEFAULT_STORAGE_CLASS}" -o jsonpath='{.volumeBindingMode}' 2>/dev/null || echo "unknown")
info "Volume binding mode: ${BINDING_MODE}"
if [[ "${BINDING_MODE}" == "WaitForFirstConsumer" ]]; then
  warn "WaitForFirstConsumer mode — PVs won't provision until pods are scheduled."
  warn "This can contribute to volume attach delays on AKS."
fi

# 5. Check Azure Disk CSI driver health
CSI_PODS_READY=$(kubectl get pods -n kube-system -l app=csi-azuredisk-node --no-headers 2>/dev/null | grep -c Running || echo "0")
if [[ "${CSI_PODS_READY}" -gt 0 ]]; then
  ok "Azure Disk CSI driver: ${CSI_PODS_READY} node pods running"
else
  warn "Azure Disk CSI node driver pods not found — may be using a different driver"
fi

info "=== Pre-flight Checks Complete ==="
echo

# ────────────────────────────────────────────────────────────
# Build and push (multi-arch: amd64 + arm64)
# ────────────────────────────────────────────────────────────

if [[ "${SKIP_BUILD}" != "true" ]]; then
  info "=== Building and Pushing Multi-Arch Controller Image ==="
  info "Image: ${CONTROLLER_IMG}"
  info "Platforms: linux/amd64, linux/arm64"

  # The Makefile's docker-build target forces single-arch via --single-target
  # and --set=*.platform="linux/${ARCH}". For AKS clusters with mixed node
  # architectures (amd64 + arm64), we need a multi-arch image.
  #
  # Strategy:
  #   1. Build Go binaries for BOTH architectures using go-releaser
  #      (without --single-target, it builds all goarch targets from .goreleaser.yml)
  #   2. Run docker buildx bake WITHOUT --set=*.platform override, so it uses
  #      both platforms from docker-bake.hcl (line 84: ["linux/amd64", "linux/arm64"])
  #
  # This produces a multi-arch manifest list pushed to the registry.

  # Resolve go-releaser (same logic as Makefile's go-releaser target)
  GO_RELEASER="${ROOT_DIR}/bin/goreleaser"
  if [ ! -x "${GO_RELEASER}" ]; then
    info "Installing go-releaser..."
    make -C "${ROOT_DIR}" go-releaser
  fi

  DATE=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
  COMMIT=$(git -C "${ROOT_DIR}" rev-parse HEAD)
  VERSION=${VERSION:-}

  # Step 1: Build Go binaries for ALL architectures (amd64 + arm64)
  # Note: no --single-target flag, so go-releaser builds all goarch from .goreleaser.yml
  SNAPSHOT_FLAG=""
  if [ -z "${VERSION}" ]; then
    SNAPSHOT_FLAG="--snapshot"
  fi

  info "Building Go binaries for amd64 + arm64..."
  GOOS=linux GOPATH="$(go env GOPATH)" DATE="${DATE}" COMMIT="${COMMIT}" VERSION="${VERSION}" \
    "${GO_RELEASER}" build --skip=validate --clean ${SNAPSHOT_FLAG}

  # Step 2: Ensure a buildx builder exists that supports multi-platform
  if ! docker buildx inspect multiarch-builder >/dev/null 2>&1; then
    info "Creating multi-platform buildx builder..."
    docker buildx create --name multiarch-builder --use --platform linux/amd64,linux/arm64 || true
  else
    docker buildx use multiarch-builder 2>/dev/null || true
  fi

  # Step 3: Build and push multi-arch Docker image
  # Omit --set=*.platform to use docker-bake.hcl's default: ["linux/amd64", "linux/arm64"]
  info "Building and pushing multi-arch Docker image..."
  DOCKER_BUILDKIT=1 buildVersion="${VERSION}" revision="${COMMIT}" \
    docker buildx bake \
    --set distroless.tags="${CONTROLLER_IMG}" \
    --push distroless

  ok "Multi-arch image built and pushed: ${CONTROLLER_IMG}"

  # Verify the manifest contains both platforms
  info "Verifying multi-arch manifest..."
  if docker buildx imagetools inspect "${CONTROLLER_IMG}" 2>/dev/null | grep -qE 'linux/(amd64|arm64)'; then
    ok "Manifest verified — contains multi-arch platforms"
    docker buildx imagetools inspect "${CONTROLLER_IMG}" 2>/dev/null | grep -E 'Platform|Digest' | head -10 || true
  else
    warn "Could not verify manifest platforms — image may still work"
  fi
  echo
else
  info "Skipping build (--skip-build)"
fi

# ────────────────────────────────────────────────────────────
# Deploy operator
# ────────────────────────────────────────────────────────────

if [[ "${SKIP_DEPLOY}" != "true" ]]; then
  info "=== Deploying Operator ==="

  # Recreate the namespace to get a clean state
  kubectl delete namespace cnpg-system --ignore-not-found=true --wait=true 2>/dev/null || true
  kubectl create namespace cnpg-system

  # Deploy operator manifests
  CONTROLLER_IMG="${CONTROLLER_IMG}" \
    POSTGRES_IMAGE_NAME="${POSTGRES_IMG}" \
    PGBOUNCER_IMAGE_NAME="${PGBOUNCER_IMG}" \
    make -C "${ROOT_DIR}" deploy

  # Wait for the operator to be ready
  info "Waiting for operator deployment..."
  if ! kubectl wait --for=condition=Available --timeout=3m \
    -n cnpg-system deployments cnpg-controller-manager; then
    fail "Operator deployment not ready after 3 minutes"
    kubectl get pods -n cnpg-system
    kubectl describe deployment -n cnpg-system cnpg-controller-manager
    exit 1
  fi
  ok "Operator deployed and ready"
  echo
else
  info "Skipping deploy (--skip-deploy)"
  # Verify operator is running
  if ! kubectl get deployment -n cnpg-system cnpg-controller-manager >/dev/null 2>&1; then
    fail "Operator not found in cnpg-system namespace. Run without --skip-deploy."
    exit 1
  fi
fi

# ────────────────────────────────────────────────────────────
# Build kubectl-cnpg plugin
# ────────────────────────────────────────────────────────────

info "Building kubectl-cnpg plugin..."
make -C "${ROOT_DIR}" build-plugin
export PATH=${ROOT_DIR}/bin/:${PATH}

# ────────────────────────────────────────────────────────────
# Install ginkgo
# ────────────────────────────────────────────────────────────

notinpath () {
  case "$PATH" in
    *:$1:* | *:$1 | $1:*) return 1 ;;
    *) return 0 ;;
  esac
}

go_bin="$(go env GOPATH)/bin"
if notinpath "${go_bin}"; then
  export PATH="${go_bin}:${PATH}"
fi

# renovate: datasource=github-releases depName=onsi/ginkgo
go install github.com/onsi/ginkgo/v2/ginkgo@v2.28.1

# ────────────────────────────────────────────────────────────
# Run auto-resize E2E tests
# ────────────────────────────────────────────────────────────

info "=== Running Auto-Resize E2E Tests ==="
info "StorageClass: ${E2E_DEFAULT_STORAGE_CLASS}"
info "Ginkgo nodes: ${GINKGO_NODES}"
info "Ginkgo timeout: ${GINKGO_TIMEOUT}"
info "Label filter: auto-resize"
if [ -n "${GINKGO_FOCUS}" ]; then
  info "Focus filter: ${GINKGO_FOCUS}"
fi

# ────────────────────────────────────────────────────────────
# Pre-test cleanup: remove orphaned autoresize namespaces and stale VolumeAttachments
# ────────────────────────────────────────────────────────────

info "=== Pre-test Cleanup ==="

# Delete orphaned namespaces from prior test runs.
# These leave behind PVCs with Azure Disks attached, which saturate
# the attach queue and cause timeouts for new tests.
# Clean up: autoresize-* (test namespaces) and minio (object store for backup tests)
ORPHAN_NS=$(kubectl get namespaces -o name 2>/dev/null | grep -E 'autoresize-|^namespace/minio$' | cut -d/ -f2 || true)
if [ -n "${ORPHAN_NS}" ]; then
  warn "Found orphaned namespaces from prior runs:"
  echo "${ORPHAN_NS}"
  info "Deleting orphaned namespaces (this frees their Azure Disks)..."
  for ns in ${ORPHAN_NS}; do
    kubectl delete namespace "${ns}" --wait=false 2>/dev/null || true
  done
  # Wait briefly for namespace termination to start releasing disks
  info "Waiting 30s for Azure Disk detach operations to begin..."
  sleep 30
else
  ok "No orphaned test namespaces found"
fi

# Check for stale VolumeAttachments (attached=false for deleted PVCs)
STALE_VA=$(kubectl get volumeattachments -o json 2>/dev/null | \
  jq -r '.items[] | select(.status.attached == false) | .metadata.name' || true)
if [ -n "${STALE_VA}" ]; then
  warn "Found $(echo "${STALE_VA}" | wc -l) stale VolumeAttachment(s) (attached=false)"
  info "These may slow down new volume operations. Kubernetes will garbage-collect them."
fi

ok "Pre-test cleanup complete"
echo

# Export required env vars
export TEST_CLOUD_VENDOR="aks"
export TEST_SKIP_UPGRADE=true
export E2E_PRE_ROLLING_UPDATE_IMG=${E2E_PRE_ROLLING_UPDATE_IMG:-${POSTGRES_IMG%.*}}
export AZURE_STORAGE_ACCOUNT=${AZURE_STORAGE_ACCOUNT:-''}

# Set increased timeouts for AKS.
# Azure Disk attach/detach is significantly slower than local storage:
#   - Single disk attach: 30s-5m depending on queue depth
#   - WAL tests create 2 PVCs × 3 instances = 6 disks
#   - With attach contention, cluster creation can take 10-15 min
# Default ClusterIsReady is 600s (10 min) — bump to 900s (15 min)
# Default ClusterIsReadySlow is 800s (~13 min) — bump to 1200s (20 min)
if [ -z "${TEST_TIMEOUTS:-}" ]; then
  export TEST_TIMEOUTS='{"clusterIsReady":900,"clusterIsReadySlow":1200}'
  info "Using AKS-tuned timeouts: ClusterIsReady=900s, ClusterIsReadySlow=1200s"
else
  info "Using user-provided TEST_TIMEOUTS: ${TEST_TIMEOUTS}"
fi

# Unset DEBUG to prevent k8s client spam
unset DEBUG 2>/dev/null || true

mkdir -p "${ROOT_DIR}/tests/e2e/out"

RC_GINKGO=0
# Build ginkgo focus flag if specified.
# --focus filters specs by regex on their description (Context + It text).
FOCUS_FLAGS=()
if [ -n "${GINKGO_FOCUS}" ]; then
  FOCUS_FLAGS=(--focus "${GINKGO_FOCUS}")
fi

# Run with --nodes=1 (sequential) by default to avoid resource contention on
# Azure Disk volume attachments. Each test creates a cluster with PVCs, and
# Azure Disk can only process a limited number of attach/detach operations
# concurrently per node. Running tests in parallel saturates this queue and
# causes timeouts during cluster creation.
#
# To run in parallel (faster, but may hit attach timeouts on small clusters):
#   GINKGO_NODES=3 hack/e2e/run-e2e-aks-autoresize.sh --skip-build --skip-deploy
#
# Parallel is safe when:
#   - The AKS cluster has 3+ nodes (spreads disk operations across nodes)
#   - Tests are not all creating PVCs simultaneously
#   - Webhook-only tests (no PVCs) are being run
ginkgo --nodes="${GINKGO_NODES}" \
       --timeout "${GINKGO_TIMEOUT}" \
       --poll-progress-after=1200s \
       --poll-progress-interval=150s \
       --label-filter "auto-resize" \
       "${FOCUS_FLAGS[@]+"${FOCUS_FLAGS[@]}"}" \
       --force-newlines \
       --output-dir "${ROOT_DIR}/tests/e2e/out/" \
       --json-report "autoresize_report.json" \
       -v "${ROOT_DIR}/tests/e2e/..." || RC_GINKGO=$?

# ────────────────────────────────────────────────────────────
# Report results
# ────────────────────────────────────────────────────────────

echo
info "=== Test Results ==="

RC=0
if [ -f "${ROOT_DIR}/tests/e2e/out/autoresize_report.json" ]; then
  jq -e -c -f "${ROOT_DIR}/hack/e2e/test-report.jq" \
    "${ROOT_DIR}/tests/e2e/out/autoresize_report.json" || RC=$?
fi

if [[ $RC_GINKGO != 0 ]]; then
  fail "Some tests failed (ginkgo exit code: ${RC_GINKGO})"
  echo
  warn "Running diagnostics..."
  diagnose_volume_attachments
  diagnose_autoresize_namespaces
  print_victorialogs_hint
else
  ok "All auto-resize E2E tests passed!"
fi

exit $RC
