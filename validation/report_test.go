package validation_test

import (
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/ShanilKoshitha/goforge/validation"
)

type notReservedRule struct{}

func (notReservedRule) Validate(input validation.Input[string]) []validation.Issue {
	value, present := input.Value()
	if present && value == "admin" {
		return []validation.Issue{{Code: "account.reserved", Message: "is reserved"}}
	}
	return nil
}

var _ validation.Rule[string] = notReservedRule{}

func TestInputDistinguishesEveryBoundaryState(t *testing.T) {
	var zero validation.Input[string]
	tests := []struct {
		name      string
		input     validation.Input[string]
		state     validation.State
		provided  bool
		hasValue  bool
		wantValue string
	}{
		{name: "zero value", input: zero, state: validation.StateMissing},
		{name: "missing", input: validation.Missing[string](), state: validation.StateMissing},
		{name: "null", input: validation.Null[string](), state: validation.StateNull, provided: true},
		{name: "empty", input: validation.Empty[string](), state: validation.StateEmpty, provided: true},
		{name: "present", input: validation.Present("Ada"), state: validation.StatePresent, provided: true, hasValue: true, wantValue: "Ada"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.input.State(); got != test.state {
				t.Fatalf("State() = %q, want %q", got, test.state)
			}
			if got := test.input.Provided(); got != test.provided {
				t.Fatalf("Provided() = %t, want %t", got, test.provided)
			}
			value, ok := test.input.Value()
			if ok != test.hasValue || value != test.wantValue {
				t.Fatalf("Value() = %q, %t; want %q, %t", value, ok, test.wantValue, test.hasValue)
			}
		})
	}
}

func TestApplicationRuleAndRuleFuncProduceStructuredViolations(t *testing.T) {
	password := "correct horse battery staple"
	confirmation := validation.RuleFunc[string](func(input validation.Input[string]) []validation.Issue {
		value, present := input.Value()
		if present && value != password {
			return []validation.Issue{{Code: "password.confirmed", Message: "must match password"}}
		}
		return nil
	})

	report := validation.Join(
		validation.Apply("username", validation.Present("admin"), notReservedRule{}),
		validation.Apply("password_confirmation", validation.Present("different"), confirmation),
	)
	want := []validation.Violation{
		{Path: "username", Code: "account.reserved", Message: "is reserved"},
		{Path: "password_confirmation", Code: "password.confirmed", Message: "must match password"},
	}
	if got := report.Violations(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Violations() = %#v, want %#v", got, want)
	}
	if report.Empty() || report.Count() != 2 || report.Error() != "validation failed with 2 violation(s)" {
		t.Fatalf("unexpected report summary: empty=%t count=%d error=%q", report.Empty(), report.Count(), report.Error())
	}
}

func TestRequiredUsesExplicitStateRatherThanZeroValue(t *testing.T) {
	for _, input := range []validation.Input[int]{
		validation.Missing[int](), validation.Null[int](), validation.Empty[int](),
	} {
		report := validation.Apply("age", input, validation.Required[int]())
		assertOneViolation(t, report, "age", validation.CodeRequired, "is required")
	}

	for _, input := range []validation.Input[int]{validation.Present(0), validation.Present(42)} {
		if report := validation.Apply("age", input, validation.Required[int]()); !report.Empty() {
			t.Fatalf("present value failed Required: %#v", report.Violations())
		}
	}
}

func TestStringAndNumberRangesAreTypedAndStable(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		report  validation.Report
		code    string
		message string
	}{
		{
			name: "Unicode string minimum", path: "name", report: validation.Apply("name", validation.Present("☕"), validation.StringLength(2, 4)),
			code: validation.CodeStringMin, message: "must be at least 2 characters",
		},
		{
			name: "string maximum", path: "name", report: validation.Apply("name", validation.Present("forge"), validation.StringLength(2, 4)),
			code: validation.CodeStringMax, message: "must be at most 4 characters",
		},
		{
			name: "number minimum", path: "priority", report: validation.Apply("priority", validation.Present(0), validation.NumberRange(1, 5)),
			code: validation.CodeNumberMin, message: "must be at least 1",
		},
		{
			name: "number maximum", path: "priority", report: validation.Apply("priority", validation.Present(int64(6)), validation.NumberRange[int64](1, 5)),
			code: validation.CodeNumberMax, message: "must be at most 5",
		},
		{
			name: "NaN", path: "ratio", report: validation.Apply("ratio", validation.Present(math.NaN()), validation.NumberRange(0.0, 1.0)),
			code: validation.CodeNumberFinite, message: "must be a finite number",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertOneViolation(t, test.report, test.path, test.code, test.message)
		})
	}

	for _, report := range []validation.Report{
		validation.Apply("name", validation.Present("日本"), validation.StringLength(2, 4)),
		validation.Apply("priority", validation.Present(5), validation.NumberRange(1, 5)),
		validation.Apply("optional", validation.Missing[int](), validation.NumberRange(1, 5)),
	} {
		if !report.Empty() {
			t.Fatalf("valid range produced %#v", report.Violations())
		}
	}
}

func TestListRulesReportListAndIndexedItemPaths(t *testing.T) {
	report := validation.Apply(
		"tags",
		validation.Present([]string{"go", "x", "go"}),
		validation.ListSize[string](4, 5),
		validation.ListUnique[string](),
		validation.Each(validation.StringLength(2, 10)),
	)
	want := []validation.Violation{
		{Path: "tags", Code: validation.CodeListMin, Message: "must contain at least 4 items"},
		{Path: "tags", Code: validation.CodeListUnique, Message: "must not contain duplicate items"},
		{Path: "tags.1", Code: validation.CodeStringMin, Message: "must be at least 2 characters"},
	}
	if got := report.Violations(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Violations() = %#v, want %#v", got, want)
	}

	if report := validation.Apply("tags", validation.Present([]string{}), validation.ListSize[string](0, 2)); !report.Empty() {
		t.Fatalf("decoded empty list should be validated as present: %#v", report.Violations())
	}
}

func TestPrefixJoinAndMessagesPreservePathsOrderAndCopies(t *testing.T) {
	base := validation.Join(
		validation.Apply("city", validation.Missing[string](), validation.Required[string]()),
		validation.Apply("postal_code", validation.Present("x"), validation.StringLength(2, 10)),
	)
	report := base.Prefix("shipping.address")
	want := []validation.Violation{
		{Path: "shipping.address.city", Code: validation.CodeRequired, Message: "is required"},
		{Path: "shipping.address.postal_code", Code: validation.CodeStringMin, Message: "must be at least 2 characters"},
	}
	got := report.Violations()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Violations() = %#v, want %#v", got, want)
	}
	got[0].Message = "mutated"
	if report.Violations()[0].Message != "is required" {
		t.Fatal("Violations exposed mutable report storage")
	}
	forPath := report.For("shipping.address.city")
	forPath[0].Message = "mutated"
	if report.For("shipping.address.city")[0].Message != "is required" {
		t.Fatal("For exposed mutable report storage")
	}
	messages := report.Messages()
	messages["shipping.address.city"][0] = "mutated"
	if report.Messages()["shipping.address.city"][0] != "is required" {
		t.Fatal("Messages exposed mutable report storage")
	}
}

func TestWhenComposesConditionalAndCrossFieldRules(t *testing.T) {
	requiredWhenPublished := validation.When(true, validation.Required[string]())
	assertOneViolation(
		t,
		validation.Apply("published_at", validation.Missing[string](), requiredWhenPublished),
		"published_at", validation.CodeRequired, "is required",
	)
	if report := validation.Apply(
		"published_at", validation.Missing[string](), validation.When(false, validation.Required[string]()),
	); !report.Empty() {
		t.Fatalf("false conditional produced %#v", report.Violations())
	}

	other := validation.Present("secret")
	sameAsOther := validation.RuleFunc[string](func(input validation.Input[string]) []validation.Issue {
		value, valueOK := input.Value()
		comparison, comparisonOK := other.Value()
		if valueOK && comparisonOK && value != comparison {
			return []validation.Issue{{Code: "field.same", Message: "must match the related field"}}
		}
		return nil
	})
	assertOneViolation(
		t,
		validation.Apply("confirmation", validation.Present("different"), sameAsOther),
		"confirmation", "field.same", "must match the related field",
	)
}

func TestInvalidConfigurationBecomesDeterministicViolations(t *testing.T) {
	var nilRule validation.Rule[string]
	var nilFunc validation.RuleFunc[string]
	tests := []struct {
		name    string
		report  validation.Report
		path    string
		message string
	}{
		{name: "field path", report: validation.Apply("bad..path", validation.Present("x"), validation.Required[string]()), path: "$", message: "validation field path must be a dot-separated field path"},
		{name: "no rules", report: validation.Apply[string]("name", validation.Present("x")), path: "name", message: "validation field must have at least one rule"},
		{name: "nil rule", report: validation.Apply("name", validation.Present("x"), nilRule), path: "name", message: "validation rule must not be nil"},
		{name: "nil function", report: validation.Apply("name", validation.Present("x"), nilFunc), path: "name", message: "validation rule function must not be nil"},
		{name: "string bounds", report: validation.Apply("name", validation.Present("x"), validation.StringLength(2, 1)), path: "name", message: "string length bounds must be non-negative and minimum must not exceed maximum"},
		{name: "number bounds", report: validation.Apply("age", validation.Present(1), validation.NumberRange(2, 1)), path: "age", message: "number range minimum and maximum must be finite ordered numbers"},
		{name: "NaN bounds", report: validation.Apply("ratio", validation.Present(1.0), validation.NumberRange(math.NaN(), 1.0)), path: "ratio", message: "number range minimum and maximum must be finite ordered numbers"},
		{name: "list bounds", report: validation.Apply("tags", validation.Present([]string{}), validation.ListSize[string](-1, 2)), path: "tags", message: "list size bounds must be non-negative and minimum must not exceed maximum"},
		{name: "empty each", report: validation.Apply("tags", validation.Present([]string{}), validation.Each[string]()), path: "tags", message: "list item validation must have at least one rule"},
		{name: "invalid nested even when false", report: validation.Apply("age", validation.Present(1), validation.When(false, validation.NumberRange(2, 1))), path: "age", message: "number range minimum and maximum must be finite ordered numbers"},
		{name: "invalid prefix", report: validation.Apply("name", validation.Missing[string](), validation.Required[string]()).Prefix("bad..prefix"), path: "$", message: "validation report prefix must be a dot-separated field path"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertOneViolation(t, test.report, test.path, validation.CodeInvalidConfiguration, test.message)
		})
	}
}

func TestMalformedCustomRuleOutputCannotCreateUnsafeReportEntries(t *testing.T) {
	rule := validation.RuleFunc[string](func(validation.Input[string]) []validation.Issue {
		return []validation.Issue{
			{Path: "bad..path", Code: "custom", Message: "message"},
			{Code: "UPPERCASE", Message: "message"},
			{Code: "custom", Message: "line one\nline two"},
			{Code: "custom", Message: strings.Repeat("x", 1025)},
		}
	})
	report := validation.Apply("name", validation.Present("value"), rule)
	if report.Count() != 4 {
		t.Fatalf("Count() = %d, want 4", report.Count())
	}
	for _, violation := range report.Violations() {
		if violation.Path != "name" || violation.Code != validation.CodeInvalidConfiguration || violation.Message != "validation rule returned an invalid issue" {
			t.Fatalf("unexpected configuration violation %#v", violation)
		}
	}
}

func TestStructuredReportConvertsToLegacyErrors(t *testing.T) {
	report := validation.Join(
		validation.Apply("name", validation.Missing[string](), validation.Required[string]()),
		validation.Apply("name", validation.Present("x"), validation.StringLength(2, 10)),
	)
	want := validation.Errors{"name": {"is required", "must be at least 2 characters"}}
	if got := report.Messages(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Messages() = %#v, want %#v", got, want)
	}
}

func TestReportMarshalsAsOrderedStructuredDetails(t *testing.T) {
	report := validation.Join(
		validation.Apply("name", validation.Missing[string](), validation.Required[string]()),
		validation.Apply("age", validation.Present(10), validation.NumberRange(18, 120)),
	)
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	want := `[{"path":"name","code":"required","message":"is required"},{"path":"age","code":"number.min","message":"must be at least 18"}]`
	if string(encoded) != want {
		t.Fatalf("MarshalJSON() = %s, want %s", encoded, want)
	}

	empty, err := json.Marshal(validation.Report{})
	if err != nil {
		t.Fatal(err)
	}
	if string(empty) != "[]" {
		t.Fatalf("empty MarshalJSON() = %s, want []", empty)
	}
}

func assertOneViolation(t *testing.T, report validation.Report, path, code, message string) {
	t.Helper()
	want := []validation.Violation{{Path: path, Code: code, Message: message}}
	if got := report.Violations(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Violations() = %#v, want %#v", got, want)
	}
}
