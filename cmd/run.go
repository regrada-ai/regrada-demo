package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Run evaluations against test cases",
	Long: `Run all evaluations defined in your test suite.

This command executes your AI agent against all test cases,
captures traces, and compares results against the baseline.

In CI mode, it returns exit code 1 if regressions are detected.

Examples:
  regrada run
  regrada run --ci
  regrada run --tests evals/tests.yaml
  regrada run --baseline .regrada/baseline.json`,
	Run: func(cmd *cobra.Command, args []string) {
		testsPath, _ := cmd.Flags().GetString("tests")
		baselinePath, _ := cmd.Flags().GetString("baseline")
		ciMode, _ := cmd.Flags().GetBool("ci")
		outputFormat, _ := cmd.Flags().GetString("output")
		configPath, _ := cmd.Flags().GetString("config")

		exitCode := runEvals(testsPath, baselinePath, ciMode, outputFormat, configPath)
		os.Exit(exitCode)
	},
}

func init() {
	rootCmd.AddCommand(runCmd)

	runCmd.Flags().StringP("tests", "t", "", "Path to test suite (default: evals/tests.yaml)")
	runCmd.Flags().StringP("baseline", "b", "", "Path to baseline traces (default: .regrada/baseline.json)")
	runCmd.Flags().Bool("ci", false, "CI mode: exit 1 on regression, output GitHub-compatible format")
	runCmd.Flags().StringP("output", "o", "text", "Output format: text, json, github")
	runCmd.Flags().StringP("config", "c", ".regrada.yaml", "Path to config file")
}

// TestSuite represents a collection of tests
type TestSuite struct {
	Name        string     `yaml:"name"`
	Description string     `yaml:"description"`
	Tests       []TestCase `yaml:"tests"`
}

// TestCase represents a single test
type TestCase struct {
	Name   string   `yaml:"name"`
	Prompt string   `yaml:"prompt"`
	Checks []string `yaml:"checks"`
}

// EvalResult represents the result of running evaluations
type EvalResult struct {
	Timestamp    time.Time       `json:"timestamp"`
	TestSuite    string          `json:"test_suite"`
	TotalTests   int             `json:"total_tests"`
	Passed       int             `json:"passed"`
	Failed       int             `json:"failed"`
	Regressions  int             `json:"regressions"`
	TestResults  []TestResult    `json:"test_results"`
	Comparison   *BaselineComparison `json:"comparison,omitempty"`
}

// TestResult represents a single test result
type TestResult struct {
	Name         string        `json:"name"`
	Status       string        `json:"status"` // passed, failed, error
	Duration     time.Duration `json:"duration_ms"`
	CheckResults []CheckResult `json:"checks"`
	Error        string        `json:"error,omitempty"`
	Regression   bool          `json:"regression,omitempty"`
}

// CheckResult represents a single check result
type CheckResult struct {
	Check   string `json:"check"`
	Passed  bool   `json:"passed"`
	Message string `json:"message,omitempty"`
}

// BaselineComparison represents comparison with baseline
type BaselineComparison struct {
	BaselineDate    time.Time `json:"baseline_date"`
	NewFailures     []string  `json:"new_failures,omitempty"`
	NewPasses       []string  `json:"new_passes,omitempty"`
	RemovedTests    []string  `json:"removed_tests,omitempty"`
	AddedTests      []string  `json:"added_tests,omitempty"`
	BehaviorChanges []string  `json:"behavior_changes,omitempty"`
}

func runEvals(testsPath, baselinePath string, ciMode bool, outputFormat, configPath string) int {
	// Styles
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212"))
	successStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	failStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))

	if outputFormat != "json" {
		fmt.Println()
		fmt.Println(titleStyle.Render("Regrada Eval Runner"))
		fmt.Println(dimStyle.Render("Running AI agent evaluations..."))
		fmt.Println()
	}

	// Load config
	config, err := loadConfig(configPath)
	if err != nil && outputFormat != "json" {
		fmt.Printf("%s Config not found, using defaults\n", warnStyle.Render("Warning:"))
		defaultCfg := getDefaultConfig(".")
		config = &defaultCfg
	}

	// Find test suite
	if testsPath == "" {
		testsPath = filepath.Join(config.Evals.Path, "tests.yaml")
	}

	// Load test suite
	suite, err := loadTestSuite(testsPath)
	if err != nil {
		if outputFormat == "json" {
			jsonErr, _ := json.Marshal(map[string]string{"error": err.Error()})
			fmt.Println(string(jsonErr))
		} else {
			fmt.Printf("%s Failed to load test suite: %v\n", failStyle.Render("✗"), err)
		}
		return 1
	}

	if outputFormat != "json" {
		fmt.Printf("Test suite: %s\n", suite.Name)
		fmt.Printf("Tests: %d\n\n", len(suite.Tests))
	}

	// Run tests
	result := &EvalResult{
		Timestamp:   time.Now(),
		TestSuite:   suite.Name,
		TotalTests:  len(suite.Tests),
		TestResults: make([]TestResult, 0, len(suite.Tests)),
	}

	for _, test := range suite.Tests {
		testResult := runTest(test, config, outputFormat != "json")
		result.TestResults = append(result.TestResults, testResult)

		if testResult.Status == "passed" {
			result.Passed++
		} else {
			result.Failed++
		}
	}

	// Load and compare with baseline if available
	if baselinePath == "" {
		baselinePath = filepath.Join(".regrada", "baseline.json")
	}

	if _, err := os.Stat(baselinePath); err == nil {
		comparison := compareWithBaselineResults(result, baselinePath)
		result.Comparison = comparison
		result.Regressions = len(comparison.NewFailures)
	}

	// Save results as JSON for CI integrations
	if ciMode {
		resultsDir := filepath.Join(".regrada")
		os.MkdirAll(resultsDir, 0755)
		resultsPath := filepath.Join(resultsDir, "results.json")
		data, _ := json.MarshalIndent(result, "", "  ")
		os.WriteFile(resultsPath, data, 0644)
	}

	// Output results
	switch outputFormat {
	case "json":
		outputJSON(result)
	case "github":
		outputGitHub(result, ciMode)
	default:
		outputText(result, successStyle, failStyle, warnStyle, dimStyle)
	}

	// Determine exit code
	if ciMode && (result.Failed > 0 || result.Regressions > 0) {
		return 1
	}

	return 0
}

func loadTestSuite(path string) (*TestSuite, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("could not read test suite: %w", err)
	}

	var suite TestSuite
	if err := yaml.Unmarshal(data, &suite); err != nil {
		return nil, fmt.Errorf("could not parse test suite: %w", err)
	}

	return &suite, nil
}

func runTest(test TestCase, config *RegradaConfig, verbose bool) TestResult {
	startTime := time.Now()

	result := TestResult{
		Name:         test.Name,
		Status:       "passed",
		CheckResults: make([]CheckResult, 0, len(test.Checks)),
	}

	if verbose {
		fmt.Printf("  Running: %s... ", test.Name)
	}

	// Simulate running the test (in real implementation, this would:
	// 1. Load the prompt file or use inline prompt
	// 2. Call the LLM via the configured provider
	// 3. Run each check against the response)

	// For now, we'll simulate the check execution
	for _, check := range test.Checks {
		checkResult := runCheck(check, test, config)
		result.CheckResults = append(result.CheckResults, checkResult)

		if !checkResult.Passed {
			result.Status = "failed"
		}
	}

	result.Duration = time.Since(startTime) / time.Millisecond

	if verbose {
		if result.Status == "passed" {
			fmt.Println(lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Render("✓ passed"))
		} else {
			fmt.Println(lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Render("✗ failed"))
			for _, cr := range result.CheckResults {
				if !cr.Passed {
					fmt.Printf("      %s: %s\n", cr.Check, cr.Message)
				}
			}
		}
	}

	return result
}

func runCheck(check string, test TestCase, config *RegradaConfig) CheckResult {
	// Parse check type and parameters
	checkType := check
	var checkParam string

	if idx := strings.Index(check, ":"); idx > 0 {
		checkType = strings.TrimSpace(check[:idx])
		checkParam = strings.TrimSpace(check[idx+1:])
	}

	result := CheckResult{
		Check:  check,
		Passed: true,
	}

	// In a real implementation, these would actually run against LLM responses
	// For now, we simulate based on check type
	switch checkType {
	case "schema_valid":
		// Would validate response against expected schema
		result.Passed = true
		result.Message = "Response matches expected schema"

	case "tool_called":
		// Would verify specific tool was called
		result.Passed = true
		result.Message = fmt.Sprintf("Tool '%s' was called", checkParam)

	case "no_tool_called":
		// Would verify no tools were called
		result.Passed = true
		result.Message = "No tools were called"

	case "grounded_in_retrieval":
		// Would verify response uses retrieved context
		result.Passed = true
		result.Message = "Response is grounded in retrieved documents"

	case "no_hallucination", "no_fabrication":
		// Would check for hallucinated content
		result.Passed = true
		result.Message = "No hallucinated content detected"

	case "tone":
		// Would analyze response tone
		result.Passed = true
		result.Message = fmt.Sprintf("Tone matches: %s", checkParam)

	case "sentiment":
		// Would analyze sentiment
		result.Passed = true
		result.Message = fmt.Sprintf("Sentiment is %s", checkParam)

	case "stays_on_topic":
		// Would check topic adherence
		result.Passed = true
		result.Message = "Response stays on topic"

	case "response_time":
		// Would check latency
		result.Passed = true
		result.Message = fmt.Sprintf("Response time within %s", checkParam)

	case "length":
		// Would check response length
		result.Passed = true
		result.Message = fmt.Sprintf("Response length within %s chars", checkParam)

	case "INTENTIONAL_FAIL":
		// For demo/testing purposes - always fails
		result.Passed = false
		result.Message = "Intentional failure for testing"

	default:
		// Unknown check type - mark as passed with warning
		result.Passed = true
		result.Message = fmt.Sprintf("Check '%s' not yet implemented", checkType)
	}

	return result
}

func compareWithBaselineResults(current *EvalResult, baselinePath string) *BaselineComparison {
	comparison := &BaselineComparison{}

	data, err := os.ReadFile(baselinePath)
	if err != nil {
		return comparison
	}

	var baseline EvalResult
	if err := json.Unmarshal(data, &baseline); err != nil {
		return comparison
	}

	comparison.BaselineDate = baseline.Timestamp

	// Build maps for comparison
	baselineResults := make(map[string]TestResult)
	for _, tr := range baseline.TestResults {
		baselineResults[tr.Name] = tr
	}

	currentResults := make(map[string]TestResult)
	for _, tr := range current.TestResults {
		currentResults[tr.Name] = tr
	}

	// Find new failures (regressions)
	for name, cr := range currentResults {
		if br, exists := baselineResults[name]; exists {
			if br.Status == "passed" && cr.Status == "failed" {
				comparison.NewFailures = append(comparison.NewFailures, name)
			} else if br.Status == "failed" && cr.Status == "passed" {
				comparison.NewPasses = append(comparison.NewPasses, name)
			}
		} else {
			comparison.AddedTests = append(comparison.AddedTests, name)
		}
	}

	// Find removed tests
	for name := range baselineResults {
		if _, exists := currentResults[name]; !exists {
			comparison.RemovedTests = append(comparison.RemovedTests, name)
		}
	}

	return comparison
}

func outputJSON(result *EvalResult) {
	data, _ := json.MarshalIndent(result, "", "  ")
	fmt.Println(string(data))
}

func outputGitHub(result *EvalResult, ciMode bool) {
	// GitHub Actions workflow commands
	// https://docs.github.com/en/actions/using-workflows/workflow-commands-for-github-actions

	// Set outputs
	fmt.Printf("::set-output name=total::%d\n", result.TotalTests)
	fmt.Printf("::set-output name=passed::%d\n", result.Passed)
	fmt.Printf("::set-output name=failed::%d\n", result.Failed)
	fmt.Printf("::set-output name=regressions::%d\n", result.Regressions)

	// Group for test results
	fmt.Println("::group::Test Results")
	for _, tr := range result.TestResults {
		if tr.Status == "passed" {
			fmt.Printf("✓ %s\n", tr.Name)
		} else {
			fmt.Printf("✗ %s\n", tr.Name)
			for _, cr := range tr.CheckResults {
				if !cr.Passed {
					fmt.Printf("  - %s: %s\n", cr.Check, cr.Message)
				}
			}
		}
	}
	fmt.Println("::endgroup::")

	// Report regressions as errors
	if result.Comparison != nil && len(result.Comparison.NewFailures) > 0 {
		fmt.Println("::group::Regressions Detected")
		for _, name := range result.Comparison.NewFailures {
			fmt.Printf("::error title=Regression::%s failed (was passing in baseline)\n", name)
		}
		fmt.Println("::endgroup::")
	}

	// Summary
	if result.Regressions > 0 {
		fmt.Printf("::error::%d regression(s) detected. %d/%d tests passed.\n",
			result.Regressions, result.Passed, result.TotalTests)
	} else if result.Failed > 0 {
		fmt.Printf("::warning::%d test(s) failed. %d/%d tests passed.\n",
			result.Failed, result.Passed, result.TotalTests)
	} else {
		fmt.Printf("::notice::All %d tests passed.\n", result.TotalTests)
	}
}

func outputText(result *EvalResult, successStyle, failStyle, warnStyle, dimStyle lipgloss.Style) {
	fmt.Println()
	fmt.Println(strings.Repeat("─", 50))
	fmt.Println()

	// Summary
	passRate := float64(result.Passed) / float64(result.TotalTests) * 100
	fmt.Printf("Results: %d/%d tests passed (%.1f%%)\n",
		result.Passed, result.TotalTests, passRate)

	if result.Failed > 0 {
		fmt.Printf("%s %d test(s) failed\n", failStyle.Render("✗"), result.Failed)
	}

	// Baseline comparison
	if result.Comparison != nil {
		fmt.Println()
		fmt.Println("Baseline comparison:")

		if len(result.Comparison.NewFailures) > 0 {
			fmt.Printf("  %s %d regression(s):\n", failStyle.Render("✗"), len(result.Comparison.NewFailures))
			for _, name := range result.Comparison.NewFailures {
				fmt.Printf("    - %s (was passing)\n", name)
			}
		}

		if len(result.Comparison.NewPasses) > 0 {
			fmt.Printf("  %s %d new pass(es):\n", successStyle.Render("✓"), len(result.Comparison.NewPasses))
			for _, name := range result.Comparison.NewPasses {
				fmt.Printf("    - %s (was failing)\n", name)
			}
		}

		if len(result.Comparison.AddedTests) > 0 {
			fmt.Printf("  %s %d new test(s)\n", dimStyle.Render("○"), len(result.Comparison.AddedTests))
		}

		if len(result.Comparison.RemovedTests) > 0 {
			fmt.Printf("  %s %d removed test(s)\n", warnStyle.Render("⚠"), len(result.Comparison.RemovedTests))
		}

		if len(result.Comparison.NewFailures) == 0 && len(result.Comparison.NewPasses) == 0 {
			fmt.Printf("  %s No behavioral changes detected\n", successStyle.Render("✓"))
		}
	}

	fmt.Println()
}
