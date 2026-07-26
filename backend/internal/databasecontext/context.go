package databasecontext

import (
	"context"
	"strings"
)

type accessContextKey struct{}

type AccessContext struct {
	ActorUserID string
	Scope       string
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
	return context.WithValue(ctx, accessContextKey{}, current)
}

func WithScope(ctx context.Context, scope string) context.Context {
	current := FromContext(ctx)
	current.ActorUserID = ""
	switch strings.TrimSpace(scope) {
	case ScopePublic, ScopeProvider, ScopeWorker:
		current.Scope = strings.TrimSpace(scope)
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
