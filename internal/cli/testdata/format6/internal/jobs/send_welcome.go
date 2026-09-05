package jobs

import (
	"context"

	"github.com/ShanilKoshitha/goforge/job"
)

// SendWelcome is the versioned, JSON-encoded payload for send_welcome.v1.
// Add only durable data needed to locate current application state. Handlers
// are delivered at least once, so make external side effects idempotent.
type SendWelcome struct{}

var SendWelcomeDefinition = job.MustDefine[SendWelcome]("send_welcome.v1", job.Policy{})

type SendWelcomeHandler struct {
	Dependencies Dependencies
}

func (handler SendWelcomeHandler) Handle(ctx context.Context, payload SendWelcome) error {
	// TODO: perform the job. Keep the handler safe to run more than once.
	return nil
}

func DispatchSendWelcome(ctx context.Context, dispatcher job.Dispatcher, payload SendWelcome, options ...job.DispatchOption) (job.DispatchResult, error) {
	return SendWelcomeDefinition.Dispatch(ctx, dispatcher, payload, options...)
}
