package topic

import "testing"

func TestParse(t *testing.T) {
	tests := []struct {
		raw     string
		want    Topic
		wantErr bool
	}{
		{"thing/product/SN123/osd", Topic{PrefixThing, "SN123", "osd"}, false},
		{"sys/product/RC1/status", Topic{PrefixSys, "RC1", "status"}, false},
		{"thing/product/RC1/drc/up", Topic{PrefixThing, "RC1", "drc/up"}, false},
		{"thing/product/RC1/services_reply", Topic{PrefixThing, "RC1", "services_reply"}, false},
		{"bad/topic", Topic{}, true},
		{"thing/notproduct/SN/osd", Topic{}, true},
		{"", Topic{}, true},
	}
	for _, tt := range tests {
		got, err := Parse(tt.raw)
		if tt.wantErr {
			if err == nil {
				t.Errorf("Parse(%q): expected error", tt.raw)
			}
			continue
		}
		if err != nil {
			t.Errorf("Parse(%q): unexpected error %v", tt.raw, err)
			continue
		}
		if got != tt.want {
			t.Errorf("Parse(%q) = %+v, want %+v", tt.raw, got, tt.want)
		}
	}
}

func TestRoundTrip(t *testing.T) {
	for _, raw := range []string{
		"thing/product/SN/osd",
		"thing/product/RC/drc/up",
		"sys/product/RC/status_reply",
	} {
		got, err := Parse(raw)
		if err != nil {
			t.Fatalf("Parse(%q): %v", raw, err)
		}
		if got.String() != raw {
			t.Errorf("round trip: %q -> %q", raw, got.String())
		}
	}
}

func TestIsReply(t *testing.T) {
	if !New(PrefixThing, "RC", SuffixServicesReply).IsReply() {
		t.Error("services_reply should be a reply")
	}
	if New(PrefixThing, "RC", SuffixOSD).IsReply() {
		t.Error("osd should not be a reply")
	}
}

func TestBuilders(t *testing.T) {
	if got := Services("RC1").String(); got != "thing/product/RC1/services" {
		t.Errorf("Services = %q", got)
	}
	if got := StatusReply("RC1").String(); got != "sys/product/RC1/status_reply" {
		t.Errorf("StatusReply = %q", got)
	}
	if got := DRCDown("RC1").String(); got != "thing/product/RC1/drc/down" {
		t.Errorf("DRCDown = %q", got)
	}
}
