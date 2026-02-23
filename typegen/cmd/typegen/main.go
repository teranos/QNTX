package main

import (
	"fmt"
	"os"

	"github.com/teranos/typegen/cli"
)

func main() {
	if err := cli.TypegenCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
