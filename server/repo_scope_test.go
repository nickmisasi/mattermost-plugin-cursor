package main

import "testing"

func TestIsMattermostMattermostRepository(t *testing.T) {
	tests := []struct {
		name       string
		repository string
		expected   bool
	}{
		{name: "owner repo format", repository: "mattermost/mattermost", expected: true},
		{name: "https github url", repository: "https://github.com/mattermost/mattermost", expected: true},
		{name: "http github url", repository: "http://github.com/mattermost/mattermost", expected: true},
		{name: "github host prefix", repository: "github.com/mattermost/mattermost", expected: true},
		{name: "ssh format", repository: "git@github.com:mattermost/mattermost", expected: true},
		{name: "trim trailing slash and git suffix", repository: "https://github.com/mattermost/mattermost.git/", expected: true},
		{name: "different repository", repository: "mattermost/mattermost-plugin-cursor", expected: false},
		{name: "different organization", repository: "acme/mattermost", expected: false},
		{name: "empty", repository: "", expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isMattermostMattermostRepository(tt.repository); got != tt.expected {
				t.Fatalf("isMattermostMattermostRepository(%q) = %v, want %v", tt.repository, got, tt.expected)
			}
		})
	}
}
