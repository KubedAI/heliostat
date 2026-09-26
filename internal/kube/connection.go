// Package kube builds authenticated connections to Kubernetes API servers and reaches in-cluster
// HTTP services (Ray heads, the Ray History Server) through them.
package kube

import (
	"context"
	"fmt"
	"net/http"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/KubedAI/heliostat/internal/config"
)

// userAgent identifies Heliostat in API server audit logs.
const userAgent = "heliostat"

// RESTConfig returns a client configuration for one cluster. EKS connections resolve the endpoint
// with eks:DescribeCluster and inject short-lived IAM tokens on every request.
func RESTConfig(ctx context.Context, conn config.Connection) (*rest.Config, error) {
	var cfg *rest.Config
	var err error
	switch conn.Type {
	case config.ConnectionDefault:
		cfg, err = rest.InClusterConfig()
		if err == rest.ErrNotInCluster {
			cfg, err = clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
				clientcmd.NewDefaultClientConfigLoadingRules(), &clientcmd.ConfigOverrides{},
			).ClientConfig()
		}
	case config.ConnectionKubeconfig:
		cfg, err = clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
			&clientcmd.ClientConfigLoadingRules{ExplicitPath: conn.Path},
			&clientcmd.ConfigOverrides{CurrentContext: conn.Context},
		).ClientConfig()
	case config.ConnectionEKS:
		cfg, err = eksRESTConfig(ctx, conn)
	default:
		err = fmt.Errorf("unsupported connection type %q", conn.Type)
	}
	if err != nil {
		return nil, err
	}
	cfg.UserAgent = userAgent
	// Informers keep one watch per resource type; the defaults throttle initial lists of large
	// clusters, so allow modest bursts.
	cfg.QPS, cfg.Burst = 20, 40
	return cfg, nil
}

// bearerTransport sets the Authorization header from a refreshing token source.
type bearerTransport struct {
	base  http.RoundTripper
	token func(context.Context) (string, error)
}

func (t *bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	token, err := t.token(req.Context())
	if err != nil {
		return nil, err
	}
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", "Bearer "+token)
	return t.base.RoundTrip(req)
}
