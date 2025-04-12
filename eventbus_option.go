package dragonscale

import "github.com/ZanzyTHEbar/dragonscale-genkit/internal/eventbus"

// WithEventBus sets the event bus component.
func WithEventBus(bus eventbus.EventBus) Option {
	return func(d *DragonScale) {
		d.eventBus = bus
	}
}
