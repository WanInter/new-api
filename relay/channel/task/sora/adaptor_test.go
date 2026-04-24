package sora

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
)

func TestParseTaskResult_AllowsNumericErrorCode(t *testing.T) {
	adaptor := &TaskAdaptor{}
	respBody := []byte(`{
		"id": "video_gen_123",
		"status": "failed",
		"progress": 100,
		"error": {
			"message": "generation failed",
			"code": 500
		}
	}`)

	result, err := adaptor.ParseTaskResult(respBody)
	if err != nil {
		t.Fatalf("ParseTaskResult returned error: %v", err)
	}
	if result == nil {
		t.Fatal("ParseTaskResult returned nil result")
	}
	if result.Status != model.TaskStatusFailure {
		t.Fatalf("unexpected status: got %q want %q", result.Status, model.TaskStatusFailure)
	}
	if result.Reason != "generation failed" {
		t.Fatalf("unexpected reason: got %q want %q", result.Reason, "generation failed")
	}
}
