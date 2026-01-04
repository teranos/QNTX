// Package ast provides AST-based code transformation functionality.
// Transformations are semantic changes to code structure, preserving formatting.
package ast

import (
	"encoding/json"
	"fmt"
)

// TransformationType represents the kind of AST transformation
type TransformationType string

const (
	// Simple transformations (high success rate)
	TransformReplaceLiteral  TransformationType = "replace_literal"  // Change literal values
	TransformRenameVariable  TransformationType = "rename_variable"  // Rename variables
	TransformExtractConstant TransformationType = "extract_constant" // Extract magic numbers

	// Medium complexity transformations
	TransformInlineVariable    TransformationType = "inline_variable"    // Remove unnecessary variables
	TransformSimplifyCondition TransformationType = "simplify_condition" // Refactor complex conditions
	TransformAddErrorCheck     TransformationType = "add_error_check"    // Add error handling
	TransformOptimizeLoop      TransformationType = "optimize_loop"      // Optimize loop patterns
)

// ASTTransformation represents a semantic code transformation.
// This is the core type that AI generates and we apply.
// See ast_apply.go for the implementation of each transformation type.
type ASTTransformation struct {
	Type        TransformationType     `json:"type"`                 // Type of transformation
	Target      map[string]interface{} `json:"target"`               // Where to find the code
	Replacement map[string]interface{} `json:"replacement"`          // What to change to
	Reasoning   string                 `json:"reasoning"`            // Why this improves code
	Confidence  float64                `json:"confidence"`           // AI confidence (0.0-1.0)
	StartLine   int                    `json:"start_line,omitempty"` // Optional: for human reference
	EndLine     int                    `json:"end_line,omitempty"`   // Optional: for human reference
}

// Validate checks if the transformation has all required fields
func (t *ASTTransformation) Validate() error {
	if t.Type == "" {
		return fmt.Errorf("transformation type is required")
	}

	if t.Target == nil || len(t.Target) == 0 {
		return fmt.Errorf("transformation target is required")
	}

	if t.Replacement == nil || len(t.Replacement) == 0 {
		return fmt.Errorf("transformation replacement is required")
	}

	if t.Confidence < 0.0 || t.Confidence > 1.0 {
		return fmt.Errorf("confidence must be between 0.0 and 1.0, got %.2f", t.Confidence)
	}

	// Type-specific validation
	switch t.Type {
	case TransformReplaceLiteral:
		if _, ok := t.Target["function"]; !ok {
			return fmt.Errorf("replace_literal requires 'function' in target")
		}
		if _, ok := t.Target["value"]; !ok {
			return fmt.Errorf("replace_literal requires 'value' in target")
		}
		if _, ok := t.Replacement["value"]; !ok {
			return fmt.Errorf("replace_literal requires 'value' in replacement")
		}

	case TransformRenameVariable:
		if _, ok := t.Target["function"]; !ok {
			return fmt.Errorf("rename_variable requires 'function' in target")
		}
		_, hasOldName := t.Target["old_name"]
		_, hasVariable := t.Target["variable"]
		if !hasOldName && !hasVariable {
			return fmt.Errorf("rename_variable requires 'old_name' or 'variable' in target")
		}
		if _, ok := t.Replacement["new_name"]; !ok {
			return fmt.Errorf("rename_variable requires 'new_name' in replacement")
		}

	case TransformExtractConstant:
		if _, ok := t.Target["function"]; !ok {
			return fmt.Errorf("extract_constant requires 'function' in target")
		}
		if _, ok := t.Target["value"]; !ok {
			return fmt.Errorf("extract_constant requires 'value' in target")
		}
		if _, ok := t.Replacement["const_name"]; !ok {
			return fmt.Errorf("extract_constant requires 'const_name' in replacement")
		}

	case TransformInlineVariable:
		if _, ok := t.Target["function"]; !ok {
			return fmt.Errorf("inline_variable requires 'function' in target")
		}
		if _, ok := t.Target["variable"]; !ok {
			return fmt.Errorf("inline_variable requires 'variable' in target")
		}

	case TransformSimplifyCondition:
		if _, ok := t.Target["function"]; !ok {
			return fmt.Errorf("simplify_condition requires 'function' in target")
		}
		if _, ok := t.Target["condition"]; !ok {
			return fmt.Errorf("simplify_condition requires 'condition' in target")
		}
		if _, ok := t.Replacement["simplified"]; !ok {
			return fmt.Errorf("simplify_condition requires 'simplified' in replacement")
		}

	case TransformAddErrorCheck:
		if _, ok := t.Target["function"]; !ok {
			return fmt.Errorf("add_error_check requires 'function' in target")
		}
		if _, ok := t.Target["call"]; !ok {
			return fmt.Errorf("add_error_check requires 'call' in target")
		}

	case TransformOptimizeLoop:
		if _, ok := t.Target["function"]; !ok {
			return fmt.Errorf("optimize_loop requires 'function' in target")
		}
		if _, ok := t.Replacement["optimization_type"]; !ok {
			return fmt.Errorf("optimize_loop requires 'optimization_type' in replacement")
		}
	}

	return nil
}

// GetString safely gets a string field from a map
func (t *ASTTransformation) GetString(m map[string]interface{}, key string) string {
	if val, ok := m[key]; ok {
		if str, ok := val.(string); ok {
			return str
		}
	}
	return ""
}

// GetTargetFunction returns the target function name
func (t *ASTTransformation) GetTargetFunction() string {
	return t.GetString(t.Target, "function")
}

// GetTargetValue returns the target value to find
func (t *ASTTransformation) GetTargetValue() string {
	return t.GetString(t.Target, "value")
}

// GetTargetOldName returns the old variable name
func (t *ASTTransformation) GetTargetOldName() string {
	oldName := t.GetString(t.Target, "old_name")
	if oldName == "" {
		oldName = t.GetString(t.Target, "variable")
	}
	return oldName
}

// GetReplacementValue returns the replacement value
func (t *ASTTransformation) GetReplacementValue() string {
	return t.GetString(t.Replacement, "value")
}

// GetReplacementNewName returns the new variable name
func (t *ASTTransformation) GetReplacementNewName() string {
	return t.GetString(t.Replacement, "new_name")
}

// GetReplacementConstName returns the constant name for extraction
func (t *ASTTransformation) GetReplacementConstName() string {
	return t.GetString(t.Replacement, "const_name")
}

// GetTargetVarName returns the variable name for inline_variable
func (t *ASTTransformation) GetTargetVarName() string {
	return t.GetString(t.Target, "variable")
}

// GetTargetCondition returns the condition expression to simplify
func (t *ASTTransformation) GetTargetCondition() string {
	return t.GetString(t.Target, "condition")
}

// GetReplacementSimplified returns the simplified condition expression
func (t *ASTTransformation) GetReplacementSimplified() string {
	return t.GetString(t.Replacement, "simplified")
}

// GetTargetCallName returns the function call name for add_error_check
func (t *ASTTransformation) GetTargetCallName() string {
	return t.GetString(t.Target, "call")
}

// GetReplacementOptimizationType returns the loop optimization type
func (t *ASTTransformation) GetReplacementOptimizationType() string {
	return t.GetString(t.Replacement, "optimization_type")
}

// String returns a human-readable representation
func (t *ASTTransformation) String() string {
	return fmt.Sprintf("%s: %s (%.0f%% confidence)",
		t.Type, t.Reasoning, t.Confidence*100)
}

// MarshalJSON customizes JSON output
func (t *ASTTransformation) MarshalJSON() ([]byte, error) {
	type Alias ASTTransformation
	return json.Marshal(&struct {
		*Alias
		ConfidencePercent int `json:"confidence_percent,omitempty"`
	}{
		Alias:             (*Alias)(t),
		ConfidencePercent: int(t.Confidence * 100),
	})
}

// TransformationSet represents a collection of transformations
type TransformationSet struct {
	Transformations []*ASTTransformation
	FilePath        string
}

// HighConfidence returns transformations with confidence >= threshold
func (ts *TransformationSet) HighConfidence(threshold float64) []*ASTTransformation {
	var result []*ASTTransformation
	for _, t := range ts.Transformations {
		if t.Confidence >= threshold {
			result = append(result, t)
		}
	}
	return result
}

// ByType returns transformations of a specific type
func (ts *TransformationSet) ByType(typ TransformationType) []*ASTTransformation {
	var result []*ASTTransformation
	for _, t := range ts.Transformations {
		if t.Type == typ {
			result = append(result, t)
		}
	}
	return result
}

// Validate validates all transformations in the set
func (ts *TransformationSet) Validate() error {
	for i, t := range ts.Transformations {
		if err := t.Validate(); err != nil {
			return fmt.Errorf("transformation %d: %w", i, err)
		}
	}
	return nil
}
