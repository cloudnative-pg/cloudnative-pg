# Build the manager binary
FROM registry.access.redhat.com/ubi8/go-toolset:1.15.7-11 as builder

# We do not use root
USER 1001

# Copy the Go Modules manifests
COPY --chown=1001 go.mod /workspace/go.mod
COPY --chown=1001 go.sum /workspace/go.sum

# Set the WORKDIR after the first COPY, otherwise the directory will be owned by root
WORKDIR /workspace

# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN CGO_ENABLED=0 go mod download

# Copy the go source
COPY --chown=1001 . /workspace

ARG VERSION="dev"
ARG COMMIT="none"
ARG DATE="unknown"

# Build
RUN CGO_ENABLED=0 GO111MODULE=on go build -a -o manager -ldflags \
    "-s -w \
    -X github.com/EnterpriseDB/cloud-native-postgresql/pkg/versions.buildVersion=\"$VERSION\" \
    -X github.com/EnterpriseDB/cloud-native-postgresql/pkg/versions.buildCommit=\"$COMMIT\" \
    -X github.com/EnterpriseDB/cloud-native-postgresql/pkg/versions.buildDate=\"$DATE\"" \
    ./cmd/manager

# Use UBI Minimal image as base https://developers.redhat.com/products/rhel/ubi
FROM registry.access.redhat.com/ubi8/ubi-minimal
ARG VERSION="dev"

ENV SUMMARY="Cloud Native PostgreSQL Operator Container Image." \
    DESCRIPTION="This Docker image contains Cloud Native PostgreSQL Operator \
based on RedHat Universal Base Images (UBI) 8."

RUN microdnf update && microdnf clean all

# TODO - automate version?
# For Certified Operator Image Dockerfile labels and license(s) are required
# See: https://redhat-connect.gitbook.io/certified-operator-guide/helm-operators/building-a-helm-operator/dockerfile-requirements-helm
LABEL summary="$SUMMARY" \
      description="$DESCRIPTION" \
      io.k8s.display-name="$SUMMARY" \
      io.k8s.description="$DESCRIPTION" \
      name="Cloud Native PostgreSQL Operator" \
      vendor="EnterpriseDB Corporation" \
      url="https://www.enterprisedb.com/" \
      version="$VERSION" \
      release="1"

COPY licenses /licenses
COPY LICENSE /licenses

WORKDIR /
COPY --from=builder /workspace/manager .
USER 1001

ENTRYPOINT ["/manager"]
