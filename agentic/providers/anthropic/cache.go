package anthropic

import "github.com/anthropics/anthropic-sdk-go"

// newCacheControl returns a CacheControlEphemeralParam with the given TTL.
// An empty ttl means "use API default" (5 minutes) via omitzero.
func newCacheControl(ttl anthropic.CacheControlEphemeralTTL) anthropic.CacheControlEphemeralParam {
	return anthropic.CacheControlEphemeralParam{
		Type: "ephemeral",
		TTL:  ttl,
	}
}

// applyPromptCaching sets cache breakpoints on the request according to the
// client's configured CacheMode and TTL.
//
// CacheModeAutomatic: sets the top-level CacheControl field. The API places a
// single breakpoint on the last cacheable block and advances it automatically
// as conversations grow. Supported on Claude API and Azure AI Foundry.
//
// CacheModeExplicit: sets cache_control on individual content blocks:
//  1. Last system prompt block
//  2. Last tool definition
//  3. Last content block in the final message
//
// Supported on all platforms including Google Vertex AI and Amazon Bedrock.
func applyPromptCaching(req *anthropic.MessageNewParams, mode CacheMode, ttl anthropic.CacheControlEphemeralTTL) {
	if req == nil {
		return
	}
	switch mode {
	case CacheModeAutomatic:
		req.CacheControl = newCacheControl(ttl)
	case CacheModeExplicit:
		applyExplicitCacheBreakpoints(req, ttl)
	case CacheModeExplicitStablePrefixWithAutomatic:
		applyExplicitStablePrefixBreakpoints(req, ttl)
		req.CacheControl = newCacheControl(ttl)
	case CacheModeDisabled:
		return
	}
}

// applyExplicitCacheBreakpoints marks up to 3 cache breakpoints on the most
// stable content blocks in the request, from most-static to most-dynamic:
//
//  1. Last system prompt block
//  2. Last tool definition
//  3. Last content block in the final message
func applyExplicitCacheBreakpoints(req *anthropic.MessageNewParams, ttl anthropic.CacheControlEphemeralTTL) {
	cc := newCacheControl(ttl)
	setSystemCacheControl(req, cc)
	setToolsCacheControl(req, cc)
	setMessageBlockCacheControl(req, len(req.Messages)-1, cc)
}

// applyExplicitStablePrefixBreakpoints marks up to 3 cache breakpoints on the
// stable prefix before the final message:
//
//  1. Last system prompt block
//  2. Last tool definition
//  3. Last content block in the penultimate message
//
// This intentionally leaves the final message uncached so callers can combine
// it with top-level automatic caching without anchoring the only reusable
// breakpoint on transient final-message content.
func applyExplicitStablePrefixBreakpoints(req *anthropic.MessageNewParams, ttl anthropic.CacheControlEphemeralTTL) {
	cc := newCacheControl(ttl)
	setSystemCacheControl(req, cc)
	setToolsCacheControl(req, cc)
	if len(req.Messages) >= 2 {
		setMessageBlockCacheControl(req, len(req.Messages)-2, cc)
	}
}

// setSystemCacheControl replaces the last system block with a copy that has
// cache_control set. Reconstructing the block avoids any potential mutation
// issues with the SDK's omitzero serialization.
func setSystemCacheControl(req *anthropic.MessageNewParams, cc anthropic.CacheControlEphemeralParam) {
	n := len(req.System)
	if n == 0 {
		return
	}
	original := req.System[n-1]
	req.System[n-1] = anthropic.TextBlockParam{
		Text:         original.Text,
		CacheControl: cc,
		Citations:    original.Citations,
	}
}

// setToolsCacheControl replaces the last tool definition with a copy that has
// cache_control set.
func setToolsCacheControl(req *anthropic.MessageNewParams, cc anthropic.CacheControlEphemeralParam) {
	n := len(req.Tools)
	if n == 0 {
		return
	}
	last := req.Tools[n-1]
	if last.OfTool == nil {
		return
	}
	replacement := *last.OfTool
	replacement.CacheControl = cc
	last.OfTool = &replacement
	req.Tools[n-1] = last
}

// setMessageBlockCacheControl replaces the last content block in the given
// message with a copy that has cache_control set.
func setMessageBlockCacheControl(req *anthropic.MessageNewParams, msgIdx int, cc anthropic.CacheControlEphemeralParam) {
	if msgIdx < 0 || msgIdx >= len(req.Messages) {
		return
	}
	blocks := req.Messages[msgIdx].Content
	nb := len(blocks)
	if nb == 0 {
		return
	}
	last := blocks[nb-1]
	switch {
	case last.OfText != nil:
		replacement := *last.OfText
		replacement.CacheControl = cc
		last.OfText = &replacement
	case last.OfToolUse != nil:
		replacement := *last.OfToolUse
		replacement.CacheControl = cc
		last.OfToolUse = &replacement
	case last.OfToolResult != nil:
		replacement := *last.OfToolResult
		replacement.CacheControl = cc
		last.OfToolResult = &replacement
	default:
		return
	}
	req.Messages[msgIdx].Content[nb-1] = last
}
