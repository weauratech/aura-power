package domain

import (
	"testing"
	"time"

	"pgregory.net/rapid"
)

// PBT-09: Cross-midnight window consistency
func TestPropertyCrossMidnightConsistency(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Create a cross-midnight window (start > end)
		startHour := rapid.IntRange(18, 23).Draw(t, "startH")
		endHour := rapid.IntRange(1, 8).Draw(t, "endH")

		window := TimeWindow{
			Start:    TimeOfDay{Hour: startHour, Minute: 0},
			End:      TimeOfDay{Hour: endHour, Minute: 0},
			Days:     nil, // every day
			Timezone: "UTC",
		}

		if !window.CrossesMidnight() {
			t.Fatal("expected cross-midnight window")
		}

		// Time in evening segment (>= start) should be IN window
		eveningTime := time.Date(2026, 7, 30, startHour, 30, 0, 0, time.UTC)
		if !IsInWindow(window, eveningTime) {
			t.Fatalf("expected %02d:30 to be in window [%02d:00-%02d:00]", startHour, startHour, endHour)
		}

		// Time in middle of day (between end and start) should be OUT
		midDay := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
		if IsInWindow(window, midDay) {
			t.Fatalf("expected 12:00 to be outside window [%02d:00-%02d:00]", startHour, endHour)
		}
	})
}

// PBT: Schedule evaluation is deterministic
func TestPropertyScheduleDeterministic(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		hour := rapid.IntRange(0, 23).Draw(t, "hour")
		minute := rapid.IntRange(0, 59).Draw(t, "minute")
		now := time.Date(2026, 7, 30, hour, minute, 0, 0, time.UTC)

		schedule := Schedule{
			Windows: []TimeWindow{{
				Start:    TimeOfDay{8, 0},
				End:      TimeOfDay{18, 0},
				Days:     []Weekday{Monday, Tuesday, Wednesday, Thursday, Friday},
				Timezone: "UTC",
			}},
			DesiredState: PowerStateOn,
		}

		r1 := EvaluateSchedule(schedule, now)
		r2 := EvaluateSchedule(schedule, now)

		if r1 != r2 {
			t.Fatal("schedule evaluation is not deterministic")
		}
	})
}
