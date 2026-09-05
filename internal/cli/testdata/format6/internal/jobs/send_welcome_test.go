package jobs

import (
	"context"
	"testing"
)

func TestSendWelcomeHandler(t *testing.T) {
	handler := SendWelcomeHandler{}
	if err := handler.Handle(context.Background(), SendWelcome{}); err != nil {
		t.Fatal(err)
	}
}
