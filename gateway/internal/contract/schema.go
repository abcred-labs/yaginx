package contract

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
)

import jsonschema "github.com/santhosh-tekuri/jsonschema/v6"

const runtimeSchemaID = "https://yaginx.local/contracts/runtime-config.schema.json"

// ValidateRuntimeSchema compiles the authoritative Draft 2020-12 schema,
// including every local $ref and vocabulary used by the runtime contract.
func ValidateRuntimeSchema(path string) error {
	contents, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var header struct {
		Schema string `json:"$schema"`
		ID     string `json:"$id"`
	}
	if err := json.Unmarshal(contents, &header); err != nil {
		return fmt.Errorf("parse runtime schema JSON: %w", err)
	}
	if header.Schema != "https://json-schema.org/draft/2020-12/schema" {
		return fmt.Errorf("$schema = %q, want Draft 2020-12", header.Schema)
	}
	if header.ID != runtimeSchemaID {
		return fmt.Errorf("$id = %q, want %q", header.ID, runtimeSchemaID)
	}
	document, err := jsonschema.UnmarshalJSON(bytes.NewReader(contents))
	if err != nil {
		return fmt.Errorf("decode runtime schema: %w", err)
	}
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource(header.ID, document); err != nil {
		return fmt.Errorf("add runtime schema: %w", err)
	}
	if _, err := compiler.Compile(header.ID); err != nil {
		return fmt.Errorf("compile runtime schema: %w", err)
	}
	return nil
}
