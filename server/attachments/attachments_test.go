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
