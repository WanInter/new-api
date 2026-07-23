package relay

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
)

// normalizeOpenAIVideoResultURLs makes task-query result URLs available at a
// stable OpenAI-compatible location. Task adaptors predate this contract and
// may return the URL at the top level or only in metadata.
func normalizeOpenAIVideoResultURLs(responseBody []byte) ([]byte, error) {
	var response map[string]any
	if err := common.Unmarshal(responseBody, &response); err != nil {
		return nil, err
	}
	if len(response) == 0 {
		return responseBody, nil
	}

	resultURL := extractOpenAIVideoResultURL(response)
	if resultURL == "" {
		return responseBody, nil
	}

	response["result_url"] = resultURL
	response["url"] = resultURL
	response["video_url"] = resultURL

	metadata, _ := response["metadata"].(map[string]any)
	if metadata == nil {
		metadata = map[string]any{}
	}
	metadata["result_url"] = resultURL
	metadata["url"] = resultURL
	metadata["video_url"] = resultURL
	response["metadata"] = metadata

	return common.Marshal(response)
}

func extractOpenAIVideoResultURL(response map[string]any) string {
	for _, key := range []string{"result_url", "video_url", "url", "output_url"} {
		if url := openAIVideoURLValue(response[key]); url != "" {
			return url
		}
	}
	if url := openAIVideoURLValue(response["video"]); url != "" {
		return url
	}
	if url := openAIVideoURLValue(response["output"]); url != "" {
		return url
	}

	metadata, _ := response["metadata"].(map[string]any)
	for _, key := range []string{"result_url", "video_url", "url", "output_url", "result_urls", "video_urls", "outputs", "output"} {
		if url := openAIVideoURLValue(metadata[key]); url != "" {
			return url
		}
	}
	return ""
}

func openAIVideoURLValue(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case []any:
		for _, item := range typed {
			if url := openAIVideoURLValue(item); url != "" {
				return url
			}
		}
	case []string:
		for _, item := range typed {
			if url := strings.TrimSpace(item); url != "" {
				return url
			}
		}
	case map[string]any:
		for _, key := range []string{"result_url", "video_url", "url", "output_url"} {
			if url := openAIVideoURLValue(typed[key]); url != "" {
				return url
			}
		}
	}
	return ""
}
