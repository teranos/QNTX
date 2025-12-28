package commands

import (
	"fmt"

	"github.com/teranos/QNTX/logger"
	"github.com/teranos/QNTX/sym"
	"github.com/teranos/QNTX/version"
)

// printStartupBanner prints the user-friendly startup message
func printStartupBanner(verbosity int, dbPath string) {
	// ANSI escape codes
	cyan := "\033[36m"
	green := "\033[32m"
	yellow := "\033[33m"
	blue := "\033[34m"
	magenta := "\033[35m"
	white := "\033[37m"
	bgBlack := "\033[40m"
	bold := "\033[1m"
	reset := "\033[0m"

	versionInfo := version.Get()

	fmt.Printf("\n%s%s", cyan, bold)
	fmt.Printf("   ╔═══════════════════════════════════════════════════╗\n")
	fmt.Printf("   ║                                                   ║\n")
	fmt.Printf("   ║        %s%s%s ██████  ███   ██   %s                       ║\n", white, bold, bgBlack, reset+cyan+bold)
	fmt.Printf("   ║        %s%s%s██    ██ ████  ██   %s                       ║\n", white, bold, bgBlack, reset+cyan+bold)
	fmt.Printf("   ║        %s%s%s██    ██ ██ ██ ██   %s                       ║\n", white, bold, bgBlack, reset+cyan+bold)
	fmt.Printf("   ║        %s%s%s██ ▄▄ ██ ██  ████   %s                       ║\n", white, bold, bgBlack, reset+cyan+bold)
	fmt.Printf("   ║        %s%s%s ██████  ██   ███   %s                       ║\n", white, bold, bgBlack, reset+cyan+bold)
	fmt.Printf("   ║           %s%s%s▀▀               %s                       ║\n", white, bold, bgBlack, reset+cyan+bold)
	fmt.Printf("   ║        %s%s%s████████ ██    ██   %s                       ║\n", white, bold, bgBlack, reset+cyan+bold)
	fmt.Printf("   ║           %s%s%s██     ██  ██    %s                       ║\n", white, bold, bgBlack, reset+cyan+bold)
	fmt.Printf("   ║           %s%s%s██      ████     %s                       ║\n", white, bold, bgBlack, reset+cyan+bold)
	fmt.Printf("   ║           %s%s%s██     ██  ██    %s                       ║\n", white, bold, bgBlack, reset+cyan+bold)
	fmt.Printf("   ║           %s%s%s██    ██    ██   %s                       ║\n", white, bold, bgBlack, reset+cyan+bold)
	fmt.Printf("   ║                                                   ║\n")
	fmt.Printf("   ║   %s▣%s Attest  %s⟐%s View  %s%s%s Graph  %s%s%s Pulse            ║\n",
		blue, reset+cyan+bold, yellow, reset+cyan+bold, green, sym.AX, reset+cyan+bold, magenta, sym.Pulse, reset+cyan+bold)
	fmt.Printf("   ║                                                   ║\n")
	fmt.Printf("   ╚═══════════════════════════════════════════════════╝%s\n\n", reset)

	fmt.Printf("%s%s┌─ QNTX Info ─────────────────────────────────────────┐%s\n", green, bold, reset)
	fmt.Printf("%s│%s Version:   %s (commit %s)\n", green, reset, versionInfo.Version, versionInfo.Short())
	fmt.Printf("%s│%s Built:     %s\n", green, reset, versionInfo.BuildTime)
	fmt.Printf("%s│%s Verbosity: %s\n", green, reset, logger.LevelName(verbosity))
	if dbPath != "" {
		fmt.Printf("%s│%s Database:  %s\n", green, reset, dbPath)
	}
	if verbosity >= 2 {
		fmt.Printf("%s│%s Logs:      tmp/graph-debug.log\n", green, reset)
	}
	fmt.Printf("%s└─────────────────────────────────────────────────────┘%s\n", green, reset)

	fmt.Printf("\n%s%s✨ Type Ax queries to see live graph updates%s\n", yellow, bold, reset)
	fmt.Printf("%s💡 Press Ctrl+C to stop%s\n\n", blue, reset)
}
