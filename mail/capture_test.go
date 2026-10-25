package mail

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func TestCaptureSenderEmpty(t *testing.T) {
	c := NewCaptureSender()
	if got := c.Messages(); len(got) != 0 {
		t.Errorf("Messages() on empty sender returned %d messages, want 0", len(got))
	}
	if _, ok := c.Last(); ok {
		t.Error("Last() on empty sender returned ok=true, want false")
	}
}

func TestCaptureSenderBasics(t *testing.T) {
	c := NewCaptureSender()

	first := validTestMessage()
	second := validTestMessage()
	second.Subject = "Second"
	second.To = []string{"carol@example.test"}

	if err := c.Send(context.Background(), first); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if err := c.Send(context.Background(), second); err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	msgs := c.Messages()
	if len(msgs) != 2 {
		t.Fatalf("Messages() returned %d messages, want 2", len(msgs))
	}
	if !reflect.DeepEqual(msgs[0], first) {
		t.Errorf("Messages()[0] = %+v, want %+v", msgs[0], first)
	}
	if !reflect.DeepEqual(msgs[1], second) {
		t.Errorf("Messages()[1] = %+v, want %+v", msgs[1], second)
	}

	last, ok := c.Last()
	if !ok {
		t.Fatal("Last() ok = false, want true")
	}
	if !reflect.DeepEqual(last, second) {
		t.Errorf("Last() = %+v, want %+v", last, second)
	}
}

func TestCaptureSenderCopySemantics(t *testing.T) {
	c := NewCaptureSender()

	msg := Message{
		From:    "alice@example.test",
		To:      []string{"bob@example.test"},
		Cc:      []string{"carol@example.test"},
		Bcc:     []string{"hidden@example.test"},
		Subject: "Original",
		Text:    "original body",
	}
	if err := c.Send(context.Background(), msg); err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	// Mutating the source message after Send must not affect the
	// stored copy.
	msg.To[0] = "mutated@example.test"
	msg.Cc[0] = "mutated@example.test"
	msg.Bcc[0] = "mutated@example.test"
	msg.Subject = "Mutated"

	stored, ok := c.Last()
	if !ok {
		t.Fatal("Last() ok = false, want true")
	}
	if stored.To[0] != "bob@example.test" {
		t.Errorf("stored To[0] = %q, want %q", stored.To[0], "bob@example.test")
	}
	if stored.Cc[0] != "carol@example.test" {
		t.Errorf("stored Cc[0] = %q, want %q", stored.Cc[0], "carol@example.test")
	}
	if stored.Bcc[0] != "hidden@example.test" {
		t.Errorf("stored Bcc[0] = %q, want %q", stored.Bcc[0], "hidden@example.test")
	}
	if stored.Subject != "Original" {
		t.Errorf("stored Subject = %q, want %q", stored.Subject, "Original")
	}

	// Mutating the returned copy must not affect internal state either.
	returned := c.Messages()[0]
	returned.To[0] = "leaked@example.test"
	returned.Bcc[0] = "leaked@example.test"

	again, _ := c.Last()
	if again.To[0] != "bob@example.test" || again.Bcc[0] != "hidden@example.test" {
		t.Errorf("internal state mutated via returned copy: To=%v Bcc=%v", again.To, again.Bcc)
	}
}

func TestCaptureSenderReset(t *testing.T) {
	c := NewCaptureSender()
	if err := c.Send(context.Background(), validTestMessage()); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	c.Reset()
	if got := len(c.Messages()); got != 0 {
		t.Errorf("Messages() after Reset returned %d messages, want 0", got)
	}
	if _, ok := c.Last(); ok {
		t.Error("Last() after Reset returned ok=true, want false")
	}
}

func TestCaptureSenderConcurrentSend(t *testing.T) {
	c := NewCaptureSender()

	const goroutines = 8
	const perGoroutine = 25
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < perGoroutine; j++ {
				msg := validTestMessage()
				msg.Subject = fmt.Sprintf("message %d-%d", n, j)
				if err := c.Send(context.Background(), msg); err != nil {
					t.Errorf("Send() error = %v", err)
				}
			}
		}(i)
	}
	wg.Wait()

	msgs := c.Messages()
	if got, want := len(msgs), goroutines*perGoroutine; got != want {
		t.Fatalf("Messages() returned %d messages, want %d", got, want)
	}
	subjects := make(map[string]bool, len(msgs))
	for _, m := range msgs {
		if subjects[m.Subject] {
			t.Errorf("duplicate subject %q captured; messages were lost or overwritten", m.Subject)
		}
		subjects[m.Subject] = true
	}
}
