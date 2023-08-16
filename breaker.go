package gocbr

import (
	"errors"
	"sync"
	"time"
)

const (
	defaultInterval = time.Duration(0) * time.Second
	defaultTimeout  = time.Duration(60) * time.Second

	defaultMaxRequests            = uint32(1)
	defaultMaxConsecutiveFailures = uint32(5)
)

// State is a type that represents a state of CircuitBreaker.
type State int

// These constants are states of CircuitBreaker.
const (
	StateClosed State = iota
	StateHalfOpen
	StateOpen
)

var (
	// ErrTooManyRequests is returned when the CB state is half open and the requests count is over the cb maxRequests
	ErrTooManyRequests = errors.New("too many requests")
	// ErrOpenState is returned when the CB state is open
	ErrOpenState = errors.New("circuit breaker is open")
)

// Counts holds the numbers of requests and their successes/failures.
// CircuitBreaker clears the internal Counts either
// on the change of the state or at the closed-state intervals.
// Counts ignores the results of the requests sent before clearing.
type Counts struct {
	Requests             uint32
	TotalSuccesses       uint32
	TotalFailures        uint32
	ConsecutiveSuccesses uint32
	ConsecutiveFailures  uint32
}

// onRequest is called before a request is made through the CircuitBreaker.
func (c *Counts) onRequest() {
	c.Requests++
}

// onSuccess is called when a request through the CircuitBreaker succeeds.
func (c *Counts) onSuccess() {
	c.TotalSuccesses++
	c.ConsecutiveSuccesses++
	c.ConsecutiveFailures = 0
}

// onFailure is called when a request through the CircuitBreaker fails.
func (c *Counts) onFailure() {
	c.TotalFailures++
	c.ConsecutiveFailures++
	c.ConsecutiveSuccesses = 0
}

// clear clears the internal Counts.
func (c *Counts) clear() {
	c.Requests = 0
	c.TotalSuccesses = 0
	c.TotalFailures = 0
	c.ConsecutiveSuccesses = 0
	c.ConsecutiveFailures = 0
}

func defaultReadyToTrip(counts Counts) bool {
	return counts.ConsecutiveFailures > defaultMaxConsecutiveFailures
}

// Config configures CircuitBreaker:
//
// Name is the name of the CircuitBreaker.
//
// MaxRequests is the maximum number of requests allowed to pass through
// when the CircuitBreaker is half-open.
// If MaxRequests is 0, the CircuitBreaker allows only 1 request.
//
// Interval is the cyclic period of the closed state
// for the CircuitBreaker to clear the internal Counts.
// If Interval is less than or equal to 0, the CircuitBreaker doesn't clear internal Counts during the closed state.
//
// Timeout is the period of the open state,
// after which the state of the CircuitBreaker becomes half-open.
// If Timeout is less than or equal to 0, the timeout value of the CircuitBreaker is set to 60 seconds.
//
// ReadyToTrip is called with a copy of Counts whenever a request fails in the closed state.
// If ReadyToTrip returns true, the CircuitBreaker will be placed into the open state.
// If ReadyToTrip is nil, default ReadyToTrip is used.
// Default ReadyToTrip returns true when the number of consecutive failures is more than 5.
//
// OnStateChange is called whenever the state of the CircuitBreaker changes.
type Config struct {
	Name          string
	MaxRequests   uint32
	Interval      time.Duration
	Timeout       time.Duration
	ReadyToTrip   func(counts Counts) bool
	OnStateChange func(name string, from State, to State)
}

type CircuitBreaker struct {
	name        string
	maxRequests uint32
	interval    time.Duration
	timeout     time.Duration

	readyToTrip   func(counts Counts) bool
	onStateChange func(name string, from State, to State)

	mutex      sync.Mutex
	state      State     // defines the state of the circuit breaker
	generation uint64    // monotonically increasing generation number
	counts     Counts    // counts of requests and their successes/failures
	expiry     time.Time // expiry of the current state
}

// NewCircuitBreaker returns a new CircuitBreaker configured with the given Settings.
func NewCircuitBreaker(st Config) *CircuitBreaker {
	cb := new(CircuitBreaker)

	cb.name = st.Name
	cb.onStateChange = st.OnStateChange

	// Set maxRequests to 1 if it is not set
	cb.maxRequests = st.MaxRequests
	if st.MaxRequests == 0 {
		cb.maxRequests = defaultMaxRequests
	}

	// Set interval to defaultInterval if it is not set
	cb.interval = st.Interval
	if st.Interval <= 0 {
		cb.interval = defaultInterval
	}

	// Set timeout to defaultTimeout if it is not set
	cb.timeout = st.Timeout
	if st.Timeout <= 0 {
		cb.timeout = defaultTimeout
	}

	// Set readyToTrip to defaultReadyToTrip if it is not set
	cb.readyToTrip = st.ReadyToTrip
	if st.ReadyToTrip == nil {
		cb.readyToTrip = defaultReadyToTrip
	}

	cb.reset(time.Now())

	return cb
}

// Name returns the name of the TwoStepCircuitBreaker.
func (cb *CircuitBreaker) Name() string {
	return cb.name
}

// State returns the current state of the TwoStepCircuitBreaker.
func (cb *CircuitBreaker) State() State {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()

	now := time.Now()
	state, _ := cb.currentState(now)
	return state
}

// Counts returns internal counters
func (cb *CircuitBreaker) Counts() Counts {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()

	return cb.counts
}

// BeforeRequest - a hook which is called right before the request is sent
func (cb *CircuitBreaker) BeforeRequest() error {
	// Checking if the circuit breaker is in the open state
	_, err := cb.beforeRequest()
	if err != nil {
		return err
	}

	return nil
}

// OnSuccess - a hook which is called when the request is successful
func (cb *CircuitBreaker) OnSuccess() {
	cb.afterRequest(cb.generation, true)
}

// OnFailure - a hook which is called when the request fails
func (cb *CircuitBreaker) OnFailure() {
	cb.afterRequest(cb.generation, false)
}

func (cb *CircuitBreaker) beforeRequest() (uint64, error) {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()

	now := time.Now()
	state, generation := cb.currentState(now)

	if state == StateOpen {
		return generation, ErrOpenState
	} else if state == StateHalfOpen && cb.counts.Requests >= cb.maxRequests {
		return generation, ErrTooManyRequests
	}

	cb.counts.onRequest()
	return generation, nil
}

func (cb *CircuitBreaker) afterRequest(before uint64, success bool) {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()

	now := time.Now()
	state, generation := cb.currentState(now)
	if generation != before {
		return
	}

	if success {
		cb.onSuccess(state, now)
	} else {
		cb.onFailure(state, now)
	}
}

func (cb *CircuitBreaker) onSuccess(state State, now time.Time) {
	switch state {
	case StateClosed:
		cb.counts.onSuccess()
	case StateHalfOpen:
		cb.counts.onSuccess()
		if cb.counts.ConsecutiveSuccesses >= cb.maxRequests {
			cb.setState(StateClosed, now)
		}
	}
}

func (cb *CircuitBreaker) onFailure(state State, now time.Time) {
	switch state {
	case StateClosed:
		cb.counts.onFailure()
		if cb.readyToTrip(cb.counts) {
			cb.setState(StateOpen, now)
		}
	case StateHalfOpen:
		cb.setState(StateOpen, now)
	}
}

// currentState returns the current state of the CircuitBreaker.
// It also updates cb.expiry if necessary.
func (cb *CircuitBreaker) currentState(now time.Time) (State, uint64) {
	switch cb.state {
	case StateClosed:
		if !cb.expiry.IsZero() && cb.expiry.Before(now) {
			cb.reset(now)
		}
	case StateOpen:
		if cb.expiry.Before(now) {
			cb.setState(StateHalfOpen, now)
		}
	}
	return cb.state, cb.generation
}

// setState sets the state of the CircuitBreaker to the given state.
// It also resets the internal counters.
func (cb *CircuitBreaker) setState(state State, now time.Time) {
	if cb.state == state {
		return
	}

	prev := cb.state
	cb.state = state

	cb.reset(now)

	if cb.onStateChange != nil {
		cb.onStateChange(cb.name, prev, state)
	}
}

// reset resets the internal counters and moves the CircuitBreaker to a new generation after a state change.
func (cb *CircuitBreaker) reset(now time.Time) {
	cb.generation++
	cb.counts.clear()

	var zero time.Time
	switch cb.state {
	case StateClosed:
		if cb.interval == 0 {
			cb.expiry = zero
		} else {
			cb.expiry = now.Add(cb.interval)
		}
	case StateOpen:
		cb.expiry = now.Add(cb.timeout)
	default: // StateHalfOpen
		cb.expiry = zero
	}
}
