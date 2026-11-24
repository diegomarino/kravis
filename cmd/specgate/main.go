// Command specgate is the CI spec gate: it fails a pull request that changes
// what was already agreed, unless the person approved it.
package main

import (
	"os"

	"github.com/diegomarino/kravis/internal/specgate"
)

func main() {
	os.Exit(specgate.Main(os.Args[1:], os.Stdout, os.Stderr, specgate.ExecRunner))
}
