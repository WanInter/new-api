package common

// TaskBillingField describes one stable dimension that a task adaptor can
// provide to a canonical billing expression. Paths are rooted at billing.
type TaskBillingField struct {
	Path       string   `json:"path"`
	Type       string   `json:"type"`
	Required   bool     `json:"required"`
	EnumValues []string `json:"enum_values,omitempty"`
}

// TaskBillingCapability is the schema contract used to decide whether a
// model-level dynamic billing expression is safe across all of its channels.
// A schema version must only be reused for identical field semantics.
type TaskBillingCapability struct {
	SchemaVersion string             `json:"schema_version"`
	Fields        []TaskBillingField `json:"fields"`
}
