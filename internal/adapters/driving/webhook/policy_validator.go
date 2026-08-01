package webhook

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	v1alpha1 "github.com/weauratech/aura-power/api/v1alpha1"
)

// PolicyValidator validates PowerPolicy resources.
type PolicyValidator struct {
	decoder admission.Decoder
}

// Handle validates the PowerPolicy resource.
func (v *PolicyValidator) Handle(ctx context.Context, req admission.Request) admission.Response {
	policy := &v1alpha1.PowerPolicy{}
	if err := v.decoder.Decode(req, policy); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	// VR-05/VR-06: Validate time windows
	for i, w := range policy.Spec.Schedule.Windows {
		if err := validateTimeWindow(w, i); err != nil {
			return admission.Denied(err.Error())
		}
	}

	// Validate desiredState
	if policy.Spec.Schedule.DesiredState != "on" && policy.Spec.Schedule.DesiredState != "off" {
		return admission.Denied(fmt.Sprintf("spec.schedule.desiredState must be 'on' or 'off' (got %q)", policy.Spec.Schedule.DesiredState))
	}

	// Validate priority
	if policy.Spec.Priority < 0 {
		return admission.Denied("spec.priority must be >= 0")
	}

	return admission.Allowed("policy is valid")
}

func validateTimeWindow(w v1alpha1.TimeWindowSpec, index int) error {
	// VR-04: Valid timezone
	if w.Timezone == "" {
		return fmt.Errorf("spec.schedule.windows[%d].timezone is required", index)
	}
	if _, err := time.LoadLocation(w.Timezone); err != nil {
		return fmt.Errorf("spec.schedule.windows[%d].timezone: invalid timezone %q", index, w.Timezone)
	}

	// VR-05: Valid time format (HH:MM)
	if err := validateTimeOfDay(w.Start, index, "start"); err != nil {
		return err
	}
	if err := validateTimeOfDay(w.End, index, "end"); err != nil {
		return err
	}

	// VR-06: Valid weekday values (0-6)
	for _, d := range w.Days {
		if d < 0 || d > 6 {
			return fmt.Errorf("spec.schedule.windows[%d].days: invalid weekday %d (must be 0-6)", index, d)
		}
	}

	return nil
}

func validateTimeOfDay(s string, windowIndex int, field string) error {
	if len(s) != 5 || s[2] != ':' {
		return fmt.Errorf("spec.schedule.windows[%d].%s: must be HH:MM format (got %q)", windowIndex, field, s)
	}
	hour := int(s[0]-'0')*10 + int(s[1]-'0')
	minute := int(s[3]-'0')*10 + int(s[4]-'0')
	if hour < 0 || hour > 23 {
		return fmt.Errorf("spec.schedule.windows[%d].%s: hour must be 0-23 (got %d)", windowIndex, field, hour)
	}
	if minute < 0 || minute > 59 {
		return fmt.Errorf("spec.schedule.windows[%d].%s: minute must be 0-59 (got %d)", windowIndex, field, minute)
	}
	return nil
}

// InjectDecoder injects the decoder.
func (v *PolicyValidator) InjectDecoder(d admission.Decoder) error {
	v.decoder = d
	return nil
}
