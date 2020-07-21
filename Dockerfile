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

# Use distroless as minimal base image to package the manager binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM registry.access.redhat.com/ubi8/ubi-minimal
WORKDIR /
COPY --from=builder /workspace/manager .
USER 1001

ENTRYPOINT ["/manager"]
