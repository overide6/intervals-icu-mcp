package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"slices"
	"strconv"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/0xalexb/intervals-icu-mcp/src/app/clients/intervals"
)

const (
	minFTP       = 1
	maxFTP       = 2000
	minHeartRate = 30
	maxHeartRate = 250
	// numberTolerance absorbs float rounding on stored values (e.g. threshold_pace kept as float32).
	numberTolerance = 1e-6
)

// sportSettingsReadbackFields are shown before and after the write so the change can be verified.
// The zones are included read-only to make it visible that they were not touched.
var sportSettingsReadbackFields = []string{ //nolint:gochecknoglobals // immutable field allowlist.
	"ftp", "lthr", "max_hr", "threshold_pace", "pace_units", "hr_zones", "pace_zones",
}

type updateSportSettingsArgs struct {
	SettingsID    string `json:"settings_id"              jsonschema:"numeric sport settings ID (see list_sport_settings)"`
	FTP           *int   `json:"ftp,omitempty"            jsonschema:"functional threshold power in watts"`
	LTHR          *int   `json:"lthr,omitempty"           jsonschema:"lactate threshold heart rate in bpm"`
	MaxHR         *int   `json:"max_hr,omitempty"         jsonschema:"maximum heart rate in bpm"`
	ThresholdPace string `json:"threshold_pace,omitempty" jsonschema:"threshold pace as min:ss per km, e.g. 4:53"`
}

type updateSportSettingsResult struct {
	SettingsID        string         `json:"settings_id"`
	Types             any            `json:"types"`
	Written           map[string]any `json:"written"`
	Before            map[string]any `json:"before"`
	After             map[string]any `json:"after"`
	Changed           []string       `json:"changed"`
	UnexpectedChanges []string       `json:"unexpected_changes"`
}

// NewUpdateSportSettingsTool returns a ToolRegistration for the update_sport_settings tool.
func NewUpdateSportSettingsTool(apiClient *intervals.Client) ToolRegistration {
	return func(server *mcp.Server) {
		mcp.AddTool(server,
			&mcp.Tool{
				Name: "update_sport_settings",
				Description: "Partially updates one sport settings entry (settings_id required, see list_sport_settings). " +
					"Only ftp, lthr, max_hr and threshold_pace (given as min:ss per km, stored as m/s) can be written; " +
					"zones are never changed. Returns the values before and after the write.",
			},
			func(
				ctx context.Context, _ *mcp.CallToolRequest, args updateSportSettingsArgs,
			) (*mcp.CallToolResult, any, error) {
				result, err := updateSportSettings(ctx, apiClient, args)
				if err != nil {
					return nil, nil, err
				}

				out, err := json.Marshal(result)
				if err != nil {
					return nil, nil, fmt.Errorf("encoding sport settings result: %w", err)
				}

				return &mcp.CallToolResult{
					Content: []mcp.Content{
						&mcp.TextContent{
							Text: string(out),
						},
					},
				}, nil, nil
			},
		)
	}
}

func updateSportSettings(
	ctx context.Context, apiClient *intervals.Client, args updateSportSettingsArgs,
) (*updateSportSettingsResult, error) {
	if args.SettingsID == "" {
		return nil, errMissingSettingsID
	}

	if !settingsIDPattern.MatchString(args.SettingsID) {
		return nil, fmt.Errorf("%w: %q (expected a numeric ID)", errInvalidSettingsID, args.SettingsID)
	}

	updates, err := buildSportSettingsUpdates(args)
	if err != nil {
		return nil, err
	}

	path := "/api/v1/athlete/" + apiClient.AthleteID() + "/sport-settings/" + args.SettingsID

	before, err := fetchSportSettings(ctx, apiClient, path, args.SettingsID)
	if err != nil {
		return nil, fmt.Errorf("reading sport settings before update: %w", err)
	}

	err = validateHeartRates(before, updates)
	if err != nil {
		return nil, err
	}

	payload, err := json.Marshal(updates)
	if err != nil {
		return nil, fmt.Errorf("marshaling sport settings body: %w", err)
	}

	_, err = apiClient.Put(ctx, path, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("updating sport settings: %w", err)
	}

	after, err := fetchSportSettings(ctx, apiClient, path, args.SettingsID)
	if err != nil {
		return nil, fmt.Errorf("reading sport settings after update: %w", err)
	}

	return &updateSportSettingsResult{
		SettingsID:        args.SettingsID,
		Types:             after["types"],
		Written:           updates,
		Before:            readbackSportSettings(before),
		After:             readbackSportSettings(after),
		Changed:           changedKeys(before, after, sportSettingsWritableFields),
		UnexpectedChanges: unexpectedChanges(before, after),
	}, nil
}

// buildSportSettingsUpdates returns only the allowlisted fields that were explicitly set.
func buildSportSettingsUpdates(args updateSportSettingsArgs) (map[string]any, error) {
	updates := make(map[string]any, len(sportSettingsWritableFields))

	limits := []struct {
		name     string
		value    *int
		min, max int
	}{
		{"ftp", args.FTP, minFTP, maxFTP},
		{"lthr", args.LTHR, minHeartRate, maxHeartRate},
		{"max_hr", args.MaxHR, minHeartRate, maxHeartRate},
	}

	for _, limit := range limits {
		if limit.value == nil {
			continue
		}

		if *limit.value < limit.min || *limit.value > limit.max {
			return nil, fmt.Errorf("%w: %s %d (allowed %d to %d)",
				errOutOfRange, limit.name, *limit.value, limit.min, limit.max)
		}

		updates[limit.name] = *limit.value
	}

	if args.ThresholdPace != "" {
		pace, err := parsePaceMinPerKm(args.ThresholdPace)
		if err != nil {
			return nil, err
		}

		updates["threshold_pace"] = pace
	}

	if len(updates) == 0 {
		return nil, errNoSportSettingsFields
	}

	return updates, enforceWritableAllowlist(updates)
}

// enforceWritableAllowlist is the last guard before the PUT: any key outside the allowlist aborts the write.
func enforceWritableAllowlist(updates map[string]any) error {
	for key := range updates {
		if !slices.Contains(sportSettingsWritableFields, key) {
			return fmt.Errorf("%w: %q", errFieldNotWritable, key)
		}
	}

	return nil
}

func fetchSportSettings(
	ctx context.Context, apiClient *intervals.Client, path, settingsID string,
) (map[string]any, error) {
	body, err := apiClient.Get(ctx, path, nil)
	if err != nil {
		return nil, fmt.Errorf("fetching sport settings: %w", err)
	}

	var settings map[string]any

	err = json.Unmarshal(body, &settings)
	if err != nil {
		return nil, fmt.Errorf("decoding sport settings: %w", err)
	}

	id, ok := settings["id"].(float64)
	if !ok || strconv.FormatFloat(id, 'f', -1, 64) != settingsID {
		return nil, fmt.Errorf("%w: requested %s, got %v", errSettingsIDMismatch, settingsID, settings["id"])
	}

	return settings, nil
}

// validateHeartRates rejects an update that would leave lthr above max_hr.
func validateHeartRates(current, updates map[string]any) error {
	lthr, lthrOK := mergedNumber(current, updates, "lthr")
	maxHR, maxOK := mergedNumber(current, updates, "max_hr")

	if lthrOK && maxOK && lthr > maxHR {
		return fmt.Errorf("%w: lthr %.0f is above max_hr %.0f", errOutOfRange, lthr, maxHR)
	}

	return nil
}

func mergedNumber(current, updates map[string]any, key string) (float64, bool) {
	if value, ok := updates[key].(int); ok {
		return float64(value), true
	}

	value, ok := current[key].(float64)

	return value, ok
}

func readbackSportSettings(settings map[string]any) map[string]any {
	out := make(map[string]any, len(sportSettingsReadbackFields)+1)

	for _, key := range sportSettingsReadbackFields {
		out[key] = settings[key]
	}

	addPaceMinPerKm(out)

	return out
}

func changedKeys(before, after map[string]any, keys []string) []string {
	changed := []string{}

	for _, key := range keys {
		if !valuesEqual(before[key], after[key]) {
			changed = append(changed, key)
		}
	}

	return changed
}

func valuesEqual(before, after any) bool {
	beforeNum, beforeOK := before.(float64)
	afterNum, afterOK := after.(float64)

	if beforeOK && afterOK {
		return math.Abs(beforeNum-afterNum) <= numberTolerance
	}

	return reflect.DeepEqual(before, after)
}

// unexpectedChanges lists fields outside the allowlist that differ between the two reads.
func unexpectedChanges(before, after map[string]any) []string {
	keys := make([]string, 0, len(after))

	for key := range before {
		keys = append(keys, key)
	}

	for key := range after {
		if _, ok := before[key]; !ok {
			keys = append(keys, key)
		}
	}

	keys = slices.DeleteFunc(keys, func(key string) bool {
		return key == "updated" || slices.Contains(sportSettingsWritableFields, key)
	})
	slices.Sort(keys)

	return changedKeys(before, after, keys)
}
