package job

import (
	"context"
	"fmt"
	"sort"
	"sync"
)

type registeredHandler struct {
	policy Policy
	decode func([]byte) (any, error)
	handle func(context.Context, any) error
}

// Registry contains explicitly registered versioned handlers.
type Registry struct {
	mu       sync.RWMutex
	handlers map[string]registeredHandler
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry { return &Registry{handlers: make(map[string]registeredHandler)} }

// Register adds a typed handler without runtime discovery.
func Register[P any](registry *Registry, definition Definition[P], handler Handler[P]) error {
	if registry == nil {
		return fmt.Errorf("job: registry is required")
	}
	if definition.name == "" {
		return fmt.Errorf("job: valid definition is required")
	}
	if handler == nil {
		return fmt.Errorf("job: handler for %s is required", definition.name)
	}
	entry := registeredHandler{
		policy: definition.Policy(),
		decode: func(payload []byte) (any, error) { return decodePayload[P](payload) },
		handle: func(ctx context.Context, payload any) error { return handler(ctx, payload.(P)) },
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if _, exists := registry.handlers[definition.name]; exists {
		return fmt.Errorf("%w: %s", ErrDuplicate, definition.name)
	}
	registry.handlers[definition.name] = entry
	return nil
}

// MustRegister panics when generated registration is invalid.
func MustRegister[P any](registry *Registry, definition Definition[P], handler Handler[P]) {
	if err := Register(registry, definition, handler); err != nil {
		panic(err)
	}
}

// Names returns stable sorted names for claim filtering.
func (registry *Registry) Names() []string {
	if registry == nil {
		return nil
	}
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	names := make([]string, 0, len(registry.handlers))
	for name := range registry.handlers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (registry *Registry) lookup(name string) (registeredHandler, bool) {
	if registry == nil {
		return registeredHandler{}, false
	}
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	handler, ok := registry.handlers[name]
	return handler, ok
}
