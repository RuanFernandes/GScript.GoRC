package gallery

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestListProjectsPreservesPathAndQuery(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/gallery/projects" {
			t.Errorf("request path = %q, want /api/gallery/projects", request.URL.Path)
		}
		if request.URL.Query().Get("type") != "weapon" {
			t.Errorf("request type query = %q, want weapon", request.URL.Query().Get("type"))
		}
		if request.URL.Query().Get("q") != "ice & fire" {
			t.Errorf("request q query = %q, want ice & fire", request.URL.Query().Get("q"))
		}
		response.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(response).Encode(projectsResponse{Projects: []Project{{ID: "project-1", Name: "Ice"}}})
	}))
	defer server.Close()

	baseURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse test server URL: %v", err)
	}
	client := &Client{baseURL: baseURL, httpClient: server.Client()}

	projects, err := client.ListProjects(context.Background(), "", "weapon", "ice & fire")
	if err != nil {
		t.Fatalf("list projects: %v", err)
	}
	if len(projects) != 1 || projects[0].ID != "project-1" {
		t.Fatalf("projects = %#v, want one project with ID project-1", projects)
	}
}
