package main

import (
	"fmt"
	"os"

	"github.com/teranos/QNTX/cmd/qntx/commands"
)

func main() {
	if err := commands.TypegenCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
