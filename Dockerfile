# Use UBI Minimal image as base https://developers.redhat.com/products/rhel/ubi
FROM registry.access.redhat.com/ubi8/ubi-minimal
ARG VERSION="dev"
ARG TARGETARCH

ENV SUMMARY="Cloud Native PostgreSQL Operator Container Image." \
    DESCRIPTION="This Docker image contains Cloud Native PostgreSQL Operator \
based on RedHat Universal Base Images (UBI) 8."

RUN microdnf update && microdnf clean all

# TODO - automate version?
# For Certified Operator Image Dockerfile labels and license(s) are required
# See: https://redhat-connect.gitbook.io/certified-operator-guide/helm-operators/building-a-helm-operator/dockerfile-requirements-helm
LABEL summary="$SUMMARY" \
      description="$DESCRIPTION" \
      io.k8s.display-name="$SUMMARY" \
      io.k8s.description="$DESCRIPTION" \
      name="Cloud Native PostgreSQL Operator" \
      vendor="EnterpriseDB Corporation" \
      url="https://www.enterprisedb.com/" \
      version="$VERSION" \
      release="1"

COPY licenses /licenses
COPY LICENSE /licenses

WORKDIR /
COPY dist/manager_linux_${TARGETARCH}*/manager .
USER 1001

ENTRYPOINT ["/manager"]
