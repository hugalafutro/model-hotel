package ctxkeys

import (
	"context"
	"testing"
)

func TestVirtualKeyHashKey(t *testing.T) {
	ctx := context.Background()
	hash := "somehash"
	ctx = context.WithValue(ctx, VirtualKeyHashKey, hash)
	val := ctx.Value(VirtualKeyHashKey)
	if val == nil || val.(string) != hash {
		t.Errorf("expected %s, got %v", hash, val)
	}
}

func TestRequestBodyKey(t *testing.T) {
	ctx := context.Background()
	body := []byte("somebody")
	ctx = context.WithValue(ctx, RequestBodyKey, body)
	val := ctx.Value(RequestBodyKey)
	if val == nil {
		t.Error("expected non-nil value")
	} else if string(val.([]byte)) != string(body) {
		t.Errorf("expected %s, got %s", body, val.([]byte))
	}
}
