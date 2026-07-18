// Package snapshot writes portable offline Scenario bundles under an
// exact-copy-or-refuse policy.
package snapshot

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"

	"github.com/silbaram/admitrace/internal/contract"
	"github.com/silbaram/admitrace/internal/manifest"
	"github.com/silbaram/admitrace/internal/scenario"
	"sigs.k8s.io/yaml"
)

// PolicyNotice describes persistence behavior that callers should expose in
// CLI help whenever snapshot export is available.
const PolicyNotice = "exact-copy snapshots refuse core/v1 Secret, preserve explicit UserInfo and general resource payloads without field redaction, and do not detect generic secrets in custom resources"

const (
	directoryMode os.FileMode = 0o700
	fileMode      os.FileMode = 0o600
)

var snapshotNamePattern = regexp.MustCompile(`^[0-9]{4}-[0-9]{4}\.yaml$`)

// ErrorKind identifies why a requested bundle was refused.
type ErrorKind string

const (
	// ErrorKindPolicy identifies a resource forbidden by SnapshotPolicy.
	ErrorKindPolicy ErrorKind = "policy"
	// ErrorKindDestination identifies a non-empty, unsafe, or invalid target.
	ErrorKindDestination ErrorKind = "destination"
	// ErrorKindCollision identifies a duplicate or unsafe generated filename.
	ErrorKindCollision ErrorKind = "collision"
	// ErrorKindStage identifies failure before publication completed.
	ErrorKindStage ErrorKind = "stage"
	// ErrorKindPublish identifies failure to atomically claim the target.
	ErrorKindPublish ErrorKind = "publish"
)

// Error is a stable snapshot refusal that never includes resource payloads or
// connection metadata in its display string.
type Error struct {
	Kind     ErrorKind
	Resource manifest.Source
	Err      error
}

// Error returns a stable refusal summary.
func (err *Error) Error() string {
	if err == nil {
		return "snapshot export refused"
	}
	return "snapshot export refused: " + string(err.Kind)
}

// Unwrap exposes the underlying diagnostic cause.
func (err *Error) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.Err
}

// Is classifies every refusal as invalid requested snapshot input.
func (err *Error) Is(target error) bool {
	return target == contract.ErrInvalidInput
}

// Diagnostic returns the stable adapter-level refusal for rendering.
func (err *Error) Diagnostic() manifest.Diagnostic {
	message := "snapshot export was refused"
	switch err.Kind {
	case ErrorKindPolicy:
		if err.Resource.Label != "" {
			message += ": core/v1 Secret cannot be persisted; " + PolicyNotice
		} else {
			message += ": there are no policy-allowed Scenarios to persist"
		}
	case ErrorKindDestination:
		message += ": target must be a non-existent or empty directory"
	case ErrorKindCollision:
		message += ": deterministic snapshot filenames collided"
	case ErrorKindStage, ErrorKindPublish:
		message += ": no partial bundle was published"
	}
	return manifest.Diagnostic{
		Code:          manifest.DiagnosticCodeSnapshotRefused,
		Severity:      contract.DiagnosticSeverityWarning,
		Message:       message,
		SourceLabel:   err.Resource.Label,
		DocumentIndex: err.Resource.DocumentIndex,
	}
}

type operations struct {
	lstat     func(string) (os.FileInfo, error)
	readDir   func(string) ([]os.DirEntry, error)
	mkdirTemp func(string, string) (string, error)
	chmod     func(string, os.FileMode) error
	writeFile func(string, []byte, os.FileMode) error
	rename    func(string, string) error
	remove    func(string) error
	removeAll func(string) error
	syncDir   func(string) error
}

// Writer publishes a complete bundle only after every policy and staged-write
// check passes.
type Writer struct {
	operations operations
}

// NewWriter creates a filesystem-backed atomic snapshot writer.
func NewWriter() Writer {
	return Writer{operations: operations{
		lstat:     os.Lstat,
		readDir:   os.ReadDir,
		mkdirTemp: os.MkdirTemp,
		chmod:     os.Chmod,
		writeFile: writeSyncedFile,
		rename:    os.Rename,
		remove:    os.Remove,
		removeAll: os.RemoveAll,
		syncDir:   syncDirectory,
	}}
}

// Write validates, stages, and atomically publishes deterministic Scenario
// files. Existing non-empty targets and all symlinks are refused.
func (writer Writer) Write(target string, built []manifest.BuiltScenario) error {
	entries, err := prepareEntries(built)
	if err != nil {
		return err
	}
	if target == "" {
		return &Error{Kind: ErrorKindDestination, Err: errors.New("snapshot target is required")}
	}
	if !writer.valid() {
		return &Error{Kind: ErrorKindStage, Err: errors.New("snapshot writer is not initialized")}
	}
	parent := filepath.Dir(filepath.Clean(target))
	parentInfo, err := writer.operations.lstat(parent)
	if err != nil || !parentInfo.IsDir() || parentInfo.Mode()&os.ModeSymlink != 0 {
		return &Error{Kind: ErrorKindDestination, Err: errors.New("snapshot parent must be an existing non-symlink directory")}
	}
	targetExists, err := writer.validateTarget(target)
	if err != nil {
		return err
	}
	stage, err := writer.operations.mkdirTemp(parent, ".admitrace-snapshot-stage-")
	if err != nil {
		return &Error{Kind: ErrorKindStage, Err: err}
	}
	stageOwned := true
	defer func() {
		if stageOwned {
			_ = writer.operations.removeAll(stage)
		}
	}()
	if err := writer.operations.chmod(stage, directoryMode); err != nil {
		return &Error{Kind: ErrorKindStage, Err: err}
	}
	for _, entry := range entries {
		if err := writer.operations.writeFile(filepath.Join(stage, entry.name), entry.data, fileMode); err != nil {
			return &Error{Kind: ErrorKindStage, Resource: entry.source, Err: err}
		}
	}
	if err := writer.operations.syncDir(stage); err != nil {
		return &Error{Kind: ErrorKindStage, Err: err}
	}

	if !targetExists {
		if err := writer.operations.rename(stage, target); err != nil {
			return &Error{Kind: ErrorKindPublish, Err: err}
		}
		stageOwned = false
		if err := writer.operations.syncDir(parent); err != nil {
			return &Error{Kind: ErrorKindPublish, Err: err}
		}
		return nil
	}
	if err := writer.replaceEmptyTarget(parent, target, stage); err != nil {
		return err
	}
	stageOwned = false
	return nil
}

type entry struct {
	name   string
	data   []byte
	source manifest.Source
}

func prepareEntries(built []manifest.BuiltScenario) ([]entry, error) {
	if len(built) == 0 {
		return nil, &Error{Kind: ErrorKindPolicy, Err: errors.New("at least one Scenario is required")}
	}
	entries := make([]entry, 0, len(built))
	seen := make(map[string]struct{}, len(built))
	for index, item := range built {
		if isCoreSecret(item) {
			return nil, &Error{Kind: ErrorKindPolicy, Resource: item.Resource, Err: errors.New("core/v1 Secret is not snapshot-safe")}
		}
		if !snapshotNamePattern.MatchString(item.SnapshotName) || filepath.Base(item.SnapshotName) != item.SnapshotName {
			return nil, &Error{Kind: ErrorKindCollision, Resource: item.Resource, Err: fmt.Errorf("invalid snapshot filename at item %d", index)}
		}
		if _, exists := seen[item.SnapshotName]; exists {
			return nil, &Error{Kind: ErrorKindCollision, Resource: item.Resource, Err: errors.New("duplicate snapshot filename")}
		}
		seen[item.SnapshotName] = struct{}{}
		if err := scenario.Validate(&item.Scenario); err != nil {
			return nil, &Error{Kind: ErrorKindPolicy, Resource: item.Resource, Err: fmt.Errorf("validate Scenario: %w", err)}
		}
		data, err := yaml.Marshal(item.Scenario)
		if err != nil {
			return nil, &Error{Kind: ErrorKindStage, Resource: item.Resource, Err: fmt.Errorf("marshal Scenario: %w", err)}
		}
		entries = append(entries, entry{name: item.SnapshotName, data: data, source: item.Resource})
	}
	return entries, nil
}

func isCoreSecret(item manifest.BuiltScenario) bool {
	kind := item.Scenario.Request.Kind
	return kind.Group == "" && kind.Version == "v1" && kind.Kind == "Secret"
}

func (writer Writer) validateTarget(target string) (bool, error) {
	info, err := writer.operations.lstat(target)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, &Error{Kind: ErrorKindDestination, Err: err}
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return false, &Error{Kind: ErrorKindDestination, Err: errors.New("snapshot target must be a non-symlink directory")}
	}
	entries, err := writer.operations.readDir(target)
	if err != nil {
		return false, &Error{Kind: ErrorKindDestination, Err: err}
	}
	if len(entries) != 0 {
		return false, &Error{Kind: ErrorKindDestination, Err: errors.New("snapshot target is not empty")}
	}
	return true, nil
}

func (writer Writer) replaceEmptyTarget(parent, target, stage string) error {
	backup, err := writer.operations.mkdirTemp(parent, ".admitrace-snapshot-empty-")
	if err != nil {
		return &Error{Kind: ErrorKindPublish, Err: err}
	}
	if err := writer.operations.remove(backup); err != nil {
		_ = writer.operations.removeAll(backup)
		return &Error{Kind: ErrorKindPublish, Err: err}
	}
	if err := writer.operations.rename(target, backup); err != nil {
		return &Error{Kind: ErrorKindPublish, Err: err}
	}
	if err := writer.operations.rename(stage, target); err != nil {
		rollbackErr := writer.operations.rename(backup, target)
		if rollbackErr != nil {
			return &Error{Kind: ErrorKindPublish, Err: errors.Join(err, fmt.Errorf("restore empty target: %w", rollbackErr))}
		}
		return &Error{Kind: ErrorKindPublish, Err: err}
	}
	if err := writer.operations.removeAll(backup); err != nil {
		return &Error{Kind: ErrorKindPublish, Err: err}
	}
	if err := writer.operations.syncDir(parent); err != nil {
		return &Error{Kind: ErrorKindPublish, Err: err}
	}
	return nil
}

func (writer Writer) valid() bool {
	operations := writer.operations
	return operations.lstat != nil && operations.readDir != nil && operations.mkdirTemp != nil && operations.chmod != nil &&
		operations.writeFile != nil && operations.rename != nil && operations.remove != nil && operations.removeAll != nil && operations.syncDir != nil
}

func writeSyncedFile(path string, data []byte, mode os.FileMode) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	written, writeErr := file.Write(data)
	if writeErr == nil && written != len(data) {
		writeErr = io.ErrShortWrite
	}
	syncErr := file.Sync()
	closeErr := file.Close()
	return errors.Join(writeErr, syncErr, closeErr)
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	syncErr := directory.Sync()
	closeErr := directory.Close()
	if errors.Is(syncErr, os.ErrInvalid) || errors.Is(syncErr, io.ErrUnexpectedEOF) {
		syncErr = nil
	}
	return errors.Join(syncErr, closeErr)
}
