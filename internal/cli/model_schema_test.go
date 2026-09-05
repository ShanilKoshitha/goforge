package cli

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParseModelSchemaBuildsDeterministicTypedSchema(t *testing.T) {
	directory := writeModelSources(t, map[string]string{
		"z_user.go": `package models

import clock "time"

type Email string

type User struct {
	_         struct{}  ` + "`forge:\"table=accounts\"`" + `
	ID        int64     ` + "`db:\"id\" json:\"id\" forge:\"primary,generated,protected,required\"`" + `
	Email     Email     ` + "`db:\"email_address\" forge:\"required,unique,type=varchar(320)\"`" + `
	Nickname  *string   ` + "`forge:\"nullable\"`" + `
	State     string    ` + "`forge:\"required,default='active',index\"`" + `
	Balance   float64   ` + "`forge:\"required,type=numeric(10,2),default=0.00\"`" + `
	CreatedAt clock.Time ` + "`forge:\"required,default=now()\"`" + `
	Posts     []Post    ` + "`forge:\"has_many,target=Post,foreign_key=UserID,references=ID\"`" + `
	Profile   *Profile  ` + "`forge:\"has_one,target=Profile,foreign_key=UserID,references=ID\"`" + `
	Roles     []*Role   ` + "`forge:\"many_to_many,target=Role,references=ID,target_key=ID,join_table=account_roles,join_foreign_key=account_id,join_reference_key=role_id\"`" + `
	Scratch   map[string]any ` + "`forge:\"ignore\"`" + `
}
`,
		"a_related.go": `package models

type Post struct {
	ID     int64  ` + "`forge:\"primary,generated,protected,required\"`" + `
	UserID int64  ` + "`forge:\"required,index,references=User.ID,on_delete=cascade\"`" + `
	Title  string ` + "`forge:\"required\"`" + `
	User   User   ` + "`forge:\"belongs_to,target=User,foreign_key=UserID,references=ID\"`" + `
}

type Profile struct {
	ID     int64  ` + "`forge:\"primary,generated,protected,required\"`" + `
	UserID int64  ` + "`forge:\"required,unique,references=User.ID,on_delete=cascade\"`" + `
	Bio    *string ` + "`forge:\"nullable\"`" + `
	User   *User  ` + "`forge:\"belongs_to,target=User,foreign_key=UserID,references=ID\"`" + `
}

type Role struct {
	ID   int64  ` + "`forge:\"primary,generated,protected,required\"`" + `
	Name string ` + "`forge:\"required,unique\"`" + `
}
`,
	})

	schema, err := parseModelSchema(directory)
	if err != nil {
		t.Fatal(err)
	}
	if got := modelNames(schema); !reflect.DeepEqual(got, []string{"Post", "Profile", "Role", "User"}) {
		t.Fatalf("models are not sorted: %v", got)
	}
	user := findModel(t, schema, "User")
	if user.Table != "accounts" || user.SourceFile != "z_user.go" {
		t.Fatalf("user identity = table %q source %q", user.Table, user.SourceFile)
	}
	if got := fieldNames(user); !reflect.DeepEqual(got, []string{"ID", "Email", "Nickname", "State", "Balance", "CreatedAt"}) {
		t.Fatalf("field source order was not preserved: %v", got)
	}
	id := findField(t, user, "ID")
	if !id.Primary || !id.Generated || !id.Protected || !id.Required || id.Nullable || id.DBType != "bigint" {
		t.Fatalf("ID metadata = %+v", id)
	}
	email := findField(t, user, "Email")
	if email.GoType != "Email" || email.Column != "email_address" || email.DBType != "varchar(320)" || !email.Unique {
		t.Fatalf("Email metadata = %+v", email)
	}
	if nickname := findField(t, user, "Nickname"); nickname.GoType != "*string" || !nickname.Nullable || nickname.DBType != "text" {
		t.Fatalf("Nickname metadata = %+v", nickname)
	}
	if state := findField(t, user, "State"); state.Default == nil || *state.Default != "'active'" || !state.Indexed {
		t.Fatalf("State metadata = %+v", state)
	}
	if balance := findField(t, user, "Balance"); balance.DBType != "numeric(10,2)" || balance.Default == nil || *balance.Default != "0.00" {
		t.Fatalf("Balance metadata = %+v", balance)
	}
	if created := findField(t, user, "CreatedAt"); created.GoType != "clock.Time" || created.GeneratedGoType != "time.Time" || created.GoImportPath != "time" || created.DBType != "timestamptz" {
		t.Fatalf("CreatedAt metadata = %+v", created)
	}
	if got := relationNames(user); !reflect.DeepEqual(got, []string{"Posts", "Profile", "Roles"}) {
		t.Fatalf("relation source order was not preserved: %v", got)
	}
	roles := findRelation(t, user, "Roles")
	if roles.Kind != relationManyToMany || roles.Target != "Role" || roles.JoinTable != "account_roles" || roles.JoinForeignKey != "account_id" || roles.JoinReferenceKey != "role_id" {
		t.Fatalf("many-to-many metadata = %+v", roles)
	}
	postUserID := findField(t, findModel(t, schema, "Post"), "UserID")
	if postUserID.Reference == nil || postUserID.Reference.Model != "User" || postUserID.Reference.Field != "ID" || postUserID.Reference.Table != "accounts" || postUserID.Reference.Column != "id" || postUserID.OnDelete != "cascade" {
		t.Fatalf("resolved reference = %+v", postUserID.Reference)
	}

	again, err := parseModelSchema(directory)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(schema, again) {
		t.Fatal("parsing the same source was not deterministic")
	}
}

func TestParseModelSchemaSupportsStandardNullableWrappersAndAliases(t *testing.T) {
	directory := writeModelSources(t, map[string]string{"model.go": `package models

import storage "database/sql"

type RecordID int64

type Record struct {
	ID      RecordID           ` + "`forge:\"primary,required\"`" + `
	Comment storage.NullString ` + "`forge:\"nullable\"`" + `
	SeenAt  storage.NullTime   ` + "`forge:\"nullable\"`" + `
}
`})
	schema, err := parseModelSchema(directory)
	if err != nil {
		t.Fatal(err)
	}
	record := findModel(t, schema, "Record")
	if field := findField(t, record, "Comment"); field.DBType != "text" || !field.Nullable {
		t.Fatalf("nullable string = %+v", field)
	}
	if field := findField(t, record, "SeenAt"); field.DBType != "timestamptz" || !field.Nullable {
		t.Fatalf("nullable time = %+v", field)
	}
}

func TestParseModelSchemaRejectsInvalidDeclarations(t *testing.T) {
	tests := []struct {
		name    string
		sources map[string]string
		want    string
	}{
		{
			name: "wrong package", want: "package must be models",
			sources: map[string]string{"model.go": `package domain; type User struct { ID int64 ` + "`forge:\"primary,required\"`" + ` }`},
		},
		{
			name: "duplicate model", want: "duplicate model User",
			sources: map[string]string{
				"a.go": `package models; type User struct { ID int64 ` + "`forge:\"primary,required\"`" + ` }`,
				"b.go": `package models; type User struct { ID int64 ` + "`forge:\"primary,required\"`" + ` }`,
			},
		},
		{
			name: "duplicate table", want: "duplicate table people",
			sources: map[string]string{"model.go": `package models
			type User struct { _ struct{} ` + "`forge:\"table=people\"`" + `; ID int64 ` + "`forge:\"primary,required\"`" + ` }
			type Person struct { _ struct{} ` + "`forge:\"table=people\"`" + `; ID int64 ` + "`forge:\"primary,required\"`" + ` }`},
		},
		{
			name: "duplicate column", want: "duplicate column value",
			sources: map[string]string{"model.go": `package models; type Item struct {
				ID int64 ` + "`forge:\"primary,required\"`" + `
				First string ` + "`db:\"value\" forge:\"required\"`" + `
				Second string ` + "`db:\"value\" forge:\"required\"`" + `
			}`},
		},
		{
			name: "missing primary", want: "exactly one primary",
			sources: map[string]string{"model.go": `package models; type Item struct { Name string ` + "`forge:\"required\"`" + ` }`},
		},
		{
			name: "multiple primary", want: "exactly one primary",
			sources: map[string]string{"model.go": `package models; type Item struct {
				ID int64 ` + "`forge:\"primary,required\"`" + `
				Code string ` + "`forge:\"primary,required\"`" + `
			}`},
		},
		{
			name: "missing nullability", want: "exactly one of required or nullable",
			sources: map[string]string{"model.go": `package models; type Item struct { ID int64 ` + "`forge:\"primary\"`" + ` }`},
		},
		{
			name: "pointer required", want: "pointer/null wrapper and nullable metadata disagree",
			sources: map[string]string{"model.go": `package models; type Item struct { ID *int64 ` + "`forge:\"primary,required\"`" + ` }`},
		},
		{
			name: "value nullable", want: "pointer/null wrapper and nullable metadata disagree",
			sources: map[string]string{"model.go": `package models; type Item struct { ID int64 ` + "`forge:\"primary,required\"`" + `; Name string ` + "`forge:\"nullable\"`" + ` }`},
		},
		{
			name: "unsupported type", want: "unsupported Go type map[string]string",
			sources: map[string]string{"model.go": `package models; type Item struct { ID int64 ` + "`forge:\"primary,required\"`" + `; Data map[string]string ` + "`forge:\"required\"`" + ` }`},
		},
		{
			name: "unresolved import alias", want: "import alias mystery cannot be resolved",
			sources: map[string]string{"model.go": `package models; type Item struct { ID int64 ` + "`forge:\"primary,required\"`" + `; Data mystery.Value ` + "`forge:\"required\"`" + ` }`},
		},
		{
			name: "dot import", want: "dot imports cannot be resolved",
			sources: map[string]string{"model.go": `package models; import . "time"; type Item struct { ID int64 ` + "`forge:\"primary,required\"`" + `; At Time ` + "`forge:\"required\"`" + ` }`},
		},
		{
			name: "unsupported imported type", want: "unsupported imported Go type Duration",
			sources: map[string]string{"model.go": `package models; import "time"; type Item struct { ID int64 ` + "`forge:\"primary,required\"`" + `; Span time.Duration ` + "`forge:\"required\"`" + ` }`},
		},
		{
			name: "unknown flag", want: "unsupported forge flag searchable",
			sources: map[string]string{"model.go": `package models; type Item struct { ID int64 ` + "`forge:\"primary,required,searchable\"`" + ` }`},
		},
		{
			name: "unsafe column", want: "unsafe column name",
			sources: map[string]string{"model.go": `package models; type Item struct { ID int64 ` + "`db:\"id;drop\" forge:\"primary,required\"`" + ` }`},
		},
		{
			name: "unsafe table", want: "unsafe table name",
			sources: map[string]string{"model.go": `package models; type Item struct { _ struct{} ` + "`forge:\"table=items;drop\"`" + `; ID int64 ` + "`forge:\"primary,required\"`" + ` }`},
		},
		{
			name: "unsafe database type", want: "unsupported database type",
			sources: map[string]string{"model.go": `package models; type Item struct { ID int64 ` + "`forge:\"primary,required,type=bigint;drop\"`" + ` }`},
		},
		{
			name: "incompatible database type", want: "unsupported database type \"boolean\" for string",
			sources: map[string]string{"model.go": `package models; type Item struct { ID int64 ` + "`forge:\"primary,required\"`" + `; Name string ` + "`forge:\"required,type=boolean\"`" + ` }`},
		},
		{
			name: "unsafe default", want: "unsafe or unsupported default",
			sources: map[string]string{"model.go": `package models; type Item struct { ID int64 ` + "`forge:\"primary,required\"`" + `; Name string ` + "`forge:\"required,default='x';DROP\"`" + ` }`},
		},
		{
			name: "default type mismatch", want: "unsafe or unsupported default",
			sources: map[string]string{"model.go": `package models; type Item struct { ID int64 ` + "`forge:\"primary,required\"`" + `; Name string ` + "`forge:\"required,default=true\"`" + ` }`},
		},
		{
			name: "null default required", want: "unsafe or unsupported default",
			sources: map[string]string{"model.go": `package models; type Item struct { ID int64 ` + "`forge:\"primary,required\"`" + `; Name string ` + "`forge:\"required,default=NULL\"`" + ` }`},
		},
		{
			name: "duplicate forge struct tag", want: "duplicate forge struct tag",
			sources: map[string]string{"model.go": `package models; type Item struct { ID int64 ` + "`forge:\"primary,required\" forge:\"primary,required\"`" + ` }`},
		},
		{
			name: "generated default", want: "cannot also declare a default",
			sources: map[string]string{"model.go": `package models; type Item struct { ID int64 ` + "`forge:\"primary,generated,required,default=1\"`" + ` }`},
		},
		{
			name: "on delete without reference", want: "on_delete without references",
			sources: map[string]string{"model.go": `package models; type Item struct { ID int64 ` + "`forge:\"primary,required,on_delete=cascade\"`" + ` }`},
		},
		{
			name: "set null required", want: "uses set_null but is required",
			sources: map[string]string{"model.go": `package models
			type User struct { ID int64 ` + "`forge:\"primary,required\"`" + ` }
			type Item struct { ID int64 ` + "`forge:\"primary,required\"`" + `; UserID int64 ` + "`forge:\"required,references=User.ID,on_delete=set_null\"`" + ` }`},
		},
		{
			name: "missing reference model", want: "references missing model User",
			sources: map[string]string{"model.go": `package models; type Item struct { ID int64 ` + "`forge:\"primary,required\"`" + `; UserID int64 ` + "`forge:\"required,references=User.ID\"`" + ` }`},
		},
		{
			name: "missing reference key", want: "references missing key User.Code",
			sources: map[string]string{"model.go": `package models
			type User struct { ID int64 ` + "`forge:\"primary,required\"`" + ` }
			type Item struct { ID int64 ` + "`forge:\"primary,required\"`" + `; UserID int64 ` + "`forge:\"required,references=User.Code\"`" + ` }`},
		},
		{
			name: "reference non key", want: "references non-key field User.Code",
			sources: map[string]string{"model.go": `package models
			type User struct { ID int64 ` + "`forge:\"primary,required\"`" + `; Code int64 ` + "`forge:\"required\"`" + ` }
			type Item struct { ID int64 ` + "`forge:\"primary,required\"`" + `; UserID int64 ` + "`forge:\"required,references=User.Code\"`" + ` }`},
		},
		{
			name: "reference type mismatch", want: "type is incompatible",
			sources: map[string]string{"model.go": `package models
			type User struct { ID int64 ` + "`forge:\"primary,required\"`" + ` }
			type Item struct { ID int64 ` + "`forge:\"primary,required\"`" + `; UserID string ` + "`forge:\"required,references=User.ID\"`" + ` }`},
		},
		{
			name: "reference database type mismatch", want: "type is incompatible",
			sources: map[string]string{"model.go": `package models
			type User struct { ID string ` + "`forge:\"primary,required,type=uuid\"`" + ` }
			type Item struct { ID int64 ` + "`forge:\"primary,required\"`" + `; UserID string ` + "`forge:\"required,references=User.ID\"`" + ` }`},
		},
		{
			name: "relation missing target", want: "requires an exported target model",
			sources: map[string]string{"model.go": `package models
			type User struct { ID int64 ` + "`forge:\"primary,required\"`" + `; Posts []Post ` + "`forge:\"has_many,foreign_key=UserID,references=ID\"`" + ` }
			type Post struct { ID int64 ` + "`forge:\"primary,required\"`" + `; UserID int64 ` + "`forge:\"required,references=User.ID\"`" + ` }`},
		},
		{
			name: "relation missing keys", want: "requires exactly foreign_key and references",
			sources: map[string]string{"model.go": `package models
			type User struct { ID int64 ` + "`forge:\"primary,required\"`" + `; Posts []Post ` + "`forge:\"has_many,target=Post\"`" + ` }
			type Post struct { ID int64 ` + "`forge:\"primary,required\"`" + ` }`},
		},
		{
			name: "relation wrong shape", want: "incompatible field shape",
			sources: map[string]string{"model.go": `package models
			type User struct { ID int64 ` + "`forge:\"primary,required\"`" + `; Posts Post ` + "`forge:\"has_many,target=Post,foreign_key=UserID,references=ID\"`" + ` }
			type Post struct { ID int64 ` + "`forge:\"primary,required\"`" + `; UserID int64 ` + "`forge:\"required,references=User.ID\"`" + ` }`},
		},
		{
			name: "unsafe relation key", want: "unsafe foreign_key",
			sources: map[string]string{"model.go": `package models
			type User struct { ID int64 ` + "`forge:\"primary,required\"`" + `; Posts []Post ` + "`forge:\"has_many,target=Post,foreign_key=user-id,references=ID\"`" + ` }
			type Post struct { ID int64 ` + "`forge:\"primary,required\"`" + ` }`},
		},
		{
			name: "relation metadata mismatch", want: "does not match UserID reference metadata",
			sources: map[string]string{"model.go": `package models
			type User struct { ID int64 ` + "`forge:\"primary,required\"`" + ` }
			type Other struct { ID int64 ` + "`forge:\"primary,required\"`" + ` }
			type Post struct { ID int64 ` + "`forge:\"primary,required\"`" + `; UserID int64 ` + "`forge:\"required,references=Other.ID\"`" + `; User User ` + "`forge:\"belongs_to,target=User,foreign_key=UserID,references=ID\"`" + ` }`},
		},
		{
			name: "ambiguous relation", want: "is ambiguous with Posts",
			sources: map[string]string{"model.go": `package models
			type User struct { ID int64 ` + "`forge:\"primary,required\"`" + `; Posts []Post ` + "`forge:\"has_many,target=Post,foreign_key=UserID,references=ID\"`" + `; Articles []Post ` + "`forge:\"has_many,target=Post,foreign_key=UserID,references=ID\"`" + ` }
			type Post struct { ID int64 ` + "`forge:\"primary,required\"`" + `; UserID int64 ` + "`forge:\"required,references=User.ID\"`" + ` }`},
		},
		{
			name: "embedded field", want: "cannot contain embedded fields",
			sources: map[string]string{"model.go": `package models; type Base struct { ID int64 ` + "`forge:\"primary,required\"`" + ` }; type Item struct { Base }`},
		},
		{
			name: "generic model", want: "cannot be generic",
			sources: map[string]string{"model.go": `package models; type Item[T any] struct { ID int64 ` + "`forge:\"primary,required\"`" + ` }`},
		},
		{
			name: "composed identifier too long", want: "generated database identifier",
			sources: map[string]string{"model.go": `package models; type Item struct { _ struct{} ` + "`forge:\"table=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\"`" + `; ID int64 ` + "`forge:\"primary,required\"`" + ` }`},
		},
		{
			name: "derived table too long", want: "derives unsafe table name",
			sources: map[string]string{"model.go": `package models; type Aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa struct { ID int64 ` + "`forge:\"primary,required\"`" + ` }`},
		},
		{
			name: "join identifier too long", want: "unsafe join_table",
			sources: map[string]string{"model.go": `package models
			type User struct { ID int64 ` + "`forge:\"primary,required\"`" + `; Roles []Role ` + "`forge:\"many_to_many,target=Role,references=ID,target_key=ID,join_table=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa,join_foreign_key=user_id,join_reference_key=role_id\"`" + ` }
			type Role struct { ID int64 ` + "`forge:\"primary,required\"`" + ` }`},
		},
		{
			name: "composed identifier collision", want: "conflicts with",
			sources: map[string]string{"model.go": `package models
			type One struct { _ struct{} ` + "`forge:\"table=a_b\"`" + `; ID int64 ` + "`forge:\"primary,required\"`" + `; C string ` + "`forge:\"required,index\"`" + ` }
			type Two struct { _ struct{} ` + "`forge:\"table=a\"`" + `; ID int64 ` + "`forge:\"primary,required\"`" + `; BC string ` + "`db:\"b_c\" forge:\"required,index\"`" + ` }`},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			directory := writeModelSources(t, test.sources)
			_, err := parseModelSchema(directory)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected error containing %q, got %v", test.want, err)
			}
		})
	}
}

func TestModelSchemaPostgreSQLIdentifierBoundaries(t *testing.T) {
	valid := "a" + strings.Repeat("b", 62)
	invalid := valid + "c"
	if !safeIdentifier(valid) {
		t.Fatal("63-byte PostgreSQL identifier was rejected")
	}
	if safeIdentifier(invalid) {
		t.Fatal("64-byte PostgreSQL identifier was accepted")
	}
	directory := writeModelSources(t, map[string]string{"model.go": `package models
type Item struct {
	ID int64 ` + "`forge:\"primary,required\"`" + `
	Value string ` + "`db:\"" + valid + "\" forge:\"required\"`" + `
}`})
	if _, err := parseModelSchema(directory); err != nil {
		t.Fatalf("63-byte column should parse when no longer name is composed: %v", err)
	}
	directory = writeModelSources(t, map[string]string{"model.go": `package models
type Item struct {
	ID int64 ` + "`forge:\"primary,required\"`" + `
	Value string ` + "`db:\"" + invalid + "\" forge:\"required\"`" + `
}`})
	if _, err := parseModelSchema(directory); err == nil || !strings.Contains(err.Error(), "unsafe column name") {
		t.Fatalf("expected 64-byte column rejection, got %v", err)
	}
}

func TestParseModelSchemaRejectsConditionalSourceFiles(t *testing.T) {
	tests := map[string]string{
		"tagged.go": `//go:build linux

package models
type Item struct { ID int64 ` + "`forge:\"primary,required\"`" + ` }
`,
		"legacy.go": `// +build linux

package models
type Item struct { ID int64 ` + "`forge:\"primary,required\"`" + ` }
`,
		"model_linux.go": `package models
type Item struct { ID int64 ` + "`forge:\"primary,required\"`" + ` }
`,
		"model_windows_amd64.go": `package models
type Item struct { ID int64 ` + "`forge:\"primary,required\"`" + ` }
`,
	}
	for filename, source := range tests {
		t.Run(filename, func(t *testing.T) {
			directory := writeModelSources(t, map[string]string{filename: source})
			if _, err := parseModelSchema(directory); err == nil || !strings.Contains(err.Error(), "model files cannot use build constraints") {
				t.Fatalf("expected build-constraint rejection, got %v", err)
			}
		})
	}
}

func TestParseModelSchemaRejectsOnlyForeignKeyCycles(t *testing.T) {
	directory := writeModelSources(t, map[string]string{"model.go": `package models

type Alpha struct {
	ID     int64 ` + "`forge:\"primary,required\"`" + `
	BetaID int64 ` + "`forge:\"required,references=Beta.ID\"`" + `
	Beta   *Beta ` + "`forge:\"belongs_to,target=Beta,foreign_key=BetaID,references=ID\"`" + `
}
type Beta struct {
	ID      int64 ` + "`forge:\"primary,required\"`" + `
	AlphaID int64 ` + "`forge:\"required,references=Alpha.ID\"`" + `
	Alpha   *Alpha ` + "`forge:\"belongs_to,target=Alpha,foreign_key=AlphaID,references=ID\"`" + `
}
`})
	_, err := parseModelSchema(directory)
	if err == nil || !strings.Contains(err.Error(), "foreign-key cycle") || !strings.Contains(err.Error(), "Alpha -> Beta -> Alpha") {
		t.Fatalf("expected deterministic foreign-key cycle, got %v", err)
	}
}

func TestParseModelSchemaAllowsSelfReferences(t *testing.T) {
	directory := writeModelSources(t, map[string]string{"model.go": `package models

type Category struct {
	ID       int64     ` + "`forge:\"primary,required\"`" + `
	ParentID *int64    ` + "`forge:\"nullable,references=Category.ID,on_delete=set_null\"`" + `
	Parent   *Category ` + "`forge:\"belongs_to,target=Category,foreign_key=ParentID,references=ID\"`" + `
	Children []Category ` + "`forge:\"has_many,target=Category,foreign_key=ParentID,references=ID\"`" + `
}
`})
	if _, err := parseModelSchema(directory); err != nil {
		t.Fatalf("self-reference should be emittable by one CREATE TABLE: %v", err)
	}
}

func TestParseModelSchemaIgnoresTestsAndGeneratedGo(t *testing.T) {
	directory := writeModelSources(t, map[string]string{
		"model.go":      `package models; type Item struct { ID int64 ` + "`forge:\"primary,required\"`" + ` }`,
		"model_test.go": `package models; type TestFixture struct { Value chan int }`,
		"orm_gen.go": `// Code generated by GoForge. DO NOT EDIT.
package models
type GeneratedDescriptor struct { Value map[string]any }
`,
	})
	schema, err := parseModelSchema(directory)
	if err != nil {
		t.Fatal(err)
	}
	if got := modelNames(schema); !reflect.DeepEqual(got, []string{"Item"}) {
		t.Fatalf("generated or test declarations leaked into schema: %v", got)
	}
}

func TestParseModelSchemaRequiresSource(t *testing.T) {
	directory := t.TempDir()
	if _, err := parseModelSchema(directory); err == nil || !strings.Contains(err.Error(), "contains no Go source files") {
		t.Fatalf("expected empty-directory error, got %v", err)
	}
}

func writeModelSources(t *testing.T, sources map[string]string) string {
	t.Helper()
	directory := t.TempDir()
	for name, source := range sources {
		filename := filepath.Join(directory, name)
		if err := os.WriteFile(filename, []byte(source), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return directory
}

func modelNames(schema modelSchema) []string {
	result := make([]string, len(schema.Models))
	for index := range schema.Models {
		result[index] = schema.Models[index].Name
	}
	return result
}

func fieldNames(model *modelDefinition) []string {
	result := make([]string, len(model.Fields))
	for index := range model.Fields {
		result[index] = model.Fields[index].Name
	}
	return result
}

func relationNames(model *modelDefinition) []string {
	result := make([]string, len(model.Relations))
	for index := range model.Relations {
		result[index] = model.Relations[index].Name
	}
	return result
}

func findModel(t *testing.T, schema modelSchema, name string) *modelDefinition {
	t.Helper()
	for index := range schema.Models {
		if schema.Models[index].Name == name {
			return &schema.Models[index]
		}
	}
	t.Fatalf("model %s not found", name)
	return nil
}

func findField(t *testing.T, model *modelDefinition, name string) *modelField {
	t.Helper()
	for index := range model.Fields {
		if model.Fields[index].Name == name {
			return &model.Fields[index]
		}
	}
	t.Fatalf("field %s.%s not found", model.Name, name)
	return nil
}

func findRelation(t *testing.T, model *modelDefinition, name string) *modelRelation {
	t.Helper()
	for index := range model.Relations {
		if model.Relations[index].Name == name {
			return &model.Relations[index]
		}
	}
	t.Fatalf("relation %s.%s not found", model.Name, name)
	return nil
}
