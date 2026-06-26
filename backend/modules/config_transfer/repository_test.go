package configtransfer

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"io"
	"strings"
	"sync"
	"testing"
)

const slugCheckTestDriverName = "config_transfer_slug_check_driver"

var slugCheckDriverState = struct {
	sync.Mutex
	query string
	args  []driver.Value
	taken bool
	err   error
}{}

func init() {
	sql.Register(slugCheckTestDriverName, slugCheckTestDriver{})
}

func TestPublicSlugTakenSupportsEmptyOnboardingSalonID(t *testing.T) {
	db, err := sql.Open(slugCheckTestDriverName, "")
	if err != nil {
		t.Fatalf("sql.Open returned error: %v", err)
	}
	defer db.Close()

	slugCheckDriverState.Lock()
	slugCheckDriverState.query = ""
	slugCheckDriverState.args = nil
	slugCheckDriverState.taken = false
	slugCheckDriverState.err = nil
	slugCheckDriverState.Unlock()

	taken, err := NewRepository(db).PublicSlugTaken(context.Background(), "", "lotus-nails")
	if err != nil {
		t.Fatalf("PublicSlugTaken returned error: %v", err)
	}
	if taken {
		t.Fatalf("taken = true, want false")
	}

	slugCheckDriverState.Lock()
	query := slugCheckDriverState.query
	args := append([]driver.Value(nil), slugCheckDriverState.args...)
	slugCheckDriverState.Unlock()

	if strings.Contains(query, "id <> $2") {
		t.Fatalf("query uses UUID comparison that fails for onboarding salon id: %s", query)
	}
	if !strings.Contains(query, "id::text <> $2") {
		t.Fatalf("query should compare salon id as text for onboarding-safe checks: %s", query)
	}
	if len(args) != 2 || args[0] != "lotus-nails" || args[1] != "" {
		t.Fatalf("query args = %#v, want slug and empty salon id", args)
	}
}

type slugCheckTestDriver struct{}

func (slugCheckTestDriver) Open(name string) (driver.Conn, error) {
	return slugCheckTestConn{}, nil
}

type slugCheckTestConn struct{}

func (slugCheckTestConn) Prepare(query string) (driver.Stmt, error) {
	return slugCheckTestStmt{query: query}, nil
}

func (slugCheckTestConn) Close() error {
	return nil
}

func (slugCheckTestConn) Begin() (driver.Tx, error) {
	return slugCheckTestTx{}, nil
}

type slugCheckTestStmt struct {
	query string
}

func (s slugCheckTestStmt) Close() error {
	return nil
}

func (s slugCheckTestStmt) NumInput() int {
	return -1
}

func (s slugCheckTestStmt) Exec(args []driver.Value) (driver.Result, error) {
	return nil, nil
}

func (s slugCheckTestStmt) Query(args []driver.Value) (driver.Rows, error) {
	slugCheckDriverState.Lock()
	defer slugCheckDriverState.Unlock()
	slugCheckDriverState.query = s.query
	slugCheckDriverState.args = append([]driver.Value(nil), args...)
	if slugCheckDriverState.err != nil {
		return nil, slugCheckDriverState.err
	}
	return &slugCheckTestRows{taken: slugCheckDriverState.taken}, nil
}

type slugCheckTestRows struct {
	sent  bool
	taken bool
}

func (r *slugCheckTestRows) Columns() []string {
	return []string{"exists"}
}

func (r *slugCheckTestRows) Close() error {
	return nil
}

func (r *slugCheckTestRows) Next(dest []driver.Value) error {
	if r.sent {
		return io.EOF
	}
	dest[0] = r.taken
	r.sent = true
	return nil
}

type slugCheckTestTx struct{}

func (slugCheckTestTx) Commit() error {
	return nil
}

func (slugCheckTestTx) Rollback() error {
	return nil
}
