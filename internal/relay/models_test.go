package relay

import "testing"

func TestModelCatalogResolveAliasAndDirect(t *testing.T) {
	c := ModelCatalog{
		Models: []ModelEntry{
			{Alias: "sonnet", ID: "us.anthropic.claude-sonnet-4-6"},
			{ID: "us.anthropic.claude-haiku-4-5"},
		},
	}
	if err := c.index(); err != nil {
		t.Fatal(err)
	}

	bedrock, exposed, err := c.Resolve("sonnet")
	if err != nil {
		t.Fatal(err)
	}
	if bedrock != "us.anthropic.claude-sonnet-4-6" || exposed != "sonnet" {
		t.Fatalf("alias resolved to %q/%q", bedrock, exposed)
	}

	bedrock, exposed, err = c.Resolve("us.anthropic.claude-haiku-4-5")
	if err != nil {
		t.Fatal(err)
	}
	if bedrock != exposed || exposed != "us.anthropic.claude-haiku-4-5" {
		t.Fatalf("direct resolved to %q/%q", bedrock, exposed)
	}
}

func TestModelCatalogRejectsUnmappedModel(t *testing.T) {
	c := ModelCatalog{Models: []ModelEntry{{Alias: "sonnet", ID: "real"}}}
	if err := c.index(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.Resolve("real"); err == nil {
		t.Fatal("expected unexposed backing model to be rejected when an alias is configured")
	}
}

func TestModelCatalogDuplicateExposedName(t *testing.T) {
	c := ModelCatalog{Models: []ModelEntry{{Alias: "same", ID: "a"}, {Alias: "same", ID: "b"}}}
	if err := c.index(); err == nil {
		t.Fatal("expected duplicate exposed model error")
	}
}
