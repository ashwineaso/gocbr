package gocbr

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNewCircuitBreaker(t *testing.T) {
	breakerName := "random-service"

	// Add a new circuit breaker to the store
	breakerSettings := Config{
		Name:        breakerName,
		MaxRequests: 6,
		Interval:    time.Second * 1,
		Timeout:     time.Second * 1,
		ReadyToTrip: func(counts Counts) bool {
			return counts.ConsecutiveFailures >= 3
		},
		OnStateChange: func(name string, from State, to State) {
			fmt.Printf("Serivce: %s: Changed from %d -> %d\n", name, from, to)
		},
	}

	breaker := NewCircuitBreaker(breakerSettings)

	// Trigger the circuit to close
	for i := 0; i < 3; i++ {
		err := breaker.BeforeRequest()
		assert.NoError(t, err)
		breaker.OnFailure()
		time.Sleep(time.Millisecond * 100)
	}
	// Check if the circuit is open
	assert.Equalf(t, StateOpen, breaker.State(), "Circuit should be open")

	// Wait for the circuit to close
	time.Sleep(time.Second * 1)
	// Check if the circuit to be half-open
	assert.Equalf(t, StateHalfOpen, breaker.State(), "Circuit should be half open")

	// Trigger the circuit to close
	for i := 0; i < 6; i++ {
		err := breaker.BeforeRequest()
		assert.NoError(t, err)
		breaker.OnSuccess()
		time.Sleep(time.Millisecond * 100)
	}
	// Check if the circuit is closed
	assert.Equalf(t, StateClosed, breaker.State(), "Circuit should be closed")
}
