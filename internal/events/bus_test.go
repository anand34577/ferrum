package events

import (
	"testing"
	"time"
)

func TestPublishDeliversToSubscriber(t *testing.T) {
	b := New()
	ch, unsub := b.Subscribe()
	defer unsub()

	b.Publish(Event{Type: TypeAlertTriggered, ResourceID: "qemu/100"})

	select {
	case evt := <-ch:
		if evt.Type != TypeAlertTriggered {
			t.Fatalf("type = %q, want %q", evt.Type, TypeAlertTriggered)
		}
		if evt.ResourceID != "qemu/100" {
			t.Fatalf("resourceId = %q, want qemu/100", evt.ResourceID)
		}
		if evt.ID == "" {
			t.Fatal("expected an auto-generated ID")
		}
		if evt.Timestamp.IsZero() {
			t.Fatal("expected an auto-filled timestamp")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestPublishFansOutToMultipleSubscribers(t *testing.T) {
	b := New()
	ch1, unsub1 := b.Subscribe()
	defer unsub1()
	ch2, unsub2 := b.Subscribe()
	defer unsub2()

	b.Publish(Event{Type: TypeConnectionDown})

	for i, ch := range []<-chan Event{ch1, ch2} {
		select {
		case <-ch:
		case <-time.After(time.Second):
			t.Fatalf("subscriber %d never received the event", i)
		}
	}
}

func TestPublishWithNoSubscribersDoesNotBlock(t *testing.T) {
	b := New()
	done := make(chan struct{})
	go func() {
		b.Publish(Event{Type: TypeTest})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Publish blocked with no subscribers")
	}
}

func TestPublishDoesNotBlockOnFullSubscriberBuffer(t *testing.T) {
	b := New()
	_, unsub := b.Subscribe() // never drained
	defer unsub()

	done := make(chan struct{})
	go func() {
		for i := 0; i < subBufferSize+10; i++ {
			b.Publish(Event{Type: TypeTest})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Publish blocked once the subscriber's buffer filled up")
	}
}

func TestUnsubscribeStopsDeliveryAndClosesChannel(t *testing.T) {
	b := New()
	ch, unsub := b.Subscribe()
	if got := b.SubscriberCount(); got != 1 {
		t.Fatalf("SubscriberCount = %d, want 1", got)
	}
	unsub()
	if got := b.SubscriberCount(); got != 0 {
		t.Fatalf("SubscriberCount after unsubscribe = %d, want 0", got)
	}

	b.Publish(Event{Type: TypeTest})

	// The channel should be closed (reads return the zero value immediately)
	// rather than blocking or receiving the event published after unsubscribe.
	select {
	case evt, ok := <-ch:
		if ok {
			t.Fatalf("received event after unsubscribe: %+v", evt)
		}
	case <-time.After(time.Second):
		t.Fatal("channel was not closed after unsubscribe")
	}
}
