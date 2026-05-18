// Package device tracks DJI devices: the RC Plus <-> aircraft topology and the
// latest known telemetry state for each device.
package device

import "sync"

// Device kinds.
const (
	KindAircraft = "aircraft"
	KindGateway  = "gateway"
	KindUnknown  = "unknown"
)

// SubDevice is an aircraft paired to a gateway, as reported by update_topo.
type SubDevice struct {
	SN           string `json:"sn"`
	Domain       string `json:"domain"`
	Type         int    `json:"type"`
	SubType      int    `json:"sub_type"`
	Index        string `json:"index"` // multi-aircraft slot, e.g. "A" / "B"
	ThingVersion string `json:"thing_version"`
	DeviceSecret string `json:"device_secret"`
	Nonce        string `json:"nonce"`
}

// Gateway is an RC Plus and the aircraft currently paired to it.
type Gateway struct {
	SN         string `json:"sn"`
	Type       int    `json:"type"`
	SubType    int    `json:"sub_type"`
	AircraftSN string `json:"aircraft_sn"` // empty when no aircraft is paired
	Online     bool   `json:"online"`      // false when the aircraft is offline
}

// Registry maps RC Plus gateways to their aircraft. It is safe for concurrent
// use.
type Registry struct {
	mu       sync.RWMutex
	gateways map[string]*Gateway // keyed by RC Plus SN
}

// NewRegistry creates an empty Registry.
func NewRegistry() *Registry {
	return &Registry{gateways: make(map[string]*Gateway)}
}

// ApplyTopology records a gateway and its paired aircraft from an update_topo
// message. An empty subs slice means the aircraft is offline (§5.3).
func (r *Registry) ApplyTopology(gatewaySN string, gwType, gwSubType int, subs []SubDevice) {
	r.mu.Lock()
	defer r.mu.Unlock()
	gw := &Gateway{SN: gatewaySN, Type: gwType, SubType: gwSubType}
	if len(subs) > 0 {
		gw.AircraftSN = subs[0].SN
		gw.Online = true
	}
	r.gateways[gatewaySN] = gw
}

// Link records an RC Plus <-> aircraft pairing learned during onboarding.
func (r *Registry) Link(gatewaySN, aircraftSN string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	gw, ok := r.gateways[gatewaySN]
	if !ok {
		gw = &Gateway{SN: gatewaySN}
		r.gateways[gatewaySN] = gw
	}
	gw.AircraftSN = aircraftSN
}

// Gateway returns the gateway with the given RC Plus SN.
func (r *Registry) Gateway(gatewaySN string) (Gateway, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	gw, ok := r.gateways[gatewaySN]
	if !ok {
		return Gateway{}, false
	}
	return *gw, true
}

// GatewayForAircraft returns the RC Plus SN paired to the given aircraft.
func (r *Registry) GatewayForAircraft(aircraftSN string) (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for sn, gw := range r.gateways {
		if gw.AircraftSN == aircraftSN {
			return sn, true
		}
	}
	return "", false
}

// Kind reports whether sn is a known gateway, a known aircraft, or unknown.
func (r *Registry) Kind(sn string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if _, ok := r.gateways[sn]; ok {
		return KindGateway
	}
	for _, gw := range r.gateways {
		if gw.AircraftSN == sn {
			return KindAircraft
		}
	}
	return KindUnknown
}

// List returns a snapshot of all known gateways.
func (r *Registry) List() []Gateway {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Gateway, 0, len(r.gateways))
	for _, gw := range r.gateways {
		out = append(out, *gw)
	}
	return out
}
