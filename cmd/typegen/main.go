package main

import (
	"fmt"
	"os"

	"github.com/teranos/QNTX/cmd/typegen/cmd"
)

func main() {
	if err := cmd.TypegenCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
