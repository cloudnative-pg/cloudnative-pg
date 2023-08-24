# Backup and Recovery

Until CloudNativePG 1.20, this page used to contain both the backup and
recovery phases of a PostgreSQL cluster. The reason was that CloudNativePG
supported only backup and recovery object stores.

Version 1.21 introduces support for the Kubernetes `VolumeSnapshot` API,
providing more possibilities for the end user.

As a result, [backup](backup.md) and [recovery](recovery.md) are now in two
separate sections.
