package adapters

import (
	"context"
	"errors"
	"testing"
)

type dummyTool struct {
	name string
	fail bool
}

func (d *dummyTool) Execute(ctx context.Context, input map[string]interface{}) (map[string]interface{}, error) {
	if d.fail {
		return nil, errors.New("fail")
	}
	return map[string]interface{}{"ok": true}, nil
}
func (d *dummyTool) Schema() map[string]interface{} {
	return map[string]interface{}{"description": "dummy"}
}
func (d *dummyTool) Validate(input map[string]interface{}) error {
	if input["bad"] == true {
		return errors.New("bad input")
	}
	return nil
}
func (d *dummyTool) Name() string { return d.name }

func TestGoToolAdapter_Execute_SuccessAndFailure(t *testing.T) {
	adapter := NewGoToolAdapter("dummy", (&dummyTool{name: "dummy"}).Execute)
	res, err := adapter.Execute(context.Background(), map[string]interface{}{})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if res["ok"] != true {
		t.Errorf("expected ok=true, got %v", res["ok"])
	}

	adapterFail := NewGoToolAdapter("dummy", (&dummyTool{name: "dummy", fail: true}).Execute)
	_, err = adapterFail.Execute(context.Background(), map[string]interface{}{})
	if err == nil {
		t.Error("expected error for failing tool, got nil")
	}
}

func TestGoToolAdapter_Validate(t *testing.T) {
	adapter := NewGoToolAdapter("dummy", (&dummyTool{name: "dummy"}).Execute)
	err := adapter.Validate(map[string]interface{}{"bad": true})
	if err == nil {
		t.Error("expected error for bad input, got nil")
	}
	err = adapter.Validate(map[string]interface{}{})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}
