package main

import (
	"os"

	"github.com/tks/coderenga/internal/app"
)

var version = "0.1.0-dev"

func main() {
	os.Exit(app.Run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, app.Options{
		Version: version,
	}))
}
