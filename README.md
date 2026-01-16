# Regrada Demo

This is a self-contained demo showing how Regrada detects AI behavioral regressions in PRs.

## Quick Start

### 1. Create a new GitHub repo and push this demo

```bash
# Create a new directory
mkdir regrada-demo && cd regrada-demo

# Copy the demo files (adjust path as needed)
cp -r /path/to/regrada/examples/demo/* .
cp -r /path/to/regrada/examples/demo/.* . 2>/dev/null

# Initialize git and push
git init
git add .
git commit -m "Initial commit"

# Create repo on GitHub, then:
git remote add origin git@github.com:YOUR_USERNAME/regrada-demo.git
git branch -M main
git push -u origin main
```

### 2. Enable GitHub Actions

Go to your repo → Settings → Actions → General → Enable "Allow all actions"

### 3. Create a PR to see it in action

```bash
git checkout -b test-regression
git commit --allow-empty -m "Test regression detection"
git push -u origin test-regression
```

Then open a PR on GitHub. You should see:
- The CI workflow run in the "Checks" tab
- A comment posted on the PR with results

## What You'll See

The PR comment will show:

```
## 🔴 Regrada AI Test Results

| Tests | Passed | Failed | Regressions |
|-------|--------|--------|-------------|
| 3     | 2      | 1      | 1           |

### 🔴 Regressions Detected
These tests were passing but are now failing:
- `refund_broken`
```

## Test Cases

| Test | Status | Description |
|------|--------|-------------|
| `greeting_works` | ✅ Pass | Checks positive sentiment |
| `refund_broken` | ❌ Fail | Has intentional failing check |
| `stays_on_topic` | ✅ Pass | Checks topic adherence |

## To Fix the Regression

Edit `evals/tests.yaml` and remove the `INTENTIONAL_FAIL` check:

```yaml
- name: refund_broken
  prompt: prompts/refund.txt
  checks:
    - schema_valid
    - "tool_called:refund.create"
    # Remove this line:
    # - "INTENTIONAL_FAIL"
```

Commit and push - the PR will update to show all green!

## Files Structure

```
.
├── main.go                 # Regrada CLI entry point
├── go.mod / go.sum         # Go dependencies
├── cmd/                    # Regrada commands
│   ├── root.go
│   ├── init.go
│   ├── run.go              # Runs evaluations
│   └── trace.go            # Traces LLM calls
├── .regrada.yaml           # Configuration
├── .regrada/
│   └── baseline.json       # Baseline (all tests passing)
├── evals/
│   ├── tests.yaml          # Test definitions
│   └── prompts/
│       └── refund.txt      # Prompt file
└── .github/workflows/
    └── ai-tests.yml        # GitHub Actions workflow
```

## Local Testing

```bash
# Run tests locally
go run . run

# Run in CI mode (exits 1 on failure)
go run . run --ci

# Output as JSON
go run . run --ci --output json
```
