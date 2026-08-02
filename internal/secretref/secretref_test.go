package secretref

import (
	"context"
	"strings"
	"testing"
)

func TestValidateRejectsLiterals(t *testing.T) {
	literals := []string{
		"ghp_abcdefghijklmnopqrstuvwxyz0123456789",
		"github_pat_11ABCDEFG",
		"not-a-reference",
		"https://github.com/token",
		"ssm:",
		"ssm://",
		"env:",
	}

	for _, ref := range literals {
		t.Run(ref, func(t *testing.T) {
			if err := Validate(ref); err == nil {
				t.Errorf("Validate(%q) accepted a value that is not a resolvable reference", ref)
			}
		})
	}
}

func TestValidateAcceptsReferences(t *testing.T) {
	refs := []string{
		"",
		"ssm:///q/box/github-token",
		"env:GITHUB_TOKEN",
	}

	for _, ref := range refs {
		t.Run(ref, func(t *testing.T) {
			if err := Validate(ref); err != nil {
				t.Errorf("Validate(%q) = %v, want nil", ref, err)
			}
		})
	}
}

// A rejected literal must not be echoed back — the error travels to logs, and
// the whole point is that the value may itself be the secret.
func TestValidateDoesNotEchoTheValue(t *testing.T) {
	const secret = "ghp_thisMustNeverAppearInAnError"

	err := Validate(secret)
	if err == nil {
		t.Fatal("Validate accepted a literal token")
	}
	if strings.Contains(err.Error(), secret) {
		t.Errorf("Validate error leaked the value it rejected: %v", err)
	}
}

func TestResolveEnv(t *testing.T) {
	t.Setenv("QNTX_TEST_TOKEN", "resolved-value")

	got, err := Resolve(context.Background(), "env:QNTX_TEST_TOKEN")
	if err != nil {
		t.Fatalf("Resolve = %v, want nil", err)
	}
	if got != "resolved-value" {
		t.Errorf("Resolve = %q, want %q", got, "resolved-value")
	}
}

func TestResolveEnvUnsetIsAnError(t *testing.T) {
	t.Setenv("QNTX_TEST_TOKEN", "")

	if _, err := Resolve(context.Background(), "env:QNTX_TEST_TOKEN"); err == nil {
		t.Error("Resolve accepted an unset variable, want an error naming it")
	} else if !strings.Contains(err.Error(), "QNTX_TEST_TOKEN") {
		t.Errorf("Resolve error does not name the variable: %v", err)
	}
}

// No reference means no credential, which is correct for a public repo.
func TestResolveEmptyIsNotAnError(t *testing.T) {
	got, err := Resolve(context.Background(), "")
	if err != nil {
		t.Fatalf("Resolve(\"\") = %v, want nil", err)
	}
	if got != "" {
		t.Errorf("Resolve(\"\") = %q, want \"\"", got)
	}
}

// Plugin config is resolved where it asks to be and passed through otherwise,
// so a hostname must not be mistaken for a reference and an empty value must
// not be mistaken for one either.
func TestIsReference(t *testing.T) {
	references := []string{
		"ssm:///path/to/parameter",
		"env:SOME_TOKEN",
	}
	for _, value := range references {
		if !IsReference(value) {
			t.Errorf("IsReference(%q) = false, want true", value)
		}
	}

	literals := []string{
		"",
		"imap.example.com",
		"user@example.com",
		"993",
		"false",
		"a-password-that-mentions-ssm-and-env",
	}
	for _, value := range literals {
		if IsReference(value) {
			t.Errorf("IsReference(%q) = true, want false", value)
		}
	}
}
