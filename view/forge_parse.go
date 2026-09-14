package view

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

type nodeKind uint8

const (
	nodeText nodeKind = iota
	nodeAction
	nodeExtends
	nodeSection
	nodeYield
	nodeInclude
	nodeProps
	nodeComponent
	nodeSlot
	nodeIf
	nodeFor
	nodeWith
	nodePush
	nodeStack
	nodeCSRF
	nodeMethod
	nodeOld
	nodeErrors
	nodeAttributes
)

type sourceSpan struct {
	path         string
	start, end   int
	line, column int
}

type argument struct {
	name  string
	value string
	span  sourceSpan
}

type attributeSpec struct {
	name  string
	value string
	span  sourceSpan
}

type attributeBag struct {
	forward     bool
	forwardSpan sourceSpan
	entries     []attributeSpec
	span        sourceSpan
}

type branch struct {
	pipeline string
	span     sourceSpan
	nodes    []node
}

type node struct {
	kind       nodeKind
	span       sourceSpan
	text       string
	name       string
	args       []argument
	children   []node
	alternate  []node
	branches   []branch
	context    []string
	attributes *attributeBag
	form       string
	formSpan   sourceSpan
}

type propDefinition struct {
	name       string
	required   bool
	defaultVal string
	span       sourceSpan
}

type document struct {
	path          string
	name          string
	source        string
	nodes         []node
	extends       string
	sections      map[string][]node
	extra         []node
	component     bool
	props         []propDefinition
	attributeSink bool
}

type parser struct {
	path   string
	source string
	pos    int
}

func parseDocument(filePath, name, source string) (*document, error) {
	p := &parser{path: filePath, source: source}
	nodes, terminal, err := p.parseNodes(nil)
	if err != nil {
		return nil, err
	}
	if terminal != "" {
		return nil, p.errorAt(p.pos, "unexpected @%s", terminal)
	}
	doc := &document{
		path: filePath, name: name, source: source, nodes: nodes,
		sections: make(map[string][]node), component: strings.HasPrefix(name, "components/"),
	}
	if err := doc.organize(); err != nil {
		return nil, err
	}
	return doc, nil
}

func (p *parser) parseNodes(stops map[string]bool) ([]node, string, error) {
	var nodes []node
	textStart := p.pos
	flush := func(end int) {
		if end > textStart {
			nodes = append(nodes, node{kind: nodeText, span: p.span(textStart, end), text: p.source[textStart:end]})
		}
	}
	for p.pos < len(p.source) {
		if strings.HasPrefix(p.source[p.pos:], "{{--") {
			flush(p.pos)
			end := strings.Index(p.source[p.pos+4:], "--}}")
			if end < 0 {
				return nil, "", p.errorAt(p.pos, "view comment is not closed")
			}
			p.pos += 4 + end + 4
			textStart = p.pos
			continue
		}
		if strings.HasPrefix(p.source[p.pos:], "@@") {
			flush(p.pos)
			start := p.pos
			p.pos += 2
			nodes = append(nodes, node{kind: nodeText, span: p.span(start, p.pos), text: "@"})
			textStart = p.pos
			continue
		}
		if strings.HasPrefix(p.source[p.pos:], "{{") {
			flush(p.pos)
			item, err := p.parseAction()
			if err != nil {
				return nil, "", err
			}
			nodes = append(nodes, item)
			textStart = p.pos
			continue
		}
		if p.source[p.pos] != '@' {
			p.pos++
			continue
		}
		name, nameEnd := p.directiveName(p.pos + 1)
		if name == "" {
			p.pos++
			continue
		}
		if stops != nil && stops[name] {
			flush(p.pos)
			if name != "elseif" {
				index := nameEnd
				for index < len(p.source) && (p.source[index] == ' ' || p.source[index] == '\t') {
					index++
				}
				if index < len(p.source) && p.source[index] == '(' {
					return nil, "", p.errorAt(p.pos, "@%s does not accept arguments", name)
				}
			}
			p.pos = nameEnd
			return nodes, name, nil
		}
		if name == "attributes" && !p.directiveAcceptsArguments(nameEnd) {
			p.pos++
			continue
		}
		if !knownDirective(name) {
			p.pos++
			continue
		}
		flush(p.pos)
		item, err := p.parseDirective(name, nameEnd)
		if err != nil {
			return nil, "", err
		}
		nodes = append(nodes, item)
		textStart = p.pos
	}
	flush(p.pos)
	if len(stops) > 0 {
		names := make([]string, 0, len(stops))
		for name := range stops {
			names = append(names, "@"+name)
		}
		sort.Strings(names)
		return nil, "", p.errorAt(p.pos, "expected %s before end of file", strings.Join(names, " or "))
	}
	return nodes, "", nil
}

func (p *parser) directiveAcceptsArguments(nameEnd int) bool {
	index := nameEnd
	for index < len(p.source) && (p.source[index] == ' ' || p.source[index] == '\t') {
		index++
	}
	return index < len(p.source) && p.source[index] == '('
}

func (p *parser) parseAction() (node, error) {
	start := p.pos
	end, err := scanGoAction(p.source, start)
	if err != nil {
		return node{}, p.errorAt(start, "%v", err)
	}
	p.pos = end
	text := p.source[start:end]
	inner := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(text, "{{"), "}}"))
	inner = strings.TrimSpace(strings.TrimPrefix(strings.TrimSuffix(inner, "-"), "-"))
	word := inner
	if index := strings.IndexFunc(word, unicode.IsSpace); index >= 0 {
		word = word[:index]
	}
	if word == "define" || word == "template" || word == "block" {
		return node{}, p.errorAt(start, "raw %s actions are not supported; use @include or @component", word)
	}
	return node{kind: nodeAction, span: p.span(start, end), text: text}, nil
}

func scanGoAction(source string, start int) (int, error) {
	quote := byte(0)
	escaped := false
	for index := start + 2; index < len(source)-1; index++ {
		ch := source[index]
		if quote != 0 {
			if quote != '`' && escaped {
				escaped = false
				continue
			}
			if quote != '`' && ch == '\\' {
				escaped = true
				continue
			}
			if ch == quote {
				quote = 0
			}
			continue
		}
		if ch == '"' || ch == '\'' || ch == '`' {
			quote = ch
			continue
		}
		if ch == '}' && source[index+1] == '}' {
			return index + 2, nil
		}
	}
	return 0, fmt.Errorf("Go template action is not closed")
}

func (p *parser) parseDirective(name string, nameEnd int) (node, error) {
	start := p.pos
	p.pos = nameEnd
	leaf := func(kind nodeKind, requireArgs bool) (node, error) {
		raw, end, err := p.arguments(requireArgs)
		if err != nil {
			return node{}, err
		}
		p.pos = end
		return node{kind: kind, span: p.span(start, end), text: raw}, nil
	}
	switch name {
	case "extends", "yield", "include", "props", "stack", "method", "old", "attributes":
		kinds := map[string]nodeKind{"extends": nodeExtends, "yield": nodeYield, "include": nodeInclude, "props": nodeProps, "stack": nodeStack, "method": nodeMethod, "old": nodeOld, "attributes": nodeAttributes}
		item, err := leaf(kinds[name], true)
		if err != nil {
			return node{}, err
		}
		return p.parseLeafArguments(item)
	case "csrf":
		index := p.pos
		for index < len(p.source) && (p.source[index] == ' ' || p.source[index] == '\t') {
			index++
		}
		if index < len(p.source) && p.source[index] == '(' {
			item, err := leaf(nodeCSRF, true)
			if err != nil {
				return node{}, err
			}
			return p.parseLeafArguments(item)
		}
		return node{kind: nodeCSRF, span: p.span(start, p.pos)}, nil
	case "section", "component", "slot", "push", "errors":
		kinds := map[string]nodeKind{"section": nodeSection, "component": nodeComponent, "slot": nodeSlot, "push": nodePush, "errors": nodeErrors}
		ends := map[string]string{"section": "endsection", "component": "endcomponent", "slot": "endslot", "push": "endpush", "errors": "enderrors"}
		item, err := leaf(kinds[name], true)
		if err != nil {
			return node{}, err
		}
		item, err = p.parseLeafArguments(item)
		if err != nil {
			return node{}, err
		}
		children, terminal, err := p.parseNodes(map[string]bool{ends[name]: true})
		if err != nil {
			return node{}, err
		}
		if terminal != ends[name] {
			return node{}, p.errorAt(start, "@%s is not closed", name)
		}
		item.children = children
		item.span.end = p.pos
		return item, nil
	case "if":
		return p.parseConditional(start, nodeIf, "endif", true)
	case "for":
		return p.parseConditional(start, nodeFor, "endfor", false)
	case "with":
		return p.parseConditional(start, nodeWith, "endwith", false)
	default:
		return node{}, p.errorAt(start, "unexpected @%s", name)
	}
}

func (p *parser) parseConditional(start int, kind nodeKind, endName string, elseif bool) (node, error) {
	raw, end, err := p.arguments(true)
	if err != nil {
		return node{}, err
	}
	p.pos = end
	if strings.TrimSpace(raw) == "" {
		return node{}, p.errorAt(start, "control directive requires a Go-template pipeline")
	}
	item := node{kind: kind, span: p.span(start, end), text: strings.TrimSpace(raw)}
	stops := map[string]bool{endName: true, "else": true}
	if kind == nodeFor {
		stops["empty"] = true
	}
	if elseif {
		stops["elseif"] = true
	}
	children, terminal, err := p.parseNodes(stops)
	if err != nil {
		return node{}, err
	}
	item.children = children
	for terminal == "elseif" {
		branchStart := p.pos - len("elseif") - 1
		raw, next, err := p.arguments(true)
		if err != nil {
			return node{}, err
		}
		p.pos = next
		if strings.TrimSpace(raw) == "" {
			return node{}, p.errorAt(branchStart, "@elseif requires a Go-template pipeline")
		}
		body, nextTerminal, err := p.parseNodes(stops)
		if err != nil {
			return node{}, err
		}
		item.branches = append(item.branches, branch{pipeline: strings.TrimSpace(raw), span: p.span(branchStart, next), nodes: body})
		terminal = nextTerminal
	}
	if terminal == "else" || terminal == "empty" {
		body, nextTerminal, err := p.parseNodes(map[string]bool{endName: true})
		if err != nil {
			return node{}, err
		}
		item.alternate = body
		terminal = nextTerminal
	}
	if terminal != endName {
		return node{}, p.errorAt(start, "control directive is not closed with @%s", endName)
	}
	item.span.end = p.pos
	return item, nil
}

func (p *parser) arguments(required bool) (string, int, error) {
	index := p.pos
	for index < len(p.source) && (p.source[index] == ' ' || p.source[index] == '\t') {
		index++
	}
	if index >= len(p.source) || p.source[index] != '(' {
		if required {
			return "", 0, p.errorAt(index, "directive arguments must be enclosed in parentheses")
		}
		return "", p.pos, nil
	}
	end, err := scanBalanced(p.source, index)
	if err != nil {
		return "", 0, p.errorAt(index, "%v", err)
	}
	return p.source[index+1 : end-1], end, nil
}

func scanBalanced(source string, start int) (int, error) {
	depth := 0
	quote := byte(0)
	escaped := false
	for index := start; index < len(source); index++ {
		ch := source[index]
		if quote != 0 {
			if quote != '`' && escaped {
				escaped = false
				continue
			}
			if quote != '`' && ch == '\\' {
				escaped = true
				continue
			}
			if ch == quote {
				quote = 0
			}
			continue
		}
		if ch == '"' || ch == '\'' || ch == '`' {
			quote = ch
			continue
		}
		switch ch {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return index + 1, nil
			}
		}
	}
	return 0, fmt.Errorf("directive arguments are not closed")
}

func (p *parser) parseLeafArguments(item node) (node, error) {
	parts, err := splitArguments(item.text)
	if err != nil {
		return node{}, p.errorAt(item.span.start, "%v", err)
	}
	quoted := func(index int, label string) (string, error) {
		if index >= len(parts) {
			return "", p.errorAt(item.span.start, "%s requires a quoted %s", directiveLabel(item.kind), label)
		}
		value, err := strconv.Unquote(strings.TrimSpace(parts[index]))
		if err != nil {
			return "", p.errorAt(item.span.start, "%s must be a quoted string", label)
		}
		return value, nil
	}
	switch item.kind {
	case nodeCSRF:
		remaining, form, formSpan, formErr := p.parseFormArgument(item, parts)
		if formErr != nil {
			return node{}, formErr
		}
		if len(remaining) != 0 || form == "" {
			return node{}, p.errorAt(item.span.start, "@csrf accepts only a final form= argument")
		}
		item.form, item.formSpan = form, formSpan
	case nodeExtends, nodeYield, nodeStack, nodePush, nodeSection:
		if len(parts) != 1 {
			return node{}, p.errorAt(item.span.start, "%s requires one quoted name", directiveLabel(item.kind))
		}
		item.name, err = quoted(0, "name")
	case nodeMethod:
		if len(parts) != 1 {
			return node{}, p.errorAt(item.span.start, "@method requires one quoted method")
		}
		item.name, err = quoted(0, "method")
		item.name = strings.ToUpper(strings.TrimSpace(item.name))
		if err == nil && item.name != "PUT" && item.name != "PATCH" && item.name != "DELETE" {
			return node{}, p.errorAt(item.span.start, "@method supports PUT, PATCH, or DELETE")
		}
	case nodeOld:
		remaining, form, formSpan, formErr := p.parseFormArgument(item, parts)
		if formErr != nil {
			return node{}, formErr
		}
		if len(remaining) < 1 || len(remaining) > 2 {
			return node{}, p.errorAt(item.span.start, "@old requires a quoted field, optional fallback pipeline, and optional final form=<pipeline>")
		}
		parts = remaining
		item.name, err = quoted(0, "field")
		item.text = ""
		if len(parts) == 2 {
			item.text = strings.TrimSpace(parts[1])
		}
		item.form, item.formSpan = form, formSpan
	case nodeErrors:
		remaining, form, formSpan, formErr := p.parseFormArgument(item, parts)
		if formErr != nil {
			return node{}, formErr
		}
		if len(remaining) != 1 {
			return node{}, p.errorAt(item.span.start, "@errors requires a quoted field and optional final form=<pipeline>")
		}
		parts = remaining
		item.name, err = quoted(0, "field")
		item.form, item.formSpan = form, formSpan
	case nodeInclude:
		if len(parts) < 1 || len(parts) > 2 {
			return node{}, p.errorAt(item.span.start, "@include requires a static name and optional data pipeline")
		}
		item.name, err = quoted(0, "include name")
		item.text = "."
		if len(parts) == 2 {
			item.text = strings.TrimSpace(parts[1])
		}
		if item.text == "" || (item.text[0] != '.' && item.text[0] != '$') {
			return node{}, p.errorAt(item.span.start, "include data must be an explicit dot or variable pipeline")
		}
	case nodeComponent:
		if len(parts) < 1 {
			return node{}, p.errorAt(item.span.start, "@component requires a static component name")
		}
		item.name, err = quoted(0, "component name")
		seen := make(map[string]struct{})
		for index, raw := range parts[1:] {
			if attributeGroup(raw) {
				if index != len(parts[1:])-1 {
					return node{}, p.errorAt(item.span.start, "component attributes(...) must be the final argument")
				}
				bag, bagErr := p.parseComponentAttributeBag(item, raw)
				if bagErr != nil {
					return node{}, bagErr
				}
				item.attributes = bag
				continue
			}
			name, value, ok := splitNamedArgument(raw)
			if !ok || !validIdentifier(name) || strings.TrimSpace(value) == "" {
				return node{}, p.errorAt(item.span.start, "component props must use name=value")
			}
			if _, exists := seen[name]; exists {
				return node{}, p.errorAt(item.span.start, "duplicate component prop %q", name)
			}
			seen[name] = struct{}{}
			item.args = append(item.args, argument{name: name, value: strings.TrimSpace(value), span: item.span})
		}
	case nodeSlot:
		if len(parts) != 1 {
			return node{}, p.errorAt(item.span.start, "@slot requires one quoted name")
		}
		item.name, err = quoted(0, "slot name")
	case nodeProps:
		for _, raw := range parts {
			name, value, assigned := splitNamedArgument(raw)
			if !assigned {
				name = strings.TrimSpace(raw)
			}
			if !validIdentifier(name) {
				return node{}, p.errorAt(item.span.start, "invalid prop name %q", name)
			}
			item.args = append(item.args, argument{name: name, value: strings.TrimSpace(value), span: item.span})
		}
	case nodeAttributes:
		base := directiveArgumentStart(p.source, item)
		bag, bagErr := p.parseAttributeBag(item.text, base, false, item.span)
		if bagErr != nil {
			return node{}, bagErr
		}
		item.attributes = bag
	}
	if err != nil {
		return node{}, err
	}
	return item, nil
}

func (p *parser) parseFormArgument(item node, parts []string) ([]string, string, sourceSpan, error) {
	remaining := append([]string{}, parts...)
	var form string
	var formSpan sourceSpan
	formCount := 0
	for _, raw := range parts {
		name, _, assigned := splitNamedArgument(raw)
		if assigned && name == "form" {
			formCount++
		}
	}
	if formCount > 1 {
		return nil, "", sourceSpan{}, p.errorAt(item.span.start, "form= may appear only once")
	}
	for index, raw := range parts {
		name, value, assigned := splitNamedArgument(raw)
		if !assigned || name != "form" {
			continue
		}
		if index != len(parts)-1 {
			return nil, "", sourceSpan{}, p.errorAt(item.span.start, "form= must be the final argument")
		}
		value = strings.TrimSpace(value)
		if value == "" {
			return nil, "", sourceSpan{}, p.errorAt(item.span.start, "form expression is required")
		}
		form = value
		formSpan = item.span
		remaining = remaining[:len(remaining)-1]
	}
	return remaining, form, formSpan, nil
}

func attributeGroup(raw string) bool {
	trimmed := strings.TrimSpace(raw)
	if !strings.HasPrefix(trimmed, "attributes") {
		return false
	}
	rest := strings.TrimSpace(strings.TrimPrefix(trimmed, "attributes"))
	return strings.HasPrefix(rest, "(")
}

func (p *parser) parseComponentAttributeBag(item node, raw string) (*attributeBag, error) {
	trimmed := strings.TrimSpace(raw)
	rest := strings.TrimSpace(strings.TrimPrefix(trimmed, "attributes"))
	if rest == "" || rest[0] != '(' {
		return nil, p.errorAt(item.span.start, "component attributes must use attributes(...)")
	}
	end, err := scanBalanced(rest, 0)
	if err != nil || end != len(rest) {
		return nil, p.errorAt(item.span.start, "component attributes(...) are malformed")
	}
	base := directiveArgumentStart(p.source, item)
	partOffset := strings.LastIndex(item.text, raw)
	if partOffset < 0 {
		partOffset = 0
	}
	openOffset := strings.Index(raw, "(")
	if openOffset < 0 {
		return nil, p.errorAt(item.span.start, "component attributes must use attributes(...)")
	}
	span := p.span(base+partOffset, base+partOffset+len(raw))
	return p.parseAttributeBag(rest[1:len(rest)-1], base+partOffset+openOffset+1, true, span)
}

func (p *parser) parseAttributeBag(raw string, base int, allowForward bool, span sourceSpan) (*attributeBag, error) {
	parts, err := splitArgumentSpans(raw, base, p)
	if err != nil {
		return nil, err
	}
	bag := &attributeBag{span: span}
	seen := make(map[string]sourceSpan)
	for index, part := range parts {
		if part.text == "..." {
			if !allowForward {
				return nil, p.errorAt(part.span.start, "@attributes cannot forward")
			}
			if bag.forward {
				return nil, p.errorAt(part.span.start, "attribute forwarding spread may appear only once")
			}
			if index != 0 {
				return nil, p.errorAt(part.span.start, "attribute forwarding spread must be first")
			}
			bag.forward = true
			bag.forwardSpan = part.span
			continue
		}
		if strings.HasSuffix(part.text, "...") {
			return nil, p.errorAt(part.span.start, "attribute map spreads are not supported")
		}
		name, value, assigned := splitNamedArgument(part.text)
		name = strings.TrimSpace(name)
		value = strings.TrimSpace(value)
		if !assigned || value == "" {
			return nil, p.errorAt(part.span.start, "attributes must use static-name=<pipeline>")
		}
		if !validAttributeName(name) {
			return nil, p.errorAt(part.span.start, "%q is not a static lowercase HTML attribute name", name)
		}
		if unsafeBagAttribute(name) {
			return nil, p.errorAt(part.span.start, "unsafe attribute %q requires explicit trusted HTML source", name)
		}
		if first, exists := seen[name]; exists && name != "class" {
			return nil, p.errorAt(part.span.start, "duplicate non-class attribute %q; first declared at %s:%d:%d", name, first.path, first.line, first.column)
		}
		seen[name] = part.span
		bag.entries = append(bag.entries, attributeSpec{name: name, value: value, span: part.span})
	}
	return bag, nil
}

type spannedArgument struct {
	text string
	span sourceSpan
}

func splitArgumentSpans(raw string, base int, p *parser) ([]spannedArgument, error) {
	parts, err := splitArguments(raw)
	if err != nil {
		return nil, p.errorAt(base, "%v", err)
	}
	result := make([]spannedArgument, 0, len(parts))
	search := 0
	for _, part := range parts {
		index := strings.Index(raw[search:], part)
		if index < 0 {
			index = 0
		}
		start := search + index
		result = append(result, spannedArgument{text: part, span: p.span(base+start, base+start+len(part))})
		search = start + len(part)
	}
	return result, nil
}

func directiveArgumentStart(source string, item node) int {
	if item.span.start < 0 || item.span.start >= len(source) {
		return item.span.start
	}
	end := item.span.end
	if end > len(source) {
		end = len(source)
	}
	open := strings.Index(source[item.span.start:end], "(")
	if open < 0 {
		return item.span.start
	}
	return item.span.start + open + 1
}

func validAttributeName(value string) bool {
	if value == "" || value[0] < 'a' || value[0] > 'z' {
		return false
	}
	previousHyphen := false
	for _, ch := range value {
		if ch == '-' {
			if previousHyphen {
				return false
			}
			previousHyphen = true
			continue
		}
		if !((ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9')) {
			return false
		}
		previousHyphen = false
	}
	return !previousHyphen
}

func unsafeBagAttribute(name string) bool {
	return strings.HasPrefix(name, "on") || name == "style" || name == "srcdoc"
}

func splitArguments(raw string) ([]string, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var result []string
	start, depth := 0, 0
	quote := byte(0)
	escaped := false
	for index := 0; index < len(raw); index++ {
		ch := raw[index]
		if quote != 0 {
			if quote != '`' && escaped {
				escaped = false
				continue
			}
			if quote != '`' && ch == '\\' {
				escaped = true
				continue
			}
			if ch == quote {
				quote = 0
			}
			continue
		}
		if ch == '"' || ch == '\'' || ch == '`' {
			quote = ch
			continue
		}
		switch ch {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
			if depth < 0 {
				return nil, fmt.Errorf("unbalanced directive argument")
			}
		case ',':
			if depth == 0 {
				result = append(result, strings.TrimSpace(raw[start:index]))
				start = index + 1
			}
		}
	}
	if quote != 0 || depth != 0 {
		return nil, fmt.Errorf("unbalanced directive argument")
	}
	result = append(result, strings.TrimSpace(raw[start:]))
	for _, item := range result {
		if item == "" {
			return nil, fmt.Errorf("empty directive argument")
		}
	}
	return result, nil
}

func splitNamedArgument(raw string) (string, string, bool) {
	quote := byte(0)
	escaped := false
	depth := 0
	for index := 0; index < len(raw); index++ {
		ch := raw[index]
		if quote != 0 {
			if quote != '`' && escaped {
				escaped = false
				continue
			}
			if quote != '`' && ch == '\\' {
				escaped = true
				continue
			}
			if ch == quote {
				quote = 0
			}
			continue
		}
		if ch == '"' || ch == '\'' || ch == '`' {
			quote = ch
			continue
		}
		switch ch {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case '=':
			if depth == 0 {
				return strings.TrimSpace(raw[:index]), strings.TrimSpace(raw[index+1:]), true
			}
		}
	}
	return "", "", false
}

func (doc *document) organize() error {
	nonWhitespace := func(item node) bool { return item.kind != nodeText || strings.TrimSpace(item.text) != "" }
	first := -1
	for index, item := range doc.nodes {
		if nonWhitespace(item) {
			first = index
			break
		}
	}
	if doc.component {
		if first < 0 || doc.nodes[first].kind != nodeProps {
			return sourceError(doc.path, doc.source, 0, "component %q must begin with @props", doc.name)
		}
		seen := make(map[string]struct{})
		for _, arg := range doc.nodes[first].args {
			if _, exists := seen[arg.name]; exists {
				return sourceError(doc.path, doc.source, arg.span.start, "duplicate prop %q", arg.name)
			}
			seen[arg.name] = struct{}{}
			definition := propDefinition{name: arg.name, required: arg.value == "", span: arg.span}
			if arg.value != "" {
				value, err := strconv.Unquote(arg.value)
				if err != nil {
					return sourceError(doc.path, doc.source, arg.span.start, "default for prop %q must be a quoted scalar literal", arg.name)
				}
				definition.defaultVal = value
			}
			doc.props = append(doc.props, definition)
		}
		doc.nodes = append(append([]node{}, doc.nodes[:first]...), doc.nodes[first+1:]...)
		if err := validateStaticScopes(doc, doc.nodes, 0, 0); err != nil {
			return err
		}
		return validateAttributeSinks(doc)
	}
	if first >= 0 && doc.nodes[first].kind == nodeExtends {
		doc.extends = normalizeReference(doc.nodes[first].name)
	}
	for index, item := range doc.nodes {
		if item.kind == nodeExtends && index != first {
			return sourceError(doc.path, doc.source, item.span.start, "@extends must be the first directive")
		}
		if item.kind == nodeProps {
			return sourceError(doc.path, doc.source, item.span.start, "@props is only valid at the start of a component")
		}
		if item.kind == nodeSection {
			if doc.extends == "" {
				return sourceError(doc.path, doc.source, item.span.start, "@section requires @extends")
			}
			if _, exists := doc.sections[item.name]; exists {
				return sourceError(doc.path, doc.source, item.span.start, "duplicate section %q", item.name)
			}
			if nested := findNode(item.children, nodeSection); nested != nil {
				return sourceError(doc.path, doc.source, nested.span.start, "nested sections are not supported")
			}
			doc.sections[item.name] = item.children
			continue
		}
		if doc.extends != "" && item.kind != nodeExtends && item.kind != nodeSection && item.kind != nodePush && nonWhitespace(item) {
			return sourceError(doc.path, doc.source, item.span.start, "templates using @extends may only contain @section and @push blocks")
		}
		if item.kind != nodeExtends && item.kind != nodeSection {
			doc.extra = append(doc.extra, item)
		}
	}
	if err := validateStaticScopes(doc, doc.nodes, 0, 0); err != nil {
		return err
	}
	return validateAttributeSinks(doc)
}

func findNode(nodes []node, kind nodeKind) *node {
	for _, item := range nodes {
		if item.kind == kind {
			found := item
			return &found
		}
		if found := findNode(item.children, kind); found != nil {
			return found
		}
		for _, branch := range item.branches {
			if found := findNode(branch.nodes, kind); found != nil {
				return found
			}
		}
		if found := findNode(item.alternate, kind); found != nil {
			return found
		}
	}
	return nil
}

func validateStaticScopes(doc *document, nodes []node, runtimeDepth, componentDepth int) error {
	for _, item := range nodes {
		childComponentDepth := componentDepth
		switch item.kind {
		case nodeIf, nodeFor, nodeWith:
			if err := validateStaticScopes(doc, item.children, runtimeDepth+1, componentDepth); err != nil {
				return err
			}
			for _, branch := range item.branches {
				if err := validateStaticScopes(doc, branch.nodes, runtimeDepth+1, componentDepth); err != nil {
					return err
				}
			}
			if err := validateStaticScopes(doc, item.alternate, runtimeDepth+1, componentDepth); err != nil {
				return err
			}
			continue
		case nodeComponent:
			childComponentDepth++
		case nodePush:
			if runtimeDepth > 0 || componentDepth > 0 || doc.component {
				return sourceError(doc.path, doc.source, item.span.start, "@push cannot capture runtime or component scope")
			}
		}
		if err := validateStaticScopes(doc, item.children, runtimeDepth, childComponentDepth); err != nil {
			return err
		}
	}
	return nil
}

func validateAttributeSinks(doc *document) error {
	var sink *node
	var walk func([]node, string) error
	walk = func(nodes []node, restriction string) error {
		for index := range nodes {
			item := &nodes[index]
			consumes := item.kind == nodeAttributes || item.kind == nodeComponent && item.attributes != nil && item.attributes.forward
			if consumes {
				if !doc.component {
					return sourceError(doc.path, doc.source, item.span.start, "attribute forwarding requires an enclosing component")
				}
				if restriction == "slot" {
					return sourceError(doc.path, doc.source, item.span.start, "component %s: attribute sink cannot appear in slot fallback content", doc.name)
				}
				if restriction != "" {
					return sourceError(doc.path, doc.source, item.span.start, "component %s: attribute sink must be unconditional", doc.name)
				}
				if sink != nil {
					return sourceError(doc.path, doc.source, item.span.start, "component has more than one attribute sink; first sink is at %s:%d:%d", sink.span.path, sink.span.line, sink.span.column)
				}
				sink = item
			}

			childRestriction := restriction
			switch item.kind {
			case nodeSlot:
				childRestriction = "slot"
			case nodeIf, nodeFor, nodeWith, nodeErrors, nodeComponent:
				if childRestriction == "" {
					childRestriction = "conditional"
				}
			}
			if err := walk(item.children, childRestriction); err != nil {
				return err
			}
			for _, itemBranch := range item.branches {
				if err := walk(itemBranch.nodes, "conditional"); err != nil {
					return err
				}
			}
			if err := walk(item.alternate, childRestriction); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(doc.nodes, ""); err != nil {
		return err
	}
	doc.attributeSink = sink != nil
	return nil
}

func knownDirective(name string) bool {
	switch name {
	case "extends", "section", "endsection", "yield", "include", "props", "component", "endcomponent", "slot", "endslot", "if", "elseif", "else", "endif", "for", "empty", "endfor", "with", "endwith", "push", "endpush", "stack", "csrf", "method", "old", "errors", "enderrors", "attributes":
		return true
	default:
		return false
	}
}

func directiveLabel(kind nodeKind) string {
	labels := map[nodeKind]string{nodeExtends: "@extends", nodeYield: "@yield", nodeStack: "@stack", nodePush: "@push", nodeSection: "@section", nodeErrors: "@errors"}
	return labels[kind]
}

func validIdentifier(value string) bool {
	if value == "" || !(value[0] == '_' || value[0] >= 'A' && value[0] <= 'Z' || value[0] >= 'a' && value[0] <= 'z') {
		return false
	}
	for index := 1; index < len(value); index++ {
		ch := value[index]
		if ch != '_' && !(ch >= 'A' && ch <= 'Z') && !(ch >= 'a' && ch <= 'z') && !(ch >= '0' && ch <= '9') {
			return false
		}
	}
	return true
}

func (p *parser) directiveName(start int) (string, int) {
	end := start
	for end < len(p.source) && (p.source[end] >= 'a' && p.source[end] <= 'z' || p.source[end] >= 'A' && p.source[end] <= 'Z') {
		end++
	}
	return p.source[start:end], end
}

func (p *parser) span(start, end int) sourceSpan {
	line, column := sourcePosition(p.source, start)
	return sourceSpan{path: p.path, start: start, end: end, line: line, column: column}
}

func (p *parser) errorAt(offset int, format string, args ...any) error {
	return sourceError(p.path, p.source, offset, format, args...)
}

func sourcePosition(source string, offset int) (int, int) {
	if offset < 0 {
		offset = 0
	}
	if offset > len(source) {
		offset = len(source)
	}
	line, column := 1, 1
	for index := 0; index < offset; {
		r, size := utf8.DecodeRuneInString(source[index:])
		if index+size > offset {
			break
		}
		index += size
		if r == '\n' {
			line, column = line+1, 1
		} else {
			column++
		}
	}
	return line, column
}

func sourceError(path, source string, offset int, format string, args ...any) error {
	if offset < 0 {
		offset = 0
	}
	if offset > len(source) {
		offset = len(source)
	}
	line, column := sourcePosition(source, offset)
	lineStart := strings.LastIndex(source[:offset], "\n") + 1
	lineEnd := strings.Index(source[offset:], "\n")
	if lineEnd < 0 {
		lineEnd = len(source)
	} else {
		lineEnd += offset
	}
	excerpt := source[lineStart:lineEnd]
	return fmt.Errorf("%s:%d:%d: %s\n%s\n%s^", path, line, column, fmt.Sprintf(format, args...), excerpt, strings.Repeat(" ", column-1))
}
