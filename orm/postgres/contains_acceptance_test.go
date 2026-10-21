package postgres_test

import (
	"context"
	"database/sql"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/ShanilKoshitha/goforge/orm"
)

func TestLivePostgresLiteralContains(t *testing.T) {
	databaseURL := os.Getenv("GOFORGE_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("GOFORGE_TEST_DATABASE_URL is not set")
	}
	db := openIsolatedPostgres(t, databaseURL)
	createLiveSchema(t, db)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	rows := []struct {
		name string
		note any
	}{
		{"prefix NeEdLe suffix", "prefix NeEdLe suffix"},
		{`literal 100%_!\ suffix`, `literal 100%_!\ suffix`},
		{`wildcard decoy 100xy!\ suffix`, `wildcard decoy 100xy!\ suffix`},
		{"SQL ' OR TRUE -- suffix", "SQL ' OR TRUE -- suffix"},
		{"empty note", ""},
		{"null note", nil},
	}
	for _, row := range rows {
		if _, err := db.ExecContext(ctx, `INSERT INTO orm_live_widgets (name, note) VALUES ($1, $2)`, row.name, row.note); err != nil {
			t.Fatal(err)
		}
	}
	type namedString string
	namedName := orm.MustColumn[liveWidget, namedString](liveWidgetTable, "name")
	note := orm.MustColumn[liveWidget, *string](liveWidgetTable, "note")
	namedNote := orm.MustColumn[liveWidget, *namedString](liveWidgetTable, "note")
	tests := []struct {
		name      string
		predicate orm.Predicate[liveWidget]
		want      []int64
	}{
		{"case insensitive substring", orm.ContainsFold(liveWidgetName, "needle"), []int64{1}},
		{"named string", orm.ContainsFold(namedName, "NEEDLE"), []int64{1}},
		{"literal percent", orm.ContainsFold(liveWidgetName, "%"), []int64{2}},
		{"literal underscore", orm.ContainsFold(liveWidgetName, "_"), []int64{2}},
		{"literal escape character", orm.ContainsFold(liveWidgetName, "!"), []int64{2, 3}},
		{"literal backslash", orm.ContainsFold(liveWidgetName, `\`), []int64{2, 3}},
		{"literal metacharacters", orm.ContainsFold(liveWidgetName, `100%_!\`), []int64{2}},
		{"nullable literal metacharacters", orm.ContainsFoldNullable(note, `100%_!\`), []int64{2}},
		{"nullable named string", orm.ContainsFoldNullable(namedNote, "needle"), []int64{1}},
		{"bound SQL-shaped literal", orm.ContainsFold(liveWidgetName, "' OR TRUE --"), []int64{4}},
		{"empty nullable substring excludes NULL", orm.ContainsFoldNullable(note, ""), []int64{1, 2, 3, 4, 5}},
		{"NULL remains explicitly queryable", note.IsNull(), []int64{6}},
		{"no match", orm.ContainsFoldNullable(note, "missing"), nil},
	}
	assertQueries := func(t *testing.T, executor orm.Executor) {
		t.Helper()
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				items, err := orm.Select(liveWidgetTable).Where(test.predicate).OrderBy(liveWidgetID.Asc()).All(ctx, executor)
				if err != nil {
					t.Fatal(err)
				}
				var got []int64
				for _, item := range items {
					got = append(got, item.ID)
				}
				if !reflect.DeepEqual(got, test.want) {
					t.Fatalf("matched IDs = %v; want %v", got, test.want)
				}
			})
		}
	}
	t.Run("database", func(t *testing.T) { assertQueries(t, db) })
	t.Run("transaction", func(t *testing.T) {
		tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		// The predicate's explicit escape character must also work when SQL
		// backslash string-literal behavior differs from the server default.
		if _, err := tx.ExecContext(ctx, `SET LOCAL standard_conforming_strings = off`); err != nil {
			t.Fatal(err)
		}
		assertQueries(t, tx)
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
	})
}
