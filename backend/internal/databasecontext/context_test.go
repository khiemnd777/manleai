package databasecontext

import (
	"context"
	"testing"
)

func TestAccessContextKeepsActorAndSystemScopesMutuallyExclusive(t *testing.T) {
	actorContext := WithActor(context.Background(), " actor-1 ")
	actor := FromContext(actorContext)
	if actor.ActorUserID != "actor-1" || actor.Scope != "" {
		t.Fatalf("actor context = %#v", actor)
	}

	worker := FromContext(WithScope(actorContext, ScopeWorker))
	if worker.ActorUserID != "" || worker.Scope != ScopeWorker {
		t.Fatalf("worker context = %#v", worker)
	}

	invalid := FromContext(WithScope(context.Background(), "caller-controlled"))
	if invalid.ActorUserID != "" || invalid.Scope != "" {
		t.Fatalf("invalid scope was retained: %#v", invalid)
	}
}
