package tools

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/0xalexb/intervals-icu-mcp/src/app/clients/intervals"
)

const sportSettingsAthleteID = "i12345"

// fakeSportSettingsAPI emulates GET/PUT on a single sport settings entry with partial-update semantics.
type fakeSportSettingsAPI struct {
	mu       sync.Mutex
	settings map[string]any
	puts     []map[string]any
	// onPut lets a test simulate server-side side effects of a PUT.
	onPut func(settings map[string]any)
}

func newFakeSportSettingsAPI() *fakeSportSettingsAPI {
	return &fakeSportSettingsAPI{
		settings: map[string]any{
			"id":             float64(1751345),
			"types":          []any{"Run", "VirtualRun", "TrailRun"},
			"ftp":            float64(352),
			"lthr":           float64(164),
			"max_hr":         float64(181),
			"threshold_pace": 3.4482758,
			"pace_units":     "MINS_KM",
			"hr_zones":       []any{float64(136), float64(153), float64(162), float64(167), float64(181)},
			"pace_zones":     []any{72.0, 91.0, 97.0, 102.0, 999.0},
			"updated":        "2026-10-11T04:42:51.839+00:00",
		},
	}
}

func (f *fakeSportSettingsAPI) handler(t *testing.T) http.Handler {
	t.Helper()

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()

		user, pass, ok := r.BasicAuth()
		if !ok || user != "API_KEY" || pass != "test-key" {
			t.Errorf("expected basic auth API_KEY:test-key, got: %s:%s (ok=%v)", user, pass, ok)
		}

		base := "/api/v1/athlete/" + sportSettingsAthleteID + "/sport-settings"

		switch {
		case r.Method == http.MethodGet && r.URL.Path == base:
			_ = json.NewEncoder(w).Encode([]any{f.settings})
		case r.Method == http.MethodGet && r.URL.Path == base+"/1751345":
			_ = json.NewEncoder(w).Encode(f.settings)
		case r.Method == http.MethodPut && r.URL.Path == base+"/1751345":
			reqBody, _ := io.ReadAll(r.Body)

			var update map[string]any
			err := json.Unmarshal(reqBody, &update)
			if err != nil {
				t.Errorf("expected valid JSON body, got error: %v", err)
			}

			f.puts = append(f.puts, update)
			for key, value := range update {
				if pace, ok := value.(float64); ok && key == "threshold_pace" {
					value = float64(float32(pace)) // the live profile returns 3.4482758 for 4:50, i.e. float32 precision
				}

				f.settings[key] = value
			}

			f.settings["updated"] = "2026-10-18T10:00:00.000+00:00"
			if f.onPut != nil {
				f.onPut(f.settings)
			}

			_ = json.NewEncoder(w).Encode(f.settings)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
}

func (f *fakeSportSettingsAPI) putCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return len(f.puts)
}

func callSportSettingsTool(
	t *testing.T, api *fakeSportSettingsAPI, registration func(*intervals.Client) ToolRegistration,
	name string, args map[string]any,
) *mcp.CallToolResult {
	t.Helper()

	apiServer := httptest.NewServer(api.handler(t))
	t.Cleanup(apiServer.Close)

	c := intervals.NewTestClient(apiServer.URL, "test-key", sportSettingsAthleteID, apiServer.Client())

	mcpServer := mcp.NewServer(&mcp.Implementation{Name: "test-server", Version: "v0.0.1"}, nil)
	registration(c)(mcpServer)

	serverTransport, clientTransport := mcp.NewInMemoryTransports()

	serverCtx, serverCancel := context.WithCancel(context.Background())
	serverDone := make(chan struct{})

	go func() {
		defer close(serverDone)

		_ = mcpServer.Run(serverCtx, serverTransport)
	}()

	t.Cleanup(func() {
		serverCancel()
		<-serverDone
	})

	mcpClient := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0.0.1"}, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	session, err := mcpClient.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("expected client to connect, got: %v", err)
	}

	t.Cleanup(func() { _ = session.Close() })

	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("expected CallTool to succeed, got: %v", err)
	}

	return result
}

func decodeUpdateResult(t *testing.T, result *mcp.CallToolResult) updateSportSettingsResult {
	t.Helper()

	if result.IsError {
		t.Fatalf("expected success, got error result: %v", result.Content)
	}

	text, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got: %T", result.Content[0])
	}

	var decoded updateSportSettingsResult
	err := json.Unmarshal([]byte(text.Text), &decoded)
	if err != nil {
		t.Fatalf("expected JSON result, got error: %v (%s)", err, text.Text)
	}

	return decoded
}

func TestUpdateSportSettings_WritesOnlyGivenFields(t *testing.T) {
	t.Parallel()

	api := newFakeSportSettingsAPI()

	result := callSportSettingsTool(t, api, NewUpdateSportSettingsTool, "update_sport_settings", map[string]any{
		"settings_id":    "1751345",
		"ftp":            353,
		"threshold_pace": "4:53",
	})

	decoded := decodeUpdateResult(t, result)

	if len(api.puts) != 1 {
		t.Fatalf("expected exactly 1 PUT, got: %d", len(api.puts))
	}

	put := api.puts[0]
	if len(put) != 2 {
		t.Fatalf("expected PUT body with only ftp and threshold_pace, got: %v", put)
	}

	if put["ftp"] != float64(353) {
		t.Errorf("expected ftp=353 in body, got: %v", put["ftp"])
	}

	if pace, _ := put["threshold_pace"].(float64); math.Abs(pace-1000.0/293.0) > 1e-9 {
		t.Errorf("expected threshold_pace=1000/293 m/s in body, got: %v", put["threshold_pace"])
	}

	if decoded.Before["ftp"] != float64(352) || decoded.After["ftp"] != float64(353) {
		t.Errorf("expected ftp 352 -> 353, got: %v -> %v", decoded.Before["ftp"], decoded.After["ftp"])
	}

	if decoded.Before["threshold_pace_min_km"] != "4:50" || decoded.After["threshold_pace_min_km"] != "4:53" {
		t.Errorf("expected pace 4:50 -> 4:53, got: %v -> %v",
			decoded.Before["threshold_pace_min_km"], decoded.After["threshold_pace_min_km"])
	}

	if decoded.After["lthr"] != float64(164) {
		t.Errorf("expected lthr unchanged at 164, got: %v", decoded.After["lthr"])
	}

	if strings.Join(decoded.Changed, ",") != "ftp,threshold_pace" {
		t.Errorf("expected changed=[ftp threshold_pace], got: %v", decoded.Changed)
	}

	if len(decoded.UnexpectedChanges) != 0 {
		t.Errorf("expected no unexpected changes, got: %v", decoded.UnexpectedChanges)
	}
}

func TestUpdateSportSettings_SameValuesChangeNothing(t *testing.T) {
	t.Parallel()

	api := newFakeSportSettingsAPI()

	result := callSportSettingsTool(t, api, NewUpdateSportSettingsTool, "update_sport_settings", map[string]any{
		"settings_id":    "1751345",
		"ftp":            352,
		"lthr":           164,
		"threshold_pace": "4:50",
	})

	decoded := decodeUpdateResult(t, result)

	if len(decoded.Changed) != 0 {
		t.Errorf("expected no changed fields, got: %v", decoded.Changed)
	}

	if len(decoded.UnexpectedChanges) != 0 {
		t.Errorf("expected no unexpected changes, got: %v", decoded.UnexpectedChanges)
	}
}

func TestUpdateSportSettings_ReportsUnexpectedServerSideChanges(t *testing.T) {
	t.Parallel()

	api := newFakeSportSettingsAPI()
	api.onPut = func(settings map[string]any) {
		settings["hr_zones"] = []any{float64(140)}
	}

	result := callSportSettingsTool(t, api, NewUpdateSportSettingsTool, "update_sport_settings", map[string]any{
		"settings_id": "1751345",
		"lthr":        165,
	})

	decoded := decodeUpdateResult(t, result)

	if strings.Join(decoded.UnexpectedChanges, ",") != "hr_zones" {
		t.Errorf("expected unexpected_changes=[hr_zones], got: %v", decoded.UnexpectedChanges)
	}
}

func TestUpdateSportSettings_RejectsWithoutWriting(t *testing.T) {
	t.Parallel()

	cases := map[string]map[string]any{
		"missing settings_id":  {"ftp": 353},
		"empty settings_id":    {"settings_id": "", "ftp": 353},
		"sport type as id":     {"settings_id": "Run", "ftp": 353},
		"no fields":            {"settings_id": "1751345"},
		"zones not allowed":    {"settings_id": "1751345", "hr_zones": []int{1, 2, 3}},
		"pace_zones forbidden": {"settings_id": "1751345", "ftp": 353, "pace_zones": []int{1}},
		"pace in m/s":          {"settings_id": "1751345", "threshold_pace": "3.41"},
		"pace out of range":    {"settings_id": "1751345", "threshold_pace": "1:30"},
		"ftp out of range":     {"settings_id": "1751345", "ftp": 0},
		"lthr above max_hr":    {"settings_id": "1751345", "lthr": 190},
		"unknown settings_id":  {"settings_id": "999", "ftp": 353},
	}

	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			api := newFakeSportSettingsAPI()

			result := callSportSettingsTool(t, api, NewUpdateSportSettingsTool, "update_sport_settings", args)
			if !result.IsError {
				t.Errorf("expected error result, got: %v", result.Content)
			}

			if api.putCount() != 0 {
				t.Errorf("expected no PUT, got: %d", api.putCount())
			}
		})
	}
}

func TestListSportSettings_Summary(t *testing.T) {
	t.Parallel()

	api := newFakeSportSettingsAPI()

	result := callSportSettingsTool(t, api, NewListSportSettingsTool, "list_sport_settings", map[string]any{})

	text, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got: %T", result.Content[0])
	}

	var decoded []map[string]any
	err := json.Unmarshal([]byte(text.Text), &decoded)
	if err != nil {
		t.Fatalf("expected JSON array, got error: %v", err)
	}

	if len(decoded) != 1 || decoded[0]["id"] != float64(1751345) {
		t.Fatalf("expected one entry with id 1751345, got: %v", decoded)
	}

	if decoded[0]["threshold_pace_min_km"] != "4:50" {
		t.Errorf("expected threshold_pace_min_km=4:50, got: %v", decoded[0]["threshold_pace_min_km"])
	}

	if _, ok := decoded[0]["hr_zones"]; ok {
		t.Error("expected hr_zones to be omitted from the summary")
	}
}

func TestParsePaceMinPerKm(t *testing.T) {
	t.Parallel()

	for input, secs := range map[string]float64{"4:53": 293, "4:50 /km": 290, "4:50 min/km": 290, " 10:05 ": 605} {
		got, err := parsePaceMinPerKm(input)
		if err != nil {
			t.Errorf("%q: unexpected error: %v", input, err)

			continue
		}

		if math.Abs(got-1000/secs) > 1e-9 {
			t.Errorf("%q: expected %v m/s, got: %v", input, 1000/secs, got)
		}
	}

	for _, input := range []string{"", "4.53", "4:5", "4:60", "abc", "1:59", "15:01"} {
		_, err := parsePaceMinPerKm(input)
		if err == nil {
			t.Errorf("%q: expected error", input)
		}
	}
}

func TestFormatPaceMinPerKm(t *testing.T) {
	t.Parallel()

	for speed, want := range map[float64]string{3.4482758: "4:50", 1000.0 / 293.0: "4:53", 0: ""} {
		if got := formatPaceMinPerKm(speed); got != want {
			t.Errorf("%v m/s: expected %q, got: %q", speed, want, got)
		}
	}
}
