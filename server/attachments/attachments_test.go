package attachments

import (
	"testing"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStatusColor(t *testing.T) {
	tests := []struct {
		name     string
		status   string
		expected string
	}{
		{"CREATING", "CREATING", ColorBlue},
		{"RUNNING", "RUNNING", ColorBlue},
		{"FINISHED", "FINISHED", ColorGreen},
		{"FAILED", "FAILED", ColorRed},
		{"STOPPED", "STOPPED", ColorGrey},
		{"lowercase running", "running", ColorBlue},
		{"mixed case Finished", "Finished", ColorGreen},
		{"unknown status", "UNKNOWN", ColorGrey},
		{"empty string", "", ColorGrey},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, StatusColor(tt.status))
		})
	}
}

func TestCursorAgentURL(t *testing.T) {
	assert.Equal(t, "https://cursor.com/agents/abc123", cursorAgentURL("abc123"))
	assert.Equal(t, "https://cursor.com/agents/", cursorAgentURL(""))
}

func TestAgentLinks(t *testing.T) {
	links := agentLinks("agent-1")
	assert.Contains(t, links, "[Open in Cursor](https://cursor.com/agents/agent-1)")
	assert.Contains(t, links, "[Open in Web](https://cursor.com/agents/agent-1)")
	assert.Contains(t, links, " | ")
}

func TestTextWithLinks(t *testing.T) {
	t.Run("empty text", func(t *testing.T) {
		result := textWithLinks("", "agent-1")
		assert.Equal(t, agentLinks("agent-1"), result)
	})

	t.Run("non-empty text", func(t *testing.T) {
		result := textWithLinks("summary here", "agent-1")
		assert.Contains(t, result, "summary here")
		assert.Contains(t, result, "[Open in Cursor]")
		assert.Contains(t, result, "\n\n")
	})
}

func TestMetadataFields(t *testing.T) {
	t.Run("all fields non-empty", func(t *testing.T) {
		fields := metadataFields("org/repo", "main", "claude-sonnet")
		require.Len(t, fields, 3)
		assert.Equal(t, "Repo", fields[0].Title)
		assert.Equal(t, "org/repo", fields[0].Value)
		assert.Equal(t, model.SlackCompatibleBool(true), fields[0].Short)
		assert.Equal(t, "Branch", fields[1].Title)
		assert.Equal(t, "main", fields[1].Value)
		assert.Equal(t, model.SlackCompatibleBool(true), fields[1].Short)
		assert.Equal(t, "Model", fields[2].Title)
		assert.Equal(t, "claude-sonnet", fields[2].Value)
		assert.Equal(t, model.SlackCompatibleBool(true), fields[2].Short)
	})

	t.Run("repo only", func(t *testing.T) {
		fields := metadataFields("org/repo", "", "")
		require.Len(t, fields, 1)
		assert.Equal(t, "Repo", fields[0].Title)
	})

	t.Run("all empty", func(t *testing.T) {
		fields := metadataFields("", "", "")
		assert.Empty(t, fields)
	})

	t.Run("branch and model only", func(t *testing.T) {
		fields := metadataFields("", "main", "gpt-4")
		require.Len(t, fields, 2)
		assert.Equal(t, "Branch", fields[0].Title)
		assert.Equal(t, "Model", fields[1].Title)
	})
}

func TestBuildLaunchAttachment(t *testing.T) {
	att := BuildLaunchAttachment("a1", "org/repo", "main", "claude-sonnet")

	assert.Equal(t, ColorBlue, att.Color)
	assert.Equal(t, "Launched an agent. I'll notify here when it's finished.", att.Title)
	require.Len(t, att.Fields, 3)
	assert.Equal(t, "Repo", att.Fields[0].Title)
	assert.Equal(t, "org/repo", att.Fields[0].Value)
	assert.Equal(t, "Branch", att.Fields[1].Title)
	assert.Equal(t, "main", att.Fields[1].Value)
	assert.Equal(t, "Model", att.Fields[2].Title)
	assert.Equal(t, "claude-sonnet", att.Fields[2].Value)
	assert.Empty(t, att.Actions)
	assert.Empty(t, att.Footer)
	assert.Contains(t, att.Text, "[Open in Cursor](https://cursor.com/agents/a1)")
	assert.Contains(t, att.Text, "[Open in Web](https://cursor.com/agents/a1)")
}

func TestBuildRunningAttachment(t *testing.T) {
	att := BuildRunningAttachment("a1", "org/repo", "main", "claude-sonnet")

	assert.Equal(t, ColorBlue, att.Color)
	assert.Equal(t, "Agent is now running...", att.Title)
	require.Len(t, att.Fields, 3) // Repo, Branch, Model preserved
	assert.Equal(t, "Repo", att.Fields[0].Title)
	assert.Equal(t, "Branch", att.Fields[1].Title)
	assert.Equal(t, "Model", att.Fields[2].Title)
	assert.Empty(t, att.Actions)
	assert.Empty(t, att.Footer)
	assert.Contains(t, att.Text, "[Open in Cursor](https://cursor.com/agents/a1)")
	assert.Contains(t, att.Text, "[Open in Web](https://cursor.com/agents/a1)")
}

func TestBuildFinishedAttachment(t *testing.T) {
	t.Run("with PR URL", func(t *testing.T) {
		prURL := "https://github.com/org/repo/pull/42"
		att := BuildFinishedAttachment("a1", "org/repo", "main", "claude-sonnet", "Fixed the bug", prURL)

		assert.Equal(t, ColorGreen, att.Color)
		assert.Equal(t, "Agent finished!", att.Title)
		assert.Contains(t, att.Text, "Fixed the bug")
		require.Len(t, att.Fields, 3) // Repo, Branch, Model
		assert.Equal(t, "Repo", att.Fields[0].Title)
		assert.Equal(t, "Branch", att.Fields[1].Title)
		assert.Equal(t, "Model", att.Fields[2].Title)

		assert.Empty(t, att.Actions)
		assert.Empty(t, att.Footer)
		assert.Contains(t, att.Text, "[View PR](https://github.com/org/repo/pull/42)")
		assert.Contains(t, att.Text, "[Open in Cursor](https://cursor.com/agents/a1)")
		assert.Contains(t, att.Text, "[Open in Web](https://cursor.com/agents/a1)")
	})

	t.Run("without PR URL", func(t *testing.T) {
		att := BuildFinishedAttachment("a1", "org/repo", "main", "", "Done", "")

		assert.Equal(t, ColorGreen, att.Color)
		assert.Equal(t, "Agent finished!", att.Title)
		assert.Contains(t, att.Text, "Done")
		require.Len(t, att.Fields, 2) // Repo and Branch, no Model
		assert.Empty(t, att.Actions)
		assert.Empty(t, att.Footer)
		assert.NotContains(t, att.Text, "View PR")
		assert.Contains(t, att.Text, "[Open in Cursor](https://cursor.com/agents/a1)")
		assert.Contains(t, att.Text, "[Open in Web](https://cursor.com/agents/a1)")
	})

	t.Run("empty summary", func(t *testing.T) {
		att := BuildFinishedAttachment("a1", "", "", "", "", "")

		assert.Contains(t, att.Text, "[Open in Cursor]")
		assert.Empty(t, att.Fields)
		assert.Empty(t, att.Actions)
		assert.Empty(t, att.Footer)
	})
}

func TestBuildFailedAttachment(t *testing.T) {
	att := BuildFailedAttachment("a1", "org/repo", "main", "claude-sonnet", "Out of memory")

	assert.Equal(t, ColorRed, att.Color)
	assert.Equal(t, "Agent failed.", att.Title)
	assert.Contains(t, att.Text, "Out of memory")
	require.Len(t, att.Fields, 3) // Repo, Branch, Model
	assert.Empty(t, att.Actions)
	assert.Empty(t, att.Footer)
	assert.Contains(t, att.Text, "[Open in Cursor](https://cursor.com/agents/a1)")
	assert.Contains(t, att.Text, "[Open in Web](https://cursor.com/agents/a1)")
}

func TestBuildStoppedAttachment(t *testing.T) {
	att := BuildStoppedAttachment("a1", "org/repo", "main", "claude-sonnet")

	assert.Equal(t, ColorGrey, att.Color)
	assert.Equal(t, "Agent was stopped.", att.Title)
	require.Len(t, att.Fields, 3) // Repo, Branch, Model
	assert.Empty(t, att.Actions)
	assert.Empty(t, att.Footer)
	assert.Contains(t, att.Text, "[Open in Cursor](https://cursor.com/agents/a1)")
	assert.Contains(t, att.Text, "[Open in Web](https://cursor.com/agents/a1)")
}

func TestBuildContextReviewAttachment(t *testing.T) {
	pluginURL := "https://mattermost.example.com/plugins/com.mattermost.plugin-cursor"
	att := BuildContextReviewAttachment(
		"Enriched context text here",
		"org/repo", "main", "claude-sonnet",
		"wf-123", pluginURL, "testuser",
	)

	assert.Equal(t, ColorYellow, att.Color)
	assert.Contains(t, att.Title, "@testuser")
	assert.Contains(t, att.Title, "I've analyzed")
	assert.Equal(t, "Enriched context text here", att.Text)
	require.Len(t, att.Fields, 3)
	assert.Equal(t, "Repo", att.Fields[0].Title)
	assert.Equal(t, "org/repo", att.Fields[0].Value)
	assert.Equal(t, "Branch", att.Fields[1].Title)
	assert.Equal(t, "main", att.Fields[1].Value)
	assert.Equal(t, "Model", att.Fields[2].Title)
	assert.Equal(t, "claude-sonnet", att.Fields[2].Value)
	assert.Contains(t, att.Footer, "Reply in this thread")

	// Buttons
	require.Len(t, att.Actions, 2)

	acceptBtn := att.Actions[0]
	assert.Equal(t, "Accept Context", acceptBtn.Name)
	assert.Equal(t, "good", acceptBtn.Style)
	require.NotNil(t, acceptBtn.Integration)
	assert.Equal(t, pluginURL+"/api/v1/actions/hitl-response", acceptBtn.Integration.URL)
	assert.Equal(t, "accept", acceptBtn.Integration.Context["action"])
	assert.Equal(t, "context_review", acceptBtn.Integration.Context["phase"])
	assert.Equal(t, "wf-123", acceptBtn.Integration.Context["workflow_id"])

	rejectBtn := att.Actions[1]
	assert.Equal(t, "Reject", rejectBtn.Name)
	assert.Equal(t, "danger", rejectBtn.Style)
	require.NotNil(t, rejectBtn.Integration)
	assert.Equal(t, pluginURL+"/api/v1/actions/hitl-response", rejectBtn.Integration.URL)
	assert.Equal(t, "reject", rejectBtn.Integration.Context["action"])
	assert.Equal(t, "context_review", rejectBtn.Integration.Context["phase"])
	assert.Equal(t, "wf-123", rejectBtn.Integration.Context["workflow_id"])
}

func TestBuildContextAcceptedAttachment(t *testing.T) {
	att := BuildContextAcceptedAttachment("org/repo", "main", "claude-sonnet", "testuser")

	assert.Equal(t, ColorGreen, att.Color)
	assert.Contains(t, att.Title, "@testuser")
	assert.Contains(t, att.Title, "approved")
	require.Len(t, att.Fields, 3)
	assert.Empty(t, att.Actions)
}

func TestBuildContextRejectedAttachment(t *testing.T) {
	att := BuildContextRejectedAttachment("testuser")

	assert.Equal(t, ColorGrey, att.Color)
	assert.Contains(t, att.Title, "@testuser")
	assert.Contains(t, att.Title, "rejected")
	assert.Empty(t, att.Actions)
	assert.Empty(t, att.Fields)
}

// --- Phase 3: Plan loop attachment tests ---

func TestBuildPlanningStatusAttachment(t *testing.T) {
	t.Run("first iteration", func(t *testing.T) {
		att := BuildPlanningStatusAttachment("org/repo", "main", "auto", 0)

		assert.Equal(t, ColorBlue, att.Color)
		assert.Contains(t, att.Title, "Planning agent")
		assert.Contains(t, att.Title, "analyzing")
		require.Len(t, att.Fields, 3)
		assert.Equal(t, "Repo", att.Fields[0].Title)
		assert.Equal(t, "org/repo", att.Fields[0].Value)
		assert.Equal(t, "Branch", att.Fields[1].Title)
		assert.Equal(t, "main", att.Fields[1].Value)
		assert.Equal(t, "Model", att.Fields[2].Title)
		assert.Equal(t, "auto", att.Fields[2].Value)
		assert.Empty(t, att.Actions)
	})

	t.Run("second iteration shows pass number", func(t *testing.T) {
		att := BuildPlanningStatusAttachment("org/repo", "main", "auto", 2)

		assert.Contains(t, att.Title, "pass 2")
		assert.Contains(t, att.Title, "revising")
	})
}

func TestBuildPlanReviewAttachment(t *testing.T) {
	pluginURL := "https://mattermost.example.com/plugins/com.mattermost.plugin-cursor"

	t.Run("first iteration", func(t *testing.T) {
		att := BuildPlanReviewAttachment(
			"### Summary\nThe plan details.",
			"org/repo", "main", "auto",
			"wf-123", pluginURL, "testuser", 1,
		)

		assert.Equal(t, ColorYellow, att.Color)
		assert.Contains(t, att.Title, "@testuser")
		assert.Contains(t, att.Title, "implementation plan")
		assert.NotContains(t, att.Title, "revised")
		assert.Equal(t, "### Summary\nThe plan details.", att.Text)
		require.Len(t, att.Fields, 3)
		assert.Contains(t, att.Footer, "Reply in this thread")

		// Buttons
		require.Len(t, att.Actions, 2)

		acceptBtn := att.Actions[0]
		assert.Equal(t, "Accept Plan", acceptBtn.Name)
		assert.Equal(t, "good", acceptBtn.Style)
		require.NotNil(t, acceptBtn.Integration)
		assert.Equal(t, pluginURL+"/api/v1/actions/hitl-response", acceptBtn.Integration.URL)
		assert.Equal(t, "accept", acceptBtn.Integration.Context["action"])
		assert.Equal(t, "plan_review", acceptBtn.Integration.Context["phase"])
		assert.Equal(t, "wf-123", acceptBtn.Integration.Context["workflow_id"])

		rejectBtn := att.Actions[1]
		assert.Equal(t, "Reject", rejectBtn.Name)
		assert.Equal(t, "danger", rejectBtn.Style)
		require.NotNil(t, rejectBtn.Integration)
		assert.Equal(t, pluginURL+"/api/v1/actions/hitl-response", rejectBtn.Integration.URL)
		assert.Equal(t, "reject", rejectBtn.Integration.Context["action"])
		assert.Equal(t, "plan_review", rejectBtn.Integration.Context["phase"])
		assert.Equal(t, "wf-123", rejectBtn.Integration.Context["workflow_id"])
	})

	t.Run("second iteration shows revised title", func(t *testing.T) {
		att := BuildPlanReviewAttachment(
			"Updated plan.",
			"org/repo", "main", "auto",
			"wf-123", pluginURL, "testuser", 2,
		)

		assert.Contains(t, att.Title, "revised")
		assert.Contains(t, att.Title, "v2")
	})

	t.Run("third iteration shows v3", func(t *testing.T) {
		att := BuildPlanReviewAttachment(
			"Third plan.",
			"org/repo", "main", "auto",
			"wf-123", pluginURL, "testuser", 3,
		)

		assert.Contains(t, att.Title, "v3")
	})

	t.Run("truncates long plans at 14000 chars", func(t *testing.T) {
		longPlan := ""
		for range 1500 {
			longPlan += "0123456789" // 15000 chars total
		}

		att := BuildPlanReviewAttachment(
			longPlan,
			"org/repo", "main", "auto",
			"wf-123", pluginURL, "testuser", 1,
		)

		assert.True(t, len(att.Text) < len(longPlan))
		assert.Contains(t, att.Text, "plan truncated")
	})
}

func TestBuildPlanAcceptedAttachment(t *testing.T) {
	t.Run("first iteration", func(t *testing.T) {
		att := BuildPlanAcceptedAttachment("testuser", 1)

		assert.Equal(t, ColorGreen, att.Color)
		assert.Contains(t, att.Title, "@testuser")
		assert.Contains(t, att.Title, "approved")
		assert.Contains(t, att.Title, "launching implementation agent")
		assert.NotContains(t, att.Title, "(v")
		assert.Empty(t, att.Actions)
	})

	t.Run("second iteration shows version", func(t *testing.T) {
		att := BuildPlanAcceptedAttachment("testuser", 2)

		assert.Equal(t, ColorGreen, att.Color)
		assert.Contains(t, att.Title, "(v2)")
		assert.Contains(t, att.Title, "@testuser")
		assert.Contains(t, att.Title, "approved")
	})

	t.Run("third iteration shows v3", func(t *testing.T) {
		att := BuildPlanAcceptedAttachment("testuser", 3)
		assert.Contains(t, att.Title, "(v3)")
	})
}

func TestBuildPlanRejectedAttachment(t *testing.T) {
	att := BuildPlanRejectedAttachment("testuser")

	assert.Equal(t, ColorGrey, att.Color)
	assert.Contains(t, att.Title, "@testuser")
	assert.Contains(t, att.Title, "rejected")
	assert.Contains(t, att.Title, "cancelled")
	assert.Empty(t, att.Actions)
}

func TestBuildImplementerLaunchAttachment(t *testing.T) {
	att := BuildImplementerLaunchAttachment("a1", "org/repo", "main", "auto")

	assert.Equal(t, ColorBlue, att.Color)
	assert.Contains(t, att.Title, "Implementation agent launched")
	require.Len(t, att.Fields, 3)
	assert.Contains(t, att.Text, "[Open in Cursor](https://cursor.com/agents/a1)")
	assert.Contains(t, att.Text, "[Open in Web](https://cursor.com/agents/a1)")
	assert.Empty(t, att.Actions)
}

// --- Phase 4: Review loop UX attachment tests ---

func TestReviewStatusLine(t *testing.T) {
	tests := []struct {
		name        string
		phase       string
		iteration   int
		contains    []string
		notContains []string
	}{
		{
			name:      "requesting_review",
			phase:     "requesting_review",
			iteration: 1,
			contains:  []string{"Requesting reviewers"},
		},
		{
			name:        "awaiting_review iteration 1 hides count",
			phase:       "awaiting_review",
			iteration:   1,
			contains:    []string{"Waiting for CodeRabbit"},
			notContains: []string{"iteration"},
		},
		{
			name:      "awaiting_review iteration 3 shows count",
			phase:     "awaiting_review",
			iteration: 3,
			contains:  []string{"Waiting for CodeRabbit", "iteration 3"},
		},
		{
			name:        "cursor_fixing iteration 1 hides count",
			phase:       "cursor_fixing",
			iteration:   1,
			contains:    []string{"Cursor fixing feedback"},
			notContains: []string{"iteration"},
		},
		{
			name:      "cursor_fixing iteration 2 shows count",
			phase:     "cursor_fixing",
			iteration: 2,
			contains:  []string{"Cursor fixing feedback", "iteration 2"},
		},
		{
			name:      "approved always shows iteration count",
			phase:     "approved",
			iteration: 1,
			contains:  []string{"Approved by CodeRabbit", "1 iteration"},
		},
		{
			name:      "approved multi-iteration",
			phase:     "approved",
			iteration: 4,
			contains:  []string{"Approved by CodeRabbit", "4 iteration"},
		},
		{
			name:      "human_review",
			phase:     "human_review",
			iteration: 1,
			contains:  []string{"Waiting for human reviewer"},
		},
		{
			name:      "complete",
			phase:     "complete",
			iteration: 1,
			contains:  []string{"Complete"},
		},
		{
			name:      "max_iterations",
			phase:     "max_iterations",
			iteration: 5,
			contains:  []string{"Max iterations", "manual review"},
		},
		{
			name:      "failed",
			phase:     "failed",
			iteration: 1,
			contains:  []string{"Error", "check logs"},
		},
		{
			name:      "unknown phase",
			phase:     "something_new",
			iteration: 1,
			contains:  []string{"something_new"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := reviewStatusLine(tt.phase, tt.iteration)
			for _, s := range tt.contains {
				assert.Contains(t, result, s)
			}
			for _, s := range tt.notContains {
				assert.NotContains(t, result, s)
			}
		})
	}
}

func TestBuildFinishedWithReviewStatusAttachment(t *testing.T) {
	t.Run("active loop phase shows blue color and status line", func(t *testing.T) {
		att := BuildFinishedWithReviewStatusAttachment(
			"a1", "org/repo", "main", "claude-sonnet", "Fixed the bug",
			"https://github.com/org/repo/pull/42",
			"awaiting_review", 2,
		)

		assert.Equal(t, ColorBlue, att.Color)
		assert.Equal(t, "Agent finished!", att.Title)
		assert.Contains(t, att.Text, "Fixed the bug")
		assert.Contains(t, att.Text, "[View PR](https://github.com/org/repo/pull/42)")
		assert.Contains(t, att.Text, "[Open in Cursor]")
		assert.Contains(t, att.Text, "---")
		assert.Contains(t, att.Text, "Waiting for CodeRabbit")
		assert.Contains(t, att.Text, "iteration 2")
		require.Len(t, att.Fields, 3)
	})

	t.Run("approved phase shows green color", func(t *testing.T) {
		att := BuildFinishedWithReviewStatusAttachment(
			"a1", "org/repo", "main", "", "",
			"https://github.com/org/repo/pull/42",
			"approved", 3,
		)

		assert.Equal(t, ColorGreen, att.Color)
		assert.Contains(t, att.Text, "Approved by CodeRabbit")
		assert.Contains(t, att.Text, "3 iteration")
	})

	t.Run("max_iterations shows grey color", func(t *testing.T) {
		att := BuildFinishedWithReviewStatusAttachment(
			"a1", "org/repo", "main", "", "",
			"",
			"max_iterations", 5,
		)

		assert.Equal(t, ColorGrey, att.Color)
		assert.Contains(t, att.Text, "Max iterations")
	})

	t.Run("failed shows red color", func(t *testing.T) {
		att := BuildFinishedWithReviewStatusAttachment(
			"a1", "", "", "", "",
			"",
			"failed", 1,
		)

		assert.Equal(t, ColorRed, att.Color)
		assert.Contains(t, att.Text, "Error")
	})

	t.Run("requesting_review shows blue", func(t *testing.T) {
		att := BuildFinishedWithReviewStatusAttachment(
			"a1", "", "", "", "",
			"",
			"requesting_review", 1,
		)

		assert.Equal(t, ColorBlue, att.Color)
		assert.Contains(t, att.Text, "Requesting reviewers")
	})

	t.Run("cursor_fixing shows blue", func(t *testing.T) {
		att := BuildFinishedWithReviewStatusAttachment(
			"a1", "", "", "", "",
			"",
			"cursor_fixing", 1,
		)

		assert.Equal(t, ColorBlue, att.Color)
		assert.Contains(t, att.Text, "Cursor fixing feedback")
	})

	t.Run("human_review keeps green", func(t *testing.T) {
		att := BuildFinishedWithReviewStatusAttachment(
			"a1", "", "", "", "",
			"https://github.com/org/repo/pull/1",
			"human_review", 2,
		)

		assert.Equal(t, ColorGreen, att.Color)
		assert.Contains(t, att.Text, "Waiting for human reviewer")
	})

	t.Run("no summary, no PR URL", func(t *testing.T) {
		att := BuildFinishedWithReviewStatusAttachment(
			"a1", "", "", "", "",
			"",
			"awaiting_review", 1,
		)

		// Should still have links and status line
		assert.Contains(t, att.Text, "[Open in Cursor]")
		assert.Contains(t, att.Text, "Waiting for CodeRabbit")
		assert.Empty(t, att.Fields)
	})

	t.Run("with summary and PR URL", func(t *testing.T) {
		att := BuildFinishedWithReviewStatusAttachment(
			"a1", "org/repo", "main", "auto",
			"Implemented the feature",
			"https://github.com/org/repo/pull/99",
			"cursor_fixing", 3,
		)

		// Verify ordering: summary, links, separator, status
		assert.Contains(t, att.Text, "Implemented the feature")
		assert.Contains(t, att.Text, "[View PR]")
		assert.Contains(t, att.Text, "---")
		assert.Contains(t, att.Text, "Cursor fixing feedback")
		assert.Contains(t, att.Text, "iteration 3")
	})
}

func TestBuildReviewApprovedAttachment(t *testing.T) {
	t.Run("single iteration", func(t *testing.T) {
		att := BuildReviewApprovedAttachment("https://github.com/org/repo/pull/42", 1)

		assert.Equal(t, ColorGreen, att.Color)
		assert.Equal(t, "CodeRabbit approved the PR!", att.Title)
		assert.NotContains(t, att.Title, "iteration")
		assert.Contains(t, att.Text, "[View PR](https://github.com/org/repo/pull/42)")
		assert.Empty(t, att.Fields)
		assert.Empty(t, att.Actions)
	})

	t.Run("multiple iterations", func(t *testing.T) {
		att := BuildReviewApprovedAttachment("https://github.com/org/repo/pull/42", 3)

		assert.Equal(t, ColorGreen, att.Color)
		assert.Contains(t, att.Title, "3 iterations")
		assert.Contains(t, att.Text, "[View PR]")
	})

	t.Run("no PR URL", func(t *testing.T) {
		att := BuildReviewApprovedAttachment("", 2)

		assert.Equal(t, ColorGreen, att.Color)
		assert.Contains(t, att.Title, "approved")
		assert.Empty(t, att.Text)
	})
}

func TestBuildMaxIterationsAttachment(t *testing.T) {
	t.Run("with PR URL", func(t *testing.T) {
		att := BuildMaxIterationsAttachment("https://github.com/org/repo/pull/42", 5)

		assert.Equal(t, ColorGrey, att.Color)
		assert.Contains(t, att.Title, "5 iterations")
		assert.Contains(t, att.Text, "[View PR](https://github.com/org/repo/pull/42)")
		assert.Contains(t, att.Text, "manual review")
		assert.Empty(t, att.Fields)
		assert.Empty(t, att.Actions)
	})

	t.Run("without PR URL", func(t *testing.T) {
		att := BuildMaxIterationsAttachment("", 3)

		assert.Equal(t, ColorGrey, att.Color)
		assert.Contains(t, att.Title, "3 iterations")
		assert.Contains(t, att.Text, "Manual review")
		assert.NotContains(t, att.Text, "[View PR]")
	})
}

func TestBuildReviewFailedAttachment(t *testing.T) {
	t.Run("with detail", func(t *testing.T) {
		att := BuildReviewFailedAttachment("GitHub API rate limited")

		assert.Equal(t, ColorRed, att.Color)
		assert.Equal(t, "AI review loop failed.", att.Title)
		assert.Equal(t, "GitHub API rate limited", att.Text)
		assert.Empty(t, att.Fields)
		assert.Empty(t, att.Actions)
	})

	t.Run("empty detail falls back to generic message", func(t *testing.T) {
		att := BuildReviewFailedAttachment("")

		assert.Equal(t, ColorRed, att.Color)
		assert.Contains(t, att.Text, "Check plugin logs")
	})
}
