package databasecontext

import (
	"context"
	"testing"
)

func TestAccessContextKeepsActorAndSystemScopesMutuallyExclusive(t *testing.T) {
	actorContext := WithActor(context.Background(), " actor-1 ")
	actor := FromContext(actorContext)
	if actor.ActorUserID != "actor-1" || actor.Scope != "" || actor.SystemSalonID != "" {
		t.Fatalf("actor context = %#v", actor)
	}

	worker := FromContext(WithScope(actorContext, ScopeWorker))
	if worker.ActorUserID != "" || worker.Scope != ScopeWorker || worker.SystemSalonID != "" {
		t.Fatalf("worker context = %#v", worker)
	}

	boundWorker := FromContext(WithSystemSalon(actorContext, ScopeWorker, " salon-1 "))
	if boundWorker.ActorUserID != "" || boundWorker.Scope != ScopeWorker || boundWorker.SystemSalonID != "salon-1" {
		t.Fatalf("bound worker context = %#v", boundWorker)
	}
	emptyBoundWorker := FromContext(WithSystemSalon(actorContext, ScopeWorker, " "))
	if emptyBoundWorker != (AccessContext{}) {
		t.Fatalf("empty bound worker context = %#v, want fail-closed empty context", emptyBoundWorker)
	}

	invalid := FromContext(WithScope(context.Background(), "caller-controlled"))
	if invalid.ActorUserID != "" || invalid.Scope != "" || invalid.SystemSalonID != "" {
		t.Fatalf("invalid scope was retained: %#v", invalid)
	}

	invalidBound := FromContext(WithSystemSalon(context.Background(), ScopePublic, "salon-1"))
	if invalidBound.ActorUserID != "" || invalidBound.Scope != "" || invalidBound.SystemSalonID != "" {
		t.Fatalf("invalid bound system context was retained: %#v", invalidBound)
	}
}
