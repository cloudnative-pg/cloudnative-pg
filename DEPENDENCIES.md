# Dependency Management Policy

## Overview

CloudNativePG (CNPG) is committed to maintaining a secure and stable software
supply chain. As an operator managing critical database workloads, the
integrity of our dependencies, ranging from Go modules to container base
images, is paramount.

**This document outlines our policies for selecting, monitoring, and updating
third-party dependencies.**

## Selection Criteria

Before introducing a new dependency to the project, maintainers must evaluate
it against the following criteria:

- **Necessity:** Does the dependency provide essential functionality that
  cannot be reasonably implemented within the project?
- **Security Posture:** Preference is given to projects with high OpenSSF
  Scorecard ratings and a clear security policy.
- **Maintenance:** The dependency must be actively maintained with a history of
  timely security patches.
- **Licensing:** All dependencies must comply with the Apache License 2.0 or a
  compatible permissive license (e.g. MIT, BSD-3-Clause).

## Automated Monitoring and Scanning

We employ a "defense in depth" approach to monitoring our dependency tree
through automated tooling:

- **Update Automation:** GitHub's built-in
  [Dependabot](https://github.com/dependabot) alerts notify us of known
  vulnerabilities in our dependencies. We use
  [Renovate](https://github.com/renovatebot/renovate) for granular version
  management, automated grouping of updates, and maintenance of GitHub Actions.
- **Vulnerability Scanning:** Every Pull Request and push to the main branch
  triggers automated scans using [Snyk](https://snyk.io/) and
  [govulncheck](https://pkg.go.dev/golang.org/x/vuln/cmd/govulncheck) to identify
  known vulnerabilities (CVEs).
- **Static Analysis:** We rely on [CodeQL](https://codeql.github.com/) and
  [golangci-lint](https://github.com/golangci/golangci-lint) (which includes the
  [gosec](https://github.com/securego/gosec) security linter) to identify security
  risks introduced by the way dependencies are utilized in our code.
- **Container Hardening:** We run
  [Dockle](https://github.com/goodwithtech/dockle) to lint our container images
  against security best practices, ensuring that our images are lean, do not run
  as `root`, and do not contain sensitive information in their history.

## Supply Chain Integrity

To ensure that the code we build is the code we intended to use, we implement
the following:

- **Checksum Verification:** We use [`go.sum`](./go.sum) to ensure the
  integrity of Go modules.
- **Artifact Signing:** All container images are cryptographically signed using
  [Cosign](https://github.com/sigstore/cosign).
- **SLSA Provenance:** We generate SLSA-formatted provenance attestations for
  all releases. These are delivered as OCI attestations integrated into our
  build process via BuildKit (`buildType: https://mobyproject.org/buildkit@v1`).
- **Software Bill of Materials (SBOM):** We provide a comprehensive SBOM for
  every image, allowing users to verify the entire bill of materials for any
  given version.

## Remediation Cadence

Security updates are treated as high-priority tasks. The project aims for the
following remediation timeframes:

- **Critical and High Vulnerabilities:** Once a fix is available in the
  upstream dependency, we aim to merge the update and initiate a release
  process within one week.
- **Medium and Low Vulnerabilities:** These updates are typically batched and
  addressed as part of our regular maintenance and release cycles once upstream
  fixes are provided.

## Contact

For security-related concerns regarding our dependencies, please refer to our
[`SECURITY.md` file](./SECURITY.md).
