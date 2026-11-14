// Package version holds the build version, set with -ldflags at build time.
package version

// Version is overridden by the build (-X github.com/0x2alx/gigg/internal/version.Version=...).
var Version = "dev"
