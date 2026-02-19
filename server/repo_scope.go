package main

import "strings"

const (
	mattermostRepositoryScope = "mattermost/mattermost"

	mattermostBrowserQAPlannerInstructions = `Mattermost browser QA workflow (mattermost/mattermost only):
- If the described task is a bug, attempt to reproduce the issue before finalizing the plan.
- Use .cursor/skills/browser-qa.md for reproduction guidance.
- If reproduction is not possible, explicitly call that out in the plan with blockers.`

	mattermostBrowserQAImplementationInstructions = `Mattermost browser QA validation (mattermost/mattermost only):
- When implementing a bug fix, use .cursor/skills/browser-qa.md to validate the fix when possible.
- Summarize what you validated and the results in your final response.`
)

func isMattermostMattermostRepository(repository string) bool {
	return normalizeRepositoryForScopeCheck(repository) == mattermostRepositoryScope
}

func normalizeRepositoryForScopeCheck(repository string) string {
	normalized := strings.ToLower(strings.TrimSpace(repository))
	normalized = strings.TrimSuffix(normalized, "/")
	normalized = strings.TrimSuffix(normalized, ".git")
	normalized = strings.TrimSuffix(normalized, "/")
	normalized = strings.TrimPrefix(normalized, "https://github.com/")
	normalized = strings.TrimPrefix(normalized, "http://github.com/")
	normalized = strings.TrimPrefix(normalized, "github.com/")
	normalized = strings.TrimPrefix(normalized, "git@github.com:")
	return normalized
}
