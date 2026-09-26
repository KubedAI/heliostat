package kube

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"k8s.io/client-go/rest"

	"github.com/KubedAI/heliostat/internal/config"
)

// maxJSONBytes bounds JSON documents read from Ray or the History Server.
const maxJSONBytes = 32 << 20

// ServiceTransport reaches HTTP services inside one Kubernetes cluster.
type ServiceTransport interface {
	// Do sends a GET or HEAD. pathAndQuery must start with "/" and be percent-encoded. The caller
	// closes the response body.
	Do(ctx context.Context, svc config.ServiceRef, method, pathAndQuery string, header http.Header) (*http.Response, error)
}

func noRedirects(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

// NewServiceTransport returns the transport configured for a cluster.
func NewServiceTransport(mode config.Transport, restCfg *rest.Config) (ServiceTransport, error) {
	if mode == config.TransportDirect {
		return &directTransport{client: &http.Client{CheckRedirect: noRedirects}}, nil
	}
	client, err := rest.HTTPClientFor(restCfg)
	if err != nil {
		return nil, err
	}
	client.CheckRedirect = noRedirects
	return &apiServerTransport{client: client, host: strings.TrimSuffix(restCfg.Host, "/")}, nil
}

// directTransport uses cluster DNS. Valid only inside the target cluster.
type directTransport struct{ client *http.Client }

func (t *directTransport) Do(ctx context.Context, svc config.ServiceRef, method, pathAndQuery string, header http.Header) (*http.Response, error) {
	target := fmt.Sprintf("http://%s.%s.svc:%d%s", svc.Name, svc.Namespace, svc.Port, pathAndQuery)
	return send(ctx, t.client, method, target, header)
}

// apiServerTransport uses the API server service proxy:
// /api/v1/namespaces/<ns>/services/http:<svc>:<port>/proxy/... With a get-only identity the API
// server rejects every POST, PUT, and DELETE, whatever the caller sends.
type apiServerTransport struct {
	client *http.Client
	host   string
}

func (t *apiServerTransport) Do(ctx context.Context, svc config.ServiceRef, method, pathAndQuery string, header http.Header) (*http.Response, error) {
	target := fmt.Sprintf("%s/api/v1/namespaces/%s/services/%s/proxy%s",
		t.host, url.PathEscape(svc.Namespace), url.PathEscape(fmt.Sprintf("http:%s:%d", svc.Name, svc.Port)), pathAndQuery)
	return send(ctx, t.client, method, target, header)
}

func send(ctx context.Context, client *http.Client, method, target string, header http.Header) (*http.Response, error) {
	if method != http.MethodGet && method != http.MethodHead {
		return nil, fmt.Errorf("method %s is not allowed", method)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range header {
		req.Header[k] = v
	}
	// Relay bytes untouched; Heliostat does its own compression.
	req.Header.Set("Accept-Encoding", "identity")
	return client.Do(req)
}

// StatusError is a non-2xx response from a service.
type StatusError struct {
	Status int
	Body   string
}

func (e *StatusError) Error() string { return fmt.Sprintf("HTTP %d: %s", e.Status, e.Body) }

// GetJSON GETs and decodes a JSON document, with a hard timeout.
func GetJSON(ctx context.Context, t ServiceTransport, svc config.ServiceRef, pathAndQuery string, header http.Header, timeout time.Duration, into any) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	h := http.Header{"Accept": {"application/json"}}
	for k, v := range header {
		h[k] = v
	}
	resp, err := t.Do(ctx, svc, http.MethodGet, pathAndQuery, h)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxJSONBytes))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		snippet := string(body)
		if len(snippet) > 200 {
			snippet = snippet[:200]
		}
		return &StatusError{Status: resp.StatusCode, Body: snippet}
	}
	return json.Unmarshal(body, into)
}
