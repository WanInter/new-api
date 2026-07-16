package dto

import "fmt"

type VideoMediaRange struct {
	Min *int `json:"min,omitempty"`
	Max *int `json:"max,omitempty"`
}

type VideoModelCapability struct {
	Images        *VideoMediaRange `json:"images,omitempty"`
	Videos        *VideoMediaRange `json:"videos,omitempty"`
	Audios        *VideoMediaRange `json:"audios,omitempty"`
	FixedDuration *int             `json:"fixed_duration,omitempty"`
	RequireJSON   *bool            `json:"require_json,omitempty"`
}

// VideoRoutingConfig contains per-channel overrides keyed by upstream model.
// The "*" key can be used as a channel-wide fallback.
type VideoRoutingConfig struct {
	Models map[string]VideoModelCapability `json:"models,omitempty"`
}

func (c *VideoRoutingConfig) Validate() error {
	if c == nil {
		return nil
	}
	for modelName, capability := range c.Models {
		if modelName == "" {
			return fmt.Errorf("video_routing model name must not be empty")
		}
		if err := capability.Validate(); err != nil {
			return fmt.Errorf("video_routing model %q: %w", modelName, err)
		}
	}
	return nil
}

func (c VideoModelCapability) Validate() error {
	media := []struct {
		name       string
		rangeValue *VideoMediaRange
	}{
		{name: "images", rangeValue: c.Images},
		{name: "videos", rangeValue: c.Videos},
		{name: "audios", rangeValue: c.Audios},
	}
	for _, item := range media {
		if item.rangeValue == nil {
			continue
		}
		if item.rangeValue.Min != nil && *item.rangeValue.Min < 0 {
			return fmt.Errorf("%s.min must be non-negative", item.name)
		}
		if item.rangeValue.Max != nil && *item.rangeValue.Max < 0 {
			return fmt.Errorf("%s.max must be non-negative", item.name)
		}
		if item.rangeValue.Min != nil && item.rangeValue.Max != nil && *item.rangeValue.Min > *item.rangeValue.Max {
			return fmt.Errorf("%s.min must not exceed %s.max", item.name, item.name)
		}
	}
	if c.FixedDuration != nil && *c.FixedDuration <= 0 {
		return fmt.Errorf("fixed_duration must be positive")
	}
	return nil
}
