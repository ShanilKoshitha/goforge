package orm

// FieldState distinguishes an omitted field from SQL NULL and a concrete value.
type FieldState uint8

const (
	FieldOmitted FieldState = iota
	FieldNull
	FieldValue
	FieldDefault
)

// Field represents one partial insert or update field. Its zero value is
// omitted, so generated change structs remain convenient and safe.
type Field[T any] struct {
	state FieldState
	value T
}

// Value constructs a present non-NULL field, including the zero value of T.
// Builders reject nil and typed-nil values; use Null for SQL NULL.
func Value[T any](value T) Field[T] { return Field[T]{state: FieldValue, value: value} }

// Null constructs an explicit SQL NULL field.
func Null[T any]() Field[T] { return Field[T]{state: FieldNull} }

// Default constructs an explicit SQL DEFAULT field for inserts or updates.
func Default[T any]() Field[T] { return Field[T]{state: FieldDefault} }

func (field Field[T]) State() FieldState { return field.state }

// Get returns the concrete value only when State is FieldValue.
func (field Field[T]) Get() (T, bool) { return field.value, field.state == FieldValue }
