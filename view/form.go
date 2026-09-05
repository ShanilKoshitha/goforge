package view

// Form is explicit render state for Forge form directives. OldValues records
// submitted fields, including submitted empty strings. ValidationErrors holds
// already-safe application messages which html/template still escapes.
type Form struct {
	CSRFToken        string
	OldValues        map[string]string
	ValidationErrors map[string][]string
}

// Old returns a submitted value when present, including an empty value. The
// optional fallback is used only when the field was not submitted.
func (form Form) Old(name string, fallback ...any) any {
	if value, exists := form.OldValues[name]; exists {
		return value
	}
	if len(fallback) > 0 {
		return fallback[0]
	}
	return ""
}

// Errors returns all validation messages for a field.
func (form Form) Errors(name string) []string {
	return form.ValidationErrors[name]
}
