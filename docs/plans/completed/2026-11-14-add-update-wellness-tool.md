# Add update_wellness MCP Tool

Add an `update_wellness` tool that writes subjective wellness values (soreness, fatigue, stress, mood, motivation, injury, comments, locked) for a date to intervals.icu. Until now only `get_wellness` and `get_wellness_trend` existed.

## Context

- Files involved: `src/app/tools/update_wellness.go`, `src/app/tools/update_wellness_test.go`, `src/app/tools/tools.go`, `src/app/tools/di.go`, `README.md`
- Related patterns: `update_event.go` (PUT with partial payload), `get_wellness.go` (path and date validation)
- Dependencies: no new dependencies
- API: `PUT /api/v1/athlete/{athleteID}/wellness/{date}`; `date` goes only into the path, never into the body
- weight bewusst nicht schreibbar, Quelle ist Garmin.

## Approach

- **Testing approach**: Regular (code first, then tests)
- Scale 1 = best, 4 = worst for the six score fields; values outside 1-4 are rejected without an API call
- Only provided fields are sent (pointers with `omitempty`); no field besides `date` set is an error without an API call
- Validation lives in a helper (`validateUpdateWellnessArgs`) to keep the handler small

## Task 1: Implement the tool

- [x] Add `update_wellness.go` with args, payload, validation helper and registration
- [x] Add sentinel error `errNoWellnessFields` in `tools.go` (reuse `errOutOfRange`)
- [x] Register the tool in `di.go`
- [x] Add the tool to the README tool table

## Task 2: Tests

- [x] PUT method and path, body contains only set fields, `date` not in body
- [x] Missing date, wrong date format, no field set, value 0, value 5 return errors without a request
- [x] API errors are passed through

## Not verified, check on first use

1. Scale direction of `soreness`, `stress` and `injury` (same direction as fatigue/mood/motivation is an assumption; only those three were measured).
2. Whether fields not sent stay untouched: take a day with existing values, write only `fatigue`, then check with `get_wellness` that `mood` and `motivation` are kept.
3. Whether the Garmin sync overwrites manually set values. One user report describes this for Oura, where `locked: true` stopped it. Set `locked` only if this actually happens, since it is unclear whether a locked day still receives HRV and sleep from Garmin.
4. How to clear a field again (the cookbook mentions `-1` in general, not confirmed for wellness). Out of scope.

## Verification

- [x] `go build ./src/...`, `go test ./src/...`, `golangci-lint run ./src/...` (see commit/report for what could run locally)

## Wrap Up

- [x] Plan stored in `docs/plans/completed/`
