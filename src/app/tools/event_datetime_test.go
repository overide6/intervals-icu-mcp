package tools

import "testing"

func TestNormalizeEventDateTime(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{in: "2026-10-03", want: "2026-10-03"},
		{in: "2026-10-03T07:30", want: "2026-10-03T07:30:00"},
		{in: "2026-10-03T07:30:15", want: "2026-10-03T07:30:15"},
		{in: "03-10-2026", wantErr: true},
		{in: "2026-10-03 07:30", wantErr: true},
	}

	for _, tt := range tests {
		got, err := normalizeEventDateTime(tt.in)
		if tt.wantErr {
			if err == nil {
				t.Errorf("normalizeEventDateTime(%q): expected error, got %q", tt.in, got)
			}

			continue
		}

		if err != nil {
			t.Errorf("normalizeEventDateTime(%q): unexpected error: %v", tt.in, err)

			continue
		}

		if got != tt.want {
			t.Errorf("normalizeEventDateTime(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestValidateEventTarget(t *testing.T) {
	t.Parallel()

	for _, ok := range []string{"", "AUTO", "POWER", "HR", "PACE"} {
		err := validateEventTarget(ok)
		if err != nil {
			t.Errorf("validateEventTarget(%q): unexpected error: %v", ok, err)
		}
	}

	err := validateEventTarget("SPEED")
	if err == nil {
		t.Error("validateEventTarget(\"SPEED\"): expected error")
	}
}

func TestDownsampleStreams(t *testing.T) {
	t.Parallel()

	data := make([]any, 2500)
	for i := range data {
		data[i] = float64(i)
	}

	streams := []activityStream{
		{Type: "time", Data: data},
		{Type: "heartrate", Data: append([]any(nil), data...)},
	}

	res := downsampleStreams("a1", streams, 1000)

	if res.OriginalPoints != 2500 {
		t.Fatalf("expected 2500 original points, got %d", res.OriginalPoints)
	}

	if res.Step != 3 {
		t.Fatalf("expected step 3, got %d", res.Step)
	}

	if res.ReturnedPoints != 834 {
		t.Fatalf("expected 834 returned points, got %d", res.ReturnedPoints)
	}

	if res.Streams[1].Data[1] != float64(3) {
		t.Fatalf("expected second sample 3, got %v", res.Streams[1].Data[1])
	}

	small := downsampleStreams("a2", []activityStream{{Type: "time", Data: []any{1.0, 2.0}}}, 1000)
	if small.Step != 1 || small.ReturnedPoints != 2 {
		t.Fatalf("expected no downsampling, got step %d, points %d", small.Step, small.ReturnedPoints)
	}
}
