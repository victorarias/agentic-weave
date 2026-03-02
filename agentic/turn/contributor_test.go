package turn

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/victorarias/agentic-weave/agentic/loop"
)

type testContributor func(context.Context, *Builder) (Contribution, error)

func (f testContributor) Contribute(ctx context.Context, b *Builder) (Contribution, error) {
	return f(ctx, b)
}

type testPromptProvider struct {
	prompt string
	err    error
	calls  int
}

func (p *testPromptProvider) ResolveSystemPrompt(context.Context) (string, error) {
	p.calls++
	if p.err != nil {
		return "", p.err
	}
	return p.prompt, nil
}

func TestAssemblerPlan_ContributorsRunInOrder(t *testing.T) {
	a := NewAssembler(WithContributors(
		testContributor(func(_ context.Context, b *Builder) (Contribution, error) {
			b.Section("first", "1")
			return Contribution{}, nil
		}),
		testContributor(func(_ context.Context, b *Builder) (Contribution, error) {
			b.Section("second", "2")
			return Contribution{}, nil
		}),
	))

	plan, err := a.Plan(context.Background(), AssemblerInput{
		SystemPrompt: "system",
		UserMessage:  "hello",
	})
	assertNoError(t, err)

	assertEqual(t, "system", plan.Request.SystemPrompt)
	assertSliceEqual(t, []string{"first", "second"}, plan.Assembled.SectionKeys)

	first := strings.Index(plan.Request.UserMessage, "[first]\n1")
	second := strings.Index(plan.Request.UserMessage, "[second]\n2")
	if first == -1 || second == -1 || first >= second {
		t.Errorf("expected [first] before [second] in user message")
	}
}

func TestAssemblerPlan_ContributorErrorAborts(t *testing.T) {
	called := false
	boom := errors.New("boom")
	a := NewAssembler(WithContributors(
		testContributor(func(_ context.Context, _ *Builder) (Contribution, error) {
			return Contribution{}, boom
		}),
		testContributor(func(_ context.Context, _ *Builder) (Contribution, error) {
			called = true
			return Contribution{}, nil
		}),
	))

	_, err := a.Plan(context.Background(), AssemblerInput{SystemPrompt: "system", UserMessage: "u"})
	assertErrorIs(t, err, boom)
	if called {
		t.Error("second contributor should not have been called")
	}
}

func TestAssemblerExecute_CallsRunnerAndHooksOnSuccess(t *testing.T) {
	hookCalled := false
	a := NewAssembler(WithContributors(
		testContributor(func(_ context.Context, b *Builder) (Contribution, error) {
			b.Section("memory", "v")
			return Contribution{RunHooks: []RunHook{RunHookFunc(func(_ context.Context, report RunReport) error {
				hookCalled = true
				if report.Err != nil {
					t.Errorf("expected no error in report, got %v", report.Err)
				}
				if report.Result == nil {
					t.Fatal("expected non-nil result")
				}
				assertEqual(t, "ok", report.Result.Reply)
				assertEqual(t, "system", report.Request.SystemPrompt)
				if !report.Metadata.HistoryCompacted {
					t.Error("expected HistoryCompacted=true")
				}
				return nil
			})}}, nil
		}),
	))

	runnerCalls := 0
	output, err := a.Execute(context.Background(), ExecuteInput{
		AssemblerInput: AssemblerInput{SystemPrompt: "system", UserMessage: "hello"},
		Runner: RunnerFunc(func(_ context.Context, req loop.Request) (loop.Result, error) {
			runnerCalls++
			assertEqual(t, "system", req.SystemPrompt)
			return loop.Result{Reply: "ok"}, nil
		}),
		Metadata: RunMetadata{HistoryCompacted: true},
	})
	assertNoError(t, err)
	if runnerCalls != 1 {
		t.Errorf("expected 1 runner call, got %d", runnerCalls)
	}
	if !hookCalled {
		t.Error("hook should have been called")
	}
	assertEqual(t, "ok", output.Result.Reply)
}

func TestAssemblerExecute_CallsHooksOnRunnerError(t *testing.T) {
	runErr := errors.New("runner failed")
	hookCalled := false
	a := NewAssembler(WithContributors(
		testContributor(func(_ context.Context, _ *Builder) (Contribution, error) {
			return Contribution{RunHooks: []RunHook{RunHookFunc(func(_ context.Context, report RunReport) error {
				hookCalled = true
				assertErrorIs(t, report.Err, runErr)
				if report.Result != nil {
					t.Error("expected nil result on error")
				}
				return nil
			})}}, nil
		}),
	))

	_, err := a.Execute(context.Background(), ExecuteInput{
		AssemblerInput: AssemblerInput{SystemPrompt: "system", UserMessage: "hello"},
		Runner: RunnerFunc(func(context.Context, loop.Request) (loop.Result, error) {
			return loop.Result{}, runErr
		}),
	})
	assertErrorIs(t, err, runErr)
	if !hookCalled {
		t.Error("hook should have been called even on runner error")
	}
}

func TestAssemblerExecute_AggregatesHookErrors(t *testing.T) {
	hookErr1 := errors.New("hook1")
	hookErr2 := errors.New("hook2")
	a := NewAssembler(WithContributors(
		testContributor(func(_ context.Context, _ *Builder) (Contribution, error) {
			return Contribution{RunHooks: []RunHook{
				RunHookFunc(func(context.Context, RunReport) error { return hookErr1 }),
				RunHookFunc(func(context.Context, RunReport) error { return hookErr2 }),
			}}, nil
		}),
	))

	_, err := a.Execute(context.Background(), ExecuteInput{
		AssemblerInput: AssemblerInput{SystemPrompt: "system", UserMessage: "hello"},
		Runner: RunnerFunc(func(context.Context, loop.Request) (loop.Result, error) {
			return loop.Result{Reply: "ok"}, nil
		}),
	})
	assertErrorIs(t, err, hookErr1)
	assertErrorIs(t, err, hookErr2)
}

func TestAssemblerPlan_PromptProviderUsedWhenInputMissingPrompt(t *testing.T) {
	provider := &testPromptProvider{prompt: "provided"}
	a := NewAssembler(WithPromptProvider(provider))

	plan, err := a.Plan(context.Background(), AssemblerInput{UserMessage: "hello"})
	assertNoError(t, err)
	assertEqual(t, "provided", plan.Request.SystemPrompt)
	if provider.calls != 1 {
		t.Errorf("expected 1 provider call, got %d", provider.calls)
	}
}

func TestAssemblerPlan_InputPromptOverridesProvider(t *testing.T) {
	provider := &testPromptProvider{prompt: "provided"}
	a := NewAssembler(WithPromptProvider(provider))

	plan, err := a.Plan(context.Background(), AssemblerInput{SystemPrompt: "input", UserMessage: "hello"})
	assertNoError(t, err)
	assertEqual(t, "input", plan.Request.SystemPrompt)
	if provider.calls != 0 {
		t.Errorf("expected 0 provider calls, got %d", provider.calls)
	}
}

func TestAssemblerPlan_PromptProviderErrorIsReturned(t *testing.T) {
	providerErr := errors.New("provider failed")
	provider := &testPromptProvider{err: providerErr}
	a := NewAssembler(WithPromptProvider(provider))

	_, err := a.Plan(context.Background(), AssemblerInput{UserMessage: "hello"})
	assertErrorIs(t, err, providerErr)
}

func TestAssemblerPlan_PromptProviderBlankPromptIsAllowed(t *testing.T) {
	provider := &testPromptProvider{prompt: "   "}
	a := NewAssembler(WithPromptProvider(provider))

	plan, err := a.Plan(context.Background(), AssemblerInput{UserMessage: "hello"})
	assertNoError(t, err)
	assertEqual(t, "   ", plan.Request.SystemPrompt)
}

func TestAssemblerExecute_AggregatesRunnerAndHookErrors(t *testing.T) {
	runErr := errors.New("runner failed")
	hookErr := errors.New("hook failed")
	a := NewAssembler(WithContributors(
		testContributor(func(_ context.Context, _ *Builder) (Contribution, error) {
			return Contribution{RunHooks: []RunHook{
				RunHookFunc(func(context.Context, RunReport) error { return hookErr }),
			}}, nil
		}),
	))

	_, err := a.Execute(context.Background(), ExecuteInput{
		AssemblerInput: AssemblerInput{SystemPrompt: "system", UserMessage: "hello"},
		Runner: RunnerFunc(func(context.Context, loop.Request) (loop.Result, error) {
			return loop.Result{}, runErr
		}),
	})
	assertErrorIs(t, err, runErr)
	assertErrorIs(t, err, hookErr)
}

func TestAssemblerPlan_ErrNoSystemPromptWhenUnavailable(t *testing.T) {
	a := NewAssembler()
	_, err := a.Plan(context.Background(), AssemblerInput{UserMessage: "hello"})
	assertErrorIs(t, err, ErrNoSystemPrompt)
}

func TestAssemblerExecute_ErrNoRunner(t *testing.T) {
	a := NewAssembler()
	_, err := a.Execute(context.Background(), ExecuteInput{AssemblerInput: AssemblerInput{SystemPrompt: "sys"}})
	assertErrorIs(t, err, ErrNoRunner)
}

// --- helpers ---

func assertNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func assertErrorIs(t *testing.T, err, target error) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error wrapping %v, got nil", target)
	}
	if !errors.Is(err, target) {
		t.Errorf("expected error to wrap %v, got %v", target, err)
	}
}
