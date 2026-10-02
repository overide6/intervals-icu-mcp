package tools

import (
	"context"
	"fmt"
	"net/url"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/0xalexb/intervals-icu-mcp/src/app/clients/intervals"
)

type getActivityIntervalsArgs struct {
	ActivityID string `json:"activity_id" jsonschema:"activity ID to retrieve laps/intervals for"`
}

// NewGetActivityIntervalsTool returns a ToolRegistration for the get_activity_intervals tool.
func NewGetActivityIntervalsTool(apiClient *intervals.Client) ToolRegistration {
	return func(server *mcp.Server) {
		mcp.AddTool(server,
			&mcp.Tool{
				Name: "get_activity_intervals",
				Description: "Returns the laps/intervals of a specific activity (icu_intervals and icu_groups) " +
					"with per-interval duration, distance, elevation, heart rate, pace and load.",
			},
			func(ctx context.Context, _ *mcp.CallToolRequest, args getActivityIntervalsArgs) (*mcp.CallToolResult, any, error) {
				if args.ActivityID == "" {
					return nil, nil, errMissingActivityID
				}

				body, err := apiClient.Get(ctx, "/api/v1/activity/"+url.PathEscape(args.ActivityID)+"/intervals", nil)
				if err != nil {
					return nil, nil, fmt.Errorf("fetching activity intervals: %w", err)
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
