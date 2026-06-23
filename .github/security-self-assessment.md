# CloudNativePG Security Self-Assessment

This document is the entry point for the CloudNativePG security
self-assessment. It is referenced from
[`SECURITY-INSIGHTS.yml`](../SECURITY-INSIGHTS.yml) as the project's
self-assessment evidence.

The assessment follows the [Gemara](https://github.com/ossf/gemara) model
(version 1.2.0) and maps capabilities and threats to the FINOS
[Common Cloud Controls (CCC) Core](https://github.com/finos/common-cloud-controls)
v2025.10. It is split across two machine-readable catalogs:

- [Threat Catalog](threat-catalog.yaml) (`CNPG.THREATS`): threats specific
  to operating PostgreSQL clusters on Kubernetes with the CloudNativePG
  operator, each mapped to the capabilities it affects and to CCC Core.
- [Capability Catalog](capability-catalog.yaml) (`CNPG.CAPABILITIES`):
  capabilities the operator provides for managing PostgreSQL clusters,
  grouped by availability, security, and data management.

Both catalogs are versioned together and are the authoritative source of
the assessment. This page exists because the Security Insights schema
records a single self-assessment evidence URL, while the assessment itself
is maintained as the two catalogs above.
