package ghclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"

	"github.com/google/go-github/v68/github"
)

// Client wraps the subset of the GitHub API needed for the review loop.
type Client interface {
	// RequestReviewers adds reviewers (users and/or teams) to a PR.
	RequestReviewers(ctx context.Context, owner, repo string, prNumber int, reviewers github.ReviewersRequest) error

	// CreateComment posts a comment on a PR (uses the issues comment API).
	CreateComment(ctx context.Context, owner, repo string, prNumber int, body string) (*github.IssueComment, error)

	// ListReviews returns all reviews on a PR (auto-paginates).
	ListReviews(ctx context.Context, owner, repo string, prNumber int) ([]*github.PullRequestReview, error)

	// ListReviewComments returns all inline review comments on a PR (auto-paginates).
	ListReviewComments(ctx context.Context, owner, repo string, prNumber int) ([]*github.PullRequestComment, error)

	// MarkPRReadyForReview transitions a draft PR to ready-for-review.
	// This is required because Cursor creates PRs as drafts, and AI reviewers
	// (e.g., CodeRabbit) skip draft PRs.
	MarkPRReadyForReview(ctx context.Context, owner, repo string, prNumber int) error

	// GetPullRequestByBranch finds an open PR with the given head branch.
	// Returns nil, nil if no matching PR is found.
	GetPullRequestByBranch(ctx context.Context, owner, repo, branch string) (*github.PullRequest, error)
}

// clientImpl implements Client by delegating to go-github.
type clientImpl struct {
	gh    *github.Client
	token string // stored for raw GraphQL requests
}

// NewClient creates a new GitHub API client authenticated with the given PAT.
// Returns nil if token is empty.
func NewClient(token string) Client {
	if token == "" {
		return nil
	}
	return &clientImpl{
		gh:    github.NewClient(nil).WithAuthToken(token),
		token: token,
	}
}

// NewClientWithGitHub creates a Client from an existing *github.Client.
// Used in tests to inject a client pointing at an httptest server.
func NewClientWithGitHub(gh *github.Client) Client {
	return &clientImpl{gh: gh}
}

func (c *clientImpl) RequestReviewers(ctx context.Context, owner, repo string, prNumber int, reviewers github.ReviewersRequest) error {
	_, _, err := c.gh.PullRequests.RequestReviewers(ctx, owner, repo, prNumber, reviewers)
	return err
}

func (c *clientImpl) CreateComment(ctx context.Context, owner, repo string, prNumber int, body string) (*github.IssueComment, error) {
	comment, _, err := c.gh.Issues.CreateComment(ctx, owner, repo, prNumber, &github.IssueComment{
		Body: github.Ptr(body),
	})
	return comment, err
}

func (c *clientImpl) ListReviews(ctx context.Context, owner, repo string, prNumber int) ([]*github.PullRequestReview, error) {
	var all []*github.PullRequestReview
	opts := &github.ListOptions{PerPage: 100}
	for {
		reviews, resp, err := c.gh.PullRequests.ListReviews(ctx, owner, repo, prNumber, opts)
		if err != nil {
			return nil, err
		}
		all = append(all, reviews...)
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	return all, nil
}

func (c *clientImpl) MarkPRReadyForReview(ctx context.Context, owner, repo string, prNumber int) error {
	// First check if the PR is actually a draft.
	pr, _, err := c.gh.PullRequests.Get(ctx, owner, repo, prNumber)
	if err != nil {
		return fmt.Errorf("failed to get PR: %w", err)
	}
	if !pr.GetDraft() {
		return nil // Already not a draft.
	}

	// Try REST API first (works with fine-grained PATs that have pull_requests:write).
	draft := false
	_, _, restErr := c.gh.PullRequests.Edit(ctx, owner, repo, prNumber, &github.PullRequest{Draft: &draft})
	if restErr == nil {
		// Verify it actually changed.
		updated, _, verifyErr := c.gh.PullRequests.Get(ctx, owner, repo, prNumber)
		if verifyErr == nil && !updated.GetDraft() {
			return nil // REST API worked.
		}
	}

	// REST didn't work — fall back to GraphQL mutation.
	nodeID := pr.GetNodeID()
	if nodeID == "" {
		return fmt.Errorf("PR %d has no node ID; REST also failed: %v", prNumber, restErr)
	}

	return c.graphqlMarkReady(ctx, nodeID)
}

// graphqlMarkReady calls the markPullRequestReadyForReview GraphQL mutation.
func (c *clientImpl) graphqlMarkReady(ctx context.Context, pullRequestNodeID string) error {
	query := `mutation($id: ID!) {
		markPullRequestReadyForReview(input: {pullRequestId: $id}) {
			pullRequest { isDraft }
		}
	}`

	payload := map[string]any{
		"query":     query,
		"variables": map[string]string{"id": pullRequestNodeID},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal GraphQL request: %w", err)
	}

	graphqlURL := "https://api.github.com/graphql"
	// Support custom base URLs (e.g. httptest servers in tests).
	if base := c.gh.BaseURL.String(); base != "" && base != "https://api.github.com/" {
		graphqlURL = base + "graphql"
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, graphqlURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create GraphQL request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("GraphQL request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("GraphQL returned HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	// Check for GraphQL-level errors.
	var result struct {
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil // Response parsed fine, mutation likely succeeded.
	}
	if len(result.Errors) > 0 {
		return fmt.Errorf("GraphQL error: %s", result.Errors[0].Message)
	}
	return nil
}

func (c *clientImpl) ListReviewComments(ctx context.Context, owner, repo string, prNumber int) ([]*github.PullRequestComment, error) {
	var all []*github.PullRequestComment
	opts := &github.PullRequestListCommentsOptions{
		ListOptions: github.ListOptions{PerPage: 100},
	}
	for {
		comments, resp, err := c.gh.PullRequests.ListComments(ctx, owner, repo, prNumber, opts)
		if err != nil {
			return nil, err
		}
		all = append(all, comments...)
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	return all, nil
}

func (c *clientImpl) GetPullRequestByBranch(ctx context.Context, owner, repo, branch string) (*github.PullRequest, error) {
	prs, _, err := c.gh.PullRequests.List(ctx, owner, repo, &github.PullRequestListOptions{
		Head:        owner + ":" + branch,
		State:       "open",
		ListOptions: github.ListOptions{PerPage: 1},
	})
	if err != nil {
		return nil, err
	}
	if len(prs) == 0 {
		return nil, nil
	}
	return prs[0], nil
}

// --- PR URL Parser ---

var prURLRegex = regexp.MustCompile(`^https?://github\.com/([^/]+)/([^/]+)/pull/(\d+)`)

// PRReference holds the parsed components of a GitHub PR URL.
type PRReference struct {
	Owner  string
	Repo   string
	Number int
}

// ParsePRURL parses a GitHub pull request URL into its owner, repo, and number.
// Returns an error if the URL does not match the expected format.
func ParsePRURL(rawURL string) (*PRReference, error) {
	matches := prURLRegex.FindStringSubmatch(rawURL)
	if matches == nil {
		return nil, fmt.Errorf("invalid GitHub PR URL: %q", rawURL)
	}
	number, err := strconv.Atoi(matches[3])
	if err != nil {
		return nil, fmt.Errorf("invalid PR number in URL %q: %w", rawURL, err)
	}
	return &PRReference{
		Owner:  matches[1],
		Repo:   matches[2],
		Number: number,
	}, nil
}
