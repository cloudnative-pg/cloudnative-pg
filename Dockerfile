FROM artifactory-gcp.netskope.io/pe-docker/ns-ubuntu-2004-fips:latest 
ARG VERSION="netskope-1.0.0"
ARG TARGETARCH="amd64"

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

USER root
WORKDIR /

COPY dist/manager_linux_amd64/manager /
RUN chmod 755 /manager

USER nobody 
ENTRYPOINT ["/manager"]
