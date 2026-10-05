package auth

import (
	"context"

	"github.com/teranos/QNTX/internal/access"
	"github.com/teranos/QNTX/internal/admission"
)

// Namespaces, Level and its rungs live in internal/access, which storage
// can import without importing the server.
const (
	NamespaceSystem  = access.NamespaceSystem
	NamespaceDefault = access.NamespaceDefault
)

type Level = access.Level

const (
	LevelSuper              = access.LevelSuper
	LevelRoot               = access.LevelRoot
	LevelToken              = access.LevelToken
	LevelAttestor           = access.LevelAttestor
	LevelPublicRegistration = access.LevelPublicRegistration
	LevelOAuth              = access.LevelOAuth
	LevelRefresh            = access.LevelRefresh
	LevelGitHub             = access.LevelGitHub
)

// What a request was granted lives in internal/admission, which handlers and
// plugins read without importing the server.
type (
	Admission = admission.Admission
	Words     = admission.Words
)

// Admitted builds one (admission.Admitted).
func Admitted(level Level, namespaces ...string) Admission {
	return admission.Admitted(level, namespaces...)
}

// Holding is an admission with roles on it, for tests (admission.Holding).
func Holding(a Admission, roles ...string) Admission { return admission.Holding(a, roles...) }

// Saying is an admission with words on it, for tests (admission.Saying).
func Saying(a Admission, words Words) Admission { return admission.Saying(a, words) }

// WithAdmissionSink puts a slot in the context for the admission (admission.WithAdmissionSink).
func WithAdmissionSink(ctx context.Context) (context.Context, *Admission) {
	return admission.WithAdmissionSink(ctx)
}

// WithAdmission returns a context carrying the admission (admission.WithAdmission).
func WithAdmission(ctx context.Context, a Admission) context.Context {
	return admission.WithAdmission(ctx, a)
}

// AdmissionFrom returns what the request was granted (admission.AdmissionFrom).
func AdmissionFrom(ctx context.Context) (Admission, bool) { return admission.AdmissionFrom(ctx) }
