package anthropic

import (
	"context"
	"encoding/json"

	anthropicapi "github.com/anthropics/anthropic-sdk-go"
	providerspkg "github.com/victorarias/agentic-weave/agentic/providers"
	"github.com/victorarias/agentic-weave/agentic/usage"
)

func emitBeforeProviderRequest(ctx context.Context, hook providerspkg.ProviderHook, model string, req anthropicapi.MessageNewParams, operation string) []byte {
	if hook == nil {
		return nil
	}
	requestJSON, err := json.Marshal(req)
	if err != nil {
		hook.OnProviderError(ctx, providerspkg.ProviderErrorEvent{Provider: "anthropic", Model: model, Operation: operation, Err: err})
		return nil
	}
	hook.BeforeProviderRequest(ctx, providerspkg.ProviderRequestEvent{Provider: "anthropic", Model: model, Operation: operation, RequestJSON: requestJSON})
	return requestJSON
}

func emitAfterProviderResponse(ctx context.Context, hook providerspkg.ProviderHook, model string, operation string, requestJSON []byte, stopReason string, usageValue *usage.Usage, response any) {
	if hook == nil {
		return
	}
	responseJSON, err := json.Marshal(response)
	if err != nil {
		hook.OnProviderError(ctx, providerspkg.ProviderErrorEvent{Provider: "anthropic", Model: model, Operation: operation, RequestJSON: requestJSON, Err: err})
		return
	}
	hook.AfterProviderResponse(ctx, providerspkg.ProviderResponseEvent{Provider: "anthropic", Model: model, Operation: operation, ResponseJSON: responseJSON, StopReason: stopReason, Usage: usageValue})
}

func emitProviderError(ctx context.Context, hook providerspkg.ProviderHook, model string, operation string, requestJSON []byte, err error) {
	if hook == nil || err == nil {
		return
	}
	hook.OnProviderError(ctx, providerspkg.ProviderErrorEvent{Provider: "anthropic", Model: model, Operation: operation, RequestJSON: requestJSON, Err: err})
}
