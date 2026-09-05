package validation

import (
	"fmt"
	"math"
	"strconv"
	"unicode/utf8"
)

const (
	CodeRequired     = "required"
	CodeStringMin    = "string.min"
	CodeStringMax    = "string.max"
	CodeNumberMin    = "number.min"
	CodeNumberMax    = "number.max"
	CodeNumberFinite = "number.finite"
	CodeListMin      = "list.min"
	CodeListMax      = "list.max"
	CodeListUnique   = "list.unique"
)

// Rule is an application-implementable, typed validation rule. Implementations
// return relative issues; Apply supplies the fully qualified field path.
type Rule[T any] interface {
	Validate(Input[T]) []Issue
}

// RuleFunc adapts an ordinary function into a Rule.
type RuleFunc[T any] func(Input[T]) []Issue

func (rule RuleFunc[T]) Validate(input Input[T]) []Issue {
	if rule == nil {
		return []Issue{configurationIssue("validation rule function must not be nil")}
	}
	return rule(input)
}

func (rule RuleFunc[T]) configurationIssues() []Issue {
	if rule == nil {
		return []Issue{configurationIssue("validation rule function must not be nil")}
	}
	return nil
}

type configurationReporter interface {
	configurationIssues() []Issue
}

// Apply evaluates rules for one field and returns fully qualified violations.
// Invalid field paths, nil rules, and malformed rule output are reported with
// CodeInvalidConfiguration and never silently ignored.
func Apply[T any](path string, input Input[T], rules ...Rule[T]) Report {
	if !validPath(path) {
		return configurationReport("validation field path must be a dot-separated field path")
	}
	if len(rules) == 0 {
		return Report{violations: []Violation{{
			Path: path, Code: CodeInvalidConfiguration, Message: "validation field must have at least one rule",
		}}}
	}

	var report Report
	for _, rule := range rules {
		if rule == nil {
			report.violations = append(report.violations, configurationViolation(path, "validation rule must not be nil"))
			continue
		}
		if configured, ok := rule.(configurationReporter); ok {
			issues := configured.configurationIssues()
			if len(issues) > 0 {
				report.appendIssues(path, issues)
				continue
			}
		}
		report.appendIssues(path, rule.Validate(input))
	}
	return report
}

func (report *Report) appendIssues(path string, issues []Issue) {
	for _, issue := range issues {
		if !validRelativePath(issue.Path) || !validCode(issue.Code) || !validMessage(issue.Message) {
			report.violations = append(report.violations, configurationViolation(path, "validation rule returned an invalid issue"))
			continue
		}
		report.violations = append(report.violations, Violation{
			Path: joinPath(path, issue.Path), Code: issue.Code, Message: issue.Message,
		})
	}
}

func configurationViolation(path, message string) Violation {
	return Violation{Path: path, Code: CodeInvalidConfiguration, Message: message}
}

func Required[T any]() Rule[T] {
	return RuleFunc[T](func(input Input[T]) []Issue {
		if input.State() == StatePresent {
			return nil
		}
		return []Issue{{Code: CodeRequired, Message: "is required"}}
	})
}

// StringLength validates Unicode code points. Non-present states are left to
// state rules such as Required.
func StringLength(minimum, maximum int) Rule[string] {
	if minimum < 0 || maximum < 0 || minimum > maximum {
		return invalidRule[string]("string length bounds must be non-negative and minimum must not exceed maximum")
	}
	return RuleFunc[string](func(input Input[string]) []Issue {
		value, present := input.Value()
		if !present {
			return nil
		}
		length := utf8.RuneCountInString(value)
		if length < minimum {
			return []Issue{{Code: CodeStringMin, Message: fmt.Sprintf("must be at least %d characters", minimum)}}
		}
		if length > maximum {
			return []Issue{{Code: CodeStringMax, Message: fmt.Sprintf("must be at most %d characters", maximum)}}
		}
		return nil
	})
}

type Number interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64 |
		~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 | ~uintptr |
		~float32 | ~float64
}

func NumberRange[T Number](minimum, maximum T) Rule[T] {
	if !finite(minimum) || !finite(maximum) || minimum > maximum {
		return invalidRule[T]("number range minimum and maximum must be finite ordered numbers")
	}
	return RuleFunc[T](func(input Input[T]) []Issue {
		value, present := input.Value()
		if !present {
			return nil
		}
		if !finite(value) {
			return []Issue{{Code: CodeNumberFinite, Message: "must be a finite number"}}
		}
		if value < minimum {
			return []Issue{{Code: CodeNumberMin, Message: fmt.Sprintf("must be at least %v", minimum)}}
		}
		if value > maximum {
			return []Issue{{Code: CodeNumberMax, Message: fmt.Sprintf("must be at most %v", maximum)}}
		}
		return nil
	})
}

func finite[T Number](value T) bool {
	number := float64(value)
	return !math.IsNaN(number) && !math.IsInf(number, 0)
}

func ListSize[T any](minimum, maximum int) Rule[[]T] {
	if minimum < 0 || maximum < 0 || minimum > maximum {
		return invalidRule[[]T]("list size bounds must be non-negative and minimum must not exceed maximum")
	}
	return RuleFunc[[]T](func(input Input[[]T]) []Issue {
		value, present := input.Value()
		if !present {
			return nil
		}
		if len(value) < minimum {
			return []Issue{{Code: CodeListMin, Message: fmt.Sprintf("must contain at least %d items", minimum)}}
		}
		if len(value) > maximum {
			return []Issue{{Code: CodeListMax, Message: fmt.Sprintf("must contain at most %d items", maximum)}}
		}
		return nil
	})
}

func ListUnique[T comparable]() Rule[[]T] {
	return RuleFunc[[]T](func(input Input[[]T]) []Issue {
		value, present := input.Value()
		if !present {
			return nil
		}
		seen := make(map[T]struct{}, len(value))
		for _, item := range value {
			if _, exists := seen[item]; exists {
				return []Issue{{Code: CodeListUnique, Message: "must not contain duplicate items"}}
			}
			seen[item] = struct{}{}
		}
		return nil
	})
}

// Each applies rules to every decoded list item and reports indexed paths.
func Each[T any](rules ...Rule[T]) Rule[[]T] {
	return eachRule[T]{rules: append([]Rule[T](nil), rules...)}
}

type eachRule[T any] struct {
	rules []Rule[T]
}

func (rule eachRule[T]) configurationIssues() []Issue {
	if len(rule.rules) == 0 {
		return []Issue{configurationIssue("list item validation must have at least one rule")}
	}
	var issues []Issue
	for _, itemRule := range rule.rules {
		if itemRule == nil {
			issues = append(issues, configurationIssue("list item validation rule must not be nil"))
			continue
		}
		if configured, ok := itemRule.(configurationReporter); ok {
			issues = append(issues, configured.configurationIssues()...)
		}
	}
	return issues
}

func (rule eachRule[T]) Validate(input Input[[]T]) []Issue {
	value, present := input.Value()
	if !present {
		return nil
	}
	var result []Issue
	for index, item := range value {
		for _, itemRule := range rule.rules {
			for _, issue := range itemRule.Validate(Present(item)) {
				issue.Path = joinPath(strconv.Itoa(index), issue.Path)
				result = append(result, issue)
			}
		}
	}
	return result
}

// When conditionally applies rules. Built-in configuration errors are still
// reported when the condition is false.
func When[T any](condition bool, rules ...Rule[T]) Rule[T] {
	return conditionalRule[T]{condition: condition, rules: append([]Rule[T](nil), rules...)}
}

type conditionalRule[T any] struct {
	condition bool
	rules     []Rule[T]
}

func (rule conditionalRule[T]) configurationIssues() []Issue {
	if len(rule.rules) == 0 {
		return []Issue{configurationIssue("conditional validation must have at least one rule")}
	}
	var issues []Issue
	for _, nested := range rule.rules {
		if nested == nil {
			issues = append(issues, configurationIssue("conditional validation rule must not be nil"))
			continue
		}
		if configured, ok := nested.(configurationReporter); ok {
			issues = append(issues, configured.configurationIssues()...)
		}
	}
	return issues
}

func (rule conditionalRule[T]) Validate(input Input[T]) []Issue {
	if !rule.condition {
		return nil
	}
	var result []Issue
	for _, nested := range rule.rules {
		result = append(result, nested.Validate(input)...)
	}
	return result
}

type invalidRule[T any] string

func (rule invalidRule[T]) Validate(Input[T]) []Issue {
	return rule.configurationIssues()
}

func (rule invalidRule[T]) configurationIssues() []Issue {
	return []Issue{configurationIssue(string(rule))}
}
