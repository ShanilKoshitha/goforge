package schedule

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	cronlib "github.com/robfig/cron/v3"
)

var numericCronField = regexp.MustCompile(`^[0-9*/,-]+$`)
var namedCronField = regexp.MustCompile(`^[A-Za-z0-9*/,-]+$`)

var monthNames = map[string]int{
	"jan": 1, "feb": 2, "mar": 3, "apr": 4, "may": 5, "jun": 6,
	"jul": 7, "aug": 8, "sep": 9, "oct": 10, "nov": 11, "dec": 12,
}

var weekdayNames = map[string]int{
	"sun": 0, "mon": 1, "tue": 2, "wed": 3, "thu": 4, "fri": 5, "sat": 6,
}

type cronExpression struct {
	canonical string
	schedule  cronlib.Schedule
	location  *time.Location
}

func parseCron(value string, location *time.Location) (cronExpression, error) {
	fields := strings.Fields(value)
	if len(fields) != 5 {
		return cronExpression{}, fmt.Errorf("schedule: cron expression must contain exactly five fields")
	}
	if strings.HasPrefix(strings.TrimSpace(value), "@") || strings.Contains(strings.ToUpper(value), "CRON_TZ") {
		return cronExpression{}, fmt.Errorf("schedule: cron descriptors and embedded time zones are not supported")
	}
	bounds := [5]struct {
		minimum int
		maximum int
		names   map[string]int
	}{
		{0, 59, nil},
		{0, 23, nil},
		{1, 31, nil},
		{1, 12, monthNames},
		{0, 7, weekdayNames},
	}
	for index, field := range fields {
		allowed := numericCronField
		if bounds[index].names != nil {
			allowed = namedCronField
		}
		if !allowed.MatchString(field) {
			return cronExpression{}, fmt.Errorf("schedule: invalid cron field %d", index+1)
		}
		normalized, err := normalizeCronField(field, bounds[index].minimum, bounds[index].maximum, bounds[index].names, index == 4)
		if err != nil {
			return cronExpression{}, fmt.Errorf("schedule: invalid cron field %d: %w", index+1, err)
		}
		fields[index] = normalized
	}
	canonical := strings.Join(fields, " ")
	parser := cronlib.NewParser(cronlib.Minute | cronlib.Hour | cronlib.Dom | cronlib.Month | cronlib.Dow)
	parsed, err := parser.Parse(canonical)
	if err != nil {
		return cronExpression{}, fmt.Errorf("schedule: invalid cron expression: %w", err)
	}
	spec, ok := parsed.(*cronlib.SpecSchedule)
	if !ok {
		return cronExpression{}, fmt.Errorf("schedule: cron expression did not produce a standard schedule")
	}
	spec.Location = location
	// Reject syntactically valid combinations that can never occur, such as
	// February 30. The deterministic epoch contains a leap year and the
	// underlying parser searches a full five-year horizon.
	if spec.Next(time.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC)).IsZero() {
		return cronExpression{}, fmt.Errorf("schedule: cron expression has no possible occurrence")
	}
	return cronExpression{canonical: canonical, schedule: spec, location: location}, nil
}

func normalizeCronField(value string, minimum, maximum int, names map[string]int, sundaySeven bool) (string, error) {
	parts := strings.Split(value, ",")
	normalized := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			return "", fmt.Errorf("empty list item")
		}
		stepParts := strings.Split(part, "/")
		if len(stepParts) > 2 || stepParts[0] == "" {
			return "", fmt.Errorf("invalid step %q", part)
		}
		step := 1
		if len(stepParts) == 2 {
			parsed, err := strconv.Atoi(stepParts[1])
			if err != nil || parsed < 1 {
				return "", fmt.Errorf("step must be a positive integer")
			}
			step = parsed
		}
		base := stepParts[0]
		if base == "*" {
			if len(stepParts) == 2 {
				normalized = append(normalized, "*/"+strconv.Itoa(step))
			} else {
				normalized = append(normalized, "*")
			}
			continue
		}
		rangeParts := strings.Split(base, "-")
		if len(rangeParts) > 2 {
			return "", fmt.Errorf("invalid range %q", base)
		}
		start, err := cronAtom(rangeParts[0], minimum, maximum, names)
		if err != nil {
			return "", err
		}
		end := start
		if len(rangeParts) == 2 {
			end, err = cronAtom(rangeParts[1], minimum, maximum, names)
			if err != nil {
				return "", err
			}
			if start > end {
				return "", fmt.Errorf("range start exceeds end")
			}
		} else if len(stepParts) == 2 {
			end = maximum
		}
		if sundaySeven && (start == 7 || end == 7) {
			for current := start; current <= end; current += step {
				value := current
				if current == 7 {
					value = 0
				}
				normalized = append(normalized, strconv.Itoa(value))
				if current == 7 {
					break
				}
			}
			continue
		}
		item := strconv.Itoa(start)
		if len(rangeParts) == 2 {
			item += "-" + strconv.Itoa(end)
		}
		if len(stepParts) == 2 {
			item += "/" + strconv.Itoa(step)
		}
		normalized = append(normalized, item)
	}
	return strings.Join(normalized, ","), nil
}

func cronAtom(value string, minimum, maximum int, names map[string]int) (int, error) {
	if named, ok := names[strings.ToLower(value)]; ok {
		return named, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < minimum || parsed > maximum {
		return 0, fmt.Errorf("value %q is outside %d-%d", value, minimum, maximum)
	}
	return parsed, nil
}

func (expression cronExpression) next(after time.Time) time.Time {
	if expression.schedule == nil {
		return time.Time{}
	}
	if next := expression.schedule.Next(after); !next.IsZero() {
		return next.UTC()
	}
	// robfig/cron intentionally bounds each search to five years. A valid
	// five-field expression can have an eight-year leap-day gap across a
	// non-leap century, so continue in deterministic chunks. Validation proved
	// that the expression has an occurrence in the Gregorian 400-year cycle.
	location := expression.location
	if location == nil {
		location = time.UTC
	}
	local := after.In(location)
	for year := local.Year() + 5; year <= local.Year()+400; year += 5 {
		probe := time.Date(year, time.January, 1, 0, 0, 0, 0, location).Add(-time.Second)
		if next := expression.schedule.Next(probe); !next.IsZero() && next.After(after) {
			return next.UTC()
		}
	}
	return time.Time{}
}
