package dto

import (
	"testing"

	"github.com/stretchr/testify/assert"
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
	capability := VideoModelCapability{Resolutions: []string{"480p", "720p", "1080p", "4k"}}
	assert.NoError(t, capability.Validate())

	assert.EqualError(t, VideoModelCapability{Resolutions: []string{"4K"}}.Validate(), `resolution "4K" must be one of 480p, 720p, 1080p, 4k`)
	assert.EqualError(t, VideoModelCapability{Resolutions: []string{"720p", "720p"}}.Validate(), `resolution "720p" must not be duplicated`)
}
