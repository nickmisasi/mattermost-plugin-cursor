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
			name:       "in repo short name",
			message:    "@cursor in backend-api, fix the auth issue",
			botMention: "@cursor",
			expected:   &ParsedMention{Prompt: "fix the auth issue", Repository: "backend-api"},
		},
		{
			name:       "in org/repo",
			message:    "@cursor in org/repo, fix the auth issue",
			botMention: "@cursor",
			expected:   &ParsedMention{Prompt: "fix the auth issue", Repository: "org/repo"},
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
			name:       "in repo with model",
			message:    "@cursor in backend-api, with opus, fix it",
			botMention: "@cursor",
			expected:   &ParsedMention{Prompt: "fix it", Repository: "backend-api", Model: "opus"},
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
			}
		})
	}
}
