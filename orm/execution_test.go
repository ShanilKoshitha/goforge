package orm_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/ShanilKoshitha/goforge/orm"
)

type recordedCall struct {
	query string
	args  []any
}

type driverScript struct {
	mu           sync.Mutex
	columns      []string
	rows         [][]driver.Value
	queryErr     error
	nextErr      error
	closeErr     error
	execErr      error
	rowsAffected int64
	queries      []recordedCall
	executions   []recordedCall
	closedRows   atomic.Int64
	commits      atomic.Int64
	rollbacks    atomic.Int64
}

type scriptedDriver struct{ script *driverScript }

func (value scriptedDriver) Open(string) (driver.Conn, error) {
	return &scriptedConn{script: value.script}, nil
}

type scriptedConn struct{ script *driverScript }

func (connection *scriptedConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepare is unsupported")
}
func (connection *scriptedConn) Close() error { return nil }
func (connection *scriptedConn) Begin() (driver.Tx, error) {
	return connection.BeginTx(context.Background(), driver.TxOptions{})
}
func (connection *scriptedConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return &scriptedTx{script: connection.script}, nil
}
func (connection *scriptedConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	connection.script.mu.Lock()
	defer connection.script.mu.Unlock()
	connection.script.queries = append(connection.script.queries, recordedCall{query: query, args: namedValues(args)})
	if connection.script.queryErr != nil {
		return nil, connection.script.queryErr
	}
	rows := make([][]driver.Value, len(connection.script.rows))
	for index := range connection.script.rows {
		rows[index] = append([]driver.Value(nil), connection.script.rows[index]...)
	}
	return &scriptedRows{
		columns: append([]string(nil), connection.script.columns...), rows: rows,
		nextErr: connection.script.nextErr, closeErr: connection.script.closeErr,
		closed: &connection.script.closedRows,
	}, nil
}
func (connection *scriptedConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	connection.script.mu.Lock()
	defer connection.script.mu.Unlock()
	connection.script.executions = append(connection.script.executions, recordedCall{query: query, args: namedValues(args)})
	if connection.script.execErr != nil {
		return nil, connection.script.execErr
	}
	return driver.RowsAffected(connection.script.rowsAffected), nil
}

type scriptedTx struct{ script *driverScript }

func (transaction *scriptedTx) Commit() error {
	transaction.script.commits.Add(1)
	return nil
}
func (transaction *scriptedTx) Rollback() error {
	transaction.script.rollbacks.Add(1)
	return nil
}

type scriptedRows struct {
	columns  []string
	rows     [][]driver.Value
	index    int
	nextErr  error
	errSent  bool
	closeErr error
	closed   *atomic.Int64
}

func (rows *scriptedRows) Columns() []string { return rows.columns }
func (rows *scriptedRows) Close() error {
	rows.closed.Add(1)
	return rows.closeErr
}
func (rows *scriptedRows) Next(destination []driver.Value) error {
	if rows.index < len(rows.rows) {
		copy(destination, rows.rows[rows.index])
		rows.index++
		return nil
	}
	if rows.nextErr != nil && !rows.errSent {
		rows.errSent = true
		return rows.nextErr
	}
	return io.EOF
}

func namedValues(values []driver.NamedValue) []any {
	result := make([]any, len(values))
	for index, value := range values {
		result[index] = value.Value
	}
	return result
}

var scriptSequence atomic.Uint64

func openScriptedDB(t *testing.T, script *driverScript) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("orm-script-%d", scriptSequence.Add(1))
	sql.Register(name, scriptedDriver{script: script})
	db, err := sql.Open(name, "")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestSelectExecutesMapsAndClosesRows(t *testing.T) {
	script := &driverScript{
		columns: []string{"id", "name", "score", "payload"},
		rows: [][]driver.Value{
			{int64(1), "one", int64(10), []byte("a")},
			{int64(2), "two", int64(20), []byte("b")},
		},
	}
	db := openScriptedDB(t, script)
	items, err := orm.Select(widgetTable).Where(widgetID.Gt(0)).OrderBy(widgetID.Asc()).All(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	want := []widget{{1, "one", 10, []byte("a")}, {2, "two", 20, []byte("b")}}
	if !reflect.DeepEqual(items, want) {
		t.Fatalf("items = %#v, want %#v", items, want)
	}
	if script.closedRows.Load() == 0 {
		t.Fatal("rows were not closed")
	}
	if len(script.queries) != 1 || !reflect.DeepEqual(script.queries[0].args, []any{int64(0)}) {
		t.Fatalf("query calls = %#v", script.queries)
	}
}

func TestFirstCountAndExists(t *testing.T) {
	firstScript := &driverScript{
		columns: []string{"id", "name", "score", "payload"},
		rows:    [][]driver.Value{{int64(4), "first", int64(1), []byte(nil)}},
	}
	first, err := orm.Select(widgetTable).OrderBy(widgetID.Asc()).First(context.Background(), openScriptedDB(t, firstScript))
	if err != nil || first.ID != 4 {
		t.Fatalf("First = %#v, %v", first, err)
	}
	if !stringsContains(firstScript.queries[0].query, " LIMIT $1") {
		t.Fatalf("First query lacks limit: %s", firstScript.queries[0].query)
	}

	emptyScript := &driverScript{columns: []string{"id", "name", "score", "payload"}}
	_, err = orm.Select(widgetTable).First(context.Background(), openScriptedDB(t, emptyScript))
	if !errors.Is(err, orm.ErrNotFound) {
		t.Fatalf("empty First error = %v", err)
	}
	var persistence *orm.PersistenceError
	if !errors.As(err, &persistence) || persistence.Operation != orm.OperationSelect || persistence.Table != "widgets" {
		t.Fatalf("empty First persistence error = %#v", persistence)
	}

	countScript := &driverScript{columns: []string{"count"}, rows: [][]driver.Value{{int64(7)}}}
	count, err := orm.Select(widgetTable).Where(widgetID.Gt(0)).Count(context.Background(), openScriptedDB(t, countScript))
	if err != nil || count != 7 {
		t.Fatalf("Count = %d, %v", count, err)
	}
	existsScript := &driverScript{columns: []string{"exists"}, rows: [][]driver.Value{{true}}}
	exists, err := orm.Select(widgetTable).Where(widgetID.Eq(4)).Exists(context.Background(), openScriptedDB(t, existsScript))
	if err != nil || !exists {
		t.Fatalf("Exists = %v, %v", exists, err)
	}
}

func TestExecutionPreservesCancellationAndDriverErrors(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := orm.Select(widgetTable).All(ctx, openScriptedDB(t, &driverScript{}))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation error = %v", err)
	}
	var persistence *orm.PersistenceError
	if !errors.As(err, &persistence) {
		t.Fatalf("cancellation lacks PersistenceError: %v", err)
	}

	queryErr := errors.New("query failed")
	_, err = orm.Select(widgetTable).All(context.Background(), openScriptedDB(t, &driverScript{queryErr: queryErr}))
	if !errors.Is(err, queryErr) {
		t.Fatalf("query error = %v", err)
	}
	nextErr := errors.New("rows failed")
	_, err = orm.Select(widgetTable).All(context.Background(), openScriptedDB(t, &driverScript{
		columns: []string{"id", "name", "score", "payload"}, nextErr: nextErr,
	}))
	if !errors.Is(err, nextErr) {
		t.Fatalf("rows error = %v", err)
	}
	closeErr := errors.New("close failed")
	_, err = orm.Select(widgetTable).All(context.Background(), openScriptedDB(t, &driverScript{
		columns: []string{"id", "name", "score", "payload"}, closeErr: closeErr,
	}))
	if !errors.Is(err, closeErr) {
		t.Fatalf("close error = %v", err)
	}

	type brokenModel struct{ ID int64 }
	scanErr := errors.New("scan failed")
	brokenMapper := orm.MustMapper([]string{"id"}, func(row orm.RowScanner) (brokenModel, error) {
		var value brokenModel
		if err := row.Scan(&value.ID); err != nil {
			return brokenModel{}, err
		}
		return brokenModel{}, scanErr
	})
	brokenTable := orm.MustTable("broken_models", brokenMapper)
	scanScript := &driverScript{columns: []string{"id"}, rows: [][]driver.Value{{int64(1)}}}
	_, err = orm.Select(brokenTable).All(context.Background(), openScriptedDB(t, scanScript))
	if !errors.Is(err, scanErr) || scanScript.closedRows.Load() == 0 {
		t.Fatalf("scan error/closure = %v, closes=%d", err, scanScript.closedRows.Load())
	}
}

func TestInsertUpdateDeleteExecution(t *testing.T) {
	insertScript := &driverScript{
		columns: []string{"id", "name", "score", "payload"},
		rows:    [][]driver.Value{{int64(10), "new", int64(0), []byte(nil)}},
	}
	created, err := orm.Insert(widgetTable).Values(widgetName.Set("new"), widgetScore.Set(0)).One(context.Background(), openScriptedDB(t, insertScript))
	if err != nil || created.ID != 10 {
		t.Fatalf("insert = %#v, %v", created, err)
	}
	if len(insertScript.queries) != 1 || !reflect.DeepEqual(insertScript.queries[0].args, []any{"new", int64(0)}) {
		t.Fatalf("insert query = %#v", insertScript.queries)
	}

	updateScript := &driverScript{rowsAffected: 1}
	updated, err := orm.Update(widgetTable).Set(widgetName.Set("updated")).Where(widgetID.Eq(10)).Exec(context.Background(), openScriptedDB(t, updateScript))
	if err != nil || updated != 1 {
		t.Fatalf("update = %d, %v", updated, err)
	}
	deleteScript := &driverScript{rowsAffected: 1}
	deleted, err := orm.Delete(widgetTable).Where(widgetID.Eq(10)).Exec(context.Background(), openScriptedDB(t, deleteScript))
	if err != nil || deleted != 1 {
		t.Fatalf("delete = %d, %v", deleted, err)
	}

	staleScript := &driverScript{rowsAffected: 0}
	_, err = orm.Update(widgetTable).Set(widgetName.Set("stale")).Where(widgetID.Eq(10)).StaleOnZero().Exec(context.Background(), openScriptedDB(t, staleScript))
	if !errors.Is(err, orm.ErrStale) {
		t.Fatalf("stale update error = %v", err)
	}
	execErr := errors.New("write failed")
	_, err = orm.Delete(widgetTable).Where(widgetID.Eq(10)).Exec(context.Background(), openScriptedDB(t, &driverScript{execErr: execErr}))
	if !errors.Is(err, execErr) {
		t.Fatalf("delete error = %v", err)
	}

	returningScript := &driverScript{
		columns: []string{"id", "name", "score", "payload"},
		rows:    [][]driver.Value{{int64(10), "returned", int64(1), []byte("x")}},
	}
	returned, err := orm.Update(widgetTable).Set(widgetName.Set("returned")).Where(widgetID.Eq(10)).One(context.Background(), openScriptedDB(t, returningScript))
	if err != nil || returned.Name != "returned" {
		t.Fatalf("returning update = %#v, %v", returned, err)
	}
	if len(returningScript.queries) != 1 || !stringsContains(returningScript.queries[0].query, " RETURNING ") {
		t.Fatalf("returning update query = %#v", returningScript.queries)
	}
	missingScript := &driverScript{columns: []string{"id", "name", "score", "payload"}}
	_, err = orm.Update(widgetTable).Set(widgetName.Set("missing")).Where(widgetID.Eq(10)).One(context.Background(), openScriptedDB(t, missingScript))
	if !errors.Is(err, orm.ErrNotFound) || errors.Is(err, orm.ErrStale) {
		t.Fatalf("missing update error = %v", err)
	}
	staleReturning := &driverScript{columns: []string{"id", "name", "score", "payload"}}
	_, err = orm.Update(widgetTable).Set(widgetName.Set("stale")).Where(widgetID.Eq(10)).StaleOnZero().One(context.Background(), openScriptedDB(t, staleReturning))
	if !errors.Is(err, orm.ErrStale) || errors.Is(err, orm.ErrNotFound) {
		t.Fatalf("stale returning update error = %v", err)
	}
}

func TestBulkInsertExecutionCancellationAndWrapping(t *testing.T) {
	bulk := orm.Insert(widgetTable).Rows(
		orm.Row(widgetName.Set("one"), widgetScore.Set(1)),
		orm.Row(widgetName.Set("two"), widgetScore.Set(2)),
	)
	script := &driverScript{
		columns: []string{"id", "name", "score", "payload"},
		rows: [][]driver.Value{
			{int64(1), "one", int64(1), []byte(nil)},
			{int64(2), "two", int64(2), []byte(nil)},
		},
	}
	items, err := bulk.All(context.Background(), openScriptedDB(t, script))
	if err != nil || len(items) != 2 || items[0].ID != 1 || items[1].ID != 2 {
		t.Fatalf("bulk returned = %#v, %v", items, err)
	}
	if len(script.queries) != 1 || script.closedRows.Load() == 0 ||
		!reflect.DeepEqual(script.queries[0].args, []any{"one", int64(1), "two", int64(2)}) {
		t.Fatalf("bulk query = %#v closes=%d", script.queries, script.closedRows.Load())
	}

	native := errors.New("bulk query failed")
	_, err = bulk.All(context.Background(), openScriptedDB(t, &driverScript{queryErr: native}))
	var persistence *orm.PersistenceError
	if !errors.Is(err, native) || !errors.As(err, &persistence) ||
		persistence.Operation != orm.OperationInsert || persistence.Table != widgetTable.Name() {
		t.Fatalf("bulk error wrapping = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = bulk.All(ctx, openScriptedDB(t, &driverScript{}))
	if !errors.Is(err, context.Canceled) || !errors.As(err, &persistence) || persistence.Operation != orm.OperationInsert {
		t.Fatalf("bulk cancellation = %v", err)
	}
}

func TestOptimisticVersionLeavesStaleClassificationExplicit(t *testing.T) {
	update := orm.Update(versionedTable).
		Where(versionedID.Eq(7)).
		OptimisticVersion(orm.ExpectVersion(versionedVersion, int64(3)))
	rows, err := update.Exec(context.Background(), openScriptedDB(t, &driverScript{rowsAffected: 0}))
	if err != nil || rows != 0 {
		t.Fatalf("unclassified optimistic miss = %d, %v", rows, err)
	}
	_, err = update.StaleOnZero().Exec(context.Background(), openScriptedDB(t, &driverScript{rowsAffected: 0}))
	if !errors.Is(err, orm.ErrStale) {
		t.Fatalf("explicit stale optimistic miss = %v", err)
	}
}

func TestSQLTransactionIsAnExecutor(t *testing.T) {
	script := &driverScript{
		columns:      []string{"id", "name", "score", "payload"},
		rows:         [][]driver.Value{{int64(1), "inside", int64(2), []byte(nil)}},
		rowsAffected: 1,
	}
	db := openScriptedDB(t, script)
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var observed atomic.Int64
	txExecutor := orm.ObserveExecutor(tx, func(context.Context, orm.StatementEvent, orm.Statement) { observed.Add(1) })
	items, err := orm.Select(widgetTable).ForUpdate().All(context.Background(), txExecutor)
	if err != nil || len(items) != 1 {
		_ = tx.Rollback()
		t.Fatalf("transaction select = %#v, %v", items, err)
	}
	if _, err := orm.Update(widgetTable).Set(widgetScore.Set(3)).Where(widgetID.Eq(1)).Exec(context.Background(), tx); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if script.commits.Load() != 1 {
		t.Fatalf("commits = %d", script.commits.Load())
	}
	if observed.Load() != 1 {
		t.Fatalf("observed transaction queries = %d", observed.Load())
	}
}

func TestLockedSelectRejectsDatabaseAndObservedDatabase(t *testing.T) {
	script := &driverScript{
		columns: []string{"id", "name", "score", "payload"},
		rows:    [][]driver.Value{{int64(1), "one", int64(1), []byte(nil)}},
	}
	db := openScriptedDB(t, script)
	for _, executor := range []orm.Executor{
		db,
		orm.ObserveExecutor(db, func(context.Context, orm.StatementEvent, orm.Statement) {}),
	} {
		if _, err := orm.Select(widgetTable).ForUpdate().All(context.Background(), executor); !errors.Is(err, orm.ErrTransactionRequired) {
			t.Fatalf("locked DB select error = %v", err)
		}
		if _, err := orm.Select(widgetTable).ForShare().First(context.Background(), executor); !errors.Is(err, orm.ErrTransactionRequired) {
			t.Fatalf("locked DB first error = %v", err)
		}
	}
	if len(script.queries) != 0 {
		t.Fatalf("rejected locked reads performed I/O: %#v", script.queries)
	}
}

func TestExecutionRejectsNilAndTypedNilExecutors(t *testing.T) {
	var nilDB *sql.DB
	executors := []orm.Executor{nil, nilDB, orm.ObserveExecutor(nilDB, func(context.Context, orm.StatementEvent, orm.Statement) {})}
	for _, executor := range executors {
		if _, err := orm.Select(widgetTable).All(context.Background(), executor); err == nil {
			t.Fatal("All accepted nil executor")
		}
		if _, err := orm.Select(widgetTable).First(context.Background(), executor); err == nil {
			t.Fatal("First accepted nil executor")
		}
		if _, err := orm.Select(widgetTable).Count(context.Background(), executor); err == nil {
			t.Fatal("Count accepted nil executor")
		}
		if _, err := orm.Select(widgetTable).Exists(context.Background(), executor); err == nil {
			t.Fatal("Exists accepted nil executor")
		}
		if _, err := orm.Insert(widgetTable).One(context.Background(), executor); err == nil {
			t.Fatal("Insert accepted nil executor")
		}
		if _, err := orm.Insert(widgetTable).Rows(orm.Row(widgetName.Set("x"))).All(context.Background(), executor); err == nil {
			t.Fatal("bulk Insert accepted nil executor")
		}
		if _, err := orm.Update(widgetTable).Set(widgetName.Set("x")).AllRows().Exec(context.Background(), executor); err == nil {
			t.Fatal("Update accepted nil executor")
		}
		if _, err := orm.Update(widgetTable).Set(widgetName.Set("x")).AllRows().One(context.Background(), executor); err == nil {
			t.Fatal("returning update accepted nil executor")
		}
		if _, err := orm.Delete(widgetTable).AllRows().Exec(context.Background(), executor); err == nil {
			t.Fatal("Delete accepted nil executor")
		}
	}
}

func TestImmutableQueriesBuildConcurrently(t *testing.T) {
	base := orm.Select(widgetTable).Where(widgetID.Gt(0))
	var group sync.WaitGroup
	for index := range 50 {
		group.Add(1)
		go func() {
			defer group.Done()
			statement, err := base.Where(widgetScore.Gte(int64(index))).OrderBy(widgetID.Desc()).Limit(index).Build()
			if err != nil || len(statement.Args()) != 3 {
				t.Errorf("concurrent build: statement=%q args=%v err=%v", statement.SQL(), statement.Args(), err)
			}
		}()
	}
	group.Wait()
	statement, err := base.Build()
	if err != nil || len(statement.Args()) != 1 {
		t.Fatalf("base mutated after concurrent builds: %q %v %v", statement.SQL(), statement.Args(), err)
	}
}

func stringsContains(value, fragment string) bool {
	for index := 0; index+len(fragment) <= len(value); index++ {
		if value[index:index+len(fragment)] == fragment {
			return true
		}
	}
	return false
}
