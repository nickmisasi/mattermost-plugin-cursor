package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mattermost/mattermost-plugin-cursor/server/store/kvstore"
)

const (
	codeRabbitReviewerLogin                 = "coderabbitai[bot]"
	codeRabbitPromptMarkerAIAgents          = "Prompt for AI Agents"
	codeRabbitPromptMarkerAllReviewComments = "Prompt for all review comments with AI agents"
	codeRabbitVerifyPreamblePrefix          = "verify each finding against the current code"
	codeRabbitVerifyGuidancePrefix          = "do not assume old snippets are still present"

	reviewerTypeAIBot = "ai_bot"
	reviewerTypeHuman = "human"

	findingStatusOpen       = "open"
	findingStatusResolved   = "resolved"
	findingStatusDismissed  = "dismissed"
	findingStatusSuperseded = "superseded"

	maxReviewFindingsRetained = 200
	maxRawFeedbackTextLen     = 2000
	maxActionableTextLen      = 1000
)

var (
	collapseBlanksRE     = regexp.MustCompile(`\n{3,}`)
	collapseSpacesRE     = regexp.MustCompile(`[ \t]+`)
	cursorRelayCommentRE = regexp.MustCompile(`(?im)^@cursor\s+please address the following review feedback:\s*`)

	nonActionableWholeRE = regexp.MustCompile(`(?is)^(all good!?|looks good!?|lgtm!?|no actionable (comments|issues) (found|posted)\.?|no changes requested\.?)$`)
)

type reviewFeedbackCandidate struct {
	SourceType    string
	SourceID      int64
	SourceNodeID  string
	SourceURL     string
	ReviewerLogin string
	ReviewerType  string
	Path          string
	Line          int
	CommitSHA     string
	CreatedAt     int64

	RawText        string
	NormalizedText string
	ActionableText string
}

type reviewerExtractionRoute string

const (
	reviewerExtractionRouteCodeRabbit    reviewerExtractionRoute = "coderabbit"
	reviewerExtractionRouteNonCodeRabbit reviewerExtractionRoute = "non_coderabbit"

	reviewerExtractionDropReasonNormalizedEmpty              = "normalized_text_empty"
	reviewerExtractionDropReasonCodeRabbitMarkersMissing     = "coderabbit_markers_missing"
	reviewerExtractionDropReasonNonCodeRabbitNonInlineSource = "non_coderabbit_non_inline_source"
	reviewerExtractionDropReasonActionableEmpty              = "actionable_text_empty"
)

type reviewFeedbackClassification struct {
	New          []kvstore.ReviewFinding
	Repeated     []kvstore.ReviewFinding
	Resolved     []kvstore.ReviewFinding
	Superseded   []kvstore.ReviewFinding
	Dispatchable []kvstore.ReviewFinding
}

type reviewFeedbackSourceSummary struct {
	Total         int
	ReviewComment int
	ReviewBody    int
	IssueComment  int
	AIBot         int
	Human         int
}

type reviewFeedbackClassificationSummary struct {
	New          int
	Repeated     int
	Resolved     int
	Superseded   int
	Dismissed    int
	Dispatchable int
}

type reviewFeedbackTelemetry struct {
	Source reviewFeedbackSourceSummary
	Counts reviewFeedbackClassificationSummary
}

func summarizeReviewFeedbackTelemetry(candidates []reviewFeedbackCandidate, classification reviewFeedbackClassification) reviewFeedbackTelemetry {
	sourceSummary := reviewFeedbackSourceSummary{
		Total: len(candidates),
	}

	for _, candidate := range candidates {
		switch candidate.SourceType {
		case "review_comment":
			sourceSummary.ReviewComment++
		case "review_body":
			sourceSummary.ReviewBody++
		case "issue_comment":
			sourceSummary.IssueComment++
		}

		switch candidate.ReviewerType {
		case reviewerTypeAIBot:
			sourceSummary.AIBot++
		case reviewerTypeHuman:
			sourceSummary.Human++
		}
	}

	countSummary := reviewFeedbackClassificationSummary{
		New:          len(classification.New),
		Repeated:     len(classification.Repeated),
		Resolved:     len(classification.Resolved),
		Superseded:   len(classification.Superseded),
		Dispatchable: len(classification.Dispatchable),
	}
	countSummary.Dismissed = countSummary.Resolved + countSummary.Superseded

	return reviewFeedbackTelemetry{
		Source: sourceSummary,
		Counts: countSummary,
	}
}

func formatReviewFeedbackCountSummary(newCount, repeatedCount, dismissedCount int) string {
	return fmt.Sprintf("%d new, %d repeated, %d dismissed", newCount, repeatedCount, dismissedCount)
}

func (p *Plugin) collectFeedbackCandidates(loop *kvstore.ReviewLoop) ([]reviewFeedbackCandidate, error) {
	ghClient := p.getGitHubClient()
	if ghClient == nil {
		return nil, fmt.Errorf("GitHub client is not configured")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var candidates []reviewFeedbackCandidate

	reviewComments, err := ghClient.ListReviewComments(ctx, loop.Owner, loop.Repo, loop.PRNumber)
	if err != nil {
		return nil, fmt.Errorf("failed to list review comments: %w", err)
	}

	for _, comment := range reviewComments {
		login := ""
		if comment.User != nil {
			login = comment.User.GetLogin()
		}
		reviewerType := p.reviewerTypeForLogin(login)
		if !shouldCollectForPhase(loop.Phase, reviewerType) {
			continue
		}

		// Keep existing behavior: only include inline comments from the latest
		// commit if LastCommitSHA is known.
		if loop.LastCommitSHA != "" && comment.GetCommitID() != "" && comment.GetCommitID() != loop.LastCommitSHA {
			continue
		}

		candidate := reviewFeedbackCandidate{
			SourceType:    "review_comment",
			SourceID:      comment.GetID(),
			SourceNodeID:  comment.GetNodeID(),
			SourceURL:     comment.GetHTMLURL(),
			ReviewerLogin: login,
			ReviewerType:  reviewerType,
			Path:          comment.GetPath(),
			Line:          comment.GetLine(),
			CommitSHA:     comment.GetCommitID(),
			RawText:       comment.GetBody(),
		}
		if comment.CreatedAt != nil {
			candidate.CreatedAt = comment.CreatedAt.Time.UnixMilli()
		}
		candidates = append(candidates, candidate)
	}

	reviews, err := ghClient.ListReviews(ctx, loop.Owner, loop.Repo, loop.PRNumber)
	if err != nil {
		p.API.LogWarn("Failed to list reviews for feedback collection", "error", err.Error())
	} else {
		for _, review := range reviews {
			login := ""
			if review.User != nil {
				login = review.User.GetLogin()
			}
			reviewerType := p.reviewerTypeForLogin(login)
			if !shouldCollectForPhase(loop.Phase, reviewerType) {
				continue
			}

			candidate := reviewFeedbackCandidate{
				SourceType:    "review_body",
				SourceID:      review.GetID(),
				SourceNodeID:  review.GetNodeID(),
				SourceURL:     review.GetHTMLURL(),
				ReviewerLogin: login,
				ReviewerType:  reviewerType,
				CommitSHA:     review.GetCommitID(),
				RawText:       review.GetBody(),
			}
			if review.SubmittedAt != nil {
				candidate.CreatedAt = review.SubmittedAt.Time.UnixMilli()
			}
			candidates = append(candidates, candidate)
		}
	}

	issueComments, err := ghClient.ListIssueComments(ctx, loop.Owner, loop.Repo, loop.PRNumber)
	if err != nil {
		p.API.LogWarn("Failed to list issue comments for feedback collection", "error", err.Error())
	} else {
		for _, issueComment := range issueComments {
			login := ""
			if issueComment.User != nil {
				login = issueComment.User.GetLogin()
			}
			reviewerType := p.reviewerTypeForLogin(login)
			if !shouldCollectForPhase(loop.Phase, reviewerType) {
				continue
			}
			if isAutomatedCursorRelayIssueComment(issueComment.GetBody()) {
				continue
			}

			candidate := reviewFeedbackCandidate{
				SourceType:    "issue_comment",
				SourceID:      issueComment.GetID(),
				SourceNodeID:  issueComment.GetNodeID(),
				SourceURL:     issueComment.GetHTMLURL(),
				ReviewerLogin: login,
				ReviewerType:  reviewerType,
				RawText:       issueComment.GetBody(),
			}
			if issueComment.CreatedAt != nil {
				candidate.CreatedAt = issueComment.CreatedAt.Time.UnixMilli()
			}
			candidates = append(candidates, candidate)
		}
	}

	return candidates, nil
}

func isAutomatedCursorRelayIssueComment(body string) bool {
	return cursorRelayCommentRE.MatchString(strings.TrimSpace(body))
}

func normalizeFeedbackCandidate(candidate reviewFeedbackCandidate) reviewFeedbackCandidate {
	candidate.Path = strings.TrimSpace(candidate.Path)
	candidate.RawText = strings.TrimSpace(candidate.RawText)
	candidate.NormalizedText = sanitizeReviewBodyForMattermost(candidate.RawText)
	candidate.NormalizedText = strings.ReplaceAll(candidate.NormalizedText, "\r\n", "\n")
	candidate.NormalizedText = strings.TrimSpace(candidate.NormalizedText)

	return candidate
}

func resolveReviewerExtractionRoute(candidate reviewFeedbackCandidate) reviewerExtractionRoute {
	if strings.EqualFold(strings.TrimSpace(candidate.ReviewerLogin), codeRabbitReviewerLogin) {
		return reviewerExtractionRouteCodeRabbit
	}
	return reviewerExtractionRouteNonCodeRabbit
}

func extractCandidateActionableText(candidate reviewFeedbackCandidate) (actionableText string, route reviewerExtractionRoute, dropReason string) {
	route = resolveReviewerExtractionRoute(candidate)
	if strings.TrimSpace(candidate.NormalizedText) == "" {
		return "", route, reviewerExtractionDropReasonNormalizedEmpty
	}

	switch route {
	case reviewerExtractionRouteCodeRabbit:
		markers := codeRabbitPromptMarkersForCandidate(candidate)
		if !containsCodeRabbitPromptMarker(candidate.NormalizedText, markers) {
			return "", route, reviewerExtractionDropReasonCodeRabbitMarkersMissing
		}
		actionableText = extractCodeRabbitActionableText(candidate)
	default:
		if !isNonCodeRabbitInlineSource(candidate) {
			return "", route, reviewerExtractionDropReasonNonCodeRabbitNonInlineSource
		}
		actionableText = extractNonCodeRabbitInlineActionableText(candidate)
	}

	if strings.TrimSpace(actionableText) == "" {
		return "", route, reviewerExtractionDropReasonActionableEmpty
	}

	return actionableText, route, ""
}

func codeRabbitPromptMarkersForCandidate(candidate reviewFeedbackCandidate) []string {
	switch strings.ToLower(strings.TrimSpace(candidate.SourceType)) {
	case "review_comment":
		return []string{codeRabbitPromptMarkerAIAgents, codeRabbitPromptMarkerAllReviewComments}
	case "review_body":
		return []string{codeRabbitPromptMarkerAllReviewComments, codeRabbitPromptMarkerAIAgents}
	default:
		return []string{codeRabbitPromptMarkerAIAgents, codeRabbitPromptMarkerAllReviewComments}
	}
}

func containsCodeRabbitPromptMarker(raw string, markers []string) bool {
	if strings.TrimSpace(raw) == "" || len(markers) == 0 {
		return false
	}

	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	for _, marker := range markers {
		for _, line := range lines {
			if isCodeRabbitPromptMarkerLine(line, marker) {
				return true
			}
		}
	}

	return false
}

func extractCodeRabbitActionableText(candidate reviewFeedbackCandidate) string {
	section := extractCodeRabbitPromptSection(candidate.NormalizedText, codeRabbitPromptMarkersForCandidate(candidate))
	if strings.TrimSpace(section) == "" {
		return ""
	}

	section = stripCodeRabbitBoilerplatePrefix(section)
	section = collapseBlanksRE.ReplaceAllString(section, "\n\n")
	section = strings.TrimSpace(section)
	if section == "" {
		return ""
	}

	normalizedWhole := canonicalizeFeedbackText(section)
	if nonActionableWholeRE.MatchString(normalizedWhole) {
		return ""
	}

	return truncateText(section, maxActionableTextLen)
}

func isNonCodeRabbitInlineSource(candidate reviewFeedbackCandidate) bool {
	return strings.ToLower(strings.TrimSpace(candidate.SourceType)) == "review_comment"
}

func extractNonCodeRabbitInlineActionableText(candidate reviewFeedbackCandidate) string {
	text := strings.TrimSpace(candidate.NormalizedText)
	if text == "" {
		return ""
	}

	text = collapseBlanksRE.ReplaceAllString(text, "\n\n")
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}

	normalizedWhole := canonicalizeFeedbackText(text)
	if nonActionableWholeRE.MatchString(normalizedWhole) {
		return ""
	}

	return truncateText(text, maxActionableTextLen)
}

func extractCodeRabbitPromptSection(raw string, markers []string) string {
	if strings.TrimSpace(raw) == "" || len(markers) == 0 {
		return ""
	}

	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")

	markerIndex := -1
	for _, marker := range markers {
		for idx, line := range lines {
			if isCodeRabbitPromptMarkerLine(line, marker) {
				markerIndex = idx
				break
			}
		}
		if markerIndex >= 0 {
			break
		}
	}
	if markerIndex < 0 {
		return ""
	}

	start := markerIndex + 1
	firstNonEmpty := -1
	for idx := start; idx < len(lines); idx++ {
		if strings.TrimSpace(lines[idx]) != "" {
			firstNonEmpty = idx
			break
		}
	}
	if firstNonEmpty < 0 {
		return ""
	}

	if isCodeRabbitFenceStartLine(lines[firstNonEmpty]) {
		fenceContent := make([]string, 0, len(lines)-firstNonEmpty)
		for idx := firstNonEmpty + 1; idx < len(lines); idx++ {
			if isCodeRabbitFenceEndLine(lines[idx]) {
				break
			}
			fenceContent = append(fenceContent, lines[idx])
		}
		section := strings.Join(fenceContent, "\n")
		section = collapseBlanksRE.ReplaceAllString(section, "\n\n")
		return strings.TrimSpace(section)
	}

	sectionLines := make([]string, 0, len(lines)-start)
	started := false
	for idx := start; idx < len(lines); idx++ {
		line := lines[idx]
		trimmed := strings.TrimSpace(line)
		if !started && trimmed == "" {
			continue
		}
		if isCodeRabbitPromptBoundaryLine(line) {
			normalizedBoundary := normalizeCodeRabbitMarkerLine(line)
			isVerifyPreamble := strings.HasPrefix(normalizedBoundary, codeRabbitVerifyPreamblePrefix)
			// Keep a leading verify preamble so prefix-only cleanup can strip it,
			// but treat later verify headings as section boundaries.
			if started || !isVerifyPreamble {
				break
			}
		}
		started = true
		sectionLines = append(sectionLines, line)
	}

	section := strings.Join(sectionLines, "\n")
	section = collapseBlanksRE.ReplaceAllString(section, "\n\n")
	return strings.TrimSpace(section)
}

func isCodeRabbitFenceStartLine(line string) bool {
	return strings.HasPrefix(strings.TrimSpace(line), "```")
}

func isCodeRabbitFenceEndLine(line string) bool {
	return strings.HasPrefix(strings.TrimSpace(line), "```")
}

func isCodeRabbitPromptBoundaryLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return false
	}
	if strings.HasPrefix(trimmed, "#") {
		return true
	}
	if isCodeRabbitPromptMarkerLine(line, codeRabbitPromptMarkerAIAgents) ||
		isCodeRabbitPromptMarkerLine(line, codeRabbitPromptMarkerAllReviewComments) {
		return true
	}

	normalized := normalizeCodeRabbitMarkerLine(line)
	if strings.HasPrefix(normalized, codeRabbitVerifyPreamblePrefix) {
		return true
	}

	switch normalized {
	case "walkthrough", "summary", "changes", "overview", "analysis chain", "script executed":
		return true
	default:
		return false
	}
}

func isCodeRabbitPromptMarkerLine(line string, marker string) bool {
	normalizedLine := normalizeCodeRabbitMarkerLine(line)
	normalizedMarker := collapseSpacesRE.ReplaceAllString(strings.TrimSpace(strings.ToLower(marker)), " ")
	return normalizedLine == normalizedMarker
}

func normalizeCodeRabbitMarkerLine(line string) string {
	normalized := strings.TrimSpace(line)

	for {
		changed := false
		normalized = strings.TrimLeft(normalized, " \t")
		switch {
		case strings.HasPrefix(normalized, ">"):
			normalized = strings.TrimLeft(strings.TrimPrefix(normalized, ">"), " \t")
			changed = true
		case strings.HasPrefix(normalized, "#"):
			normalized = strings.TrimLeft(strings.TrimPrefix(normalized, "#"), " \t")
			changed = true
		case strings.HasPrefix(normalized, "-"):
			normalized = strings.TrimLeft(strings.TrimPrefix(normalized, "-"), " \t")
			changed = true
		}
		if !changed {
			break
		}
	}

	for {
		previous := normalized
		normalized = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(normalized), ":"))

		switch {
		case len(normalized) >= 4 && strings.HasPrefix(normalized, "**") && strings.HasSuffix(normalized, "**"):
			normalized = strings.TrimSpace(normalized[2 : len(normalized)-2])
		case len(normalized) >= 2 && strings.HasPrefix(normalized, "*") && strings.HasSuffix(normalized, "*"):
			normalized = strings.TrimSpace(normalized[1 : len(normalized)-1])
		case len(normalized) >= 2 && strings.HasPrefix(normalized, "`") && strings.HasSuffix(normalized, "`"):
			normalized = strings.TrimSpace(normalized[1 : len(normalized)-1])
		}

		if normalized == previous {
			break
		}
	}

	normalized = strings.TrimSpace(strings.ToLower(normalized))
	return collapseSpacesRE.ReplaceAllString(normalized, " ")
}

func stripCodeRabbitBoilerplatePrefix(text string) string {
	if strings.TrimSpace(text) == "" {
		return ""
	}

	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	firstContent := -1
	for idx, line := range lines {
		if strings.TrimSpace(line) != "" {
			firstContent = idx
			break
		}
	}
	if firstContent < 0 {
		return ""
	}

	firstLine := normalizeCodeRabbitMarkerLine(lines[firstContent])
	if !strings.HasPrefix(firstLine, codeRabbitVerifyPreamblePrefix) {
		return strings.TrimSpace(text)
	}

	start := firstContent + 1
	for start < len(lines) {
		trimmed := strings.TrimSpace(lines[start])
		if trimmed == "" {
			start++
			break
		}
		if !isCodeRabbitVerifyGuidanceLine(trimmed) {
			break
		}
		start++
	}

	trimmedText := strings.Join(lines[start:], "\n")
	trimmedText = collapseBlanksRE.ReplaceAllString(trimmedText, "\n\n")
	return strings.TrimSpace(trimmedText)
}

func isCodeRabbitVerifyGuidanceLine(line string) bool {
	normalized := normalizeCodeRabbitMarkerLine(strings.TrimLeft(strings.TrimSpace(line), "-*"))
	return strings.HasPrefix(normalized, codeRabbitVerifyGuidancePrefix)
}

func classifyFeedback(loop *kvstore.ReviewLoop, candidates []reviewFeedbackCandidate, now int64) reviewFeedbackClassification {
	classification := reviewFeedbackClassification{}

	findings := make([]kvstore.ReviewFinding, len(loop.Findings))
	copy(findings, loop.Findings)

	openByKey := map[string]int{}
	openByLocation := map[string][]int{}
	seenInBatch := map[string]bool{}
	seenTextCandidates := map[string]reviewFeedbackCandidate{}

	for i := range findings {
		if findings[i].Status == "" {
			findings[i].Status = findingStatusOpen
		}

		if findings[i].Key == "" {
			baseText := findings[i].ActionableText
			if baseText == "" {
				baseText = findings[i].RawText
			}
			findings[i].Key = buildFindingKey(reviewFeedbackCandidate{
				Path:           findings[i].Path,
				Line:           findings[i].Line,
				SourceURL:      findings[i].SourceURL,
				ActionableText: baseText,
			})
		}

		if findings[i].Status != findingStatusOpen || findings[i].Key == "" {
			continue
		}

		openByKey[findings[i].Key] = i
		locationKey := findingLocationKey(findings[i].Path, findings[i].Line, findings[i].SourceURL, findings[i].ReviewerType)
		if locationKey != "" {
			openByLocation[locationKey] = append(openByLocation[locationKey], i)
		}
	}

	for _, candidate := range candidates {
		if strings.TrimSpace(candidate.ActionableText) == "" {
			continue
		}

		findingKey := buildFindingKey(candidate)
		if findingKey == "" {
			continue
		}

		if seenInBatch[findingKey] {
			continue
		}

		textKey := canonicalizeFeedbackText(candidate.ActionableText)
		if previous, ok := seenTextCandidates[textKey]; ok && shouldCollapseByText(previous, candidate) {
			continue
		}

		seenInBatch[findingKey] = true
		seenTextCandidates[textKey] = candidate

		if existingIdx, ok := openByKey[findingKey]; ok {
			existing := findings[existingIdx]
			existing.Status = findingStatusOpen
			existing.RawText = truncateText(candidate.RawText, maxRawFeedbackTextLen)
			existing.ActionableText = truncateText(candidate.ActionableText, maxActionableTextLen)
			existing.SourceType = candidate.SourceType
			existing.SourceID = candidate.SourceID
			existing.SourceNodeID = candidate.SourceNodeID
			existing.SourceURL = candidate.SourceURL
			existing.ReviewerLogin = candidate.ReviewerLogin
			existing.ReviewerType = candidate.ReviewerType
			existing.Path = candidate.Path
			existing.Line = candidate.Line
			existing.CommitSHA = candidate.CommitSHA
			existing.LastSeenAt = now
			existing.LastSeenIteration = loop.Iteration
			findings[existingIdx] = existing

			classification.Repeated = append(classification.Repeated, existing)
			classification.Dispatchable = append(classification.Dispatchable, existing)
			continue
		}

		locationKey := findingLocationKey(candidate.Path, candidate.Line, candidate.SourceURL, candidate.ReviewerType)
		if locationKey != "" {
			for _, existingIdx := range openByLocation[locationKey] {
				existing := findings[existingIdx]
				if existing.Status != findingStatusOpen || existing.Key == findingKey {
					continue
				}
				existing.Status = findingStatusSuperseded
				existing.LastSeenAt = now
				existing.LastSeenIteration = loop.Iteration
				findings[existingIdx] = existing
				classification.Superseded = append(classification.Superseded, existing)
			}
		}

		newFinding := kvstore.ReviewFinding{
			Key:                findingKey,
			Status:             findingStatusOpen,
			SourceType:         candidate.SourceType,
			SourceID:           candidate.SourceID,
			SourceNodeID:       candidate.SourceNodeID,
			SourceURL:          candidate.SourceURL,
			ReviewerLogin:      candidate.ReviewerLogin,
			ReviewerType:       candidate.ReviewerType,
			Path:               candidate.Path,
			Line:               candidate.Line,
			CommitSHA:          candidate.CommitSHA,
			RawText:            truncateText(candidate.RawText, maxRawFeedbackTextLen),
			ActionableText:     truncateText(candidate.ActionableText, maxActionableTextLen),
			FirstSeenAt:        now,
			LastSeenAt:         now,
			FirstSeenIteration: loop.Iteration,
			LastSeenIteration:  loop.Iteration,
		}

		findings = append(findings, newFinding)
		idx := len(findings) - 1
		openByKey[newFinding.Key] = idx
		if locationKey != "" {
			openByLocation[locationKey] = append(openByLocation[locationKey], idx)
		}

		classification.New = append(classification.New, newFinding)
		classification.Dispatchable = append(classification.Dispatchable, newFinding)
	}

	for i := range findings {
		finding := findings[i]
		if finding.Status != findingStatusOpen {
			continue
		}
		if seenInBatch[finding.Key] {
			continue
		}
		if !shouldCollectForPhase(loop.Phase, finding.ReviewerType) {
			continue
		}

		finding.Status = findingStatusResolved
		finding.LastSeenAt = now
		finding.LastSeenIteration = loop.Iteration
		findings[i] = finding
		classification.Resolved = append(classification.Resolved, finding)
	}

	loop.Findings = boundReviewFindings(findings, maxReviewFindingsRetained)

	return classification
}

func buildFindingKey(candidate reviewFeedbackCandidate) string {
	actionable := canonicalizeFeedbackText(candidate.ActionableText)
	if actionable == "" {
		return ""
	}

	location := findingFingerprintScope(candidate)
	keyMaterial := actionable
	if location != "" {
		keyMaterial = location + "|" + actionable
	}

	digest := sha256.Sum256([]byte(keyMaterial))
	return hex.EncodeToString(digest[:16])
}

func findingFingerprintScope(candidate reviewFeedbackCandidate) string {
	normalizedPath := strings.TrimSpace(candidate.Path)
	if normalizedPath != "" || candidate.Line > 0 {
		return findingLocationKey(candidate.Path, candidate.Line, "", "")
	}

	sourceType := strings.ToLower(strings.TrimSpace(candidate.SourceType))
	reviewerLogin := strings.ToLower(strings.TrimSpace(candidate.ReviewerLogin))
	if sourceType != "" || reviewerLogin != "" {
		return sourceType + "|" + reviewerLogin
	}

	// Fallback for rare cases with missing metadata.
	return findingLocationKey("", 0, candidate.SourceURL, "")
}

func formatFindingsForCursorComment(findings []kvstore.ReviewFinding) string {
	if len(findings) == 0 {
		return ""
	}

	var sb strings.Builder
	index := 0
	for _, finding := range findings {
		text := strings.TrimSpace(finding.ActionableText)
		if text == "" {
			text = strings.TrimSpace(finding.RawText)
		}
		if text == "" {
			continue
		}

		index++
		switch {
		case finding.Path != "" && finding.Line > 0:
			sb.WriteString(fmt.Sprintf("%d. **%s:%d** - %s\n", index, finding.Path, finding.Line, text))
		case finding.Path != "":
			sb.WriteString(fmt.Sprintf("%d. **%s** - %s\n", index, finding.Path, text))
		default:
			sb.WriteString(fmt.Sprintf("%d. %s\n", index, text))
		}
	}

	return strings.TrimSpace(sb.String())
}

func defaultReviewLoopFeedbackText() string {
	return strings.TrimSpace(`
You are an implementation agent performing a final pass on your pull request before approval. Your goal is to ensure every unresolved review comment is either addressed or explicitly responded to.

## Task

Using the ` + "`gh`" + ` CLI (including the GraphQL API where needed), audit all review comments on this PR that are **not in a resolved state**. For each unresolved comment, take exactly one of the following actions:

1. **OUTDATED comments** (the underlying code has already changed): Resolve the thread. No reply is needed.
2. **Feedback that still requires a code change**: Make the change, then reply to the thread (via its source ID) stating what you changed and why.
3. **Feedback you previously determined to be incorrect or no longer applicable**: Reply to the thread (via its source ID) with a concise explanation of why no change was made - e.g., the suggestion conflicts with project requirements, was based on a misunderstanding, or was already addressed by a different change.

## Workflow

1. Fetch all review threads on this PR using the GraphQL API. Filter to threads where ` + "`isResolved == false`" + `.
2. For each unresolved thread, check its ` + "`isOutdated`" + ` state via the GraphQL API.
3. Classify the thread into one of the three categories above.
4. Execute the appropriate action (resolve, apply fix + reply, or reply with justification).
5. Do **not** leave any review thread unresolved and unreplied.

## Constraints

- Use the ` + "`gh api graphql`" + ` command to query thread state (` + "`isResolved`" + `, ` + "`isOutdated`" + `).
- Every non-outdated, unresolved thread **must** receive a reply - even if the answer is "no change needed." Silence is not acceptable.
- When replying, be specific: reference the exact change made (file + line if applicable) or the exact reason the suggestion was declined.
- Do not fabricate changes. If you are unsure whether feedback was already addressed, diff the relevant file against the base branch before deciding.
`)
}

func formatFindingsForCursorFollowup(loop *kvstore.ReviewLoop, pr ghPullRequest, findings []kvstore.ReviewFinding) string {
	var sb strings.Builder
	sb.WriteString("Apply the latest pull request review feedback and push fixes to the existing branch.\n\n")
	sb.WriteString("PR context:\n")

	repository := strings.TrimSpace(loop.Repository)
	if repository == "" {
		repository = strings.Trim(strings.TrimSpace(loop.Owner+"/"+loop.Repo), "/")
	}
	if repository != "" {
		sb.WriteString(fmt.Sprintf("- repository: %s\n", repository))
	}
	if loop.PRURL != "" {
		sb.WriteString(fmt.Sprintf("- pull_request_url: %s\n", loop.PRURL))
	}
	if loop.PRNumber > 0 {
		sb.WriteString(fmt.Sprintf("- pull_request_number: %d\n", loop.PRNumber))
	}
	if pr.Head.Ref != "" {
		sb.WriteString(fmt.Sprintf("- branch: %s\n", pr.Head.Ref))
	}

	dispatchSHA := strings.TrimSpace(pr.Head.SHA)
	if dispatchSHA == "" {
		dispatchSHA = strings.TrimSpace(loop.LastCommitSHA)
	}
	if dispatchSHA != "" {
		sb.WriteString(fmt.Sprintf("- head_sha: %s\n", dispatchSHA))
	}
	sb.WriteString(fmt.Sprintf("- review_loop_iteration: %d\n\n", loop.Iteration))

	sb.WriteString("Execution constraints:\n")
	sb.WriteString("- work on the existing pull request branch\n")
	sb.WriteString("- do not create a new pull request\n")
	sb.WriteString("- keep changes scoped to the findings below\n\n")

	if len(findings) == 0 {
		sb.WriteString("No actionable findings were extracted from structured review data.\n")
		sb.WriteString(defaultReviewLoopFeedbackText())
		return strings.TrimSpace(sb.String())
	}

	sb.WriteString("Actionable findings:\n")
	index := 0
	for _, finding := range findings {
		text := strings.TrimSpace(finding.ActionableText)
		if text == "" {
			text = strings.TrimSpace(finding.RawText)
		}
		if text == "" {
			continue
		}

		index++
		sb.WriteString(fmt.Sprintf("%d. %s\n", index, text))

		metadata := make([]string, 0, 7)
		if finding.SourceType != "" {
			metadata = append(metadata, "source_type="+finding.SourceType)
		}
		if finding.SourceID > 0 {
			metadata = append(metadata, "source_id="+strconv.FormatInt(finding.SourceID, 10))
		}
		if finding.SourceURL != "" {
			metadata = append(metadata, "source_url="+finding.SourceURL)
		}
		if finding.Path != "" {
			metadata = append(metadata, "path="+finding.Path)
		}
		if finding.Line > 0 {
			metadata = append(metadata, "line="+strconv.Itoa(finding.Line))
		}
		if finding.ReviewerLogin != "" {
			metadata = append(metadata, "reviewer="+finding.ReviewerLogin)
		}
		if finding.CommitSHA != "" {
			metadata = append(metadata, "commit_sha="+finding.CommitSHA)
		}
		if len(metadata) > 0 {
			sb.WriteString("   metadata: " + strings.Join(metadata, ", ") + "\n")
		}
	}

	if index == 0 {
		sb.WriteString("No actionable findings were extracted from structured review data.\n")
		sb.WriteString(defaultReviewLoopFeedbackText())
	}

	return strings.TrimSpace(sb.String())
}

func reviewFeedbackDigest(findings []kvstore.ReviewFinding) string {
	if len(findings) == 0 {
		return ""
	}

	parts := make([]string, 0, len(findings))
	for _, finding := range findings {
		parts = append(parts, strings.Join([]string{
			finding.Key,
			finding.Path,
			strconv.Itoa(finding.Line),
			canonicalizeFeedbackText(finding.ActionableText),
		}, "|"))
	}
	sort.Strings(parts)

	sum := sha256.Sum256([]byte(strings.Join(parts, "\n")))
	return hex.EncodeToString(sum[:])
}

func (p *Plugin) reviewerTypeForLogin(login string) string {
	if p.isAIReviewerBot(login) {
		return reviewerTypeAIBot
	}
	return reviewerTypeHuman
}

func shouldCollectForPhase(phase, reviewerType string) bool {
	switch phase {
	case kvstore.ReviewPhaseAwaitingReview:
		return reviewerType == reviewerTypeAIBot
	case kvstore.ReviewPhaseHumanReview:
		return reviewerType == reviewerTypeHuman
	default:
		return false
	}
}

func canonicalizeFeedbackText(text string) string {
	trimmed := strings.TrimSpace(strings.ToLower(text))
	if trimmed == "" {
		return ""
	}

	trimmed = strings.ReplaceAll(trimmed, "\r\n", "\n")
	// Normalize line-level whitespace while preserving sentence boundaries.
	lines := strings.Split(trimmed, "\n")
	for i := range lines {
		lines[i] = collapseSpacesRE.ReplaceAllString(strings.TrimSpace(lines[i]), " ")
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func shouldCollapseByText(a, b reviewFeedbackCandidate) bool {
	if canonicalizeFeedbackText(a.ActionableText) != canonicalizeFeedbackText(b.ActionableText) {
		return false
	}

	locationA := findingLocationKey(a.Path, a.Line, a.SourceURL, "")
	locationB := findingLocationKey(b.Path, b.Line, b.SourceURL, "")

	if locationA == locationB {
		return true
	}

	// Treat unscoped text (e.g., review body) as duplicate of scoped inline
	// feedback when the actionable directive text matches exactly.
	return locationA == "" || locationB == ""
}

func findingLocationKey(path string, line int, sourceURL, reviewerType string) string {
	normalizedPath := strings.ToLower(strings.TrimSpace(path))
	if normalizedPath != "" || line > 0 {
		if reviewerType != "" {
			return reviewerType + "|" + normalizedPath + ":" + strconv.Itoa(line)
		}
		return normalizedPath + ":" + strconv.Itoa(line)
	}

	normalizedURL := strings.ToLower(strings.TrimSpace(sourceURL))
	if reviewerType != "" && normalizedURL != "" {
		return reviewerType + "|" + normalizedURL
	}
	return normalizedURL
}

func boundReviewFindings(findings []kvstore.ReviewFinding, limit int) []kvstore.ReviewFinding {
	if limit <= 0 || len(findings) <= limit {
		return findings
	}

	start := len(findings) - limit
	bounded := make([]kvstore.ReviewFinding, limit)
	copy(bounded, findings[start:])
	return bounded
}
