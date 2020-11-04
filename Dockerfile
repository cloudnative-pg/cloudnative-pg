# Build the manager binary
FROM registry.access.redhat.com/ubi8/go-toolset:1.13.4 as builder

# We do not use root
USER 1001

# Copy the Go Modules manifests
COPY --chown=1001 go.mod /workspace/go.mod
COPY --chown=1001 go.sum /workspace/go.sum

# Set the WORKDIR after the first COPY, otherwise the directory will be owned by root
WORKDIR /workspace

# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download

# Copy the go source
COPY --chown=1001 . /workspace

# Build
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GO111MODULE=on go build -a -o manager ./cmd/manager

# Use UBI Minimal image as base https://developers.redhat.com/products/rhel/ubi
FROM registry.access.redhat.com/ubi8/ubi-minimal

ENV SUMMARY="Cloud Native PostgreSQL Operator Container Image." \
    DESCRIPTION="This Docker image contains Cloud Native PostgreSQL Operator \
based on RedHat Universal Base Images (UBI) 8."

# TODO - automate version?
# For Certified Operator Image Dockerfile labels and license(s) are required
# See: https://redhat-connect.gitbook.io/certified-operator-guide/helm-operators/building-a-helm-operator/dockerfile-requirements-helm
LABEL summary="$SUMMARY" \
      description="$DESCRIPTION" \
      io.k8s.display-name="$SUMMARY" \
      io.k8s.description="$DESCRIPTION" \
      name="Cloud Native PostgreSQL Operator" \
      vendor="2ndQuadrant" \
      url="https://www.2ndquadrant.com/" \
      version="0.4.0" \
      release="1"

COPY licenses /licenses
COPY LICENSE /licenses

WORKDIR /
COPY --from=builder /workspace/manager .
USER 1001

ENTRYPOINT ["/manager"]
