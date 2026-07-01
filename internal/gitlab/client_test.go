package gitlab_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/martmull/github-to-gitlab-mirror/internal/gitlab"
)

func TestResolveGroup(t *testing.T) {
	ctx := context.Background()

	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v4/groups/my-group/subgroup" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"id":        42,
				"full_path": "my-group/subgroup",
			})
			return
		}
		w.WriteHeader(404)
	}))
	defer srv.Close()

	client, err := gitlab.NewClientWithURL("test-token", srv.URL)
	if err != nil {
		t.Fatalf("NewClientWithURL: %v", err)
	}
	info, err := client.ResolveGroup(ctx, "my-group/subgroup")
	if err != nil {
		t.Fatalf("ResolveGroup: %v", err)
	}
	if info.ID != 42 {
		t.Errorf("expected ID 42, got %d", info.ID)
	}
	if info.FullPath != "my-group/subgroup" {
		t.Errorf("expected FullPath my-group/subgroup, got %s", info.FullPath)
	}
}

func TestEnsureProject_CreatesWhenNotFound(t *testing.T) {
	ctx := context.Background()
	created := false

	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && r.URL.Path == "/api/v4/projects/my-group/my-repo" {
			w.WriteHeader(404)
			json.NewEncoder(w).Encode(map[string]string{"message": "Not Found"})
			return
		}
		if r.Method == "POST" && r.URL.Path == "/api/v4/projects" {
			created = true
			w.WriteHeader(201)
			json.NewEncoder(w).Encode(map[string]interface{}{"id": 1, "name": "my-repo"})
			return
		}
		w.WriteHeader(404)
	}))
	defer srv.Close()

	client, err := gitlab.NewClientWithURL("test-token", srv.URL)
	if err != nil {
		t.Fatalf("NewClientWithURL: %v", err)
	}
	group := gitlab.GroupInfo{ID: 42, FullPath: "my-group"}
	err = client.EnsureProject(ctx, group, "my-repo", true)
	if err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	if !created {
		t.Error("expected project to be created")
	}
}

func TestEnsureProject_SkipsWhenExists(t *testing.T) {
	ctx := context.Background()

	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && r.URL.Path == "/api/v4/projects/my-group/my-repo" {
			json.NewEncoder(w).Encode(map[string]interface{}{"id": 1, "name": "my-repo"})
			return
		}
		if r.Method == "POST" {
			t.Error("should not have called POST when project exists")
		}
		w.WriteHeader(404)
	}))
	defer srv.Close()

	client, err := gitlab.NewClientWithURL("test-token", srv.URL)
	if err != nil {
		t.Fatalf("NewClientWithURL: %v", err)
	}
	group := gitlab.GroupInfo{ID: 42, FullPath: "my-group"}
	err = client.EnsureProject(ctx, group, "my-repo", false)
	if err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
}

func TestSetDefaultBranch(t *testing.T) {
	ctx := context.Background()

	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "PUT" && r.URL.Path == "/api/v4/projects/my-group/my-repo" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"id":             1,
				"default_branch": "develop",
			})
			return
		}
		w.WriteHeader(404)
	}))
	defer srv.Close()

	client, err := gitlab.NewClientWithURL("test-token", srv.URL)
	if err != nil {
		t.Fatalf("NewClientWithURL: %v", err)
	}
	err = client.SetDefaultBranch(ctx, "my-group/my-repo", "develop")
	if err != nil {
		t.Fatalf("SetDefaultBranch: %v", err)
	}
}
