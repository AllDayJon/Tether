package cmd

// Version is set at build time via -ldflags "-X tether/cmd.Version=<tag>".
// Falls back to "dev" for local builds.
var Version = "dev"
