package schedule

import (
	"time"

	"github.com/google/uuid"
)

// Clock supplies the current time. Schedule components use it so acceptance
// tests can advance time without sleeping.
type Clock interface {
	Now() time.Time
}

// IDGenerator supplies schedule, execution, and session IDs.
type IDGenerator interface {
	New() string
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now().UTC() }

type uuidGenerator struct{}

func (uuidGenerator) New() string { return uuid.New().String() }
