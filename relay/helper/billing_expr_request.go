package helper

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
)

func ResolveIncomingBillingExprRequestInput(c *gin.Context, info *relaycommon.RelayInfo) (billingexpr.RequestInput, error) {
	if info != nil && info.BillingRequestInput != nil {
		input := cloneRequestInput(*info.BillingRequestInput)
		merged := cloneStringMap(info.RequestHeaders)
		for k, v := range input.Headers {
			merged[k] = v
		}
		input.Headers = merged
		return input, nil
	}
	return BuildIncomingBillingExprRequestInput(c, info)
}

// BuildIncomingBillingExprRequestInput always reads the original incoming
// request. Unlike ResolveIncomingBillingExprRequestInput, it does not reuse a
// previously frozen billing input, so task retries can normalize it again for
// the newly selected upstream profile.
func BuildIncomingBillingExprRequestInput(c *gin.Context, info *relaycommon.RelayInfo) (billingexpr.RequestInput, error) {
	input := billingexpr.RequestInput{}
	if info != nil {
		input.Headers = cloneStringMap(info.RequestHeaders)
	}

	bodyBytes, err := readIncomingBillingExprBody(c)
	if err != nil {
		return billingexpr.RequestInput{}, err
	}
	input.Body = bodyBytes
	return input, nil
}

func BuildBillingExprRequestInputFromRequest(request dto.Request, headers map[string]string) (billingexpr.RequestInput, error) {
	input := billingexpr.RequestInput{
		Headers: cloneStringMap(headers),
	}
	if request == nil {
		return input, nil
	}

	bodyBytes, err := common.Marshal(request)
	if err != nil {
		return billingexpr.RequestInput{}, err
	}
	input.Body = bodyBytes
	return input, nil
}

func readIncomingBillingExprBody(c *gin.Context) ([]byte, error) {
	if c == nil || c.Request == nil {
		return nil, nil
	}

	contentType := c.Request.Header.Get("Content-Type")
	if isMultipartContentType(contentType) {
		return marshalMultipartValues(c)
	}
	if isFormURLEncodedContentType(contentType) {
		return marshalFormURLEncodedValues(c)
	}
	if !isJSONContentType(contentType) {
		return nil, nil
	}

	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return nil, err
	}
	return storage.Bytes()
}

func marshalMultipartValues(c *gin.Context) ([]byte, error) {
	form := c.Request.MultipartForm
	if form == nil {
		var err error
		form, err = common.ParseMultipartFormReusable(c)
		if err != nil {
			return nil, err
		}
		c.Request.MultipartForm = form
	}
	if form == nil || len(form.Value) == 0 {
		return nil, nil
	}

	return marshalFormValues(form.Value)
}

func marshalFormURLEncodedValues(c *gin.Context) ([]byte, error) {
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return nil, err
	}
	body, err := storage.Bytes()
	if err != nil {
		return nil, err
	}
	values, err := url.ParseQuery(string(body))
	if err != nil {
		return nil, err
	}
	return marshalFormValues(values)
}

func marshalFormValues(formValues map[string][]string) ([]byte, error) {
	values := make(map[string]interface{}, len(formValues))
	for key, valuesForKey := range formValues {
		switch len(valuesForKey) {
		case 0:
			continue
		case 1:
			values[key] = valuesForKey[0]
		default:
			values[key] = append([]string(nil), valuesForKey...)
		}
	}
	return common.Marshal(values)
}

// MergeCanonicalBillingExprRequestInput keeps the legacy request body intact
// while replacing its billing object with the channel-derived value. This lets
// existing expressions continue reading raw fields during the migration, while
// expressions using billing.* cannot be influenced by client-supplied values.
func MergeCanonicalBillingExprRequestInput(base, canonical billingexpr.RequestInput) (billingexpr.RequestInput, error) {
	merged := cloneRequestInput(base)
	if len(canonical.Headers) > 0 {
		if merged.Headers == nil {
			merged.Headers = map[string]string{}
		}
		for key, value := range canonical.Headers {
			if strings.TrimSpace(key) == "" {
				continue
			}
			merged.Headers[key] = value
		}
	}
	if len(canonical.Body) == 0 {
		return merged, nil
	}

	baseBody := map[string]interface{}{}
	if len(merged.Body) > 0 {
		if err := common.Unmarshal(merged.Body, &baseBody); err != nil {
			return billingexpr.RequestInput{}, err
		}
	}

	canonicalBody := map[string]interface{}{}
	if err := common.Unmarshal(canonical.Body, &canonicalBody); err != nil {
		return billingexpr.RequestInput{}, err
	}
	billing, exists := canonicalBody["billing"]
	if !exists {
		return billingexpr.RequestInput{}, fmt.Errorf("canonical billing input has no billing object")
	}
	if _, ok := billing.(map[string]interface{}); !ok {
		return billingexpr.RequestInput{}, fmt.Errorf("canonical billing input has invalid billing object")
	}
	baseBody["billing"] = billing

	body, err := common.Marshal(baseBody)
	if err != nil {
		return billingexpr.RequestInput{}, err
	}
	merged.Body = body
	return merged, nil
}

func cloneRequestInput(src billingexpr.RequestInput) billingexpr.RequestInput {
	input := billingexpr.RequestInput{
		Headers: cloneStringMap(src.Headers),
	}
	if len(src.Body) > 0 {
		input.Body = append([]byte(nil), src.Body...)
	}
	return input
}

func isJSONContentType(contentType string) bool {
	contentType = strings.ToLower(strings.TrimSpace(contentType))
	return strings.HasPrefix(contentType, "application/json")
}

func isMultipartContentType(contentType string) bool {
	contentType = strings.ToLower(strings.TrimSpace(contentType))
	return strings.HasPrefix(contentType, "multipart/form-data")
}

func isFormURLEncodedContentType(contentType string) bool {
	contentType = strings.ToLower(strings.TrimSpace(contentType))
	return strings.HasPrefix(contentType, "application/x-www-form-urlencoded")
}

func cloneStringMap(src map[string]string) map[string]string {
	if len(src) == 0 {
		return map[string]string{}
	}
	dst := make(map[string]string, len(src))
	for key, value := range src {
		if strings.TrimSpace(key) == "" {
			continue
		}
		dst[key] = value
	}
	return dst
}
