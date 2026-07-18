package manifest

import (
	"errors"
	"fmt"
	"strings"

	"github.com/silbaram/admitrace/internal/contract"
	admissionv1 "k8s.io/api/admission/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
)

// UserExtraHelp is the canonical CLI contract for admission user extras.
const UserExtraHelp = `admission user extra as key=value[,value...]; repeat to append values; escape '\', '=', and ',' with a backslash; empty and duplicate values are preserved`

// IdentityOptions contains only explicit admission identity flags. It has no
// kubeconfig, transport, or workload identity fallback field by design.
type IdentityOptions struct {
	User      string
	Groups    []string
	UID       string
	UserExtra []string
}

// Identity is an immutable explicit admission identity. Its zero value means
// that no identity was supplied.
type Identity struct {
	userInfo authenticationv1.UserInfo
	provided bool
}

// NewIdentity constructs exact UserInfo from explicit flags. Supplemental
// fields without --user are rejected rather than attached to inferred users.
func NewIdentity(options IdentityOptions) (Identity, error) {
	extra, err := ParseUserExtra(options.UserExtra)
	if err != nil {
		return Identity{}, err
	}
	hasSupplemental := len(options.Groups) > 0 || options.UID != "" || len(options.UserExtra) > 0
	if options.User == "" {
		if hasSupplemental {
			return Identity{}, &contract.InvalidInputError{Field: "user", Err: errors.New("--user is required when group, user-uid, or user-extra is supplied")}
		}
		return Identity{}, nil
	}
	return Identity{
		userInfo: authenticationv1.UserInfo{
			Username: options.User,
			UID:      options.UID,
			Groups:   append([]string(nil), options.Groups...),
			Extra:    extra,
		},
		provided: true,
	}, nil
}

// Provided reports whether --user explicitly established admission identity.
func (identity Identity) Provided() bool {
	return identity.provided
}

// UserInfo returns an owned copy of the exact explicit identity.
func (identity Identity) UserInfo() authenticationv1.UserInfo {
	return *identity.userInfoCopy()
}

func (identity Identity) userInfoCopy() *authenticationv1.UserInfo {
	return identity.userInfo.DeepCopy()
}

// ParseUserExtra parses repeated key=value[,value...] entries. Duplicate keys
// append and duplicate or empty values remain in flag order.
func ParseUserExtra(entries []string) (map[string]authenticationv1.ExtraValue, error) {
	if len(entries) == 0 {
		return nil, nil
	}
	result := make(map[string]authenticationv1.ExtraValue)
	for index, entry := range entries {
		key, values, err := parseUserExtraEntry(entry)
		if err != nil {
			return nil, &contract.InvalidInputError{Field: fmt.Sprintf("user-extra[%d]", index), Err: err}
		}
		result[key] = append(result[key], values...)
	}
	return result, nil
}

func parseUserExtraEntry(entry string) (string, authenticationv1.ExtraValue, error) {
	key := strings.Builder{}
	value := strings.Builder{}
	values := make(authenticationv1.ExtraValue, 0, 1)
	inValues := false
	escaped := false
	for _, character := range entry {
		if escaped {
			if character != '\\' && character != '=' && character != ',' {
				return "", nil, fmt.Errorf("unsupported escape \\%c", character)
			}
			if inValues {
				value.WriteRune(character)
			} else {
				key.WriteRune(character)
			}
			escaped = false
			continue
		}
		if character == '\\' {
			escaped = true
			continue
		}
		switch {
		case character == '=' && !inValues:
			inValues = true
		case character == '=':
			return "", nil, errors.New("unescaped '=' in value")
		case character == ',' && !inValues:
			return "", nil, errors.New("unescaped ',' in key")
		case character == ',':
			values = append(values, value.String())
			value.Reset()
		case inValues:
			value.WriteRune(character)
		default:
			key.WriteRune(character)
		}
	}
	if escaped {
		return "", nil, errors.New("dangling escape")
	}
	if !inValues {
		return "", nil, errors.New("expected key=value")
	}
	if key.Len() == 0 {
		return "", nil, errors.New("key must not be empty")
	}
	values = append(values, value.String())
	return key.String(), values, nil
}

// OperationError exposes the stable adapter diagnostic for a rejected
// non-CREATE operation while retaining unsupported-capability classification.
type OperationError struct {
	Diagnostic Diagnostic
	Err        error
}

// Error returns the operation guard failure.
func (err *OperationError) Error() string {
	if err == nil || err.Err == nil {
		return "unsupported manifest operation"
	}
	return err.Err.Error()
}

// Unwrap exposes the stable unsupported category.
func (err *OperationError) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.Err
}

// ValidateOperation applies the adapter v1 CREATE-only contract. An empty
// operation means the documented CREATE default.
func ValidateOperation(operation admissionv1.Operation) error {
	if operation == "" || operation == admissionv1.Create {
		return nil
	}
	unsupported := &contract.UnsupportedCapabilityError{
		Capability: "manifest operation",
		Err:        fmt.Errorf("operation %q is unsupported; only CREATE is supported and oldObject is never hydrated", operation),
	}
	return &OperationError{
		Diagnostic: Diagnostic{
			Code:     DiagnosticCodeUnsupportedOperation,
			Severity: contract.DiagnosticSeverityError,
			Message:  fmt.Sprintf("operation %s is unsupported; resource mode supports CREATE only", operation),
		},
		Err: unsupported,
	}
}
