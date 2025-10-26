ARG BASE=gcr.io/distroless/static-debian12:nonroot@sha256:e8a4044e0b4ae4257efa45fc026c0bc30ad320d43bd4c1a7d5271bd241e386d0

# This builder stage it's only because we need a command
# to create a symlink and we do not have it in a distroless image
FROM gcr.io/distroless/static-debian12:debug-nonroot@sha256:7d3273ed75e3c6b4a159e215dd30187b856fdfdb3266ec7777a3fce51cecccfe AS builder
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
