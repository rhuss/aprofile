// Command aprofile manages side-by-side profiles for coding harnesses.
package main

import (
	"os"

	"github.com/cc-deck/aprofile/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
