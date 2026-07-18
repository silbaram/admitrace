package manifest

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/silbaram/admitrace/internal/contract"
	"github.com/silbaram/admitrace/internal/scenario"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/yaml"
	jsonstrict "sigs.k8s.io/json"
)

// InputMode identifies the compatibility boundary selected for decoded input.
type InputMode string

const (
	// InputModeLegacyScenario preserves the existing single-Scenario path.
	InputModeLegacyScenario InputMode = "legacy-scenario"
	// InputModeResource selects the manifest adapter path.
	InputModeResource InputMode = "resource"
)

// DocumentClass identifies supported typed adapter inputs.
type DocumentClass string

const (
	// DocumentClassResource identifies a generic Kubernetes resource.
	DocumentClassResource DocumentClass = "resource"
	// DocumentClassValidatingConfiguration identifies a validating webhook configuration.
	DocumentClassValidatingConfiguration DocumentClass = "validating-webhook-configuration"
	// DocumentClassMutatingConfiguration identifies a mutating webhook configuration.
	DocumentClassMutatingConfiguration DocumentClass = "mutating-webhook-configuration"
	// DocumentClassNamespace identifies a core Namespace object.
	DocumentClassNamespace DocumentClass = "namespace"
)

// Document is one strictly decoded Kubernetes manifest and its provenance.
// RawJSON is canonical JSON derived from the supplied document and never
// contains filesystem or credential provenance added by the decoder.
type Document struct {
	Source                  Source
	Class                   DocumentClass
	RawJSON                 json.RawMessage
	Object                  *unstructured.Unstructured
	ValidatingConfiguration *admissionregistrationv1.ValidatingWebhookConfiguration
	MutatingConfiguration   *admissionregistrationv1.MutatingWebhookConfiguration
	Namespace               *corev1.Namespace
}

// DecodedInput contains either the unchanged legacy Scenario or ordered
// resource-mode documents. Exactly one branch is populated.
type DecodedInput struct {
	Mode      InputMode
	Scenario  *contract.Scenario
	Documents []Document
}

// DocumentError associates an invalid input with a logical source and a
// 1-based document index. Directory-level errors use index zero.
type DocumentError struct {
	Source Source
	Err    error
}

// Error formats source provenance without exposing an absolute path.
func (err *DocumentError) Error() string {
	if err.Source.DocumentIndex > 0 {
		return fmt.Sprintf("%s document %d: %v", err.Source.Label, err.Source.DocumentIndex, err.Err)
	}
	return fmt.Sprintf("%s: %v", err.Source.Label, err.Err)
}

// Unwrap exposes the stable contract error category.
func (err *DocumentError) Unwrap() error {
	return err.Err
}

// Decode reads a YAML or JSON stream. A single exact Scenario selects legacy
// mode; every multi-document stream is resource mode and rejects Scenario
// documents.
func Decode(reader io.Reader, kind SourceKind, label string) (*DecodedInput, error) {
	if kind != SourceKindFile && kind != SourceKindStdin && kind != SourceKindDirectoryEntry {
		return nil, sourceError(kind, label, 0, fmt.Errorf("unsupported manifest source kind %q", kind))
	}
	return decode(reader, kind, logicalLabel(label, kind), true)
}

// DecodeFile reads one explicit file using a basename-only logical source.
func DecodeFile(path string) (*DecodedInput, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, sourceError(SourceKindFile, filepath.Base(path), 0, err)
	}
	defer file.Close()

	return Decode(file, SourceKindFile, filepath.Base(path))
}

// DecodeDirectory reads supported regular entries in lexical filename order.
// Directory mode is always resource mode and never follows a symlink directory.
func DecodeDirectory(path string) (*DecodedInput, error) {
	label := filepath.Base(filepath.Clean(path))
	info, err := os.Lstat(path)
	if err != nil {
		return nil, sourceError(SourceKindDirectoryEntry, label, 0, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, sourceError(SourceKindDirectoryEntry, label, 0, errors.New("symlink directory is not allowed"))
	}
	if !info.IsDir() {
		return nil, sourceError(SourceKindDirectoryEntry, label, 0, errors.New("input is not a directory"))
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, sourceError(SourceKindDirectoryEntry, label, 0, err)
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	documents := make([]Document, 0)
	for _, entry := range entries {
		if !supportedManifestName(entry.Name()) {
			continue
		}
		entryInfo, err := entry.Info()
		if err != nil {
			return nil, sourceError(SourceKindDirectoryEntry, entry.Name(), 0, err)
		}
		if !entryInfo.Mode().IsRegular() {
			continue
		}

		file, err := os.Open(filepath.Join(path, entry.Name()))
		if err != nil {
			return nil, sourceError(SourceKindDirectoryEntry, entry.Name(), 0, err)
		}
		decoded, decodeErr := decode(file, SourceKindDirectoryEntry, entry.Name(), false)
		closeErr := file.Close()
		if decodeErr != nil {
			return nil, decodeErr
		}
		if closeErr != nil {
			return nil, sourceError(SourceKindDirectoryEntry, entry.Name(), 0, closeErr)
		}
		documents = append(documents, decoded.Documents...)
		if err := scenario.CheckDocumentCount(len(documents)); err != nil {
			return nil, sourceError(SourceKindDirectoryEntry, label, 0, err)
		}
	}
	if len(documents) == 0 {
		return nil, sourceError(SourceKindDirectoryEntry, label, 0, errors.New("directory contains no regular YAML or JSON documents"))
	}
	return &DecodedInput{Mode: InputModeResource, Documents: documents}, nil
}

// DecodePath selects explicit-file or directory behavior without following a
// symlink directory.
func DecodePath(path string) (*DecodedInput, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, sourceError(SourceKindFile, filepath.Base(path), 0, err)
	}
	if info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		if info.Mode()&os.ModeSymlink != 0 {
			target, statErr := os.Stat(path)
			if statErr == nil && target.IsDir() {
				return DecodeDirectory(path)
			}
		}
		if info.IsDir() {
			return DecodeDirectory(path)
		}
	}
	return DecodeFile(path)
}

func decode(reader io.Reader, kind SourceKind, label string, allowLegacy bool) (*DecodedInput, error) {
	type rawDocument struct {
		source Source
		raw    []byte
		object *unstructured.Unstructured
	}
	rawInputs, err := splitDocuments(reader)
	if err != nil {
		return nil, sourceError(kind, label, err.documentIndex, err.err)
	}
	rawDocuments := make([]rawDocument, 0, len(rawInputs))
	for offset, raw := range rawInputs {
		index := offset + 1
		source := Source{Kind: kind, Label: label, DocumentIndex: index}
		object, err := decodeObject(raw)
		if err != nil {
			return nil, &DocumentError{Source: source, Err: err}
		}
		rawDocuments = append(rawDocuments, rawDocument{source: source, raw: raw, object: object})
		if err := scenario.CheckDocumentCount(len(rawDocuments)); err != nil {
			return nil, &DocumentError{Source: source, Err: err}
		}
	}
	if len(rawDocuments) == 0 {
		return nil, sourceError(kind, label, 1, invalidInput(".", errors.New("empty document")))
	}

	if len(rawDocuments) == 1 && isScenario(rawDocuments[0].object) {
		if !allowLegacy {
			return nil, &DocumentError{Source: rawDocuments[0].source, Err: invalidInput(".kind", errors.New("Scenario is only allowed as a single explicit file or stdin document"))}
		}
		decoded, err := scenario.Decode(rawDocuments[0].raw)
		if err != nil {
			return nil, &DocumentError{Source: rawDocuments[0].source, Err: err}
		}
		return &DecodedInput{Mode: InputModeLegacyScenario, Scenario: decoded}, nil
	}

	documents := make([]Document, 0, len(rawDocuments))
	for _, rawDocument := range rawDocuments {
		if isScenario(rawDocument.object) {
			return nil, &DocumentError{Source: rawDocument.source, Err: invalidInput(".kind", errors.New("Scenario cannot be mixed into resource-mode input"))}
		}
		document, err := classifyDocument(rawDocument.source, rawDocument.raw, rawDocument.object)
		if err != nil {
			return nil, &DocumentError{Source: rawDocument.source, Err: err}
		}
		documents = append(documents, document)
	}
	return &DecodedInput{Mode: InputModeResource, Documents: documents}, nil
}

type documentSplitError struct {
	documentIndex int
	err           error
}

func splitDocuments(reader io.Reader) ([][]byte, *documentSplitError) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), scenario.MaximumDocumentBytes+2)
	documents := make([][]byte, 0, 1)
	current := make([]byte, 0)
	sawSeparator := false
	for scanner.Scan() {
		line := scanner.Text()
		if isYAMLDocumentSeparator(line) {
			if !sawSeparator && documentIsEmpty(current) {
				current = current[:0]
				sawSeparator = true
				continue
			}
			documents = append(documents, append([]byte(nil), current...))
			if err := scenario.CheckDocumentCount(len(documents)); err != nil {
				return nil, &documentSplitError{documentIndex: len(documents), err: err}
			}
			current = current[:0]
			sawSeparator = true
			continue
		}
		current = append(current, line...)
		current = append(current, '\n')
		if len(current) > scenario.MaximumDocumentBytes {
			return nil, &documentSplitError{
				documentIndex: len(documents) + 1,
				err: &contract.ResourceLimitError{
					Field:    ".",
					Resource: "document bytes",
					Actual:   len(current),
					Limit:    scenario.MaximumDocumentBytes,
				},
			}
		}
	}
	if err := scanner.Err(); err != nil {
		if strings.Contains(err.Error(), "token too long") {
			return nil, &documentSplitError{
				documentIndex: len(documents) + 1,
				err: &contract.ResourceLimitError{
					Field:    ".",
					Resource: "document bytes",
					Actual:   scenario.MaximumDocumentBytes + 1,
					Limit:    scenario.MaximumDocumentBytes,
				},
			}
		}
		return nil, &documentSplitError{documentIndex: len(documents) + 1, err: fmt.Errorf("read YAML document: %w", err)}
	}
	if sawSeparator || len(current) > 0 {
		documents = append(documents, append([]byte(nil), current...))
	}
	return documents, nil
}

func isYAMLDocumentSeparator(line string) bool {
	if !strings.HasPrefix(line, "---") {
		return false
	}
	remainder := strings.TrimSpace(strings.TrimPrefix(line, "---"))
	return remainder == "" || strings.HasPrefix(remainder, "#")
}

func documentIsEmpty(data []byte) bool {
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			return false
		}
	}
	return true
}

func decodeObject(data []byte) (*unstructured.Unstructured, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, invalidInput(".", errors.New("empty document"))
	}
	if err := scenario.CheckDocumentLimits(data); err != nil {
		return nil, err
	}
	if !yaml.IsJSONBuffer(data) {
		var untyped any
		if err := yaml.UnmarshalStrict(data, &untyped); err != nil {
			return nil, invalidInput(".", err)
		}
		if err := scenario.CheckDecodedDepth(untyped); err != nil {
			return nil, err
		}
	}

	jsonData, err := yaml.ToJSON(data)
	if err != nil {
		return nil, invalidInput(".", err)
	}
	var object map[string]any
	strictErrors, err := jsonstrict.UnmarshalStrict(jsonData, &object)
	if err != nil {
		return nil, invalidInput(strictSourcePath(err), err)
	}
	if len(strictErrors) > 0 {
		return nil, invalidInput(strictSourcePath(strictErrors[0]), strictErrors[0])
	}
	if object == nil {
		return nil, invalidInput(".", errors.New("document must be a Kubernetes object"))
	}
	if err := scenario.CheckDecodedDepth(object); err != nil {
		return nil, err
	}
	result := &unstructured.Unstructured{Object: object}
	if result.GetAPIVersion() == "" {
		return nil, invalidInput(".apiVersion", errors.New("apiVersion is required"))
	}
	if result.GetKind() == "" {
		return nil, invalidInput(".kind", errors.New("kind is required"))
	}
	return result, nil
}

func classifyDocument(source Source, raw []byte, object *unstructured.Unstructured) (Document, error) {
	jsonData, err := yaml.ToJSON(raw)
	if err != nil {
		return Document{}, invalidInput(".", err)
	}
	document := Document{
		Source:  source,
		Class:   DocumentClassResource,
		RawJSON: append(json.RawMessage(nil), jsonData...),
		Object:  object,
	}

	switch object.GetKind() {
	case "ValidatingWebhookConfiguration":
		if object.GetAPIVersion() != "admissionregistration.k8s.io/v1" {
			return Document{}, invalidInput(".apiVersion", fmt.Errorf("unsupported ValidatingWebhookConfiguration apiVersion %q", object.GetAPIVersion()))
		}
		var configuration admissionregistrationv1.ValidatingWebhookConfiguration
		if err := strictTypedJSON(jsonData, &configuration); err != nil {
			return Document{}, err
		}
		document.Class = DocumentClassValidatingConfiguration
		document.ValidatingConfiguration = &configuration
	case "MutatingWebhookConfiguration":
		if object.GetAPIVersion() != "admissionregistration.k8s.io/v1" {
			return Document{}, invalidInput(".apiVersion", fmt.Errorf("unsupported MutatingWebhookConfiguration apiVersion %q", object.GetAPIVersion()))
		}
		var configuration admissionregistrationv1.MutatingWebhookConfiguration
		if err := strictTypedJSON(jsonData, &configuration); err != nil {
			return Document{}, err
		}
		document.Class = DocumentClassMutatingConfiguration
		document.MutatingConfiguration = &configuration
	case "Namespace":
		if object.GetAPIVersion() != "v1" {
			return Document{}, invalidInput(".apiVersion", fmt.Errorf("unsupported Namespace apiVersion %q", object.GetAPIVersion()))
		}
		var namespace corev1.Namespace
		if err := strictTypedJSON(jsonData, &namespace); err != nil {
			return Document{}, err
		}
		document.Class = DocumentClassNamespace
		document.Namespace = &namespace
	}
	return document, nil
}

func strictTypedJSON(data []byte, target any) error {
	strictErrors, err := jsonstrict.UnmarshalStrict(data, target)
	if err != nil {
		return invalidInput(strictSourcePath(err), err)
	}
	if len(strictErrors) > 0 {
		return invalidInput(strictSourcePath(strictErrors[0]), strictErrors[0])
	}
	return nil
}

func isScenario(object *unstructured.Unstructured) bool {
	return contract.IsSupportedScenarioVersion(object.GetAPIVersion(), object.GetKind())
}

func sourceError(kind SourceKind, label string, documentIndex int, err error) error {
	err = sanitizedFilesystemError(err)
	var categorized *contract.InvalidInputError
	if !errors.As(err, &categorized) {
		err = invalidInput(".", err)
	}
	return &DocumentError{
		Source: Source{Kind: kind, Label: logicalLabel(label, kind), DocumentIndex: documentIndex},
		Err:    err,
	}
}

func sanitizedFilesystemError(err error) error {
	var pathError *os.PathError
	if errors.As(err, &pathError) {
		return fmt.Errorf("%s input: %w", pathError.Op, pathError.Err)
	}
	var linkError *os.LinkError
	if errors.As(err, &linkError) {
		return fmt.Errorf("%s input: %w", linkError.Op, linkError.Err)
	}
	return err
}

func invalidInput(field string, err error) error {
	return &contract.InvalidInputError{Field: normalizedPath(field), Err: err}
}

func strictSourcePath(err error) string {
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

func logicalLabel(label string, kind SourceKind) string {
	label = strings.TrimSpace(label)
	if kind == SourceKindStdin || label == "-" {
		return "stdin"
	}
	if label == "" {
		return "input"
	}
	return filepath.Base(filepath.Clean(label))
}

func supportedManifestName(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".yaml", ".yml", ".json":
		return true
	default:
		return false
	}
}
