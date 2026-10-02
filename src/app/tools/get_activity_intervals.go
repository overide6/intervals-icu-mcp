package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/0xalexb/intervals-icu-mcp/src/app/clients/intervals"
)

// intervalFields are the per-lap fields kept for running/trail analysis.
var intervalFields = []string{ //nolint:gochecknoglobals // immutable field allowlist.
	"id", "type", "label", "group_id", "start_index", "end_index", "start_time", "end_time",
	"moving_time", "elapsed_time", "distance",
	"total_elevation_gain", "min_altitude", "max_altitude", "average_gradient",
	"average_speed", "max_speed", "gap",
	"average_heartrate", "min_heartrate", "max_heartrate", "decoupling",
	"average_watts", "weighted_average_watts", "wbal_start", "wbal_end",
	"average_cadence", "average_stride", "average_stance_time", "average_vertical_oscillation",
	"training_load", "intensity", "zone",
	"average_temp", "average_weather_temp", "average_feels_like", "average_wind_speed",
	"headwind_percent", "tailwind_percent",
}

// groupFields are the fields kept for interval groups (repeated efforts).
var groupFields = []string{ //nolint:gochecknoglobals // immutable field allowlist.
	"id", "count", "moving_time", "distance", "total_elevation_gain",
	"average_speed", "gap", "average_heartrate", "max_heartrate", "average_watts", "average_cadence",
}

type getActivityIntervalsArgs struct {
	ActivityID string `json:"activity_id"          jsonschema:"activity ID to retrieve laps/intervals for"`
	AllFields  bool   `json:"all_fields,omitempty" jsonschema:"return every field from intervals.icu (default false)"`
}

type intervalsResponse struct {
	ID           any              `json:"id"`
	ICUIntervals []map[string]any `json:"icu_intervals"`
	ICUGroups    []map[string]any `json:"icu_groups"`
}

// NewGetActivityIntervalsTool returns a ToolRegistration for the get_activity_intervals tool.
func NewGetActivityIntervalsTool(apiClient *intervals.Client) ToolRegistration {
	return func(server *mcp.Server) {
		mcp.AddTool(server,
			&mcp.Tool{
				Name: "get_activity_intervals",
				Description: "Returns the laps/intervals of a specific activity with per-interval time, distance, " +
					"elevation, gradient, pace and GAP, heart rate and decoupling, running power and W'bal, " +
					"cadence/running dynamics, load and temperature/weather. Speeds are in m/s. " +
					"Set all_fields=true for the raw intervals.icu response.",
			},
			func(ctx context.Context, _ *mcp.CallToolRequest, args getActivityIntervalsArgs) (*mcp.CallToolResult, any, error) {
				if args.ActivityID == "" {
					return nil, nil, errMissingActivityID
				}

				body, err := apiClient.Get(ctx, "/api/v1/activity/"+url.PathEscape(args.ActivityID)+"/intervals", nil)
				if err != nil {
					return nil, nil, fmt.Errorf("fetching activity intervals: %w", err)
				}

				text := string(body)

				if !args.AllFields {
					text, err = trimIntervals(body)
					if err != nil {
						return nil, nil, err
					}
				}

				return &mcp.CallToolResult{
					Content: []mcp.Content{
						&mcp.TextContent{
							Text: text,
						},
					},
				}, nil, nil
			},
		)
	}
}

func trimIntervals(body []byte) (string, error) {
	var resp intervalsResponse

	err := json.Unmarshal(body, &resp)
	if err != nil {
		return "", fmt.Errorf("decoding activity intervals: %w", err)
	}

	resp.ICUIntervals = pickFields(resp.ICUIntervals, intervalFields)
	resp.ICUGroups = pickFields(resp.ICUGroups, groupFields)

	out, err := json.Marshal(resp)
	if err != nil {
		return "", fmt.Errorf("encoding activity intervals: %w", err)
	}

	return string(out), nil
}

// pickFields keeps only the given keys and drops null values.
func pickFields(items []map[string]any, fields []string) []map[string]any {
	out := make([]map[string]any, 0, len(items))

	for _, item := range items {
		kept := make(map[string]any, len(fields))

		for _, field := range fields {
			value, ok := item[field]
			if ok && value != nil {
				kept[field] = value
			}
		}

		out = append(out, kept)
	}

	return out
}
