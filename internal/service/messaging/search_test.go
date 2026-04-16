package messaging

import (
	"testing"
)

func TestGenerateSnippet(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		keywords []string
		want     string
	}{
		{
			name:     "Single keyword match",
			content:  "Hello world, this is a test message.",
			keywords: []string{"test"},
			want:     "Hello world, this is a <em>test</em> message.",
		},
		{
			name:     "Multiple keywords match",
			content:  "The quick brown fox jumps over the lazy dog.",
			keywords: []string{"fox", "dog"},
			want:     "The quick brown <em>fox</em> jumps over the lazy <em>dog</em>.",
		},
		{
			name:     "Multiple keywords, one match",
			content:  "The quick brown fox jumps over the lazy dog.",
			keywords: []string{"cat", "fox"},
			want:     "The quick brown <em>fox</em> jumps over the lazy dog.",
		},
		{
			name:     "No match",
			content:  "Hello world",
			keywords: []string{"test"},
			want:     "Hello world",
		},
		{
			name:     "Case insensitive match",
			content:  "Hello World",
			keywords: []string{"world"},
			want:     "Hello <em>World</em>",
		},
		{
			name:     "Multiple keywords case insensitive",
			content:  "Hello World, this is Go.",
			keywords: []string{"world", "go"},
			want:     "Hello <em>World</em>, this is <em>Go</em>.",
		},
		{
			name:     "Long content with match at start",
			content:  "Keyword at the start of a very long message that should be truncated because it is too long to display fully.",
			keywords: []string{"Keyword"},
			want:     "<em>Keyword</em> at the start of a very long message that should be truncated because it is too long to displ...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := generateSnippet(tt.content, tt.keywords)
			// Simple check for presence of highlighted keywords
			// Note: The exact truncation logic might vary, so we check if the highlighted parts are present
			// and if the length is reasonable.

			// We expect at least one keyword to be highlighted if it exists in content
			// But generateSnippet might only show a window around the FIRST match.
			// So this verification is a bit tricky without exact implementation details.
			// Let's just check if the output matches our expectation string which we manually constructed.
			// Wait, I haven't implemented the change yet, so I should write the test to expect the NEW signature.
			// The current generateSnippet takes a string, I will change it to take []string.

			if got != tt.want {
				t.Errorf("generateSnippet() = %v, want %v", got, tt.want)
			}
		})
	}
}
