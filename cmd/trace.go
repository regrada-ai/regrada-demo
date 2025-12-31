package cmd

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var traceCmd = &cobra.Command{
	Use:   "trace [command]",
	Short: "Run a command and trace all LLM API calls",
	Long: `Run a command while capturing all LLM API calls for analysis.

Regrada automatically intercepts calls to:
  - OpenAI (api.openai.com)
  - Anthropic (api.anthropic.com)
  - Azure OpenAI (*.openai.azure.com)
  - Custom endpoints (configured in .regrada.yaml)

The traced calls are compared against baselines to detect regressions.

Examples:
  regrada trace -- python app.py
  regrada trace -- npm run dev
  regrada trace -- go run ./...
  regrada trace --save-baseline -- python test_agent.py`,
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) == 0 {
			fmt.Println("Error: No command specified")
			fmt.Println("Usage: regrada trace -- <command>")
			os.Exit(1)
		}

		saveBaseline, _ := cmd.Flags().GetBool("save-baseline")
		outputFile, _ := cmd.Flags().GetString("output")
		configPath, _ := cmd.Flags().GetString("config")
		noProxy, _ := cmd.Flags().GetBool("no-proxy")

		runTrace(args, saveBaseline, outputFile, configPath, noProxy)
	},
}

func init() {
	rootCmd.AddCommand(traceCmd)

	traceCmd.Flags().BoolP("save-baseline", "b", false, "Save traces as the new baseline")
	traceCmd.Flags().StringP("output", "o", "", "Output file for traces (default: .regrada/traces/<timestamp>.json)")
	traceCmd.Flags().StringP("config", "c", ".regrada.yaml", "Path to config file")
	traceCmd.Flags().Bool("no-proxy", false, "Run without proxy (use existing traces)")
}

// LLMTrace represents a captured LLM API call
type LLMTrace struct {
	ID           string            `json:"id"`
	Timestamp    time.Time         `json:"timestamp"`
	Provider     string            `json:"provider"`
	Endpoint     string            `json:"endpoint"`
	Model        string            `json:"model,omitempty"`
	Request      TraceRequest      `json:"request"`
	Response     TraceResponse     `json:"response"`
	Latency      time.Duration     `json:"latency_ms"`
	ToolCalls    []ToolCall        `json:"tool_calls,omitempty"`
	TokensIn     int               `json:"tokens_in,omitempty"`
	TokensOut    int               `json:"tokens_out,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

type TraceRequest struct {
	Method  string            `json:"method"`
	Path    string            `json:"path"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    json.RawMessage   `json:"body,omitempty"`
}

type TraceResponse struct {
	StatusCode int               `json:"status_code"`
	Headers    map[string]string `json:"headers,omitempty"`
	Body       json.RawMessage   `json:"body,omitempty"`
}

type ToolCall struct {
	ID       string          `json:"id"`
	Name     string          `json:"name"`
	Args     json.RawMessage `json:"arguments"`
	Response json.RawMessage `json:"response,omitempty"`
}

// TraceSession holds all traces from a single run
type TraceSession struct {
	ID        string     `json:"id"`
	StartTime time.Time  `json:"start_time"`
	EndTime   time.Time  `json:"end_time"`
	Command   string     `json:"command"`
	Traces    []LLMTrace `json:"traces"`
	Summary   TraceSummary `json:"summary"`
}

type TraceSummary struct {
	TotalCalls    int           `json:"total_calls"`
	TotalTokensIn int           `json:"total_tokens_in"`
	TotalTokensOut int          `json:"total_tokens_out"`
	TotalLatency  time.Duration `json:"total_latency_ms"`
	ByProvider    map[string]int `json:"by_provider"`
	ByModel       map[string]int `json:"by_model"`
	ToolsCalled   []string      `json:"tools_called"`
}

// LLMProxy intercepts and records LLM API calls
type LLMProxy struct {
	listener   net.Listener
	server     *http.Server
	traces     []LLMTrace
	mu         sync.Mutex
	config     *RegradaConfig
	providers  map[string]*url.URL
	httpClient *http.Client
}

// Known LLM API endpoints
var knownProviders = map[string]string{
	"api.openai.com":     "openai",
	"api.anthropic.com":  "anthropic",
}

func runTrace(args []string, saveBaseline bool, outputFile string, configPath string, noProxy bool) {
	// Styles
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212"))
	successStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))

	fmt.Println()
	fmt.Println(titleStyle.Render("Regrada Trace"))
	fmt.Println(dimStyle.Render("Capturing LLM API calls..."))
	fmt.Println()

	// Load config
	config, err := loadConfig(configPath)
	if err != nil {
		fmt.Printf("%s Config not found, using defaults\n", warnStyle.Render("Warning:"))
		defaultCfg := getDefaultConfig(".")
		config = &defaultCfg
	}

	// Ensure trace directory exists
	traceDir := filepath.Join(".regrada", "traces")
	if err := os.MkdirAll(traceDir, 0755); err != nil {
		fmt.Printf("Error creating trace directory: %v\n", err)
		os.Exit(1)
	}

	var session *TraceSession

	if noProxy {
		// Run without proxy - just execute the command
		session = &TraceSession{
			ID:        generateTraceID(),
			StartTime: time.Now(),
			Command:   strings.Join(args, " "),
			Traces:    []LLMTrace{},
		}

		exitCode := executeCommand(args, nil)
		session.EndTime = time.Now()

		if exitCode != 0 {
			os.Exit(exitCode)
		}
	} else {
		// Start proxy
		proxy, err := newLLMProxy(config)
		if err != nil {
			fmt.Printf("Error starting proxy: %v\n", err)
			os.Exit(1)
		}

		proxyAddr := proxy.listener.Addr().String()
		fmt.Printf("%s Proxy listening on %s\n", successStyle.Render("✓"), proxyAddr)

		// Build environment with proxy settings
		env := buildProxyEnv(proxyAddr, config)

		session = &TraceSession{
			ID:        generateTraceID(),
			StartTime: time.Now(),
			Command:   strings.Join(args, " "),
		}

		// Handle graceful shutdown
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

		go func() {
			<-sigChan
			fmt.Println("\n\nShutting down...")
			cancel()
			proxy.shutdown()
		}()

		// Execute the command
		fmt.Printf("%s Running: %s\n\n", dimStyle.Render("→"), strings.Join(args, " "))
		fmt.Println(strings.Repeat("─", 50))

		exitCode := executeCommandWithCtx(ctx, args, env)

		fmt.Println(strings.Repeat("─", 50))
		fmt.Println()

		// Stop proxy and collect traces
		proxy.shutdown()
		session.EndTime = time.Now()
		session.Traces = proxy.getTraces()

		if exitCode != 0 && ctx.Err() == nil {
			fmt.Printf("%s Command exited with code %d\n", warnStyle.Render("Warning:"), exitCode)
		}
	}

	// Calculate summary
	session.Summary = calculateSummary(session.Traces)

	// Print summary
	printTraceSummary(session, successStyle, dimStyle)

	// Save traces
	if outputFile == "" {
		outputFile = filepath.Join(traceDir, fmt.Sprintf("%s.json", session.ID))
	}

	if err := saveTraceSession(session, outputFile); err != nil {
		fmt.Printf("Error saving traces: %v\n", err)
	} else {
		fmt.Printf("\n%s Traces saved to %s\n", successStyle.Render("✓"), outputFile)
	}

	// Save as baseline if requested
	if saveBaseline {
		baselinePath := filepath.Join(".regrada", "baseline.json")
		if err := saveTraceSession(session, baselinePath); err != nil {
			fmt.Printf("Error saving baseline: %v\n", err)
		} else {
			fmt.Printf("%s Baseline saved to %s\n", successStyle.Render("✓"), baselinePath)
		}
	}

	// Compare with baseline if it exists
	baselinePath := filepath.Join(".regrada", "baseline.json")
	if _, err := os.Stat(baselinePath); err == nil && !saveBaseline {
		compareWithBaseline(session, baselinePath, successStyle, warnStyle)
	}
}

func loadConfig(path string) (*RegradaConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var config RegradaConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	return &config, nil
}

func newLLMProxy(config *RegradaConfig) (*LLMProxy, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("failed to start listener: %w", err)
	}

	proxy := &LLMProxy{
		listener:  listener,
		traces:    []LLMTrace{},
		config:    config,
		providers: make(map[string]*url.URL),
		httpClient: &http.Client{
			Timeout: 120 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: false},
				MaxIdleConns:    100,
				IdleConnTimeout: 90 * time.Second,
			},
		},
	}

	// Set up known provider URLs
	proxy.providers["openai"], _ = url.Parse("https://api.openai.com")
	proxy.providers["anthropic"], _ = url.Parse("https://api.anthropic.com")

	// Add custom provider if configured
	if config.Provider.BaseURL != "" {
		customURL, err := url.Parse(config.Provider.BaseURL)
		if err == nil {
			proxy.providers["custom"] = customURL
		}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", proxy.handleRequest)

	proxy.server = &http.Server{
		Handler: mux,
	}

	go proxy.server.Serve(listener)

	return proxy, nil
}

func (p *LLMProxy) handleRequest(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()

	// Determine target based on X-Regrada-Target header or path
	targetProvider := r.Header.Get("X-Regrada-Target")
	if targetProvider == "" {
		targetProvider = "openai" // default
	}

	targetURL, ok := p.providers[targetProvider]
	if !ok {
		http.Error(w, "Unknown provider", http.StatusBadGateway)
		return
	}

	// Read request body
	var requestBody []byte
	if r.Body != nil {
		requestBody, _ = io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewBuffer(requestBody))
	}

	// Create proxy request
	proxyURL := *targetURL
	proxyURL.Path = r.URL.Path
	proxyURL.RawQuery = r.URL.RawQuery

	proxyReq, err := http.NewRequest(r.Method, proxyURL.String(), bytes.NewBuffer(requestBody))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Copy headers, removing proxy-specific ones
	for key, values := range r.Header {
		if strings.HasPrefix(key, "X-Regrada-") {
			continue
		}
		for _, value := range values {
			proxyReq.Header.Add(key, value)
		}
	}

	// Make the request
	resp, err := p.httpClient.Do(proxyReq)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// Read response body
	var responseBody []byte
	if resp.Header.Get("Content-Encoding") == "gzip" {
		gzReader, err := gzip.NewReader(resp.Body)
		if err == nil {
			responseBody, _ = io.ReadAll(gzReader)
			gzReader.Close()
		}
	} else {
		responseBody, _ = io.ReadAll(resp.Body)
	}

	latency := time.Since(startTime)

	// Record trace
	trace := p.createTrace(targetProvider, r, requestBody, resp, responseBody, latency)
	p.mu.Lock()
	p.traces = append(p.traces, trace)
	p.mu.Unlock()

	// Write response
	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	w.Header().Del("Content-Encoding") // We've already decompressed
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(responseBody)))
	w.WriteHeader(resp.StatusCode)
	w.Write(responseBody)
}

func (p *LLMProxy) createTrace(provider string, req *http.Request, reqBody []byte, resp *http.Response, respBody []byte, latency time.Duration) LLMTrace {
	trace := LLMTrace{
		ID:        generateTraceID(),
		Timestamp: time.Now(),
		Provider:  provider,
		Endpoint:  req.URL.Path,
		Latency:   latency / time.Millisecond,
		Request: TraceRequest{
			Method:  req.Method,
			Path:    req.URL.Path,
			Headers: flattenHeaders(req.Header),
			Body:    sanitizeBody(reqBody),
		},
		Response: TraceResponse{
			StatusCode: resp.StatusCode,
			Headers:    flattenHeaders(resp.Header),
			Body:       sanitizeBody(respBody),
		},
	}

	// Extract model and tokens from request/response
	trace.Model, trace.TokensIn, trace.TokensOut, trace.ToolCalls = parseAPIDetails(provider, reqBody, respBody)

	return trace
}

func parseAPIDetails(provider string, reqBody, respBody []byte) (model string, tokensIn, tokensOut int, toolCalls []ToolCall) {
	var reqData map[string]interface{}
	var respData map[string]interface{}

	json.Unmarshal(reqBody, &reqData)
	json.Unmarshal(respBody, &respData)

	// Extract model from request
	if m, ok := reqData["model"].(string); ok {
		model = m
	}

	// Provider-specific parsing
	switch provider {
	case "openai":
		if usage, ok := respData["usage"].(map[string]interface{}); ok {
			if pt, ok := usage["prompt_tokens"].(float64); ok {
				tokensIn = int(pt)
			}
			if ct, ok := usage["completion_tokens"].(float64); ok {
				tokensOut = int(ct)
			}
		}
		// Extract tool calls
		if choices, ok := respData["choices"].([]interface{}); ok && len(choices) > 0 {
			if choice, ok := choices[0].(map[string]interface{}); ok {
				if msg, ok := choice["message"].(map[string]interface{}); ok {
					if tcs, ok := msg["tool_calls"].([]interface{}); ok {
						for _, tc := range tcs {
							if tcMap, ok := tc.(map[string]interface{}); ok {
								toolCall := ToolCall{
									ID: getString(tcMap, "id"),
								}
								if fn, ok := tcMap["function"].(map[string]interface{}); ok {
									toolCall.Name = getString(fn, "name")
									if args, ok := fn["arguments"].(string); ok {
										toolCall.Args = json.RawMessage(args)
									}
								}
								toolCalls = append(toolCalls, toolCall)
							}
						}
					}
				}
			}
		}

	case "anthropic":
		if usage, ok := respData["usage"].(map[string]interface{}); ok {
			if it, ok := usage["input_tokens"].(float64); ok {
				tokensIn = int(it)
			}
			if ot, ok := usage["output_tokens"].(float64); ok {
				tokensOut = int(ot)
			}
		}
		// Extract tool use from Anthropic format
		if content, ok := respData["content"].([]interface{}); ok {
			for _, c := range content {
				if cMap, ok := c.(map[string]interface{}); ok {
					if cMap["type"] == "tool_use" {
						toolCall := ToolCall{
							ID:   getString(cMap, "id"),
							Name: getString(cMap, "name"),
						}
						if input, ok := cMap["input"]; ok {
							if inputBytes, err := json.Marshal(input); err == nil {
								toolCall.Args = json.RawMessage(inputBytes)
							}
						}
						toolCalls = append(toolCalls, toolCall)
					}
				}
			}
		}
	}

	return
}

func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func (p *LLMProxy) getTraces() []LLMTrace {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]LLMTrace{}, p.traces...)
}

func (p *LLMProxy) shutdown() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	p.server.Shutdown(ctx)
}

func buildProxyEnv(proxyAddr string, config *RegradaConfig) []string {
	env := os.Environ()
	proxyURL := fmt.Sprintf("http://%s", proxyAddr)

	// OpenAI SDK
	env = append(env, fmt.Sprintf("OPENAI_BASE_URL=%s", proxyURL))
	env = append(env, fmt.Sprintf("OPENAI_API_BASE=%s", proxyURL)) // older SDK versions

	// Anthropic SDK
	env = append(env, fmt.Sprintf("ANTHROPIC_BASE_URL=%s", proxyURL))

	// Azure OpenAI - needs special header
	env = append(env, fmt.Sprintf("AZURE_OPENAI_ENDPOINT=%s", proxyURL))

	// Generic proxy settings (for custom implementations)
	env = append(env, fmt.Sprintf("LLM_API_BASE=%s", proxyURL))
	env = append(env, fmt.Sprintf("REGRADA_PROXY=%s", proxyURL))

	// Mark that we're running under regrada
	env = append(env, "REGRADA_TRACING=1")

	return env
}

func executeCommand(args []string, env []string) int {
	return executeCommandWithCtx(context.Background(), args, env)
}

func executeCommandWithCtx(ctx context.Context, args []string, env []string) int {
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	if env != nil {
		cmd.Env = env
	}

	err := cmd.Run()
	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			return exitError.ExitCode()
		}
		return 1
	}
	return 0
}

func generateTraceID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

func flattenHeaders(h http.Header) map[string]string {
	result := make(map[string]string)
	for key, values := range h {
		// Skip sensitive headers
		lowerKey := strings.ToLower(key)
		if lowerKey == "authorization" || lowerKey == "x-api-key" || lowerKey == "api-key" {
			result[key] = "[REDACTED]"
			continue
		}
		result[key] = strings.Join(values, ", ")
	}
	return result
}

func sanitizeBody(body []byte) json.RawMessage {
	if len(body) == 0 {
		return nil
	}

	// Try to parse as JSON to validate
	var js interface{}
	if json.Unmarshal(body, &js) != nil {
		// Not valid JSON, return as quoted string
		quoted, _ := json.Marshal(string(body))
		return json.RawMessage(quoted)
	}

	return json.RawMessage(body)
}

func calculateSummary(traces []LLMTrace) TraceSummary {
	summary := TraceSummary{
		TotalCalls:  len(traces),
		ByProvider:  make(map[string]int),
		ByModel:     make(map[string]int),
		ToolsCalled: []string{},
	}

	toolSet := make(map[string]bool)

	for _, t := range traces {
		summary.TotalTokensIn += t.TokensIn
		summary.TotalTokensOut += t.TokensOut
		summary.TotalLatency += t.Latency
		summary.ByProvider[t.Provider]++
		if t.Model != "" {
			summary.ByModel[t.Model]++
		}
		for _, tc := range t.ToolCalls {
			toolSet[tc.Name] = true
		}
	}

	for tool := range toolSet {
		summary.ToolsCalled = append(summary.ToolsCalled, tool)
	}

	return summary
}

func printTraceSummary(session *TraceSession, successStyle, dimStyle lipgloss.Style) {
	summary := session.Summary

	fmt.Printf("%s Captured %d LLM calls in %v\n",
		successStyle.Render("✓"),
		summary.TotalCalls,
		session.EndTime.Sub(session.StartTime).Round(time.Millisecond))

	if summary.TotalCalls == 0 {
		fmt.Println(dimStyle.Render("  No LLM API calls detected"))
		return
	}

	fmt.Println()
	fmt.Println("  Summary:")

	if len(summary.ByProvider) > 0 {
		fmt.Print("    Providers: ")
		parts := []string{}
		for provider, count := range summary.ByProvider {
			parts = append(parts, fmt.Sprintf("%s (%d)", provider, count))
		}
		fmt.Println(strings.Join(parts, ", "))
	}

	if len(summary.ByModel) > 0 {
		fmt.Print("    Models: ")
		parts := []string{}
		for model, count := range summary.ByModel {
			parts = append(parts, fmt.Sprintf("%s (%d)", model, count))
		}
		fmt.Println(strings.Join(parts, ", "))
	}

	if summary.TotalTokensIn > 0 || summary.TotalTokensOut > 0 {
		fmt.Printf("    Tokens: %d in / %d out\n", summary.TotalTokensIn, summary.TotalTokensOut)
	}

	fmt.Printf("    Total latency: %dms\n", summary.TotalLatency)

	if len(summary.ToolsCalled) > 0 {
		fmt.Printf("    Tools called: %s\n", strings.Join(summary.ToolsCalled, ", "))
	}
}

func saveTraceSession(session *TraceSession, path string) error {
	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(session, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}

func compareWithBaseline(session *TraceSession, baselinePath string, successStyle, warnStyle lipgloss.Style) {
	data, err := os.ReadFile(baselinePath)
	if err != nil {
		return
	}

	var baseline TraceSession
	if err := json.Unmarshal(data, &baseline); err != nil {
		return
	}

	fmt.Println()
	fmt.Println("  Comparison with baseline:")

	// Compare call counts
	if session.Summary.TotalCalls != baseline.Summary.TotalCalls {
		fmt.Printf("    %s Call count changed: %d → %d\n",
			warnStyle.Render("⚠"),
			baseline.Summary.TotalCalls,
			session.Summary.TotalCalls)
	} else {
		fmt.Printf("    %s Call count unchanged: %d\n",
			successStyle.Render("✓"),
			session.Summary.TotalCalls)
	}

	// Compare tools called
	baselineTools := make(map[string]bool)
	for _, t := range baseline.Summary.ToolsCalled {
		baselineTools[t] = true
	}

	currentTools := make(map[string]bool)
	for _, t := range session.Summary.ToolsCalled {
		currentTools[t] = true
	}

	// New tools
	for tool := range currentTools {
		if !baselineTools[tool] {
			fmt.Printf("    %s New tool called: %s\n", warnStyle.Render("⚠"), tool)
		}
	}

	// Removed tools
	for tool := range baselineTools {
		if !currentTools[tool] {
			fmt.Printf("    %s Tool no longer called: %s\n", warnStyle.Render("⚠"), tool)
		}
	}

	// Compare models
	for model, count := range session.Summary.ByModel {
		if baselineCount, ok := baseline.Summary.ByModel[model]; ok {
			if count != baselineCount {
				fmt.Printf("    %s Model %s usage changed: %d → %d\n",
					warnStyle.Render("⚠"), model, baselineCount, count)
			}
		} else {
			fmt.Printf("    %s New model used: %s\n", warnStyle.Render("⚠"), model)
		}
	}

	// Token usage change
	tokenDiff := (session.Summary.TotalTokensIn + session.Summary.TotalTokensOut) -
		(baseline.Summary.TotalTokensIn + baseline.Summary.TotalTokensOut)
	if tokenDiff != 0 {
		direction := "increased"
		if tokenDiff < 0 {
			direction = "decreased"
			tokenDiff = -tokenDiff
		}
		fmt.Printf("    %s Token usage %s by %d\n", warnStyle.Render("⚠"), direction, tokenDiff)
	}
}

// DumpRequest is a helper for debugging
func dumpRequest(req *http.Request) {
	dump, _ := httputil.DumpRequest(req, true)
	fmt.Printf("Request:\n%s\n", dump)
}
