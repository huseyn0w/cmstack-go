// Package health implements the canonical thin-handler vertical slice: all
// logic lives in the service; the handler is a pure HTTP boundary.
package health

import "context"

// Pinger is the dependency the readiness check relies on. The pgx pool
// satisfies it; tests provide a fake.
type Pinger interface {
	Ping(ctx context.Context) error
}

// Status is the result of a health probe.
type Status struct {
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

// StatusOK / StatusUnavailable are the two terminal statuses.
const (
	StatusOK          = "ok"
	StatusUnavailable = "unavailable"
)

// Service holds health-check logic. It is constructed with its dependencies and
// has no global state.
type Service struct {
	db Pinger
}

// NewService constructs a Service. db may be nil; readiness then reports
// unavailable rather than panicking, which is the correct behaviour before the
// pool is wired.
func NewService(db Pinger) *Service {
	return &Service{db: db}
}

// Live reports liveness: the process is up and able to serve. It never depends
// on external systems.
func (s *Service) Live() Status {
	return Status{Status: StatusOK}
}

// Ready reports readiness: the process can serve traffic, which requires a
// healthy database connection.
func (s *Service) Ready(ctx context.Context) Status {
	if s.db == nil {
		return Status{Status: StatusUnavailable, Detail: "database not configured"}
	}
	if err := s.db.Ping(ctx); err != nil {
		return Status{Status: StatusUnavailable, Detail: "database unreachable"}
	}
	return Status{Status: StatusOK}
}
