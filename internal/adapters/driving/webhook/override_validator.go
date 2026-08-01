package webhook

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	v1alpha1 "github.com/weauratech/aura-power/api/v1alpha1"
)

// OverrideValidator validates PowerOverride resources.
type OverrideValidator struct {
	decoder admission.Decoder
}

// Handle validates the PowerOverride resource.
func (v *OverrideValidator) Handle(ctx context.Context, req admission.Request) admission.Response {
	override := &v1alpha1.PowerOverride{}
	if err := v.decoder.Decode(req, override); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	// VR-01: Override expiration required
	if override.Spec.ExpiresAt.IsZero() {
		return admission.Denied("spec.expiresAt is required. Overrides must have an expiration timestamp. Use --duration or set spec.expiresAt.")
	}

	// VR-02: Override expiration must be in the future
	if !override.Spec.ExpiresAt.Time.After(time.Now()) {
		return admission.Denied(fmt.Sprintf("spec.expiresAt must be in the future (got %s)", override.Spec.ExpiresAt.Time.Format(time.RFC3339)))
	}

	// VR-03: Override reason required
	if len(override.Spec.Reason) < 3 {
		return admission.Denied("spec.reason is required and must be at least 3 characters. Provide a justification for this override.")
	}

	// Validate state
	if override.Spec.State != "on" && override.Spec.State != "off" {
		return admission.Denied(fmt.Sprintf("spec.state must be 'on' or 'off' (got %q)", override.Spec.State))
	}

	// Validate priority
	if override.Spec.Priority < 0 {
		return admission.Denied("spec.priority must be >= 0")
	}

	return admission.Allowed("override is valid")
}

// InjectDecoder injects the decoder.
func (v *OverrideValidator) InjectDecoder(d admission.Decoder) error {
	v.decoder = d
	return nil
}
