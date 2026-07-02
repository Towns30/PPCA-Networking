package cc

// registry.go maps a CC algorithm name (chosen via the -cc flag on the
// binaries) to a factory that builds a fresh Controller per connection, and
// exposes a helper that installs it on a live QUIC connection.
// DO NOT EDIT for the assignment — register your algorithm from student.go.

import (
	"fmt"
	"sort"

	quic "github.com/apernet/quic-go"
	"github.com/apernet/quic-go/congestion"
)

// Factory builds a fresh Controller for a single connection. Each connection
// gets its own Controller instance so per-connection state never leaks.
type Factory func() Controller

var registry = map[string]Factory{}

// Register makes a Controller available under name. Call it from an init()
// function. Registering the same name twice panics.
func Register(name string, f Factory) {
	if _, dup := registry[name]; dup {
		panic("cc: duplicate registration for " + name)
	}
	registry[name] = f
}

// Names lists all registered algorithm names, sorted.
func Names() []string {
	out := make([]string, 0, len(registry))
	for n := range registry {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// New builds the quic-go congestion controller for the named algorithm.
func New(name string) (congestion.CongestionControl, error) {
	f, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("cc: unknown algorithm %q (registered: %v)", name, Names())
	}
	return newAdapter(f()), nil
}

// Apply installs the named congestion controller on a QUIC connection. Call it
// right after the connection is established, on whichever side sends bulk data.
func Apply(conn *quic.Conn, name string) error {
	cc, err := New(name)
	if err != nil {
		return err
	}
	conn.SetCongestionControl(cc)
	return nil
}
