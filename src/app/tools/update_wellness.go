package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/0xalexb/intervals-icu-mcp/src/app/clients/intervals"
)

const (
	minWellnessScore = 1
	maxWellnessScore = 4
)

type updateWellnessArgs struct {
	Date       string `json:"date"                 jsonschema:"date in yyyy-MM-dd format"`
	Soreness   *int   `json:"soreness,omitempty"   jsonschema:"muscle soreness, 1 (best) to 4 (worst)"`
	Fatigue    *int   `json:"fatigue,omitempty"    jsonschema:"fatigue, 1 (best) to 4 (worst), 2 = average"`
	Stress     *int   `json:"stress,omitempty"     jsonschema:"stress, 1 (best) to 4 (worst)"`
	Mood       *int   `json:"mood,omitempty"       jsonschema:"mood, 1 (best) to 4 (worst), 3 = ok"`
	Motivation *int   `json:"motivation,omitempty" jsonschema:"motivation, 1 (best) to 4 (worst), 4 = low"`
	Injury     *int   `json:"injury,omitempty"     jsonschema:"injury, 1 (best) to 4 (worst)"`
	Comments   string `json:"comments,omitempty"   jsonschema:"free-text comment for the day"`
	Locked     *bool  `json:"locked,omitempty"     jsonschema:"sets the locked flag of the wellness day"`
}

type updateWellnessPayload struct {
	Soreness   *int   `json:"soreness,omitempty"`
	Fatigue    *int   `json:"fatigue,omitempty"`
	Stress     *int   `json:"stress,omitempty"`
	Mood       *int   `json:"mood,omitempty"`
	Motivation *int   `json:"motivation,omitempty"`
	Injury     *int   `json:"injury,omitempty"`
	Comments   string `json:"comments,omitempty"`
	Locked     *bool  `json:"locked,omitempty"`
}

// NewUpdateWellnessTool returns a ToolRegistration that registers the update_wellness tool on an MCP server.
func NewUpdateWellnessTool(apiClient *intervals.Client) ToolRegistration {
	return func(server *mcp.Server) {
		mcp.AddTool(server,
			&mcp.Tool{
				Name: "update_wellness",
				Description: "Updates subjective wellness values for the athlete on a specific date. " +
					"Only the provided fields are sent. Scale for soreness, fatigue, stress, mood, motivation " +
					"and injury: 1 = best, 4 = worst (fatigue: 2 = average; mood: 3 = ok; motivation: 4 = low).",
			},
			func(ctx context.Context, _ *mcp.CallToolRequest, args updateWellnessArgs) (*mcp.CallToolResult, any, error) {
				err := validateUpdateWellnessArgs(args)
				if err != nil {
					return nil, nil, err
				}

				payload := updateWellnessPayload{
					Soreness:   args.Soreness,
					Fatigue:    args.Fatigue,
					Stress:     args.Stress,
					Mood:       args.Mood,
					Motivation: args.Motivation,
					Injury:     args.Injury,
					Comments:   args.Comments,
					Locked:     args.Locked,
				}

				payloadBytes, err := json.Marshal(payload)
				if err != nil {
					return nil, nil, fmt.Errorf("marshaling wellness body: %w", err)
				}

				path := "/api/v1/athlete/" + apiClient.AthleteID() + "/wellness/" + url.PathEscape(args.Date)

				body, err := apiClient.Put(ctx, path, bytes.NewReader(payloadBytes))
				if err != nil {
					return nil, nil, fmt.Errorf("updating wellness: %w", err)
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

func validateUpdateWellnessArgs(args updateWellnessArgs) error {
	if args.Date == "" {
		return errMissingDate
	}

	err := validateDateFormat(args.Date)
	if err != nil {
		return err
	}

	scores := []struct {
		name  string
		value *int
	}{
		{"soreness", args.Soreness},
		{"fatigue", args.Fatigue},
		{"stress", args.Stress},
		{"mood", args.Mood},
		{"motivation", args.Motivation},
		{"injury", args.Injury},
	}

	hasField := args.Comments != "" || args.Locked != nil

	for _, score := range scores {
		if score.value == nil {
			continue
		}

		hasField = true

		if *score.value < minWellnessScore || *score.value > maxWellnessScore {
			return fmt.Errorf("%w: %s %d, expected %d to %d",
				errOutOfRange, score.name, *score.value, minWellnessScore, maxWellnessScore)
		}
	}

	if !hasField {
		return errNoWellnessFields
	}

	return nil
}
