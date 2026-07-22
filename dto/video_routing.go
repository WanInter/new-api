package dto

import (
	"fmt"
	"strings"
)

var supportedVideoResolutions = map[string]struct{}{
	"480p":  {},
	"720p":  {},
	"1080p": {},
	"4k":    {},
}

// NormalizeVideoResolution converts accepted resolution aliases to the
// canonical values stored in video routing capabilities.
func NormalizeVideoResolution(value string) (string, bool) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "2160p" {
		value = "4k"
	}
	_, ok := supportedVideoResolutions[value]
	return value, ok
}

type VideoMediaRange struct {
	Min *int `json:"min,omitempty"`
	Max *int `json:"max,omitempty"`
}

type VideoModelCapability struct {
	Images        *VideoMediaRange `json:"images,omitempty"`
	Videos        *VideoMediaRange `json:"videos,omitempty"`
	Audios        *VideoMediaRange `json:"audios,omitempty"`
	Duration      *VideoMediaRange `json:"duration,omitempty"`
	FixedDuration *int             `json:"fixed_duration,omitempty"`
	Resolutions   []string         `json:"resolutions,omitempty"`
	RequireJSON   *bool            `json:"require_json,omitempty"`
	RequireText   *bool            `json:"require_text,omitempty"`
	// ContentPrecedence means explicit content items replace legacy media fields
	// when request references are counted for this upstream model.
	ContentPrecedence *bool `json:"content_precedence,omitempty"`
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
	seenResolutions := make(map[string]struct{}, len(c.Resolutions))
	for _, resolution := range c.Resolutions {
		normalized, ok := NormalizeVideoResolution(resolution)
		if !ok || normalized != resolution {
			return fmt.Errorf("resolution %q must be one of 480p, 720p, 1080p, 4k", resolution)
		}
		if _, duplicated := seenResolutions[normalized]; duplicated {
			return fmt.Errorf("resolution %q must not be duplicated", resolution)
		}
		seenResolutions[normalized] = struct{}{}
	}

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
	if c.Duration != nil {
		if c.FixedDuration != nil {
			return fmt.Errorf("duration and fixed_duration must not both be set")
		}
		if c.Duration.Min != nil && *c.Duration.Min <= 0 {
			return fmt.Errorf("duration.min must be positive")
		}
		if c.Duration.Max != nil && *c.Duration.Max <= 0 {
			return fmt.Errorf("duration.max must be positive")
		}
		if c.Duration.Min != nil && c.Duration.Max != nil && *c.Duration.Min > *c.Duration.Max {
			return fmt.Errorf("duration.min must not exceed duration.max")
		}
	}
	return nil
}
