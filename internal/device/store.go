package device

import (
	"sync"
	"time"
)

// Snapshot is the latest telemetry for a single device, exposed by the API.
type Snapshot struct {
	SN        string    `json:"sn"`
	Kind      string    `json:"kind"` // KindAircraft or KindGateway
	UpdatedAt time.Time `json:"updated_at"`
	OSD       any       `json:"osd,omitempty"`
	State     any       `json:"state,omitempty"`
}

// Store holds the latest telemetry snapshot per device SN and fans updates out
// to subscribers (used by the SSE endpoint). It is safe for concurrent use.
type Store struct {
	mu        sync.RWMutex
	snapshots map[string]*Snapshot
	subs      map[chan Snapshot]struct{}
}

// NewStore creates an empty Store.
func NewStore() *Store {
	return &Store{
		snapshots: make(map[string]*Snapshot),
		subs:      make(map[chan Snapshot]struct{}),
	}
}

// SetOSD records the latest OSD telemetry for a device and notifies subscribers.
func (s *Store) SetOSD(sn, kind string, osd any) {
	s.update(sn, kind, func(snap *Snapshot) { snap.OSD = osd })
}

// SetState records the latest change-triggered state for a device.
func (s *Store) SetState(sn, kind string, state any) {
	s.update(sn, kind, func(snap *Snapshot) { snap.State = state })
}

func (s *Store) update(sn, kind string, mut func(*Snapshot)) {
	s.mu.Lock()
	snap, ok := s.snapshots[sn]
	if !ok {
		snap = &Snapshot{SN: sn}
		s.snapshots[sn] = snap
	}
	if kind != KindUnknown {
		snap.Kind = kind
	}
	snap.UpdatedAt = time.Now()
	mut(snap)
	out := *snap
	subs := make([]chan Snapshot, 0, len(s.subs))
	for ch := range s.subs {
		subs = append(subs, ch)
	}
	s.mu.Unlock()

	for _, ch := range subs {
		select {
		case ch <- out:
		default: // drop the update if a subscriber is too slow
		}
	}
}

// List returns a snapshot of every device's latest telemetry.
func (s *Store) List() []Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Snapshot, 0, len(s.snapshots))
	for _, snap := range s.snapshots {
		out = append(out, *snap)
	}
	return out
}

// Subscribe returns a channel receiving every telemetry update and a cancel
// func that must be called to unsubscribe (defer it).
func (s *Store) Subscribe() (<-chan Snapshot, func()) {
	ch := make(chan Snapshot, 16)
	s.mu.Lock()
	s.subs[ch] = struct{}{}
	s.mu.Unlock()
	return ch, func() {
		s.mu.Lock()
		delete(s.subs, ch)
		s.mu.Unlock()
	}
}
