package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"regexp"
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

	// Test cases for the Interview Specialist
	testCases := []struct {
		name    string
		content string
	}{
		{
			name:    "Vad gör du på fritiden?",
			content: "Jag gillar att läsa böcker, spela spel, dricka drinkar och så. Umgås med kompisar.",
		},
		{
			name:    "Berätta om dig själv!",
			content: "Jag heter Olle, jag är 41år gammal och jag har jobbat på kumpan i 4 år. Jag äter vanligtvis inte frukost, det är något som jag har börjat hoppa över sen ca 2 år tillbaka. Efter att jag började hoppa över frukosten så insåg jag att jag inte är hungrig på morgonen, det är bara en vana. Ehm, vad mer. Jag brukade illustrera mycket. Det var faktiskt mitt jobb innan jag började på kumpan, jag jobbade som frilansillustratör. Det var kul, men typ stressigt ekonomiskt, speciellt efter att jag fick barn - då kände jag ett ansvar för att kunna ta hand om barnet ekonomiskt. Inte bara ha sig själv att rå om lliksom. Jag driver fortfarande ett serieförlag som heter Peow2. Förut hette det peow, men vi slutade för 3 år sen. Men nu har vi startat igen, och då heter det 2. Precis som uppföljare till filmer.",
		},
	}

	fmt.Println("🧪 Testing Interview Specialist journalist with real Anthropic API...")
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

		// Process with interview journalist
		ctx := context.Background()
		fmt.Println("⏳ Processing with Interview Specialist...")

		article, err := aiService.ProcessSubmission(ctx, submission, "interview")
		if err != nil {
			fmt.Printf("❌ Processing failed: %v\n\n", err)
			continue
		}

		// Display results
		fmt.Println("✅ SUCCESS! Here's what the Interview Specialist wrote:")
		fmt.Println(strings.Repeat("=", 80))
		fmt.Printf("%s\n", article.ProcessedContent)
		fmt.Println(strings.Repeat("=", 80))

		fmt.Printf("\n📊 Article Metadata:\n")
		fmt.Printf("   📏 Word Count: %d words (max: 200)\n", article.WordCount)
		fmt.Printf("   👨‍💼 Journalist: %s\n", article.JournalistType)
		fmt.Printf("   ✅ Status: %s\n", article.ProcessingStatus)
		fmt.Printf("   🎨 Template: %s\n", article.TemplateFormat)
		if article.ProcessedAt != nil {
			fmt.Printf("   ⏰ Processed: %s\n", article.ProcessedAt.Format("15:04:05"))
		}

		// Quality checks
		fmt.Printf("\n🔍 Quality Assessment:\n")
		if article.WordCount >= 150 && article.WordCount <= 200 {
			fmt.Printf("   ✅ Perfect length (%d words, target: 150-200)\n", article.WordCount)
		} else if article.WordCount <= 200 {
			fmt.Printf("   ⚠️  Short but acceptable (%d words, target: 150-200)\n", article.WordCount)
		} else {
			fmt.Printf("   ❌ Too long (%d words > 200)\n", article.WordCount)
		}

		// Check Q&A format
		content := article.ProcessedContent
		checks := 0

		// Count question marks (should be 3-4 questions)
		questionRegex := regexp.MustCompile(`\?`)
		questions := questionRegex.FindAllString(content, -1)
		questionCount := len(questions)

		if questionCount >= 3 && questionCount <= 4 {
			fmt.Printf("   ✅ Perfect question count (%d questions, target: 3-4)\n", questionCount)
			checks++
		} else if questionCount >= 2 {
			fmt.Printf("   ⚠️  Acceptable question count (%d questions, target: 3-4)\n", questionCount)
		} else {
			fmt.Printf("   ❌ Too few questions (%d questions, minimum: 3)\n", questionCount)
		}

		// Check for Q: and A: format indicators
		hasQAFormat := strings.Contains(content, "Q:") || strings.Contains(content, "Question:") ||
			strings.Contains(content, "A:") || strings.Contains(content, "Answer:")

		if hasQAFormat {
			fmt.Printf("   ✅ Proper Q&A format detected\n")
			checks++
		} else {
			fmt.Printf("   ⚠️  Q&A format not clearly indicated\n")
		}

		// Check for conversational tone
		conversationalWords := []string{"tell us", "how do", "what's", "can you", "that's", "it's", "you're"}
		conversationalFound := 0
		contentLower := strings.ToLower(content)
		for _, word := range conversationalWords {
			if strings.Contains(contentLower, word) {
				conversationalFound++
			}
		}

		if conversationalFound >= 3 {
			fmt.Printf("   ✅ Natural conversational tone (%d conversational indicators)\n", conversationalFound)
			checks++
		} else {
			fmt.Printf("   ⚠️  Could be more conversational (%d conversational indicators)\n", conversationalFound)
		}

		// Check for follow-up/building questions
		buildingWords := []string{"and what about", "how did that", "what was", "tell me more", "speaking of"}
		buildingFound := 0
		for _, phrase := range buildingWords {
			if strings.Contains(contentLower, phrase) {
				buildingFound++
			}
		}

		if buildingFound >= 1 {
			fmt.Printf("   ✅ Questions build on each other (%d building indicators)\n", buildingFound)
			checks++
		} else {
			fmt.Printf("   ⚠️  Questions could build more on each other\n")
		}

		overallScore := float64(checks) / 4.0 * 100
		fmt.Printf("   📈 Overall Interview Quality: %.0f%%\n", overallScore)

		fmt.Printf("\n" + strings.Repeat("=", 100) + "\n\n")
	}

	fmt.Println("🎉 Interview Specialist testing complete!")
}
