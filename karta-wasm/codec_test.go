// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 NVIDIA Corporation

//go:build js && wasm

package main

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestEncodeEnvelope_Success(t *testing.T) {
	env := encodeEnvelope(map[string]string{"hello": "world"}, nil)

	if !env.Get("error").IsNull() {
		t.Fatalf("expected error = null, got %v", env.Get("error"))
	}
	var parsed map[string]string
	if err := json.Unmarshal([]byte(env.Get("data").String()), &parsed); err != nil {
		t.Fatalf("failed to unmarshal data: %v", err)
	}
	if parsed["hello"] != "world" {
		t.Errorf("expected data.hello = %q, got %q", "world", parsed["hello"])
	}
}

func TestEncodeEnvelope_Error(t *testing.T) {
	env := encodeEnvelope(nil, errors.New("boom"))

	if !env.Get("data").IsNull() {
		t.Fatalf("expected data = null, got %v", env.Get("data"))
	}
	if got := env.Get("error").String(); got != "boom" {
		t.Errorf("expected error = %q, got %q", "boom", got)
	}
}

func TestEncodeEnvelope_MarshalError(t *testing.T) {
	// A channel cannot be marshaled to JSON.
	env := encodeEnvelope(make(chan int), nil)

	if !env.Get("data").IsNull() {
		t.Fatalf("expected data = null, got %v", env.Get("data"))
	}
	if env.Get("error").String() == "" {
		t.Fatal("expected a non-empty error for an unmarshalable value")
	}
}
