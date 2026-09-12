package webhook

import (
	"net/http"

	"k8s.io/apimachinery/pkg/runtime"
	crwebhook "sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

const (
	ValidatePowerPolicyPath   = "/validate-power-aura-sh-v1alpha1-powerpolicy"
	ValidatePowerOverridePath = "/validate-power-aura-sh-v1alpha1-poweroverride"
)

type registrar interface {
	Register(path string, hook http.Handler)
}

// Register installs every admission route served by the controller. The
// matching ValidatingWebhookConfiguration is rendered by the Helm chart.
func Register(server crwebhook.Server, scheme *runtime.Scheme) {
	register(server, scheme)
}

func register(server registrar, scheme *runtime.Scheme) {
	decoder := admission.NewDecoder(scheme)
	server.Register(ValidatePowerPolicyPath, &admission.Webhook{
		Handler: NewPolicyValidator(decoder),
	})
	server.Register(ValidatePowerOverridePath, &admission.Webhook{
		Handler: NewOverrideValidator(decoder),
	})
}
