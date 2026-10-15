package observability

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestMonitoringConfigs validates the Prometheus and alert rule YAML syntax
// and asserts the documented metric names exist. This runs in CI without a
// monitoring stack; live Prometheus/Grafana/OTLP integration is manual
// (see docs/MODULE8_OBSERVABILITY.md).
func TestMonitoringConfigs(t *testing.T) {
	root := repoRoot(t)

	promBytes := readFile(t, filepath.Join(root, "infra", "monitoring", "prometheus.yml"))
	var prom struct {
		ScrapeConfigs []struct {
			JobName      string `yaml:"job_name"`
			MetricsPath  string `yaml:"metrics_path"`
			StaticConfig []struct {
				Targets []string `yaml:"targets"`
			} `yaml:"static_configs"`
		} `yaml:"scrape_configs"`
		RuleFiles []string `yaml:"rule_files"`
	}
	if err := yaml.Unmarshal(promBytes, &prom); err != nil {
		t.Fatalf("prometheus.yml: %v", err)
	}
	foundAPI := false
	for _, sc := range prom.ScrapeConfigs {
		if sc.JobName == "jnana-api" {
			foundAPI = true
			if sc.MetricsPath != "/api/v1/metrics" {
				t.Errorf("scrape path = %q", sc.MetricsPath)
			}
		}
	}
	if !foundAPI {
		t.Error("jnana-api scrape job missing")
	}
	if len(prom.RuleFiles) == 0 {
		t.Error("no rule_files configured")
	}

	alertBytes := readFile(t, filepath.Join(root, "infra", "monitoring", "alerts.yml"))
	var alerts struct {
		Groups []struct {
			Name  string `yaml:"name"`
			Rules []struct {
				Alert string `yaml:"alert"`
				Expr  string `yaml:"expr"`
				For   string `yaml:"for"`
			} `yaml:"rules"`
		} `yaml:"groups"`
	}
	if err := yaml.Unmarshal(alertBytes, &alerts); err != nil {
		t.Fatalf("alerts.yml: %v", err)
	}
	if len(alerts.Groups) == 0 || len(alerts.Groups[0].Rules) == 0 {
		t.Fatal("no alert rules defined")
	}
	documented := []string{
		"http_requests_total", "http_request_duration_seconds",
		"platform_ready", "llm_model_load_errors_total", "ingestion_jobs",
	}
	var exprs strings.Builder
	for _, g := range alerts.Groups {
		for _, r := range g.Rules {
			if r.Alert == "" || r.Expr == "" {
				t.Errorf("rule missing alert/expr: %+v", r)
			}
			exprs.WriteString(r.Expr)
		}
	}
	for _, m := range documented {
		if !strings.Contains(exprs.String(), m) {
			t.Errorf("metric %q not referenced by any alert", m)
		}
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	// Walk up from services/api/internal/observability to the repo root.
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(dir, "infra", "monitoring", "prometheus.yml")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatal("repo root not found")
	return ""
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return b
}
