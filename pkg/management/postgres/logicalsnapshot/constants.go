package logicalsnapshot

import "fmt"

type (
	executable = string
)

const (
	pgDump           executable = "pg_dump"
	pgRestore        executable = "pg_restore"
	postgresDatabase            = "postgres"
)

func generateFileNameForDatabase(database string) string {
	return fmt.Sprintf("/var/lib/postgresql/data/%s.dump", database)
}
