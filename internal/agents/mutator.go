package agents

import (
	"context"
	_ "embed"
	"fmt"
	"strings"

	"go.uber.org/zap"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/model"
	"google.golang.org/adk/model/gemini"
	"google.golang.org/genai"
)

const mutatorName = "Mutator"

// mutatorTemplate is the Gemini prompt loaded from prompts/mutator.tmpl
// at compile time via go:embed. Edits to the .tmpl require a rebuild.
//
//go:embed prompts/mutator.tmpl
var mutatorTemplate string

// MutatorOptions holds the LLM-specific knobs the Mutator factory needs in
// addition to the shared Deps. They are passed separately so DepsConfig
// stays free of credentials.
type MutatorOptions struct {
	APIKey    string
	ModelName string
	// Temperature controls the LLM's stochasticity. Default is 0.2 (deterministic).
	Temperature float32
}

// NewMutator builds the Gemini-backed Mutator. It is an LlmAgent whose
// instruction is assembled dynamically from session state each iteration
// (target source, uncovered summary, accepted tests, etc.), and whose text
// response is written into state under StateCandidateTest via OutputKey.
//
// The Selector picks up StateCandidateTest unchanged from state, cleans
// any stray markdown fences via cleanCandidateCode, and feeds the result
// to the Workspace's AppendTest validator.
//
// The Evaluator and Selector are unchanged from Phase 3.
func NewMutator(ctx context.Context, deps Deps, opts MutatorOptions) (agent.Agent, error) {
	if opts.APIKey == "" {
		return nil, fmt.Errorf("mutator: API key is empty")
	}
	if opts.ModelName == "" {
		return nil, fmt.Errorf("mutator: model name is empty")
	}
	model, err := gemini.NewModel(ctx, opts.ModelName, &genai.ClientConfig{
		APIKey: opts.APIKey,
	})
	if err != nil {
		return nil, fmt.Errorf("mutator: gemini.NewModel: %w", err)
	}

	temp := opts.Temperature
	if temp == 0 {
		temp = 0.2
	}
	genCfg := &genai.GenerateContentConfig{
		Temperature: &temp,
	}

	logger := deps.Logger.Named("agents.mutator")

	// InstructionProvider gives us full control over how state values are
	// formatted in the prompt — slices joined with commas, percent
	// rendered to one decimal, etc. — instead of relying on ADK's
	// {key} substitution which would force the state shape to match
	// the template literally.
	provider := func(rctx agent.ReadonlyContext) (string, error) {
		prompt, err := buildMutatorPrompt(rctx)
		if err != nil {
			logger.Warn("mutator: prompt build failed",
				zap.String("phase", "mutator"),
				zap.String("component", "prompt"),
				zap.Error(err),
			)
			return "", err
		}
		logger.Debug("mutator prompt built",
			zap.String("phase", "mutator"),
			zap.String("component", "prompt"),
			zap.Int("prompt_chars", len(prompt)),
		)
		return prompt, nil
	}

	return llmagent.New(llmagent.Config{
		Name:                  mutatorName,
		Model:                 model,
		Description:           "Proposes a single Go test function targeting an uncovered branch.",
		InstructionProvider:   provider,
		OutputKey:             StateCandidateTest,
		GenerateContentConfig: genCfg,
		AfterModelCallbacks:   []llmagent.AfterModelCallback{geminiResponseLogger(logger)},
	})
}

// geminiResponseLogger returns an AfterModelCallback that emits one info
// log per finalized Gemini response, including token usage and a short
// preview of the generated text. Partial stream chunks are skipped so the
// log shows exactly one line per iteration.
//
// Returning (nil, nil) means "do not replace the model response" — we are
// purely observing, not rewriting.
func geminiResponseLogger(logger *zap.Logger) llmagent.AfterModelCallback {
	return func(_ agent.CallbackContext, resp *model.LLMResponse, callErr error) (*model.LLMResponse, error) {
		if callErr != nil {
			logger.Warn("mutator: gemini call failed",
				zap.String("phase", "mutator"),
				zap.String("component", "gemini"),
				zap.Error(callErr),
			)
			return nil, nil
		}
		if resp == nil || resp.Partial {
			return nil, nil
		}

		var respText string
		if resp.Content != nil {
			for _, p := range resp.Content.Parts {
				respText += p.Text
			}
		}
		preview := strings.ReplaceAll(respText, "\n", " ")
		if len(preview) > 80 {
			preview = preview[:80] + "..."
		}

		fields := []zap.Field{
			zap.String("phase", "mutator"),
			zap.String("component", "gemini"),
			zap.Int("response_chars", len(respText)),
			zap.String("response_preview", preview),
		}
		if resp.UsageMetadata != nil {
			fields = append(fields,
				zap.Int32("prompt_tokens", resp.UsageMetadata.PromptTokenCount),
				zap.Int32("response_tokens", resp.UsageMetadata.CandidatesTokenCount),
				zap.Int32("total_tokens", resp.UsageMetadata.TotalTokenCount),
			)
		}
		if resp.ModelVersion != "" {
			fields = append(fields, zap.String("model_version", resp.ModelVersion))
		}

		logger.Info("mutator: gemini call complete", fields...)
		return nil, nil
	}
}
