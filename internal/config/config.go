// Package config loads and validates Heliostat's configuration file. See config/heliostat.yaml
// for an annotated example; the Helm chart renders the same schema from its values.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"sigs.k8s.io/yaml"
)

const (
	mountedConfig = "/etc/heliostat/config.yaml"
	mountedModels = "/etc/heliostat/models.yaml"
)

var (
	dnsLabel    = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$`)
	serviceHost = regexp.MustCompile(`^([a-z0-9-]+)\.([a-z0-9-]+):(\d+)$`)
)

// ServiceRef is a Kubernetes Service port that Heliostat talks to over HTTP.
type ServiceRef struct {
	Namespace string
	Name      string
	Port      int
}

// ConnectionType selects how Heliostat authenticates to a Kubernetes API server.
type ConnectionType string

const (
	// ConnectionDefault uses the in-cluster service account, or the current kubeconfig context.
	ConnectionDefault ConnectionType = "default"
	// ConnectionKubeconfig uses an explicit kubeconfig file and optional context.
	ConnectionKubeconfig ConnectionType = "kubeconfig"
	// ConnectionEKS mints EKS IAM tokens, optionally after assuming a role in another account.
	ConnectionEKS ConnectionType = "eks"
)

// Connection describes how to reach one Kubernetes API server.
type Connection struct {
	Type ConnectionType
	// Path and Context apply to ConnectionKubeconfig.
	Path    string
	Context string
	// ClusterName, Region, RoleARN, and ExternalID apply to ConnectionEKS.
	ClusterName string
	Region      string
	RoleARN     string
	ExternalID  string
}

// Transport selects how Heliostat reaches Ray heads and the History Server.
type Transport string

const (
	// TransportAPIServer goes through the Kubernetes API server's service proxy with a get-only
	// identity, so Kubernetes itself rejects any write to Ray. The default.
	TransportAPIServer Transport = "apiserver"
	// TransportDirect uses cluster DNS from inside the cluster. Faster, but gives Heliostat's pod a
	// network path to Ray's unauthenticated Jobs API. See SECURITY.md.
	TransportDirect Transport = "direct"
)

// HistoryServer points at the KubeRay History Server's API and dashboard services.
type HistoryServer struct {
	API       ServiceRef
	Dashboard ServiceRef
}

// Cluster is one Kubernetes cluster to index.
type Cluster struct {
	// Name is a stable DNS label stored with every job and used in URLs.
	Name        string
	DisplayName string
	Region      string
	Connection  Connection
	Transport   Transport
	// RayDashboards enables the dashboard proxy and Jobs API polling for this cluster.
	RayDashboards bool
	HistoryServer *HistoryServer
}

// Collector tunes the background collectors.
type Collector struct {
	ResyncIntervalSeconds         int
	SubmissionPollIntervalSeconds int
	HistoryPollIntervalSeconds    int
	MaxConcurrentRequests         int
	RequestTimeoutSeconds         int
}

// Config is the full runtime configuration.
type Config struct {
	Clusters      []Cluster
	Collector     Collector
	RetentionDays int
	DatabasePath  string
	ModelsPath    string
}

// Error is a configuration validation failure.
type Error struct{ msg string }

func (e *Error) Error() string { return "invalid Heliostat configuration: " + e.msg }

func errf(format string, args ...any) error { return &Error{fmt.Sprintf(format, args...)} }

// Load reads HELIOSTAT_CONFIG, the Helm-mounted file, or config/heliostat.yaml, in that order.
// Without any file, Heliostat indexes the single cluster reachable through the default kubeconfig.
func Load(getenv func(string) string, cwd string) (*Config, error) {
	file := firstExisting(getenv("HELIOSTAT_CONFIG"), mountedConfig, filepath.Join(cwd, "config", "heliostat.yaml"))
	document := map[string]any{}
	if file != "" {
		data, err := os.ReadFile(file)
		if err != nil {
			return nil, err
		}
		if err := yaml.Unmarshal(data, &document); err != nil {
			return nil, errf("%s: %v", file, err)
		}
	}
	return Parse(document, getenv, cwd)
}

func firstExisting(candidates ...string) string {
	for _, c := range candidates {
		if c == "" {
			continue
		}
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return ""
}

// Parse validates a decoded configuration document.
func Parse(document map[string]any, getenv func(string) string, cwd string) (*Config, error) {
	var clusters []Cluster
	rawClusters, _ := document["clusters"].([]any)
	if len(rawClusters) == 0 {
		c, err := defaultCluster(getenv)
		if err != nil {
			return nil, err
		}
		clusters = []Cluster{c}
	}
	names := map[string]bool{}
	for i, raw := range rawClusters {
		c, err := parseCluster(dict(raw), fmt.Sprintf("clusters[%d]", i))
		if err != nil {
			return nil, err
		}
		if names[c.Name] {
			return nil, errf("duplicate cluster name %q", c.Name)
		}
		names[c.Name] = true
		clusters = append(clusters, c)
	}

	collector := dict(document["collector"])
	cfg := &Config{Clusters: clusters}
	var err error
	fields := []struct {
		target   *int
		raw      any
		fallback int
		where    string
	}{
		{&cfg.Collector.ResyncIntervalSeconds, collector["resyncIntervalSeconds"], 900, "collector.resyncIntervalSeconds"},
		{&cfg.Collector.SubmissionPollIntervalSeconds, collector["submissionPollIntervalSeconds"], 30, "collector.submissionPollIntervalSeconds"},
		{&cfg.Collector.HistoryPollIntervalSeconds, collector["historyPollIntervalSeconds"], 60, "collector.historyPollIntervalSeconds"},
		{&cfg.Collector.MaxConcurrentRequests, collector["maxConcurrentRequests"], 8, "collector.maxConcurrentRequests"},
		{&cfg.Collector.RequestTimeoutSeconds, collector["requestTimeoutSeconds"], 5, "collector.requestTimeoutSeconds"},
		{&cfg.RetentionDays, dict(document["retention"])["days"], 90, "retention.days"},
	}
	for _, f := range fields {
		if *f.target, err = positive(f.raw, f.fallback, f.where); err != nil {
			return nil, err
		}
	}

	cfg.DatabasePath = getenv("HELIOSTAT_DB_PATH")
	if cfg.DatabasePath == "" {
		if cfg.DatabasePath, err = optString(dict(document["database"])["path"], "database.path"); err != nil {
			return nil, err
		}
	}
	if cfg.DatabasePath == "" {
		cfg.DatabasePath = filepath.Join(cwd, ".data", "heliostat.db")
	}
	cfg.ModelsPath = firstExisting(getenv("HELIOSTAT_MODELS_PATH"), mountedModels)
	if cfg.ModelsPath == "" {
		cfg.ModelsPath = filepath.Join(cwd, "config", "models.yaml")
	}
	return cfg, nil
}

func defaultCluster(getenv func(string) string) (Cluster, error) {
	inCluster := getenv("KUBERNETES_SERVICE_HOST") != ""
	name := getenv("HELIOSTAT_CLUSTER_NAME")
	if name == "" {
		name = "local"
		if inCluster {
			name = "in-cluster"
		}
	}
	if !dnsLabel.MatchString(name) {
		return Cluster{}, errf("HELIOSTAT_CLUSTER_NAME %q must be a DNS label", name)
	}
	return Cluster{
		Name:          name,
		DisplayName:   name,
		Region:        getenv("HELIOSTAT_CLUSTER_REGION"),
		Connection:    Connection{Type: ConnectionDefault},
		Transport:     TransportAPIServer,
		RayDashboards: true,
	}, nil
}

func parseCluster(raw map[string]any, where string) (Cluster, error) {
	name, err := requiredString(raw["name"], where+".name")
	if err != nil {
		return Cluster{}, err
	}
	if !dnsLabel.MatchString(name) {
		return Cluster{}, errf("%s.name %q must be a DNS label", where, name)
	}
	conn, err := parseConnection(dict(raw["connection"]), where+".connection")
	if err != nil {
		return Cluster{}, err
	}
	transport, err := optString(raw["serviceTransport"], where+".serviceTransport")
	if err != nil {
		return Cluster{}, err
	}
	switch transport {
	case "", "auto", string(TransportAPIServer):
		transport = string(TransportAPIServer)
	case string(TransportDirect):
	default:
		return Cluster{}, errf("%s.serviceTransport must be one of apiserver, direct", where)
	}
	displayName, err := optString(raw["displayName"], where+".displayName")
	if err != nil {
		return Cluster{}, err
	}
	if displayName == "" {
		displayName = name
	}
	region, err := optString(raw["region"], where+".region")
	if err != nil {
		return Cluster{}, err
	}
	if region == "" && conn.Type == ConnectionEKS {
		region = conn.Region
	}
	dashboards := true
	if v, ok := raw["rayDashboards"]; ok {
		b, isBool := v.(bool)
		if !isBool {
			return Cluster{}, errf("%s.rayDashboards must be true or false", where)
		}
		dashboards = b
	}
	c := Cluster{
		Name: name, DisplayName: displayName, Region: region, Connection: conn,
		Transport: Transport(transport), RayDashboards: dashboards,
	}
	if hs, ok := raw["historyServer"]; ok && hs != nil {
		h := dict(hs)
		api, err := ParseServiceRef(h["api"], where+".historyServer.api")
		if err != nil {
			return Cluster{}, err
		}
		dash, err := ParseServiceRef(h["dashboard"], where+".historyServer.dashboard")
		if err != nil {
			return Cluster{}, err
		}
		c.HistoryServer = &HistoryServer{API: api, Dashboard: dash}
	}
	return c, nil
}

func parseConnection(raw map[string]any, where string) (Connection, error) {
	typ, err := optString(raw["type"], where+".type")
	if err != nil {
		return Connection{}, err
	}
	switch ConnectionType(typ) {
	case "", ConnectionDefault:
		return Connection{Type: ConnectionDefault}, nil
	case ConnectionKubeconfig:
		path, err := requiredString(raw["path"], where+".path")
		if err != nil {
			return Connection{}, err
		}
		ctx, err := optString(raw["context"], where+".context")
		return Connection{Type: ConnectionKubeconfig, Path: path, Context: ctx}, err
	case ConnectionEKS:
		c := Connection{Type: ConnectionEKS}
		if c.ClusterName, err = requiredString(raw["clusterName"], where+".clusterName"); err != nil {
			return c, err
		}
		if c.Region, err = requiredString(raw["region"], where+".region"); err != nil {
			return c, err
		}
		if c.RoleARN, err = optString(raw["roleArn"], where+".roleArn"); err != nil {
			return c, err
		}
		c.ExternalID, err = optString(raw["externalId"], where+".externalId")
		return c, err
	}
	return Connection{}, errf("%s.type must be one of default, kubeconfig, eks", where)
}

// ParseServiceRef accepts {namespace, name, port} or the shorthand "<name>.<namespace>:<port>".
func ParseServiceRef(raw any, where string) (ServiceRef, error) {
	if s, ok := raw.(string); ok {
		m := serviceHost.FindStringSubmatch(s)
		if m == nil {
			return ServiceRef{}, errf(`%s must look like "<service>.<namespace>:<port>"`, where)
		}
		port, _ := strconv.Atoi(m[3])
		return ServiceRef{Name: m[1], Namespace: m[2], Port: port}, nil
	}
	d := dict(raw)
	ns, err := requiredString(d["namespace"], where+".namespace")
	if err != nil {
		return ServiceRef{}, err
	}
	name, err := requiredString(d["name"], where+".name")
	if err != nil {
		return ServiceRef{}, err
	}
	port, err := positive(d["port"], 0, where+".port")
	return ServiceRef{Namespace: ns, Name: name, Port: port}, err
}

func dict(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

func requiredString(v any, where string) (string, error) {
	s, ok := v.(string)
	if !ok || strings.TrimSpace(s) == "" {
		return "", errf("%s is required", where)
	}
	return strings.TrimSpace(s), nil
}

func optString(v any, where string) (string, error) {
	if v == nil {
		return "", nil
	}
	s, ok := v.(string)
	if !ok {
		return "", errf("%s must be a string", where)
	}
	return strings.TrimSpace(s), nil
}

// positive accepts a positive number; a zero fallback means the field is required.
func positive(v any, fallback int, where string) (int, error) {
	if v == nil {
		if fallback == 0 {
			return 0, errf("%s is required", where)
		}
		return fallback, nil
	}
	f, ok := v.(float64)
	if !ok || f <= 0 || f != float64(int(f)) {
		return 0, errf("%s must be a positive whole number", where)
	}
	return int(f), nil
}

// IsConfigError reports whether err is a validation failure.
func IsConfigError(err error) bool {
	var e *Error
	return errors.As(err, &e)
}
