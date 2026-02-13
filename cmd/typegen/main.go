package main

import (
	"fmt"
	"os"

	"github.com/teranos/QNTX/typegen/cli"
)

func main() {
	if err := cli.TypegenCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
