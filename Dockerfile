ARG BASE=gcr.io/distroless/static-debian12:nonroot@sha256:627d6c5a23ad24e6bdff827f16c7b60e0289029b0c79e9f7ccd54ae3279fb45f

# This builder stage it's only because we need a command
# to create a symlink and we do not have it in a distroless image
FROM gcr.io/distroless/static-debian12:debug-nonroot@sha256:0895d6fc256a6938a60c87d92e1148eec0d36198bff9c5d3082e6a56db7756bd AS builder
ARG TARGETARCH
SHELL ["/busybox/sh", "-c"]
RUN ln -sf operator/manager_${TARGETARCH} manager

FROM ${BASE}
WORKDIR /
COPY --chown=nonroot:nonroot --chmod=0755 dist/manager/* operator/
COPY --from=builder /home/nonroot/ .
COPY licenses /licenses
COPY LICENSE /licenses
USER 65532:65532
ENTRYPOINT ["/manager"]
