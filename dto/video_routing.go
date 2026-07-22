package dto

import (
	"fmt"
	"strconv"
	"strings"
)

const maxVideoOutputDimension = 32768

// NormalizeVideoResolution converts a public video quality label to the
// canonical value stored in video routing capabilities. Keep this grammar in
// sync with relay/common.NormalizeVideoOutputResolution: a capability must be
// able to describe every quality label the public request accepts.
func NormalizeVideoResolution(value string) (string, bool) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "2160p" || value == "4k" {
		return "4k", true
	}
	if !strings.HasSuffix(value, "p") {
		return "", false
	}
	resolution, err := strconv.Atoi(strings.TrimSuffix(value, "p"))
	if err != nil || resolution <= 0 || resolution > maxVideoOutputDimension {
		return "", false
	}
	return strconv.Itoa(resolution) + "p", true
}

// NormalizeVideoAspectRatio converts a W:H ratio to its reduced canonical
// form. adaptive is retained as a provider-level output mode.
func NormalizeVideoAspectRatio(value string) (string, bool) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "adaptive" {
		return value, true
	}
	parts := strings.Split(value, ":")
	if len(parts) != 2 {
		return "", false
	}
	width, widthErr := strconv.Atoi(strings.TrimSpace(parts[0]))
	height, heightErr := strconv.Atoi(strings.TrimSpace(parts[1]))
	if widthErr != nil || heightErr != nil || width <= 0 || height <= 0 || width > maxVideoOutputDimension || height > maxVideoOutputDimension {
		return "", false
	}
	divisor := greatestCommonDivisor(width, height)
	return strconv.Itoa(width/divisor) + ":" + strconv.Itoa(height/divisor), true
}

func greatestCommonDivisor(left, right int) int {
	for right != 0 {
		left, right = right, left%right
	}
	return left
}

type VideoMediaRange struct {
	Min *int `json:"min,omitempty"`
	Max *int `json:"max,omitempty"`
}

type VideoModelCapability struct {
	Images          *VideoMediaRange `json:"images,omitempty"`
	Videos          *VideoMediaRange `json:"videos,omitempty"`
	Audios          *VideoMediaRange `json:"audios,omitempty"`
	VideoAudioTotal *VideoMediaRange `json:"video_audio_total,omitempty"`
	Duration        *VideoMediaRange `json:"duration,omitempty"`
	FixedDuration   *int             `json:"fixed_duration,omitempty"`
	AspectRatios    []string         `json:"aspect_ratios,omitempty"`
	Resolutions     []string         `json:"resolutions,omitempty"`
	Sizes           []string         `json:"sizes,omitempty"`
	RequireJSON     *bool            `json:"require_json,omitempty"`
	RequireText     *bool            `json:"require_text,omitempty"`
	// ContentPrecedence means explicit content items replace legacy media fields
	// when request references are counted for this upstream model.
	ContentPrecedence *bool `json:"content_precedence,omitempty"`
}

// VideoRoutingConfig is retained for stored channel-setting compatibility.
// Effective video capabilities are resolved from exact channel_model rules.
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
	seenAspectRatios := make(map[string]struct{}, len(c.AspectRatios))
	for _, aspectRatio := range c.AspectRatios {
		normalized, ok := NormalizeVideoAspectRatio(aspectRatio)
		if !ok || normalized != aspectRatio {
			return fmt.Errorf("aspect_ratio %q must use canonical W:H format or adaptive", aspectRatio)
		}
		if _, duplicated := seenAspectRatios[normalized]; duplicated {
			return fmt.Errorf("aspect_ratio %q must not be duplicated", aspectRatio)
		}
		seenAspectRatios[normalized] = struct{}{}
	}

	seenResolutions := make(map[string]struct{}, len(c.Resolutions))
	for _, resolution := range c.Resolutions {
		normalized, ok := NormalizeVideoResolution(resolution)
		if !ok || normalized != resolution {
			return fmt.Errorf("resolution %q must use a canonical quality label such as 720p or 4k", resolution)
		}
		if _, duplicated := seenResolutions[normalized]; duplicated {
			return fmt.Errorf("resolution %q must not be duplicated", resolution)
		}
		seenResolutions[normalized] = struct{}{}
	}

	seenSizes := make(map[string]struct{}, len(c.Sizes))
	for _, size := range c.Sizes {
		normalized, ok := NormalizeVideoSize(size)
		if !ok || normalized != size {
			return fmt.Errorf("size %q must use canonical WxH format", size)
		}
		if _, duplicated := seenSizes[normalized]; duplicated {
			return fmt.Errorf("size %q must not be duplicated", size)
		}
		seenSizes[normalized] = struct{}{}
	}

	media := []struct {
		name       string
		rangeValue *VideoMediaRange
	}{
		{name: "images", rangeValue: c.Images},
		{name: "videos", rangeValue: c.Videos},
		{name: "audios", rangeValue: c.Audios},
		{name: "video_audio_total", rangeValue: c.VideoAudioTotal},
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

// NormalizeVideoSize validates an exact output pixel size used by a model
// capability. Legacy provider size formats are handled only by adaptors.
func NormalizeVideoSize(value string) (string, bool) {
	parts := strings.Split(strings.TrimSpace(value), "x")
	if len(parts) != 2 {
		return "", false
	}
	width, widthErr := strconv.Atoi(strings.TrimSpace(parts[0]))
	height, heightErr := strconv.Atoi(strings.TrimSpace(parts[1]))
	if widthErr != nil || heightErr != nil || width <= 0 || height <= 0 || width > maxVideoOutputDimension || height > maxVideoOutputDimension {
		return "", false
	}
	return strconv.Itoa(width) + "x" + strconv.Itoa(height), true
}
