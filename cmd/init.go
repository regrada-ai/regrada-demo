package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var initCmd = &cobra.Command{
	Use:   "init [path]",
	Short: "Initialize a new regrada project",
	Long: `Initialize a new regrada project in the specified directory.
If no path is provided, the current directory will be used.

Example:
  regrada init
  regrada init ./my-project
  regrada init --yes  # Use defaults without prompts`,
	Args: cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		path := "."
		if len(args) > 0 {
			path = args[0]
		}

		force, _ := cmd.Flags().GetBool("force")
		configFile, _ := cmd.Flags().GetString("config")
		useDefaults, _ := cmd.Flags().GetBool("yes")

		runInit(path, force, configFile, useDefaults)
	},
}

func init() {
	rootCmd.AddCommand(initCmd)

	initCmd.Flags().BoolP("force", "f", false, "Force initialization even if project exists")
	initCmd.Flags().StringP("config", "c", "", "Specify a custom config file name")
	initCmd.Flags().BoolP("yes", "y", false, "Use default values without interactive prompts")
}

// RegradaConfig represents the complete configuration for a regrada project
type RegradaConfig struct {
	Version  string         `yaml:"version"`
	Project  string         `yaml:"project"`
	Env      string         `yaml:"env"`
	Provider ProviderConfig `yaml:"provider"`
	Capture  CaptureConfig  `yaml:"capture"`
	Evals    EvalsConfig    `yaml:"evals"`
	Gate     GateConfig     `yaml:"gate"`
	Output   OutputConfig   `yaml:"output"`
}

type ProviderConfig struct {
	Type    string `yaml:"type"`
	BaseURL string `yaml:"base_url,omitempty"`
	Model   string `yaml:"model,omitempty"`
}

type CaptureConfig struct {
	Requests  bool `yaml:"requests"`
	Responses bool `yaml:"responses"`
	Traces    bool `yaml:"traces"`
	Latency   bool `yaml:"latency"`
}

type EvalsConfig struct {
	Path       string   `yaml:"path"`
	Types      []string `yaml:"types"`
	Timeout    string   `yaml:"timeout"`
	Concurrent int      `yaml:"concurrent"`
}

type GateConfig struct {
	Enabled   bool    `yaml:"enabled"`
	Threshold float64 `yaml:"threshold"`
	FailOn    string  `yaml:"fail_on"`
}

type OutputConfig struct {
	Format  string `yaml:"format"`
	Verbose bool   `yaml:"verbose"`
}

func runInit(path string, force bool, configFile string, useDefaults bool) {
	// Styles
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("212")).
		MarginBottom(1)

	subtitleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("241")).
		MarginBottom(1)

	fmt.Println()
	fmt.Println(titleStyle.Render("Regrada Project Setup"))
	fmt.Println(subtitleStyle.Render("CI for AI - Detect LLM regressions before production"))

	// Ensure the path exists
	absPath, err := filepath.Abs(path)
	if err != nil {
		absPath = path
	}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		fmt.Printf("Creating directory: %s\n\n", absPath)
		if err := os.MkdirAll(path, 0755); err != nil {
			fmt.Printf("Error creating directory: %v\n", err)
			os.Exit(1)
		}
	}

	// Determine config file name
	if configFile == "" {
		configFile = ".regrada.yaml"
	}
	configPath := filepath.Join(path, configFile)

	// Prevent overwrite unless --force
	if _, err := os.Stat(configPath); err == nil && !force {
		fmt.Printf("Warning: %s already exists. Use --force to overwrite.\n", configFile)
		os.Exit(1)
	}

	var config RegradaConfig

	if useDefaults {
		config = getDefaultConfig(filepath.Base(absPath))
		fmt.Println("Using default configuration...")
	} else {
		var err error
		config, err = runInteractiveSetup(filepath.Base(absPath))
		if err != nil {
			if err == huh.ErrUserAborted {
				fmt.Println("\nSetup cancelled.")
				os.Exit(0)
			}
			fmt.Printf("Error during setup: %v\n", err)
			os.Exit(1)
		}
	}

	// Marshal to YAML
	data, err := yaml.Marshal(&config)
	if err != nil {
		fmt.Printf("Failed to generate config: %v\n", err)
		os.Exit(1)
	}

	// Add header comment
	header := `# Regrada Configuration
# CI for AI systems - detect behavioral regressions in LLM-powered apps
# Documentation: https://regrada.com/docs
`
	fullContent := header + string(data)

	// Write config file
	if err := os.WriteFile(configPath, []byte(fullContent), 0644); err != nil {
		fmt.Printf("Failed to write config: %v\n", err)
		os.Exit(1)
	}

	// Create evals directory if it doesn't exist
	evalsDir := filepath.Join(path, config.Evals.Path)
	if _, err := os.Stat(evalsDir); os.IsNotExist(err) {
		if err := os.MkdirAll(evalsDir, 0755); err != nil {
			fmt.Printf("Warning: Could not create evals directory: %v\n", err)
		} else {
			createExampleEval(evalsDir)
		}
	}

	// Success message
	successStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("42")).
		MarginTop(1)

	fmt.Println(successStyle.Render("Project initialized!"))
	fmt.Println()
	fmt.Printf("  Created: %s\n", configFile)
	fmt.Printf("  Evals:   %s/\n", config.Evals.Path)
	fmt.Printf("  Prompts: %s/prompts/\n", config.Evals.Path)
	fmt.Println()
	fmt.Println("  Next steps:")
	
	// Provider-aware API key instructions
	switch config.Provider.Type {
	case "openai":
		fmt.Println("  1. Set your API key: export OPENAI_API_KEY=sk-...")
	case "anthropic":
		fmt.Println("  1. Set your API key: export ANTHROPIC_API_KEY=sk-ant-...")
	case "azure-openai":
		fmt.Println("  1. Set your API key: export AZURE_OPENAI_API_KEY=...")
	case "custom":
		fmt.Println("  1. Set your API key (if needed): export API_KEY=...")
	default:
		fmt.Println("  1. Set your API key: export API_KEY=...")
	}
	
	fmt.Println("  2. Add test cases to the evals/ directory")
	fmt.Println("  3. Run your first evaluation: regrada trace")
	fmt.Println()
}

func getDefaultConfig(projectName string) RegradaConfig {
	return RegradaConfig{
		Version: "1",
		Project: projectName,
		Env:     "local",
		Provider: ProviderConfig{
			Type:  "openai",
			Model: "gpt-4o",
		},
		Capture: CaptureConfig{
			Requests:  true,
			Responses: true,
			Traces:    true,
			Latency:   true,
		},
		Evals: EvalsConfig{
			Path:       "evals",
			Types:      []string{"semantic", "exact"},
			Timeout:    "30s",
			Concurrent: 5,
		},
		Gate: GateConfig{
			Enabled:   true,
			Threshold: 0.8,
			FailOn:    "regression",
		},
		Output: OutputConfig{
			Format:  "text",
			Verbose: false,
		},
	}
}

func runInteractiveSetup(defaultProjectName string) (RegradaConfig, error) {
	var (
		projectName  string
		env          string
		provider     string
		model        string
		baseURL      string
		captureOpts  []string
		evalsPath    string
		evalTypes    []string
		gateEnabled  bool
		threshold    string
		failOn       string
		outputFormat string
	)

	// Create custom theme
	theme := huh.ThemeCharm()

	// Step 1: Project basics
	form1 := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Project name").
				Description("Name for your Regrada project").
				Value(&projectName).
				Placeholder(defaultProjectName),

			huh.NewSelect[string]().
				Title("Environment").
				Description("Where will this configuration be used?").
				Options(
					huh.NewOption("Local development", "local"),
					huh.NewOption("Development server", "development"),
					huh.NewOption("Staging", "staging"),
					huh.NewOption("Production", "production"),
					huh.NewOption("CI/CD pipeline", "ci"),
				).
				Value(&env),
		).Title("Project Basics"),
	).WithTheme(theme)

	if err := form1.Run(); err != nil {
		return RegradaConfig{}, err
	}

	if projectName == "" {
		projectName = defaultProjectName
	}

	// Step 2: Provider configuration
	form2 := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("LLM Provider").
				Description("Which provider powers your AI application?").
				Options(
					huh.NewOption("OpenAI", "openai"),
					huh.NewOption("Anthropic", "anthropic"),
					huh.NewOption("Azure OpenAI", "azure-openai"),
					huh.NewOption("Custom / Self-hosted", "custom"),
				).
				Value(&provider),
		).Title("LLM Provider"),
	).WithTheme(theme)

	if err := form2.Run(); err != nil {
		return RegradaConfig{}, err
	}

	// Provider-specific configuration
	switch provider {
	case "openai":
		form := huh.NewForm(
			huh.NewGroup(
				huh.NewInput().
					Title("Model").
					Description("OpenAI model to use for evaluations").
					Value(&model).
					Placeholder("gpt-4o"),
			),
		).WithTheme(theme)
		if err := form.Run(); err != nil {
			return RegradaConfig{}, err
		}
		if model == "" {
			model = "gpt-4o"
		}

	case "anthropic":
		form := huh.NewForm(
			huh.NewGroup(
				huh.NewInput().
					Title("Model").
					Description("Anthropic model to use for evaluations").
					Value(&model).
					Placeholder("claude-sonnet-4-20250514"),
			),
		).WithTheme(theme)
		if err := form.Run(); err != nil {
			return RegradaConfig{}, err
		}
		if model == "" {
			model = "claude-sonnet-4-20250514"
		}

	case "azure-openai":
		form := huh.NewForm(
			huh.NewGroup(
				huh.NewInput().
					Title("Azure endpoint URL").
					Description("Your Azure OpenAI endpoint").
					Value(&baseURL).
					Placeholder("https://your-resource.openai.azure.com"),

				huh.NewInput().
					Title("Deployment name").
					Description("Your Azure OpenAI deployment").
					Value(&model).
					Placeholder("gpt-4o"),
			),
		).WithTheme(theme)
		if err := form.Run(); err != nil {
			return RegradaConfig{}, err
		}
		if model == "" {
			model = "gpt-4o"
		}

	case "custom":
		form := huh.NewForm(
			huh.NewGroup(
				huh.NewInput().
					Title("API base URL").
					Description("Base URL for your custom LLM endpoint").
					Value(&baseURL).
					Placeholder("http://localhost:8000/v1"),

				huh.NewInput().
					Title("Model name").
					Description("Model identifier (optional)").
					Value(&model),
			),
		).WithTheme(theme)
		if err := form.Run(); err != nil {
			return RegradaConfig{}, err
		}
		if baseURL == "" {
			baseURL = "http://localhost:8000/v1"
		}
	}

	// Step 3: Capture settings
	form3 := huh.NewForm(
		huh.NewGroup(
			huh.NewMultiSelect[string]().
				Title("Data capture").
				Description("What data should Regrada capture during tests?").
				Options(
					huh.NewOption("Requests", "requests").Selected(true),
					huh.NewOption("Responses", "responses").Selected(true),
					huh.NewOption("Traces", "traces").Selected(true),
					huh.NewOption("Latency metrics", "latency").Selected(true),
				).
				Value(&captureOpts),
		).Title("Data Capture"),
	).WithTheme(theme)

	if err := form3.Run(); err != nil {
		return RegradaConfig{}, err
	}

	// Step 4: Evaluation settings
	form4 := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Evals directory").
				Description("Where to store test case files").
				Value(&evalsPath).
				Placeholder("evals"),

			huh.NewMultiSelect[string]().
				Title("Evaluation methods").
				Description("How should Regrada evaluate LLM outputs?").
				Options(
					huh.NewOption("Semantic similarity", "semantic").Selected(true),
					huh.NewOption("Exact match", "exact").Selected(true),
					huh.NewOption("LLM-as-judge", "llm-judge"),
					huh.NewOption("Custom evaluators", "custom"),
				).
				Value(&evalTypes),
		).Title("Evaluation Settings"),
	).WithTheme(theme)

	if err := form4.Run(); err != nil {
		return RegradaConfig{}, err
	}

	if evalsPath == "" {
		evalsPath = "evals"
	}
	if len(evalTypes) == 0 {
		evalTypes = []string{"semantic", "exact"}
	}

	// Step 5: Quality gate
	form5 := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Enable quality gate?").
				Description("Fail CI when regressions are detected").
				Value(&gateEnabled).
				Affirmative("Yes").
				Negative("No"),
		).Title("Quality Gate"),
	).WithTheme(theme)

	if err := form5.Run(); err != nil {
		return RegradaConfig{}, err
	}

	thresholdFloat := 0.8
	if gateEnabled {
		form := huh.NewForm(
			huh.NewGroup(
				huh.NewInput().
					Title("Pass threshold").
					Description("Minimum score to pass (0.0-1.0)").
					Value(&threshold).
					Placeholder("0.8"),

				huh.NewSelect[string]().
					Title("Fail condition").
					Description("When should the quality gate fail?").
					Options(
						huh.NewOption("On regression from baseline", "regression"),
						huh.NewOption("On any test failure", "any-failure"),
						huh.NewOption("When below threshold", "threshold"),
					).
					Value(&failOn),
			),
		).WithTheme(theme)
		if err := form.Run(); err != nil {
			return RegradaConfig{}, err
		}
		if threshold != "" {
			fmt.Sscanf(threshold, "%f", &thresholdFloat)
		}
		if failOn == "" {
			failOn = "regression"
		}
	} else {
		failOn = "regression"
	}

	// Step 6: Output settings
	form6 := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Output format").
				Description("How should results be displayed?").
				Options(
					huh.NewOption("Text (human readable)", "text"),
					huh.NewOption("JSON", "json"),
					huh.NewOption("GitHub Actions", "github"),
					huh.NewOption("JUnit XML", "junit"),
				).
				Value(&outputFormat),
		).Title("Output Settings"),
	).WithTheme(theme)

	if err := form6.Run(); err != nil {
		return RegradaConfig{}, err
	}

	// Build capture map
	captureMap := make(map[string]bool)
	for _, c := range captureOpts {
		captureMap[c] = true
	}

	return RegradaConfig{
		Version: "1",
		Project: projectName,
		Env:     env,
		Provider: ProviderConfig{
			Type:    provider,
			BaseURL: baseURL,
			Model:   model,
		},
		Capture: CaptureConfig{
			Requests:  captureMap["requests"],
			Responses: captureMap["responses"],
			Traces:    captureMap["traces"],
			Latency:   captureMap["latency"],
		},
		Evals: EvalsConfig{
			Path:       evalsPath,
			Types:      evalTypes,
			Timeout:    "30s",
			Concurrent: 5,
		},
		Gate: GateConfig{
			Enabled:   gateEnabled,
			Threshold: thresholdFloat,
			FailOn:    failOn,
		},
		Output: OutputConfig{
			Format:  outputFormat,
			Verbose: false,
		},
	}, nil
}

func createExampleEval(evalsDir string) {
	// Create prompts directory
	promptsDir := filepath.Join(evalsDir, "prompts")
	if err := os.MkdirAll(promptsDir, 0755); err != nil {
		fmt.Printf("Warning: Could not create prompts directory: %v\n", err)
		return
	}

	// Create example prompt files
	refundPrompt := `You are a customer service agent. A customer wants to request a refund for order #12345.

Process the refund request following company policy:
1. Verify the order exists
2. Check if it's within the refund window (30 days)
3. Create a refund if eligible

Customer message: "I'd like a refund for my recent order, it arrived damaged."
`
	if err := os.WriteFile(filepath.Join(promptsDir, "refund.txt"), []byte(refundPrompt), 0644); err != nil {
		fmt.Printf("Warning: Could not create refund prompt: %v\n", err)
	}

	searchPrompt := `You are a helpful assistant with access to a knowledge base.

Answer the user's question using only information from the retrieved documents.
If the answer isn't in the documents, say you don't have that information.

User question: "What is your return policy?"
`
	if err := os.WriteFile(filepath.Join(promptsDir, "search.txt"), []byte(searchPrompt), 0644); err != nil {
		fmt.Printf("Warning: Could not create search prompt: %v\n", err)
	}

	// Create example test suite
	exampleContent := `# Regrada Test Suite
# Run on every PR to detect behavioral regressions

name: customer-service-agent
description: Tests for customer service AI agent

tests:
  # Test tool calling behavior
  - name: refund_request
    prompt: prompts/refund.txt
    checks:
      - schema_valid
      - "tool_called:refund.create"
      - no_hallucination

  # Test RAG grounding
  - name: policy_question
    prompt: prompts/search.txt
    checks:
      - grounded_in_retrieval
      - no_fabrication
      - "tone:professional"

  # Test with inline prompt
  - name: greeting_response
    prompt: |
      You are a friendly assistant.
      User: Hello!
    checks:
      - "response_time:<2s"
      - "sentiment:positive"
      - "length:10-500"

  # Test edge cases
  - name: out_of_scope
    prompt: |
      You are a customer service agent for an e-commerce store.
      User: What's the meaning of life?
    checks:
      - stays_on_topic
      - no_tool_called
`

	examplePath := filepath.Join(evalsDir, "tests.yaml")
	if err := os.WriteFile(examplePath, []byte(exampleContent), 0644); err != nil {
		fmt.Printf("Warning: Could not create example test file: %v\n", err)
	}
}
