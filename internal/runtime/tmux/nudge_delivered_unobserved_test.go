package tmux

import (
	"errors"
	"testing"

	"github.com/gastownhall/gascity/internal/runtime"
)

// Callers above the runtime layer (internal/session) cannot import tmux, so
// they must be able to recognize a delivered-but-unobserved submit through the
// provider-neutral runtime sentinel.
func TestErrNudgeSubmitDeliveredUnobservedIsRuntimeSentinel(t *testing.T) {
	if !errors.Is(ErrNudgeSubmitDeliveredUnobserved, runtime.ErrNudgeDeliveredUnobserved) {
		t.Fatal("ErrNudgeSubmitDeliveredUnobserved does not match runtime.ErrNudgeDeliveredUnobserved")
	}
	if errors.Is(ErrNudgeSubmitUnconfirmed, runtime.ErrNudgeDeliveredUnobserved) {
		t.Fatal("ErrNudgeSubmitUnconfirmed must not match runtime.ErrNudgeDeliveredUnobserved")
	}
}
