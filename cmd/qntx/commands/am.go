package commands

import (
	"fmt"

	"github.com/pelletier/go-toml/v2"
	"github.com/spf13/cobra"
	"github.com/teranos/QNTX/am"
	"github.com/teranos/QNTX/sym"
	"gopkg.in/yaml.v3"
)

// AmCmd represents the am (configuration) command
var AmCmd = &cobra.Command{
	Use:   "am",
	Short: sym.AM + " Manage QNTX core configuration",
	Long: sym.AM + ` am — Manage QNTX core configuration ("I am")

Display and manage QNTX core configuration settings.

Configuration sources (in order of precedence):
1. Command line flags
2. Environment variables (QNTX_* prefix)
3. Project config (./am.toml or ./config.toml)
4. User config (~/.qntx/am.toml or ~/.qntx/config.toml)
5. System config (/etc/qntx/config.toml)
6. Default values

Examples:
  qntx am show                    # Show current configuration
  qntx am show --format json      # Show configuration in JSON format
  qntx am get database.path       # Get specific config value
  qntx am validate                # Validate current configuration`,
}

var amShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Show current configuration",
	Long:  "Display the current QNTX core configuration from all sources",
	RunE:  runAmShow,
}

var amGetCmd = &cobra.Command{
	Use:   "get <key>",
	Short: "Get a specific configuration value",
	Long:  "Get a specific configuration value using dot notation (e.g., database.path, pulse.workers)",
	Args:  cobra.ExactArgs(1),
	RunE:  runAmGet,
}

var amValidateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate current configuration",
	Long:  "Validate that the current QNTX core configuration is valid",
	RunE:  runAmValidate,
}

var configFormat string

func init() {
	// Add flags
	amShowCmd.Flags().StringVar(&configFormat, "format", "toml", "Output format: toml, json, yaml")

	// Add subcommands
	AmCmd.AddCommand(amShowCmd)
	AmCmd.AddCommand(amGetCmd)
	AmCmd.AddCommand(amValidateCmd)
}

func runAmShow(cmd *cobra.Command, args []string) error {
	// Load configuration
	cfg, err := am.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Marshal to requested format
	switch configFormat {
	case "json":
		// Simple JSON output
		fmt.Printf("{\n")
		fmt.Printf("  \"database\": {\"path\": %q},\n", cfg.Database.Path)
		fmt.Printf("  \"server\": {\"log_theme\": %q, \"allowed_origins\": %v},\n", cfg.Server.LogTheme, cfg.Server.AllowedOrigins)
		fmt.Printf("  \"pulse\": {\"workers\": %d, \"ticker_interval_seconds\": %d}\n", cfg.Pulse.Workers, cfg.Pulse.TickerIntervalSeconds)
		// ... add more fields as needed
		fmt.Printf("}\n")

	case "yaml":
		data, err := yaml.Marshal(cfg)
		if err != nil {
			return fmt.Errorf("failed to marshal config to YAML: %w", err)
		}
		fmt.Printf("# QNTX core configuration\n%s", string(data))

	case "toml":
		data, err := toml.Marshal(cfg)
		if err != nil {
			return fmt.Errorf("failed to marshal config to TOML: %w", err)
		}
		fmt.Printf("# QNTX core configuration\n%s", string(data))

	default:
		return fmt.Errorf("unsupported format: %s (supported: toml, json, yaml)", configFormat)
	}

	return nil
}

func runAmGet(cmd *cobra.Command, args []string) error {
	key := args[0]
	value := am.GetString(key)

	if value == "" {
		// Try other types
		if intVal := am.GetInt(key); intVal != 0 {
			fmt.Println(intVal)
			return nil
		}
		if boolVal := am.GetBool(key); boolVal {
			fmt.Println(boolVal)
			return nil
		}
	}

	fmt.Println(value)
	return nil
}

func runAmValidate(cmd *cobra.Command, args []string) error {
	// Load configuration
	cfg, err := am.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Validate
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("configuration validation failed: %w", err)
	}

	fmt.Println("✓ Configuration is valid")
	return nil
}
