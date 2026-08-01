package domain

import (
	"testing"
	"time"
)

func TestIsInWindow_NormalWindow_Inside(t *testing.T) {
	window := TimeWindow{
		Start:    TimeOfDay{8, 0},
		End:      TimeOfDay{18, 0},
		Days:     []Weekday{Monday, Tuesday, Wednesday, Thursday, Friday},
		Timezone: "UTC",
	}
	// Wednesday at 10:00 UTC
	now := time.Date(2026, 7, 29, 10, 0, 0, 0, time.UTC)

	if !IsInWindow(window, now) {
		t.Fatal("expected to be inside window (10:00 is between 08:00-18:00)")
	}
}

func TestIsInWindow_NormalWindow_Outside(t *testing.T) {
	window := TimeWindow{
		Start:    TimeOfDay{8, 0},
		End:      TimeOfDay{18, 0},
		Days:     []Weekday{Monday, Tuesday, Wednesday, Thursday, Friday},
		Timezone: "UTC",
	}
	// Wednesday at 20:00 UTC
	now := time.Date(2026, 7, 29, 20, 0, 0, 0, time.UTC)

	if IsInWindow(window, now) {
		t.Fatal("expected to be outside window (20:00 is after 18:00)")
	}
}

func TestIsInWindow_CrossMidnight_InEvening(t *testing.T) {
	window := TimeWindow{
		Start:    TimeOfDay{22, 0},
		End:      TimeOfDay{6, 0},
		Days:     []Weekday{Monday, Tuesday, Wednesday, Thursday, Friday},
		Timezone: "UTC",
	}
	// Wednesday at 23:30 UTC
	now := time.Date(2026, 7, 29, 23, 30, 0, 0, time.UTC)

	if !IsInWindow(window, now) {
		t.Fatal("expected inside cross-midnight window (23:30 >= 22:00)")
	}
}

func TestIsInWindow_CrossMidnight_InMorning(t *testing.T) {
	window := TimeWindow{
		Start:    TimeOfDay{22, 0},
		End:      TimeOfDay{6, 0},
		Days:     []Weekday{Wednesday}, // Window starts on Wednesday
		Timezone: "UTC",
	}
	// Thursday at 03:00 UTC (early morning segment of Wed night window)
	now := time.Date(2026, 7, 30, 3, 0, 0, 0, time.UTC) // Thursday

	if !IsInWindow(window, now) {
		t.Fatal("expected inside cross-midnight window morning segment (03:00 < 06:00, previous day=Wed is in Days)")
	}
}

func TestIsInWindow_CrossMidnight_Outside(t *testing.T) {
	window := TimeWindow{
		Start:    TimeOfDay{22, 0},
		End:      TimeOfDay{6, 0},
		Days:     []Weekday{Monday, Tuesday, Wednesday, Thursday, Friday},
		Timezone: "UTC",
	}
	// Wednesday at 10:00 UTC
	now := time.Date(2026, 7, 29, 10, 0, 0, 0, time.UTC)

	if IsInWindow(window, now) {
		t.Fatal("expected outside cross-midnight window (10:00 is not >= 22:00 and not < 06:00)")
	}
}

func TestIsInWindow_WeekendExcluded(t *testing.T) {
	window := TimeWindow{
		Start:    TimeOfDay{8, 0},
		End:      TimeOfDay{18, 0},
		Days:     []Weekday{Monday, Tuesday, Wednesday, Thursday, Friday},
		Timezone: "UTC",
	}
	// Saturday at 10:00 UTC
	now := time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC)

	if IsInWindow(window, now) {
		t.Fatal("expected outside window on Saturday (not in Days list)")
	}
}

func TestIsInWindow_EmptyDays_AlwaysMatches(t *testing.T) {
	window := TimeWindow{
		Start:    TimeOfDay{8, 0},
		End:      TimeOfDay{18, 0},
		Days:     nil, // empty = every day
		Timezone: "UTC",
	}
	now := time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC) // Saturday

	if !IsInWindow(window, now) {
		t.Fatal("expected inside window: empty Days means every day")
	}
}

func TestEvaluateSchedule_NoWindows_AlwaysDesiredState(t *testing.T) {
	schedule := Schedule{
		Windows:      nil,
		DesiredState: PowerStateOff,
	}

	result := EvaluateSchedule(schedule, time.Now())

	if result != PowerStateOff {
		t.Fatalf("expected off (24/7 when no windows), got %s", result)
	}
}

func TestEvaluateSchedule_InsideWindow_ReturnsDesiredState(t *testing.T) {
	schedule := Schedule{
		Windows: []TimeWindow{{
			Start:    TimeOfDay{8, 0},
			End:      TimeOfDay{18, 0},
			Days:     []Weekday{Monday, Tuesday, Wednesday, Thursday, Friday},
			Timezone: "UTC",
		}},
		DesiredState: PowerStateOn,
	}
	now := time.Date(2026, 7, 29, 10, 0, 0, 0, time.UTC) // Wednesday 10:00

	result := EvaluateSchedule(schedule, now)

	if result != PowerStateOn {
		t.Fatalf("expected on (inside window), got %s", result)
	}
}

func TestEvaluateSchedule_OutsideWindow_ReturnsOpposite(t *testing.T) {
	schedule := Schedule{
		Windows: []TimeWindow{{
			Start:    TimeOfDay{8, 0},
			End:      TimeOfDay{18, 0},
			Days:     []Weekday{Monday, Tuesday, Wednesday, Thursday, Friday},
			Timezone: "UTC",
		}},
		DesiredState: PowerStateOn,
	}
	now := time.Date(2026, 7, 29, 22, 0, 0, 0, time.UTC) // Wednesday 22:00

	result := EvaluateSchedule(schedule, now)

	if result != PowerStateOff {
		t.Fatalf("expected off (outside window, opposite of on), got %s", result)
	}
}
