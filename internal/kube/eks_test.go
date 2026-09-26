package kube

import (
	"context"
	"encoding/base64"
	"net/url"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

func TestEKSToken(t *testing.T) {
	client := sts.NewFromConfig(aws.Config{
		Region:      "us-west-2",
		Credentials: credentials.NewStaticCredentialsProvider("AKIDEXAMPLE", "secret", "session"),
	})
	token, err := EKSToken(context.Background(), client, "ray-on-eks")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(token, "k8s-aws-v1.") || strings.ContainsAny(token, "=+/") {
		t.Fatalf("token format: %s", token)
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(token, "k8s-aws-v1."))
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	checks := map[string]string{
		"Action":               "GetCallerIdentity",
		"X-Amz-Expires":        "60",
		"X-Amz-Security-Token": "session",
	}
	for k, want := range checks {
		if got := q.Get(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
	if u.Host != "sts.us-west-2.amazonaws.com" {
		t.Errorf("host = %s", u.Host)
	}
	if !strings.Contains(q.Get("X-Amz-SignedHeaders"), "x-k8s-aws-id") {
		t.Errorf("cluster header not signed: %s", q.Get("X-Amz-SignedHeaders"))
	}
	if !strings.HasPrefix(q.Get("X-Amz-Credential"), "AKIDEXAMPLE/") {
		t.Errorf("credential scope: %s", q.Get("X-Amz-Credential"))
	}
}
