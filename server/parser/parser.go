package parser

import (
	"regexp"
	"strings"
)

// ParsedMention holds the parsed result of a @cursor mention message.
type ParsedMention struct {
	// Prompt is the user's instruction to the agent, with all option tokens removed.
	Prompt string

	// Repository is the GitHub repo in "owner/repo" format, extracted from
	// "in <repo>" or "repo=owner/repo". Empty string means "use defaults".
	Repository string

	// Branch is the base branch, extracted from "branch=<name>".
	// Empty string means "use defaults".
	Branch string

	// Model is the AI model name, extracted from "with <model>" or "model=<name>".
	// Empty string means "use defaults".
	Model string

	// AutoPR is a pointer to a bool. nil means "use defaults".
	// Extracted from "autopr=true|false".
	AutoPR *bool

	// ForceNew is true when the user wrote "@cursor agent <prompt>",
	// which means "always launch a new agent even in an existing thread".
	ForceNew bool
}

var (
	bracketedRe = regexp.MustCompile(`^\[([^\]]+)\]`)
	inlineOptRe = regexp.MustCompile(`(?i)\b(repo|branch|model|autopr)=(\S+)`)
	inRepoRe    = regexp.MustCompile(`(?i)\bin\s+([a-zA-Z0-9._-]+(?:/[a-zA-Z0-9._-]+)?)\s*,?`)
	withModelRe = regexp.MustCompile(`(?i)\bwith\s+([a-zA-Z0-9._-]+)\s*,?`)
	multiSpace  = regexp.MustCompile(`\s{2,}`)
)

// Parse extracts structured fields from a message that has already been
// identified as a @cursor mention. The botMention parameter is the exact
// string to strip (e.g., "@cursor" or "@cursor-bot"), which varies by
// bot username configuration.
//
// Returns nil if the remaining message is empty after stripping the mention
// (i.e., the user just typed "@cursor" with no prompt).
func Parse(message string, botMention string) *ParsedMention {
	// Step 1: Trim whitespace from message.
	message = strings.TrimSpace(message)

	// Step 2: Find botMention in the message (case-insensitive).
	lowerMsg := strings.ToLower(message)
	lowerMention := strings.ToLower(botMention)
	idx := strings.Index(lowerMsg, lowerMention)
	if idx < 0 {
		return nil
	}
	// Strip everything up to and including the botMention.
	remainder := message[idx+len(botMention):]

	// Step 3: Trim leading whitespace from the remainder.
	remainder = strings.TrimSpace(remainder)

	// Step 4: If remainder is empty, return nil.
	if remainder == "" {
		return nil
	}

	result := &ParsedMention{}

	// Step 5: Check for "agent " prefix (case-insensitive).
	if len(remainder) > 6 && strings.EqualFold(remainder[:6], "agent ") {
		result.ForceNew = true
		remainder = strings.TrimSpace(remainder[6:])
	}

	// Step 6: Extract bracketed options block: match `\[([^\]]+)\]` at the start.
	if loc := bracketedRe.FindStringSubmatchIndex(remainder); loc != nil {
		bracketContent := remainder[loc[2]:loc[3]]
		parseBracketedOptions(bracketContent, result)
		remainder = strings.TrimSpace(remainder[loc[1]:])
	}

	// Step 7: Extract inline key=value options from the remainder.
	remainder = extractInlineOptions(remainder, result)

	// Step 8: Extract natural language patterns from the remainder.
	// 8a. "in <repo-name>" pattern — always strip from remainder, but only
	//     set Repository if it is still empty (bracketed/inline takes precedence).
	if loc := inRepoRe.FindStringSubmatchIndex(remainder); loc != nil {
		if result.Repository == "" {
			result.Repository = remainder[loc[2]:loc[3]]
		}
		remainder = remainder[:loc[0]] + remainder[loc[1]:]
	}

	// 8b. "with <model>" pattern — always strip from remainder, but only
	//     set Model if it is still empty.
	if loc := withModelRe.FindStringSubmatchIndex(remainder); loc != nil {
		if result.Model == "" {
			result.Model = remainder[loc[2]:loc[3]]
		}
		remainder = remainder[:loc[0]] + remainder[loc[1]:]
	}

	// Step 9: Trim, collapse multiple spaces to single spaces.
	remainder = strings.TrimSpace(remainder)
	remainder = multiSpace.ReplaceAllString(remainder, " ")
	result.Prompt = remainder

	// Step 10: Return the populated ParsedMention.
	return result
}

// parseBracketedOptions parses comma-separated key=value pairs inside brackets.
func parseBracketedOptions(content string, result *ParsedMention) {
	pairs := strings.Split(content, ",")
	for _, pair := range pairs {
		pair = strings.TrimSpace(pair)
		eqIdx := strings.Index(pair, "=")
		if eqIdx < 0 {
			continue
		}
		key := strings.TrimSpace(strings.ToLower(pair[:eqIdx]))
		value := strings.TrimSpace(pair[eqIdx+1:])
		applyOption(key, value, result)
	}
}

// extractInlineOptions extracts key=value options from the remainder and returns
// the remainder with those options removed.
func extractInlineOptions(remainder string, result *ParsedMention) string {
	matches := inlineOptRe.FindAllStringSubmatchIndex(remainder, -1)
	// Process in reverse order to maintain correct indices when removing.
	for i := len(matches) - 1; i >= 0; i-- {
		loc := matches[i]
		key := strings.ToLower(remainder[loc[2]:loc[3]])
		value := remainder[loc[4]:loc[5]]
		applyOption(key, value, result)
		remainder = remainder[:loc[0]] + remainder[loc[1]:]
	}
	return remainder
}

// applyOption applies a key=value option to the ParsedMention.
func applyOption(key, value string, result *ParsedMention) {
	switch key {
	case "repo":
		result.Repository = value
	case "branch":
		result.Branch = value
	case "model":
		result.Model = value
	case "autopr":
		b := strings.EqualFold(value, "true")
		result.AutoPR = &b
	}
}
