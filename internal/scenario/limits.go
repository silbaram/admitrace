package scenario

import (
	"bufio"
	"bytes"
	"fmt"
	"reflect"
	"strings"

	"github.com/silbaram/admitrace/internal/contract"
)

const (
	// MaximumDocumentBytes is the maximum encoded size of one Scenario.
	MaximumDocumentBytes = 1 << 20
	// MaximumDocumentDepth is the maximum container nesting depth of one Scenario.
	MaximumDocumentDepth = 100
	// MaximumScenarioDocuments is the maximum number of files accepted by one test invocation.
	MaximumScenarioDocuments = 1000
)

func checkDocumentLimits(data []byte) error {
	if len(data) > MaximumDocumentBytes {
		return resourceLimit(".", "document bytes", len(data), MaximumDocumentBytes)
	}
	depth, err := lexicalDepth(data)
	if err != nil {
		return invalidInput(".", fmt.Errorf("scan document nesting: %w", err))
	}
	if depth > MaximumDocumentDepth {
		return resourceLimit(".", "document nesting depth", depth, MaximumDocumentDepth)
	}
	return nil
}

// CheckDocumentLimits applies the encoded size and nesting limits shared by
// Scenario and manifest input decoders.
func CheckDocumentLimits(data []byte) error {
	return checkDocumentLimits(data)
}

func checkDecodedDepth(document any) error {
	if depth := valueDepth(reflect.ValueOf(document), 0); depth > MaximumDocumentDepth {
		return resourceLimit(".", "document nesting depth", depth, MaximumDocumentDepth)
	}
	return nil
}

// CheckDecodedDepth applies the decoded container nesting limit shared by
// Scenario and manifest input decoders.
func CheckDecodedDepth(document any) error {
	return checkDecodedDepth(document)
}

func lexicalDepth(data []byte) (int, error) {
	flowDepth := 0
	maximum := 0
	quote := byte(0)
	escaped := false
	comment := false
	for _, character := range data {
		if comment {
			if character == '\n' {
				comment = false
			}
			continue
		}
		if quote != 0 {
			if quote == '"' && escaped {
				escaped = false
				continue
			}
			if quote == '"' && character == '\\' {
				escaped = true
				continue
			}
			if character == quote {
				quote = 0
			}
			continue
		}
		switch character {
		case '#':
			comment = true
		case '\'', '"':
			quote = character
		case '{', '[':
			flowDepth++
			if flowDepth > maximum {
				maximum = flowDepth
			}
		case '}', ']':
			if flowDepth > 0 {
				flowDepth--
			}
		}
	}

	blockDepth, err := yamlBlockDepth(data)
	if err != nil {
		return 0, err
	}
	if blockDepth > maximum {
		return blockDepth, nil
	}
	return maximum, nil
}

func yamlBlockDepth(data []byte) (int, error) {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 64*1024), MaximumDocumentBytes+1)
	indents := []int{-1}
	maximum := 0
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), " \t\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		for len(indents) > 1 && indent <= indents[len(indents)-1] {
			indents = indents[:len(indents)-1]
		}
		if indent > indents[len(indents)-1] {
			indents = append(indents, indent)
		}
		depth := len(indents) - 1
		depth += leadingSequenceDepth(trimmed)
		if depth > maximum {
			maximum = depth
		}
	}
	if err := scanner.Err(); err != nil {
		return 0, fmt.Errorf("scan YAML lines: %w", err)
	}
	return maximum, nil
}

func leadingSequenceDepth(line string) int {
	depth := 0
	for strings.HasPrefix(line, "- ") {
		depth++
		line = strings.TrimSpace(strings.TrimPrefix(line, "- "))
	}
	return depth
}

func valueDepth(value reflect.Value, depth int) int {
	if !value.IsValid() {
		return depth
	}
	if value.Kind() == reflect.Interface || value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return depth
		}
		return valueDepth(value.Elem(), depth)
	}
	if value.Kind() != reflect.Map && value.Kind() != reflect.Slice && value.Kind() != reflect.Array {
		return depth
	}
	depth++
	if depth > MaximumDocumentDepth {
		return depth
	}
	maximum := depth
	switch value.Kind() {
	case reflect.Map:
		iterator := value.MapRange()
		for iterator.Next() {
			if candidate := valueDepth(iterator.Value(), depth); candidate > maximum {
				maximum = candidate
			}
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < value.Len(); i++ {
			if candidate := valueDepth(value.Index(i), depth); candidate > maximum {
				maximum = candidate
			}
		}
	}
	return maximum
}

func resourceLimit(field, resource string, actual, limit int) error {
	return &contract.ResourceLimitError{
		Field:    field,
		Resource: resource,
		Actual:   actual,
		Limit:    limit,
	}
}

func documentCountLimit(actual int) error {
	return resourceLimit("paths", "Scenario documents", actual, MaximumScenarioDocuments)
}

// CheckDocumentCount rejects a test invocation that exceeds the deterministic
// Scenario document limit.
func CheckDocumentCount(actual int) error {
	if actual < 0 {
		return &contract.InvalidInputError{Field: "paths", Err: fmt.Errorf("document count must not be negative")}
	}
	if actual > MaximumScenarioDocuments {
		return documentCountLimit(actual)
	}
	return nil
}
