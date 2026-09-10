package schedule

import (
	"fmt"
	"sort"
	"sync"
)

// Registry contains explicit code-owned schedule definitions.
type Registry struct {
	mu          sync.RWMutex
	definitions map[string]Definition
}

func NewRegistry() *Registry { return &Registry{definitions: make(map[string]Definition)} }

// Register adds a definition without runtime discovery.
func Register(registry *Registry, definition Definition) error {
	if registry == nil {
		return fmt.Errorf("schedule: registry is required")
	}
	if definition.name == "" || definition.materialize == nil {
		return fmt.Errorf("schedule: valid definition is required")
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if _, exists := registry.definitions[definition.name]; exists {
		return fmt.Errorf("schedule: duplicate registration: %s", definition.name)
	}
	registry.definitions[definition.name] = definition
	return nil
}

func MustRegister(registry *Registry, definition Definition) {
	if err := Register(registry, definition); err != nil {
		panic(err)
	}
}

func (registry *Registry) Names() []string {
	definitions := registry.Definitions()
	names := make([]string, len(definitions))
	for index, definition := range definitions {
		names[index] = definition.Name()
	}
	return names
}

// Definitions returns deterministic immutable definition values.
func (registry *Registry) Definitions() []Definition {
	if registry == nil {
		return nil
	}
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	definitions := make([]Definition, 0, len(registry.definitions))
	for _, definition := range registry.definitions {
		definitions = append(definitions, definition)
	}
	sort.Slice(definitions, func(i, j int) bool { return definitions[i].name < definitions[j].name })
	return definitions
}
