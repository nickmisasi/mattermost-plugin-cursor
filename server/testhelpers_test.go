package main

import (
	"strings"

	"github.com/mattermost/mattermost/server/public/model"
)

// getAttachments extracts SlackAttachments from a post's Props.
// Returns nil if none found.
func getAttachments(post *model.Post) []*model.SlackAttachment {
	if post.Props == nil {
		return nil
	}
	raw, ok := post.Props["attachments"]
	if !ok || raw == nil {
		return nil
	}
	// model.ParseSlackAttachment stores []*model.SlackAttachment in Props["attachments"].
	// In test matchers the post object is constructed directly before serialization,
	// so we can type-assert the slice directly.
	if atts, ok := raw.([]*model.SlackAttachment); ok {
		return atts
	}
	return nil
}

// hasAttachmentWithColor checks if the post has at least one
// SlackAttachment with the specified Color hex value.
func hasAttachmentWithColor(post *model.Post, color string) bool {
	for _, a := range getAttachments(post) {
		if a.Color == color {
			return true
		}
	}
	return false
}

// hasAttachmentWithTitle checks if the post has at least one
// SlackAttachment whose Title contains the specified substring.
func hasAttachmentWithTitle(post *model.Post, titleSubstr string) bool {
	for _, a := range getAttachments(post) {
		if strings.Contains(a.Title, titleSubstr) {
			return true
		}
	}
	return false
}

// containsSubstring checks if s contains substr (case-insensitive).
func containsSubstring(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}
