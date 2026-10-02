package tools

import (
	"encoding/json"
	"testing"
)

func TestTrimIntervals(t *testing.T) {
	t.Parallel()

	raw := `{"id":"i1","icu_intervals":[{"id":1,"type":"WORK","distance":1000,"gap":3.1,` +
		`"average_heartrate":150,"wbal_end":20000,"average_torque":35,"average_smo2":null,"average_temp":null}],` +
		`"icu_groups":[{"id":"g1","count":5,"gap":3.0,"average_torque":30}]}`

	out, err := trimIntervals([]byte(raw))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var parsed intervalsResponse

	err = json.Unmarshal([]byte(out), &parsed)
	if err != nil {
		t.Fatalf("invalid JSON output: %v", err)
	}

	lap := parsed.ICUIntervals[0]

	for _, want := range []string{"id", "type", "distance", "gap", "average_heartrate", "wbal_end"} {
		if _, ok := lap[want]; !ok {
			t.Errorf("expected field %q to be kept", want)
		}
	}

	for _, dropped := range []string{"average_torque", "average_smo2", "average_temp"} {
		if _, ok := lap[dropped]; ok {
			t.Errorf("expected field %q to be dropped", dropped)
		}
	}

	group := parsed.ICUGroups[0]
	if group["count"] != float64(5) {
		t.Errorf("expected group count 5, got %v", group["count"])
	}

	if _, ok := group["average_torque"]; ok {
		t.Error("expected group field average_torque to be dropped")
	}
}
