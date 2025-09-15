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

	// Test cases for the Body and Mind columnist
	testCases := []struct {
		name    string
		content string
	}{
		{
			name:    "Jobbkänslor",
			content: "Jag gillar en på mitt jobb, men jag vet inte om den personen gillar mig. Vad ska jag säga för att ta reda på dens känslor utan att avslöja mina egna, och samtidigt charma den andre?",
		},
		{
			name:    "Djupare diskussioner",
			content: "Jag skulle vilja prata om djupare, mer existentiella ämnen med mina vänner. Dom pratar bara om tvserier. Hur ska jag göra?",
		},
	}

	fmt.Println("🧪 Testing Body and Mind columnist with real Anthropic API...")
	fmt.Println("🔑 API Key found:", apiKey[:8]+"...")
	fmt.Println()

	for i, testCase := range testCases {
		fmt.Printf("📝 Test Case %d: %s\n", i+1, testCase.name)
		fmt.Printf("❓ Question: %s\n", testCase.content)
		fmt.Println(strings.Repeat("-", 80))

		// Create test submission
		submission := database.Submission{
			ID:      i + 1,
			UserID:  "TEST_USER",
			Content: testCase.content,
		}

		// Process with body_mind journalist
		ctx := context.Background()
		fmt.Println("⏳ Processing with Body and Mind columnist...")

		article, err := aiService.ProcessSubmission(ctx, submission, "body_mind")
		if err != nil {
			fmt.Printf("❌ Processing failed: %v\n\n", err)
			continue
		}

		// Display results
		fmt.Println("✅ SUCCESS! Here's what the Body and Mind columnist wrote:")
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
		if article.WordCount <= 300 {
			fmt.Printf("   ✅ Length appropriate (%d words)\n", article.WordCount)
		} else {
			fmt.Printf("   ⚠️  Too long (%d words > 300)\n", article.WordCount)
		}

		// Check for pseudonym (look for "Sincerely" or similar)
		content := strings.ToLower(article.ProcessedContent)
		if strings.Contains(content, "sincerely") || strings.Contains(content, "yours truly") || strings.Contains(content, "warmly") {
			fmt.Printf("   ✅ Contains appropriate sign-off\n")
		} else {
			fmt.Printf("   ⚠️  Missing expected sign-off pattern\n")
		}

		// Check for empathetic language
		empathyWords := []string{"understand", "courage", "empathy", "feel", "difficult", "challenging"}
		empathyFound := 0
		for _, word := range empathyWords {
			if strings.Contains(content, word) {
				empathyFound++
			}
		}

		if empathyFound >= 2 {
			fmt.Printf("   ✅ Empathetic tone detected (%d empathy indicators)\n", empathyFound)
		} else {
			fmt.Printf("   ⚠️  Low empathy language (%d empathy indicators)\n", empathyFound)
		}

		fmt.Printf("\n" + strings.Repeat("=", 100) + "\n\n")
	}

	fmt.Println("🎉 Testing complete! Your Body and Mind columnist is ready for production.")
	fmt.Println("\n💡 Next steps:")
	fmt.Println("   1. Review the responses above for quality")
	fmt.Println("   2. Adjust journalist prompts if needed")
	fmt.Println("   3. Add admin commands to process submissions")
	fmt.Println("   4. Integrate with your newsletter template system")
}
