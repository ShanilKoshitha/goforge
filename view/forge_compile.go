package view

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

type forgeCompiler struct {
	documents map[string]*document
	sequence  int
}

type compileEnvironment struct {
	bindings       map[string]string
	slots          map[string]slotValue
	componentStack []string
}

type slotValue struct {
	nodes          []node
	bindings       map[string]string
	componentStack []string
	span           sourceSpan
}

type sourceBuilder struct {
	text     strings.Builder
	mappings []SourceMapping
}

func (compiler *forgeCompiler) validateDependencies() error {
	incoming := make(map[string]bool)
	for _, doc := range compiler.documents {
		if doc.extends != "" {
			incoming[doc.extends] = true
		}
		walkNodes(doc.nodes, func(item node) {
			if item.kind == nodeInclude || item.kind == nodeComponent {
				incoming[normalizeReference(item.name)] = true
			}
		})
	}
	names := sortedDocumentNames(compiler.documents)
	validated := make(map[string]bool)
	for _, name := range names {
		if compiler.documents[name].component || incoming[name] {
			continue
		}
		if err := compiler.validateDocument(name, []string{"view " + name}, nil, make(map[string]bool), validated); err != nil {
			return err
		}
	}
	for _, name := range names {
		if validated[name] {
			continue
		}
		prefix := "view "
		if compiler.documents[name].component {
			prefix = "component "
		}
		if err := compiler.validateDocument(name, []string{prefix + name}, nil, make(map[string]bool), validated); err != nil {
			return err
		}
	}
	return nil
}

func (compiler *forgeCompiler) validateDocument(name string, context []string, edge *node, active, validated map[string]bool) error {
	doc, exists := compiler.documents[name]
	if !exists {
		if edge == nil {
			return fmt.Errorf("unknown view %q", name)
		}
		label := "view"
		switch edge.kind {
		case nodeExtends:
			label = "layout"
		case nodeInclude:
			label = "include"
		case nodeComponent:
			label = "component"
		}
		return compiler.nodeError(*edge, context[:len(context)-1], "unknown %s %q", label, name)
	}
	if active[name] {
		if edge == nil {
			return fmt.Errorf("view %q: dependency cycle", name)
		}
		kind := "template include cycle"
		if edge.kind == nodeExtends {
			kind = "template inheritance cycle"
		} else if edge.kind == nodeComponent {
			kind = "component cycle"
		}
		return compiler.nodeError(*edge, context, "%s: %s", kind, strings.Join(context, " -> "))
	}
	if validated[name] {
		return nil
	}
	active[name] = true
	defer delete(active, name)
	if doc.extends != "" {
		var extendsNode *node
		for index := range doc.nodes {
			if doc.nodes[index].kind == nodeExtends {
				extendsNode = &doc.nodes[index]
				break
			}
		}
		if extendsNode != nil {
			if err := compiler.validateDocument(doc.extends, append(context, "layout "+doc.extends), extendsNode, active, validated); err != nil {
				return err
			}
		}
	}
	var dependencyErr error
	walkNodes(doc.nodes, func(item node) {
		if dependencyErr != nil || item.kind != nodeInclude && item.kind != nodeComponent {
			return
		}
		dependency := normalizeReference(item.name)
		label := "include " + dependency
		if item.kind == nodeComponent {
			label = "component " + dependency
		}
		dependencyErr = compiler.validateDocument(dependency, append(context, label), &item, active, validated)
	})
	if dependencyErr != nil {
		return dependencyErr
	}
	validated[name] = true
	return nil
}

func walkNodes(nodes []node, visit func(node)) {
	for _, item := range nodes {
		visit(item)
		walkNodes(item.children, visit)
		for _, branch := range item.branches {
			walkNodes(branch.nodes, visit)
		}
		walkNodes(item.alternate, visit)
	}
}

func (compiler *forgeCompiler) compile(name string) (CompiledTemplate, error) {
	allowOpen := compiler.documents[name].extends == "" || compiler.isExtended(name)
	nodes, err := compiler.resolve(name, nil, nil, allowOpen, name, nil)
	if err != nil {
		return CompiledTemplate{}, err
	}
	stacks := make(map[string][][]node)
	collectStacks(nodes, stacks)
	builder := new(sourceBuilder)
	environment := compileEnvironment{bindings: make(map[string]string)}
	if err := compiler.compileNodes(builder, nodes, environment, stacks, []string{"view " + name}); err != nil {
		return CompiledTemplate{}, err
	}
	return CompiledTemplate{Name: name, Source: builder.text.String(), Mappings: builder.mappings}, nil
}

func (compiler *forgeCompiler) resolve(name string, inherited map[string][]node, stack []string, allowOpen bool, root string, layoutContext []string) ([]node, error) {
	doc, exists := compiler.documents[name]
	if !exists {
		return nil, fmt.Errorf("unknown view %q", name)
	}
	for _, ancestor := range stack {
		if ancestor == name {
			return nil, sourceError(doc.path, doc.source, 0, "template inheritance cycle: %s", strings.Join(append(stack, name), " -> "))
		}
	}
	sections := make(map[string][]node, len(doc.sections)+len(inherited))
	for section, body := range doc.sections {
		sections[section] = body
	}
	for section, body := range inherited {
		sections[section] = body
	}
	if doc.extends != "" {
		parent, exists := compiler.documents[doc.extends]
		if !exists {
			return nil, sourceError(doc.path, doc.source, 0, "unknown layout %q", doc.extends)
		}
		if parent.component {
			return nil, sourceError(doc.path, doc.source, 0, "component %q cannot be used as a layout", doc.extends)
		}
		resolved, err := compiler.resolve(doc.extends, sections, append(stack, name), allowOpen, root, append(layoutContext, "layout "+doc.extends))
		if err != nil {
			return nil, err
		}
		var pushes []node
		for _, item := range doc.extra {
			if item.kind == nodePush {
				pushes = append(pushes, item)
			}
		}
		return append(pushes, resolved...), nil
	}
	base := annotateNodes(doc.extra, layoutContext)
	return compiler.substituteYields(base, sections, allowOpen, compiler.documents[root], make(map[string]bool))
}

func (compiler *forgeCompiler) substituteYields(nodes []node, sections map[string][]node, allowOpen bool, root *document, resolving map[string]bool) ([]node, error) {
	var result []node
	for _, item := range nodes {
		if item.kind == nodeYield {
			if body, exists := sections[item.name]; exists {
				if resolving[item.name] {
					doc := compiler.documents[templateName(item.span.path)]
					return nil, sourceError(item.span.path, doc.source, item.span.start, "section yield cycle through %q while compiling %q", item.name, root.name)
				}
				resolving[item.name] = true
				sectionBody := annotateNodes(body, append(item.context, "section "+item.name))
				expanded, err := compiler.substituteYields(sectionBody, sections, allowOpen, root, resolving)
				delete(resolving, item.name)
				if err != nil {
					return nil, err
				}
				result = append(result, expanded...)
			} else if !allowOpen {
				// Leave the yield for compileNodes to report with both locations.
				result = append(result, item)
			}
			continue
		}
		var err error
		item.children, err = compiler.substituteYields(item.children, sections, allowOpen, root, resolving)
		if err != nil {
			return nil, err
		}
		item.alternate, err = compiler.substituteYields(item.alternate, sections, allowOpen, root, resolving)
		if err != nil {
			return nil, err
		}
		for index := range item.branches {
			item.branches[index].nodes, err = compiler.substituteYields(item.branches[index].nodes, sections, allowOpen, root, resolving)
			if err != nil {
				return nil, err
			}
		}
		result = append(result, item)
	}
	return result, nil
}

func annotateNodes(nodes []node, context []string) []node {
	result := make([]node, len(nodes))
	for index, item := range nodes {
		item.context = mergeContext(item.context, context)
		item.children = annotateNodes(item.children, context)
		item.alternate = annotateNodes(item.alternate, context)
		for branchIndex := range item.branches {
			item.branches[branchIndex].nodes = annotateNodes(item.branches[branchIndex].nodes, context)
		}
		result[index] = item
	}
	return result
}

func (compiler *forgeCompiler) isExtended(name string) bool {
	for _, doc := range compiler.documents {
		if doc.extends == name {
			return true
		}
	}
	return false
}

func collectStacks(nodes []node, stacks map[string][][]node) {
	for _, item := range nodes {
		if item.kind == nodePush {
			stacks[item.name] = append(stacks[item.name], item.children)
			continue
		}
		collectStacks(item.children, stacks)
		for _, branch := range item.branches {
			collectStacks(branch.nodes, stacks)
		}
		collectStacks(item.alternate, stacks)
	}
}

func (compiler *forgeCompiler) compileNodes(builder *sourceBuilder, nodes []node, environment compileEnvironment, stacks map[string][][]node, context []string) error {
	for _, item := range nodes {
		itemContext := mergeContext(context, item.context)
		switch item.kind {
		case nodeText:
			if len(item.text) == item.span.end-item.span.start {
				builder.appendLiteral(item.text, item.span, itemContext)
			} else {
				builder.append(item.text, item.span, itemContext)
			}
		case nodeAction:
			rewritten := rewriteVariables(item.text, environment.bindings)
			if rewritten == item.text {
				builder.appendLiteral(rewritten, item.span, itemContext)
			} else {
				builder.append(rewritten, item.span, itemContext)
			}
		case nodeYield:
			return compiler.nodeError(item, itemContext, "layout requires section %q", item.name)
		case nodeInclude:
			name := normalizeReference(item.name)
			dependency, exists := compiler.documents[name]
			if !exists {
				return compiler.nodeError(item, itemContext, "unknown include %q", name)
			}
			if dependency.component {
				return compiler.nodeError(item, itemContext, "component %q must be called with @component", name)
			}
			pipeline := rewriteVariables(item.text, environment.bindings)
			builder.append(`{{template `+strconv.Quote(name)+` `+pipeline+`}}`, item.span, append(itemContext, "include "+name))
		case nodeComponent:
			if err := compiler.compileComponent(builder, item, environment, stacks, itemContext); err != nil {
				return err
			}
		case nodeSlot:
			value, exists := environment.slots[item.name]
			if !exists {
				if environment.slots == nil {
					return compiler.nodeError(item, itemContext, "@slot is only valid in a component definition or call")
				}
				if err := compiler.compileNodes(builder, item.children, environment, stacks, append(itemContext, "slot "+item.name+" default")); err != nil {
					return err
				}
				continue
			}
			slotEnvironment := compileEnvironment{bindings: value.bindings, componentStack: value.componentStack}
			if err := compiler.compileNodes(builder, value.nodes, slotEnvironment, stacks, append(itemContext, "slot "+item.name)); err != nil {
				return err
			}
		case nodeIf, nodeFor, nodeWith:
			keyword := map[nodeKind]string{nodeIf: "if", nodeFor: "range", nodeWith: "with"}[item.kind]
			builder.append(`{{`+keyword+` `+rewriteVariables(item.text, environment.bindings)+`}}`, item.span, itemContext)
			if err := compiler.compileNodes(builder, item.children, environment, stacks, itemContext); err != nil {
				return err
			}
			for _, branch := range item.branches {
				builder.append(`{{else if `+rewriteVariables(branch.pipeline, environment.bindings)+`}}`, branch.span, itemContext)
				if err := compiler.compileNodes(builder, branch.nodes, environment, stacks, itemContext); err != nil {
					return err
				}
			}
			if item.alternate != nil {
				builder.append(`{{else}}`, item.span, itemContext)
				if err := compiler.compileNodes(builder, item.alternate, environment, stacks, itemContext); err != nil {
					return err
				}
			}
			builder.append(`{{end}}`, item.span, itemContext)
		case nodePush:
			// Push bodies are emitted only at matching stacks.
		case nodeStack:
			for _, body := range stacks[item.name] {
				if err := compiler.compileNodes(builder, body, environment, stacks, append(itemContext, "stack "+item.name)); err != nil {
					return err
				}
			}
		case nodeCSRF:
			builder.append(`<input type="hidden" name="_token" value="{{.Form.CSRFToken}}">`, item.span, itemContext)
		case nodeMethod:
			builder.append(`<input type="hidden" name="_method" value="`+htmlAttribute(item.name)+`">`, item.span, itemContext)
		case nodeOld:
			action := `{{.Form.Old ` + strconv.Quote(item.name)
			if item.text != "" {
				action += ` ` + rewriteVariables(item.text, environment.bindings)
			}
			builder.append(action+`}}`, item.span, itemContext)
		case nodeErrors:
			builder.append(`{{range .Form.Errors `+strconv.Quote(item.name)+`}}`, item.span, itemContext)
			if err := compiler.compileNodes(builder, item.children, environment, stacks, itemContext); err != nil {
				return err
			}
			builder.append(`{{end}}`, item.span, itemContext)
		case nodeExtends, nodeSection, nodeProps:
			return compiler.nodeError(item, itemContext, "misplaced structural directive")
		default:
			return compiler.nodeError(item, itemContext, "unsupported view node")
		}
	}
	return nil
}

func (compiler *forgeCompiler) compileComponent(builder *sourceBuilder, call node, outer compileEnvironment, stacks map[string][][]node, context []string) error {
	name := normalizeReference(call.name)
	component, exists := compiler.documents[name]
	if !exists || !component.component {
		return compiler.nodeError(call, context, "unknown component %q", name)
	}
	for _, ancestor := range outer.componentStack {
		if ancestor == name {
			return compiler.nodeError(call, context, "component cycle: %s", strings.Join(append(outer.componentStack, name), " -> "))
		}
	}
	provided := make(map[string]argument, len(call.args))
	for _, arg := range call.args {
		provided[arg.name] = arg
	}
	declared := make(map[string]propDefinition, len(component.props))
	for _, prop := range component.props {
		declared[prop.name] = prop
		if prop.required {
			if _, exists := provided[prop.name]; !exists {
				return compiler.nodeError(call, context, "component %q requires prop %q", name, prop.name)
			}
		}
	}
	for prop := range provided {
		if _, exists := declared[prop]; !exists {
			return compiler.nodeError(call, context, "component %q has no prop %q", name, prop)
		}
	}

	definitions := make(map[string]struct{})
	if duplicate := collectSlotDefinitions(component.nodes, definitions); duplicate != nil {
		return compiler.nodeError(*duplicate, context, "component %q declares slot %q more than once", name, duplicate.name)
	}
	providedSlots := make(map[string]slotValue)
	var defaultNodes []node
	for _, child := range call.children {
		if child.kind == nodeSlot {
			if _, exists := providedSlots[child.name]; exists {
				return compiler.nodeError(child, context, "duplicate slot %q", child.name)
			}
			providedSlots[child.name] = slotValue{nodes: child.children, bindings: outer.bindings, componentStack: outer.componentStack, span: child.span}
		} else {
			defaultNodes = append(defaultNodes, child)
		}
	}
	if hasContent(defaultNodes) {
		if _, exists := providedSlots["default"]; exists {
			return compiler.nodeError(call, context, "component %q receives the default slot twice", name)
		}
		providedSlots["default"] = slotValue{nodes: defaultNodes, bindings: outer.bindings, componentStack: outer.componentStack, span: call.span}
	}
	for slot := range providedSlots {
		if _, exists := definitions[slot]; !exists {
			return compiler.nodeError(call, context, "component %q has no slot %q", name, slot)
		}
	}

	compiler.sequence++
	prefix := fmt.Sprintf("$__forge_component_%d_", compiler.sequence)
	bindings := copyBindings(outer.bindings)
	componentContext := append(append([]string{}, context...), "component "+name)
	builder.append(`{{if true}}`, call.span, componentContext)
	for _, prop := range component.props {
		variable := prefix + prop.name
		bindings[prop.name] = variable
		value := strconv.Quote(prop.defaultVal)
		if arg, exists := provided[prop.name]; exists {
			value = rewriteVariables(arg.value, outer.bindings)
		}
		builder.append(`{{`+variable+` := `+value+`}}`, call.span, componentContext)
	}
	environment := compileEnvironment{bindings: bindings, slots: providedSlots, componentStack: append(append([]string{}, outer.componentStack...), name)}
	if err := compiler.compileNodes(builder, component.nodes, environment, stacks, componentContext); err != nil {
		return err
	}
	builder.append(`{{end}}`, call.span, componentContext)
	return nil
}

func collectSlotDefinitions(nodes []node, result map[string]struct{}) *node {
	for _, item := range nodes {
		if item.kind == nodeComponent {
			continue
		}
		if item.kind == nodeSlot {
			if _, exists := result[item.name]; exists {
				duplicate := item
				return &duplicate
			}
			result[item.name] = struct{}{}
			continue
		}
		if duplicate := collectSlotDefinitions(item.children, result); duplicate != nil {
			return duplicate
		}
		for _, branch := range item.branches {
			if duplicate := collectSlotDefinitions(branch.nodes, result); duplicate != nil {
				return duplicate
			}
		}
		if duplicate := collectSlotDefinitions(item.alternate, result); duplicate != nil {
			return duplicate
		}
	}
	return nil
}

func hasContent(nodes []node) bool {
	for _, item := range nodes {
		if item.kind != nodeText || strings.TrimSpace(item.text) != "" {
			return true
		}
	}
	return false
}

func copyBindings(source map[string]string) map[string]string {
	result := make(map[string]string, len(source))
	for name, value := range source {
		result[name] = value
	}
	return result
}

func mergeContext(left, right []string) []string {
	result := append([]string{}, left...)
	for _, value := range right {
		seen := false
		for _, existing := range result {
			if existing == value {
				seen = true
				break
			}
		}
		if !seen {
			result = append(result, value)
		}
	}
	return result
}

func rewriteVariables(source string, bindings map[string]string) string {
	if len(bindings) == 0 {
		return source
	}
	var result strings.Builder
	quote := byte(0)
	escaped := false
	for index := 0; index < len(source); {
		ch := source[index]
		if quote != 0 {
			result.WriteByte(ch)
			index++
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
			result.WriteByte(ch)
			index++
			continue
		}
		if ch == '$' {
			end := index + 1
			for end < len(source) && (source[end] == '_' || source[end] >= 'a' && source[end] <= 'z' || source[end] >= 'A' && source[end] <= 'Z' || source[end] >= '0' && source[end] <= '9') {
				end++
			}
			name := source[index+1 : end]
			if replacement, exists := bindings[name]; exists {
				result.WriteString(replacement)
			} else {
				result.WriteString(source[index:end])
			}
			index = end
			continue
		}
		result.WriteByte(ch)
		index++
	}
	return result.String()
}

func (builder *sourceBuilder) append(text string, span sourceSpan, context []string) {
	builder.appendMapping(text, span, context, true)
}

func (builder *sourceBuilder) appendLiteral(text string, span sourceSpan, context []string) {
	builder.appendMapping(text, span, context, false)
}

func (builder *sourceBuilder) appendMapping(text string, span sourceSpan, context []string, synthetic bool) {
	if text == "" {
		return
	}
	start := builder.text.Len()
	builder.text.WriteString(text)
	builder.mappings = append(builder.mappings, SourceMapping{
		Start: start, End: builder.text.Len(), SourceStart: span.start, Path: span.path, Line: span.line, Column: span.column,
		Synthetic: synthetic,
		Context:   append([]string{}, context...),
	})
}

func (compiler *forgeCompiler) nodeError(item node, context []string, format string, args ...any) error {
	doc := compiler.documents[templateName(item.span.path)]
	message := fmt.Sprintf(format, args...)
	if len(context) > 1 {
		message += " (" + strings.Join(context, " -> ") + ")"
	}
	return sourceError(item.span.path, doc.source, item.span.start, "%s", message)
}

func htmlAttribute(value string) string {
	value = strings.ReplaceAll(value, "&", "&amp;")
	value = strings.ReplaceAll(value, `"`, "&quot;")
	value = strings.ReplaceAll(value, "<", "&lt;")
	value = strings.ReplaceAll(value, ">", "&gt;")
	return value
}

var standardTemplateCall = regexp.MustCompile(`\{\{\s*template\s+"([^"]+)"`)

func templateDependencies(source string) []string {
	matches := standardTemplateCall.FindAllStringSubmatch(source, -1)
	result := make([]string, 0, len(matches))
	seen := make(map[string]bool, len(matches))
	for _, match := range matches {
		if !seen[match[1]] {
			seen[match[1]] = true
			result = append(result, match[1])
		}
	}
	return result
}

func validateTemplateDependencies(compiled []CompiledTemplate) error {
	sources := make(map[string]string, len(compiled))
	for _, item := range compiled {
		sources[item.Name] = item.Source
	}
	state := make(map[string]uint8, len(compiled))
	var visit func(string, []string) error
	visit = func(name string, stack []string) error {
		switch state[name] {
		case 1:
			return fmt.Errorf("view %q: template include cycle: %s", name, strings.Join(append(stack, name), " -> "))
		case 2:
			return nil
		}
		state[name] = 1
		for _, match := range standardTemplateCall.FindAllStringSubmatch(sources[name], -1) {
			dependency := match[1]
			if _, exists := sources[dependency]; !exists {
				return fmt.Errorf("view %q: unknown template dependency %q", name, dependency)
			}
			if err := visit(dependency, append(stack, name)); err != nil {
				return err
			}
		}
		state[name] = 2
		return nil
	}
	for _, item := range compiled {
		if err := visit(item.Name, nil); err != nil {
			return err
		}
	}
	return nil
}
