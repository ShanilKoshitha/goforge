package view

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

type compiledMapping struct {
	source   string
	mappings []SourceMapping
	prefix   int
}

type includeEdge struct {
	target  string
	context []string
}

var templateLocation = regexp.MustCompile(`template: ([^:]+):(\d+)(?::(\d+))?`)

func mapCompiledError(operation string, item CompiledTemplate, prefix int, includes map[string][]includeEdge, err error) error {
	line, column, ok := errorLocation(err)
	if !ok {
		return fmt.Errorf("%s compiled view %q: %w", operation, item.Name, err)
	}
	if line == 1 {
		column -= prefix
		if column < 1 {
			column = 1
		}
	}
	offset := lineColumnOffset(item.Source, line, column)
	if mapping, exists := findMapping(item.Mappings, item.Source, offset); exists {
		context := includeCompileContext(item.Name, includes)
		for _, value := range mapping.Context {
			if value != "view "+item.Name {
				context = append(context, value)
			}
		}
		mapping.Context = deduplicateContext(context)
		return mappedError(operation, item.Name, mapping, err)
	}
	return fmt.Errorf("%s compiled view %q: %w", operation, item.Name, err)
}

func includeCompileContext(target string, includes map[string][]includeEdge) []string {
	names := make([]string, 0, len(includes))
	incoming := make(map[string]bool)
	for name, dependencies := range includes {
		names = append(names, name)
		for _, dependency := range dependencies {
			incoming[dependency.target] = true
		}
	}
	sort.Strings(names)
	var best []string
	for _, root := range names {
		if incoming[root] {
			continue
		}
		context := includeContext(root, target, includes)
		if len(context) > 1 || root == target {
			if len(context) > len(best) {
				best = context
			}
		}
	}
	if len(best) > 0 {
		return best
	}
	return []string{"view " + target}
}

func mapExecutionError(root string, mappings map[string]compiledMapping, includes map[string][]includeEdge, err error) error {
	match := templateLocation.FindStringSubmatch(err.Error())
	if len(match) == 0 {
		return fmt.Errorf("render view %q: %w", root, err)
	}
	name := match[1]
	line, _ := strconv.Atoi(match[2])
	column := 1
	if len(match) > 3 && match[3] != "" {
		column, _ = strconv.Atoi(match[3])
	}
	compiled, exists := mappings[name]
	if !exists {
		return fmt.Errorf("render view %q: %w", root, err)
	}
	if line == 1 {
		column -= compiled.prefix
		if column < 1 {
			column = 1
		}
	}
	offset := lineColumnOffset(compiled.source, line, column)
	mapping, exists := findMapping(compiled.mappings, compiled.source, offset)
	if !exists {
		return fmt.Errorf("render view %q: %w", root, err)
	}
	context := includeContext(root, name, includes)
	for _, value := range mapping.Context {
		if value != "view "+name {
			context = append(context, value)
		}
	}
	mapping.Context = deduplicateContext(context)
	return mappedError("render", root, mapping, err)
}

func errorLocation(err error) (int, int, bool) {
	match := templateLocation.FindStringSubmatch(err.Error())
	if len(match) == 0 {
		return 0, 0, false
	}
	line, lineErr := strconv.Atoi(match[2])
	column := 1
	if len(match) > 3 && match[3] != "" {
		column, _ = strconv.Atoi(match[3])
	}
	return line, column, lineErr == nil
}

func lineColumnOffset(source string, line, column int) int {
	if line < 1 {
		line = 1
	}
	if column < 1 {
		column = 1
	}
	offset := 0
	for current := 1; current < line && offset < len(source); current++ {
		next := strings.IndexByte(source[offset:], '\n')
		if next < 0 {
			return len(source)
		}
		offset += next + 1
	}
	offset += column - 1
	if offset > len(source) {
		return len(source)
	}
	return offset
}

func findMapping(mappings []SourceMapping, generated string, offset int) (SourceMapping, bool) {
	var previous SourceMapping
	foundPrevious := false
	for _, mapping := range mappings {
		if offset >= mapping.Start && offset < mapping.End {
			return adjustMapping(mapping, generated, offset-mapping.Start), true
		}
		if mapping.Start <= offset {
			previous, foundPrevious = mapping, true
		}
	}
	return previous, foundPrevious
}

func adjustMapping(mapping SourceMapping, generated string, delta int) SourceMapping {
	if mapping.Synthetic || delta <= 0 {
		return mapping
	}
	end := mapping.Start + delta
	if end > mapping.End {
		end = mapping.End
	}
	segment := generated[mapping.Start:end]
	for len(segment) > 0 {
		r, size := utf8.DecodeRuneInString(segment)
		segment = segment[size:]
		if r == '\n' {
			mapping.Line++
			mapping.Column = 1
		} else {
			mapping.Column++
		}
	}
	mapping.SourceStart += delta
	return mapping
}

func includeContext(root, target string, includes map[string][]includeEdge) []string {
	if root == target {
		return []string{"view " + root}
	}
	type pathItem struct {
		name  string
		chain []string
	}
	queue := []pathItem{{name: root, chain: []string{"view " + root}}}
	seen := map[string]bool{root: true}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, dependency := range includes[current.name] {
			chain := append([]string{}, current.chain...)
			for _, value := range dependency.context {
				if value != "view "+current.name {
					chain = append(chain, value)
				}
			}
			if len(chain) == 0 || chain[len(chain)-1] != "include "+dependency.target {
				chain = append(chain, "include "+dependency.target)
			}
			chain = deduplicateContext(chain)
			if dependency.target == target {
				return chain
			}
			if !seen[dependency.target] {
				seen[dependency.target] = true
				queue = append(queue, pathItem{name: dependency.target, chain: chain})
			}
		}
	}
	return []string{"view " + root, "template " + target}
}

func templateIncludeEdges(item CompiledTemplate) []includeEdge {
	matches := standardTemplateCall.FindAllStringSubmatchIndex(item.Source, -1)
	result := make([]includeEdge, 0, len(matches))
	for _, match := range matches {
		target := item.Source[match[2]:match[3]]
		context := []string{"view " + item.Name, "include " + target}
		if mapping, exists := findMapping(item.Mappings, item.Source, match[0]); exists && len(mapping.Context) > 0 {
			context = append([]string{}, mapping.Context...)
		}
		result = append(result, includeEdge{target: target, context: deduplicateContext(context)})
	}
	return result
}

func mappedError(operation, name string, mapping SourceMapping, err error) error {
	context := ""
	if len(mapping.Context) > 0 {
		context = " (" + strings.Join(mapping.Context, " -> ") + ")"
	}
	return fmt.Errorf("%s:%d:%d: %s view %q%s: %w", mapping.Path, mapping.Line, mapping.Column, operation, name, context, err)
}

func deduplicateContext(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if len(result) == 0 || result[len(result)-1] != value {
			result = append(result, value)
		}
	}
	return result
}
