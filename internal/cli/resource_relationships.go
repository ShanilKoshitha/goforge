package cli

import (
	"fmt"
	"path/filepath"
)

// resourceRelationship is the resolved, one-shot relationship contract used
// only while rendering a resource. It is deliberately absent from .forge.
type resourceRelationship struct {
	resourceBelongsTo
	TargetSpec          resourceSpec
	TargetQueryAccessor string
	IndexName           string
	ConstraintName      string
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
		})
	}
	return resolved, nil
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
