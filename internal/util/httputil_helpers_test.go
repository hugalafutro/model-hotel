package util

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFilterContainers_ComposeProject(t *testing.T) {
	all := []DockerContainer{
		{
			Name:   "app1",
			Labels: map[string]string{"com.docker.compose.project": "myproject"},
		},
		{
			Name:   "app2",
			Labels: map[string]string{"com.docker.compose.project": "other"},
		},
		{
			Name:   "app3",
			Labels: map[string]string{},
		},
	}
	result := filterContainers(all, ContainerFilter{ComposeProject: "myproject"})
	if len(result) != 1 {
		t.Fatalf("Expected 1 container, got %d", len(result))
	}
	if result[0].Name != "app1" {
		t.Errorf("Expected app1, got %s", result[0].Name)
	}
}

func TestFilterContainers_AppGroup(t *testing.T) {
	all := []DockerContainer{
		{
			Name:   "app1",
			Labels: map[string]string{"app.group": "model-hotel"},
		},
		{
			Name:   "app2",
			Labels: map[string]string{"app.group": "other-app"},
		},
	}
	result := filterContainers(all, ContainerFilter{AppGroup: "model-hotel"})
	if len(result) != 1 {
		t.Fatalf("Expected 1 container, got %d", len(result))
	}
	if result[0].Name != "app1" {
		t.Errorf("Expected app1, got %s", result[0].Name)
	}
}

func TestFilterContainers_EmptyFilter(t *testing.T) {
	all := []DockerContainer{
		{Name: "app1", Labels: map[string]string{"com.docker.compose.project": "x"}},
	}
	result := filterContainers(all, ContainerFilter{})
	if len(result) != 0 {
		t.Errorf("Expected 0 containers with empty filter, got %d", len(result))
	}
}

func TestFilterContainers_EmptyInput(t *testing.T) {
	result := filterContainers(nil, ContainerFilter{ComposeProject: "test"})
	if len(result) != 0 {
		t.Errorf("Expected 0 containers, got %d", len(result))
	}
}

func TestBuildProviderTargetURL_Anthropic(t *testing.T) {
	tests := []struct {
		baseURL string
		want    string
	}{
		{"https://api.anthropic.com", "https://api.anthropic.com/v1/chat/completions"},
		{"https://api.anthropic.com/v1", "https://api.anthropic.com/v1/chat/completions"},
	}
	for _, tc := range tests {
		t.Run(tc.baseURL, func(t *testing.T) {
			if got := BuildProviderTargetURL(tc.baseURL, "anthropic"); got != tc.want {
				t.Errorf("BuildProviderTargetURL(%q, anthropic) = %q, want %q", tc.baseURL, got, tc.want)
			}
		})
	}
}

func TestBuildProviderTargetURL_Other(t *testing.T) {
	got := BuildProviderTargetURL("https://api.openai.com/v1", "openai")
	want := "https://api.openai.com/v1/chat/completions"
	if got != want {
		t.Errorf("BuildProviderTargetURL(openai) = %q, want %q", got, want)
	}
}

func TestSetProviderAuthHeaders_Anthropic(t *testing.T) {
	req := httptest.NewRequest("POST", "/", http.NoBody)
	SetProviderAuthHeaders(req, "anthropic", "sk-test-key")

	if got := req.Header.Get("x-api-key"); got != "sk-test-key" {
		t.Errorf("Expected x-api-key header, got %q", got)
	}
	if got := req.Header.Get("anthropic-version"); got != "2023-06-01" {
		t.Errorf("Expected anthropic-version header, got %q", got)
	}
}

func TestSetProviderAuthHeaders_Default(t *testing.T) {
	req := httptest.NewRequest("POST", "/", http.NoBody)
	SetProviderAuthHeaders(req, "openai", "sk-test-key")

	if got := req.Header.Get("Authorization"); got != "Bearer sk-test-key" {
		t.Errorf("Expected Bearer auth header, got %q", got)
	}
}

func TestSetProviderAuthHeaders_EmptyKey(t *testing.T) {
	req := httptest.NewRequest("POST", "/", http.NoBody)
	SetProviderAuthHeaders(req, "openai", "")

	if got := req.Header.Get("Authorization"); got != "" {
		t.Errorf("Expected no auth header for empty key, got %q", got)
	}
}
