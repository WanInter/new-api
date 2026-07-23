package vertex

import (
	"fmt"

	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/relay/channel"
	geminitask "github.com/QuantumNous/new-api/relay/channel/task/gemini"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
)

func (a *TaskAdaptor) BuildBillingInput(c *gin.Context, info *relaycommon.RelayInfo) (billingexpr.RequestInput, error) {
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return billingexpr.RequestInput{}, err
	}
	params, err := resolveVertexVeoParameters(&req, vertexVeoModelName(info))
	if err != nil {
		return billingexpr.RequestInput{}, err
	}
	return geminitask.BuildVeoCanonicalBillingInput(c, info, params, *params.GenerateAudio)
}

func (a *TaskAdaptor) GetTaskBillingCapability(info *relaycommon.RelayInfo) *channel.TaskBillingCapability {
	return geminitask.GetVertexVeoBillingCapability(vertexVeoModelName(info))
}

func resolveVertexVeoParameters(req *relaycommon.TaskSubmitReq, modelName string) (*geminitask.VeoParameters, error) {
	params, err := geminitask.ResolveVeoParameters(req, modelName)
	if err != nil {
		return nil, err
	}
	if params == nil {
		return nil, fmt.Errorf("Veo parameters are required")
	}
	if params.GenerateAudio == nil {
		generateAudio := false
		params.GenerateAudio = &generateAudio
	}
	return params, nil
}
