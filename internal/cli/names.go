package cli

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

var nonIdentifier = regexp.MustCompile(`[^a-zA-Z0-9]+`)

func pluralize(value string) string {
	if strings.HasSuffix(value, "y") && len(value) > 1 && !strings.ContainsRune("aeiou", rune(value[len(value)-2])) {
		return strings.TrimSuffix(value, "y") + "ies"
	}
	for _, suffix := range []string{"s", "x", "z", "ch", "sh"} {
		if strings.HasSuffix(value, suffix) {
			return value + "es"
		}
	}
	return value + "s"
}

func words(value string) []string {
	value = nonIdentifier.ReplaceAllString(value, " ")
	var result []string
	for _, field := range strings.Fields(value) {
		runes := []rune(field)
		var current []rune
		for index, r := range runes {
			// Keep initialisms together, splitting HTTPRequest into HTTP and Request.
			boundary := index > 0 && unicode.IsUpper(r) &&
				(!unicode.IsUpper(runes[index-1]) || index+1 < len(runes) && unicode.IsLower(runes[index+1]))
			if boundary {
				result = append(result, strings.ToLower(string(current)))
				current = current[:0]
			}
			current = append(current, r)
		}
		if len(current) > 0 {
			result = append(result, strings.ToLower(string(current)))
		}
	}
	return result
}

func snake(value string) (string, error) {
	parts := words(value)
	if len(parts) == 0 {
		return "", fmt.Errorf("name %q contains no letters or numbers", value)
	}
	return strings.Join(parts, "_"), nil
}

func pascal(value string) (string, error) {
	parts := words(value)
	if len(parts) == 0 {
		return "", fmt.Errorf("name %q contains no letters or numbers", value)
	}
	for index, part := range parts {
		if commonInitialisms[strings.ToUpper(part)] {
			parts[index] = strings.ToUpper(part)
			continue
		}
		runes := []rune(part)
		runes[0] = unicode.ToUpper(runes[0])
		parts[index] = string(runes)
	}
	return strings.Join(parts, ""), nil
}

var commonInitialisms = map[string]bool{
	"API": true, "CPU": true, "CSS": true, "DNS": true, "EOF": true,
	"GUID": true, "HTML": true, "HTTP": true, "HTTPS": true, "ID": true,
	"IP": true, "JSON": true, "QPS": true, "RAM": true, "RPC": true,
	"SLA": true, "SMTP": true, "SQL": true, "SSH": true, "TCP": true,
	"TLS": true, "TTL": true, "UDP": true, "UI": true, "UID": true,
	"UUID": true, "URI": true, "URL": true, "UTF8": true, "VM": true,
	"XML": true, "XSRF": true, "XSS": true,
}
