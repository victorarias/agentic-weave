package turn

import (
	"context"
	"errors"
	"fmt"

	"github.com/victorarias/agentic-weave/agentic/loop"
	"github.com/victorarias/agentic-weave/agentic/message"
)

var (
	// ErrNoSystemPrompt indicates that no system prompt source was configured or
	// selected for this turn.
	//
	// Empty/whitespace prompts are considered explicitly provided values and are
	// allowed by design.
	ErrNoSystemPrompt = errors.New("turn: no system prompt provided")

	// ErrNoRunner indicates Execute was called without a runner.
	ErrNoRunner = errors.New("turn: no runner provided")
)

// PromptProvider resolves the system prompt for the current turn.
//
// Returning an empty/whitespace prompt is allowed and treated as an explicit
// prompt value.
//
// Applications should pass per-turn identity (user/conversation) through
// context values.
type PromptProvider interface {
	ResolveSystemPrompt(ctx context.Context) (string, error)
}

// Contribution is returned by a contributor for a specific turn.
//
// RunHooks may keep turn-local state captured during Contribute.
type Contribution struct {
	RunHooks []RunHook
}

// SectionContributor adds runtime sections and optionally registers run hooks.
type SectionContributor interface {
	Contribute(ctx context.Context, b *Builder) (Contribution, error)
}

// SectionContributorFunc adapts a simple function to SectionContributor.
type SectionContributorFunc func(ctx context.Context, b *Builder) error

func (f SectionContributorFunc) Contribute(ctx context.Context, b *Builder) (Contribution, error) {
	if err := f(ctx, b); err != nil {
		return Contribution{}, err
	}
	return Contribution{}, nil
}

// RunHook executes after the runner completes (success or failure).
type RunHook interface {
	AfterRun(ctx context.Context, report RunReport) error
}

// RunHookFunc adapts a function to RunHook.
type RunHookFunc func(ctx context.Context, report RunReport) error

func (f RunHookFunc) AfterRun(ctx context.Context, report RunReport) error {
	return f(ctx, report)
}

// RunMetadata carries optional execution metadata for hooks.
type RunMetadata struct {
	HistoryCompacted bool
	Values           map[string]any
}

// RunReport is passed to run hooks after execution.
type RunReport struct {
	Request   loop.Request
	Assembled Assembled

	// Result is nil when Err != nil.
	Result *loop.Result
	Err    error

	Metadata RunMetadata
}

// TurnRunner is implemented by loop runners.
type TurnRunner interface {
	Run(ctx context.Context, req loop.Request) (loop.Result, error)
}

// RunnerFunc adapts a function to TurnRunner.
type RunnerFunc func(ctx context.Context, req loop.Request) (loop.Result, error)

func (f RunnerFunc) Run(ctx context.Context, req loop.Request) (loop.Result, error) {
	return f(ctx, req)
}

// AssemblerOption configures an Assembler.
type AssemblerOption func(*Assembler)

// WithContributors appends contributors in invocation order.
func WithContributors(contributors ...SectionContributor) AssemblerOption {
	return func(a *Assembler) {
		a.contributors = append(a.contributors, contributors...)
	}
}

// WithPromptProvider configures a prompt provider used when input omits
// SystemPrompt.
func WithPromptProvider(provider PromptProvider) AssemblerOption {
	return func(a *Assembler) {
		a.promptProvider = provider
	}
}

// Assembler orchestrates turn planning and execution lifecycle.
type Assembler struct {
	contributors   []SectionContributor
	promptProvider PromptProvider
}

// NewAssembler creates an assembler with optional configuration.
func NewAssembler(opts ...AssemblerOption) *Assembler {
	a := &Assembler{}
	for _, opt := range opts {
		if opt != nil {
			opt(a)
		}
	}
	return a
}

// AssemblerInput provides per-turn build inputs.
type AssemblerInput struct {
	// Optional if PromptProvider is configured. If both are set,
	// SystemPrompt takes precedence.
	//
	// Empty string means "unset" (fall back to PromptProvider). Whitespace-only
	// values are treated as explicit prompts and are preserved.
	SystemPrompt string
	UserMessage  string

	// Pass-through to loop.Request.
	History []message.AgentMessage
}

// Plan is the built turn plus captured per-turn hooks.
type Plan struct {
	Request   loop.Request
	Assembled Assembled
	hooks     []RunHook
}

// Hooks returns a copy of turn-local hooks captured during planning.
func (p Plan) Hooks() []RunHook {
	if len(p.hooks) == 0 {
		return nil
	}
	out := make([]RunHook, 0, len(p.hooks))
	out = append(out, p.hooks...)
	return out
}

// RunHooks invokes captured run hooks and aggregates their errors.
func (p Plan) RunHooks(ctx context.Context, report RunReport) error {
	var hookErrs []error
	for i, hook := range p.hooks {
		hookErr := hook.AfterRun(ctx, report)
		if hookErr != nil {
			hookErrs = append(hookErrs, fmt.Errorf("turn: run hook %d failed: %w", i, hookErr))
		}
	}
	if len(hookErrs) == 0 {
		return nil
	}
	return errors.Join(hookErrs...)
}

// Plan resolves a system prompt, runs contributors, and builds a request.
func (a *Assembler) Plan(ctx context.Context, in AssemblerInput) (Plan, error) {
	systemPrompt, err := a.resolveSystemPrompt(ctx, in)
	if err != nil {
		return Plan{}, err
	}

	builder := NewBuilder(systemPrompt)
	hooks := make([]RunHook, 0)
	for i, contributor := range a.contributors {
		if contributor == nil {
			continue
		}
		contribution, contributeErr := contributor.Contribute(ctx, builder)
		if contributeErr != nil {
			return Plan{}, fmt.Errorf("turn: contributor %d failed: %w", i, contributeErr)
		}
		for _, hook := range contribution.RunHooks {
			if hook == nil {
				continue
			}
			hooks = append(hooks, hook)
		}
	}

	assembled := builder.Assemble(in.UserMessage)
	return Plan{
		Request: loop.Request{
			SystemPrompt: assembled.SystemPrompt,
			UserMessage:  assembled.UserMessage,
			History:      in.History,
		},
		Assembled: assembled,
		hooks:     hooks,
	}, nil
}

// ExecuteInput configures full turn execution.
type ExecuteInput struct {
	AssemblerInput
	Runner   TurnRunner
	Metadata RunMetadata
}

// ExecuteOutput captures assembled turn and runner result.
type ExecuteOutput struct {
	Assembled Assembled
	Result    loop.Result
}

// Execute runs full lifecycle: plan, runner execution, then run hooks.
//
// Run hooks are invoked even when the runner fails.
func (a *Assembler) Execute(ctx context.Context, in ExecuteInput) (ExecuteOutput, error) {
	if in.Runner == nil {
		return ExecuteOutput{}, ErrNoRunner
	}

	plan, err := a.Plan(ctx, in.AssemblerInput)
	if err != nil {
		return ExecuteOutput{}, err
	}

	result, runErr := in.Runner.Run(ctx, plan.Request)

	report := RunReport{
		Request:   plan.Request,
		Assembled: plan.Assembled,
		Err:       runErr,
		Metadata:  in.Metadata,
	}
	if runErr == nil {
		resultCopy := result
		report.Result = &resultCopy
	}

	hookErr := plan.RunHooks(ctx, report)
	joinedErr := errors.Join(runErr, hookErr)

	if joinedErr != nil {
		return ExecuteOutput{Assembled: plan.Assembled}, joinedErr
	}

	return ExecuteOutput{
		Assembled: plan.Assembled,
		Result:    result,
	}, nil
}

func (a *Assembler) resolveSystemPrompt(ctx context.Context, in AssemblerInput) (string, error) {
	if in.SystemPrompt != "" {
		return in.SystemPrompt, nil
	}
	if a.promptProvider == nil {
		return "", ErrNoSystemPrompt
	}
	prompt, err := a.promptProvider.ResolveSystemPrompt(ctx)
	if err != nil {
		return "", fmt.Errorf("turn: resolve system prompt: %w", err)
	}
	return prompt, nil
}
