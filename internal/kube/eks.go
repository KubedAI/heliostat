package kube

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	smithyhttp "github.com/aws/smithy-go/transport/http"
	"k8s.io/client-go/rest"

	"github.com/KubedAI/heliostat/internal/config"
)

const (
	// EKS accepts tokens for 15 minutes; refresh well before that.
	tokenTTL      = 10 * time.Minute
	tokenPrefix   = "k8s-aws-v1."
	clusterHeader = "x-k8s-aws-id"
)

// EKSToken mints an EKS bearer token: a presigned STS GetCallerIdentity URL bound to the cluster
// name through the signed x-k8s-aws-id header. This is what `aws eks get-token` produces.
func EKSToken(ctx context.Context, client *sts.Client, clusterName string) (string, error) {
	presigned, err := sts.NewPresignClient(client).PresignGetCallerIdentity(ctx, &sts.GetCallerIdentityInput{},
		func(o *sts.PresignOptions) {
			o.ClientOptions = append(o.ClientOptions, func(opts *sts.Options) {
				opts.APIOptions = append(opts.APIOptions,
					smithyhttp.SetHeaderValue(clusterHeader, clusterName),
					smithyhttp.SetHeaderValue("X-Amz-Expires", "60"),
				)
			})
		})
	if err != nil {
		return "", err
	}
	return tokenPrefix + base64.RawURLEncoding.EncodeToString([]byte(presigned.URL)), nil
}

// awsConfig loads base credentials from the default chain (EKS Pod Identity, IRSA, or a profile)
// and, when roleArn is set, assumes that role, typically in another account.
func awsConfig(ctx context.Context, conn config.Connection) (aws.Config, error) {
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(conn.Region))
	if err != nil {
		return cfg, err
	}
	if conn.RoleARN != "" {
		provider := stscreds.NewAssumeRoleProvider(sts.NewFromConfig(cfg), conn.RoleARN, func(o *stscreds.AssumeRoleOptions) {
			o.RoleSessionName = "heliostat"
			if conn.ExternalID != "" {
				o.ExternalID = aws.String(conn.ExternalID)
			}
		})
		cfg.Credentials = aws.NewCredentialsCache(provider)
	}
	return cfg, nil
}

func eksRESTConfig(ctx context.Context, conn config.Connection) (*rest.Config, error) {
	awsCfg, err := awsConfig(ctx, conn)
	if err != nil {
		return nil, err
	}
	out, err := eks.NewFromConfig(awsCfg).DescribeCluster(ctx, &eks.DescribeClusterInput{Name: aws.String(conn.ClusterName)})
	if err != nil {
		return nil, fmt.Errorf("describe EKS cluster %s: %w", conn.ClusterName, err)
	}
	if out.Cluster == nil || out.Cluster.Endpoint == nil || out.Cluster.CertificateAuthority == nil || out.Cluster.CertificateAuthority.Data == nil {
		return nil, fmt.Errorf("EKS cluster %s has no endpoint yet", conn.ClusterName)
	}
	ca, err := base64.StdEncoding.DecodeString(*out.Cluster.CertificateAuthority.Data)
	if err != nil {
		return nil, fmt.Errorf("EKS cluster %s certificate: %w", conn.ClusterName, err)
	}

	tokens := &tokenCache{client: sts.NewFromConfig(awsCfg), clusterName: conn.ClusterName}
	return &rest.Config{
		Host:            *out.Cluster.Endpoint,
		TLSClientConfig: rest.TLSClientConfig{CAData: ca},
		WrapTransport: func(rt http.RoundTripper) http.RoundTripper {
			return &bearerTransport{base: rt, token: tokens.get}
		},
	}, nil
}

// tokenCache coalesces token minting so concurrent requests share one fresh token.
type tokenCache struct {
	client      *sts.Client
	clusterName string

	mu        sync.Mutex
	token     string
	expiresAt time.Time
}

func (c *tokenCache) get(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" && time.Now().Before(c.expiresAt) {
		return c.token, nil
	}
	token, err := EKSToken(ctx, c.client, c.clusterName)
	if err != nil {
		return "", fmt.Errorf("mint EKS token for %s: %w", c.clusterName, err)
	}
	c.token, c.expiresAt = token, time.Now().Add(tokenTTL)
	return token, nil
}
