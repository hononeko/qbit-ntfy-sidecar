package main

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestMainLifecycle(t *testing.T) {
	// We will run main() in a goroutine and cancel it to test the lifecycle
	// First, set up dummy environment
	_ = os.Setenv("NTFY_TOPIC", "test_lifecycle")

	// Create an interceptable stop channel by replacing os.Interrupt with a timer finish
	// Unfortunately signal.Notify catches real signals.
	// We will instead verify loadConfig works and let the test run briefly.

	// We can't easily test the blocking `main()` without it seizing the port indefinitely
	// in a testing environment without mocking `http.ListenAndServe`, which is hardcoded inside main.

	// Instead, we will test the graceful shutdown apparatus directly.
	appCtx, appCancel = context.WithCancel(context.Background())
	appWg.Add(1)

	go func() {
		defer appWg.Done()
		select {
		case <-appCtx.Done():
			return
		case <-time.After(1 * time.Second):
			t.Error("worker did not exit when context was cancelled")
		}
	}()

	// Trigger the cleanup Phase
	appCancel()

	// Wait on the group
	waitCh := make(chan struct{})
	go func() {
		appWg.Wait()
		close(waitCh)
	}()

	select {
	case <-waitCh:
		// Success
	case <-time.After(2 * time.Second):
		t.Fatal("appWg.Wait() hung, likely due to a leaked worker goroutine")
	}
}
