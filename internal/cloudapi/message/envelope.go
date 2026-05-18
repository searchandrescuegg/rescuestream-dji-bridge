// Package message defines the DJI Cloud API MQTT message envelope shared by
// every pilot-to-cloud message (§6.2 of m30t-rcplus-cloud-mvp.md).
package message

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Envelope is the common wrapper around every Cloud API MQTT message. Data is
// kept raw so a handler can decode it into a method-specific struct.
type Envelope struct {
	Tid       string          `json:"tid"`
	Bid       string          `json:"bid"`
	Timestamp int64           `json:"timestamp"`
	Gateway   string          `json:"gateway,omitempty"`
	Method    string          `json:"method,omitempty"`
	NeedReply int             `json:"need_reply,omitempty"` // set on events requiring an events_reply
	Data      json.RawMessage `json:"data,omitempty"`
}

// Decode unmarshals an envelope from a raw MQTT payload. Unknown fields are
// tolerated on purpose: DJI adds fields across firmware versions (§10).
func Decode(payload []byte) (*Envelope, error) {
	var e Envelope
	if err := json.Unmarshal(payload, &e); err != nil {
		return nil, fmt.Errorf("decode envelope: %w", err)
	}
	return &e, nil
}

// New builds an outbound envelope with a fresh tid/bid and the current
// timestamp. data is marshalled into the Data field.
func New(method string, data any) (*Envelope, error) {
	e := &Envelope{
		Tid:       uuid.NewString(),
		Bid:       uuid.NewString(),
		Timestamp: time.Now().UnixMilli(),
		Method:    method,
	}
	if err := e.setData(data); err != nil {
		return nil, err
	}
	return e, nil
}

// Reply builds a reply envelope that echoes the request's tid/bid; replies are
// correlated to requests by tid (§6.2).
func (e *Envelope) Reply(data any) (*Envelope, error) {
	r := &Envelope{
		Tid:       e.Tid,
		Bid:       e.Bid,
		Timestamp: time.Now().UnixMilli(),
		Method:    e.Method,
	}
	if err := r.setData(data); err != nil {
		return nil, err
	}
	return r, nil
}

func (e *Envelope) setData(data any) error {
	if data == nil {
		return nil
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("marshal envelope data: %w", err)
	}
	e.Data = raw
	return nil
}

// DecodeData unmarshals the envelope's data field into v.
func (e *Envelope) DecodeData(v any) error {
	if len(e.Data) == 0 {
		return nil
	}
	if err := json.Unmarshal(e.Data, v); err != nil {
		return fmt.Errorf("decode envelope data: %w", err)
	}
	return nil
}

// Encode marshals the envelope to its wire JSON form.
func (e *Envelope) Encode() ([]byte, error) {
	raw, err := json.Marshal(e)
	if err != nil {
		return nil, fmt.Errorf("encode envelope: %w", err)
	}
	return raw, nil
}
