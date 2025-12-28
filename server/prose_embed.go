package server

import (
	"path/filepath"
	"strings"
)

// proseFilesEmbedded is defined in embed_prod.go (production) or embed_stub.go (testing)

// buildProseTreeFromEmbedded builds prose tree from embedded files (production)
func (s *QNTXServer) buildProseTreeFromEmbedded() ([]ProseEntry, error) {
	return s.buildProseTreeFromEmbeddedFS("docs_embedded", "")
}

// buildProseTreeFromEmbeddedFS recursively builds tree from embedded FS
func (s *QNTXServer) buildProseTreeFromEmbeddedFS(basePath, relPath string) ([]ProseEntry, error) {
	fullPath := filepath.Join(basePath, relPath)

	entries, err := proseFilesEmbedded.ReadDir(fullPath)
	if err != nil {
		return nil, err
	}

	var prose []ProseEntry
	for _, entry := range entries {
		// Skip hidden files
		name := entry.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}

		entryPath := filepath.Join(relPath, name)

		if entry.IsDir() {
			children, err := s.buildProseTreeFromEmbeddedFS(basePath, entryPath)
			if err != nil {
				s.logger.Warnw("Failed to build prose tree for directory",
					"directory", entryPath,
					"error", err)
				continue
			}
			prose = append(prose, ProseEntry{
				Name:     name,
				Path:     entryPath,
				IsDir:    true,
				Children: children,
			})
		} else if strings.HasSuffix(name, ".md") {
			prose = append(prose, ProseEntry{
				Name:  strings.TrimSuffix(name, ".md"),
				Path:  entryPath,
				IsDir: false,
			})
		}
	}

	return prose, nil
}

// readProseFileEmbedded reads a prose file from embedded filesystem (production)
func (s *QNTXServer) readProseFileEmbedded(prosePath string) ([]byte, error) {
	fullPath := filepath.Join("docs_embedded", prosePath)
	return proseFilesEmbedded.ReadFile(fullPath)
}
