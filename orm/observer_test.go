package orm_test

import (
	"context"
	"database/sql/driver"
	"reflect"
	"testing"

	"github.com/ShanilKoshitha/goforge/orm"
)

func TestObservedExecutorReportsCopiedStatements(t *testing.T) {
	script := &driverScript{
		columns:      []string{"id", "name", "score", "payload"},
		rows:         [][]driver.Value{{int64(1), "one", int64(1), []byte("x")}},
		rowsAffected: 1,
	}
	type event struct {
		kind      orm.StatementEvent
		statement orm.Statement
	}
	events := make([]event, 0, 3)
	executor := orm.ObserveExecutor(openScriptedDB(t, script), func(_ context.Context, kind orm.StatementEvent, statement orm.Statement) {
		events = append(events, event{kind: kind, statement: statement})
		args := statement.Args()
		if len(args) > 0 {
			args[0] = "observer mutation"
		}
	})
	if _, err := orm.Select(widgetTable).Where(widgetID.Eq(1)).All(context.Background(), executor); err != nil {
		t.Fatal(err)
	}
	if _, err := orm.Insert(widgetTable).Values(widgetName.Set("one"), widgetScore.Set(1)).One(context.Background(), executor); err != nil {
		t.Fatal(err)
	}
	if _, err := orm.Delete(widgetTable).Where(widgetID.Eq(1)).Exec(context.Background(), executor); err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 || events[0].kind != orm.StatementQuery || events[1].kind != orm.StatementQuery || events[2].kind != orm.StatementExec {
		t.Fatalf("events = %#v", events)
	}
	if !reflect.DeepEqual(script.queries[0].args, []any{int64(1)}) ||
		!reflect.DeepEqual(script.queries[1].args, []any{"one", int64(1)}) ||
		!reflect.DeepEqual(script.executions[0].args, []any{int64(1)}) {
		t.Fatalf("observer mutated execution args: queries=%#v exec=%#v", script.queries, script.executions)
	}
	if orm.ObserveExecutor(executor, nil) != executor {
		t.Fatal("nil observer did not preserve executor")
	}
}
