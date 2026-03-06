// Example: structured output using generics.
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/sadhakbj/aisdk-go"
	_ "github.com/sadhakbj/aisdk-go/examples/config"
)

// Define your output types as regular Go structs.
// The SDK generates JSON schema automatically from struct tags.

type Recipe struct {
	Name        string   `json:"name"`
	Cuisine     string   `json:"cuisine"`
	PrepTime    string   `json:"prep_time"`
	Ingredients []string `json:"ingredients"`
	Steps       []string `json:"steps"`
	Difficulty  string   `json:"difficulty"`
}

type CodeReview struct {
	Score      int      `json:"score"`
	Issues     []Issue  `json:"issues"`
	Strengths  []string `json:"strengths"`
	Suggestion string   `json:"suggestion"`
}

type Issue struct {
	Line        int    `json:"line"`
	Severity    string `json:"severity"`
	Description string `json:"description"`
}

func main() {
	ctx := context.Background()

	// --- Structured output with GenerateObject[T] ---

	fmt.Println("=== Recipe Generation ===")
	recipe, err := aisdk.GenerateObject[Recipe](ctx, aisdk.ObjectParams{
		Model:  "smart",
		Prompt: "Give me a simple pasta carbonara recipe.",
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Name: %s\n", recipe.Object.Name)
	fmt.Printf("Cuisine: %s\n", recipe.Object.Cuisine)
	fmt.Printf("Prep Time: %s\n", recipe.Object.PrepTime)
	fmt.Printf("Difficulty: %s\n", recipe.Object.Difficulty)
	fmt.Printf("Ingredients (%d):\n", len(recipe.Object.Ingredients))
	for _, ing := range recipe.Object.Ingredients {
		fmt.Printf("  - %s\n", ing)
	}

	// --- Structured output with PromptObject[T] from an agent ---

	fmt.Println("\n=== Code Review ===")
	reviewer := aisdk.Quick(aisdk.AgentConfig{
		Model:        "smart",
		Instructions: "You are a senior Go developer reviewing code.",
	})

	code := `
func getData(url string) ([]byte, error) {
    resp, _ := http.Get(url)
    body, _ := ioutil.ReadAll(resp.Body)
    return body, nil
}
`
	review, err := aisdk.PromptObject[CodeReview](ctx, reviewer, "Review this Go code:\n"+code)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Score: %d/100\n", review.Object.Score)
	fmt.Printf("Issues (%d):\n", len(review.Object.Issues))
	for _, issue := range review.Object.Issues {
		fmt.Printf("  [%s] Line %d: %s\n", issue.Severity, issue.Line, issue.Description)
	}
	fmt.Printf("Strengths:\n")
	for _, s := range review.Object.Strengths {
		fmt.Printf("  + %s\n", s)
	}
	fmt.Printf("Suggestion: %s\n", review.Object.Suggestion)
}
