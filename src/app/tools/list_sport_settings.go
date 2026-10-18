package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/0xalexb/intervals-icu-mcp/src/app/clients/intervals"
)

type listSportSettingsArgs struct {
	AllFields bool `json:"all_fields,omitempty" jsonschema:"return every field from intervals.icu (default false)"`
}

// NewListSportSettingsTool returns a ToolRegistration for the list_sport_settings tool.
func NewListSportSettingsTool(apiClient *intervals.Client) ToolRegistration {
	return func(server *mcp.Server) {
		mcp.AddTool(server,
			&mcp.Tool{
				Name: "list_sport_settings",
				Description: "Lists the athlete's sport settings with their IDs, activity types, ftp, lthr, max_hr " +
					"and threshold_pace (m/s; threshold_pace_min_km is added for min/km pace units). " +
					"Use the id as settings_id for update_sport_settings. " +
					"Set all_fields=true for the raw intervals.icu response.",
			},
			func(ctx context.Context, _ *mcp.CallToolRequest, args listSportSettingsArgs) (*mcp.CallToolResult, any, error) {
				body, err := apiClient.Get(ctx, "/api/v1/athlete/"+apiClient.AthleteID()+"/sport-settings", nil)
				if err != nil {
					return nil, nil, fmt.Errorf("fetching sport settings: %w", err)
				}

				text := string(body)

				if !args.AllFields {
					text, err = summarizeSportSettings(body)
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

func summarizeSportSettings(body []byte) (string, error) {
	var settings []map[string]any

	err := json.Unmarshal(body, &settings)
	if err != nil {
		return "", fmt.Errorf("decoding sport settings: %w", err)
	}

	summary := pickFields(settings, sportSettingsSummaryFields)
	for _, item := range summary {
		addPaceMinPerKm(item)
	}

	out, err := json.Marshal(summary)
	if err != nil {
		return "", fmt.Errorf("encoding sport settings: %w", err)
	}

	return string(out), nil
}
