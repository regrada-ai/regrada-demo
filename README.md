# Regrada Demo

This demo shows how Regrada detects AI behavioral regressions in PRs.

## Quick Test

1. **Copy this demo to a new repo:**
   ```bash
   # Create a new repo on GitHub, then:
   cp -r examples/demo/* /path/to/your/new/repo/
   cd /path/to/your/new/repo
   git init
   git add .
   git commit -m "Initial commit with baseline"
   git remote add origin git@github.com:YOUR_USERNAME/regrada-demo.git
   git push -u origin main
   ```

2. **Create a PR:**
   ```bash
   git checkout -b test-regression
   # The tests are already set up with one failing test
   git commit --allow-empty -m "Trigger CI"
   git push -u origin test-regression
   # Open a PR on GitHub
   ```

3. **See the results:**
   - The CI will run and detect a regression
   - A comment will be posted on your PR showing:
     - 🔴 1 regression detected
     - `refund_broken` was passing in baseline but now fails

## What's in this demo

```
.
├── .regrada.yaml           # Regrada configuration
├── .regrada/
│   └── baseline.json       # Baseline where all tests passed
├── evals/
│   ├── tests.yaml          # Test definitions (1 fails, 2 pass)
│   └── prompts/
│       └── refund.txt      # Example prompt file
└── .github/
    └── workflows/
        └── ai-tests.yml    # GitHub Actions workflow
```

## Test Cases

| Test | Status | Why |
|------|--------|-----|
| `greeting_works` | ✅ Pass | Checks sentiment is positive |
| `refund_broken` | ❌ Fail | Has `INTENTIONAL_FAIL` check |
| `stays_on_topic` | ✅ Pass | Checks bot stays on topic |

## The Baseline

The baseline (`.regrada/baseline.json`) shows all 3 tests were passing before.
When CI runs now, `refund_broken` fails, triggering a **regression alert**.

## To make all tests pass

Edit `evals/tests.yaml` and remove the `INTENTIONAL_FAIL` check:

```yaml
  - name: refund_broken
    prompt: prompts/refund.txt
    checks:
      - schema_valid
      - "tool_called:refund.create"
      # - "INTENTIONAL_FAIL"  # Remove this line
```

Then the PR will show all tests passing!
