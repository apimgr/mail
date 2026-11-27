package security

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestPublishPGPKeyVKS(t *testing.T) {
	const fingerprint = "0123456789ABCDEF0123456789ABCDEF01234567"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/vks/v1/upload" || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("request = %s %s %q", r.Method, r.URL.Path, r.Header.Get("Content-Type"))
		}
		var body struct {
			KeyText string `json:"keytext"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.KeyText != "PUBLIC KEY" {
			t.Errorf("VKS request body = %#v, error %v", body, err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"key_fpr":"0123456789ABCDEF0123456789ABCDEF01234567","status":"ok"}`)
	}))
	defer server.Close()
	client := server.Client()
	endpoint := server.URL + "/vks/v1/upload"
	if err := publishPGPKey(context.Background(), client, endpoint, true, []byte("PUBLIC KEY"), fingerprint); err != nil {
		t.Fatalf("publishPGPKey() error = %v", err)
	}
}

func TestPublishPGPKeyHKPAndFailureClassification(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		retryable  bool
	}{
		{name: "success", statusCode: http.StatusOK},
		{name: "retry on rate limit", statusCode: http.StatusTooManyRequests, retryable: true},
		{name: "retry on server error", statusCode: http.StatusServiceUnavailable, retryable: true},
		{name: "do not retry client error", statusCode: http.StatusBadRequest},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.URL.Path != "/pks/add" || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
					t.Errorf("request = %s %s %q", r.Method, r.URL.Path, r.Header.Get("Content-Type"))
				}
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Errorf("read request body: %v", err)
				}
				values, err := url.ParseQuery(string(body))
				if err != nil || values.Get("keytext") != "PUBLIC KEY" {
					t.Errorf("HKP request body = %q, error %v", body, err)
				}
				w.WriteHeader(test.statusCode)
			}))
			defer server.Close()
			err := publishPGPKey(context.Background(), server.Client(), server.URL+"/pks/add", false, []byte("PUBLIC KEY"), "fingerprint")
			if test.statusCode == http.StatusOK {
				if err != nil {
					t.Fatalf("publishPGPKey() error = %v", err)
				}
				return
			}
			var keyserverErr *PGPKeyserverError
			if !errors.As(err, &keyserverErr) || keyserverErr.Retryable != test.retryable {
				t.Fatalf("publishPGPKey() error = %#v, retryable %t", err, test.retryable)
			}
		})
	}
}

func TestPublishPGPKeyRejectsInvalidInputAndVKSResponse(t *testing.T) {
	for _, test := range []struct {
		name        string
		host        string
		publicKey   []byte
		fingerprint string
	}{
		{name: "scheme", host: "https://keys.example", publicKey: []byte("KEY"), fingerprint: "fingerprint"},
		{name: "missing public key", host: "keys.example", fingerprint: "fingerprint"},
		{name: "missing fingerprint", host: "keys.example", publicKey: []byte("KEY")},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := PublishPGPKey(context.Background(), test.host, test.publicKey, test.fingerprint); err == nil {
				t.Fatal("PublishPGPKey() error = nil")
			}
		})
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"key_fpr":"wrong","status":"ok"}`)
	}))
	defer server.Close()
	if err := publishPGPKey(context.Background(), server.Client(), server.URL, true, []byte("KEY"), "expected"); err == nil {
		t.Fatal("mismatched VKS fingerprint error = nil")
	}
}
