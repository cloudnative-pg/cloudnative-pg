package logicalimport

import (
	"database/sql"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestLogicalimport(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "logicalimport test suite")
}

type fakePooler struct {
	db *sql.DB
}

func (f fakePooler) Connection(_ string) (*sql.DB, error) {
	return f.db, nil
}

func (f fakePooler) GetDsn(dbName string) string {
	return dbName
}

func (f fakePooler) ShutdownConnections() {
}
