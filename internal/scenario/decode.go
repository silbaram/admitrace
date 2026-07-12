package scenario

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/silbaram/admitrace/internal/contract"
	"k8s.io/apimachinery/pkg/util/yaml"
	jsonstrict "sigs.k8s.io/json"
)

// Decode converts one YAML or JSON document into a validated, defaulted Scenario.
//
// Strict decoding stops at the raw request object, oldObject, and options
// boundaries declared by contract.AdmissionRequest. This keeps resource-specific
// fields unstructured while retaining strictness for the Scenario envelope and
// the typed Kubernetes configuration.
func Decode(data []byte) (*contract.Scenario, error) {
	if !yaml.IsJSONBuffer(data) {
		// The YAML-to-JSON conversion is intentionally preflighted against an
		// untyped target. It catches ambiguous duplicate YAML keys without
		// applying the Scenario schema inside raw request payload boundaries.
		var document any
		if err := yaml.UnmarshalStrict(data, &document); err != nil {
			return nil, invalidInput(".", err)
		}
	}

	jsonData, err := yaml.ToJSON(data)
	if err != nil {
		return nil, invalidInput(".", err)
	}

	var decoded contract.Scenario
	strictErrors, err := jsonstrict.UnmarshalStrict(jsonData, &decoded)
	if err != nil {
		return nil, invalidInput(sourcePath(err), err)
	}
	if len(strictErrors) > 0 {
		return nil, invalidInput(sourcePath(strictErrors[0]), strictErrors[0])
	}
	if err := Validate(&decoded); err != nil {
		return nil, err
	}

	ApplyDefaults(&decoded)
	return &decoded, nil
}

func invalidInput(path string, err error) error {
	return &contract.InvalidInputError{Field: normalizedPath(path), Err: err}
}

func sourcePath(err error) string {
	var fieldErr jsonstrict.FieldError
	if errors.As(err, &fieldErr) {
		return fieldErr.FieldPath()
	}

	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &typeErr) && typeErr.Field != "" {
		return typeErr.Field
	}
	return "."
}

func normalizedPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" || path == "." {
		return "."
	}
	return "." + strings.TrimPrefix(path, ".")
}
