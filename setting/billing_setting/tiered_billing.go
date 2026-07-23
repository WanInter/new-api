package billing_setting

import (
	"fmt"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/samber/lo"
)

const (
	BillingModeRatio      = "ratio"
	BillingModeTieredExpr = "tiered_expr"
	BillingModeField      = "billing_mode"
	BillingExprField      = "billing_expr"
	BillingSchemaField    = "billing_schema"
)

// BillingSetting is managed by config.GlobalConfig.Register.
// DB keys: billing_setting.billing_mode, billing_setting.billing_expr
type BillingSetting struct {
	BillingMode   map[string]string `json:"billing_mode"`
	BillingExpr   map[string]string `json:"billing_expr"`
	BillingSchema map[string]string `json:"billing_schema"`
}

var billingSetting = BillingSetting{
	BillingMode:   make(map[string]string),
	BillingExpr:   make(map[string]string),
	BillingSchema: make(map[string]string),
}

var billingSettingMu sync.RWMutex

func init() {
	config.GlobalConfig.Register("billing_setting", &billingSetting)
}

// ---------------------------------------------------------------------------
// Read accessors (hot path, must be fast)
// ---------------------------------------------------------------------------

func GetBillingMode(model string) string {
	billingSettingMu.RLock()
	defer billingSettingMu.RUnlock()
	if mode, ok := billingSetting.BillingMode[model]; ok {
		return mode
	}
	return BillingModeRatio
}

func GetBillingExpr(model string) (string, bool) {
	billingSettingMu.RLock()
	defer billingSettingMu.RUnlock()
	expr, ok := billingSetting.BillingExpr[model]
	return expr, ok
}

// GetBillingSchema returns the canonical task-billing schema pinned for a
// model. An empty value means the model remains on the legacy request-input
// path for backwards compatibility.
func GetBillingSchema(model string) string {
	billingSettingMu.RLock()
	defer billingSettingMu.RUnlock()
	return billingSetting.BillingSchema[model]
}

func GetBillingModeCopy() map[string]string {
	billingSettingMu.RLock()
	defer billingSettingMu.RUnlock()
	return lo.Assign(billingSetting.BillingMode)
}

func GetBillingExprCopy() map[string]string {
	billingSettingMu.RLock()
	defer billingSettingMu.RUnlock()
	return lo.Assign(billingSetting.BillingExpr)
}

func GetBillingSchemaCopy() map[string]string {
	billingSettingMu.RLock()
	defer billingSettingMu.RUnlock()
	return lo.Assign(billingSetting.BillingSchema)
}

// GetBillingSettingsCopy reads the three coupled maps under one lock. Callers
// that need more than one map must use this instead of taking independent
// copies, otherwise a concurrent atomic replacement can be observed as a
// mixed configuration.
func GetBillingSettingsCopy() (map[string]string, map[string]string, map[string]string) {
	billingSettingMu.RLock()
	defer billingSettingMu.RUnlock()
	return lo.Assign(billingSetting.BillingMode), lo.Assign(billingSetting.BillingExpr), lo.Assign(billingSetting.BillingSchema)
}

// GetBillingModelSettings returns a coherent configuration snapshot for one
// model. An empty expression or schema means the corresponding setting is not
// configured.
func GetBillingModelSettings(model string) (mode, expr, schema string) {
	billingSettingMu.RLock()
	defer billingSettingMu.RUnlock()
	mode = billingSetting.BillingMode[model]
	if mode == "" {
		mode = BillingModeRatio
	}
	return mode, billingSetting.BillingExpr[model], billingSetting.BillingSchema[model]
}

// ReplaceBillingSettings atomically swaps all model-level dynamic billing
// maps after the caller has validated them. Keeping the three maps together
// prevents a request from observing a new expression with an old schema.
func ReplaceBillingSettings(modes, exprs, schemas map[string]string) {
	billingSettingMu.Lock()
	defer billingSettingMu.Unlock()
	billingSetting.BillingMode = copyBillingMap(modes)
	billingSetting.BillingExpr = copyBillingMap(exprs)
	billingSetting.BillingSchema = copyBillingMap(schemas)
}

// UpdateBillingSettingOption is used by the generic option endpoint for
// backwards compatibility. The dedicated billing-model endpoint should use
// ReplaceBillingSettings so all three maps change as one unit.
func UpdateBillingSettingOption(key, value string) error {
	parsed := make(map[string]string)
	if err := common.UnmarshalJsonStr(value, &parsed); err != nil {
		return err
	}
	billingSettingMu.Lock()
	defer billingSettingMu.Unlock()
	switch key {
	case BillingModeField:
		billingSetting.BillingMode = parsed
	case BillingExprField:
		billingSetting.BillingExpr = parsed
	case BillingSchemaField:
		billingSetting.BillingSchema = parsed
	default:
		return fmt.Errorf("unknown billing setting option %q", key)
	}
	return nil
}

func copyBillingMap(source map[string]string) map[string]string {
	if len(source) == 0 {
		return make(map[string]string)
	}
	return lo.Assign(source)
}

func GetPricingSyncData(base map[string]any) map[string]any {
	extra := make(map[string]any, 2)
	modes, exprs, schemas := GetBillingSettingsCopy()
	if len(modes) > 0 {
		extra[BillingModeField] = modes
	}
	if len(exprs) > 0 {
		extra[BillingExprField] = exprs
	}
	if len(schemas) > 0 {
		extra[BillingSchemaField] = schemas
	}
	return lo.Assign(base, extra)
}

// ---------------------------------------------------------------------------
// Smoke test (called externally for validation before save)
// ---------------------------------------------------------------------------

func SmokeTestExpr(exprStr string) error {
	return smokeTestExpr(exprStr)
}

func smokeTestExpr(exprStr string) error {
	vectors := []billingexpr.TokenParams{
		{P: 0, C: 0, Len: 0},
		{P: 1000, C: 1000, Len: 1000},
		{P: 100000, C: 100000, Len: 100000},
		{P: 1000000, C: 1000000, Len: 1000000},
	}
	requests := []billingexpr.RequestInput{
		{},
		{
			Headers: map[string]string{
				"anthropic-beta": "fast-mode-2026-02-01",
			},
			Body: []byte(`{"service_tier":"fast","stream_options":{"include_usage":true},"messages":[1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,17,18,19,20,21]}`),
		},
	}

	for _, v := range vectors {
		for _, request := range requests {
			result, _, err := billingexpr.RunExprWithRequest(exprStr, v, request)
			if err != nil {
				return fmt.Errorf("vector {p=%g, c=%g}: run failed: %w", v.P, v.C, err)
			}
			if result < 0 {
				return fmt.Errorf("vector {p=%g, c=%g}: result %f < 0", v.P, v.C, result)
			}
		}
	}
	return nil
}
