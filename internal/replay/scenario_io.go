package replay

import (
	"encoding/json"
	"fmt"
)

// ExportScenario serialises s to pretty-printed JSON.
func ExportScenario(s Scenario) ([]byte, error) {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("export scenario: %w", err)
	}
	return data, nil
}

// ImportScenario parses a JSON blob produced by ExportScenario back into a
// Scenario value.
func ImportScenario(data []byte) (Scenario, error) {
	var s Scenario
	if err := json.Unmarshal(data, &s); err != nil {
		return Scenario{}, fmt.Errorf("import scenario: %w", err)
	}
	return s, nil
}
