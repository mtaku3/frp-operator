package provisioning

import (
	"context"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/types"
)

func TestBatcher_SingleTrigger_ReturnsAfterIdle(t *testing.T) {
	b := NewBatcher[types.UID](40*time.Millisecond, 1*time.Second)
	b.Trigger("a")
	start := time.Now()
	if !b.Wait(context.Background()) {
		t.Fatal("wait should return true")
	}
	if elapsed := time.Since(start); elapsed < 40*time.Millisecond {
		t.Fatalf("returned too early: %s", elapsed)
	}
	if got := b.Drain(); len(got) != 1 {
		t.Fatalf("expected 1 drained, got %v", got)
	}
}

func TestBatcher_KeepsExtendingOnNewTriggers(t *testing.T) {
	b := NewBatcher[types.UID](80*time.Millisecond, 5*time.Second)
	done := make(chan bool, 1)
	go func() { done <- b.Wait(context.Background()) }()
	// Re-trigger every 30ms 5 times — Wait shouldn't return until we stop.
	b.Trigger("a")
	for i := 0; i < 4; i++ {
		time.Sleep(30 * time.Millisecond)
		select {
		case <-done:
			t.Fatal("Wait returned while triggers were still arriving")
		default:
		}
		b.Trigger(types.UID([]byte{byte(i)}))
	}
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Wait should have returned after idle settled")
	}
}

func TestBatcher_MaxCapsTotalTime(t *testing.T) {
	b := NewBatcher[types.UID](50*time.Millisecond, 100*time.Millisecond)
	go func() {
		// Continuous triggers — would extend idle forever.
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		stop := time.After(300 * time.Millisecond)
		b.Trigger("a")
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				b.Trigger("b")
			}
		}
	}()
	start := time.Now()
	b.Wait(context.Background())
	if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
		t.Fatalf("expected return near max=100ms, got %s", elapsed)
	}
}

func TestBatcher_CtxCancelBeforeTrigger_ReturnsFalse(t *testing.T) {
	b := NewBatcher[types.UID](1*time.Second, 1*time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	if b.Wait(ctx) {
		t.Fatal("expected false on ctx cancel before trigger")
	}
}

func TestBatcher_DrainDeduped(t *testing.T) {
	b := NewBatcher[types.UID](1*time.Second, 1*time.Second)
	b.Trigger("a")
	b.Trigger("a")
	b.Trigger("b")
	drained := b.Drain()
	if len(drained) != 2 {
		t.Fatalf("expected 2 deduped UIDs, got %v", drained)
	}
	if again := b.Drain(); len(again) != 0 {
		t.Fatalf("Drain should clear; got %v", again)
	}
}
