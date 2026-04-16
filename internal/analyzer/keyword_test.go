package analyzer

import (
	"context"
	"testing"
)

func TestKeywordAnalyzer_Name(t *testing.T) {
	a := NewKeywordAnalyzer()
	if a.Name() != "keyword" {
		t.Errorf("expected name 'keyword', got '%s'", a.Name())
	}
}

func TestKeywordAnalyzer_AnalyzeEmpty(t *testing.T) {
	a := NewKeywordAnalyzer()
	results, err := a.Analyze(context.Background(), "hello world")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results with empty cache, got %d", len(results))
	}
}

func TestKeywordAnalyzer_ExactMatch(t *testing.T) {
	a := NewKeywordAnalyzer()
	replaceText := "***"
	a.RefreshCache([]Word{
		{ID: 1, Word: "bad", MatchType: "exact", Category: "test", Enabled: true, Priority: 1, ReplaceText: &replaceText},
	})

	tests := []struct {
		content  string
		expected int
	}{
		{"this is bad content", 1},
		{"BAD word here", 1},      // case insensitive
		{"this is good content", 0},
		{"", 0},
	}

	for _, tt := range tests {
		results, err := a.Analyze(context.Background(), tt.content)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(results) != tt.expected {
			t.Errorf("content '%s': expected %d results, got %d", tt.content, tt.expected, len(results))
		}
	}
}

func TestKeywordAnalyzer_FuzzyMatch(t *testing.T) {
	a := NewKeywordAnalyzer()
	a.RefreshCache([]Word{
		{ID: 1, Word: "bad word", MatchType: "fuzzy", Category: "test", Enabled: true, Priority: 1},
	})

	tests := []struct {
		content  string
		expected int
	}{
		{"this is badword here", 1},   // no space
		{"this is bad word here", 1},  // with space
		{"BADWORD", 1},                // case insensitive
		{"this is good content", 0},
	}

	for _, tt := range tests {
		results, err := a.Analyze(context.Background(), tt.content)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(results) != tt.expected {
			t.Errorf("content '%s': expected %d results, got %d", tt.content, tt.expected, len(results))
		}
	}
}

func TestKeywordAnalyzer_RegexMatch(t *testing.T) {
	a := NewKeywordAnalyzer()
	a.RefreshCache([]Word{
		{ID: 1, Word: `\d{3}-\d{4}`, MatchType: "regex", Category: "phone", Enabled: true, Priority: 1},
	})

	tests := []struct {
		content  string
		expected int
	}{
		{"call me at 123-4567", 1},
		{"my number is 123-4567-890", 1},
		{"no phone here", 0},
	}

	for _, tt := range tests {
		results, err := a.Analyze(context.Background(), tt.content)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(results) != tt.expected {
			t.Errorf("content '%s': expected %d results, got %d", tt.content, tt.expected, len(results))
		}
	}
}

func TestKeywordAnalyzer_DisabledWord(t *testing.T) {
	a := NewKeywordAnalyzer()
	a.RefreshCache([]Word{
		{ID: 1, Word: "bad", MatchType: "exact", Category: "test", Enabled: false, Priority: 1},
	})

	results, err := a.Analyze(context.Background(), "this is bad content")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results for disabled word, got %d", len(results))
	}
}

func TestKeywordAnalyzer_MultipleMatches(t *testing.T) {
	a := NewKeywordAnalyzer()
	a.RefreshCache([]Word{
		{ID: 1, Word: "bad", MatchType: "exact", Category: "test1", Enabled: true, Priority: 1},
		{ID: 2, Word: "evil", MatchType: "exact", Category: "test2", Enabled: true, Priority: 2},
	})

	results, err := a.Analyze(context.Background(), "this is bad and evil content")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 results, got %d", len(results))
	}
}

func TestKeywordAnalyzer_ResultFields(t *testing.T) {
	a := NewKeywordAnalyzer()
	replaceText := "***"
	a.RefreshCache([]Word{
		{ID: 1, Word: "bad", MatchType: "exact", Category: "profanity", Enabled: true, Priority: 3, ReplaceText: &replaceText},
	})

	results, err := a.Analyze(context.Background(), "this is bad")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	r := results[0]
	if r.Word != "bad" {
		t.Errorf("expected Word 'bad', got '%s'", r.Word)
	}
	if r.Category != "profanity" {
		t.Errorf("expected Category 'profanity', got '%s'", r.Category)
	}
	if r.MatchType != "exact" {
		t.Errorf("expected MatchType 'exact', got '%s'", r.MatchType)
	}
	if r.Priority != 3 {
		t.Errorf("expected Priority 3, got %d", r.Priority)
	}
	if r.Source != "keyword" {
		t.Errorf("expected Source 'keyword', got '%s'", r.Source)
	}
	if r.Confidence != 1.0 {
		t.Errorf("expected Confidence 1.0, got %f", r.Confidence)
	}
	if r.ReplaceText == nil || *r.ReplaceText != "***" {
		t.Errorf("expected ReplaceText '***', got %v", r.ReplaceText)
	}
}

func TestKeywordAnalyzer_GetCacheSize(t *testing.T) {
	a := NewKeywordAnalyzer()
	if a.GetCacheSize() != 0 {
		t.Errorf("expected cache size 0, got %d", a.GetCacheSize())
	}

	a.RefreshCache([]Word{
		{ID: 1, Word: "bad", MatchType: "exact", Enabled: true},
		{ID: 2, Word: "evil", MatchType: "exact", Enabled: true},
	})

	if a.GetCacheSize() != 2 {
		t.Errorf("expected cache size 2, got %d", a.GetCacheSize())
	}
}
