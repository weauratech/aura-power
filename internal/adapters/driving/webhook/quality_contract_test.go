package webhook

import (
	"strings"
	"testing"

	v1alpha1 "github.com/weauratech/aura-power/api/v1alpha1"
)

func TestQualityTimeWindowValidationContract(t *testing.T) {
	valid := v1alpha1.TimeWindowSpec{Start: "22:00", End: "06:30", Days: []int{0, 6}, Timezone: "America/Sao_Paulo"}
	if err := validateTimeWindow(valid, 0); err != nil {
		t.Fatalf("valid window rejected: %v", err)
	}
	for name, mutate := range map[string]func(*v1alpha1.TimeWindowSpec){
		"missing timezone": func(w *v1alpha1.TimeWindowSpec) { w.Timezone = "" },
		"invalid timezone": func(w *v1alpha1.TimeWindowSpec) { w.Timezone = "Mars/Olympus" },
		"hour":             func(w *v1alpha1.TimeWindowSpec) { w.Start = "24:00" },
		"minute":           func(w *v1alpha1.TimeWindowSpec) { w.End = "06:60" },
		"weekday":          func(w *v1alpha1.TimeWindowSpec) { w.Days = []int{7} },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := valid
			mutate(&candidate)
			if err := validateTimeWindow(candidate, 2); err == nil || !strings.Contains(err.Error(), "windows[2]") {
				t.Fatalf("invalid window accepted or poorly located: %v", err)
			}
		})
	}
}

func FuzzQualityValidateTimeOfDayNeverPanics(f *testing.F) {
	for _, seed := range []string{"00:00", "23:59", "", "0", "aa:bb", "12:345", "12-34"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, value string) { _ = validateTimeOfDay(value, 0, "start") })
}
