package config

import (
	"reflect"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"
)

func env(values map[string]string) func(string) string {
	return func(k string) string { return values[k] }
}

func parse(t *testing.T, doc string, e map[string]string) (*Config, error) {
	t.Helper()
	m := map[string]any{}
	if err := yaml.Unmarshal([]byte(doc), &m); err != nil {
		t.Fatal(err)
	}
	return Parse(m, env(e), "/app")
}

func TestDefaults(t *testing.T) {
	cfg, err := parse(t, "{}", nil)
	if err != nil {
		t.Fatal(err)
	}
	want := Cluster{Name: "local", DisplayName: "local", Connection: Connection{Type: ConnectionDefault}, Transport: TransportAPIServer, RayDashboards: true}
	if !reflect.DeepEqual(cfg.Clusters, []Cluster{want}) {
		t.Errorf("clusters: %+v", cfg.Clusters)
	}
	if cfg.Collector.SubmissionPollIntervalSeconds != 30 || cfg.RetentionDays != 90 || cfg.DatabasePath != "/app/.data/heliostat.db" {
		t.Errorf("defaults: %+v", cfg)
	}
	inCluster, _ := parse(t, "{}", map[string]string{"KUBERNETES_SERVICE_HOST": "10.0.0.1", "HELIOSTAT_DB_PATH": "/data/x.db"})
	if inCluster.Clusters[0].Name != "in-cluster" || inCluster.DatabasePath != "/data/x.db" {
		t.Errorf("in-cluster: %+v", inCluster)
	}
}

func TestTransportsAndConnections(t *testing.T) {
	cfg, err := parse(t, `
clusters:
  - name: home
  - name: fast
    serviceTransport: direct
  - name: remote
    connection: {type: eks, clusterName: prod, region: us-east-1, roleArn: "arn:aws:iam::1:role/r"}
    historyServer:
      api: ray-history-server.ray-history-server:8080
      dashboard: {namespace: ray-history-server, name: ray-history-dashboard, port: 8265}
`, nil)
	if err != nil {
		t.Fatal(err)
	}
	var transports, regions []string
	for _, c := range cfg.Clusters {
		transports = append(transports, string(c.Transport))
		regions = append(regions, c.Region)
	}
	if !reflect.DeepEqual(transports, []string{"apiserver", "direct", "apiserver"}) {
		t.Errorf("transports: %v", transports)
	}
	if !reflect.DeepEqual(regions, []string{"", "", "us-east-1"}) {
		t.Errorf("regions: %v", regions)
	}
	remote := cfg.Clusters[2]
	if remote.Connection.RoleARN != "arn:aws:iam::1:role/r" || remote.HistoryServer.API.Port != 8080 || remote.HistoryServer.Dashboard.Name != "ray-history-dashboard" {
		t.Errorf("remote: %+v", remote)
	}
}

func TestValidation(t *testing.T) {
	cases := map[string]string{
		"clusters: [{name: Bad_Name}]":                                              "DNS label",
		"clusters: [{name: a}, {name: a}]":                                          "duplicate",
		"clusters: [{name: a, connection: {type: gke}}]":                            "connection.type",
		"clusters: [{name: a, connection: {type: eks, region: x}}]":                 "clusterName is required",
		"clusters: [{name: a, serviceTransport: magic}]":                            "serviceTransport",
		"collector: {resyncIntervalSeconds: -1}":                                    "positive",
		"clusters: [{name: a, rayDashboards: yes-please}]":                          "true or false",
		"clusters: [{name: a, historyServer: {api: 'svc:80', dashboard: 'a.b:1'}}]": "<service>.<namespace>:<port>",
	}
	for doc, want := range cases {
		_, err := parse(t, doc, nil)
		if err == nil || !IsConfigError(err) || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: got %v, want error containing %q", doc, err, want)
		}
	}
}

// The annotated example shipped in config/ must always load.
func TestExampleConfigLoads(t *testing.T) {
	cfg, err := Load(env(map[string]string{"HELIOSTAT_CONFIG": "../../config/heliostat.yaml"}), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Clusters) != 1 || cfg.Clusters[0].Name != "local" || cfg.Clusters[0].Transport != TransportAPIServer {
		t.Errorf("example: %+v", cfg.Clusters)
	}
}
