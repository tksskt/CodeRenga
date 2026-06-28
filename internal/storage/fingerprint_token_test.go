package storage

import (
	"context"
	"testing"
)

func TestCreateSessionStoresFingerprints(t *testing.T) {
	s, err := Open("", true)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	if err := s.CreateSessionWithFingerprints(ctx, "s", "p", "coder", "local", "cfg", "prompt"); err != nil {
		t.Fatal(err)
	}
	got, err := s.SessionByID(ctx, "s")
	if err != nil {
		t.Fatal(err)
	}
	if got.ConfigFingerprint != "cfg" || got.PromptFingerprint != "prompt" {
		t.Fatalf("session=%#v", got)
	}
}

func TestAddMessageStoresTokenEstimate(t *testing.T) {
	s, err := Open("", true)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	if err := s.CreateSession(ctx, "s", "p", "coder", "local"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddMessage(ctx, "s", "user", "12345678"); err != nil {
		t.Fatal(err)
	}
	tokens, err := s.UncompactedTokenEstimate(ctx, "s")
	if err != nil {
		t.Fatal(err)
	}
	if tokens != 2 {
		t.Fatalf("tokens=%d", tokens)
	}
}
