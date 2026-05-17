package drc

import "errors"

// Payload (camera / gimbal) command method names issued during an active DRC
// session (§8.4). These drive the M30T camera and gimbal only — never flight.
const (
	MethodCameraModeSwitch     = "camera_mode_switch"
	MethodCameraPhotoTake      = "camera_photo_take"
	MethodCameraPhotoStop      = "camera_photo_stop"
	MethodCameraRecordingStart = "camera_recording_start"
	MethodCameraRecordingStop  = "camera_recording_stop"
	MethodCameraAim            = "camera_aim"
	MethodCameraLookAt         = "camera_look_at"
	MethodCameraFocalLengthSet = "camera_focal_length_set"
	MethodGimbalReset          = "gimbal_reset"
)

// Errors returned by Controller while P3 command publishing is unimplemented.
var (
	ErrSessionNotActive = errors.New("drc: session is not active")
	ErrNotImplemented   = errors.New("drc: payload command not implemented (P3)")
)

// Controller issues payload (camera / gimbal) commands for an active DRC
// session. P3 scaffold: the commands gate on session state but do not yet
// publish — see §8.4.
type Controller struct {
	session *Session
}

// NewController creates a payload Controller bound to a DRC session.
func NewController(session *Session) *Controller {
	return &Controller{session: session}
}

// TakePhoto issues a camera_photo_take command (§8.4). Stub.
func (c *Controller) TakePhoto() error {
	if !c.session.Active() {
		return ErrSessionNotActive
	}
	return ErrNotImplemented
}

// ResetGimbal issues a gimbal_reset command (§8.4). Stub.
func (c *Controller) ResetGimbal() error {
	if !c.session.Active() {
		return ErrSessionNotActive
	}
	return ErrNotImplemented
}
