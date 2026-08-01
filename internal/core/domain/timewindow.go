package domain

import "time"

// IsInWindow evaluates whether a given time falls within a time window.
// Supports cross-midnight windows (Start > End).
func IsInWindow(window TimeWindow, now time.Time) bool {
	loc, err := time.LoadLocation(window.Timezone)
	if err != nil {
		return false
	}
	localNow := now.In(loc)

	if !isDayMatch(window.Days, localNow, window) {
		return false
	}

	currentMinutes := localNow.Hour()*60 + localNow.Minute()
	startMinutes := window.Start.ToMinutes()
	endMinutes := window.End.ToMinutes()

	if !window.CrossesMidnight() {
		return currentMinutes >= startMinutes && currentMinutes < endMinutes
	}

	// Cross-midnight: active if >= start OR < end
	return currentMinutes >= startMinutes || currentMinutes < endMinutes
}

// isDayMatch checks if the current day (or previous day for cross-midnight early segment)
// is in the window's day list.
func isDayMatch(days []Weekday, localNow time.Time, window TimeWindow) bool {
	if len(days) == 0 {
		return true // empty days = every day
	}

	currentDay := Weekday(localNow.Weekday())
	currentMinutes := localNow.Hour()*60 + localNow.Minute()

	if window.CrossesMidnight() && currentMinutes < window.End.ToMinutes() {
		// We are in the early-morning segment (past midnight).
		// The window started on the PREVIOUS day.
		previousDay := previousWeekday(currentDay)
		return containsDay(days, previousDay)
	}

	return containsDay(days, currentDay)
}

// previousWeekday returns the day before the given day.
func previousWeekday(day Weekday) Weekday {
	if day == Sunday {
		return Saturday
	}
	return day - 1
}

// containsDay checks if a weekday is in the list.
func containsDay(days []Weekday, day Weekday) bool {
	for _, d := range days {
		if d == day {
			return true
		}
	}
	return false
}

// IsInAnyWindow returns true if the time falls within any of the given windows.
func IsInAnyWindow(windows []TimeWindow, now time.Time) bool {
	for _, w := range windows {
		if IsInWindow(w, now) {
			return true
		}
	}
	return false
}

// EvaluateSchedule determines the effective power state at a given time.
// If the time is inside any window, returns the schedule's DesiredState.
// If outside all windows, returns the opposite.
func EvaluateSchedule(schedule Schedule, now time.Time) PowerState {
	if len(schedule.Windows) == 0 {
		// No windows defined = always active for desired state (24/7)
		return schedule.DesiredState
	}
	if IsInAnyWindow(schedule.Windows, now) {
		return schedule.DesiredState
	}
	return schedule.DesiredState.Opposite()
}
