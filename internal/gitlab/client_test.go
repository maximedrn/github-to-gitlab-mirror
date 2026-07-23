package gitlab_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/maximedrn/github-to-gitlab-mirror/internal/gitlab"
)

// TestResolveGroup verifies that ResolveGroup calls the GitLab groups
// endpoint and returns the identifier and full path decoded from the
// response payload.
func TestResolveGroup(test *testing.T) {
	var requestContext context.Context = context.Background()

	var server *httptest.Server = httptest.NewServer(http.HandlerFunc(
		func(writer http.ResponseWriter, request *http.Request) {
			if request.URL.Path == "/api/v4/groups/my-group/subgroup" {
				var encodeError error = json.NewEncoder(writer).Encode(
					map[string]any{
						"id":        42,
						"full_path": "my-group/subgroup",
					},
				)
				if encodeError != nil {
					test.Fatalf("Encode response: %v", encodeError)
				}
				return
			}
			writer.WriteHeader(404)
		}))
	defer server.Close()

	var client *gitlab.Client
	var clientError error
	client, clientError = gitlab.NewClientWithURL("test-token", server.URL)
	if clientError != nil {
		test.Fatalf("NewClientWithURL: %v", clientError)
	}
	var info gitlab.GroupInfo
	var resolveError error
	info, resolveError = client.ResolveGroup(
		requestContext,
		"my-group/subgroup",
	)
	if resolveError != nil {
		test.Fatalf("ResolveGroup: %v", resolveError)
	}
	if info.ID != 42 {
		test.Errorf("Expected ID 42, got %d", info.ID)
	}
	if info.FullPath != "my-group/subgroup" {
		test.Errorf(
			"Expected FullPath my-group/subgroup, got %s",
			info.FullPath,
		)
	}
}

// TestEnsureProject_CreatesWhenNotFound verifies that EnsureProject
// issues a POST /projects when the target project does not yet exist.
func TestEnsureProject_CreatesWhenNotFound(test *testing.T) {
	var requestContext context.Context = context.Background()
	var created bool = false

	var server *httptest.Server = httptest.NewServer(http.HandlerFunc(
		func(writer http.ResponseWriter, request *http.Request) {
			if request.Method == "GET" &&
				request.URL.Path == "/api/v4/projects/my-group/my-repo" {
				writer.WriteHeader(404)
				var encodeError error = json.NewEncoder(writer).Encode(
					map[string]string{"message": "Not Found"},
				)
				if encodeError != nil {
					test.Fatalf("encode 404 body: %v", encodeError)
				}
				return
			}
			if request.Method == "POST" &&
				request.URL.Path == "/api/v4/projects" {
				created = true
				writer.WriteHeader(201)
				var encodeError error = json.NewEncoder(writer).Encode(
					map[string]any{"id": 1, "name": "my-repo"},
				)
				if encodeError != nil {
					test.Fatalf("encode 201 body: %v", encodeError)
				}
				return
			}
			writer.WriteHeader(404)
		}))
	defer server.Close()

	var client *gitlab.Client
	var clientError error
	client, clientError = gitlab.NewClientWithURL("test-token", server.URL)
	if clientError != nil {
		test.Fatalf("NewClientWithURL: %v", clientError)
	}
	var group gitlab.GroupInfo = gitlab.GroupInfo{
		ID:       42,
		FullPath: "my-group",
	}
	var ensureError error = client.EnsureProject(
		requestContext,
		group,
		"my-repo",
		true,
	)
	if ensureError != nil {
		test.Fatalf("EnsureProject: %v", ensureError)
	}
	if !created {
		test.Error("Expected project to be created")
	}
}

// TestEnsureProject_SkipsWhenExists verifies that EnsureProject does not
// issue any POST or PUT request when the target project already exists
// and has Git LFS enabled.
func TestEnsureProject_SkipsWhenExists(test *testing.T) {
	var requestContext context.Context = context.Background()

	var server *httptest.Server = httptest.NewServer(http.HandlerFunc(
		func(writer http.ResponseWriter, request *http.Request) {
			if request.Method == "GET" &&
				request.URL.Path == "/api/v4/projects/my-group/my-repo" {
				var encodeError error = json.NewEncoder(writer).Encode(
					map[string]any{
						"id":          1,
						"name":        "my-repo",
						"lfs_enabled": true,
					},
				)
				if encodeError != nil {
					test.Fatalf("Encode response: %v", encodeError)
				}
				return
			}
			if request.Method == "POST" {
				test.Error("Expected no POST request when project exists")
			}
			if request.Method == "PUT" {
				test.Error("Expected no PUT request when LFS already enabled")
			}
			writer.WriteHeader(404)
		}))
	defer server.Close()

	var client *gitlab.Client
	var clientError error
	client, clientError = gitlab.NewClientWithURL("test-token", server.URL)
	if clientError != nil {
		test.Fatalf("NewClientWithURL: %v", clientError)
	}
	var group gitlab.GroupInfo = gitlab.GroupInfo{
		ID:       42,
		FullPath: "my-group",
	}
	var ensureError error = client.EnsureProject(
		requestContext,
		group,
		"my-repo",
		false,
	)
	if ensureError != nil {
		test.Fatalf("EnsureProject: %v", ensureError)
	}
}

// TestEnsureProject_EnablesLFSWhenExists verifies that EnsureProject issues
// a PUT to enable Git LFS when the target project already exists but has
// Git LFS disabled (for instance a project created by a previous run of
// the tool before LFS was turned on by default).
func TestEnsureProject_EnablesLFSWhenExists(test *testing.T) {
	var requestContext context.Context = context.Background()
	var lfsEnabled bool = false

	var server *httptest.Server = httptest.NewServer(http.HandlerFunc(
		func(writer http.ResponseWriter, request *http.Request) {
			if request.Method == "GET" &&
				request.URL.Path == "/api/v4/projects/my-group/my-repo" {
				var encodeError error = json.NewEncoder(writer).Encode(
					map[string]any{
						"id":          1,
						"name":        "my-repo",
						"lfs_enabled": lfsEnabled,
					},
				)
				if encodeError != nil {
					test.Fatalf("Encode response: %v", encodeError)
				}
				return
			}
			if request.Method == "PUT" &&
				request.URL.Path == "/api/v4/projects/my-group/my-repo" {
				var payload struct {
					LFSEnabled bool `json:"lfs_enabled"`
				}
				var decodeError error = json.NewDecoder(request.Body).Decode(
					&payload,
				)
				if decodeError != nil {
					test.Fatalf("Decode request body: %v", decodeError)
				}
				if !payload.LFSEnabled {
					test.Error(
						"Expected lfs_enabled=true in request body",
					)
				}
				lfsEnabled = true
				var encodeError error = json.NewEncoder(writer).Encode(
					map[string]any{
						"id":          1,
						"name":        "my-repo",
						"lfs_enabled": true,
					},
				)
				if encodeError != nil {
					test.Fatalf("Encode response: %v", encodeError)
				}
				return
			}
			if request.Method == "POST" {
				test.Error("Expected no POST request when project exists")
			}
			writer.WriteHeader(404)
		}))
	defer server.Close()

	var client *gitlab.Client
	var clientError error
	client, clientError = gitlab.NewClientWithURL("test-token", server.URL)
	if clientError != nil {
		test.Fatalf("NewClientWithURL: %v", clientError)
	}
	var group gitlab.GroupInfo = gitlab.GroupInfo{
		ID:       42,
		FullPath: "my-group",
	}
	var ensureError error = client.EnsureProject(
		requestContext,
		group,
		"my-repo",
		false,
	)
	if ensureError != nil {
		test.Fatalf("EnsureProject: %v", ensureError)
	}
	if !lfsEnabled {
		test.Error("Expected LFS to be enabled via PUT")
	}
}

// TestSetDefaultBranch verifies that SetDefaultBranch issues the expected
// PUT request against the GitLab projects endpoint.
func TestSetDefaultBranch(test *testing.T) {
	var requestContext context.Context = context.Background()

	var server *httptest.Server = httptest.NewServer(http.HandlerFunc(
		func(writer http.ResponseWriter, request *http.Request) {
			if request.Method == "PUT" &&
				request.URL.Path == "/api/v4/projects/my-group/my-repo" {
				var encodeError error = json.NewEncoder(writer).Encode(
					map[string]any{
						"id":             1,
						"default_branch": "develop",
					},
				)
				if encodeError != nil {
					test.Fatalf("Encode response: %v", encodeError)
				}
				return
			}
			writer.WriteHeader(404)
		}))
	defer server.Close()

	var client *gitlab.Client
	var clientError error
	client, clientError = gitlab.NewClientWithURL("test-token", server.URL)
	if clientError != nil {
		test.Fatalf("NewClientWithURL: %v", clientError)
	}
	var setError error = client.SetDefaultBranch(
		requestContext,
		"my-group/my-repo",
		"develop",
	)
	if setError != nil {
		test.Fatalf("SetDefaultBranch: %v", setError)
	}
}
