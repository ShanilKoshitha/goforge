package orm_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ShanilKoshitha/goforge/orm"
)

type widget struct {
	ID      int64
	Name    string
	Score   int64
	Payload []byte
}

func scanWidget(row orm.RowScanner) (widget, error) {
	var value widget
	err := row.Scan(&value.ID, &value.Name, &value.Score, &value.Payload)
	return value, err
}

var (
	widgetMapper  = orm.MustMapper([]string{"id", "name", "score", "payload"}, scanWidget)
	widgetTable   = orm.MustTable("widgets", widgetMapper)
	widgetID      = orm.MustColumn[widget, int64](widgetTable, "id")
	widgetName    = orm.MustColumn[widget, string](widgetTable, "name")
	widgetScore   = orm.MustColumn[widget, int64](widgetTable, "score")
	widgetPayload = orm.MustColumn[widget, []byte](widgetTable, "payload")
)

type versioned struct {
	ID        int64
	Version   int64
	UpdatedAt time.Time
}

func scanVersioned(row orm.RowScanner) (versioned, error) {
	var value versioned
	err := row.Scan(&value.ID, &value.Version, &value.UpdatedAt)
	return value, err
}

var (
	versionedMapper    = orm.MustMapper([]string{"id", "version", "updated_at"}, scanVersioned)
	versionedTable     = orm.MustTable("versioned_records", versionedMapper)
	versionedID        = orm.MustColumn[versioned, int64](versionedTable, "id")
	versionedVersion   = orm.MustColumn[versioned, int64](versionedTable, "version")
	versionedUpdatedAt = orm.MustColumn[versioned, time.Time](versionedTable, "updated_at")
)

const widgetSelect = `SELECT "widgets"."id", "widgets"."name", "widgets"."score", "widgets"."payload" FROM "widgets"`

func TestDefinitionsRejectUnsafeGeneratedIdentifiers(t *testing.T) {
	for _, name := range []string{"", "Users", "user-id", "users; DROP TABLE users", strings.Repeat("a", 64)} {
		if _, err := orm.NewTable(name, widgetMapper); err == nil {
			t.Errorf("NewTable(%q) succeeded", name)
		}
	}
	if _, err := orm.NewMapper([]string{"id", "id"}, scanWidget); err == nil {
		t.Fatal("duplicate mapper columns succeeded")
	}
	if _, err := orm.NewMapper[widget](nil, scanWidget); err == nil {
		t.Fatal("empty mapper succeeded")
	}
	if _, err := orm.NewMapper[widget]([]string{"id"}, nil); err == nil {
		t.Fatal("nil scan function succeeded")
	}
	if _, err := orm.NewColumn[widget, string](widgetTable, "missing"); err == nil {
		t.Fatal("unmapped column succeeded")
	}
	if _, err := orm.Select(orm.Table[widget]{}).Build(); err == nil {
		t.Fatal("zero table built SQL")
	}
}

func TestSelectBuildsDeterministicPostgresSQL(t *testing.T) {
	query := orm.Select(widgetTable).
		Where(widgetID.Gte(2), orm.Like(widgetName, "%needle%' OR TRUE --"), widgetID.In(2, 3)).
		OrderBy(widgetScore.Desc(), widgetName.Asc()).
		Limit(20).
		Offset(40).
		ForUpdate()
	statement, err := query.Build()
	if err != nil {
		t.Fatal(err)
	}
	wantSQL := widgetSelect + ` WHERE "widgets"."id" >= $1 AND "widgets"."name" LIKE $2 AND "widgets"."id" IN ($3, $4) ORDER BY "widgets"."score" DESC, "widgets"."name" ASC LIMIT $5 OFFSET $6 FOR UPDATE`
	if statement.SQL() != wantSQL {
		t.Fatalf("SQL:\n%s\nwant:\n%s", statement.SQL(), wantSQL)
	}
	wantArgs := []any{int64(2), "%needle%' OR TRUE --", int64(2), int64(3), 20, 40}
	if !reflect.DeepEqual(statement.Args(), wantArgs) {
		t.Fatalf("args = %#v, want %#v", statement.Args(), wantArgs)
	}
	if strings.Contains(statement.SQL(), "needle") {
		t.Fatal("injection-shaped value was interpolated into SQL")
	}
}

func TestColumnComparisonPredicates(t *testing.T) {
	tests := []struct {
		name string
		pred orm.Predicate[widget]
		op   string
	}{
		{"eq", widgetID.Eq(1), " = $1"},
		{"not-eq", widgetID.NotEq(1), " <> $1"},
		{"lt", widgetID.Lt(1), " < $1"},
		{"lte", widgetID.Lte(1), " <= $1"},
		{"gt", widgetID.Gt(1), " > $1"},
		{"gte", widgetID.Gte(1), " >= $1"},
		{"null", widgetID.IsNull(), " IS NULL"},
		{"not-null", widgetID.IsNotNull(), " IS NOT NULL"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			statement, err := orm.Select(widgetTable).Where(test.pred).Build()
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasSuffix(statement.SQL(), `"widgets"."id"`+test.op) {
				t.Fatalf("SQL = %q", statement.SQL())
			}
		})
	}
	empty, err := orm.Select(widgetTable).Where(widgetID.In()).Build()
	if err != nil || !strings.HasSuffix(empty.SQL(), " WHERE FALSE") || len(empty.Args()) != 0 {
		t.Fatalf("empty IN: statement=%q args=%v err=%v", empty.SQL(), empty.Args(), err)
	}
}

func TestPredicateGroupsCompileWithStableParentheses(t *testing.T) {
	query := orm.Select(widgetTable).Where(
		orm.Or(
			widgetName.Eq("first"),
			orm.And(widgetScore.Gte(10), orm.Not(widgetID.Eq(7))),
		),
	)
	statement, err := query.Build()
	if err != nil {
		t.Fatal(err)
	}
	want := widgetSelect + ` WHERE ("widgets"."name" = $1 OR ("widgets"."score" >= $2 AND NOT ("widgets"."id" = $3)))`
	if statement.SQL() != want || !reflect.DeepEqual(statement.Args(), []any{"first", int64(10), int64(7)}) {
		t.Fatalf("grouped predicate = %q %#v", statement.SQL(), statement.Args())
	}
	if _, err := orm.Select(widgetTable).Where(orm.And[widget]()).Build(); err == nil {
		t.Fatal("empty AND group succeeded")
	}
	archive := orm.MustTable("group_archive", widgetMapper)
	archiveID := orm.MustColumn[widget, int64](archive, "id")
	if _, err := orm.Select(widgetTable).Where(orm.Or(widgetID.Eq(1), archiveID.Eq(2))).Build(); err == nil {
		t.Fatal("cross-table grouped predicate succeeded")
	}
}

func TestSelectBuildersAreImmutable(t *testing.T) {
	base := orm.Select(widgetTable).Where(widgetID.Gt(0))
	byName := base.Where(widgetName.Eq("Ada")).OrderBy(widgetName.Asc()).Limit(1)
	byScore := base.Where(widgetScore.Gt(10)).OrderBy(widgetScore.Desc()).ForShare()

	baseStatement, _ := base.Build()
	nameStatement, _ := byName.Build()
	scoreStatement, _ := byScore.Build()
	if strings.Contains(baseStatement.SQL(), "ORDER BY") || len(baseStatement.Args()) != 1 {
		t.Fatalf("base mutated: %s %#v", baseStatement.SQL(), baseStatement.Args())
	}
	if strings.Contains(nameStatement.SQL(), "score") && strings.Contains(nameStatement.SQL(), "FOR SHARE") {
		t.Fatalf("name branch contains score branch: %s", nameStatement.SQL())
	}
	if strings.Contains(scoreStatement.SQL(), `"name" =`) || strings.Contains(scoreStatement.SQL(), "LIMIT") {
		t.Fatalf("score branch contains name branch: %s", scoreStatement.SQL())
	}
}

func TestStatementAndPredicateArgumentsAreCopied(t *testing.T) {
	payload := []byte("original")
	query := orm.Select(widgetTable).Where(widgetPayload.Eq(payload))
	payload[0] = 'X'
	statement, err := query.Build()
	if err != nil {
		t.Fatal(err)
	}
	first := statement.Args()
	if got := string(first[0].([]byte)); got != "original" {
		t.Fatalf("builder retained caller bytes: %q", got)
	}
	first[0].([]byte)[0] = 'Y'
	first[0] = "replaced"
	if got := string(statement.Args()[0].([]byte)); got != "original" {
		t.Fatalf("Statement.Args exposed internal values: %q", got)
	}

	values := []int64{1, 2}
	inQuery := orm.Select(widgetTable).Where(widgetID.In(values...))
	values[0] = 99
	inStatement, _ := inQuery.Build()
	if !reflect.DeepEqual(inStatement.Args(), []any{int64(1), int64(2)}) {
		t.Fatalf("IN retained caller slice: %#v", inStatement.Args())
	}
}

func TestByteAliasesAreCopiedAndTypedNilValuesAreRejected(t *testing.T) {
	type byteAlias []byte
	rawColumn := orm.MustColumn[widget, json.RawMessage](widgetTable, "payload")
	aliasColumn := orm.MustColumn[widget, byteAlias](widgetTable, "payload")
	raw := json.RawMessage(`{"safe":true}`)
	alias := byteAlias("alias")
	query := orm.Select(widgetTable).Where(rawColumn.Eq(raw), aliasColumn.Eq(alias))
	raw[0], alias[0] = 'X', 'Y'
	statement, err := query.Build()
	if err != nil {
		t.Fatal(err)
	}
	if string(statement.Args()[0].(json.RawMessage)) != `{"safe":true}` || string(statement.Args()[1].(byteAlias)) != "alias" {
		t.Fatalf("byte aliases were not frozen: %#v", statement.Args())
	}
	if _, err := orm.Select(widgetTable).Where(widgetPayload.Eq(nil)).Build(); err == nil {
		t.Fatal("typed-nil comparison succeeded")
	}
	if _, err := orm.Update(widgetTable).Set(widgetPayload.Set(nil)).Where(widgetID.Eq(1)).Build(); err == nil {
		t.Fatal("typed-nil assignment succeeded")
	}
	if _, err := orm.Update(widgetTable).Set(widgetPayload.From(orm.Value[[]byte](nil))).Where(widgetID.Eq(1)).Build(); err == nil {
		t.Fatal("typed-nil FieldValue succeeded")
	}
}

func TestSelectAggregateAndLockValidation(t *testing.T) {
	query := orm.Select(widgetTable).Where(widgetScore.Gt(5)).OrderBy(widgetID.Desc()).Limit(3).Offset(2).ForShare()
	count, err := query.BuildCount()
	if err != nil {
		t.Fatal(err)
	}
	if want := `SELECT COUNT(*) FROM "widgets" WHERE "widgets"."score" > $1`; count.SQL() != want {
		t.Fatalf("count SQL = %q, want %q", count.SQL(), want)
	}
	exists, err := query.BuildExists()
	if err != nil {
		t.Fatal(err)
	}
	if want := `SELECT EXISTS (SELECT 1 FROM "widgets" WHERE "widgets"."score" > $1)`; exists.SQL() != want {
		t.Fatalf("exists SQL = %q, want %q", exists.SQL(), want)
	}
	for _, invalid := range []orm.SelectBuilder[widget]{
		orm.Select(widgetTable).Limit(-1),
		orm.Select(widgetTable).Offset(-1),
		orm.Select(widgetTable).ForUpdate().ForShare(),
	} {
		if _, err := invalid.Build(); err == nil {
			t.Fatal("invalid select built successfully")
		}
	}
}

func TestPredicatesAndOrdersCannotCrossTables(t *testing.T) {
	archive := orm.MustTable("widget_archive", widgetMapper)
	archiveID := orm.MustColumn[widget, int64](archive, "id")
	if _, err := orm.Select(widgetTable).Where(archiveID.Eq(1)).Build(); err == nil {
		t.Fatal("cross-table predicate succeeded")
	}
	if _, err := orm.Select(widgetTable).OrderBy(archiveID.Asc()).Build(); err == nil {
		t.Fatal("cross-table order succeeded")
	}
	if _, err := orm.Update(widgetTable).Set(archiveID.Set(1)).Where(widgetID.Eq(1)).Build(); err == nil {
		t.Fatal("cross-table assignment succeeded")
	}
}

func TestFieldsAndMutationBuilders(t *testing.T) {
	var omitted orm.Field[string]
	if omitted.State() != orm.FieldOmitted {
		t.Fatalf("zero field state = %v", omitted.State())
	}
	if _, ok := omitted.Get(); ok {
		t.Fatal("omitted field returned a value")
	}
	if orm.Null[string]().State() != orm.FieldNull {
		t.Fatal("Null field state is not FieldNull")
	}
	zero := orm.Value("")
	if value, ok := zero.Get(); !ok || value != "" {
		t.Fatalf("zero value field = %q, %v", value, ok)
	}
	if orm.Default[string]().State() != orm.FieldDefault {
		t.Fatal("Default field state is not FieldDefault")
	}

	insert, err := orm.Insert(widgetTable).Values(
		widgetName.From(omitted),
		widgetScore.Set(0),
		widgetPayload.From(orm.Null[[]byte]()),
	).Build()
	if err != nil {
		t.Fatal(err)
	}
	wantInsert := `INSERT INTO "widgets" ("score", "payload") VALUES ($1, $2) RETURNING "widgets"."id", "widgets"."name", "widgets"."score", "widgets"."payload"`
	if insert.SQL() != wantInsert || !reflect.DeepEqual(insert.Args(), []any{int64(0), nil}) {
		t.Fatalf("insert = %q %#v", insert.SQL(), insert.Args())
	}
	defaults, err := orm.Insert(widgetTable).Build()
	if err != nil || !strings.Contains(defaults.SQL(), " DEFAULT VALUES RETURNING ") {
		t.Fatalf("default insert = %q err=%v", defaults.SQL(), err)
	}

	if _, err := orm.Update(widgetTable).Set(widgetName.Set("x")).Build(); !errors.Is(err, orm.ErrUnsafeMutation) {
		t.Fatalf("unsafe update error = %v", err)
	}
	update, err := orm.Update(widgetTable).
		Set(widgetName.Set("x"), widgetPayload.From(orm.Null[[]byte]())).
		Where(widgetID.Eq(7)).Build()
	if err != nil {
		t.Fatal(err)
	}
	wantUpdate := `UPDATE "widgets" SET "name" = $1, "payload" = $2 WHERE "widgets"."id" = $3`
	if update.SQL() != wantUpdate || !reflect.DeepEqual(update.Args(), []any{"x", nil, int64(7)}) {
		t.Fatalf("update = %q %#v", update.SQL(), update.Args())
	}
	all, err := orm.Update(widgetTable).Set(widgetScore.Set(0)).AllRows().Build()
	if err != nil || strings.Contains(all.SQL(), " WHERE ") {
		t.Fatalf("explicit all-row update = %q err=%v", all.SQL(), err)
	}
	defaulted, err := orm.Update(widgetTable).Set(widgetName.From(orm.Default[string]())).Where(widgetID.Eq(1)).Build()
	if err != nil || defaulted.SQL() != `UPDATE "widgets" SET "name" = DEFAULT WHERE "widgets"."id" = $1` ||
		!reflect.DeepEqual(defaulted.Args(), []any{int64(1)}) {
		t.Fatalf("default assignment = %q %#v err=%v", defaulted.SQL(), defaulted.Args(), err)
	}
	if _, err := orm.Update(widgetTable).Set(widgetName.From(omitted)).AllRows().Build(); err == nil {
		t.Fatal("all-omitted update succeeded")
	}
	if _, err := orm.Update(widgetTable).Set(widgetName.Set("a"), widgetName.Set("b")).AllRows().Build(); err == nil {
		t.Fatal("duplicate update assignment succeeded")
	}

	if _, err := orm.Delete(widgetTable).Build(); !errors.Is(err, orm.ErrUnsafeMutation) {
		t.Fatalf("unsafe delete error = %v", err)
	}
	deleted, err := orm.Delete(widgetTable).Where(widgetID.Eq(9)).Build()
	if err != nil || deleted.SQL() != `DELETE FROM "widgets" WHERE "widgets"."id" = $1` {
		t.Fatalf("delete = %q err=%v", deleted.SQL(), err)
	}
	allDeleted, err := orm.Delete(widgetTable).AllRows().Build()
	if err != nil || allDeleted.SQL() != `DELETE FROM "widgets"` {
		t.Fatalf("all delete = %q err=%v", allDeleted.SQL(), err)
	}
}

func TestSafeGeneratedMutationExpressionsAndReturning(t *testing.T) {
	update := orm.Update(versionedTable).
		Set(orm.Increment(versionedVersion, int64(1)), orm.SetCurrentTime(versionedUpdatedAt)).
		Where(versionedID.Eq(8)).
		StaleOnZero()
	statement, err := update.BuildReturning()
	if err != nil {
		t.Fatal(err)
	}
	want := `UPDATE "versioned_records" SET "version" = "version" + $1, "updated_at" = CURRENT_TIMESTAMP WHERE "versioned_records"."id" = $2 RETURNING "versioned_records"."id", "versioned_records"."version", "versioned_records"."updated_at"`
	if statement.SQL() != want || !reflect.DeepEqual(statement.Args(), []any{int64(1), int64(8)}) {
		t.Fatalf("returning update = %q %#v", statement.SQL(), statement.Args())
	}
	if _, err := orm.Insert(versionedTable).Values(orm.Increment(versionedVersion, int64(1))).Build(); err == nil {
		t.Fatal("insert accepted an increment expression")
	}
}

func TestBulkInsertBuildsDeterministicReturningSQL(t *testing.T) {
	var omitted orm.Field[int64]
	payload := []byte("second")
	bulk := orm.Insert(widgetTable).Rows(
		orm.Row(
			widgetID.From(omitted),
			widgetName.Set("first"),
			widgetScore.From(orm.Default[int64]()),
			widgetPayload.From(orm.Null[[]byte]()),
		),
		orm.Row(
			widgetPayload.Set(payload),
			widgetScore.Set(2),
			widgetName.Set("second"),
		),
	)
	payload[0] = 'X'
	statement, err := bulk.Build()
	if err != nil {
		t.Fatal(err)
	}
	wantSQL := `INSERT INTO "widgets" ("name", "score", "payload") VALUES ($1, DEFAULT, $2), ($3, $4, $5) RETURNING "widgets"."id", "widgets"."name", "widgets"."score", "widgets"."payload"`
	wantArgs := []any{"first", nil, "second", int64(2), []byte("second")}
	if statement.SQL() != wantSQL || !reflect.DeepEqual(statement.Args(), wantArgs) {
		t.Fatalf("bulk insert = %q %#v", statement.SQL(), statement.Args())
	}
	args := statement.Args()
	args[4].([]byte)[0] = 'Y'
	if !reflect.DeepEqual(statement.Args(), wantArgs) {
		t.Fatalf("bulk statement args were mutable: %#v", statement.Args())
	}
}

func TestBulkInsertRejectsInvalidRows(t *testing.T) {
	if _, err := orm.Insert(widgetTable).Rows().Build(); err == nil {
		t.Fatal("empty bulk insert succeeded")
	}
	if _, err := orm.Insert(widgetTable).Values(widgetName.Set("one")).Rows(orm.Row(widgetName.Set("two"))).Build(); err == nil {
		t.Fatal("combined Values and Rows succeeded")
	}
	if _, err := orm.Insert(widgetTable).Rows(
		orm.Row(widgetName.Set("one")),
		orm.Row(widgetScore.Set(2)),
	).Build(); err == nil {
		t.Fatal("incompatible row shapes succeeded")
	}
	if _, err := orm.Insert(widgetTable).Rows(
		orm.Row(widgetName.Set("one"), widgetName.Set("again")),
	).Build(); err == nil {
		t.Fatal("duplicate bulk assignment succeeded")
	}
	archive := orm.MustTable("bulk_widget_archive", widgetMapper)
	archiveName := orm.MustColumn[widget, string](archive, "name")
	if _, err := orm.Insert(widgetTable).Rows(orm.Row(archiveName.Set("cross-table"))).Build(); err == nil {
		t.Fatal("cross-table bulk assignment succeeded")
	}
	if _, err := orm.Insert(widgetTable).Rows(orm.Row[widget](), orm.Row[widget]()).Build(); err == nil {
		t.Fatal("multi-row DEFAULT VALUES succeeded")
	}
}

func TestPostgreSQLParameterLimitIsEnforcedAtBuild(t *testing.T) {
	t.Run("direct IN", func(t *testing.T) {
		values := make([]int64, orm.PostgreSQLParameterLimit)
		statement, err := orm.Select(widgetTable).Where(widgetID.In(values...)).Build()
		if err != nil {
			t.Fatalf("parameter limit should build: %v", err)
		}
		if len(statement.Args()) != orm.PostgreSQLParameterLimit || !strings.Contains(statement.SQL(), "$65535") {
			t.Fatalf("limit statement has %d args", len(statement.Args()))
		}
		values = append(values, 0)
		if _, err := orm.Select(widgetTable).Where(widgetID.In(values...)).Build(); err == nil ||
			!strings.Contains(err.Error(), "statement has 65536 parameters; PostgreSQL limit is 65535") {
			t.Fatalf("cap+1 IN error = %v", err)
		}
	})

	t.Run("atomic bulk insert", func(t *testing.T) {
		rows := make([]orm.InsertRow[widget], orm.PostgreSQLParameterLimit)
		for index := range rows {
			rows[index] = orm.Row(widgetScore.Set(int64(index)))
		}
		statement, err := orm.Insert(widgetTable).Rows(rows...).Build()
		if err != nil {
			t.Fatalf("bulk parameter limit should build: %v", err)
		}
		if len(statement.Args()) != orm.PostgreSQLParameterLimit {
			t.Fatalf("bulk limit statement has %d args", len(statement.Args()))
		}
		rows = append(rows, orm.Row(widgetScore.Set(0)))
		if _, err := orm.Insert(widgetTable).Rows(rows...).Build(); err == nil ||
			!strings.Contains(err.Error(), "statement has 65536 parameters; PostgreSQL limit is 65535") {
			t.Fatalf("cap+1 bulk error = %v", err)
		}
	})
}

func TestOptimisticVersionComposesPredicateAndIncrement(t *testing.T) {
	base := orm.Update(versionedTable).
		Set(orm.SetCurrentTime(versionedUpdatedAt)).
		Where(versionedID.Eq(9))
	guarded := base.OptimisticVersion(orm.ExpectVersion(versionedVersion, int64(4)))
	statement, err := guarded.Build()
	if err != nil {
		t.Fatal(err)
	}
	wantSQL := `UPDATE "versioned_records" SET "updated_at" = CURRENT_TIMESTAMP, "version" = "version" + $1 WHERE "versioned_records"."id" = $2 AND "versioned_records"."version" = $3`
	if statement.SQL() != wantSQL || !reflect.DeepEqual(statement.Args(), []any{int64(1), int64(9), int64(4)}) {
		t.Fatalf("optimistic update = %q %#v", statement.SQL(), statement.Args())
	}
	baseStatement, err := base.Build()
	if err != nil || strings.Contains(baseStatement.SQL(), `"version" +`) || len(baseStatement.Args()) != 1 {
		t.Fatalf("optimistic builder mutated base: %q %#v err=%v", baseStatement.SQL(), baseStatement.Args(), err)
	}

	if _, err := orm.Update(versionedTable).
		Set(versionedVersion.Set(5)).
		Where(versionedID.Eq(9)).
		OptimisticVersion(orm.ExpectVersion(versionedVersion, int64(4))).
		Build(); err == nil {
		t.Fatal("duplicate optimistic version assignment succeeded")
	}
	archive := orm.MustTable("versioned_archive", versionedMapper)
	archiveVersion := orm.MustColumn[versioned, int64](archive, "version")
	if _, err := base.OptimisticVersion(orm.ExpectVersion(archiveVersion, int64(4))).Build(); err == nil {
		t.Fatal("cross-table optimistic guard succeeded")
	}
}

func TestStructuredErrorsPreserveStableAndNativeTargets(t *testing.T) {
	native := errors.New("submitted secret value")
	constraint := &orm.ConstraintError{Kind: orm.ConstraintUnique, Constraint: "widgets_name_key", Code: "23505", Cause: native}
	persisted := &orm.PersistenceError{Operation: orm.OperationInsert, Table: "widgets", Cause: constraint}
	if !errors.Is(persisted, orm.ErrUnique) || !errors.Is(persisted, native) {
		t.Fatalf("constraint wrapping lost errors.Is: %v", persisted)
	}
	var foundConstraint *orm.ConstraintError
	if !errors.As(persisted, &foundConstraint) || foundConstraint.Constraint != "widgets_name_key" {
		t.Fatalf("constraint wrapping lost errors.As: %v", persisted)
	}
	if strings.Contains(persisted.Error(), "submitted secret value") || strings.Contains(constraint.Error(), "submitted secret value") {
		t.Fatalf("constraint error disclosed native detail: %q / %q", persisted.Error(), constraint.Error())
	}
	if !strings.Contains(persisted.Error(), "23505") {
		t.Fatalf("constraint error omitted safe code: %q", persisted.Error())
	}

	for _, test := range []struct {
		kind   orm.ConstraintKind
		target error
	}{
		{orm.ConstraintUnique, orm.ErrUnique},
		{orm.ConstraintForeignKey, orm.ErrForeignKey},
		{orm.ConstraintNotNull, orm.ErrNotNull},
		{orm.ConstraintCheck, orm.ErrCheck},
	} {
		if !errors.Is(&orm.ConstraintError{Kind: test.kind, Cause: native}, test.target) {
			t.Errorf("kind %s does not match %v", test.kind, test.target)
		}
	}
	for _, test := range []struct {
		kind   orm.TransientKind
		target error
	}{
		{orm.TransientSerialization, orm.ErrSerialization},
		{orm.TransientDeadlock, orm.ErrDeadlock},
	} {
		transient := &orm.TransientError{Kind: test.kind, Code: "40001", Cause: native}
		wrapped := &orm.PersistenceError{Operation: orm.OperationUpdate, Table: "widgets", Cause: transient}
		if !errors.Is(wrapped, test.target) || !errors.Is(wrapped, native) {
			t.Errorf("transient %s lost targets: %v", test.kind, wrapped)
		}
		if strings.Contains(wrapped.Error(), "submitted secret value") || strings.Contains(transient.Error(), "submitted secret value") {
			t.Errorf("transient %s disclosed native detail", test.kind)
		}
		if !strings.Contains(wrapped.Error(), "40001") {
			t.Errorf("transient %s omitted safe code", test.kind)
		}
	}
}
