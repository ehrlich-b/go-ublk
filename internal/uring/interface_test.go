package uring

import (
	"testing"

	"github.com/ehrlich-b/go-ublk/internal/uapi"
)

func TestNewRing(t *testing.T) {
	config := Config{
		Entries: 32,
		FD:      -1,
		Flags:   0,
	}

	ring, err := NewRing(config)
	if err != nil {
		t.Fatalf("NewRing failed: %v", err)
	}
	defer ring.Close()

	if ring == nil {
		t.Error("ring is nil")
	}
}

func TestStubRingOperations(t *testing.T) {
	config := Config{
		Entries: 16,
		FD:      -1,
		Flags:   0,
	}

	ring, err := NewRing(config)
	if err != nil {
		t.Fatalf("NewRing failed: %v", err)
	}
	defer ring.Close()

	// Test control command
	ctrlCmd := &uapi.UblksrvCtrlCmd{
		DevID:   42,
		QueueID: 0xFFFF,
		Len:     0,
		Addr:    0,
	}

	result, err := ring.SubmitCtrlCmd(uapi.UBLK_CMD_GET_DEV_INFO, ctrlCmd, 123)
	if err != nil {
		t.Errorf("SubmitCtrlCmd failed: %v", err)
	}

	if result.UserData() != 123 {
		t.Errorf("UserData = %d, want 123", result.UserData())
	}

	if result.Value() != -38 {
		t.Errorf("Value = %d, want -38 (ENOSYS)", result.Value())
	}

	// Test I/O command
	ioCmd := &uapi.UblksrvIOCmd{
		QID:    1,
		Tag:    42,
		Result: 0,
		Addr:   0x1000,
	}

	result, err = ring.SubmitIOCmd(uapi.UBLK_IO_FETCH_REQ, ioCmd, 456)
	if err != nil {
		t.Errorf("SubmitIOCmd failed: %v", err)
	}

	if result.UserData() != 456 {
		t.Errorf("UserData = %d, want 456", result.UserData())
	}
}

func TestBatchOperations(t *testing.T) {
	config := Config{
		Entries: 16,
		FD:      -1,
		Flags:   0,
	}

	ring, err := NewRing(config)
	if err != nil {
		t.Fatalf("NewRing failed: %v", err)
	}
	defer ring.Close()

	batch := ring.NewBatch()

	// Add commands to batch
	ctrlCmd := &uapi.UblksrvCtrlCmd{
		DevID:   1,
		QueueID: 0xFFFF,
		Len:     0,
		Addr:    0,
	}

	err = batch.AddCtrlCmd(uapi.UBLK_CMD_GET_DEV_INFO, ctrlCmd, 1)
	if err != nil {
		t.Errorf("AddCtrlCmd failed: %v", err)
	}

	ioCmd := &uapi.UblksrvIOCmd{
		QID:    0,
		Tag:    10,
		Result: 0,
		Addr:   0x2000,
	}

	err = batch.AddIOCmd(uapi.UBLK_IO_FETCH_REQ, ioCmd, 2)
	if err != nil {
		t.Errorf("AddIOCmd failed: %v", err)
	}

	if batch.Len() != 2 {
		t.Errorf("batch length = %d, want 2", batch.Len())
	}

	// Submit batch
	results, err := batch.Submit()
	if err != nil {
		t.Errorf("Submit failed: %v", err)
	}

	if len(results) != 2 {
		t.Errorf("got %d results, want 2", len(results))
	}

	if batch.Len() != 0 {
		t.Errorf("batch should be empty after submit, got %d", batch.Len())
	}

	// Verify results
	for i, result := range results {
		if result.UserData() != uint64(i) {
			t.Errorf("result %d UserData = %d, want %d", i, result.UserData(), i)
		}
		if result.Value() != -38 {
			t.Errorf("result %d Value = %d, want -38", i, result.Value())
		}
	}
}

func TestFeatureDetection(t *testing.T) {
	err := SupportsFeatures()
	if err != nil {
		t.Logf("Features not supported: %v", err)
		return
	}

	features, err := GetFeatures()
	if err != nil {
		t.Fatalf("GetFeatures failed: %v", err)
	}

	if !features.SQE128 {
		t.Error("SQE128 should be supported in stub")
	}
	if !features.CQE32 {
		t.Error("CQE32 should be supported in stub")
	}
	if !features.UringCmd {
		t.Error("UringCmd should be supported in stub")
	}

	t.Logf("Features: SQE128=%t, CQE32=%t, UringCmd=%t, SQPOLL=%t",
		features.SQE128, features.CQE32, features.UringCmd, features.SQPOLL)
}

func BenchmarkStubOperations(b *testing.B) {
	config := Config{
		Entries: 64,
		FD:      -1,
		Flags:   0,
	}

	ring, err := NewRing(config)
	if err != nil {
		b.Fatalf("NewRing failed: %v", err)
	}
	defer ring.Close()

	ctrlCmd := &uapi.UblksrvCtrlCmd{
		DevID:   42,
		QueueID: 0,
		Len:     0,
		Addr:    0,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := ring.SubmitCtrlCmd(uapi.UBLK_CMD_GET_DEV_INFO, ctrlCmd, uint64(i))
		if err != nil {
			b.Fatalf("SubmitCtrlCmd failed: %v", err)
		}
	}
}

func BenchmarkBatchOperations(b *testing.B) {
	config := Config{
		Entries: 64,
		FD:      -1,
		Flags:   0,
	}

	ring, err := NewRing(config)
	if err != nil {
		b.Fatalf("NewRing failed: %v", err)
	}
	defer ring.Close()

	ctrlCmd := &uapi.UblksrvCtrlCmd{
		DevID:   42,
		QueueID: 0,
		Len:     0,
		Addr:    0,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		batch := ring.NewBatch()

		// Add 8 commands per iteration
		for j := 0; j < 8; j++ {
			err := batch.AddCtrlCmd(uapi.UBLK_CMD_GET_DEV_INFO, ctrlCmd, uint64(i*8+j))
			if err != nil {
				b.Fatalf("AddCtrlCmd failed: %v", err)
			}
		}

		_, err := batch.Submit()
		if err != nil {
			b.Fatalf("Submit failed: %v", err)
		}
	}
}