package main

import (
	"context"
	"strings"
	"testing"
)

func TestPprofDisabledAndLoopbackOnly(t *testing.T) {
	if err := startPprofServer(context.Background(), ""); err != nil {
		t.Fatalf("disabled profiler returned error: %v", err)
	}
	for _, address := range []string{"0.0.0.0:6060", ":6060", "controller:6060", "bad"} {
		if err := startPprofServer(context.Background(), address); err == nil {
			t.Fatalf("unsafe profiler address %q was accepted", address)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := startPprofServer(ctx, "127.0.0.1:0"); err != nil {
		t.Fatalf("loopback profiler failed: %v", err)
	}
}

func TestPprofErrorDoesNotExposeAddressDetails(t *testing.T) {
	err := startPprofServer(context.Background(), "public.example:6060")
	if err == nil || !strings.Contains(err.Error(), "loopback") {
		t.Fatalf("unexpected validation error: %v", err)
	}
}
