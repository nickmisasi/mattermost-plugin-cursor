package ghclient

import (
	"context"
	"fmt"
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
}

// clientImpl implements Client by delegating to go-github.
type clientImpl struct {
	gh *github.Client
}

// NewClient creates a new GitHub API client authenticated with the given PAT.
// Returns nil if token is empty.
func NewClient(token string) Client {
	if token == "" {
		return nil
	}
	return &clientImpl{
		gh: github.NewClient(nil).WithAuthToken(token),
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
