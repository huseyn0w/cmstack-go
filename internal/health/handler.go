package health

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/huseyn0w/agentic-cms-go/internal/platform/httpx"
)

// Handler is the thin HTTP boundary for health checks. It contains ZERO logic:
// it calls the service and encodes the result. This is the reference pattern
// every feature handler in Agentic CMS-Go copies.
type Handler struct {
	svc *Service
}

// NewHandler constructs a Handler.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// Routes mounts the health endpoints onto a chi router.
func (h *Handler) Routes(r chi.Router) {
	r.Get("/health", h.live)
	r.Get("/health/ready", h.ready)
}

func (h *Handler) live(w http.ResponseWriter, _ *http.Request) {
	httpx.JSON(w, http.StatusOK, h.svc.Live())
}

func (h *Handler) ready(w http.ResponseWriter, r *http.Request) {
	status := h.svc.Ready(r.Context())
	code := http.StatusOK
	if status.Status != StatusOK {
		code = http.StatusServiceUnavailable
	}
	httpx.JSON(w, code, status)
}
