package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/0xalexb/intervals-icu-mcp/src/app/clients/intervals"
)

const (
	defaultStreamTypes  = "time,distance,altitude,heartrate,velocity_smooth,cadence,grade_smooth"
	defaultMaxPoints    = 1000
	maxAllowedMaxPoints = 20000
)

var errInvalidMaxPoints = errors.New("max_points must be between 0 and 20000")

type getActivityStreamsArgs struct {
	ActivityID string `json:"activity_id"          jsonschema:"activity ID to retrieve streams for"`
	Types      string `json:"types,omitempty"      jsonschema:"comma-separated stream types, e.g. time,distance,altitude,heartrate,velocity_smooth,cadence,grade_smooth,watts,latlng (default: time,distance,altitude,heartrate,velocity_smooth,cadence,grade_smooth)"`
	MaxPoints  int    `json:"max_points,omitempty" jsonschema:"downsample every stream to at most this many evenly spaced points (default 1000, 0 = default, max 20000)"`
}

type activityStream struct {
	Type  string `json:"type"`
	Name  string `json:"name,omitempty"`
	Data  []any  `json:"data"`
	Data2 []any  `json:"data2,omitempty"`
}

type streamsResult struct {
	ActivityID     string           `json:"activity_id"`
	OriginalPoints int              `json:"original_points"`
	ReturnedPoints int              `json:"returned_points"`
	Step           int              `json:"step"`
	Streams        []activityStream `json:"streams"`
}

// NewGetActivityStreamsTool returns a ToolRegistration for the get_activity_streams tool.
func NewGetActivityStreamsTool(apiClient *intervals.Client) ToolRegistration {
	return func(server *mcp.Server) {
		mcp.AddTool(server,
			&mcp.Tool{
				Name: "get_activity_streams",
				Description: "Returns time-series streams (e.g. time, distance, altitude, heartrate, pace/velocity, " +
					"cadence, grade) for a specific activity, downsampled to keep the response small. " +
					"Use it for segment analysis such as climbs.",
			},
			func(ctx context.Context, _ *mcp.CallToolRequest, args getActivityStreamsArgs) (*mcp.CallToolResult, any, error) {
				if args.ActivityID == "" {
					return nil, nil, errMissingActivityID
				}

				if args.MaxPoints < 0 || args.MaxPoints > maxAllowedMaxPoints {
					return nil, nil, errInvalidMaxPoints
				}

				maxPoints := args.MaxPoints
				if maxPoints == 0 {
					maxPoints = defaultMaxPoints
				}

				types := strings.TrimSpace(args.Types)
				if types == "" {
					types = defaultStreamTypes
				}

				query := url.Values{}
				query.Set("types", types)

				body, err := apiClient.Get(ctx, "/api/v1/activity/"+url.PathEscape(args.ActivityID)+"/streams", query)
				if err != nil {
					return nil, nil, fmt.Errorf("fetching activity streams: %w", err)
				}

				var streams []activityStream

				err = json.Unmarshal(body, &streams)
				if err != nil {
					return nil, nil, fmt.Errorf("decoding activity streams: %w", err)
				}

				result := downsampleStreams(args.ActivityID, streams, maxPoints)

				out, err := json.Marshal(result)
				if err != nil {
					return nil, nil, fmt.Errorf("encoding activity streams: %w", err)
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

func downsampleStreams(activityID string, streams []activityStream, maxPoints int) streamsResult {
	original := 0
	for _, s := range streams {
		if len(s.Data) > original {
			original = len(s.Data)
		}
	}

	step := 1
	if original > maxPoints {
		step = (original + maxPoints - 1) / maxPoints
	}

	returned := 0

	for i := range streams {
		streams[i].Data = takeEvery(streams[i].Data, step)
		streams[i].Data2 = takeEvery(streams[i].Data2, step)

		if len(streams[i].Data) > returned {
			returned = len(streams[i].Data)
		}
	}

	return streamsResult{
		ActivityID:     activityID,
		OriginalPoints: original,
		ReturnedPoints: returned,
		Step:           step,
		Streams:        streams,
	}
}

func takeEvery(data []any, step int) []any {
	if step <= 1 || len(data) == 0 {
		return data
	}

	out := make([]any, 0, (len(data)+step-1)/step)
	for i := 0; i < len(data); i += step {
		out = append(out, data[i])
	}

	return out
}
