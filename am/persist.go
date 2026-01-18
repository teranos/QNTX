package am

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"

	"github.com/teranos/QNTX/errors"
)

// createBackup creates rotating backups (.back1, .back2, .back3) before modifying config
func createBackup(configPath string) error {
	// Check if file exists before backing up
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return nil // No file to backup
	}

	// Rotate backups: .back3 -> delete, .back2 -> .back3, .back1 -> .back2, current -> .back1
	back3 := configPath + ".back3"
	back2 := configPath + ".back2"
	back1 := configPath + ".back1"

	// Delete oldest backup if exists
	if err := os.Remove(back3); err != nil && !os.IsNotExist(err) {
		// Log deletion failures (but don't fail config save)
		fmt.Printf("⚠️  Failed to delete old backup %s: %v\n", back3, err)
	}

	// Rotate .back2 to .back3
	if _, err := os.Stat(back2); err == nil {
		if err := os.Rename(back2, back3); err != nil {
			return errors.Wrap(err, "failed to rotate .back2 to .back3")
		}
	}

	// Rotate .back1 to .back2
	if _, err := os.Stat(back1); err == nil {
		if err := os.Rename(back1, back2); err != nil {
			return errors.Wrap(err, "failed to rotate .back1 to .back2")
		}
	}

	// Copy current to .back1
	content, err := os.ReadFile(configPath)
	if err != nil {
		return errors.Wrap(err, "failed to read config for backup")
	}

	if err := os.WriteFile(back1, content, 0644); err != nil {
		return errors.Wrap(err, "failed to create .back1")
	}

	return nil
}

// GetUIConfigPath returns the path to the UI-managed config file in ~/.qntx/am_from_ui.toml
func GetUIConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".qntx", "am_from_ui.toml")
}

// loadOrInitializeUIConfig loads the UI config file, or creates an empty one if it doesn't exist
func loadOrInitializeUIConfig() (map[string]interface{}, string, error) {
	configPath := GetUIConfigPath()
	if configPath == "" {
		return nil, "", errors.New("could not determine home directory")
	}

	// Ensure ~/.qntx directory exists
	qntxDir := filepath.Dir(configPath)
	if err := os.MkdirAll(qntxDir, 0750); err != nil {
		return nil, "", errors.Wrap(err, "failed to create .qntx directory")
	}

	// Try to read existing config
	var config map[string]interface{}
	if data, err := os.ReadFile(configPath); err == nil {
		// File exists, parse it
		if err := toml.Unmarshal(data, &config); err != nil {
			return nil, "", errors.Wrap(err, "failed to parse UI config")
		}
	} else {
		// File doesn't exist, create empty config
		config = make(map[string]interface{})
	}

	return config, configPath, nil
}

// saveUIConfig writes the config to the UI config file with backup
func saveUIConfig(config map[string]interface{}, configPath string) error {
	// Create backup
	if err := createBackup(configPath); err != nil {
		return errors.Wrap(err, "failed to create backup")
	}

	// Marshal to TOML
	data, err := toml.Marshal(config)
	if err != nil {
		return errors.Wrap(err, "failed to marshal config")
	}

	// Mark this as our own write to prevent reload loops
	globalWatcherMu.Lock()
	if globalWatcher != nil {
		globalWatcher.MarkOwnWrite()
	}
	globalWatcherMu.Unlock()

	// Write to file
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		return errors.Wrap(err, "failed to write UI config")
	}

	return nil
}

// UpdateLocalInferenceEnabled updates the local_inference.enabled setting in UI config
func UpdateLocalInferenceEnabled(enabled bool) error {
	config, configPath, err := loadOrInitializeUIConfig()
	if err != nil {
		return errors.Wrap(err, "failed to load UI config")
	}

	// Get or create local_inference section
	var localInference map[string]interface{}
	if li, ok := config["local_inference"].(map[string]interface{}); ok {
		localInference = li
	} else {
		localInference = make(map[string]interface{})
	}

	// Update enabled field
	localInference["enabled"] = enabled
	config["local_inference"] = localInference

	return saveUIConfig(config, configPath)
}

// UpdateLocalInferenceModel updates the local_inference.model setting in UI config
func UpdateLocalInferenceModel(model string) error {
	config, configPath, err := loadOrInitializeUIConfig()
	if err != nil {
		return errors.Wrap(err, "failed to load UI config")
	}

	// Get or create local_inference section
	var localInference map[string]interface{}
	if li, ok := config["local_inference"].(map[string]interface{}); ok {
		localInference = li
	} else {
		localInference = make(map[string]interface{})
	}

	// Update model field
	localInference["model"] = model
	config["local_inference"] = localInference

	return saveUIConfig(config, configPath)
}

// UpdateLocalInferenceONNXModelPath updates the local_inference.onnx_model_path setting in UI config
func UpdateLocalInferenceONNXModelPath(path string) error {
	config, configPath, err := loadOrInitializeUIConfig()
	if err != nil {
		return errors.Wrap(err, "failed to load UI config")
	}

	// Get or create local_inference section
	var localInference map[string]interface{}
	if li, ok := config["local_inference"].(map[string]interface{}); ok {
		localInference = li
	} else {
		localInference = make(map[string]interface{})
	}

	// Update onnx_model_path field
	localInference["onnx_model_path"] = path
	config["local_inference"] = localInference

	return saveUIConfig(config, configPath)
}

// UpdatePulseDailyBudget updates the daily budget in UI config
func UpdatePulseDailyBudget(dailyBudget float64) error {
	config, configPath, err := loadOrInitializeUIConfig()
	if err != nil {
		return errors.Wrap(err, "failed to load UI config")
	}

	// Get or create pulse section
	var pulse map[string]interface{}
	if p, ok := config["pulse"].(map[string]interface{}); ok {
		pulse = p
	} else {
		pulse = make(map[string]interface{})
	}

	// Update daily_budget_usd field
	pulse["daily_budget_usd"] = dailyBudget
	config["pulse"] = pulse

	return saveUIConfig(config, configPath)
}

// UpdatePulseMonthlyBudget updates the monthly budget in UI config
func UpdatePulseMonthlyBudget(monthlyBudget float64) error {
	config, configPath, err := loadOrInitializeUIConfig()
	if err != nil {
		return errors.Wrap(err, "failed to load UI config")
	}

	// Get or create pulse section
	var pulse map[string]interface{}
	if p, ok := config["pulse"].(map[string]interface{}); ok {
		pulse = p
	} else {
		pulse = make(map[string]interface{})
	}

	// Update monthly_budget_usd field
	pulse["monthly_budget_usd"] = monthlyBudget
	config["pulse"] = pulse

	return saveUIConfig(config, configPath)
}
