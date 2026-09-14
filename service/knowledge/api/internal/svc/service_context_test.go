package svc

import (
	"strings"
	"testing"

	"sea-try-go/service/knowledge/api/internal/config"
)

func TestProductIdentityRequiresUserIssuerAndRPC(t *testing.T) {
	var c config.Config
	c.Auth.AccessSecret = "admin-test-secret"
	c.WorkerToken = "worker-test-token"
	c.AdministratorIDs = []string{"1"}
	if _, err := NewServiceContext(c, nil); err == nil || !strings.Contains(err.Error(), "user auth") {
		t.Fatalf("missing user issuer accepted: %v", err)
	}
	c.UserAuth.AccessSecret = "user-test-secret"
	if _, err := NewServiceContext(c, nil); err == nil || !strings.Contains(err.Error(), "user RPC configuration") {
		t.Fatalf("missing user RPC accepted: %v", err)
	}
}
