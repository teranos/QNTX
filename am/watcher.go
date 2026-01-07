package am

import (
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/teranos/QNTX/logger"
)

// ConfigWatcher watches config files for changes and triggers reload callbacks
type ConfigWatcher struct {
	configPath      string
	watcher         *fsnotify.Watcher
	callbacks       []ReloadCallback
	mu              sync.RWMutex
	debounceTimer   *time.Timer
	debouncePeriod  time.Duration
	isOwnWrite      bool // Flag to prevent reload loops
	isOwnWriteMutex sync.Mutex
}

// ReloadCallback is called when config is reloaded
// Receives the new config and returns any error
type ReloadCallback func(*Config) error

// globalWatcher holds the singleton config watcher instance
var (
	globalWatcher   *ConfigWatcher
	globalWatcherMu sync.Mutex
)

// NewConfigWatcher creates a new config file watcher
func NewConfigWatcher(configPath string) (*ConfigWatcher, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("failed to create fsnotify watcher: %w", err)
	}

	// Watch the config file
	if err := watcher.Add(configPath); err != nil {
		watcher.Close()
		return nil, fmt.Errorf("failed to watch config file %s: %w", configPath, err)
	}

	cw := &ConfigWatcher{
		configPath:     configPath,
		watcher:        watcher,
		callbacks:      make([]ReloadCallback, 0),
		debouncePeriod: 500 * time.Millisecond, // Debounce rapid file changes
	}

	return cw, nil
}

// OnReload registers a callback to be called when config is reloaded
func (cw *ConfigWatcher) OnReload(callback ReloadCallback) {
	cw.mu.Lock()
	defer cw.mu.Unlock()
	cw.callbacks = append(cw.callbacks, callback)
}

// MarkOwnWrite marks the next write as coming from us (prevents reload loops)
func (cw *ConfigWatcher) MarkOwnWrite() {
	cw.isOwnWriteMutex.Lock()
	defer cw.isOwnWriteMutex.Unlock()
	cw.isOwnWrite = true
}

// checkOwnWrite checks and clears the own-write flag
func (cw *ConfigWatcher) checkOwnWrite() bool {
	cw.isOwnWriteMutex.Lock()
	defer cw.isOwnWriteMutex.Unlock()

	if cw.isOwnWrite {
		cw.isOwnWrite = false
		return true
	}
	return false
}

// Start begins watching for config file changes
func (cw *ConfigWatcher) Start() {
	go cw.watchLoop()
}

// watchLoop monitors file system events
func (cw *ConfigWatcher) watchLoop() {
	for {
		select {
		case event, ok := <-cw.watcher.Events:
			if !ok {
				return
			}

			// Only reload on Write or Create events
			if event.Op&fsnotify.Write == fsnotify.Write || event.Op&fsnotify.Create == fsnotify.Create {
				// Ignore backup files
				if isBackupFile(event.Name) {
					continue
				}

				// Check if this is our own write
				if cw.checkOwnWrite() {
					logger.Debugw("Config watcher ignoring own write",
						"file", event.Name)
					continue
				}

				logger.Infow("Config watcher detected change",
					"file", event.Name,
					"op", event.Op.String())
				cw.scheduleReload()
			}

		case err, ok := <-cw.watcher.Errors:
			if !ok {
				return
			}
			logger.Warnw("Config watcher error",
				"error", err)
		}
	}
}

// scheduleReload debounces rapid file changes and triggers reload
func (cw *ConfigWatcher) scheduleReload() {
	cw.mu.Lock()
	defer cw.mu.Unlock()

	// Cancel existing timer if any
	if cw.debounceTimer != nil {
		cw.debounceTimer.Stop()
	}

	// Schedule reload after debounce period
	cw.debounceTimer = time.AfterFunc(cw.debouncePeriod, func() {
		if err := cw.reload(); err != nil {
			logger.Errorw("Config reload failed",
				"error", err)
		}
	})
}

// reload reloads the configuration and calls all callbacks
func (cw *ConfigWatcher) reload() error {
	// Reset global config to force reload
	Reset()

	// Load new config
	newConfig, err := Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	logger.Infow("Config reloaded successfully",
		"path", cw.configPath)

	// Call all registered callbacks
	cw.mu.RLock()
	callbacks := make([]ReloadCallback, len(cw.callbacks))
	copy(callbacks, cw.callbacks)
	cw.mu.RUnlock()

	for _, callback := range callbacks {
		if err := callback(newConfig); err != nil {
			logger.Warnw("Config reload callback error",
				"error", err)
			// Continue calling other callbacks even if one fails
		}
	}

	return nil
}

// Stop stops watching for config changes
func (cw *ConfigWatcher) Stop() error {
	return cw.watcher.Close()
}

// isBackupFile checks if the file is a backup file (.back1, .back2, .back3)
func isBackupFile(path string) bool {
	base := filepath.Base(path)
	return base == "am.toml.back1" ||
		base == "am.toml.back2" ||
		base == "am.toml.back3" ||
		base == "config.toml.back1" ||
		base == "config.toml.back2" ||
		base == "config.toml.back3"
}

// SetGlobalWatcher sets the global watcher instance (used to prevent reload loops)
func SetGlobalWatcher(watcher *ConfigWatcher) {
	globalWatcherMu.Lock()
	defer globalWatcherMu.Unlock()
	globalWatcher = watcher
}

// GetGlobalWatcher returns the global watcher instance
func GetGlobalWatcher() *ConfigWatcher {
	globalWatcherMu.Lock()
	defer globalWatcherMu.Unlock()
	return globalWatcher
}
