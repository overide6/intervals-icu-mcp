package tools

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

const (
	metersPerKm        = 1000.0
	secondsPerMinute   = 60
	minPaceSecsPerKm   = 120 // 2:00 /km
	maxPaceSecsPerKm   = 900 // 15:00 /km
	paceUnitsMinsPerKm = "MINS_KM"
)

// sportSettingsWritableFields is the allowlist of sport settings fields that update_sport_settings may write.
// Everything else (notably hr_zones and pace_zones) is never sent to intervals.icu.
var sportSettingsWritableFields = []string{ //nolint:gochecknoglobals // immutable field allowlist.
	"ftp", "lthr", "max_hr", "threshold_pace",
}

// sportSettingsSummaryFields are the fields returned by list_sport_settings unless all_fields is set.
var sportSettingsSummaryFields = []string{ //nolint:gochecknoglobals // immutable field allowlist.
	"id", "types", "ftp", "indoor_ftp", "lthr", "max_hr", "threshold_pace", "pace_units",
}

var (
	settingsIDPattern = regexp.MustCompile(`^[0-9]+$`)
	pacePattern       = regexp.MustCompile(`^(\d{1,2}):([0-5]\d)$`)
)

// parsePaceMinPerKm converts a pace given as "m:ss" (optionally suffixed with "/km" or "min/km") to m/s.
func parsePaceMinPerKm(value string) (float64, error) {
	trimmed := strings.TrimSpace(value)
	trimmed = strings.TrimSuffix(trimmed, "/km")
	trimmed = strings.TrimSpace(strings.TrimSuffix(trimmed, "min"))

	match := pacePattern.FindStringSubmatch(trimmed)
	if match == nil {
		return 0, fmt.Errorf("%w: %q (expected m:ss per km, e.g. 4:53)", errInvalidPace, value)
	}

	minutes, err := strconv.Atoi(match[1])
	if err != nil {
		return 0, fmt.Errorf("%w: %q: %w", errInvalidPace, value, err)
	}

	seconds, err := strconv.Atoi(match[2])
	if err != nil {
		return 0, fmt.Errorf("%w: %q: %w", errInvalidPace, value, err)
	}

	total := minutes*secondsPerMinute + seconds
	if total < minPaceSecsPerKm || total > maxPaceSecsPerKm {
		return 0, fmt.Errorf("%w: %q (allowed 2:00 to 15:00 per km)", errInvalidPace, value)
	}

	return metersPerKm / float64(total), nil
}

// formatPaceMinPerKm converts a speed in m/s to "m:ss" per km, rounded to the nearest second.
func formatPaceMinPerKm(metersPerSecond float64) string {
	if metersPerSecond <= 0 {
		return ""
	}

	total := int(math.Round(metersPerKm / metersPerSecond))

	return fmt.Sprintf("%d:%02d", total/secondsPerMinute, total%secondsPerMinute)
}

// addPaceMinPerKm adds a human-readable threshold_pace_min_km to a sport settings map when pace is in min/km.
func addPaceMinPerKm(settings map[string]any) {
	pace, ok := settings["threshold_pace"].(float64)
	if !ok || settings["pace_units"] != paceUnitsMinsPerKm {
		return
	}

	settings["threshold_pace_min_km"] = formatPaceMinPerKm(pace)
}
