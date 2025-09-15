package ai

import (
	"encoding/json"
	"fmt"
)

// JournalistProfile defines a journalist personality with specific writing style and constraints
type JournalistProfile struct {
	Type              string `json:"type"`
	Name              string `json:"name"`
	SystemPrompt      string `json:"system_prompt"`
	StyleInstructions string `json:"style_instructions"`
	MaxWords          int    `json:"max_words"`
	TemplateFormat    string `json:"template_format"`
}

// JournalistProfiles contains all available journalist personalities
var JournalistProfiles = map[string]JournalistProfile{
	"feature": {
		Type:              "feature",
		Name:              "Feature Writer",
		SystemPrompt:      `You are an engaging feature writer for a leading newspaper, who are assigned to write for a company called "Kumpan". Your writing is warm, compelling, and draws readers in with strong openings. You have a talent for finding the human interest angle in any story and making it relatable to the world. One thing that really sets your writing apart is your way of making absolutely amazing storys. The smallest of grey every day life matter can be the most heart-felt amazing story. Or extremely dramatic. You have a way of finding sources to back up your stories that is hard to believe (greatly exagregated to a comedic point), but that is what makes the reader engaged. But remember, never let anyone know it's made up!`,
		StyleInstructions: `Write 250-300 words. Use active voice and engaging tone. Create a strong lead paragraph that hooks the reader. Focus on the small human element and why this matters to the common man. Use your famous storytelling tecniques (comedy, exagregated comparisons, made up statistics, etc). Use conversational language while maintaining professionalism. Always write in the Swedish language.`,
		MaxWords:          300,
		TemplateFormat:    "hero",
	},
	"interview": {
		Type:              "interview",
		Name:              "Interview Specialist",
		SystemPrompt:      `You are conducting written interviews for an up-and-coming company newsletter. The company is called "Kumpan". You excel at transforming submissions into Q&A format conversations. Even if the original submission don't contain much information. You're a master at making up questions to make it seem like you conducted an interview, even if you didn't. Only make up questions, never the answer. Though you may rewrite or rephrase it to suit the question. Try and make it funny. It can be funny with an ironic style, a dry style, a dead-pan style. Be creative. If there is material to use, then use it. Try and sense the interviee style and tone when writing their answer.  You ask follow-up questions that reveal interesting details.`,
		StyleInstructions: `Format as Q&A with as many questions needed dependent on the input. Keep responses natural and conversational. Each question should build on the previous one. Total length 150-200 words. Make questions specific and engaging, not generic. If you have little to no information from the input, make your questions longer to fill out the space. Always write in the Swedish language. Always end with some sort of "tack för pratstunden" - but make it fit the tone of the interview.`,
		MaxWords:          200,
		TemplateFormat:    "interview",
	},
	"sports": {
		Type:              "sports",
		Name:              "Sports Reporter",
		SystemPrompt:      `You are an enthusiastic sports reporter with great energy and a sense of humor. You cover team activities, competitions, workplace fitness challenges, and athletic achievements with excitement and sports terminology. You make everyone feel included, whether they're athletes or not.`,
		StyleInstructions: `Write with high energy and enthusiasm. Use appropriate sports terminology and metaphors. Include specific details about achievements or events. Keep it inclusive for non-athletes too. 150-200 words with dynamic, energetic tone.`,
		MaxWords:          200,
		TemplateFormat:    "column",
	},
	"general": {
		Type:              "general",
		Name:              "Staff Reporter",
		SystemPrompt:      `You are a friendly staff reporter covering general company news and updates on the web developer agency called "Kumpan". Your tone is professional but warm, accessible to all team members regardless of their role or department. You focus on clarity and making information useful for everyone. Still, write it as if it were to be published in a newspaper.`,
		StyleInstructions: `Write clearly and concisely with professional but friendly tone. Focus on the key information and why it matters to team members. Use simple, direct language. Avoid jargon. 100-150 words maximum. Always write in the Swedish language.`,
		MaxWords:          150,
		TemplateFormat:    "column",
	},
	"body_mind": {
		Type:              "body_mind",
		Name:              "Body and Mind Columnist",
		SystemPrompt:      `You are a desillusionized advice columnist specializing in body and mind wellness, on a web developer company called "Kumpan". You handle anonymous questions about life, relationships, physical concerns, mental health, sexuality, and personal struggles with lack luster. You respond to people seeking guidance on intimate, sometimes vulnerable topics. Your approach combines practical advice with philosophical insight, drawing from psychology, science, and human experience. You're not mean, but snarky. In the end, you know what's best for everyone else.`,
		StyleInstructions: `You are tired, you've heard it all before. You just want to get this answer in as few words as possible. Be true, but very short. If there's no clear solution, try and make up a "word of wisdom" that is really hard to interpret and understand. End with an encouraging sign-off. Create a witty, ironic and relevant pseudonym for the letter writer that relates to their situation. Keep responses 150 - 200 words, hardly conversational yet wise. Always answer in the Swedish language.`,
		MaxWords:          200,
		TemplateFormat:    "advice",
	},
}

// GetJournalistProfile returns the profile for a given journalist type
func GetJournalistProfile(journalistType string) (*JournalistProfile, error) {
	if profile, exists := JournalistProfiles[journalistType]; exists {
		return &profile, nil
	}
	return nil, fmt.Errorf("journalist type '%s' not found", journalistType)
}

// GetAvailableJournalistTypes returns a list of all available journalist types
func GetAvailableJournalistTypes() []string {
	types := make([]string, 0, len(JournalistProfiles))
	for journalistType := range JournalistProfiles {
		types = append(types, journalistType)
	}
	return types
}

// ValidateJournalistType checks if a journalist type is valid
func ValidateJournalistType(journalistType string) bool {
	_, exists := JournalistProfiles[journalistType]
	return exists
}

// BuildPrompt creates a complete prompt for AI processing (legacy - use BuildJSONPrompt instead)
func BuildPrompt(submission, journalistType string) (string, error) {
	profile, err := GetJournalistProfile(journalistType)
	if err != nil {
		return "", err
	}

	prompt := fmt.Sprintf(`%s

%s

Original submission to transform:
"%s"

Return ONLY the processed article content. Do not include any preamble, explanation, or meta-commentary. Just the article text itself.`,
		profile.SystemPrompt,
		profile.StyleInstructions,
		submission,
	)

	return prompt, nil
}

// BuildJSONPrompt creates a complete prompt for AI processing with structured JSON output
func BuildJSONPrompt(submission, authorName, authorDepartment, journalistType string) (string, error) {
	profile, err := GetJournalistProfile(journalistType)
	if err != nil {
		return "", err
	}

	// Get journalist-specific JSON structure requirements
	jsonStructure := getJSONStructureForJournalist(journalistType)
	requiredFields := GetRequiredJSONFields(journalistType)

	prompt := fmt.Sprintf(`%s

%s

Author Information:
- Name: %s
- Department: %s

Original submission to transform:
"%s"

CRITICAL: You MUST return your response as valid JSON in the following structure:
%s

Required fields: %v

Example JSON format:
{
  "headline": "Your catchy headline here",
  "lead": "Your engaging opening paragraph...",
  "body": "Main content of the article...",
  "byline": "%s"
}

Return ONLY valid JSON. No preamble, explanation, or additional text. The JSON must be parseable and contain all required fields.`,
		profile.SystemPrompt,
		profile.StyleInstructions,
		authorName,
		authorDepartment,
		submission,
		jsonStructure,
		requiredFields,
		profile.Name,
	)

	return prompt, nil
}

// GetRequiredJSONFields returns the required JSON fields for a journalist type
func GetRequiredJSONFields(journalistType string) []string {
	switch journalistType {
	case "feature":
		return []string{"headline", "lead", "body", "byline"}
	case "interview":
		return []string{"headline", "introduction", "questions", "byline"}
	case "general":
		return []string{"headline", "body", "byline"}
	case "body_mind":
		return []string{"headline", "response", "signoff", "byline"}
	default:
		return []string{"headline", "body", "byline"} // Default structure
	}
}

// ValidateJSONResponse validates that the JSON response contains required fields
func ValidateJSONResponse(jsonResponse, journalistType string) error {
	// Parse JSON
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(jsonResponse), &parsed); err != nil {
		return fmt.Errorf("invalid JSON format: %w", err)
	}

	// Check required fields
	requiredFields := GetRequiredJSONFields(journalistType)
	for _, field := range requiredFields {
		if _, exists := parsed[field]; !exists {
			return fmt.Errorf("missing required field: %s", field)
		}

		// Ensure field is not empty
		if str, ok := parsed[field].(string); ok && str == "" {
			return fmt.Errorf("required field %s cannot be empty", field)
		}
	}

	return nil
}

// getJSONStructureForJournalist returns the JSON structure description for a journalist type
func getJSONStructureForJournalist(journalistType string) string {
	switch journalistType {
	case "feature":
		return `{
  "headline": "Catchy, engaging headline",
  "lead": "Strong opening paragraph that hooks the reader",
  "body": "Main article content with human interest angle",
  "byline": "Erik Lindqvist, Feature Writer"
}`
	case "interview":
		return `{
  "headline": "Interview-style headline",
  "introduction": "Brief introduction to the interview",
  "questions": [
    {"q": "Question text", "a": "Answer text"},
    {"q": "Follow-up question", "a": "Response"}
  ],
  "byline": "Anna Bergström, Interview Specialist"
}`
	case "general":
		return `{
  "headline": "Clear, informative headline",
  "body": "Straightforward news content",
  "byline": "Lars Petersson, Staff Reporter"
}`
	case "body_mind":
		return `{
  "headline": "Question or topic headline",
  "response": "Advice response content", 
  "signoff": "Snarky but encouraging closing",
  "byline": "Dr. Astrid Holmberg, Body & Mind Columnist"
}`
	default:
		return `{
  "headline": "Article headline",
  "body": "Article content",
  "byline": "Staff Writer"
}`
	}
}
