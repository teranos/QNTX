package commands

import (
	"encoding/json"
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

var amWhereCmd = &cobra.Command{
	Use:   "where",
	Short: "Show where configuration is loaded from",
	Long: `Show the configuration cascade and which files were checked.

Lists all configuration sources in order of precedence, showing
which files exist and which are missing.`,
	RunE: runAmWhere,
}

var configFormat string

func init() {
	// Add flags
	amShowCmd.Flags().StringVar(&configFormat, "format", "toml", "Output format: toml, json, yaml")

	// Add subcommands
	AmCmd.AddCommand(amShowCmd)
	AmCmd.AddCommand(amGetCmd)
	AmCmd.AddCommand(amValidateCmd)
	AmCmd.AddCommand(amWhereCmd)
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
		// TODO: Extract display package to QNTX for proper output formatting
		// See: https://github.com/teranos/QNTX/issues/41
		data, err := json.MarshalIndent(cfg, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal config to JSON: %w", err)
		}
		fmt.Println(string(data))

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

	// Check if key exists in configuration
	v := am.GetViper()
	if !v.IsSet(key) {
		return fmt.Errorf("configuration key %q not found", key)
	}

	// Get the value as interface{} to preserve type
	value := am.Get(key)
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

func runAmWhere(cmd *cobra.Command, args []string) error {
	// Get the full introspection data
	intro, err := am.GetConfigIntrospection()
	if err != nil {
		return fmt.Errorf("failed to get config introspection: %w", err)
	}

	// Show config cascade header
	fmt.Println("Configuration cascade (later overrides earlier):")
	fmt.Println("  1. [DEFAULT]  Built-in defaults")
	fmt.Println("  2. [SYSTEM]   /etc/qntx/am.toml")
	fmt.Println("  3. [USER]     ~/.qntx/am.toml")
	fmt.Println("  4. [USER_UI]  ~/.qntx/am_from_ui.toml (if exists)")
	fmt.Println("  5. [PROJECT]  ./am.toml (searches up directories)")
	fmt.Println("  6. [ENV]      QNTX_* environment variables")
	fmt.Println()

	// Count sources actually in use
	sourceCounts := make(map[am.ConfigSource]int)
	sourceFiles := make(map[am.ConfigSource]string)

	for _, setting := range intro.Settings {
		sourceCounts[setting.Source]++
		if setting.SourcePath != "" && setting.Source != am.SourceDefault && setting.Source != am.SourceEnvironment {
			sourceFiles[setting.Source] = setting.SourcePath
		}
	}

	// Show active sources
	fmt.Println("Active configuration sources:")
	for source, count := range sourceCounts {
		if count > 0 {
			if path, ok := sourceFiles[source]; ok {
				fmt.Printf("  %s: %d settings from %s\n", source, count, path)
			} else if source == am.SourceEnvironment {
				fmt.Printf("  %s: %d settings from environment variables\n", source, count)
			} else if source == am.SourceDefault {
				fmt.Printf("  %s: %d settings\n", source, count)
			}
		}
	}

	// Show the primary config file being used
	if intro.ConfigFile != "" {
		fmt.Printf("\nPrimary config file: %s\n", intro.ConfigFile)
	}

	return nil
}
