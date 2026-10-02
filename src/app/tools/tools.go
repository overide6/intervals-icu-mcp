// Package tools contains MCP tool registrations for the intervals.icu MCP server.
package tools

import (
	"errors"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var (
	errMissingEventID    = errors.New("event_id is required")
	errMissingDate       = errors.New("date is required")
	errMissingOldest     = errors.New("oldest is required")
	errMissingNewest     = errors.New("newest is required")
	errMissingActivityID = errors.New("activity_id is required")
	errMissingType       = errors.New("type is required")
	errMissingName       = errors.New("name is required")
	errMissingStartDate  = errors.New("start_date_local is required")
	errMissingCategory   = errors.New("category is required")
	errInvalidDateTime   = errors.New("unsupported date/time format")
	errInvalidTarget     = errors.New("invalid target")
)

func validateDateFormat(s string) error {
	_, err := time.Parse(time.DateOnly, s)
	if err != nil {
		return fmt.Errorf("invalid date format %q, expected yyyy-MM-dd: %w", s, err)
	}

	return nil
}

// eventDateTimeLayouts lists the accepted start_date_local formats for events.
var eventDateTimeLayouts = []string{ //nolint:gochecknoglobals // immutable lookup table.
	time.DateOnly,
	"2006-01-02T15:04",
	"2006-01-02T15:04:05",
}

// normalizeEventDateTime accepts yyyy-MM-dd, yyyy-MM-ddTHH:mm or yyyy-MM-ddTHH:mm:ss.
// Date-only values are returned unchanged; values with a time are normalized to yyyy-MM-ddTHH:mm:ss.
func normalizeEventDateTime(s string) (string, error) {
	for _, layout := range eventDateTimeLayouts {
		parsed, err := time.Parse(layout, s)
		if err != nil {
			continue
		}

		if layout == time.DateOnly {
			return s, nil
		}

		return parsed.Format("2006-01-02T15:04:05"), nil
	}

	return "", fmt.Errorf(
		"invalid date format %q, expected yyyy-MM-dd, yyyy-MM-ddTHH:mm or yyyy-MM-ddTHH:mm:ss: %w",
		s, errInvalidDateTime)
}

var validEventTargets = map[string]bool{ //nolint:gochecknoglobals // immutable lookup table.
	"AUTO": true, "POWER": true, "HR": true, "PACE": true,
}

func validateEventTarget(s string) error {
	if s == "" || validEventTargets[s] {
		return nil
	}

	return fmt.Errorf("%w: %q (expected AUTO, POWER, HR or PACE)", errInvalidTarget, s)
}

// ToolRegistration is a function that registers an MCP tool on the given server.
type ToolRegistration func(server *mcp.Server)
