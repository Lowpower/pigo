package codemode

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestMaxWorkersEnv(t *testing.T) {
	t.Setenv("PIGO_CODEMODE_MAX_WORKERS", "")
	t.Setenv("PI_CODEMODE_MAX_WORKERS", "4")
	if maxWorkers() != 2 {
		t.Fatalf("pi env changed the limit to %d", maxWorkers())
	}
	t.Setenv("PIGO_CODEMODE_MAX_WORKERS", "1")
	if maxWorkers() != 1 {
		t.Fatalf("pigo %d", maxWorkers())
	}
	t.Setenv("PIGO_CODEMODE_MAX_WORKERS", "nope")
	if maxWorkers() != 2 {
		t.Fatalf("invalid pigo %d", maxWorkers())
	}
	t.Setenv("PIGO_CODEMODE_MAX_WORKERS", "-3")
	if maxWorkers() != 2 {
		t.Fatalf("negative pigo %d", maxWorkers())
	}
}

func TestReserveQueuesInOrder(t *testing.T) {
	t.Setenv("PIGO_CODEMODE_MAX_WORKERS", "1")

	release1, err := reserve(context.Background(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	secondReady := make(chan struct{})
	go func() {
		release2, err := reserve(context.Background(), time.Second)
		if err != nil {
			t.Error(err)
			close(secondReady)
			return
		}
		close(secondReady)
		release2()
	}()
	deadline := time.Now().Add(2 * time.Second)
	for {
		poolMu.Lock()
		waiting := len(poolWait)
		poolMu.Unlock()
		if waiting == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("second caller did not queue")
		}
		time.Sleep(5 * time.Millisecond)
	}
	release1()
	select {
	case <-secondReady:
	case <-time.After(2 * time.Second):
		t.Fatal("second did not get the slot after release")
	}
}

func TestReserveWaitTimeout(t *testing.T) {
	t.Setenv("PIGO_CODEMODE_MAX_WORKERS", "1")
	release, err := reserve(context.Background(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	_, err = reserve(context.Background(), 30*time.Millisecond)
	var timeout *TimeoutError
	if !errors.As(err, &timeout) || !timeout.Waiting {
		t.Fatalf("err %v", err)
	}
}

func TestReserveCancel(t *testing.T) {
	t.Setenv("PIGO_CODEMODE_MAX_WORKERS", "1")
	release, err := reserve(context.Background(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := reserve(ctx, time.Second)
		done <- err
	}()
	deadline := time.Now().Add(2 * time.Second)
	for {
		poolMu.Lock()
		waiting := len(poolWait)
		poolMu.Unlock()
		if waiting == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("caller did not queue")
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancel did not unblock the waiter")
	}
}
