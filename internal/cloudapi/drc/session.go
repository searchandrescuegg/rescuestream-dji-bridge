// Package drc implements the P3 DRC (Direct Remote Control) session lifecycle
// and payload commands (§8). DRC lets the cloud drive the M30T camera/gimbal —
// never flight (§9) — with operator consent.
//
// P3 scaffold: the types and state machine are in place; the consent handshake
// orchestration and command publishing are future work. Verify payload schemas
// against docs/.../10.pilot-to-cloud/00.mqtt/20.rc-pro/30.drc.md before
// implementing P3 for real.
package drc

import (
	"fmt"
	"slices"
	"sync"
)

// DRC service / event method names (§8).
const (
	MethodAuthRequest    = "cloud_control_auth_request"
	MethodAuthNotify     = "cloud_control_auth_notify"
	MethodControlRelease = "cloud_control_release"
	MethodModeEnter      = "drc_mode_enter"
	MethodStatusNotify   = "drc_status_notify"
	MethodHeartBeat      = "heart_beat"
)

// State is the lifecycle state of a DRC session (§10 state machine).
type State int

// DRC session states.
const (
	StateOffline State = iota
	StateOnline
	StateAuthPending // cloud_control_auth_request sent, awaiting operator
	StateAuthOK      // operator accepted; drc_mode_enter not yet confirmed
	StateActive      // drc_status_notify reported connected — commands allowed
)

func (s State) String() string {
	switch s {
	case StateOffline:
		return "offline"
	case StateOnline:
		return "online"
	case StateAuthPending:
		return "auth_pending"
	case StateAuthOK:
		return "auth_ok"
	case StateActive:
		return "active"
	default:
		return "unknown"
	}
}

// validTransitions lists the states reachable from each state.
var validTransitions = map[State][]State{
	StateOffline:     {StateOnline},
	StateOnline:      {StateOffline, StateAuthPending},
	StateAuthPending: {StateOffline, StateOnline, StateAuthOK},
	StateAuthOK:      {StateOffline, StateOnline, StateActive},
	StateActive:      {StateOffline, StateOnline},
}

// Session tracks one RC Plus's DRC session lifecycle. It is safe for
// concurrent use.
type Session struct {
	gatewaySN string
	mu        sync.Mutex
	state     State
}

// NewSession creates an offline DRC session for a gateway.
func NewSession(gatewaySN string) *Session {
	return &Session{gatewaySN: gatewaySN, state: StateOffline}
}

// GatewaySN returns the RC Plus serial number this session belongs to.
func (s *Session) GatewaySN() string { return s.gatewaySN }

// State returns the current session state.
func (s *Session) State() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

// Transition moves the session to state to, rejecting invalid transitions.
func (s *Session) Transition(to State) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !slices.Contains(validTransitions[s.state], to) {
		return fmt.Errorf("drc: invalid transition %s -> %s", s.state, to)
	}
	s.state = to
	return nil
}

// Active reports whether the session is ready to accept payload commands.
func (s *Session) Active() bool { return s.State() == StateActive }
