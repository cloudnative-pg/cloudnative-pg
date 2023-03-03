FROM gcr.io/distroless/static:nonroot
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

USER nonroot:nonroot

COPY --chown=nonroot:nonroot --chmod=0755 dist/manager_linux_${TARGETARCH}*/manager .

ENTRYPOINT ["/manager"]
