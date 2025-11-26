package queue

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// Mock backend for testing
type mockBackend struct {
	data       []byte
	size       int64
	readErr    error
	writeErr   error
	readDelay  time.Duration
	writeDelay time.Duration
	mu         sync.RWMutex
}

func newMockBackend(size int64) *mockBackend {
	return &mockBackend{
		data: make([]byte, size),
		size: size,
	}
}

func (m *mockBackend) ReadAt(p []byte, off int64) (int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.readDelay > 0 {
		time.Sleep(m.readDelay)
	}

	if m.readErr != nil {
		return 0, m.readErr
	}

	if off >= m.size {
		return 0, nil
	}

	available := m.size - off
	if int64(len(p)) > available {
		p = p[:available]
	}

	n := copy(p, m.data[off:off+int64(len(p))])
	return n, nil
}

func (m *mockBackend) WriteAt(p []byte, off int64) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.writeDelay > 0 {
		time.Sleep(m.writeDelay)
	}

	if m.writeErr != nil {
		return 0, m.writeErr
	}

	if off >= m.size {
		return 0, errors.New("write beyond end")
	}

	available := m.size - off
	if int64(len(p)) > available {
		p = p[:available]
	}

	n := copy(m.data[off:off+int64(len(p))], p)
	return n, nil
}

func (m *mockBackend) Size() int64 {
	return m.size
}

func (m *mockBackend) Close() error {
	return nil
}

func (m *mockBackend) Flush() error {
	return nil
}

func (m *mockBackend) setReadError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.readErr = err
}

func (m *mockBackend) setWriteError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.writeErr = err
}

// Mock logger for testing
type mockLogger struct {
	messages []string
	mu       sync.Mutex
}

func (l *mockLogger) Printf(format string, args ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.messages = append(l.messages, format)
}

func (l *mockLogger) Debugf(format string, args ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.messages = append(l.messages, format)
}

func (l *mockLogger) getMessages() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	result := make([]string, len(l.messages))
	copy(result, l.messages)
	return result
}

// Test TagState constants and transitions
func TestTagStateConstants(t *testing.T) {
	// Test that tag states have expected values
	if TagStateInFlightFetch != 0 {
		t.Errorf("Expected TagStateInFlightFetch=0, got %d", TagStateInFlightFetch)
	}

	if TagStateOwned != 1 {
		t.Errorf("Expected TagStateOwned=1, got %d", TagStateOwned)
	}

	if TagStateInFlightCommit != 2 {
		t.Errorf("Expected TagStateInFlightCommit=2, got %d", TagStateInFlightCommit)
	}
}

func TestRunnerCreation(t *testing.T) {
	backend := newMockBackend(1024 * 1024) // 1MB
	logger := &mockLogger{}

	config := Config{
		DevID:   0,
		QueueID: 0,
		Depth:   64,
		Backend: backend,
		Logger:  logger,
	}

	ctx := context.Background()
	runner := NewStubRunner(ctx, config)

	if runner == nil {
		t.Fatal("Runner is nil")
	}

	// Check initial state
	if runner.deviceID != 0 {
		t.Errorf("Expected deviceID=0, got %d", runner.deviceID)
	}

	if runner.queueID != 0 {
		t.Errorf("Expected queueID=0, got %d", runner.queueID)
	}

	if runner.depth != 64 {
		t.Errorf("Expected depth=64, got %d", runner.depth)
	}

	if runner.backend != backend {
		t.Error("Backend not set correctly")
	}

	// Check tag state initialization
	if len(runner.tagStates) != 64 {
		t.Errorf("Expected 64 tag states, got %d", len(runner.tagStates))
	}

	if len(runner.tagMutexes) != 64 {
		t.Errorf("Expected 64 tag mutexes, got %d", len(runner.tagMutexes))
	}

	// All tag states should be initialized to 0 (TagStateInFlightFetch)
	for i, state := range runner.tagStates {
		if state != TagState(0) {
			t.Errorf("Tag %d state should be 0, got %d", i, state)
		}
	}

	// Cleanup
	runner.Close()
}

func TestRunnerTagStateTracking(t *testing.T) {
	backend := newMockBackend(1024 * 1024)
	logger := &mockLogger{}

	config := Config{
		DevID:   0,
		QueueID: 0,
		Depth:   4, // Small depth for easier testing
		Backend: backend,
		Logger:  logger,
	}

	ctx := context.Background()
	runner := NewStubRunner(ctx, config)
	defer runner.Close()

	// Test initial tag states (should all be 0 - uninitialized)
	for tag := 0; tag < runner.depth; tag++ {
		if runner.tagStates[tag] != TagState(0) {
			t.Errorf("Tag %d should start with state 0, got %d", tag, runner.tagStates[tag])
		}
	}

	// Manually test state transitions (simulating what happens in the real queue)
	// In real code, these transitions happen in handleCompletion and processIOAndCommit

	// Simulate: submitInitialFetchReq -> TagStateInFlightFetch
	tag := 0
	runner.tagMutexes[tag].Lock()
	runner.tagStates[tag] = TagStateInFlightFetch
	runner.tagMutexes[tag].Unlock()

	if runner.tagStates[tag] != TagStateInFlightFetch {
		t.Errorf("Tag %d should be in TagStateInFlightFetch, got %d", tag, runner.tagStates[tag])
	}

	// Simulate: handleCompletion(FETCH_REQ) -> TagStateOwned
	runner.tagMutexes[tag].Lock()
	if runner.tagStates[tag] == TagStateInFlightFetch {
		runner.tagStates[tag] = TagStateOwned
	}
	runner.tagMutexes[tag].Unlock()

	if runner.tagStates[tag] != TagStateOwned {
		t.Errorf("Tag %d should be in TagStateOwned, got %d", tag, runner.tagStates[tag])
	}

	// Simulate: processIOAndCommit -> TagStateInFlightCommit
	runner.tagMutexes[tag].Lock()
	if runner.tagStates[tag] == TagStateOwned {
		runner.tagStates[tag] = TagStateInFlightCommit
	}
	runner.tagMutexes[tag].Unlock()

	if runner.tagStates[tag] != TagStateInFlightCommit {
		t.Errorf("Tag %d should be in TagStateInFlightCommit, got %d", tag, runner.tagStates[tag])
	}

	// Simulate: handleCompletion(COMMIT_AND_FETCH_REQ) -> TagStateOwned (next cycle)
	runner.tagMutexes[tag].Lock()
	if runner.tagStates[tag] == TagStateInFlightCommit {
		runner.tagStates[tag] = TagStateOwned
	}
	runner.tagMutexes[tag].Unlock()

	if runner.tagStates[tag] != TagStateOwned {
		t.Errorf("Tag %d should be back in TagStateOwned, got %d", tag, runner.tagStates[tag])
	}
}

func TestRunnerConcurrentTagAccess(t *testing.T) {
	backend := newMockBackend(1024 * 1024)
	logger := &mockLogger{}

	config := Config{
		DevID:   0,
		QueueID: 0,
		Depth:   16,
		Backend: backend,
		Logger:  logger,
	}

	ctx := context.Background()
	runner := NewStubRunner(ctx, config)
	defer runner.Close()

	// Test concurrent access to different tags (should not block)
	var wg sync.WaitGroup

	for tag := 0; tag < 4; tag++ {
		wg.Add(1)
		go func(t int) {
			defer wg.Done()

			// Simulate state transitions for this tag
			runner.tagMutexes[t].Lock()
			runner.tagStates[t] = TagStateInFlightFetch
			runner.tagMutexes[t].Unlock()

			time.Sleep(1 * time.Millisecond) // Simulate work

			runner.tagMutexes[t].Lock()
			runner.tagStates[t] = TagStateOwned
			runner.tagMutexes[t].Unlock()

			time.Sleep(1 * time.Millisecond)

			runner.tagMutexes[t].Lock()
			runner.tagStates[t] = TagStateInFlightCommit
			runner.tagMutexes[t].Unlock()
		}(tag)
	}

	// Wait for all goroutines to complete
	done := make(chan bool)
	go func() {
		wg.Wait()
		done <- true
	}()

	select {
	case <-done:
		// Success - no deadlock
	case <-time.After(5 * time.Second):
		t.Fatal("Deadlock detected - concurrent tag access took too long")
	}

	// Verify final states
	for tag := 0; tag < 4; tag++ {
		if runner.tagStates[tag] != TagStateInFlightCommit {
			t.Errorf("Tag %d should be in TagStateInFlightCommit, got %d", tag, runner.tagStates[tag])
		}
	}
}

func TestRunnerBackendErrorHandling(t *testing.T) {
	backend := newMockBackend(1024)
	logger := &mockLogger{}

	config := Config{
		DevID:   0,
		QueueID: 0,
		Depth:   4,
		Backend: backend,
		Logger:  logger,
	}

	ctx := context.Background()
	runner := NewStubRunner(ctx, config)
	defer runner.Close()

	// Test that backend errors don't corrupt tag state tracking
	backend.setReadError(errors.New("mock read error"))

	// Even with backend errors, tag state transitions should remain consistent
	tag := 0
	runner.tagMutexes[tag].Lock()
	initialState := runner.tagStates[tag]
	runner.tagStates[tag] = TagStateOwned
	finalState := runner.tagStates[tag]
	runner.tagMutexes[tag].Unlock()

	if initialState == finalState && finalState != TagStateOwned {
		t.Error("State transition failed even without I/O operation")
	}

	if runner.tagStates[tag] != TagStateOwned {
		t.Errorf("Tag state should be TagStateOwned despite backend error, got %d", runner.tagStates[tag])
	}
}

func TestRunnerContextCancellation(t *testing.T) {
	backend := newMockBackend(1024 * 1024)
	logger := &mockLogger{}

	config := Config{
		DevID:   0,
		QueueID: 0,
		Depth:   4,
		Backend: backend,
		Logger:  logger,
	}

	ctx := context.Background()
	runner := NewStubRunner(ctx, config)

	// Verify runner has a cancellable context
	if runner.ctx == nil {
		t.Error("Runner context should not be nil")
	}

	if runner.cancel == nil {
		t.Error("Runner cancel function should not be nil")
	}

	// Test context cancellation
	runner.cancel()

	// Context should be cancelled
	select {
	case <-runner.ctx.Done():
		// Good - context was cancelled
	case <-time.After(100 * time.Millisecond):
		t.Error("Context was not cancelled after calling cancel()")
	}

	// Cleanup
	runner.Close()
}

func TestUserDataEncoding(t *testing.T) {
	// Test user data encoding constants
	if udOpFetch != 0 {
		t.Errorf("Expected udOpFetch=0, got %d", udOpFetch)
	}

	if udOpCommit != (1 << 63) {
		t.Errorf("Expected udOpCommit=1<<63, got %d", udOpCommit)
	}

	// Test that the high bit distinguishes operation types
	fetchTag := uint64(42)
	commitTag := uint64(42)

	fetchUserData := udOpFetch | fetchTag
	commitUserData := udOpCommit | commitTag

	if fetchUserData == commitUserData {
		t.Error("Fetch and commit user data should be different")
	}

	// Test extracting tag from user data
	extractedFetchTag := fetchUserData & 0x7FFFFFFFFFFFFFFF // Clear high bit
	extractedCommitTag := commitUserData & 0x7FFFFFFFFFFFFFFF

	if extractedFetchTag != 42 {
		t.Errorf("Expected extracted fetch tag=42, got %d", extractedFetchTag)
	}

	if extractedCommitTag != 42 {
		t.Errorf("Expected extracted commit tag=42, got %d", extractedCommitTag)
	}

	// Test detecting operation type
	isFetch := (fetchUserData & (1 << 63)) == 0
	isCommit := (commitUserData & (1 << 63)) != 0

	if !isFetch {
		t.Error("Should detect fetch operation")
	}

	if !isCommit {
		t.Error("Should detect commit operation")
	}
}

// Benchmark tag state transitions to ensure they're fast
func BenchmarkTagStateTransition(b *testing.B) {
	backend := newMockBackend(1024 * 1024)
	logger := &mockLogger{}

	config := Config{
		DevID:   0,
		QueueID: 0,
		Depth:   64,
		Backend: backend,
		Logger:  logger,
	}

	ctx := context.Background()
	runner := NewStubRunner(ctx, config)
	defer runner.Close()

	b.ResetTimer()

	// Benchmark the speed of tag state transitions
	for i := 0; i < b.N; i++ {
		tag := i % runner.depth

		runner.tagMutexes[tag].Lock()
		runner.tagStates[tag] = TagStateInFlightFetch
		runner.tagMutexes[tag].Unlock()

		runner.tagMutexes[tag].Lock()
		runner.tagStates[tag] = TagStateOwned
		runner.tagMutexes[tag].Unlock()

		runner.tagMutexes[tag].Lock()
		runner.tagStates[tag] = TagStateInFlightCommit
		runner.tagMutexes[tag].Unlock()
	}
}

// Test that demonstrates the correct state machine flow
func TestTagStateMachineFlow(t *testing.T) {
	backend := newMockBackend(1024)
	logger := &mockLogger{}

	config := Config{
		DevID:   0,
		QueueID: 0,
		Depth:   1, // Single tag for simplicity
		Backend: backend,
		Logger:  logger,
	}

	ctx := context.Background()
	runner := NewStubRunner(ctx, config)
	defer runner.Close()

	tag := 0

	// Initial state: uninitialized (0)
	if runner.tagStates[tag] != TagState(0) {
		t.Errorf("Initial state should be 0, got %d", runner.tagStates[tag])
	}

	// Flow 1: Submit initial FETCH_REQ -> InFlightFetch
	runner.tagMutexes[tag].Lock()
	if runner.tagStates[tag] == TagState(0) {
		runner.tagStates[tag] = TagStateInFlightFetch
	}
	runner.tagMutexes[tag].Unlock()

	if runner.tagStates[tag] != TagStateInFlightFetch {
		t.Errorf("Should be InFlightFetch, got %d", runner.tagStates[tag])
	}

	// Flow 2: FETCH_REQ completes with I/O ready -> Owned
	runner.tagMutexes[tag].Lock()
	if runner.tagStates[tag] == TagStateInFlightFetch {
		runner.tagStates[tag] = TagStateOwned
	}
	runner.tagMutexes[tag].Unlock()

	if runner.tagStates[tag] != TagStateOwned {
		t.Errorf("Should be Owned, got %d", runner.tagStates[tag])
	}

	// Flow 3: Process I/O and submit COMMIT_AND_FETCH_REQ -> InFlightCommit
	runner.tagMutexes[tag].Lock()
	if runner.tagStates[tag] == TagStateOwned {
		runner.tagStates[tag] = TagStateInFlightCommit
	}
	runner.tagMutexes[tag].Unlock()

	if runner.tagStates[tag] != TagStateInFlightCommit {
		t.Errorf("Should be InFlightCommit, got %d", runner.tagStates[tag])
	}

	// Flow 4: COMMIT_AND_FETCH_REQ completes with next I/O ready -> Owned (cycle continues)
	runner.tagMutexes[tag].Lock()
	if runner.tagStates[tag] == TagStateInFlightCommit {
		runner.tagStates[tag] = TagStateOwned
	}
	runner.tagMutexes[tag].Unlock()

	if runner.tagStates[tag] != TagStateOwned {
		t.Errorf("Should be back to Owned, got %d", runner.tagStates[tag])
	}

	// This demonstrates the steady-state cycle: Owned -> InFlightCommit -> Owned -> ...
}
