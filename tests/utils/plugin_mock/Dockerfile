FROM golang:1.24 as builder

WORKDIR /workspace

# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum

# cache deps
RUN go mod download

# Copy the go source
COPY tests/utils/plugin_mock/main.go main.go

# Build
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -o plugin-mock main.go

FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=builder /workspace/plugin-mock .
USER 65532:65532

ENTRYPOINT ["/plugin-mock"]
