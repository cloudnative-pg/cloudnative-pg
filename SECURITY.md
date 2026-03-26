# Security Policy

Vulnerability: CVE-2026-33186 in gRPC-Go dependency

Description

A vulnerability has been identified in gRPC-Go (CVE-2026-33186), affecting versions prior to v1.79.3.

The issue allows a potential authorization bypass due to improper validation of the HTTP/2 :path pseudo-header. Specifically, the gRPC-Go server accepts malformed requests where the :path does not include the required leading /, which may result in incorrect routing and unintended access.

Affected Version

CloudNativePG image: ghcr.io/cloudnative-pg/cloudnative-pg:1.28.1
Likely affected versions: Any release using gRPC-Go < v1.79.3

Impact

Potential authorization bypass in gRPC server routing
May allow malformed requests to reach unintended handlers
Severity: High (depending on exposure of gRPC endpoints)

Reproduction / Detection

Detected via Black Duck scan during container image upload
Vulnerability flagged due to usage of vulnerable gRPC-Go version

Expected Behavior

The project should depend on a patched version of gRPC-Go:

google.golang.org/grpc >= v1.79.3
