package adapters

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"log"

	"github.com/ZanzyTHEbar/dragonscale-genkit"
	"github.com/firebase/genkit/go/core"
)

// GenkitPlannerAdapter uses a Genkit Flow to implement the Planner interface.
type GenkitPlannerAdapter struct {
	plannerFlow *core.Flow[*dragonscale.PlannerInput, *dragonscale.ExecutionPlan, struct{}]
	cache       dragonscale.Cache
}

// NewGenkitPlannerAdapter creates a new adapter for the planner flow.
func NewGenkitPlannerAdapter(plannerFlow *core.Flow[*dragonscale.PlannerInput, *dragonscale.ExecutionPlan, struct{}], cache dragonscale.Cache) *GenkitPlannerAdapter {
	return &GenkitPlannerAdapter{
		plannerFlow: plannerFlow,
		cache:       cache,
	}
}

// GeneratePlan implements the dragonscale.Planner interface.
func (a *GenkitPlannerAdapter) GeneratePlan(ctx context.Context, input dragonscale.PlannerInput) (*dragonscale.ExecutionPlan, error) {
	cacheKey := a.generateCacheKey(ctx, input)

	// Try fetching from cache
	if cachedPlan, found := a.cache.Get(ctx, cacheKey); found {
		if plan, ok := cachedPlan.(*dragonscale.ExecutionPlan); ok {
			log.Printf("Planner cache hit for key: %s", cacheKey)
			// Re-initialize the non-serializable parts of the plan
			return dragonscale.NewExecutionPlan(plan.Tasks), nil
		} else {
			log.Printf("Planner cache hit for key %s, but type assertion failed (expected *dragonscale.ExecutionPlan, got %T)", cacheKey, cachedPlan)
		}
	}
	log.Printf("Planner cache miss for key: %s", cacheKey)

	// Run the Genkit planner flow
	plan, err := a.plannerFlow.Run(ctx, &input) // Pass pointer
	if err != nil {
		// Wrap the error using NewPlannerError
		return nil, dragonscale.NewPlannerError("planner flow execution failed", err)
	}

	if plan == nil || len(plan.Tasks) == 0 {
		// Use NewPlannerError for empty/nil plan result
		return nil, dragonscale.NewPlannerError("planner flow returned an empty or nil plan", nil)
	}

	// Store the generated plan in cache (store the serializable part)
	a.cache.Set(ctx, cacheKey, plan)
	log.Printf("Planner result stored in cache with key: %s", cacheKey)

	// Re-initialize the non-serializable parts before returning
	return dragonscale.NewExecutionPlan(plan.Tasks), nil
}

// generateCacheKey creates a unique key for caching planner results.
func (a *GenkitPlannerAdapter) generateCacheKey(ctx context.Context, input dragonscale.PlannerInput) string {
	// Create a stable representation of the input (e.g., JSON)
	// Avoid including CurrentState and Reason in the cache key for initial planning
	cacheableInput := struct {
		Query      string                            `json:"query"`
		ToolSchema map[string]map[string]interface{} `json:"tool_schema"` // Updated type
	}{
		Query:      input.Query,
		ToolSchema: input.ToolSchema, // Use the map[string]map[string]interface{} directly
	}

	inputBytes, err := json.Marshal(cacheableInput)
	if err != nil {
		log.Printf("Failed to marshal planner input for cache key: %v", err)
		// Fallback to a simpler key if marshalling fails
		return "planner:" + input.Query
	}

	hasher := sha1.New()
	hasher.Write(inputBytes)
	return "planner:" + hex.EncodeToString(hasher.Sum(nil))
}
