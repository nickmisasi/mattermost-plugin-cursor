package parser

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func boolPtr(b bool) *bool {
	return &b
}

func TestParse(t *testing.T) {
	tests := []struct {
		name       string
		message    string
		botMention string
		expected   *ParsedMention // nil means "should return nil"
	}{
		// --- Basic prompts ---
		{
			name:       "simple prompt",
			message:    "@cursor fix the login bug",
			botMention: "@cursor",
			expected:   &ParsedMention{Prompt: "fix the login bug"},
		},
		{
			name:       "empty after mention",
			message:    "@cursor",
			botMention: "@cursor",
			expected:   nil,
		},
		{
			name:       "whitespace only after mention",
			message:    "@cursor   ",
			botMention: "@cursor",
			expected:   nil,
		},

		// --- Natural language repo ---
		{
			name:       "in repo short name no longer matches",
			message:    "@cursor in backend-api, fix the auth issue",
			botMention: "@cursor",
			expected:   &ParsedMention{Prompt: "in backend-api, fix the auth issue"},
		},
		{
			name:       "in org/repo",
			message:    "@cursor in org/repo, fix the auth issue",
			botMention: "@cursor",
			expected:   &ParsedMention{Prompt: "fix the auth issue", Repository: "org/repo"},
		},

		// --- False positive prevention for "in <word>" ---
		{
			name:       "in common word not extracted as repo",
			message:    "@cursor fix the alignment issue in compact mode",
			botMention: "@cursor",
			expected:   &ParsedMention{Prompt: "fix the alignment issue in compact mode"},
		},
		{
			name:       "in production not extracted as repo",
			message:    "@cursor fix issue in production environment",
			botMention: "@cursor",
			expected:   &ParsedMention{Prompt: "fix issue in production environment"},
		},
		{
			name:       "in org/repo extracted correctly",
			message:    "@cursor fix issue in mattermost/mattermost",
			botMention: "@cursor",
			expected:   &ParsedMention{Prompt: "fix issue", Repository: "mattermost/mattermost"},
		},
		{
			name:       "in org/repo with comma extracted correctly",
			message:    "@cursor fix issue in org/repo, on main",
			botMention: "@cursor",
			expected:   &ParsedMention{Prompt: "fix issue on main", Repository: "org/repo"},
		},
		{
			name:       "repo=single-word still works",
			message:    "@cursor repo=myrepo fix issue",
			botMention: "@cursor",
			expected:   &ParsedMention{Prompt: "fix issue", Repository: "myrepo"},
		},

		// --- Natural language model ---
		{
			name:       "with model",
			message:    "@cursor with opus, fix the login bug",
			botMention: "@cursor",
			expected:   &ParsedMention{Prompt: "fix the login bug", Model: "opus"},
		},

		// --- Combined natural language ---
		{
			name:       "in org/repo with model",
			message:    "@cursor in org/backend-api, with opus, fix it",
			botMention: "@cursor",
			expected:   &ParsedMention{Prompt: "fix it", Repository: "org/backend-api", Model: "opus"},
		},

		// --- Inline key=value ---
		{
			name:       "inline branch and autopr",
			message:    "@cursor branch=dev autopr=false Fix the bug",
			botMention: "@cursor",
			expected:   &ParsedMention{Prompt: "Fix the bug", Branch: "dev", AutoPR: boolPtr(false)},
		},
		{
			name:       "inline repo model and branch",
			message:    "@cursor repo=org/repo model=o3 branch=dev Fix it",
			botMention: "@cursor",
			expected:   &ParsedMention{Prompt: "Fix it", Repository: "org/repo", Branch: "dev", Model: "o3"},
		},

		// --- Bracketed options ---
		{
			name:       "bracketed options",
			message:    "@cursor [branch=dev, model=o3, repo=org/repo] Fix the bug",
			botMention: "@cursor",
			expected:   &ParsedMention{Prompt: "Fix the bug", Repository: "org/repo", Branch: "dev", Model: "o3"},
		},

		// --- Force new agent ---
		{
			name:       "force new agent",
			message:    "@cursor agent start a new agent for this",
			botMention: "@cursor",
			expected:   &ParsedMention{Prompt: "start a new agent for this", ForceNew: true},
		},

		// --- Mixed ---
		{
			name:       "bracket + natural",
			message:    "@cursor [repo=org/repo] with opus, fix it",
			botMention: "@cursor",
			expected:   &ParsedMention{Prompt: "fix it", Repository: "org/repo", Model: "opus"},
		},

		// --- Case insensitivity ---
		{
			name:       "case insensitive mention",
			message:    "@Cursor fix it",
			botMention: "@cursor",
			expected:   &ParsedMention{Prompt: "fix it"},
		},

		// --- Edge cases ---
		{
			name:       "no mention present",
			message:    "hello world",
			botMention: "@cursor",
			expected:   nil,
		},
		{
			name:       "mention in middle",
			message:    "hey @cursor fix it",
			botMention: "@cursor",
			expected:   &ParsedMention{Prompt: "fix it"},
		},
		{
			name:       "autopr true",
			message:    "@cursor autopr=true fix it",
			botMention: "@cursor",
			expected:   &ParsedMention{Prompt: "fix it", AutoPR: boolPtr(true)},
		},
		{
			name:       "different bot username",
			message:    "@cursor-bot fix the bug",
			botMention: "@cursor-bot",
			expected:   &ParsedMention{Prompt: "fix the bug"},
		},
		{
			name:       "inline options only no extra text",
			message:    "@cursor repo=org/repo branch=main",
			botMention: "@cursor",
			expected:   &ParsedMention{Repository: "org/repo", Branch: "main"},
		},
		{
			name:       "bracket option overrides natural language",
			message:    "@cursor [repo=explicit/repo] in ignored/repo, fix it",
			botMention: "@cursor",
			expected:   &ParsedMention{Prompt: "fix it", Repository: "explicit/repo"},
		},
		{
			name:       "inline option overrides natural language",
			message:    "@cursor repo=explicit/repo in ignored/repo, fix it",
			botMention: "@cursor",
			expected:   &ParsedMention{Prompt: "fix it", Repository: "explicit/repo"},
		},
		{
			name:       "all options combined",
			message:    "@cursor [repo=org/repo] branch=dev autopr=true with opus, fix the thing",
			botMention: "@cursor",
			expected:   &ParsedMention{Prompt: "fix the thing", Repository: "org/repo", Branch: "dev", Model: "opus", AutoPR: boolPtr(true)},
		},
		{
			name:       "agent prefix with options",
			message:    "@cursor agent repo=org/repo fix the thing",
			botMention: "@cursor",
			expected:   &ParsedMention{Prompt: "fix the thing", Repository: "org/repo", ForceNew: true},
		},

		// --- HITL flags ---
		{
			name:       "no-review flag",
			message:    "@cursor --no-review fix the login bug",
			botMention: "@cursor",
			expected:   &ParsedMention{Prompt: "fix the login bug", SkipReview: boolPtr(true)},
		},
		{
			name:       "no-plan flag",
			message:    "@cursor --no-plan fix the login bug",
			botMention: "@cursor",
			expected:   &ParsedMention{Prompt: "fix the login bug", SkipPlan: boolPtr(true)},
		},
		{
			name:       "direct flag",
			message:    "@cursor --direct fix the login bug",
			botMention: "@cursor",
			expected:   &ParsedMention{Prompt: "fix the login bug", Direct: true},
		},
		{
			name:       "review=off inline",
			message:    "@cursor review=off fix the bug",
			botMention: "@cursor",
			expected:   &ParsedMention{Prompt: "fix the bug", SkipReview: boolPtr(true)},
		},
		{
			name:       "review=on inline",
			message:    "@cursor review=on fix the bug",
			botMention: "@cursor",
			expected:   &ParsedMention{Prompt: "fix the bug", SkipReview: boolPtr(false)},
		},
		{
			name:       "plan=off inline",
			message:    "@cursor plan=off fix the bug",
			botMention: "@cursor",
			expected:   &ParsedMention{Prompt: "fix the bug", SkipPlan: boolPtr(true)},
		},
		{
			name:       "plan=on inline",
			message:    "@cursor plan=on fix the bug",
			botMention: "@cursor",
			expected:   &ParsedMention{Prompt: "fix the bug", SkipPlan: boolPtr(false)},
		},
		{
			name:       "review=off plan=off same as direct",
			message:    "@cursor review=off plan=off fix the bug",
			botMention: "@cursor",
			expected:   &ParsedMention{Prompt: "fix the bug", SkipReview: boolPtr(true), SkipPlan: boolPtr(true)},
		},
		{
			name:       "bracketed review and plan options",
			message:    "@cursor [review=off, plan=off] fix the bug",
			botMention: "@cursor",
			expected:   &ParsedMention{Prompt: "fix the bug", SkipReview: boolPtr(true), SkipPlan: boolPtr(true)},
		},
		{
			name:       "direct flag with other options",
			message:    "@cursor --direct repo=org/repo fix the bug",
			botMention: "@cursor",
			expected:   &ParsedMention{Prompt: "fix the bug", Repository: "org/repo", Direct: true},
		},
		{
			name:       "all flags combined",
			message:    "@cursor --no-review --no-plan repo=org/repo fix it",
			botMention: "@cursor",
			expected:   &ParsedMention{Prompt: "fix it", Repository: "org/repo", SkipReview: boolPtr(true), SkipPlan: boolPtr(true)},
		},
		{
			name:       "direct flag case insensitive",
			message:    "@cursor --DIRECT fix the bug",
			botMention: "@cursor",
			expected:   &ParsedMention{Prompt: "fix the bug", Direct: true},
		},
		{
			name:       "agent prefix with direct flag",
			message:    "@cursor agent --direct fix the thing",
			botMention: "@cursor",
			expected:   &ParsedMention{Prompt: "fix the thing", ForceNew: true, Direct: true},
		},

		// --- BUG-1: "with" in natural prose should NOT extract a model ---
		{
			name:       "with in natural prose not extracted as model",
			message:    "@cursor fix the alignment issue with tests",
			botMention: "@cursor",
			expected:   &ParsedMention{Prompt: "fix the alignment issue with tests"},
		},
		{
			name:       "with errors in natural prose not extracted as model",
			message:    "@cursor Add ratelimit.go with errors handled properly",
			botMention: "@cursor",
			expected:   &ParsedMention{Prompt: "Add ratelimit.go with errors handled properly"},
		},
		{
			name:       "with tests. trailing period not extracted as model",
			message:    "@cursor Include server/ratelimit_test.go with tests.",
			botMention: "@cursor",
			expected:   &ParsedMention{Prompt: "Include server/ratelimit_test.go with tests."},
		},
		{
			name:       "with model at start still works",
			message:    "@cursor with opus fix the login bug",
			botMention: "@cursor",
			expected:   &ParsedMention{Prompt: "fix the login bug", Model: "opus"},
		},
		{
			name:       "with model after comma still works",
			message:    "@cursor in org/repo, with claude-3.5-sonnet, fix it",
			botMention: "@cursor",
			expected:   &ParsedMention{Prompt: "fix it", Repository: "org/repo", Model: "claude-3.5-sonnet"},
		},
		{
			name:       "QA repro: ratelimiter prompt with tests at end",
			message:    "@cursor --direct repo=nickmisasi/mattermost-plugin-cursor Add a new file server/ratelimit.go that implements a simple in-memory per-user rate limiter for the plugin HTTP API. Use a map to track request counts per user ID, provide a RateLimitMiddleware(next http.Handler) http.Handler function, and return HTTP 429 when a user exceeds 100 requests per minute. Include server/ratelimit_test.go with tests.",
			botMention: "@cursor",
			expected: &ParsedMention{
				Prompt:     "Add a new file server/ratelimit.go that implements a simple in-memory per-user rate limiter for the plugin HTTP API. Use a map to track request counts per user ID, provide a RateLimitMiddleware(next http.Handler) http.Handler function, and return HTTP 429 when a user exceeds 100 requests per minute. Include server/ratelimit_test.go with tests.",
				Repository: "nickmisasi/mattermost-plugin-cursor",
				Direct:     true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Parse(tt.message, tt.botMention)
			if tt.expected == nil {
				assert.Nil(t, result)
			} else {
				assert.NotNil(t, result)
				if result == nil {
					return
				}
				assert.Equal(t, tt.expected.Prompt, result.Prompt)
				assert.Equal(t, tt.expected.Repository, result.Repository)
				assert.Equal(t, tt.expected.Branch, result.Branch)
				assert.Equal(t, tt.expected.Model, result.Model)
				assert.Equal(t, tt.expected.ForceNew, result.ForceNew)
				if tt.expected.AutoPR == nil {
					assert.Nil(t, result.AutoPR)
				} else {
					assert.NotNil(t, result.AutoPR)
					if result.AutoPR != nil {
						assert.Equal(t, *tt.expected.AutoPR, *result.AutoPR)
					}
				}
				// SkipReview
				if tt.expected.SkipReview == nil {
					assert.Nil(t, result.SkipReview)
				} else {
					assert.NotNil(t, result.SkipReview)
					if result.SkipReview != nil {
						assert.Equal(t, *tt.expected.SkipReview, *result.SkipReview)
					}
				}
				// SkipPlan
				if tt.expected.SkipPlan == nil {
					assert.Nil(t, result.SkipPlan)
				} else {
					assert.NotNil(t, result.SkipPlan)
					if result.SkipPlan != nil {
						assert.Equal(t, *tt.expected.SkipPlan, *result.SkipPlan)
					}
				}
				// Direct
				assert.Equal(t, tt.expected.Direct, result.Direct)
			}
		})
	}
}
