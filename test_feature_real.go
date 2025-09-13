package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/olle-forsslof/kumpan-newspaper/internal/ai"
	"github.com/olle-forsslof/kumpan-newspaper/internal/database"
)

func main() {
	// Get API key from environment
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		log.Fatal("❌ ANTHROPIC_API_KEY environment variable is required\n\n" +
			"💡 Set it by running: export ANTHROPIC_API_KEY='your-key-here'\n" +
			"   You can get your API key from: https://console.anthropic.com/account/keys")
	}

	// Create AI service
	aiService := ai.NewAnthropicService(apiKey)

	// Test cases for the Feature Writer
	testCases := []struct {
		name    string
		content string
	}{
		{
			name:    "Mötesleda",
			content: "Jag har just varit på ett möte som var en timme långt och handlade om när vi ska ha ett annat möte för en viktig förändring. Ibland är jag så less på möten, men jag förstår ju att de är nödvändiga.",
		},
		{
			name:    "Konferens",
			content: "Vi har just planerat en konferens som vi ska ha i höst. Det känns kul, det är ett ställe vi inte varit på förut, men jag tror att de allra flesta på företaget verkligen kommer gilla det.",
		},
	}

	fmt.Println("🧪 Testing Feature Writer journalist with real Anthropic API...")
	fmt.Println("🔑 API Key found:", apiKey[:8]+"...")
	fmt.Println()

	for i, testCase := range testCases {
		fmt.Printf("📝 Test Case %d: %s\n", i+1, testCase.name)
		fmt.Printf("📄 Submission: %s\n", testCase.content)
		fmt.Println(strings.Repeat("-", 80))

		// Create test submission
		submission := database.Submission{
			ID:      i + 1,
			UserID:  "TEST_USER",
			Content: testCase.content,
		}

		// Process with feature journalist
		ctx := context.Background()
		fmt.Println("⏳ Processing with Feature Writer...")

		article, err := aiService.ProcessSubmission(ctx, submission, "feature")
		if err != nil {
			fmt.Printf("❌ Processing failed: %v\n\n", err)
			continue
		}

		// Display results
		fmt.Println("✅ SUCCESS! Here's what the Feature Writer wrote:")
		fmt.Println(strings.Repeat("=", 80))
		fmt.Printf("%s\n", article.ProcessedContent)
		fmt.Println(strings.Repeat("=", 80))

		fmt.Printf("\n📊 Article Metadata:\n")
		fmt.Printf("   📏 Word Count: %d words (max: 300)\n", article.WordCount)
		fmt.Printf("   👨‍💼 Journalist: %s\n", article.JournalistType)
		fmt.Printf("   ✅ Status: %s\n", article.ProcessingStatus)
		fmt.Printf("   🎨 Template: %s\n", article.TemplateFormat)
		if article.ProcessedAt != nil {
			fmt.Printf("   ⏰ Processed: %s\n", article.ProcessedAt.Format("15:04:05"))
		}

		// Quality checks
		fmt.Printf("\n🔍 Quality Assessment:\n")
		if article.WordCount >= 250 && article.WordCount <= 300 {
			fmt.Printf("   ✅ Perfect length (%d words, target: 250-300)\n", article.WordCount)
		} else if article.WordCount <= 300 {
			fmt.Printf("   ⚠️  Short but acceptable (%d words, target: 250-300)\n", article.WordCount)
		} else {
			fmt.Printf("   ❌ Too long (%d words > 300)\n", article.WordCount)
		}

		// Check for engaging features
		content := strings.ToLower(article.ProcessedContent)
		checks := 0

		// Check for strong opening
		firstSentence := strings.Split(article.ProcessedContent, ".")[0]
		if len(firstSentence) > 20 && (strings.Contains(strings.ToLower(firstSentence), "when") ||
			strings.Contains(strings.ToLower(firstSentence), "imagine") ||
			strings.Contains(strings.ToLower(firstSentence), "picture") ||
			!strings.HasPrefix(strings.TrimSpace(firstSentence), "The ")) {
			fmt.Printf("   ✅ Strong, engaging opening\n")
			checks++
		} else {
			fmt.Printf("   ⚠️  Opening could be more engaging\n")
		}

		// Check for human element
		humanWords := []string{"team", "colleague", "people", "everyone", "member", "person", "individual"}
		humanFound := 0
		for _, word := range humanWords {
			if strings.Contains(content, word) {
				humanFound++
			}
		}
		if humanFound >= 3 {
			fmt.Printf("   ✅ Strong human element (%d human-focused terms)\n", humanFound)
			checks++
		} else {
			fmt.Printf("   ⚠️  Could emphasize human element more (%d human-focused terms)\n", humanFound)
		}

		// Check for active voice indicators
		activeWords := []string{"launched", "created", "discovered", "achieved", "built", "developed"}
		activeFound := 0
		for _, word := range activeWords {
			if strings.Contains(content, word) {
				activeFound++
			}
		}
		if activeFound >= 2 {
			fmt.Printf("   ✅ Active voice detected (%d active verbs)\n", activeFound)
			checks++
		} else {
			fmt.Printf("   ⚠️  Could use more active voice (%d active verbs)\n", activeFound)
		}

		overallScore := float64(checks) / 3.0 * 100
		fmt.Printf("   📈 Overall Feature Quality: %.0f%%\n", overallScore)

		fmt.Printf("\n" + strings.Repeat("=", 100) + "\n\n")
	}

	fmt.Println("🎉 Feature Writer testing complete!")
}
