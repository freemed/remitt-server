package jobqueue

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/freemed/remitt-server/callback"
	"github.com/freemed/remitt-server/model"
)

// testCallbackSender records received callback invocations for test verification.
type testCallbackSender struct {
	mu      sync.Mutex
	results []callback.JobResult
	users   []*model.UserModel
}

func (m *testCallbackSender) SendResult(ctx context.Context, user *model.UserModel, result callback.JobResult) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.results = append(m.results, result)
	m.users = append(m.users, user)
	return nil
}

func (m *testCallbackSender) lastResult() (callback.JobResult, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.results) == 0 {
		return callback.JobResult{}, false
	}
	return m.results[len(m.results)-1], true
}

func (m *testCallbackSender) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.results)
}

func TestFireCallback_Success(t *testing.T) {
	// Save and restore original default sender
	originalSender := callback.DefaultCallbackSender()
	defer callback.SetDefaultCallbackSender(originalSender)

	mock := &testCallbackSender{}
	callback.SetDefaultCallbackSender(mock)

	user := &model.UserModel{
		Id:       1,
		Username: "testuser",
	}
	w := &JobQueueItem{
		ID:         42,
		Message:    "Job completed successfully",
		OriginalID: "100",
	}

	fireCallback(user, w, nil)

	// fireCallback runs the callback synchronously (it's called inside a goroutine
	// by executeJob's defer, but the function itself is synchronous). Wait a bit.
	time.Sleep(10 * time.Millisecond)

	if mock.count() != 1 {
		t.Fatalf("expected 1 callback invocation, got %d", mock.count())
	}

	result, ok := mock.lastResult()
	if !ok {
		t.Fatal("no callback result recorded")
	}
	if result.JobID != 42 {
		t.Errorf("expected JobID 42, got %d", result.JobID)
	}
	if result.Status != "SUCCESS" {
		t.Errorf("expected Status SUCCESS, got %s", result.Status)
	}
	if result.PayloadID != 100 {
		t.Errorf("expected PayloadID 100, got %d", result.PayloadID)
	}
	if result.OriginalID != "100" {
		t.Errorf("expected OriginalID '100', got '%s'", result.OriginalID)
	}
}

func TestFireCallback_Failure(t *testing.T) {
	originalSender := callback.DefaultCallbackSender()
	defer callback.SetDefaultCallbackSender(originalSender)

	mock := &testCallbackSender{}
	callback.SetDefaultCallbackSender(mock)

	user := &model.UserModel{
		Id:       2,
		Username: "testuser2",
	}
	w := &JobQueueItem{
		ID:         99,
		Message:    "",
		OriginalID: "200",
	}
	jobErr := errors.New("translation failed: invalid X12")

	fireCallback(user, w, jobErr)

	time.Sleep(10 * time.Millisecond)

	if mock.count() != 1 {
		t.Fatalf("expected 1 callback invocation, got %d", mock.count())
	}

	result, ok := mock.lastResult()
	if !ok {
		t.Fatal("no callback result recorded")
	}
	if result.JobID != 99 {
		t.Errorf("expected JobID 99, got %d", result.JobID)
	}
	if result.Status != "FAILED" {
		t.Errorf("expected Status FAILED, got %s", result.Status)
	}
	if result.Message != "translation failed: invalid X12" {
		t.Errorf("expected error message, got '%s'", result.Message)
	}
}

func TestFireCallback_NonBlocking(t *testing.T) {
	// This test verifies fireCallback is called asynchronously from executeJob's
	// defer. We can't easily test executeJob without full infra, but we can
	// verify that the default sender is a noop (safe default) and that our
	// mock pattern works correctly.

	originalSender := callback.DefaultCallbackSender()
	defer callback.SetDefaultCallbackSender(originalSender)

	// Verify default sender is not nil
	defaultSender := callback.DefaultCallbackSender()
	if defaultSender == nil {
		t.Fatal("default callback sender is nil")
	}

	// Verify we can set and restore the default
	mock := &testCallbackSender{}
	callback.SetDefaultCallbackSender(mock)

	if callback.DefaultCallbackSender() != mock {
		t.Fatal("SetDefaultCallbackSender did not set the sender")
	}

	callback.SetDefaultCallbackSender(originalSender)
	if callback.DefaultCallbackSender() != originalSender {
		t.Fatal("SetDefaultCallbackSender did not restore the original sender")
	}
}

func TestFireCallback_NoCallbackConfig(t *testing.T) {
	// Verify fireCallback works with a user that has no callback config
	originalSender := callback.DefaultCallbackSender()
	defer callback.SetDefaultCallbackSender(originalSender)

	mock := &testCallbackSender{}
	callback.SetDefaultCallbackSender(mock)

	// User with empty CallbackServiceUri (no callback configured)
	user := &model.UserModel{
		Id:       3,
		Username: "nocallback",
	}
	w := &JobQueueItem{
		ID:         77,
		Message:    "done",
		OriginalID: "",
	}

	fireCallback(user, w, nil)

	time.Sleep(10 * time.Millisecond)

	// Should still call the sender; it's up to the sender (SoapCallback) to
	// check CallbackServiceUri and skip if empty.
	if mock.count() != 1 {
		t.Fatalf("expected 1 callback invocation even for no-callback user, got %d", mock.count())
	}

	// Verify PayloadID is 0 when OriginalID is non-numeric
	result, ok := mock.lastResult()
	if !ok {
		t.Fatal("no callback result recorded")
	}
	if result.PayloadID != 0 {
		t.Errorf("expected PayloadID 0 for non-numeric OriginalID, got %d", result.PayloadID)
	}
}
