package shared

import "context"

type ActorType string

const (
	ActorAdmin  ActorType = "ADMIN"
	ActorTenant ActorType = "TENANT"
	ActorSystem ActorType = "SYSTEM" // background jobs, e.g. document retention
)

// Actor is who performed an action. ID is empty for ActorSystem.
type Actor struct {
	Type ActorType
	ID   string
}

type actorKey struct{}

func WithActor(ctx context.Context, a Actor) context.Context {
	return context.WithValue(ctx, actorKey{}, a)
}

func ActorFrom(ctx context.Context) (Actor, bool) {
	a, ok := ctx.Value(actorKey{}).(Actor)
	return a, ok
}
