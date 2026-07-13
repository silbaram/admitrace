package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/silbaram/admitrace/internal/contract"
	"github.com/silbaram/admitrace/internal/scenario"
)

func TestResourceLimitsBoundReaderBeforeEndOfInput(t *testing.T) {
	t.Parallel()

	input := bytes.NewReader(bytes.Repeat([]byte("x"), scenario.MaximumDocumentBytes+128))
	_, err := readLimitedScenario(input, "-", "read stdin")
	if !errors.Is(err, contract.ErrResourceLimit) || !errors.Is(err, contract.ErrInvalidInput) {
		t.Fatalf("readLimitedScenario() error = %v, want invalid resource limit", err)
	}
	var limit *contract.ResourceLimitError
	if !errors.As(err, &limit) {
		t.Fatalf("readLimitedScenario() error type = %T, want *contract.ResourceLimitError", err)
	}
	if limit.Actual != scenario.MaximumDocumentBytes+1 {
		t.Errorf("ResourceLimitError.Actual = %d, want bounded observation %d", limit.Actual, scenario.MaximumDocumentBytes+1)
	}
	if got := input.Len(); got != 127 {
		t.Errorf("unread input bytes = %d, want 127", got)
	}
}

func TestResourceLimitsDiscoveryRejectsTooManyDocuments(t *testing.T) {
	root := t.TempDir()
	for i := 0; i <= scenario.MaximumScenarioDocuments; i++ {
		path := filepath.Join(root, fmt.Sprintf("%04d.yaml", i))
		if err := os.WriteFile(path, []byte("fixture"), 0o600); err != nil {
			t.Fatalf("os.WriteFile(%q) error = %v", path, err)
		}
	}

	got := discoverScenarios([]string{root})
	if len(got) != 1 {
		t.Fatalf("discoverScenarios() length = %d, want one limit diagnostic", len(got))
	}
	if !errors.Is(got[0].err, contract.ErrResourceLimit) {
		t.Errorf("discoverScenarios() error = %v, want ErrResourceLimit", got[0].err)
	}
}

func TestResourceLimitsReadAndClosePreservesReadErrorPriority(t *testing.T) {
	t.Parallel()

	readCause := errors.New("read failed")
	closeCause := errors.New("close failed")
	input := &testReadCloser{reader: testErrorReader{err: readCause}, closeErr: closeCause}
	_, err := readAndCloseScenario(input, "scenario.yaml", "read Scenario file")
	if !errors.Is(err, readCause) {
		t.Errorf("readAndCloseScenario() error = %v, want wrapped read error %v", err, readCause)
	}
	if !errors.Is(err, closeCause) {
		t.Errorf("readAndCloseScenario() error = %v, want wrapped close error %v", err, closeCause)
	}
	if !input.closed {
		t.Error("readAndCloseScenario() closed = false, want true")
	}
}

func TestResourceLimitsReadAndCloseReportsCloseError(t *testing.T) {
	t.Parallel()

	closeCause := errors.New("close failed")
	input := &testReadCloser{reader: bytes.NewBufferString("scenario"), closeErr: closeCause}
	_, err := readAndCloseScenario(input, "scenario.yaml", "read Scenario file")
	if !errors.Is(err, closeCause) {
		t.Errorf("readAndCloseScenario() error = %v, want wrapped close error %v", err, closeCause)
	}
	if !errors.Is(err, contract.ErrInvalidInput) {
		t.Errorf("readAndCloseScenario() error = %v, want ErrInvalidInput", err)
	}
	if !input.closed {
		t.Error("readAndCloseScenario() closed = false, want true")
	}
}

type testReadCloser struct {
	reader   io.Reader
	closeErr error
	closed   bool
}

type testErrorReader struct {
	err error
}

func (reader testErrorReader) Read(_ []byte) (int, error) {
	return 0, reader.err
}

func (input *testReadCloser) Read(buffer []byte) (int, error) {
	return input.reader.Read(buffer)
}

func (input *testReadCloser) Close() error {
	input.closed = true
	return input.closeErr
}
