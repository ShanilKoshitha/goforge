package cli

import (
	"bytes"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestParseMakeResourceArgumentsAcceptsRepeatedFieldFormsInOrder(t *testing.T) {
	name, fields, belongsTo, schemaDriven, err := parseMakeResourceArguments([]string{
		"Article",
		"--field", "title:string",
		"--field=body:text:nullable",
		"--field", "priority:integer:required",
		"--field=published:boolean",
	})
	if err != nil {
		t.Fatal(err)
	}
	if name != "Article" {
		t.Fatalf("resource name = %q", name)
	}
	if !schemaDriven {
		t.Fatal("explicit fields did not select the schema-driven contract")
	}
	if len(belongsTo) != 0 {
		t.Fatalf("belongs-to relationships = %#v", belongsTo)
	}
	want := []resourceField{
		{Name: "title", GoName: "Title", Label: "Title", Kind: resourceFieldString, GoType: "string", DBType: "varchar(255)", Required: true},
		{Name: "body", GoName: "Body", Label: "Body", Kind: resourceFieldText, GoType: "*string", DBType: "text", Nullable: true},
		{Name: "priority", GoName: "Priority", Label: "Priority", Kind: resourceFieldInteger, GoType: "int64", DBType: "bigint", Required: true},
		{Name: "published", GoName: "Published", Label: "Published", Kind: resourceFieldBoolean, GoType: "bool", DBType: "boolean", Required: true},
	}
	if !reflect.DeepEqual(fields, want) {
		t.Fatalf("fields = %#v, want %#v", fields, want)
	}
}

func TestParseMakeResourceArgumentsRetainsLegacyDefaultField(t *testing.T) {
	name, fields, belongsTo, schemaDriven, err := parseMakeResourceArguments([]string{"Issue"})
	if err != nil {
		t.Fatal(err)
	}
	if name != "Issue" || len(belongsTo) != 0 || schemaDriven || !reflect.DeepEqual(fields, defaultResourceFields()) {
		t.Fatalf("legacy resource = %q schema-driven=%t fields=%#v belongs-to=%#v", name, schemaDriven, fields, belongsTo)
	}

	fields[0].Name = "changed"
	if got := defaultResourceFields()[0].Name; got != "name" {
		t.Fatalf("default field storage was shared: %q", got)
	}
}

func TestExplicitLegacyShapedFieldStillSelectsSchemaDrivenContract(t *testing.T) {
	name, fields, belongsTo, schemaDriven, err := parseMakeResourceArguments([]string{"Issue", "--field", "name:string"})
	if err != nil {
		t.Fatal(err)
	}
	want := []resourceField{{
		Name: "name", GoName: "Name", Label: "Name", Kind: resourceFieldString,
		GoType: "string", DBType: "varchar(255)", Required: true,
	}}
	if name != "Issue" || len(belongsTo) != 0 || !schemaDriven || !reflect.DeepEqual(fields, want) {
		t.Fatalf("explicit legacy-shaped resource = %q schema-driven=%t fields=%#v belongs-to=%#v", name, schemaDriven, fields, belongsTo)
	}
}

func TestParseMakeResourceArgumentsAcceptsRequiredBelongsToFormsInOrder(t *testing.T) {
	name, fields, belongsTo, schemaDriven, err := parseMakeResourceArguments([]string{
		"Article",
		"--field", "title:string",
		"--belongs-to", "category:Category",
		"--belongs-to=reviewer:APIClient",
	})
	if err != nil {
		t.Fatal(err)
	}
	if name != "Article" || !schemaDriven {
		t.Fatalf("resource = %q schema-driven=%t", name, schemaDriven)
	}
	if len(fields) != 1 || fields[0].Name != "title" {
		t.Fatalf("fields = %#v", fields)
	}
	want := []resourceBelongsTo{
		{
			Name: "category", GoName: "Category", Label: "Category",
			ForeignKey: "category_id", ForeignKeyGoName: "CategoryID",
			Target: "Category", Required: true,
		},
		{
			Name: "reviewer", GoName: "Reviewer", Label: "Reviewer",
			ForeignKey: "reviewer_id", ForeignKeyGoName: "ReviewerID",
			Target: "APIClient", Required: true,
		},
	}
	if !reflect.DeepEqual(belongsTo, want) {
		t.Fatalf("belongs-to relationships = %#v, want %#v", belongsTo, want)
	}
}

func TestBelongsToAloneUsesDefaultFieldAndSchemaDrivenContract(t *testing.T) {
	_, fields, belongsTo, schemaDriven, err := parseMakeResourceArguments([]string{
		"Issue", "--belongs-to", "category:blog-category",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !schemaDriven || !reflect.DeepEqual(fields, defaultResourceFields()) {
		t.Fatalf("schema-driven=%t fields=%#v", schemaDriven, fields)
	}
	if len(belongsTo) != 1 || belongsTo[0].Target != "BlogCategory" {
		t.Fatalf("belongs-to relationships = %#v", belongsTo)
	}
}

func TestParseResourceBelongsToRejectsInvalidSpecifications(t *testing.T) {
	tests := []struct {
		name           string
		resource       string
		specifications []string
		want           string
	}{
		{name: "empty", resource: "Issue", specifications: []string{""}, want: "cannot be empty"},
		{name: "missing target", resource: "Issue", specifications: []string{"category"}, want: "name:ExistingResource"},
		{name: "extra segment", resource: "Issue", specifications: []string{"category:Category:required"}, want: "name:ExistingResource"},
		{name: "uppercase name", resource: "Issue", specifications: []string{"Category:Category"}, want: "lower_snake_case"},
		{name: "trailing separator", resource: "Issue", specifications: []string{"category_:Category"}, want: "lower_snake_case"},
		{name: "surrounding whitespace", resource: "Issue", specifications: []string{" category:Category"}, want: "surrounding whitespace"},
		{name: "inner whitespace", resource: "Issue", specifications: []string{"category: Category"}, want: "without whitespace"},
		{name: "invalid target", resource: "Issue", specifications: []string{"category:123"}, want: "valid Go identifiers"},
		{name: "duplicate name", resource: "Issue", specifications: []string{"category:Category", "category:OtherCategory"}, want: "duplicate belongs-to"},
		{name: "self canonical", resource: "BlogPost", specifications: []string{"parent:blog-post"}, want: "cannot target its own resource"},
		{name: "reserved relation", resource: "Issue", specifications: []string{"owner:Category"}, want: "reserved"},
		{name: "reserved foreign key", resource: "Issue", specifications: []string{"user:Account"}, want: "reserved foreign key user_id"},
		{name: "overlong foreign key", resource: "Issue", specifications: []string{strings.Repeat("a", maximumResourceIdentifier) + ":Category"}, want: "unsafe foreign key"},
		{name: "overlong specification", resource: "Issue", specifications: []string{strings.Repeat("a", maximumResourceFieldLength+1)}, want: "exceeds 256 bytes"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := parseResourceBelongsTo(test.resource, test.specifications)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want text %q", err, test.want)
			}
		})
	}
}

func TestParseResourceBelongsToCapsRelationshipCount(t *testing.T) {
	specifications := make([]string, maximumResourceBelongsTo+1)
	for index := range specifications {
		specifications[index] = fmt.Sprintf("relation_%d:Category", index)
	}
	_, err := parseResourceBelongsTo("Issue", specifications)
	if err == nil || !strings.Contains(err.Error(), "maximum of 8") {
		t.Fatalf("error = %v", err)
	}
}

func TestParseMakeResourceArgumentsRejectsFieldAndRelationshipCollisions(t *testing.T) {
	tests := []struct {
		name      string
		arguments []string
		want      string
	}{
		{
			name: "relation name and field", want: "resource name category conflicts",
			arguments: []string{"Issue", "--field", "category:string", "--belongs-to", "category:Category"},
		},
		{
			name: "foreign key and field", want: "resource name category_id conflicts",
			arguments: []string{"Issue", "--field", "category_id:integer", "--belongs-to", "category:Category"},
		},
		{
			name: "foreign key Go name and field", want: "resource Go name APIID conflicts",
			arguments: []string{"Issue", "--field", "api_i_d:integer", "--belongs-to", "api:APIClient"},
		},
		{
			name: "relationship name and preceding foreign key", want: "resource name category_id conflicts",
			arguments: []string{"Issue", "--belongs-to", "category:Category", "--belongs-to", "category_id:OtherCategory"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, _, _, _, err := parseMakeResourceArguments(test.arguments)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want text %q", err, test.want)
			}
		})
	}
}

func TestParseResourceBelongsToAllowsDistinctAliasesForOneTarget(t *testing.T) {
	belongsTo, err := parseResourceBelongsTo("Invoice", []string{
		"billing_address:Address",
		"shipping_address:Address",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(belongsTo) != 2 || belongsTo[0].Target != "Address" || belongsTo[1].Target != "Address" {
		t.Fatalf("belongs-to relationships = %#v", belongsTo)
	}
}

func TestParseResourceFieldsRejectsInvalidSpecifications(t *testing.T) {
	tests := []struct {
		name           string
		specifications []string
		want           string
	}{
		{name: "empty", specifications: []string{""}, want: "cannot be empty"},
		{name: "missing type", specifications: []string{"title"}, want: "name:type"},
		{name: "uppercase", specifications: []string{"Title:string"}, want: "lower_snake_case"},
		{name: "trailing separator", specifications: []string{"title_:string"}, want: "lower_snake_case"},
		{name: "unknown type", specifications: []string{"title:uuid"}, want: "unknown type"},
		{name: "unknown modifier", specifications: []string{"title:string:optional"}, want: "unknown modifier"},
		{name: "conflicting modifiers", specifications: []string{"title:string:required:nullable"}, want: "both required and nullable"},
		{name: "duplicate modifier", specifications: []string{"title:string:required:required"}, want: "repeats modifier"},
		{name: "duplicate name", specifications: []string{"title:string", "title:text"}, want: "duplicate resource field"},
		{name: "canonical Go collision", specifications: []string{"api_id:string", "api_i_d:string"}, want: "same Go name APIID"},
		{name: "reserved SQL name", specifications: []string{"user_id:integer"}, want: "reserved"},
		{name: "reserved Go name", specifications: []string{"i_d:integer"}, want: "reserved Go name ID"},
		{name: "reserved form name", specifications: []string{"_token:string"}, want: "reserved"},
		{name: "overlong identifier", specifications: []string{strings.Repeat("a", maximumResourceIdentifier+1) + ":string"}, want: "safe PostgreSQL identifier"},
		{name: "overlong specification", specifications: []string{strings.Repeat("a", maximumResourceFieldLength+1)}, want: "exceeds 256 bytes"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := parseResourceFields(test.specifications)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want text %q", err, test.want)
			}
		})
	}
}

func TestParseResourceFieldsCapsFieldCount(t *testing.T) {
	specifications := make([]string, maximumResourceFields+1)
	for index := range specifications {
		specifications[index] = fmt.Sprintf("field_%d:string", index)
	}
	_, err := parseResourceFields(specifications)
	if err == nil || !strings.Contains(err.Error(), "maximum of 32") {
		t.Fatalf("error = %v", err)
	}
}

func TestParseMakeResourceArgumentsRejectsMalformedOptions(t *testing.T) {
	tests := [][]string{
		nil,
		{"--field", "title:string"},
		{"Issue", "--field"},
		{"Issue", "--field="},
		{"Issue", "--belongs-to"},
		{"Issue", "--belongs-to="},
		{"Issue", "--unknown", "title:string"},
		{"Issue", "Other"},
	}
	for _, arguments := range tests {
		_, _, _, _, err := parseMakeResourceArguments(arguments)
		if err == nil || !strings.Contains(err.Error(), "usage: forge make:resource") {
			t.Fatalf("arguments %#v error = %v", arguments, err)
		}
	}
}

func TestHelpDocumentsTypedResourceFieldsAndCompatibilityBoundary(t *testing.T) {
	var output bytes.Buffer
	if err := Run([]string{"help"}, &output, &output); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"forge make:resource <name> [--field <name>:<type>[:required|nullable]]...",
		"Resource field types: string, text, integer, boolean.",
		"Without --field, resources retain the",
		"legacy name:string and versionless-update contract.",
		"Any explicit --field uses",
		"schema-driven output and requires a positive version on updates.",
	} {
		if !strings.Contains(output.String(), expected) {
			t.Errorf("help omits %q:\n%s", expected, output.String())
		}
	}
}

func TestOtherMakeCommandsStillRequireExactlyOneName(t *testing.T) {
	for _, arguments := range [][]string{
		{"make:controller"},
		{"make:controller", "Users", "Extra"},
		{"make", "migration"},
		{"make", "job", "SendWelcome", "Extra"},
	} {
		var output bytes.Buffer
		err := Run(arguments, &output, &output)
		if err == nil || !strings.Contains(err.Error(), "usage: forge make:") {
			t.Fatalf("arguments %#v error = %v", arguments, err)
		}
	}
}

func TestResourceFieldLabelsPreserveCommonInitialisms(t *testing.T) {
	field, err := parseResourceField("api_client_id:string")
	if err != nil {
		t.Fatal(err)
	}
	if field.GoName != "APIClientID" || field.Label != "API Client ID" {
		t.Fatalf("names = %q %q", field.GoName, field.Label)
	}
}
