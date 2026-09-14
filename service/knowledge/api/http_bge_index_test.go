package main

import (
	"encoding/json"
	"net"
	"net/url"
	"os"
	"testing"

	"sea-try-go/service/knowledge/api/internal/types"
)

// The live DC acceptance owns this disposable gateway and the pinned provider.
// Read only its profile receipt; the index worker calls the gateway itself.
func liveBGEIndexProfiles(t *testing.T, path string) []types.RetrievalProfile {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var runtime struct {
		Endpoint       string `json:"endpoint"`
		AccessToken    string `json:"access_token"`
		Configurations map[string]struct {
			Profile struct {
				Model    string `json:"model"`
				Space    string `json:"representation_space"`
				Contract struct {
					Dimensions  int    `json:"dimensions"`
					TokenizerID string `json:"tokenizer_id"`
					Aggregation string `json:"aggregation"`
				} `json:"representation_contract"`
			} `json:"profile"`
		} `json:"configurations"`
	}
	if err := json.Unmarshal(raw, &runtime); err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(runtime.Endpoint)
	if err != nil || u.Scheme != "http" || net.ParseIP(u.Hostname()) == nil ||
		!net.ParseIP(u.Hostname()).IsLoopback() || runtime.AccessToken == "" || len(runtime.Configurations) != 3 {
		t.Fatal("live BGE runtime must be a disposable loopback DC gateway with all three profiles")
	}
	profiles := make([]types.RetrievalProfile, 0, 3)
	for _, lane := range []struct{ key, name string }{{"dense", "dense"}, {"sparse", "sparse"}, {"token_matrix", "multivector"}} {
		p, ok := runtime.Configurations[lane.key]
		if !ok || p.Profile.Model == "" || p.Profile.Space == "" || p.Profile.Contract.Dimensions <= 0 ||
			p.Profile.Contract.TokenizerID == "" {
			t.Fatalf("live BGE %s profile missing", lane.key)
		}
		profile := types.RetrievalProfile{Lane: lane.name, Encoder: p.Profile.Model,
			Tokenizer: p.Profile.Contract.TokenizerID, Space: p.Profile.Space,
			Dimensions: p.Profile.Contract.Dimensions}
		if lane.name == "multivector" {
			profile.Mask, profile.Aggregation = "valid", p.Profile.Contract.Aggregation
			if profile.Aggregation != "mean_maxsim" {
				t.Fatal("live BGE multi-vector profile must use mean_maxsim")
			}
		}
		profiles = append(profiles, profile)
	}
	return profiles
}
