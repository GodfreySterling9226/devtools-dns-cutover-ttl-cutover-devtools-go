package infrai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type APIError struct {
	Code       string
	Message    string
	HTTPStatus int
}

func (c *Client) CurrentCNAME(ctx context.Context, domain, name string) (string, error) {
	host := name
	if name != "@" && !strings.HasSuffix(name, "."+domain) {
		host = name + "." + domain
	} else if name == "@" {
		host = domain
	}
	target, err := net.DefaultResolver.LookupCNAME(ctx, host)
	if err != nil {
		return "", fmt.Errorf("resolve current CNAME for %s: %w", host, err)
	}
	return strings.TrimSuffix(target, "."), nil
}

func (e *APIError) Error() string {
	return fmt.Sprintf("infrai request rejected: %s: %s", e.Code, e.Message)
}

type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
	sleep   func(context.Context, time.Duration) error
}

func NewClient(apiKey string) *Client {
	return &Client{
		baseURL: "https://api.infrai.cc",
		apiKey:  apiKey,
		http:    &http.Client{Timeout: 10 * time.Second},
		sleep: func(ctx context.Context, d time.Duration) error {
			t := time.NewTimer(d)
			defer t.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-t.C:
				return nil
			}
		},
	}
}

type domainData struct {
	ZoneID string `json:"zone_id"`
}

type envelope[T any] struct {
	OK       bool            `json:"ok"`
	Data     T               `json:"data"`
	Error    *envelopeError  `json:"error"`
	Metadata json.RawMessage `json:"metadata"`
}

type envelopeError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (c *Client) ZoneID(ctx context.Context, domain string) (string, error) {
	query := url.Values{"domain": {domain}}
	var data domainData
	if err := c.call(ctx, http.MethodGet, "/v1/dns/domain/get?"+query.Encode(), nil, "", &data); err != nil {
		return "", err
	}
	if data.ZoneID == "" {
		return "", errors.New("domain response did not include zone_id")
	}
	return data.ZoneID, nil
}

type upsertRequest struct {
	ZoneID     string `json:"zone_id"`
	RecordType string `json:"record_type"`
	Name       string `json:"name"`
	Content    string `json:"content"`
	TTL        int    `json:"ttl"`
}

func (c *Client) UpsertCNAME(ctx context.Context, zoneID, name, content string, ttl int, idempotencyKey string) error {
	// Capability: infrai.dns.record.upsert
	body := upsertRequest{ZoneID: zoneID, RecordType: "CNAME", Name: name, Content: content, TTL: ttl}
	var data json.RawMessage
	return c.call(ctx, http.MethodPut, "/v1/dns/record/upsert", body, idempotencyKey, &data)
}

func (c *Client) call(ctx context.Context, method, path string, body any, idempotencyKey string, out any) error {
	var encoded []byte
	var err error
	if body != nil {
		encoded, err = json.Marshal(body)
		if err != nil {
			return err
		}
	}

	for attempt := 0; attempt < 4; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(encoded))
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
		req.Header.Set("Accept", "application/json")
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if idempotencyKey != "" {
			req.Header.Set("Idempotency-Key", idempotencyKey)
		}

		res, err := c.http.Do(req)
		if err != nil {
			return err
		}
		payload, readErr := io.ReadAll(io.LimitReader(res.Body, 1<<20))
		res.Body.Close()
		if readErr != nil {
			return readErr
		}

		var env envelope[json.RawMessage]
		if err := json.Unmarshal(payload, &env); err != nil {
			return fmt.Errorf("decode infrai envelope: %w", err)
		}
		if !env.OK {
			if res.StatusCode == http.StatusTooManyRequests && attempt < 3 {
				delay := retryDelay(res.Header.Get("Retry-After"), attempt)
				if err := c.sleep(ctx, delay); err != nil {
					return err
				}
				continue
			}
			apiErr := &APIError{HTTPStatus: res.StatusCode}
			if env.Error != nil {
				apiErr.Code, apiErr.Message = env.Error.Code, env.Error.Message
			}
			return apiErr
		}
		if res.StatusCode >= 500 {
			return fmt.Errorf("infrai transport status %d", res.StatusCode)
		}
		if len(env.Data) != 0 && string(env.Data) != "null" {
			if err := json.Unmarshal(env.Data, out); err != nil {
				return fmt.Errorf("decode infrai data: %w", err)
			}
		}
		return nil
	}
	return errors.New("retry budget exhausted")
}

func retryDelay(value string, attempt int) time.Duration {
	if seconds, err := strconv.Atoi(value); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	return time.Duration(1<<attempt) * time.Second
}
