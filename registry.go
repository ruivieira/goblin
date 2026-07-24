package goblin

import (
	"fmt"
	"sort"
	"sync"
)

var (
	regMu   sync.RWMutex
	byName  = make(map[string]Action)
	ordered []Action
)

// Register adds an Action to the global registry. Panics on duplicate Name().
func Register(a Action) {
	if a == nil {
		panic("goblin: Register(nil)")
	}
	name := a.Name()
	if name == "" {
		panic("goblin: Register action with empty Name")
	}
	regMu.Lock()
	defer regMu.Unlock()
	if _, exists := byName[name]; exists {
		panic(fmt.Sprintf("goblin: duplicate action registration: %s", name))
	}
	byName[name] = a
	ordered = append(ordered, a)
	sort.Slice(ordered, func(i, j int) bool {
		return ordered[i].Name() < ordered[j].Name()
	})
}

// All returns all registered actions in stable Name order.
func All() []Action {
	regMu.RLock()
	defer regMu.RUnlock()
	out := make([]Action, len(ordered))
	copy(out, ordered)
	return out
}

// ByName returns the action with the given name, or nil if not found.
func ByName(name string) Action {
	regMu.RLock()
	defer regMu.RUnlock()
	return byName[name]
}
