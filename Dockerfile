# This builder stage it's only because we need a command
# to create a symlink and reduce the size of the image
FROM gcr.io/distroless/static-debian11:debug-nonroot as builder
ARG TARGETARCH

SHELL ["/busybox/sh", "-c"]
COPY --chown=nonroot:nonroot --chmod=0755 dist/manager/* bin/
RUN ln -sf bin/manager_${TARGETARCH} manager

FROM gcr.io/distroless/static-debian11:nonroot
ARG VERSION="dev"
ARG TARGETARCH

ENV SUMMARY="CloudNativePG Operator Container Image." \
    DESCRIPTION="This Docker image contains CloudNativePG Operator."

LABEL summary="$SUMMARY" \
      description="$DESCRIPTION" \
      io.k8s.display-name="$SUMMARY" \
      io.k8s.description="$DESCRIPTION" \
      name="CloudNativePG Operator" \
      vendor="CloudNativePG Contributors" \
      url="https://cloudnative-pg.io/" \
      version="$VERSION" \
      release="1"

WORKDIR /

# Needs to copy the entire content, otherwise, it will not
# copy the symlink properly.
COPY --from=builder /home/nonroot/ .
USER 65532:65532

ENTRYPOINT ["/manager"]
