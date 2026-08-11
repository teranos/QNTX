// Package secretref resolves references to secrets without ever storing one.
//
// am.toml is delivered as an SSM parameter of type String and is world-readable
// in tofu state. A secret written there is disclosed, not configured. So config
// carries only a reference — a place to go look — and the value is fetched at
// the moment it is used. A literal is rejected rather than accepted with a
// warning, because a warning does not un-publish a token.
package secretref

import (
	"context"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/teranos/errors"
)

const (
	// SchemeSSM reads from AWS Systems Manager Parameter Store using the
	// host's ambient credentials: ssm:///qntx/github-token
	SchemeSSM = "ssm://"

	// SchemeEnv reads from the process environment: env:GITHUB_TOKEN
	SchemeEnv = "env:"
)

// IsReference reports whether value names a secret rather than being one.
//
// Where a field is known to hold a credential, a literal is a mistake and
// Validate rejects it. Plugin configuration is not like that: QNTX cannot know
// which of a plugin's own keys are secret, and a hostname is a literal by
// rights. So values are resolved when they ask to be and passed through
// otherwise.
func IsReference(value string) bool {
	return strings.HasPrefix(value, SchemeSSM) || strings.HasPrefix(value, SchemeEnv)
}

// Validate reports whether ref is a reference rather than a secret.
// An empty ref is valid — it means no credential is configured.
func Validate(ref string) error {
	switch {
	case ref == "":
		return nil

	case strings.HasPrefix(ref, SchemeSSM):
		if strings.TrimPrefix(ref, SchemeSSM) == "" {
			return errors.Newf("%s reference names no parameter", SchemeSSM)
		}
		return nil

	case strings.HasPrefix(ref, SchemeEnv):
		if strings.TrimPrefix(ref, SchemeEnv) == "" {
			return errors.Newf("%s reference names no variable", SchemeEnv)
		}
		return nil
	}

	// Deliberately does not echo ref — it may be the secret itself, and this
	// error reaches logs.
	err := errors.Newf("value is a literal, not a %s or %s reference", SchemeSSM, SchemeEnv)
	return errors.WithHint(err, "am.toml is world-readable; store the secret in SSM and reference it as ssm:///path or env:VAR")
}

// Resolve fetches the secret a reference points at.
// An empty ref resolves to an empty value with no error — no credential.
func Resolve(ctx context.Context, ref string) (string, error) {
	if err := Validate(ref); err != nil {
		return "", err
	}

	switch {
	case ref == "":
		return "", nil

	case strings.HasPrefix(ref, SchemeEnv):
		name := strings.TrimPrefix(ref, SchemeEnv)
		value := os.Getenv(name)
		if value == "" {
			err := errors.Newf("environment variable %s is unset or empty", name)
			return "", errors.WithHintf(err, "export %s before starting QNTX, or point the reference elsewhere", name)
		}
		return value, nil

	default:
		return resolveSSM(ctx, strings.TrimPrefix(ref, SchemeSSM))
	}
}

// resolveSSM reads a parameter through the host's existing credentials.
// WithDecryption covers SecureString parameters, which is what a token should be.
func resolveSSM(ctx context.Context, name string) (string, error) {
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		return "", errors.Wrapf(err, "failed to load AWS credentials to read SSM parameter %s", name)
	}

	out, err := ssm.NewFromConfig(awsCfg).GetParameter(ctx, &ssm.GetParameterInput{
		Name:           aws.String(name),
		WithDecryption: aws.Bool(true),
	})
	if err != nil {
		return "", errors.Wrapf(err, "failed to read SSM parameter %s in region %s", name, awsCfg.Region)
	}

	if out.Parameter == nil || aws.ToString(out.Parameter.Value) == "" {
		return "", errors.Newf("SSM parameter %s is empty", name)
	}

	return aws.ToString(out.Parameter.Value), nil
}
