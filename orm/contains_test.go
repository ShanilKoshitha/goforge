package orm_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/ShanilKoshitha/goforge/orm"
)

func TestContainsFoldBindsEscapedLiteralPatterns(t *testing.T) {
	tests := []struct {
		name    string
		literal string
		pattern string
	}{
		{"empty", "", "%%"},
		{"mixed case", "NeEdLe", "%NeEdLe%"},
		{"percent", "100%", "%100!%%"},
		{"underscore", "a_b", "%a!_b%"},
		{"escape character", "a!b", "%a!!b%"},
		{"backslash", `a\b`, `%a\b%`},
		{"combined", `a\b%_!`, `%a\b!%!_!!%`},
		{"SQL-shaped", "' OR TRUE --", "%' OR TRUE --%"},
		{"unicode", "Été", "%Été%"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			statement, err := orm.Select(widgetTable).Where(orm.ContainsFold(widgetName, test.literal)).Build()
			if err != nil {
				t.Fatal(err)
			}
			wantSQL := widgetSelect + ` WHERE "widgets"."name" ILIKE $1 ESCAPE '!'`
			if statement.SQL() != wantSQL || !reflect.DeepEqual(statement.Args(), []any{test.pattern}) {
				t.Fatalf("contains SQL = %q %#v; want %q %#v", statement.SQL(), statement.Args(), wantSQL, []any{test.pattern})
			}
		})
	}
}

func TestContainsFoldSupportsNamedAndNullableStrings(t *testing.T) {
	type title string
	named := orm.MustColumn[widget, title](widgetTable, "name")
	nullable := orm.MustColumn[widget, *string](widgetTable, "name")
	nullableNamed := orm.MustColumn[widget, *title](widgetTable, "name")
	for name, predicate := range map[string]orm.Predicate[widget]{
		"named":          orm.ContainsFold(named, "value"),
		"nullable":       orm.ContainsFoldNullable(nullable, "value"),
		"nullable named": orm.ContainsFoldNullable(nullableNamed, "value"),
	} {
		t.Run(name, func(t *testing.T) {
			statement, err := orm.Select(widgetTable).Where(predicate).Build()
			if err != nil || !reflect.DeepEqual(statement.Args(), []any{"%value%"}) {
				t.Fatalf("typed contains = %q %#v, %v", statement.SQL(), statement.Args(), err)
			}
		})
	}
}

func TestContainsFoldGroupsAndAggregatesKeepParameterOrder(t *testing.T) {
	nullable := orm.MustColumn[widget, *string](widgetTable, "name")
	query := orm.Select(widgetTable).Where(widgetID.Gte(7), orm.Or(
		orm.ContainsFold(widgetName, "a%b"),
		orm.Not(orm.ContainsFoldNullable(nullable, "x_y")),
	))
	wantWhere := ` WHERE "widgets"."id" >= $1 AND ("widgets"."name" ILIKE $2 ESCAPE '!' OR NOT ("widgets"."name" ILIKE $3 ESCAPE '!'))`
	wantArgs := []any{int64(7), "%a!%b%", "%x!_y%"}
	statement, err := query.Build()
	if err != nil || statement.SQL() != widgetSelect+wantWhere || !reflect.DeepEqual(statement.Args(), wantArgs) {
		t.Fatalf("grouped contains = %q %#v, %v", statement.SQL(), statement.Args(), err)
	}
	count, err := query.BuildCount()
	if err != nil || count.SQL() != `SELECT COUNT(*) FROM "widgets"`+wantWhere || !reflect.DeepEqual(count.Args(), wantArgs) {
		t.Fatalf("count contains = %q %#v, %v", count.SQL(), count.Args(), err)
	}
	exists, err := query.BuildExists()
	if err != nil || exists.SQL() != `SELECT EXISTS (SELECT 1 FROM "widgets"`+wantWhere+")" || !reflect.DeepEqual(exists.Args(), wantArgs) {
		t.Fatalf("exists contains = %q %#v, %v", exists.SQL(), exists.Args(), err)
	}
}

func TestContainsFoldRejectsInvalidAndForeignDescriptors(t *testing.T) {
	archive := orm.MustTable("widget_archive", widgetMapper)
	sameName := orm.MustTable("widgets", widgetMapper)
	for name, predicate := range map[string]orm.Predicate[widget]{
		"zero":          orm.ContainsFold(orm.Column[widget, string]{}, "value"),
		"zero nullable": orm.ContainsFoldNullable(orm.Column[widget, *string]{}, "value"),
		"foreign":       orm.ContainsFold(orm.MustColumn[widget, string](archive, "name"), "value"),
		"same name":     orm.ContainsFoldNullable(orm.MustColumn[widget, *string](sameName, "name"), "value"),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := orm.Select(widgetTable).Where(predicate).Build(); err == nil {
				t.Fatal("invalid contains descriptor built successfully")
			}
		})
	}
}

func TestContainsFoldDoesNotChangeLikePatterns(t *testing.T) {
	statement, err := orm.Select(widgetTable).Where(orm.Like(widgetName, "%a_b!")).Build()
	if err != nil || !strings.HasSuffix(statement.SQL(), `"widgets"."name" LIKE $1`) || !reflect.DeepEqual(statement.Args(), []any{"%a_b!"}) {
		t.Fatalf("existing LIKE changed: %q %#v, %v", statement.SQL(), statement.Args(), err)
	}
}
