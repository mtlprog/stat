package horizon

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestClientGetSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, 3, 10*time.Millisecond)
	body, err := client.get(context.Background(), "/test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(body) != `{"status":"ok"}` {
		t.Errorf("body = %q, want {\"status\":\"ok\"}", string(body))
	}
}

func TestClientRetryOn429(t *testing.T) {
	var attempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := attempts.Add(1)
		if n <= 2 {
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`rate limited`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, 3, 10*time.Millisecond)
	body, err := client.get(context.Background(), "/test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(body) != `{"status":"ok"}` {
		t.Errorf("body = %q, want success response", string(body))
	}
	if got := attempts.Load(); got != 3 {
		t.Errorf("attempts = %d, want 3", got)
	}
}

func TestClientMaxRetriesExceeded(t *testing.T) {
	var attempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`rate limited`))
	}))
	defer server.Close()

	client := NewClient(server.URL, 2, 10*time.Millisecond)
	_, err := client.get(context.Background(), "/test")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if got := attempts.Load(); got != 3 { // initial + 2 retries
		t.Errorf("attempts = %d, want 3", got)
	}
}

func TestClientNon429Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`not found`))
	}))
	defer server.Close()

	client := NewClient(server.URL, 3, 10*time.Millisecond)
	_, err := client.get(context.Background(), "/test")
	if err == nil {
		t.Fatal("expected error for 404, got nil")
	}
}

func TestClientRetryOn5xx(t *testing.T) {
	statuses := []int{http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout}
	for _, status := range statuses {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var attempts atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if n := attempts.Add(1); n <= 2 {
					w.WriteHeader(status)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(`{"ok":1}`))
			}))
			defer server.Close()

			client := NewClient(server.URL, 3, 5*time.Millisecond)
			body, err := client.get(context.Background(), "/test")
			if err != nil {
				t.Fatalf("expected success after 5xx retries, got %v", err)
			}
			if string(body) != `{"ok":1}` {
				t.Errorf("body = %q", body)
			}
			if got := attempts.Load(); got != 3 {
				t.Errorf("attempts = %d, want 3", got)
			}
		})
	}
}

func TestClientRetryOn5xxExhausted(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	const max = 2
	client := NewClient(server.URL, max, 5*time.Millisecond)
	if _, err := client.get(context.Background(), "/test"); err == nil {
		t.Fatal("expected error after exhausted 5xx retries")
	}
	if got := attempts.Load(); got != max+1 {
		t.Errorf("attempts = %d, want %d (initial + %d retries)", got, max+1, max)
	}
}

func TestClientNoRetryOn4xx(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	client := NewClient(server.URL, 3, 5*time.Millisecond)
	if _, err := client.get(context.Background(), "/test"); err == nil {
		t.Fatal("expected error for 400")
	}
	if got := attempts.Load(); got != 1 {
		t.Errorf("attempts = %d, want 1 (no retry on 4xx)", got)
	}
}

func TestClientContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`rate limited`))
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	client := NewClient(server.URL, 5, 1*time.Second)
	_, err := client.get(ctx, "/test")
	if err == nil {
		t.Fatal("expected error on cancelled context, got nil")
	}
}
