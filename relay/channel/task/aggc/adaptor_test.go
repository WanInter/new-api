package aggc

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

func TestParseTaskResultSuccess(t *testing.T) {
	adaptor := &TaskAdaptor{}
	body := []byte(`{"code":0,"message":"OK","data":{"job_id":123,"status":"success","video_url":"https://example.com/result.mp4","video_cover_url":"https://example.com/cover.jpg"}}`)
	info, err := adaptor.ParseTaskResult(body)
	if err != nil {
		t.Fatalf("ParseTaskResult returned error: %v", err)
	}
	if info.Status != model.TaskStatusSuccess {
		t.Fatalf("unexpected status: %s", info.Status)
	}
	if info.Url != "https://example.com/result.mp4" {
		t.Fatalf("unexpected url: %s", info.Url)
	}
}

func TestDoResponseExtractsTaskID(t *testing.T) {
	adaptor := &TaskAdaptor{}
	payload := []byte(`{"code":0,"message":"OK","data":{"job_id":123,"status":"queued"}}`)
	var resp submitResponse
	if err := common.Unmarshal(payload, &resp); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if anyToString(resp.Data.JobID) != "123" {
		t.Fatalf("unexpected job id: %s", anyToString(resp.Data.JobID))
	}
	_ = adaptor
}
