// Command kravis is the Kravis CLI: a thin layer over the library.
package main

import "os"

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
