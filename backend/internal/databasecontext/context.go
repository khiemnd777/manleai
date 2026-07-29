package databasecontext

import (
	"context"
	"strings"
)

type accessContextKey struct{}

type AccessContext struct {
	ActorUserID   string
	Scope         string
	SystemSalonID string
}

const (
	ScopePublic   = "public"
	ScopeProvider = "provider"
	ScopeWorker   = "worker"
)

func WithActor(ctx context.Context, actorUserID string) context.Context {
	current := FromContext(ctx)
	current.ActorUserID = strings.TrimSpace(actorUserID)
	current.Scope = ""
	current.SystemSalonID = ""
	return context.WithValue(ctx, accessContextKey{}, current)
}

func WithScope(ctx context.Context, scope string) context.Context {
	current := FromContext(ctx)
	current.ActorUserID = ""
	current.SystemSalonID = ""
	switch strings.TrimSpace(scope) {
	case ScopePublic, ScopeProvider, ScopeWorker:
		current.Scope = strings.TrimSpace(scope)
	default:
		current.Scope = ""
	}
	return context.WithValue(ctx, accessContextKey{}, current)
}

// WithSystemSalon creates a provider/worker context bound to exactly one
// salon. The scope must be selected by server code; caller input cannot create
// a new database scope.
func WithSystemSalon(ctx context.Context, scope string, salonID string) context.Context {
	current := FromContext(ctx)
	current.ActorUserID = ""
	current.SystemSalonID = ""
	salonID = strings.TrimSpace(salonID)
	if salonID == "" {
		current.Scope = ""
		return context.WithValue(ctx, accessContextKey{}, current)
	}
	switch strings.TrimSpace(scope) {
	case ScopeProvider, ScopeWorker:
		current.Scope = strings.TrimSpace(scope)
		current.SystemSalonID = salonID
	default:
		current.Scope = ""
	}
	return context.WithValue(ctx, accessContextKey{}, current)
}

func FromContext(ctx context.Context) AccessContext {
	if ctx == nil {
		return AccessContext{}
	}
	value, _ := ctx.Value(accessContextKey{}).(AccessContext)
	return value
}
