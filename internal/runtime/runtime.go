package runtime

import (
	"context"

	"agenthub/pkg/protocol"
)

type Runtime interface {
	RunTurn(ctx context.Context, req protocol.TurnRequest, emit func(protocol.UniversalEvent) error) error
}
