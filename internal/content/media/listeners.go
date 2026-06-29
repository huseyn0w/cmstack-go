package media

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/huseyn0w/cmstack-go/internal/platform/events"
)

// UploadListener consumes the async media.uploaded events drained from the
// outbox. Its side effects (CDN warm, search/index, virus-scan handoff) are
// SEAMS for now: the listener is the honest async handler the relay invokes
// after commit, logging the intent so the wiring is real while backends are
// stubs. Register it on BOTH the server bus (so the event is marked async +
// enqueued in-tx) and the worker bus (so the relay dispatches it).
type UploadListener struct {
	log *slog.Logger
}

// NewUploadListener constructs the listener.
func NewUploadListener(log *slog.Logger) *UploadListener {
	return &UploadListener{log: log}
}

// Register subscribes the async media.uploaded handler.
func (l *UploadListener) Register(bus *events.Bus) {
	bus.SubscribeAsyncHandler(EventMediaUploaded, l.onUploaded)
}

func (l *UploadListener) onUploaded(_ context.Context, payload []byte) error {
	var ev UploadedEvent
	if err := json.Unmarshal(payload, &ev); err != nil {
		return fmt.Errorf("unmarshal %s: %w", EventMediaUploaded, err)
	}
	if l.log != nil {
		l.log.Debug("media uploaded seam (no-op)",
			"media_id", ev.MediaID.String(), "mime", ev.MIME, "size", ev.SizeBytes)
	}
	return nil
}
