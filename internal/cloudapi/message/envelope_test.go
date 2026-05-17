package message

import "testing"

func TestDecodeLenient(t *testing.T) {
	// Unknown fields must be tolerated — DJI adds fields across firmware (§10).
	raw := []byte(`{"tid":"t1","bid":"b1","timestamp":123,"gateway":"RC1",
		"method":"update_topo","need_reply":1,"data":{"x":1},"future_field":"ignored"}`)
	env, err := Decode(raw)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if env.Tid != "t1" || env.Bid != "b1" || env.Timestamp != 123 {
		t.Errorf("envelope ids/timestamp wrong: %+v", env)
	}
	if env.Gateway != "RC1" || env.Method != "update_topo" || env.NeedReply != 1 {
		t.Errorf("envelope fields wrong: %+v", env)
	}
}

func TestDecodeData(t *testing.T) {
	env, err := Decode([]byte(`{"data":{"known":5,"future_field":"x"}}`))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	var d struct {
		Known int `json:"known"`
	}
	if err := env.DecodeData(&d); err != nil {
		t.Fatalf("DecodeData: %v", err)
	}
	if d.Known != 5 {
		t.Errorf("Known = %d, want 5", d.Known)
	}
}

func TestNewAndReply(t *testing.T) {
	env, err := New("update_topo", map[string]any{"k": "v"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if env.Tid == "" || env.Bid == "" || env.Timestamp == 0 {
		t.Errorf("New did not populate tid/bid/timestamp: %+v", env)
	}
	if env.Method != "update_topo" {
		t.Errorf("Method = %q", env.Method)
	}

	reply, err := env.Reply(map[string]any{"result": 0})
	if err != nil {
		t.Fatalf("Reply: %v", err)
	}
	if reply.Tid != env.Tid || reply.Bid != env.Bid {
		t.Error("reply must echo the request tid/bid")
	}
	if reply.Method != env.Method {
		t.Errorf("reply method = %q, want %q", reply.Method, env.Method)
	}
}

func TestEncodeRoundTrip(t *testing.T) {
	env, err := New("m", map[string]any{"a": "b"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	raw, err := env.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := Decode(raw)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.Tid != env.Tid || got.Method != env.Method {
		t.Errorf("round trip mismatch: %+v vs %+v", got, env)
	}
}

func TestDecodeInvalid(t *testing.T) {
	if _, err := Decode([]byte(`not json`)); err == nil {
		t.Error("Decode should reject invalid JSON")
	}
}
