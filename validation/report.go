package validation

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// State records how an input appeared at the request boundary. It keeps
// omission and explicit null separate from an empty submitted value and a
// successfully decoded value.
type State string

const (
	StateMissing State = "missing"
	StateNull    State = "null"
	StateEmpty   State = "empty"
	StatePresent State = "present"
)

// Input is an explicitly classified value. Its zero value is Missing.
// Decoders remain application-owned and choose the state without reflection.
// Empty represents an input that contained no decodable value, such as an
// empty scalar form field. A decoded empty list is normally Present([]T{}).
type Input[T any] struct {
	state State
	value T
}

func Missing[T any]() Input[T] { return Input[T]{state: StateMissing} }
func Null[T any]() Input[T]    { return Input[T]{state: StateNull} }
func Empty[T any]() Input[T]   { return Input[T]{state: StateEmpty} }

func Present[T any](value T) Input[T] {
	return Input[T]{state: StatePresent, value: value}
}

func (input Input[T]) State() State {
	if input.state == "" {
		return StateMissing
	}
	return input.state
}

// Provided reports whether the input was present, including null and empty.
func (input Input[T]) Provided() bool { return input.State() != StateMissing }

// Value returns a decoded value only for Present input.
func (input Input[T]) Value() (T, bool) {
	return input.value, input.State() == StatePresent
}

// Issue is returned by a Rule. Path is relative to the field being checked;
// an empty path refers to that field. Messages are plain text, never trusted
// HTML. Built-in rules do not include submitted values in messages.
type Issue struct {
	Path    string
	Code    string
	Message string
}

// Violation is a fully qualified, transport-safe validation result. Path and
// Code are stable machine-readable identifiers. Message is single-line plain
// text and still must be escaped by its eventual renderer.
type Violation struct {
	Path    string `json:"path"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

const CodeInvalidConfiguration = "validation.configuration"

// Report preserves validation order while providing a structured alternative
// to the legacy Errors map.
type Report struct {
	violations []Violation
}

func (report Report) Empty() bool { return len(report.violations) == 0 }
func (report Report) Count() int  { return len(report.violations) }

func (report Report) Error() string {
	return fmt.Sprintf("validation failed with %d violation(s)", len(report.violations))
}

// MarshalJSON emits the ordered violation list, making Report safe to pass
// directly as structured HTTP error details.
func (report Report) MarshalJSON() ([]byte, error) {
	violations := report.violations
	if violations == nil {
		violations = []Violation{}
	}
	return json.Marshal(violations)
}

// Violations returns a defensive copy in rule evaluation order.
func (report Report) Violations() []Violation {
	return append([]Violation(nil), report.violations...)
}

// For returns a defensive copy of violations for one fully qualified path.
func (report Report) For(path string) []Violation {
	var result []Violation
	for _, violation := range report.violations {
		if violation.Path == path {
			result = append(result, violation)
		}
	}
	return result
}

// Messages converts a structured report to the established field-message map.
// Rule codes remain available through Violations.
func (report Report) Messages() Errors {
	result := make(Errors)
	for _, violation := range report.violations {
		result[violation.Path] = append(result[violation.Path], violation.Message)
	}
	return result
}

// Join combines reports in argument order.
func Join(reports ...Report) Report {
	var joined Report
	for _, report := range reports {
		joined.violations = append(joined.violations, report.violations...)
	}
	return joined
}

// Prefix returns a report whose paths are nested beneath prefix. Invalid
// prefixes become an explicit configuration violation instead of being
// silently accepted.
func (report Report) Prefix(prefix string) Report {
	if !validPath(prefix) {
		return configurationReport("validation report prefix must be a dot-separated field path")
	}
	result := Report{violations: make([]Violation, 0, len(report.violations))}
	for _, violation := range report.violations {
		path := prefix
		if violation.Path != "$" {
			path = joinPath(prefix, violation.Path)
		}
		result.violations = append(result.violations, Violation{
			Path: path, Code: violation.Code, Message: violation.Message,
		})
	}
	return result
}

func configurationReport(message string) Report {
	return Report{violations: []Violation{{
		Path: "$", Code: CodeInvalidConfiguration, Message: message,
	}}}
}

func configurationIssue(message string) Issue {
	return Issue{Code: CodeInvalidConfiguration, Message: message}
}

func validPath(path string) bool {
	if path == "" || path == "$" || len(path) > 512 || !utf8.ValidString(path) || strings.TrimSpace(path) != path {
		return false
	}
	for _, segment := range strings.Split(path, ".") {
		if segment == "" {
			return false
		}
		for _, character := range segment {
			if !unicode.IsLetter(character) && !unicode.IsDigit(character) && character != '_' && character != '-' {
				return false
			}
		}
	}
	return true
}

func validRelativePath(path string) bool { return path == "" || validPath(path) }

func validCode(code string) bool {
	if code == "" || len(code) > 128 || strings.TrimSpace(code) != code {
		return false
	}
	for index, character := range code {
		if character >= 'a' && character <= 'z' {
			continue
		}
		if index > 0 && ((character >= '0' && character <= '9') || character == '_' || character == '-' || character == '.') {
			continue
		}
		return false
	}
	return !strings.Contains(code, "..") && !strings.HasSuffix(code, ".")
}

func validMessage(message string) bool {
	if strings.TrimSpace(message) == "" || len(message) > 1024 || !utf8.ValidString(message) {
		return false
	}
	for _, character := range message {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}

func joinPath(prefix, suffix string) string {
	if prefix == "" {
		return suffix
	}
	if suffix == "" {
		return prefix
	}
	return prefix + "." + suffix
}
