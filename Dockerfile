ARG BASE=gcr.io/distroless/static-debian13:nonroot@sha256:aafe993665bf6200db38a02c11a9351e782a84b6a45e7f33b6695638c88e3931

# This builder stage it's only because we need a command
# to create a symlink and we do not have it in a distroless image
FROM gcr.io/distroless/static-debian13:debug-nonroot@sha256:7df9fac2f5d3927260696af33fc2391da26366825838a47337088ed78f854a0c AS builder
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
