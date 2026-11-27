package security

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const maxPGPKeyserverResponse = 1 << 20

// PGPKeyserverError describes whether a publication failure may succeed later.
type PGPKeyserverError struct {
	StatusCode int
	Retryable  bool
	Err        error
}

func (err *PGPKeyserverError) Error() string {
	if err.Err != nil {
		return err.Err.Error()
	}
	return fmt.Sprintf("keyserver returned HTTP %d", err.StatusCode)
}

func (err *PGPKeyserverError) Unwrap() error { return err.Err }

// PublishPGPKey sends an ASCII-armored public key to a VKS or HKP keyserver.
func PublishPGPKey(ctx context.Context, host string, publicKey []byte, fingerprint string) error {
	host = strings.TrimSpace(host)
	if host == "" || strings.ContainsAny(host, ":/@?#\\") || len(host) > 253 || len(bytes.TrimSpace(publicKey)) == 0 || fingerprint == "" {
		return errors.New("invalid keyserver publication input")
	}
	endpoint := "https://" + host + "/pks/add"
	vks := host == "keys.openpgp.org"
	if vks {
		endpoint = "https://keys.openpgp.org/vks/v1/upload"
	}
	client := &http.Client{
		Timeout: 20 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	return publishPGPKey(ctx, client, endpoint, vks, publicKey, fingerprint)
}

func publishPGPKey(ctx context.Context, client *http.Client, endpoint string, vks bool, publicKey []byte, fingerprint string) error {
	var body io.Reader
	method := http.MethodPost
	contentType := "application/x-www-form-urlencoded"
	if vks {
		payload, err := json.Marshal(struct {
			KeyText string `json:"keytext"`
		}{KeyText: string(publicKey)})
		if err != nil {
			return fmt.Errorf("encode VKS request: %w", err)
		}
		body = bytes.NewReader(payload)
		contentType = "application/json"
	} else {
		body = strings.NewReader(url.Values{"keytext": {string(publicKey)}}.Encode())
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return fmt.Errorf("build keyserver request: %w", err)
	}
	request.Header.Set("Content-Type", contentType)
	request.Header.Set("Accept", "application/json, text/plain")
	response, err := client.Do(request)
	if err != nil {
		return &PGPKeyserverError{Retryable: true, Err: fmt.Errorf("submit public key: %w", err)}
	}
	defer func() { _ = response.Body.Close() }()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, maxPGPKeyserverResponse+1))
	if err != nil {
		return &PGPKeyserverError{Retryable: true, Err: fmt.Errorf("read keyserver response: %w", err)}
	}
	if len(responseBody) > maxPGPKeyserverResponse {
		return &PGPKeyserverError{Err: errors.New("keyserver response exceeds size limit")}
	}
	if response.StatusCode != http.StatusOK {
		return &PGPKeyserverError{
			StatusCode: response.StatusCode,
			Retryable:  response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500,
			Err:        fmt.Errorf("keyserver returned HTTP %d", response.StatusCode),
		}
	}
	if vks {
		var result struct {
			Fingerprint string `json:"key_fpr"`
			Status      string `json:"status"`
		}
		if err := json.Unmarshal(responseBody, &result); err != nil {
			return fmt.Errorf("decode VKS response: %w", err)
		}
		if !strings.EqualFold(strings.ReplaceAll(result.Fingerprint, " ", ""), fingerprint) || strings.TrimSpace(result.Status) == "" {
			return errors.New("VKS response did not confirm the submitted fingerprint")
		}
	}
	return nil
}
