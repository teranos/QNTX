// Command typegen generates TypeScript type definitions from Go source files.
//
// Usage:
//
//	typegen -src <go-files-or-dirs> -out <output.ts>
//	typegen -src server/types.go,server/pulse_types.go -out web/types/generated.ts
//	typegen -check -src server -out web/types/generated.ts
//
// The -check flag compares generated output against the existing file and
// exits with code 1 if they differ (useful for CI).
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/teranos/QNTX/tools/typegen"
)

func main() {
	var (
		srcFlag   = flag.String("src", "", "comma-separated Go source files or directories")
		outFlag   = flag.String("out", "", "output TypeScript file path")
		checkFlag = flag.Bool("check", false, "check mode: verify generated matches existing file")
	)
	flag.Parse()

	if *srcFlag == "" {
		fmt.Fprintln(os.Stderr, "error: -src is required")
		flag.Usage()
		os.Exit(1)
	}

	if *outFlag == "" {
		fmt.Fprintln(os.Stderr, "error: -out is required")
		flag.Usage()
		os.Exit(1)
	}

	// Parse source paths
	srcPaths := strings.Split(*srcFlag, ",")
	var allPaths []string

	for _, src := range srcPaths {
		src = strings.TrimSpace(src)
		if src == "" {
			continue
		}

		info, err := os.Stat(src)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}

		if info.IsDir() {
			// Collect all .go files from directory
			entries, err := os.ReadDir(src)
			if err != nil {
				fmt.Fprintf(os.Stderr, "error reading directory %s: %v\n", src, err)
				os.Exit(1)
			}
			for _, entry := range entries {
				if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".go") && !strings.HasSuffix(entry.Name(), "_test.go") {
					allPaths = append(allPaths, filepath.Join(src, entry.Name()))
				}
			}
		} else {
			allPaths = append(allPaths, src)
		}
	}

	if len(allPaths) == 0 {
		fmt.Fprintln(os.Stderr, "error: no Go source files found")
		os.Exit(1)
	}

	// Generate TypeScript
	generated, err := typegen.GenerateFromFiles(allPaths)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error generating: %v\n", err)
		os.Exit(1)
	}

	if generated == "" {
		fmt.Fprintln(os.Stderr, "warning: no @ts-export types found in source files")
		os.Exit(0)
	}

	if *checkFlag {
		// Check mode: compare against existing file
		existing, err := os.ReadFile(*outFlag)
		if err != nil {
			if os.IsNotExist(err) {
				fmt.Fprintf(os.Stderr, "error: output file %s does not exist\n", *outFlag)
				os.Exit(1)
			}
			fmt.Fprintf(os.Stderr, "error reading %s: %v\n", *outFlag, err)
			os.Exit(1)
		}

		if normalizeForComparison(string(existing)) != normalizeForComparison(generated) {
			fmt.Fprintf(os.Stderr, "error: generated types differ from %s\n", *outFlag)
			fmt.Fprintln(os.Stderr, "run 'make typegen' to update")
			os.Exit(1)
		}

		fmt.Printf("ok: %s is up to date\n", *outFlag)
		os.Exit(0)
	}

	// Write mode: output to file
	if err := os.WriteFile(*outFlag, []byte(generated), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "error writing %s: %v\n", *outFlag, err)
		os.Exit(1)
	}

	fmt.Printf("generated: %s\n", *outFlag)
}

// normalizeForComparison removes trailing whitespace for comparison
func normalizeForComparison(s string) string {
	lines := strings.Split(s, "\n")
	var result []string
	for _, line := range lines {
		result = append(result, strings.TrimRight(line, " \t"))
	}
	return strings.TrimSpace(strings.Join(result, "\n"))
}
