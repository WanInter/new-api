package kling

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConvertToOpenAIVideoKeepsKlingTimestampUnitsConsistent(t *testing.T) {
	var payload responsePayload
	payload.Data.CreatedAt = 1712345678000
	payload.Data.UpdatedAt = 1712345689000
	payload.Data.TaskStatus = "succeed"
	taskData, err := common.Marshal(payload)
	require.NoError(t, err)

	task := &model.Task{
		TaskID:     "task_kling_timestamp",
		Status:     model.TaskStatusSuccess,
		Progress:   "100%",
		FinishTime: 1712345689,
		Data:       taskData,
	}
	responseBody, err := (&TaskAdaptor{}).ConvertToOpenAIVideo(task)
	require.NoError(t, err)

	var video dto.OpenAIVideo
	require.NoError(t, common.Unmarshal(responseBody, &video))
	assert.EqualValues(t, payload.Data.CreatedAt, video.CreatedAt)
	assert.EqualValues(t, payload.Data.UpdatedAt, video.CompletedAt)
	assert.GreaterOrEqual(t, video.CompletedAt, video.CreatedAt)

	task.Status = model.TaskStatusInProgress
	responseBody, err = (&TaskAdaptor{}).ConvertToOpenAIVideo(task)
	require.NoError(t, err)
	video = dto.OpenAIVideo{}
	require.NoError(t, common.Unmarshal(responseBody, &video))
	assert.Zero(t, video.CompletedAt)
}
