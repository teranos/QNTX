package commands

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"

	"github.com/pelletier/go-toml/v2"
	"github.com/spf13/cobra"
	"github.com/teranos/QNTX/internal/config"
	"github.com/teranos/QNTX/sym"
	"github.com/teranos/errors"
	"gopkg.in/yaml.v3"
)

// AmCmd represents the am (configuration) command
var AmCmd = &cobra.Command{
	Use:     "am",
	Aliases: []string{"config"},
	Short:   sym.AM + " Manage QNTX core configuration",
	Long: sym.AM + ` config — Manage QNTX core configuration

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
	cfg, err := config.Load()
	if err != nil {
		return errors.Wrapf(err, "failed to load config")
	}

	// Marshal to requested format
	switch configFormat {
	case "json":
		// TODO: Extract display package to QNTX for proper output formatting
		// See: https://github.com/teranos/QNTX/issues/41
		data, err := json.MarshalIndent(cfg, "", "  ")
		if err != nil {
			return errors.Wrapf(err, "failed to marshal config to JSON")
		}
		fmt.Println(string(data))

	case "yaml":
		data, err := yaml.Marshal(cfg)
		if err != nil {
			return errors.Wrapf(err, "failed to marshal config to YAML")
		}
		fmt.Printf("# QNTX core configuration\n%s", string(data))

	case "toml":
		data, err := toml.Marshal(cfg)
		if err != nil {
			return errors.Wrapf(err, "failed to marshal config to TOML")
		}
		fmt.Printf("# QNTX core configuration\n%s", string(data))

	default:
		return errors.Newf("unsupported format: %s (supported: toml, json, yaml)", configFormat)
	}

	return nil
}

func runAmGet(cmd *cobra.Command, args []string) error {
	key := args[0]

	// Check if key exists in configuration
	v := config.GetViper()
	if !v.IsSet(key) {
		return errors.Newf("configuration key %q not found", key)
	}

	// Get the value as interface{} to preserve type
	value := config.Get(key)
	fmt.Println(value)
	return nil
}

func runAmValidate(cmd *cobra.Command, args []string) error {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		return errors.Wrapf(err, "failed to load config")
	}

	// Validate
	if err := cfg.Validate(); err != nil {
		return errors.Wrapf(err, "configuration validation failed")
	}

	fmt.Println("✓ Configuration is valid")
	return nil
}

func runAmWhere(cmd *cobra.Command, args []string) error {
	// Get the full introspection data
	intro, err := config.GetConfigIntrospection()
	if err != nil {
		return errors.Wrapf(err, "failed to get config introspection")
	}

	// Show config cascade header
	fmt.Println("Configuration cascade (later overrides earlier):")
	fmt.Println("  1. [DEFAULT]  Built-in defaults")
	fmt.Println("  2. [SYSTEM]   /etc/qntx/config.toml")
	fmt.Println("  3. [USER]     ~/.qntx/config.toml (backward compat)")
	fmt.Println("  4. [USER]     ~/.qntx/am.toml (preferred)")
	fmt.Println("  5. [USER_UI]  ~/.qntx/config_from_ui.toml (backward compat)")
	fmt.Println("  6. [USER_UI]  ~/.qntx/am_from_ui.toml (preferred)")
	fmt.Println("  7. [PROJECT]  ./am.toml or ./config.toml (searches up directories)")
	fmt.Println("  8. [ENV]      QNTX_* environment variables")
	fmt.Println()

	// Group settings by actual file path (to distinguish config.toml from am.toml)
	type fileGroup struct {
		source   config.ConfigSource
		path     string
		settings []config.SettingInfo
	}

	// Map from path to settings
	settingsByPath := make(map[string]*fileGroup)

	// Group settings by their actual source file
	for _, setting := range intro.Settings {
		key := setting.SourcePath
		if key == "" {
			// For defaults and env vars, use source as key
			key = string(setting.Source)
		}

		if group, exists := settingsByPath[key]; exists {
			group.settings = append(group.settings, setting)
		} else {
			settingsByPath[key] = &fileGroup{
				source:   setting.Source,
				path:     setting.SourcePath,
				settings: []config.SettingInfo{setting},
			}
		}
	}

	// Define source order for consistent output
	sourceOrder := []config.ConfigSource{
		config.SourceDefault,
		config.SourceSystem,
		config.SourceUser,
		config.SourceUserUI,
		config.SourceProject,
		config.SourceEnvironment,
	}

	// Show active sources with their settings
	fmt.Println("Active configuration:")
	for _, source := range sourceOrder {
		// Collect and sort file groups for this source level
		var groups []*fileGroup
		for _, group := range settingsByPath {
			if group.source == source && len(group.settings) > 0 {
				groups = append(groups, group)
			}
		}

		// Sort groups for consistent display order (not precedence!)
		sort.Slice(groups, func(i, j int) bool {
			// Special case for default/env (empty paths)
			if groups[i].path == "" {
				return true
			}
			if groups[j].path == "" {
				return false
			}
			// Put config.toml before am.toml at same level
			iBase := filepath.Base(groups[i].path)
			jBase := filepath.Base(groups[j].path)
			if iBase == "config.toml" && jBase == "am.toml" {
				return true
			}
			if iBase == "am.toml" && jBase == "config.toml" {
				return false
			}
			// Similarly for UI configs
			if iBase == "config_from_ui.toml" && jBase == "am_from_ui.toml" {
				return true
			}
			if iBase == "am_from_ui.toml" && jBase == "config_from_ui.toml" {
				return false
			}
			return groups[i].path < groups[j].path
		})

		// Print each group
		for _, group := range groups {
			// Print source header
			if group.path != "" {
				fmt.Printf("\n%s: %d settings from %s\n", source, len(group.settings), group.path)
			} else if source == config.SourceEnvironment {
				fmt.Printf("\n%s: %d settings from environment variables\n", source, len(group.settings))
			} else if source == config.SourceDefault {
				fmt.Printf("\n%s: %d settings\n", source, len(group.settings))
			}

			// Print each setting
			for _, setting := range group.settings {
				// Format the value for display
				valueStr := fmt.Sprintf("%v", setting.Value)
				// Truncate long values
				if len(valueStr) > 50 {
					valueStr = valueStr[:47] + "..."
				}
				fmt.Printf("  %s = %s\n", setting.Key, valueStr)
			}
		}
	}

	return nil
}
