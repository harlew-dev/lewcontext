package main

import (
	"lewcontext/api/internal/config"
	"testing"
)

// Every seeder, the events one included, is refused in production before
// anything connects.
func TestSeedingIsRefusedInProduction(t *testing.T) {
	if err := allowed(config.Config{Production: true}); err == nil {
		t.Fatal("seeding allowed in production")
	}
	if err := allowed(config.Config{}); err != nil {
		t.Fatalf("seeding refused in development: %v", err)
	}
}
