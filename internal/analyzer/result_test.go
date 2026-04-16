package analyzer

import "testing"

func TestResult_HasReplacement(t *testing.T) {
	replaceText := "***"

	tests := []struct {
		name     string
		result   Result
		expected bool
	}{
		{"with replacement", Result{ReplaceText: &replaceText}, true},
		{"without replacement", Result{ReplaceText: nil}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.result.HasReplacement(); got != tt.expected {
				t.Errorf("HasReplacement() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestNeedsReplacement(t *testing.T) {
	replaceText := "***"

	tests := []struct {
		name     string
		results  []Result
		expected bool
	}{
		{"empty", []Result{}, false},
		{"no replacement", []Result{{Word: "bad"}}, false},
		{"with replacement", []Result{{Word: "bad", ReplaceText: &replaceText}}, true},
		{"mixed", []Result{{Word: "bad"}, {Word: "evil", ReplaceText: &replaceText}}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NeedsReplacement(tt.results); got != tt.expected {
				t.Errorf("NeedsReplacement() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestExtractWords(t *testing.T) {
	results := []Result{
		{Word: "bad"},
		{Word: "evil"},
		{Word: "wrong"},
	}

	words := ExtractWords(results)
	if len(words) != 3 {
		t.Fatalf("expected 3 words, got %d", len(words))
	}

	expected := []string{"bad", "evil", "wrong"}
	for i, w := range words {
		if w != expected[i] {
			t.Errorf("word[%d] = %s, want %s", i, w, expected[i])
		}
	}
}

func TestExtractWords_Empty(t *testing.T) {
	words := ExtractWords([]Result{})
	if len(words) != 0 {
		t.Errorf("expected 0 words, got %d", len(words))
	}
}
