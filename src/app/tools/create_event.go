package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/0xalexb/intervals-icu-mcp/src/app/clients/intervals"
)

type createEventArgs struct {
	Name           string   `json:"name"                    jsonschema:"event name"`
	StartDateLocal string   `json:"start_date_local"        jsonschema:"start date as yyyy-MM-dd, or with time as yyyy-MM-ddTHH:mm[:ss] (local time)"`
	Category       string   `json:"category"                jsonschema:"event category (WORKOUT, NOTE, RACE, SEASON_START)"`
	Type           string   `json:"type,omitempty"          jsonschema:"sport type (e.g. Ride or Run or Swim)"`
	Description    string   `json:"description,omitempty"   jsonschema:"event description or notes"`
	MovingTime     *float64 `json:"moving_time,omitempty"   jsonschema:"planned moving time in seconds"`
	Distance       *float64 `json:"distance,omitempty"      jsonschema:"planned distance in meters"`
	TrainingLoad   *float64 `json:"training_load,omitempty" jsonschema:"planned training load (TSS or similar)"`
	Target         string   `json:"target,omitempty"        jsonschema:"workout target metric for structured steps: AUTO, POWER, HR or PACE"`
}

// NewCreateEventTool returns a ToolRegistration that registers the create_event tool on an MCP server.
func NewCreateEventTool(apiClient *intervals.Client) ToolRegistration {
	return func(server *mcp.Server) {
		mcp.AddTool(server,
			&mcp.Tool{
				Name: "create_event",
				Description: "Creates a new event (workout, race, note, etc.) for the athlete on the specified date, " +
					"optionally with a start time. For structured workouts put the steps in intervals.icu workout " +
					"syntax into description (e.g. \"Warmup\\n- 10m Z1 HR\\n\\nMain\\n- 5x 4m Z4 HR, 2m Z1 HR\") " +
					"and set target (HR, PACE, POWER or AUTO).",
			},
			func(ctx context.Context, _ *mcp.CallToolRequest, args createEventArgs) (*mcp.CallToolResult, any, error) {
				if args.Name == "" {
					return nil, nil, errMissingName
				}

				if args.StartDateLocal == "" {
					return nil, nil, errMissingStartDate
				}

				if args.Category == "" {
					return nil, nil, errMissingCategory
				}

				normalized, err := normalizeEventDateTime(args.StartDateLocal)
				if err != nil {
					return nil, nil, err
				}

				args.StartDateLocal = normalized

				err = validateEventTarget(args.Target)
				if err != nil {
					return nil, nil, err
				}

				payload, err := json.Marshal(args)
				if err != nil {
					return nil, nil, fmt.Errorf("marshaling event body: %w", err)
				}

				path := "/api/v1/athlete/" + apiClient.AthleteID() + "/events"

				body, err := apiClient.Post(ctx, path, bytes.NewReader(payload))
				if err != nil {
					return nil, nil, fmt.Errorf("creating event: %w", err)
				}

				return &mcp.CallToolResult{
					Content: []mcp.Content{
						&mcp.TextContent{
							Text: string(body),
						},
					},
				}, nil, nil
			},
		)
	}
}
