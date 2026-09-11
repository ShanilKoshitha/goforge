package cli

import (
	"fmt"
	"strings"
)

func parseMakeModelArguments(arguments []string) (string, []resourceField, []resourceBelongsTo, error) {
	if len(arguments) == 0 || strings.HasPrefix(arguments[0], "--") {
		return "", nil, nil, modelUsageError()
	}
	name := arguments[0]
	var fieldSpecifications []string
	var belongsToSpecifications []string
	for index := 1; index < len(arguments); index++ {
		argument := arguments[index]
		switch {
		case argument == "--field":
			if index+1 >= len(arguments) || strings.HasPrefix(arguments[index+1], "--") {
				return "", nil, nil, fmt.Errorf("--field requires a field specification; %w", modelUsageError())
			}
			index++
			fieldSpecifications = append(fieldSpecifications, arguments[index])
		case strings.HasPrefix(argument, "--field="):
			value := strings.TrimPrefix(argument, "--field=")
			if value == "" {
				return "", nil, nil, fmt.Errorf("--field requires a field specification; %w", modelUsageError())
			}
			fieldSpecifications = append(fieldSpecifications, value)
		case argument == "--belongs-to":
			if index+1 >= len(arguments) || strings.HasPrefix(arguments[index+1], "--") {
				return "", nil, nil, fmt.Errorf("--belongs-to requires a relationship specification; %w", modelUsageError())
			}
			index++
			belongsToSpecifications = append(belongsToSpecifications, arguments[index])
		case strings.HasPrefix(argument, "--belongs-to="):
			value := strings.TrimPrefix(argument, "--belongs-to=")
			if value == "" {
				return "", nil, nil, fmt.Errorf("--belongs-to requires a relationship specification; %w", modelUsageError())
			}
			belongsToSpecifications = append(belongsToSpecifications, value)
		default:
			return "", nil, nil, fmt.Errorf("unknown model option %q; %w", argument, modelUsageError())
		}
	}
	var fields []resourceField
	var err error
	if len(fieldSpecifications) > 0 {
		fields, err = parseModelFields(fieldSpecifications)
		if err != nil {
			return "", nil, nil, fmt.Errorf("invalid model field: %w", err)
		}
	}
	relationships, err := parseModelBelongsTo(name, belongsToSpecifications)
	if err != nil {
		return "", nil, nil, fmt.Errorf("invalid model relationship: %w", err)
	}
	if err := validateModelMemberCollisions(fields, relationships); err != nil {
		return "", nil, nil, fmt.Errorf("invalid model members: %w", err)
	}
	return name, fields, relationships, nil
}

func modelUsageError() error {
	return fmt.Errorf("usage: forge make:model <name> [--field <name>:<string|text|integer|boolean>[:required|nullable]]... [--belongs-to <name>:<ExistingModel>]...")
}
