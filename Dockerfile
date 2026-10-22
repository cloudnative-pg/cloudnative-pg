ARG BASE=gcr.io/distroless/static-debian13:nonroot@sha256:d62067879de890a1def9da0e7766b7d20810a5e5555cef8506b752437d7a3e88

# This builder stage it's only because we need a command
# to create a symlink and we do not have it in a distroless image
FROM gcr.io/distroless/static-debian13:debug-nonroot@sha256:06bfc1ed6f52f8efecd4e68e5f146fc06891b1c40f2d1654f1518392ba89815a AS builder
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
