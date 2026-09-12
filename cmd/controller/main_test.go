package main

import "testing"

func TestWebhookConfigurationDefaultsToDisabled(t *testing.T) {
	t.Setenv("WEBHOOK_ENABLED", "")
	if envBool("WEBHOOK_ENABLED", false) {
		t.Fatal("webhook unexpectedly enabled by default")
	}
	t.Setenv("WEBHOOK_ENABLED", "true")
	if !envBool("WEBHOOK_ENABLED", false) {
		t.Fatal("explicit webhook enablement was ignored")
	}
}

func TestWebhookPortValidation(t *testing.T) {
	for value, want := range map[string]int{
		"10443": 10443,
		"0":     9443,
		"70000": 9443,
		"bad":   9443,
	} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("WEBHOOK_PORT", value)
			if got := envInt("WEBHOOK_PORT", 9443); got != want {
				t.Fatalf("envInt() = %d, want %d", got, want)
			}
		})
	}
}
