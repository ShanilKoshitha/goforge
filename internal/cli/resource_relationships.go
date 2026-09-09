package cli

import (
	"fmt"
	"path/filepath"
	"strings"
)

// resourceRelationship is the resolved, one-shot relationship contract used
// only while rendering a resource. It is deliberately absent from .forge.
type resourceRelationship struct {
	resourceBelongsTo
	TargetSpec          resourceSpec
	TargetQueryAccessor string
	IndexName           string
	ConstraintName      string
	targetMigrationPath string
	targetMigration     developmentFileState
}

func resolveResourceRelationships(child resourceSpec, state resourceState, requested []resourceBelongsTo) ([]resourceRelationship, error) {
	if len(requested) == 0 {
		return nil, nil
	}
	schema, err := parseModelSchema(filepath.Join("internal", "models"))
	if err != nil {
		return nil, fmt.Errorf("inspect belongs-to targets: %w", err)
	}
	models := make(map[string]modelDefinition, len(schema.Models))
	for _, model := range schema.Models {
		models[model.Name] = model
	}
	resources := make(map[string]resourceSpec, len(state.Resources))
	for _, resource := range state.Resources {
		resources[resource.Name] = resource
	}

	resolved := make([]resourceRelationship, 0, len(requested))
	for _, relation := range requested {
		target, exists := resources[relation.Target]
		if !exists {
			return nil, fmt.Errorf("belongs-to relationship %q targets %s, which is not an existing generated resource", relation.Name, relation.Target)
		}
		model, exists := models[target.Name]
		if !exists {
			return nil, fmt.Errorf("belongs-to target %s is missing its application model", target.Name)
		}
		if err := validateResourceRelationshipTarget(target, model); err != nil {
			return nil, fmt.Errorf("belongs-to target %s is incompatible: %w", target.Name, err)
		}
		migrationPath, migration, err := captureResourceOwnerCandidateKey(target)
		if err != nil {
			return nil, fmt.Errorf("belongs-to target %s is incompatible: %w", target.Name, err)
		}
		accessor, err := pascal(target.Plural)
		if err != nil {
			return nil, fmt.Errorf("build belongs-to target accessor for %s: %w", target.Name, err)
		}
		indexName := child.Plural + "_" + relation.ForeignKey + "_idx"
		constraintName := child.Plural + "_" + relation.Name + "_owner_fkey"
		identifiers := []struct{ kind, value string }{
			{kind: "index", value: indexName},
			{kind: "constraint", value: constraintName},
		}
		for _, identifier := range identifiers {
			if !safeIdentifier(identifier.value) || len(identifier.value) > maximumResourceIdentifier {
				return nil, fmt.Errorf("belongs-to relationship %q produces %s name %q longer than PostgreSQL's %d-byte identifier limit", relation.Name, identifier.kind, identifier.value, maximumResourceIdentifier)
			}
		}
		resolved = append(resolved, resourceRelationship{
			resourceBelongsTo:   relation,
			TargetSpec:          target,
			TargetQueryAccessor: accessor,
			IndexName:           indexName,
			ConstraintName:      constraintName,
			targetMigrationPath: migrationPath,
			targetMigration:     migration,
		})
	}
	return resolved, nil
}

func captureResourceOwnerCandidateKey(resource resourceSpec) (string, developmentFileState, error) {
	path := filepath.Join("database", "migrations", resource.MigrationVersion+"_create_"+resource.Plural+".up.sql")
	state, err := captureDevelopmentFile(path)
	if err != nil {
		return "", developmentFileState{}, fmt.Errorf("read owner-safe migration %s: %w", filepath.ToSlash(path), err)
	}
	if !state.exists {
		return "", developmentFileState{}, fmt.Errorf("read owner-safe migration %s: file does not exist", filepath.ToSlash(path))
	}
	constraint := resource.Plural + "_owner_id_key"
	if !containsOwnerCandidateKey(state.data, resource.Plural, constraint) {
		return "", developmentFileState{}, fmt.Errorf("migration must retain effective constraint %s UNIQUE (user_id, id)", constraint)
	}
	return path, state, nil
}

func recheckResourceRelationshipDependencies(relationships []resourceRelationship) error {
	for _, relationship := range relationships {
		current, err := captureDevelopmentFile(relationship.targetMigrationPath)
		if err != nil {
			return fmt.Errorf("recheck belongs-to target migration %s: %w", filepath.ToSlash(relationship.targetMigrationPath), err)
		}
		if !current.equal(relationship.targetMigration) {
			return fmt.Errorf("belongs-to target migration %s changed during resource generation; retry the command", filepath.ToSlash(relationship.targetMigrationPath))
		}
	}
	return nil
}

func containsOwnerCandidateKey(contents []byte, table, constraint string) bool {
	tokens := postgresMigrationTokens(contents)
	present := false
	for len(tokens) > 0 {
		end := len(tokens)
		for index, token := range tokens {
			if token == ";" {
				end = index
				break
			}
		}
		statement := tokens[:end]
		for index := 0; index < len(statement); index++ {
			switch {
			case statement[index] == "create":
				tableKeyword := index + 1
				for tableKeyword < len(statement) && (statement[tableKeyword] == "unlogged" || statement[tableKeyword] == "temporary" || statement[tableKeyword] == "temp") {
					tableKeyword++
				}
				if tableKeyword >= len(statement) || statement[tableKeyword] != "table" {
					continue
				}
				if afterTable, ok := targetTableAfter(statement, tableKeyword+1, table); ok {
					present = containsConstraintDeclaration(statement[afterTable:], constraint)
				}
			case index+1 < len(statement) && statement[index] == "alter" && statement[index+1] == "table":
				afterTable, ok := targetTableAfter(statement, index+2, table)
				if !ok {
					continue
				}
				for clause := afterTable; clause < len(statement); clause++ {
					switch {
					case statement[clause] == "add" && containsConstraintDeclaration(statement[clause+1:], constraint):
						present = true
					case statement[clause] == "drop" && droppedConstraint(statement[clause+1:], constraint):
						present = false
					case statement[clause] == "rename" && renamedConstraint(statement[clause+1:], constraint):
						present = false
					case statement[clause] == "rename" && clause+1 < len(statement) && statement[clause+1] == "to":
						present = false
					}
				}
			case index+1 < len(statement) && statement[index] == "drop" && statement[index+1] == "table":
				if _, ok := targetTableAfter(statement, index+2, table); ok {
					present = false
				}
			}
		}
		if end == len(tokens) {
			break
		}
		tokens = tokens[end+1:]
	}
	return present
}

func targetTableAfter(tokens []string, start int, table string) (int, bool) {
	for start < len(tokens) && (tokens[start] == "if" || tokens[start] == "not" || tokens[start] == "exists" || tokens[start] == "only") {
		start++
	}
	if start < len(tokens) && tokens[start] == table {
		return start + 1, true
	}
	if start+2 < len(tokens) && tokens[start+1] == "." && tokens[start+2] == table {
		return start + 3, true
	}
	return 0, false
}

func containsConstraintDeclaration(tokens []string, constraint string) bool {
	wanted := []string{"constraint", constraint, "unique", "(", "user_id", ",", "id", ")"}
	return containsSQLTokenSequence(tokens, wanted)
}

func containsSQLTokenSequence(tokens, wanted []string) bool {
	for start := 0; start+len(wanted) <= len(tokens); start++ {
		matched := true
		for offset := range wanted {
			if tokens[start+offset] != wanted[offset] {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}
	return false
}

func droppedConstraint(tokens []string, constraint string) bool {
	if len(tokens) == 0 || tokens[0] != "constraint" {
		return false
	}
	index := 1
	for index < len(tokens) && (tokens[index] == "if" || tokens[index] == "exists") {
		index++
	}
	return index < len(tokens) && tokens[index] == constraint
}

func renamedConstraint(tokens []string, constraint string) bool {
	return len(tokens) >= 2 && tokens[0] == "constraint" && tokens[1] == constraint
}

// postgresMigrationTokens extracts only identifiers and punctuation relevant
// to constraint declarations. Comments and string bodies are skipped so an
// inert example cannot satisfy a schema preflight; quoted identifiers remain
// valid escape hatches.
func postgresMigrationTokens(source []byte) []string {
	var tokens []string
	for index := 0; index < len(source); {
		switch {
		case isSQLSpace(source[index]):
			index++
		case source[index] == '-' && index+1 < len(source) && source[index+1] == '-':
			index += 2
			for index < len(source) && source[index] != '\n' {
				index++
			}
		case source[index] == '/' && index+1 < len(source) && source[index+1] == '*':
			index = skipSQLBlockComment(source, index+2)
		case source[index] == '\'':
			index = skipSQLQuotedBody(source, index+1, '\'', false)
		case (source[index] == 'e' || source[index] == 'E') && index+1 < len(source) && source[index+1] == '\'' && (index == 0 || !isSQLIdentifierByte(source[index-1])):
			index = skipSQLQuotedBody(source, index+2, '\'', true)
		case (source[index] == 'u' || source[index] == 'U') && index+2 < len(source) && source[index+1] == '&' && source[index+2] == '\'' && (index == 0 || !isSQLIdentifierByte(source[index-1])):
			index = skipSQLQuotedBody(source, index+3, '\'', false)
		case source[index] == '$':
			if next, ok := skipSQLDollarQuotedBody(source, index); ok {
				index = next
			} else {
				index++
			}
		case source[index] == '"':
			identifier, next := readSQLQuotedIdentifier(source, index+1)
			if identifier != "" {
				tokens = append(tokens, identifier)
			}
			index = next
		case isSQLIdentifierByte(source[index]):
			start := index
			for index < len(source) && isSQLIdentifierByte(source[index]) {
				index++
			}
			tokens = append(tokens, strings.ToLower(string(source[start:index])))
		case strings.ContainsRune("().,;", rune(source[index])):
			tokens = append(tokens, string(source[index]))
			index++
		default:
			index++
		}
	}
	return tokens
}

func skipSQLBlockComment(source []byte, index int) int {
	depth := 1
	for index < len(source) && depth > 0 {
		switch {
		case index+1 < len(source) && source[index] == '/' && source[index+1] == '*':
			depth++
			index += 2
		case index+1 < len(source) && source[index] == '*' && source[index+1] == '/':
			depth--
			index += 2
		default:
			index++
		}
	}
	return index
}

func skipSQLQuotedBody(source []byte, index int, quote byte, backslashEscapes bool) int {
	for index < len(source) {
		if backslashEscapes && source[index] == '\\' && index+1 < len(source) {
			index += 2
			continue
		}
		if source[index] != quote {
			index++
			continue
		}
		if index+1 < len(source) && source[index+1] == quote {
			index += 2
			continue
		}
		return index + 1
	}
	return index
}

func skipSQLDollarQuotedBody(source []byte, start int) (int, bool) {
	end := start + 1
	for end < len(source) && (isSQLIdentifierByte(source[end]) || source[end] == '$') {
		if source[end] == '$' {
			delimiter := source[start : end+1]
			body := source[end+1:]
			if closing := strings.Index(string(body), string(delimiter)); closing >= 0 {
				return end + 1 + closing + len(delimiter), true
			}
			return len(source), true
		}
		end++
	}
	return start, false
}

func readSQLQuotedIdentifier(source []byte, index int) (string, int) {
	var value strings.Builder
	for index < len(source) {
		if source[index] != '"' {
			value.WriteByte(source[index])
			index++
			continue
		}
		if index+1 < len(source) && source[index+1] == '"' {
			value.WriteByte('"')
			index += 2
			continue
		}
		return value.String(), index + 1
	}
	return value.String(), index
}

func isSQLIdentifierByte(value byte) bool {
	return value == '_' || value == '$' || value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' || value >= '0' && value <= '9'
}

func isSQLSpace(value byte) bool {
	return value == ' ' || value == '\t' || value == '\n' || value == '\r' || value == '\f'
}

func validateResourceRelationshipTarget(resource resourceSpec, model modelDefinition) error {
	if model.Table != resource.Plural {
		return fmt.Errorf("model table is %q, want %q", model.Table, resource.Plural)
	}
	id, idFound := resourceModelField(model, "ID")
	if !idFound || id.GoType != "int64" || !id.Primary || !id.Generated || !id.Required {
		return fmt.Errorf("model must retain its generated required int64 ID")
	}
	ownerID, ownerFound := resourceModelField(model, "UserID")
	if !ownerFound || ownerID.GoType != "int64" || !ownerID.Protected || !ownerID.Required || ownerID.Reference == nil || ownerID.Reference.Model != "User" || ownerID.Reference.Field != "ID" {
		return fmt.Errorf("model must retain its protected required int64 UserID reference")
	}
	for _, relation := range model.Relations {
		if relation.Name == "Owner" && relation.Kind == relationBelongsTo && relation.Target == "User" && relation.ForeignKey == "UserID" && relation.References == "ID" {
			return nil
		}
	}
	return fmt.Errorf("model must retain its Owner belongs-to relationship")
}

func resourceModelField(model modelDefinition, name string) (modelField, bool) {
	for _, field := range model.Fields {
		if field.Name == name {
			return field, true
		}
	}
	return modelField{}, false
}
