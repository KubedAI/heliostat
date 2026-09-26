// Package models reads the approved-model catalog.
package models

import (
	"os"

	"sigs.k8s.io/yaml"

	"github.com/KubedAI/heliostat/internal/domain"
)

// Load reads the catalog. It is re-read on every call so ConfigMap updates apply without a
// restart; the file is small and read only when the Models page is open.
func Load(path string) ([]domain.Model, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc struct {
		Models []domain.Model `json:"models"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	out := make([]domain.Model, 0, len(doc.Models))
	for _, m := range doc.Models {
		if m.ID == "" {
			continue
		}
		if m.Name == "" {
			m.Name = m.ID
		}
		if m.Status == "" {
			m.Status = "approved"
		}
		if m.GPUProfiles == nil {
			m.GPUProfiles = []string{}
		}
		if m.Tags == nil {
			m.Tags = []string{}
		}
		out = append(out, m)
	}
	return out, nil
}
