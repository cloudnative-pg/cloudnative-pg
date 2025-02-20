ARG BASE=gcr.io/distroless/static-debian12:nonroot

# This builder stage it's only because we need a command
# to create a symlink and reduce the size of the image
FROM gcr.io/distroless/static-debian12:debug-nonroot AS builder
ARG TARGETARCH
SHELL ["/busybox/sh", "-c"]
COPY --chown=nonroot:nonroot --chmod=0755 dist/manager/* operator/
RUN ln -sf operator/manager_${TARGETARCH} manager

FROM ${BASE} AS data
WORKDIR /
# Needs to copy the entire content, otherwise, it will not
# copy the symlink properly.
COPY --from=builder /home/nonroot/ .
USER 65532:65532
ENTRYPOINT ["/manager"]

FROM data AS distroless

FROM data AS ubi
USER root
COPY licenses /licenses
COPY LICENSE /licenses
USER 65532:65532
