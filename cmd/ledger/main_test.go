package main

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/CarriedWorldUniverse/ledger"
)

func TestMain_ServiceOpens(t *testing.T) {
	dir := t.TempDir()
	svc, err := ledger.New(context.Background(), ledger.Config{DBPath: filepath.Join(dir, "ledger.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()
}
