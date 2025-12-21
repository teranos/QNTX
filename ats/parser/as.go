package parser

import (
	"fmt"
	"strings"
	"time"

	"github.com/sbvh/qntx/ats"
	"github.com/sbvh/qntx/ats/types"
)

// ParserOptions provides optional configuration for the AS command parser
type ParserOptions struct {
	// ActorDetector provides custom actor detection logic.
	// If nil, uses DefaultActorDetector with system username.
	ActorDetector ats.ActorDetector
}

// ParseAsCommand parses CLI arguments into an AsCommand using default options.
// Grammar: qntx as SUBJECTS [is PREDICATES] [of CONTEXTS] [by ACTOR] [on DATE]
func ParseAsCommand(args []string) (*types.AsCommand, error) {
	return ParseAsCommandWithOptions(args, ParserOptions{})
}

// ParseAsCommandWithOptions parses CLI arguments with custom options.
// Grammar: qntx as SUBJECTS [is PREDICATES] [of CONTEXTS] [by ACTOR] [on DATE]
func ParseAsCommandWithOptions(args []string, opts ParserOptions) (*types.AsCommand, error) {
	// Use default detector if none provided
	detector := opts.ActorDetector
	if detector == nil {
		detector = &ats.DefaultActorDetector{FallbackActor: "unknown"}
	}

	if len(args) == 0 {
		return nil, fmt.Errorf("no arguments provided")
	}

	cmd := &types.AsCommand{
		Subjects:   []string{},
		Predicates: []string{},
		Contexts:   []string{},
		Actors:     []string{},
		Timestamp:  time.Now(),
		Attributes: make(map[string]interface{}),
	}

	// Tokenize with quote handling
	tokens := tokenizeWithQuotes(args)
	if len(tokens) == 0 {
		return nil, fmt.Errorf("no valid tokens found")
	}

	// Parse tokens
	err := parseAsTokens(tokens, cmd)
	if err != nil {
		return nil, err
	}

	// Always add LLM actor when in LLM environment (if detector provides one)
	llmActor := detector.GetLLMActor()
	if llmActor != "" {
		// Add LLM actor if not already present
		found := false
		for _, actor := range cmd.Actors {
			if actor == llmActor {
				found = true
				break
			}
		}
		if !found {
			cmd.Actors = append(cmd.Actors, llmActor)
		}
	}

	// Set default actor if still no actors provided
	if len(cmd.Actors) == 0 {
		cmd.Actors = []string{detector.GetDefaultActor()}
	}

	// Validate that we have at least subjects
	if len(cmd.Subjects) == 0 {
		return nil, fmt.Errorf("at least one subject is required")
	}

	return cmd, nil
}

// tokenizeWithQuotes handles single-quoted strings
func tokenizeWithQuotes(args []string) []string {
	var tokens []string
	var currentToken strings.Builder
	inQuotes := false

	for _, arg := range args {
		if !inQuotes {
			if strings.HasPrefix(arg, "'") {
				// Starting a quoted string
				inQuotes = true
				if strings.HasSuffix(arg, "'") && len(arg) > 1 {
					// Single word in quotes
					tokens = append(tokens, strings.Trim(arg, "'"))
					inQuotes = false
				} else {
					currentToken.WriteString(strings.TrimPrefix(arg, "'"))
				}
			} else {
				// Regular token
				tokens = append(tokens, arg)
			}
		} else {
			// Inside quotes
			if strings.HasSuffix(arg, "'") {
				// Ending quoted string
				currentToken.WriteString(" ")
				currentToken.WriteString(strings.TrimSuffix(arg, "'"))
				tokens = append(tokens, currentToken.String())
				currentToken.Reset()
				inQuotes = false
			} else {
				// Continue building quoted string
				if currentToken.Len() > 0 {
					currentToken.WriteString(" ")
				}
				currentToken.WriteString(arg)
			}
		}
	}

	// Handle unclosed quotes
	if inQuotes {
		tokens = append(tokens, currentToken.String())
	}

	return tokens
}

// parseAsTokens parses the tokenized input for as command
func parseAsTokens(tokens []string, cmd *types.AsCommand) error {
	i := 0
	state := "subjects" // subjects, predicates, contexts, actors, timestamp

	for i < len(tokens) {
		token := tokens[i]

		switch token {
		case "is", "are":
			state = "predicates"
			i++
			continue
		case "of":
			state = "contexts"
			i++
			continue
		case "by":
			state = "actors"
			i++
			continue
		case "on":
			state = "timestamp"
			i++
			continue
		}

		switch state {
		case "subjects":
			cmd.Subjects = append(cmd.Subjects, strings.ToUpper(token))
		case "predicates":
			cmd.Predicates = append(cmd.Predicates, token)
		case "contexts":
			cmd.Contexts = append(cmd.Contexts, strings.ToUpper(token))
		case "actors":
			cmd.Actors = append(cmd.Actors, token)
		case "timestamp":
			// Parse timestamp (simplified for now)
			parsedTime, err := parseTimeExpression(token)
			if err != nil {
				return fmt.Errorf("invalid timestamp '%s': %w", token, err)
			}
			cmd.Timestamp = parsedTime
			state = "subjects" // Reset to subjects for any remaining tokens
		}

		i++
	}

	// Apply grammar inference ONLY for exactly 3 subjects without keywords
	if len(cmd.Subjects) == 3 && len(cmd.Predicates) == 0 && len(cmd.Contexts) == 0 {
		// Infer: JACK manager IBM -> JACK is manager of IBM
		pred := strings.ToLower(cmd.Subjects[1]) // Keep original case for predicates
		context := cmd.Subjects[2]
		cmd.Subjects = cmd.Subjects[:1] // Keep only first subject
		cmd.Predicates = []string{pred}
		cmd.Contexts = []string{context}
	}

	return nil
}

// parseTimeExpression parses temporal expressions using shared temporal logic
func parseTimeExpression(expr string) (time.Time, error) {
	result, err := ParseTemporalExpression(expr)
	if err != nil {
		return time.Time{}, err
	}
	return *result, nil
}
