package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/errors"
	id "github.com/teranos/vanity-id"
)

// HandlerCmd represents the handler command
var HandlerCmd = &cobra.Command{
	Use:   "handler",
	Short: "Manage Python handler scripts",
	Long: `Manage Python handler scripts for the ix command.

Handlers are Python scripts that can be invoked via "ix <handler_name> <args>".
Each handler is stored as an attestation and discovered by the Python plugin.`,
}

var handlerCreateCmd = &cobra.Command{
	Use:   "create NAME",
	Short: "Create a new Python handler",
	Long: `Create a new Python handler script.

The handler will be stored as an attestation with self-certifying pattern:
  Subject: handler_name
  Predicate: "handler"
  Context: "python"
  Actor: handler_name (self-certifying)

This allows unlimited handlers with version history per handler.

Example:
  qntx handler create vacancies --code "print('Hello from vacancies')"
  qntx handler create vacancies --file handler.py`,
	Args: cobra.ExactArgs(1),
	RunE: runHandlerCreate,
}

var (
	handlerCode string
	handlerFile string
)

func init() {
	handlerCreateCmd.Flags().StringVar(&handlerCode, "code", "", "Python code as string")
	handlerCreateCmd.Flags().StringVar(&handlerFile, "file", "", "Path to Python file")

	HandlerCmd.AddCommand(handlerCreateCmd)
}

func runHandlerCreate(cmd *cobra.Command, args []string) error {
	handlerName := args[0]

	// Validate that either code or file is provided
	if handlerCode == "" && handlerFile == "" {
		return errors.New("either --code or --file must be provided")
	}
	if handlerCode != "" && handlerFile != "" {
		return errors.New("cannot specify both --code and --file")
	}

	// Read code from file if specified
	var code string
	if handlerFile != "" {
		fileBytes, err := os.ReadFile(handlerFile)
		if err != nil {
			return errors.Wrapf(err, "failed to read file %s", handlerFile)
		}
		code = string(fileBytes)
	} else {
		code = handlerCode
	}

	// Open database
	database, err := openDatabase("")
	if err != nil {
		return errors.Wrap(err, "failed to open database")
	}
	defer database.Close()

	// Create bounded store
	boundedStore := storage.NewBoundedStoreWithConfig(
		database,
		nil, // logger
		storage.DefaultBoundedStoreConfig(),
	)

	// Generate ASID with self-certifying pattern
	// Actor is empty string to create self-certifying ASID
	asid, err := id.GenerateASID(handlerName, "handler", "python", "")
	if err != nil {
		return errors.Wrap(err, "failed to generate ASID")
	}

	// Create attestation with self-certifying pattern
	attestation := &types.As{
		ID:         asid,
		Subjects:   []string{handlerName},
		Predicates: []string{"handler"},
		Contexts:   []string{"python"},
		Actors:     []string{handlerName}, // Self-certifying: handler is its own actor
		Source:     "qntx-cli",
		Attributes: map[string]interface{}{
			"code": code,
		},
	}

	// Store attestation
	err = boundedStore.CreateAttestation(context.Background(), attestation)
	if err != nil {
		return errors.Wrap(err, "failed to create handler attestation")
	}

	// Pretty print the attributes for confirmation
	attrsJSON, _ := json.MarshalIndent(attestation.Attributes, "", "  ")

	fmt.Printf("✓ Handler '%s' created\n", handlerName)
	fmt.Printf("  ASID: %s\n", asid)
	fmt.Printf("  Subject: %v\n", attestation.Subjects)
	fmt.Printf("  Predicate: %v\n", attestation.Predicates)
	fmt.Printf("  Context: %v\n", attestation.Contexts)
	fmt.Printf("  Actor: %v (self-certifying)\n", attestation.Actors)
	fmt.Printf("  Attributes:\n%s\n", string(attrsJSON))

	return nil
}
