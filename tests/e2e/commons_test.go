/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package e2e

const (
	minioClientName = "mc"
	switchWalCmd    = "psql -U postgres app -tAc 'CHECKPOINT; SELECT pg_walfile_name(pg_switch_wal())'"
)
