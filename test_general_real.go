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

	// Test cases for the General/Staff Reporter
	testCases := []struct {
		name    string
		content string
	}{
		{
			name:    "Lunch",
			content: "Jag åt en wrap med falafel från coop. Den får 5 / 10.",
		},
		{
			name:    "Bästa dagen",
			content: "Jag skulle nog säga onsdag är den bästa dagen. Mitt i veckan, inte så långt kvar till helgen. Och ibland är det brädspelskväll.",
		},
		{
			name:    "Parkering",
			content: "Vi vill påminna om att vi har två parkeringsplatser tillgängliga. 220A och 220b. Markera i Synqa appen om du plaerar att komma med bil så alla kan se om det är upptaget.",
		},
	}

	fmt.Println("🧪 Testing General/Staff Reporter journalist with real Anthropic API...")
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

		// Process with general journalist
		ctx := context.Background()
		fmt.Println("⏳ Processing with General/Staff Reporter...")

		article, err := aiService.ProcessSubmission(ctx, submission, "general")
		if err != nil {
			fmt.Printf("❌ Processing failed: %v\n\n", err)
			continue
		}

		// Display results
		fmt.Println("✅ SUCCESS! Here's what the Staff Reporter wrote:")
		fmt.Println(strings.Repeat("=", 80))
		fmt.Printf("%s\n", article.ProcessedContent)
		fmt.Println(strings.Repeat("=", 80))

		fmt.Printf("\n📊 Article Metadata:\n")
		fmt.Printf("   📏 Word Count: %d words (max: 150)\n", article.WordCount)
		fmt.Printf("   👨‍💼 Journalist: %s\n", article.JournalistType)
		fmt.Printf("   ✅ Status: %s\n", article.ProcessingStatus)
		fmt.Printf("   🎨 Template: %s\n", article.TemplateFormat)
		if article.ProcessedAt != nil {
			fmt.Printf("   ⏰ Processed: %s\n", article.ProcessedAt.Format("15:04:05"))
		}

		// Quality checks
		fmt.Printf("\n🔍 Quality Assessment:\n")
		if article.WordCount >= 100 && article.WordCount <= 150 {
			fmt.Printf("   ✅ Perfect length (%d words, target: 100-150)\n", article.WordCount)
		} else if article.WordCount <= 150 {
			fmt.Printf("   ⚠️  Short but acceptable (%d words, target: 100-150)\n", article.WordCount)
		} else {
			fmt.Printf("   ❌ Too long (%d words > 150)\n", article.WordCount)
		}

		content := strings.ToLower(article.ProcessedContent)
		checks := 0

		// Check for clarity and directness
		clarityWords := []string{"starting", "beginning", "effective", "new", "important", "please", "will", "should"}
		clarityFound := 0
		for _, word := range clarityWords {
			if strings.Contains(content, word) {
				clarityFound++
			}
		}

		if clarityFound >= 3 {
			fmt.Printf("   ✅ Clear and direct language (%d clarity indicators)\n", clarityFound)
			checks++
		} else {
			fmt.Printf("   ⚠️  Could be clearer and more direct (%d clarity indicators)\n", clarityFound)
		}

		// Check for actionable information
		actionWords := []string{"need to", "should", "must", "required", "contact", "visit", "bring", "schedule"}
		actionFound := 0
		for _, word := range actionWords {
			if strings.Contains(content, word) {
				actionFound++
			}
		}

		if actionFound >= 2 {
			fmt.Printf("   ✅ Contains actionable information (%d action indicators)\n", actionFound)
			checks++
		} else {
			fmt.Printf("   ⚠️  Could include more actionable information (%d action indicators)\n", actionFound)
		}

		// Check for accessibility (no jargon)
		jargonWords := []string{"leverage", "synergy", "utilize", "paradigm", "optimize", "streamline"}
		jargonFound := 0
		for _, word := range jargonWords {
			if strings.Contains(content, word) {
				jargonFound++
			}
		}

		if jargonFound == 0 {
			fmt.Printf("   ✅ Jargon-free language\n")
			checks++
		} else {
			fmt.Printf("   ⚠️  Contains jargon (%d jargon words)\n", jargonFound)
		}

		// Check for team relevance
		teamWords := []string{"team", "employees", "everyone", "all", "staff", "colleagues", "members"}
		teamFound := 0
		for _, word := range teamWords {
			if strings.Contains(content, word) {
				teamFound++
			}
		}

		if teamFound >= 2 {
			fmt.Printf("   ✅ Team-focused messaging (%d team references)\n", teamFound)
			checks++
		} else {
			fmt.Printf("   ⚠️  Could be more team-focused (%d team references)\n", teamFound)
		}

		overallScore := float64(checks) / 4.0 * 100
		fmt.Printf("   📈 Overall General Report Quality: %.0f%%\n", overallScore)

		fmt.Printf("\n" + strings.Repeat("=", 100) + "\n\n")
	}

	fmt.Println("🎉 General/Staff Reporter testing complete!")
}
