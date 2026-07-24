package relay

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/gin-gonic/gin"
)

// prepareTaskBillingRequestInput freezes request-aware billing input after a
// task adaptor has validated and mapped the request. A schema-pinned dynamic
// model receives only the provider-generated billing object; legacy models
// retain the raw request compatibility path during migration.
func prepareTaskBillingRequestInput(c *gin.Context, info *relaycommon.RelayInfo, adaptor channel.TaskAdaptor, modelName string) error {
	if info == nil || adaptor == nil {
		return fmt.Errorf("task billing input requires relay info and adaptor")
	}
	info.BillingRequestInput = nil
	info.BillingCanonicalFields = nil
	info.TaskBillingConfig = nil

	billingMode, billingExpr, schemaVersion := billing_setting.GetBillingModelSettings(modelName)
	info.TaskBillingConfig = &relaycommon.TaskBillingConfigSnapshot{
		ModelName: modelName,
		Mode:      billingMode,
		Expr:      billingExpr,
		Schema:    schemaVersion,
	}
	schemaVersion = strings.TrimSpace(schemaVersion)
	if billingMode == billing_setting.BillingModeTieredExpr && schemaVersion != "" {
		capabilityProvider, ok := adaptor.(channel.TaskBillingCapabilityProvider)
		if !ok {
			return fmt.Errorf("channel does not provide canonical task billing capability for schema %q", schemaVersion)
		}
		capability := normalizeTaskBillingCapability(capabilityProvider.GetTaskBillingCapability(info))
		if capability == nil {
			return fmt.Errorf("channel does not provide canonical task billing schema %q for mapped model %q", schemaVersion, info.UpstreamModelName)
		}
		modelCapability := capability
		if capability.SchemaVersion != schemaVersion {
			summary, err := GetTaskBillingCapabilitySummary(modelName)
			if err != nil {
				return fmt.Errorf("resolve model canonical billing schema: %w", err)
			}
			if !summary.Compatible {
				return fmt.Errorf("model canonical billing schema is no longer compatible: %s", summary.Reason)
			}
			if summary.SchemaVersion != schemaVersion {
				return fmt.Errorf("current model canonical billing schema %q does not match configured schema %q", summary.SchemaVersion, schemaVersion)
			}
			modelCapability = normalizeTaskBillingCapability(&channel.TaskBillingCapability{
				SchemaVersion: summary.SchemaVersion,
				Fields:        cloneTaskBillingFields(summary.Fields),
			})
			if err := taskBillingCapabilityFitsModelSchema(capability, modelCapability); err != nil {
				return fmt.Errorf("channel canonical billing schema cannot satisfy model schema %q: %w", schemaVersion, err)
			}
		}
		if err := billingexpr.ValidateCanonicalBillingExpression(billingExpr, CanonicalBillingFields(modelCapability)); err != nil {
			return fmt.Errorf("canonical billing expression is invalid: %w", err)
		}
		inputProvider, ok := adaptor.(channel.TaskBillingInputProvider)
		if !ok {
			return fmt.Errorf("channel does not provide canonical task billing input for schema %q", schemaVersion)
		}
		canonicalInput, err := inputProvider.BuildBillingInput(c, info)
		if err != nil {
			return err
		}
		// Providers retain request headers for the transient expression context,
		// but schema-pinned expressions reject header() and only the validated
		// canonical body is persisted for task billing audit.
		canonicalInput.Body = append([]byte(nil), canonicalInput.Body...)
		if err := billingexpr.ValidateCanonicalBillingInput(canonicalInput.Body, CanonicalBillingFields(capability)); err != nil {
			return err
		}
		if capability.SchemaVersion != modelCapability.SchemaVersion {
			if err := billingexpr.ValidateCanonicalBillingInput(canonicalInput.Body, CanonicalBillingFields(modelCapability)); err != nil {
				return fmt.Errorf("canonical billing input does not satisfy model schema %q: %w", schemaVersion, err)
			}
		}
		info.BillingRequestInput = &canonicalInput
		info.BillingCanonicalFields = CanonicalBillingFields(modelCapability)
		return nil
	}

	// Legacy migration path. Existing expressions may still read original
	// request paths, while adaptors that already generate billing.* can protect
	// that object from client-supplied values by merging it into the request.
	normalizer, hasNormalizer := adaptor.(channel.TaskBillingRequestBodyNormalizer)
	provider, hasBillingInputProvider := adaptor.(channel.TaskBillingInputProvider)
	if !hasNormalizer && !hasBillingInputProvider {
		return nil
	}
	requestInput, err := helper.BuildIncomingBillingExprRequestInput(c, info)
	if err != nil {
		return err
	}
	if hasNormalizer {
		normalizedBody, err := normalizer.NormalizeBillingRequestBody(info, requestInput.Body)
		if err != nil {
			return err
		}
		requestInput.Body = normalizedBody
	}
	if hasBillingInputProvider {
		canonicalInput, err := provider.BuildBillingInput(c, info)
		if err != nil {
			return err
		}
		requestInput, err = helper.MergeCanonicalBillingExprRequestInput(requestInput, canonicalInput)
		if err != nil {
			return err
		}
	}
	info.BillingRequestInput = &requestInput
	return nil
}
