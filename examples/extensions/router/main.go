// Command router registers a virtual model that picks a physical model from
// the route reason. It does not call a classifier.
//
//	go build -o /tmp/router-ext ./examples/extensions/router
//	go run ./cmd/pigo -e /tmp/router-ext --model router/auto -p "hi"
package main

import (
	"context"
	"log"

	"github.com/Lowpower/pigo/internal/ext"
)

func main() {
	err := ext.Serve(ext.Handler{
		Name: "router",
		VirtualModels: []ext.VirtualModelDef{{
			Provider:       "router",
			ID:             "auto",
			Name:           "Auto",
			ThinkingLevels: []string{"low", "high"},
			ContextWindow:  200000,
			MaxTokens:      8192,
			Route:          route,
		}},
	})
	if err != nil {
		log.Fatalf("router extension: %v", err)
	}
}

func route(_ context.Context, req ext.RouteRequest) (ext.RouteResult, error) {
	sticky := req.Previous
	if req.Reason == "retry" && req.Failed != nil {
		sticky = &ext.RouteTarget{Model: req.Failed.Model, ThinkingLevel: req.Failed.ThinkingLevel}
	}
	if req.Reason != "user" && req.Reason != "direct" && sticky != nil && sticky.Model.Provider != "" {
		level := sticky.ThinkingLevel
		if level == "" {
			level = "medium"
		}
		return ext.RouteResult{Model: sticky.Model, ThinkingLevel: level, KeepState: true}, nil
	}
	id := "claude-haiku-4-5"
	if req.Reason == "direct" || req.ThinkingLevel == "high" {
		id = "claude-sonnet-4-5"
	}
	return ext.RouteResult{
		Model:         ext.RouteModel{Provider: "anthropic", ID: id},
		ThinkingLevel: "medium",
	}, nil
}
