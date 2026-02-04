# Client State Storage

Canvas state migrates from localStorage to IndexedDB because 5-10MB limits prevent large canvases with code/results.
Client-primary storage: IndexedDB owns state locally, server backup planned for cross-device access.
Feature blocks entirely when IndexedDB unavailable (private browsing) to prevent confusion about missing state.
Fresh start on migration: no localStorage import, users rebuild canvases to avoid schema conflicts during transition.
