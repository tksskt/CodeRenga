package templatefs

import "embed"

// Files contains templates used exclusively by the explicit --init command.
//
//go:embed coderenga.d
var Files embed.FS
