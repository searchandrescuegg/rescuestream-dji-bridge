// Package telemetry defines typed structs for the DJI Cloud API OSD and state
// payloads. Field names are taken from the authoritative dji-sdk/Cloud-API-Doc
// aircraft + rc-pro properties pages. Every struct decodes leniently — unknown
// fields are ignored, since DJI varies them across firmware versions (§10).
package telemetry

// Aircraft mode_code values (§6.3; full enum 0-18 in the DJI docs).
const (
	ModeStandby            = 0
	ModeManualFlight       = 3
	ModeReturnToHome       = 9
	ModeLanding            = 10
	ModeNotConnected       = 14
	ModeLiveFlightControls = 17
)

// ModeName returns a human-readable name for an aircraft mode_code.
func ModeName(code int) string {
	switch code {
	case ModeStandby:
		return "standby"
	case ModeManualFlight:
		return "manual flight"
	case ModeReturnToHome:
		return "return to home"
	case ModeLanding:
		return "landing"
	case ModeNotConnected:
		return "not connected"
	case ModeLiveFlightControls:
		return "live flight controls"
	default:
		return "unknown"
	}
}

// BatteryPack is one battery in the M30T's battery.batteries[] array.
type BatteryPack struct {
	Index           int     `json:"index"`
	SN              string  `json:"sn"`
	Type            int     `json:"type"`
	SubType         int     `json:"sub_type"`
	FirmwareVersion string  `json:"firmware_version"`
	CapacityPercent int     `json:"capacity_percent"`
	Voltage         int     `json:"voltage"`     // millivolts
	Temperature     float64 `json:"temperature"` // degrees Celsius
	LoopTimes       int     `json:"loop_times"`
}

// Battery is the aggregate M30T battery state from the OSD battery struct.
type Battery struct {
	CapacityPercent  int           `json:"capacity_percent"`
	RemainFlightTime int           `json:"remain_flight_time"` // seconds
	ReturnHomePower  int           `json:"return_home_power"`
	LandingPower     int           `json:"landing_power"`
	Batteries        []BatteryPack `json:"batteries"`
}

// PositionState reports GNSS / RTK fix quality.
type PositionState struct {
	IsFixed   int `json:"is_fixed"`
	Quality   int `json:"quality"`
	GPSNumber int `json:"gps_number"`
	RTKNumber int `json:"rtk_number"`
}

// AircraftOSD is the high-frequency M30T telemetry published on
// thing/product/{aircraft_sn}/osd (§6.3).
//
// The M30T gimbal/camera OSD is keyed by payload index (e.g. "53-0-0") at the
// top level of the data object; that is consumed by the DRC layer (P3) and is
// intentionally not modelled here.
type AircraftOSD struct {
	ModeCode        int           `json:"mode_code"`
	Latitude        float64       `json:"latitude"`
	Longitude       float64       `json:"longitude"`
	Height          float64       `json:"height"`    // ellipsoidal
	Elevation       float64       `json:"elevation"` // relative to takeoff
	AttitudeHead    float64       `json:"attitude_head"`
	AttitudePitch   float64       `json:"attitude_pitch"`
	AttitudeRoll    float64       `json:"attitude_roll"`
	HorizontalSpeed float64       `json:"horizontal_speed"`
	VerticalSpeed   float64       `json:"vertical_speed"`
	HomeDistance    float64       `json:"home_distance"`
	HomeLatitude    float64       `json:"home_latitude"`
	HomeLongitude   float64       `json:"home_longitude"`
	WindSpeed       float64       `json:"wind_speed"`
	WindDirection   int           `json:"wind_direction"`
	PositionState   PositionState `json:"position_state"`
	Battery         Battery       `json:"battery"`
	ControlSource   string        `json:"control_source"`
}

// Mode returns the human-readable name of the aircraft's current mode.
func (o AircraftOSD) Mode() string { return ModeName(o.ModeCode) }

// GatewayOSD is the RC Plus telemetry published on
// thing/product/{rc_sn}/osd. The RC Plus reports only battery and location.
type GatewayOSD struct {
	CapacityPercent int     `json:"capacity_percent"`
	Latitude        float64 `json:"latitude"`
	Longitude       float64 `json:"longitude"`
	Height          float64 `json:"height"`
}
