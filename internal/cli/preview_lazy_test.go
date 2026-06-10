package cli

import (
	"context"
	"errors"
	"sync"
	"testing"
)

func TestLazyContent(t *testing.T) {
	t.Run("does not cache errors", func(t *testing.T) {
		// Regression test: the previous sync.Once implementation cached a
		// failed first render (e.g. context.Canceled from an aborted request)
		// forever, breaking the presentation view for all later requests.
		calls := 0
		fail := true
		fn := lazyContent(func(_ context.Context) ([]byte, string, error) {
			calls++
			if fail {
				return nil, "", context.Canceled
			}
			return []byte("rendered"), "text/html", nil
		})

		if _, _, err := fn(context.Background()); !errors.Is(err, context.Canceled) {
			t.Fatalf("first call error = %v, want context.Canceled", err)
		}

		fail = false
		body, ct, err := fn(context.Background())
		if err != nil {
			t.Fatalf("second call error = %v, want nil", err)
		}
		if string(body) != "rendered" || ct != "text/html" {
			t.Errorf("second call = (%q, %q), want (%q, %q)", body, ct, "rendered", "text/html")
		}
		if calls != 2 {
			t.Errorf("render called %d times, want 2", calls)
		}
	})

	t.Run("caches first success", func(t *testing.T) {
		calls := 0
		fn := lazyContent(func(_ context.Context) ([]byte, string, error) {
			calls++
			return []byte("rendered"), "text/html", nil
		})

		for range 3 {
			body, _, err := fn(context.Background())
			if err != nil {
				t.Fatalf("call error = %v, want nil", err)
			}
			if string(body) != "rendered" {
				t.Errorf("body = %q, want %q", body, "rendered")
			}
		}
		if calls != 1 {
			t.Errorf("render called %d times, want 1", calls)
		}
	})

	t.Run("safe for concurrent use", func(t *testing.T) {
		calls := 0
		fn := lazyContent(func(_ context.Context) ([]byte, string, error) {
			calls++
			return []byte("rendered"), "text/html", nil
		})

		var wg sync.WaitGroup
		for range 8 {
			wg.Go(func() {
				if _, _, err := fn(context.Background()); err != nil {
					t.Errorf("concurrent call error = %v", err)
				}
			})
		}
		wg.Wait()
		if calls != 1 {
			t.Errorf("render called %d times, want 1", calls)
		}
	})
}
