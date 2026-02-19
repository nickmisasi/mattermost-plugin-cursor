package ghclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/google/go-github/v68/github"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const baseURLPath = "/api-v3"

// setup creates a test HTTP server and a go-github Client configured to talk to it.
// Handlers registered on the returned mux will receive requests with baseURLPath stripped.
func setup(t *testing.T) (client Client, mux *http.ServeMux, serverURL string) {
	t.Helper()

	mux = http.NewServeMux()

	apiHandler := http.NewServeMux()
	apiHandler.Handle(baseURLPath+"/", http.StripPrefix(baseURLPath, mux))

	server := httptest.NewServer(apiHandler)
	t.Cleanup(server.Close)

	ghClient := github.NewClient(nil)
	u, _ := url.Parse(server.URL + baseURLPath + "/")
	ghClient.BaseURL = u

	return NewClientWithGitHub(ghClient), mux, server.URL
}

func TestRequestReviewers(t *testing.T) {
	client, mux, _ := setup(t)

	mux.HandleFunc("/repos/owner/repo/pulls/42/requested_reviewers", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)

		var req github.ReviewersRequest
		err := json.NewDecoder(r.Body).Decode(&req)
		require.NoError(t, err)
		assert.Equal(t, []string{"coderabbitai[bot]"}, req.Reviewers)

		// Return a minimal PR response (RequestReviewers returns *PullRequest).
		_, _ = fmt.Fprint(w, `{"number":42}`)
	})

	err := client.RequestReviewers(context.Background(), "owner", "repo", 42, github.ReviewersRequest{
		Reviewers: []string{"coderabbitai[bot]"},
	})
	require.NoError(t, err)
}

func TestCreateComment(t *testing.T) {
	client, mux, _ := setup(t)

	mux.HandleFunc("/repos/owner/repo/issues/42/comments", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)

		var body map[string]string
		err := json.NewDecoder(r.Body).Decode(&body)
		require.NoError(t, err)
		assert.Equal(t, "@cursor fix the lint errors", body["body"])

		w.WriteHeader(http.StatusCreated)
		_, _ = fmt.Fprint(w, `{"id":1,"body":"@cursor fix the lint errors"}`)
	})

	comment, err := client.CreateComment(context.Background(), "owner", "repo", 42, "@cursor fix the lint errors")
	require.NoError(t, err)
	assert.Equal(t, "@cursor fix the lint errors", comment.GetBody())
}

func TestListReviews(t *testing.T) {
	client, mux, _ := setup(t)

	page := 0
	mux.HandleFunc("/repos/owner/repo/pulls/42/reviews", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		page++

		switch page {
		case 1:
			// Page 1: return 1 review with Link header pointing to page 2.
			w.Header().Set("Link", fmt.Sprintf(`<http://%s%s/repos/owner/repo/pulls/42/reviews?page=2>; rel="next"`, r.Host, baseURLPath))
			_, _ = fmt.Fprint(w, `[{"id":1,"state":"COMMENTED","user":{"login":"coderabbitai[bot]"}}]`)
		case 2:
			// Page 2: return 1 review, no next link.
			_, _ = fmt.Fprint(w, `[{"id":2,"state":"APPROVED","user":{"login":"human-dev"}}]`)
		default:
			t.Fatal("unexpected page request")
		}
	})

	reviews, err := client.ListReviews(context.Background(), "owner", "repo", 42)
	require.NoError(t, err)
	assert.Len(t, reviews, 2)
	assert.Equal(t, "coderabbitai[bot]", reviews[0].GetUser().GetLogin())
	assert.Equal(t, "human-dev", reviews[1].GetUser().GetLogin())
}

func TestListReviewComments(t *testing.T) {
	client, mux, _ := setup(t)

	mux.HandleFunc("/repos/owner/repo/pulls/42/comments", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		_, _ = fmt.Fprint(w, `[{"id":1,"body":"fix this","path":"main.go","line":10,"user":{"login":"coderabbitai[bot]"}}]`)
	})

	comments, err := client.ListReviewComments(context.Background(), "owner", "repo", 42)
	require.NoError(t, err)
	assert.Len(t, comments, 1)
	assert.Equal(t, "fix this", comments[0].GetBody())
	assert.Equal(t, "main.go", comments[0].GetPath())
}

func TestListIssueComments(t *testing.T) {
	client, mux, _ := setup(t)

	page := 0
	mux.HandleFunc("/repos/owner/repo/issues/42/comments", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		page++

		switch page {
		case 1:
			w.Header().Set("Link", fmt.Sprintf(`<http://%s%s/repos/owner/repo/issues/42/comments?page=2>; rel="next"`, r.Host, baseURLPath))
			_, _ = fmt.Fprint(w, `[{"id":1,"body":"first","user":{"login":"coderabbitai[bot]"}}]`)
		case 2:
			_, _ = fmt.Fprint(w, `[{"id":2,"body":"second","user":{"login":"copilot-pull-request-reviewer"}}]`)
		default:
			t.Fatal("unexpected page request")
		}
	})

	comments, err := client.ListIssueComments(context.Background(), "owner", "repo", 42)
	require.NoError(t, err)
	assert.Len(t, comments, 2)
	assert.Equal(t, "first", comments[0].GetBody())
	assert.Equal(t, "second", comments[1].GetBody())
}

func TestReplyToReviewComment_PreferredEndpointSuccess(t *testing.T) {
	client, mux, _ := setup(t)

	preferredCalls := 0
	fallbackCalls := 0

	mux.HandleFunc("/repos/owner/repo/pulls/42/comments/101/replies", func(w http.ResponseWriter, r *http.Request) {
		preferredCalls++
		assert.Equal(t, http.MethodPost, r.Method)

		var body map[string]string
		err := json.NewDecoder(r.Body).Decode(&body)
		require.NoError(t, err)
		assert.Equal(t, "Thanks, fixed.", body["body"])

		w.WriteHeader(http.StatusCreated)
		_, _ = fmt.Fprint(w, `{"id":501,"body":"Thanks, fixed.","in_reply_to_id":101}`)
	})

	mux.HandleFunc("/repos/owner/repo/pulls/42/comments", func(w http.ResponseWriter, r *http.Request) {
		fallbackCalls++
		t.Fatalf("fallback endpoint should not be called in preferred success case")
	})

	comment, err := client.ReplyToReviewComment(context.Background(), "owner", "repo", 42, 101, "Thanks, fixed.")
	require.NoError(t, err)
	require.NotNil(t, comment)
	assert.Equal(t, int64(501), comment.GetID())
	assert.Equal(t, 1, preferredCalls)
	assert.Equal(t, 0, fallbackCalls)
}

func TestReplyToReviewComment_FallsBackToInReplyToEndpoint(t *testing.T) {
	client, mux, _ := setup(t)

	preferredCalls := 0
	fallbackCalls := 0

	mux.HandleFunc("/repos/owner/repo/pulls/42/comments/101/replies", func(w http.ResponseWriter, r *http.Request) {
		preferredCalls++
		assert.Equal(t, http.MethodPost, r.Method)
		http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
	})

	mux.HandleFunc("/repos/owner/repo/pulls/42/comments", func(w http.ResponseWriter, r *http.Request) {
		fallbackCalls++
		assert.Equal(t, http.MethodPost, r.Method)

		var body map[string]any
		err := json.NewDecoder(r.Body).Decode(&body)
		require.NoError(t, err)
		assert.Equal(t, "Thanks, fixed.", body["body"])
		assert.Equal(t, float64(101), body["in_reply_to"])

		w.WriteHeader(http.StatusCreated)
		_, _ = fmt.Fprint(w, `{"id":601,"body":"Thanks, fixed.","in_reply_to_id":101}`)
	})

	comment, err := client.ReplyToReviewComment(context.Background(), "owner", "repo", 42, 101, "Thanks, fixed.")
	require.NoError(t, err)
	require.NotNil(t, comment)
	assert.Equal(t, int64(601), comment.GetID())
	assert.Equal(t, 1, preferredCalls)
	assert.Equal(t, 1, fallbackCalls)
}

func TestReplyToReviewComment_ReturnsErrorWhenAllEndpointsFail(t *testing.T) {
	client, mux, _ := setup(t)

	mux.HandleFunc("/repos/owner/repo/pulls/42/comments/101/replies", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
	})

	mux.HandleFunc("/repos/owner/repo/pulls/42/comments", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		http.Error(w, `{"message":"validation failed"}`, http.StatusUnprocessableEntity)
	})

	comment, err := client.ReplyToReviewComment(context.Background(), "owner", "repo", 42, 101, "Thanks, fixed.")
	require.Error(t, err)
	assert.Nil(t, comment)
	assert.Contains(t, err.Error(), "reply to review comment failed")
	assert.Contains(t, err.Error(), "preferred")
	assert.Contains(t, err.Error(), "fallback")
}

func TestReplyToReviewComment_DoesNotFallbackOnPreferredServerError(t *testing.T) {
	client, mux, _ := setup(t)

	fallbackCalls := 0

	mux.HandleFunc("/repos/owner/repo/pulls/42/comments/101/replies", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		http.Error(w, `{"message":"server error"}`, http.StatusInternalServerError)
	})

	mux.HandleFunc("/repos/owner/repo/pulls/42/comments", func(w http.ResponseWriter, r *http.Request) {
		fallbackCalls++
		t.Fatalf("fallback endpoint should not be called for preferred 5xx errors")
	})

	comment, err := client.ReplyToReviewComment(context.Background(), "owner", "repo", 42, 101, "Thanks, fixed.")
	require.Error(t, err)
	assert.Nil(t, comment)
	assert.Contains(t, err.Error(), "preferred")
	assert.Equal(t, 0, fallbackCalls)
}

func TestParsePRURL(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		want    *PRReference
		wantErr bool
	}{
		{
			name: "valid HTTPS URL",
			url:  "https://github.com/mattermost/mattermost/pull/12345",
			want: &PRReference{Owner: "mattermost", Repo: "mattermost", Number: 12345},
		},
		{
			name: "valid HTTP URL",
			url:  "http://github.com/owner/repo/pull/1",
			want: &PRReference{Owner: "owner", Repo: "repo", Number: 1},
		},
		{
			name: "valid URL with trailing path",
			url:  "https://github.com/owner/repo/pull/42/files",
			want: &PRReference{Owner: "owner", Repo: "repo", Number: 42},
		},
		{
			name: "valid URL with query string",
			url:  "https://github.com/owner/repo/pull/42?diff=split",
			want: &PRReference{Owner: "owner", Repo: "repo", Number: 42},
		},
		{
			name:    "not a GitHub URL",
			url:     "https://gitlab.com/owner/repo/pull/42",
			wantErr: true,
		},
		{
			name:    "missing PR number",
			url:     "https://github.com/owner/repo/pull/",
			wantErr: true,
		},
		{
			name:    "not a pull request URL",
			url:     "https://github.com/owner/repo/issues/42",
			wantErr: true,
		},
		{
			name:    "empty string",
			url:     "",
			wantErr: true,
		},
		{
			name:    "random string",
			url:     "not a url at all",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParsePRURL(tt.url)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, got)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func TestNewClient_NilForEmptyToken(t *testing.T) {
	client := NewClient("")
	assert.Nil(t, client)
}

func TestNewClient_NonNilForValidToken(t *testing.T) {
	client := NewClient("ghp_test123")
	assert.NotNil(t, client)
}
