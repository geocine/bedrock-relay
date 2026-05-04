package relay

import (
	"encoding/json"
	"fmt"
	"os"
)

type ModelEntry struct {
	Alias string `json:"alias,omitempty"`
	ID    string `json:"id"`
}

type ModelCatalog struct {
	Models []ModelEntry `json:"models"`
	byName map[string]ModelEntry
}

func LoadModelCatalog(path string) (ModelCatalog, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return ModelCatalog{}, fmt.Errorf("load %s: %w", path, err)
	}
	var catalog ModelCatalog
	if err := json.Unmarshal(data, &catalog); err != nil {
		return ModelCatalog{}, fmt.Errorf("parse %s: %w", path, err)
	}
	if err := catalog.index(); err != nil {
		return ModelCatalog{}, err
	}
	return catalog, nil
}

func (c *ModelCatalog) index() error {
	c.byName = make(map[string]ModelEntry, len(c.Models))
	for i, entry := range c.Models {
		if entry.ID == "" {
			return fmt.Errorf("models[%d].id is required", i)
		}
		exposed := entry.ExposedID()
		if _, exists := c.byName[exposed]; exists {
			return fmt.Errorf("duplicate exposed model %q", exposed)
		}
		c.byName[exposed] = entry
	}
	return nil
}

func (e ModelEntry) ExposedID() string {
	if e.Alias != "" {
		return e.Alias
	}
	return e.ID
}

func (c ModelCatalog) Resolve(requested string) (bedrockID string, exposedID string, err error) {
	if requested == "" {
		return "", "", fmt.Errorf("model is required")
	}
	entry, ok := c.byName[requested]
	if !ok {
		return "", "", fmt.Errorf("unsupported model %q", requested)
	}
	return entry.ID, entry.ExposedID(), nil
}

func (c ModelCatalog) OpenAIModels() OpenAIModelsResponse {
	resp := OpenAIModelsResponse{
		Object: "list",
		Data:   make([]OpenAIModel, 0, len(c.Models)),
	}
	for _, entry := range c.Models {
		resp.Data = append(resp.Data, OpenAIModel{
			ID:      entry.ExposedID(),
			Object:  "model",
			OwnedBy: "bedrock",
		})
	}
	return resp
}
