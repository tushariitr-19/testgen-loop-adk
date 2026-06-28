// Package workspace owns the target Go file and its companion _test.go file
// on disk. It provides snapshot/append/restore semantics so the Selector
// agent can try a candidate test, measure its effect, and either keep the
// change or roll back cleanly. The package contains no LLM or agent code.
package workspace

import "time"

// Snapshot is a point-in-time capture of the test file's contents. A
// Snapshot lives on the Workspace's internal stack; it is consumed either
// by Restore (rollback) or Discard (commit). Callers receive a pointer
// returned from Snapshot only for introspection — restoration always goes
// through the Workspace so the stack stays consistent.
type Snapshot struct {
	// Path is the absolute path of the file the snapshot captured.
	Path string
	// Contents is the byte-for-byte snapshot of the file at capture time.
	Contents []byte
	// Size is len(Contents). Stored separately so logs can show the
	// number without copying the bytes.
	Size int64
	// TakenAt is the wall-clock time the snapshot was captured.
	TakenAt time.Time
}

// AppendResult describes the outcome of a successful AppendTest call.
type AppendResult struct {
	// TestName is the name of the appended test function.
	TestName string
	// BytesWritten is the number of bytes added to the test file
	// (final size minus original size).
	BytesWritten int
	// NewSize is the size of the test file after the append.
	NewSize int64
}
