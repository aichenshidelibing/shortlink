// Package version holds the runtime-mutable version string.
//
// The default is a compile-time constant, but the admin panel can override it
// at runtime via the settings JSON (key: "version"). All external readers
// should use Get() so they observe hot-updates.
package version

import "sync/atomic"

// Default is the build-time default. Overridable at runtime via Set().
const Default = "BETA2.0-ALPHA"

var current atomic.Value // string

func init() {
	current.Store(Default)
}

// Get returns the current runtime version string.
func Get() string {
	if v, ok := current.Load().(string); ok && v != "" {
		return v
	}
	return Default
}

// Set updates the runtime version string. Empty input resets to Default.
func Set(v string) {
	if v == "" {
		current.Store(Default)
		return
	}
	current.Store(v)
}
