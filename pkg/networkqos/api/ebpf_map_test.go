package api

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestEbpfNetThrottlingConfGetResultJSONRoundTrip(t *testing.T) {
	original := EbpfNetThrottlingConfGetResult{
		WaterLine: "100mbps",
		Interval:  30,
		LowRate:   "10mbps",
		HighRate:  "50mbps",
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("failed to marshal EbpfNetThrottlingConfGetResult: %v", err)
	}

	var parsed EbpfNetThrottlingConfGetResult
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal EbpfNetThrottlingConfGetResult: %v", err)
	}

	if !reflect.DeepEqual(parsed, original) {
		t.Errorf("roundtrip mismatch:\ngot  %#v\nwant %#v", parsed, original)
	}
}

func TestEbpfNetThrottlingConfigJSONRoundTrip(t *testing.T) {
	original := EbpfNetThrottlingConfig{
		WaterLine: 100,
		Interval:  60,
		LowRate:   20,
		HighRate:  80,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("failed to marshal EbpfNetThrottlingConfig: %v", err)
	}

	var parsed EbpfNetThrottlingConfig
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal EbpfNetThrottlingConfig: %v", err)
	}

	if !reflect.DeepEqual(parsed, original) {
		t.Errorf("roundtrip mismatch:\ngot  %#v\nwant %#v", parsed, original)
	}
}

func TestEbpfNetThrottlingStatusJSONRoundTrip(t *testing.T) {
	original := EbpfNetThrottlingStatus{
		CheckTimes:      5,
		HighTimes:       2,
		LowTimes:        3,
		OnlinePKTs:      1000,
		OfflinePKTs:     500,
		OfflinePrio:     1,
		RatePast:        45,
		OfflineRatePast: 15,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("failed to marshal EbpfNetThrottlingStatus: %v", err)
	}

	var parsed EbpfNetThrottlingStatus
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal EbpfNetThrottlingStatus: %v", err)
	}

	if !reflect.DeepEqual(parsed, original) {
		t.Errorf("roundtrip mismatch:\ngot  %#v\nwant %#v", parsed, original)
	}
}

func TestEbpfNetThrottlingJSONRoundTrip(t *testing.T) {
	original := EbpfNetThrottling{
		TLast:         12345,
		Rate:          75,
		TXBytes:       2048,
		OnlineTXBytes: 4096,
		TStart:        67890,
		EbpfNetThrottlingStatus: EbpfNetThrottlingStatus{
			CheckTimes:      10,
			HighTimes:       4,
			LowTimes:        6,
			OnlinePKTs:      2000,
			OfflinePKTs:     1000,
			OfflinePrio:     2,
			RatePast:        55,
			OfflineRatePast: 25,
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("failed to marshal EbpfNetThrottling: %v", err)
	}

	var parsed EbpfNetThrottling
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal EbpfNetThrottling: %v", err)
	}

	if !reflect.DeepEqual(parsed, original) {
		t.Errorf("roundtrip mismatch:\ngot  %#v\nwant %#v", parsed, original)
	}
}
