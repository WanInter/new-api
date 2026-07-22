package dto

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVideoModelCapabilityValidateDurationConstraints(t *testing.T) {
	positiveFive := 5
	positiveFifteen := 15
	zero := 0
	reversedMin := 16
	fixed := 10

	testCases := []struct {
		name       string
		capability VideoModelCapability
		wantError  string
	}{
		{
			name:       "valid duration range",
			capability: VideoModelCapability{Duration: &VideoMediaRange{Min: &positiveFive, Max: &positiveFifteen}},
		},
		{
			name:       "non-positive minimum",
			capability: VideoModelCapability{Duration: &VideoMediaRange{Min: &zero}},
			wantError:  "duration.min must be positive",
		},
		{
			name:       "non-positive maximum",
			capability: VideoModelCapability{Duration: &VideoMediaRange{Max: &zero}},
			wantError:  "duration.max must be positive",
		},
		{
			name:       "reversed range",
			capability: VideoModelCapability{Duration: &VideoMediaRange{Min: &reversedMin, Max: &positiveFifteen}},
			wantError:  "duration.min must not exceed duration.max",
		},
		{
			name: "range conflicts with fixed duration",
			capability: VideoModelCapability{
				Duration:      &VideoMediaRange{Min: &positiveFive, Max: &positiveFifteen},
				FixedDuration: &fixed,
			},
			wantError: "duration and fixed_duration must not both be set",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			err := testCase.capability.Validate()
			if testCase.wantError == "" {
				assert.NoError(t, err)
				return
			}
			assert.EqualError(t, err, testCase.wantError)
		})
	}
}

func TestVideoModelCapabilityValidateVideoAudioTotal(t *testing.T) {
	negative := -1
	validMax := 3

	testCases := []struct {
		name       string
		capability VideoModelCapability
		wantError  string
	}{
		{
			name: "valid joint media maximum",
			capability: VideoModelCapability{
				VideoAudioTotal: &VideoMediaRange{Max: &validMax},
			},
		},
		{
			name: "negative joint media maximum",
			capability: VideoModelCapability{
				VideoAudioTotal: &VideoMediaRange{Max: &negative},
			},
			wantError: "video_audio_total.max must be non-negative",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			err := testCase.capability.Validate()
			if testCase.wantError == "" {
				assert.NoError(t, err)
				return
			}
			assert.EqualError(t, err, testCase.wantError)
		})
	}
}

func TestVideoModelCapabilityValidateResolutions(t *testing.T) {
	capability := VideoModelCapability{Resolutions: []string{"512p", "768p", "1024p", "32768p", "4k"}}
	require.NoError(t, capability.Validate())

	assert.EqualError(t, VideoModelCapability{Resolutions: []string{"4K"}}.Validate(), `resolution "4K" must use a canonical quality label such as 720p or 4k`)
	assert.EqualError(t, VideoModelCapability{Resolutions: []string{"2160p"}}.Validate(), `resolution "2160p" must use a canonical quality label such as 720p or 4k`)
	assert.EqualError(t, VideoModelCapability{Resolutions: []string{"720"}}.Validate(), `resolution "720" must use a canonical quality label such as 720p or 4k`)
	assert.EqualError(t, VideoModelCapability{Resolutions: []string{"32769p"}}.Validate(), `resolution "32769p" must use a canonical quality label such as 720p or 4k`)
	assert.EqualError(t, VideoModelCapability{Resolutions: []string{"720p", "720p"}}.Validate(), `resolution "720p" must not be duplicated`)
}

func TestVideoModelCapabilityValidateAspectRatios(t *testing.T) {
	capability := VideoModelCapability{AspectRatios: []string{"1:1", "3:4", "16:9", "32768:1", "adaptive"}}
	assert.NoError(t, capability.Validate())

	assert.EqualError(t, VideoModelCapability{AspectRatios: []string{"32:18"}}.Validate(), `aspect_ratio "32:18" must use canonical W:H format or adaptive`)
	assert.EqualError(t, VideoModelCapability{AspectRatios: []string{"32769:1"}}.Validate(), `aspect_ratio "32769:1" must use canonical W:H format or adaptive`)
	assert.EqualError(t, VideoModelCapability{AspectRatios: []string{"16:9", "16:9"}}.Validate(), `aspect_ratio "16:9" must not be duplicated`)
}

func TestVideoModelCapabilityValidateSizes(t *testing.T) {
	capability := VideoModelCapability{Sizes: []string{"720x1280", "1280x720", "32768x1"}}
	assert.NoError(t, capability.Validate())

	assert.EqualError(t, VideoModelCapability{Sizes: []string{"720X1280"}}.Validate(), `size "720X1280" must use canonical WxH format`)
	assert.EqualError(t, VideoModelCapability{Sizes: []string{"32769x1"}}.Validate(), `size "32769x1" must use canonical WxH format`)
	assert.EqualError(t, VideoModelCapability{Sizes: []string{"720x1280", "720x1280"}}.Validate(), `size "720x1280" must not be duplicated`)
}
