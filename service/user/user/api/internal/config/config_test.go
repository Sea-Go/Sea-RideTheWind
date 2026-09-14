package config

import (
	"testing"

	"github.com/zeromicro/go-zero/core/conf"
)

func TestLegacyUserCenterConfigLeavesAccountLinkDisabled(t *testing.T) {
	var c Config
	if err := conf.Load("../../etc/usercenter.yaml", &c); err != nil {
		t.Fatalf("existing User Center config no longer parses: %v", err)
	}
	if c.AccountLink.Enabled || c.AccountLink.PostgresDSN != "" || c.AccountLink.DataCenterMeURL != "" {
		t.Fatal("account link must remain off unless explicitly configured")
	}
}
