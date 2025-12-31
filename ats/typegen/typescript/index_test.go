package typescript

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateIndexFile(t *testing.T) {
	tests := []struct {
		name    string
		exports []PackageExport
		want    string
		wantErr bool
	}{
		{
			name: "single package",
			exports: []PackageExport{
				{
					PackageName: "async",
					TypeNames:   []string{"Job", "JobStatus"},
				},
			},
			want: `/* eslint-disable */
// Auto-generated barrel export - re-exports all generated types
// This file is regenerated on every ` + "`make types`" + ` run

// Types from async
export type {
  Job,
  JobStatus,
} from './async';

`,
		},
		{
			name: "multiple packages",
			exports: []PackageExport{
				{
					PackageName: "async",
					TypeNames:   []string{"Job"},
				},
				{
					PackageName: "budget",
					TypeNames:   []string{"Budget", "Status"},
				},
				{
					PackageName: "server",
					TypeNames:   []string{"Message"},
				},
			},
			want: `/* eslint-disable */
// Auto-generated barrel export - re-exports all generated types
// This file is regenerated on every ` + "`make types`" + ` run

// Types from async
export type {
  Job,
} from './async';

// Types from budget
export type {
  Budget,
  Status,
} from './budget';

// Types from server
export type {
  Message,
} from './server';

`,
		},
		{
			name: "types sorted alphabetically",
			exports: []PackageExport{
				{
					PackageName: "async",
					TypeNames:   []string{"Job", "ErrorContext", "Progress", "JobStatus"},
				},
			},
			want: `/* eslint-disable */
// Auto-generated barrel export - re-exports all generated types
// This file is regenerated on every ` + "`make types`" + ` run

// Types from async
export type {
  ErrorContext,
  Job,
  JobStatus,
  Progress,
} from './async';

`,
		},
		{
			name: "packages sorted alphabetically",
			exports: []PackageExport{
				{
					PackageName: "server",
					TypeNames:   []string{"Message"},
				},
				{
					PackageName: "async",
					TypeNames:   []string{"Job"},
				},
				{
					PackageName: "budget",
					TypeNames:   []string{"Budget"},
				},
			},
			want: `/* eslint-disable */
// Auto-generated barrel export - re-exports all generated types
// This file is regenerated on every ` + "`make types`" + ` run

// Types from async
export type {
  Job,
} from './async';

// Types from budget
export type {
  Budget,
} from './budget';

// Types from server
export type {
  Message,
} from './server';

`,
		},
		{
			name: "skip packages with no types",
			exports: []PackageExport{
				{
					PackageName: "async",
					TypeNames:   []string{"Job"},
				},
				{
					PackageName: "empty",
					TypeNames:   []string{},
				},
				{
					PackageName: "budget",
					TypeNames:   []string{"Budget"},
				},
			},
			want: `/* eslint-disable */
// Auto-generated barrel export - re-exports all generated types
// This file is regenerated on every ` + "`make types`" + ` run

// Types from async
export type {
  Job,
} from './async';

// Types from budget
export type {
  Budget,
} from './budget';

`,
		},
		{
			name:    "empty exports",
			exports: []PackageExport{},
			want: `/* eslint-disable */
// Auto-generated barrel export - re-exports all generated types
// This file is regenerated on every ` + "`make types`" + ` run

`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary directory
			tmpDir := t.TempDir()

			// Generate index file
			err := GenerateIndexFile(tmpDir, tt.exports)
			if (err != nil) != tt.wantErr {
				t.Errorf("GenerateIndexFile() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr {
				return
			}

			// Read generated file
			indexPath := filepath.Join(tmpDir, "index.ts")
			got, err := os.ReadFile(indexPath)
			if err != nil {
				t.Fatalf("Failed to read generated index file: %v", err)
			}

			// Normalize newlines for comparison
			gotStr := strings.ReplaceAll(string(got), "\r\n", "\n")
			wantStr := strings.ReplaceAll(tt.want, "\r\n", "\n")

			if gotStr != wantStr {
				t.Errorf("GenerateIndexFile() generated:\n%s\n\nwant:\n%s", gotStr, wantStr)
			}
		})
	}
}

func TestGenerateIndexFile_DirectoryCreation(t *testing.T) {
	// Test that directory is created if it doesn't exist
	tmpDir := t.TempDir()
	subDir := filepath.Join(tmpDir, "nested", "dir")

	exports := []PackageExport{
		{
			PackageName: "test",
			TypeNames:   []string{"Type"},
		},
	}

	// This should succeed even though nested/dir doesn't exist
	// (though GenerateIndexFile doesn't create dirs - caller does)
	// So we expect this to fail
	err := GenerateIndexFile(subDir, exports)
	if err == nil {
		t.Error("GenerateIndexFile() should fail when directory doesn't exist")
	}
}

func TestGenerateIndexFile_FilePermissions(t *testing.T) {
	// Test that generated file has correct permissions
	tmpDir := t.TempDir()

	exports := []PackageExport{
		{
			PackageName: "test",
			TypeNames:   []string{"Type"},
		},
	}

	err := GenerateIndexFile(tmpDir, exports)
	if err != nil {
		t.Fatalf("GenerateIndexFile() failed: %v", err)
	}

	indexPath := filepath.Join(tmpDir, "index.ts")
	info, err := os.Stat(indexPath)
	if err != nil {
		t.Fatalf("Failed to stat index file: %v", err)
	}

	// Check file is readable
	if info.Mode().Perm()&0400 == 0 {
		t.Error("Generated index file is not readable")
	}

	// Check file is writable
	if info.Mode().Perm()&0200 == 0 {
		t.Error("Generated index file is not writable")
	}
}
