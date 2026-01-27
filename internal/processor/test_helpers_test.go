package processor

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func mkdirAll(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatalf("mkdirAll(%q): %v", p, err)
	}
}

func writeFile(t *testing.T, p string, contents string) {
	t.Helper()
	mkdirAll(t, filepath.Dir(p))
	if err := os.WriteFile(p, []byte(contents), 0o644); err != nil {
		t.Fatalf("writeFile(%q): %v", p, err)
	}
}

func planOne(t *testing.T, p *processorImpl, inputPath string) (Plan, error) {
	t.Helper()

	plans, err := p.Plan(context.Background(), Request{InputPath: inputPath})
	if err != nil {
		return Plan{}, err
	}
	if len(plans) != 1 {
		t.Fatalf("expected 1 plan, got %d", len(plans))
	}
	return plans[0], nil
}
