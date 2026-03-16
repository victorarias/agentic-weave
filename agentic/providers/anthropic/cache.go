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

	// 1. Last system prompt block.
	if n := len(req.System); n > 0 {
		req.System[n-1].CacheControl = cc
	}

	// 2. Last tool definition.
	if n := len(req.Tools); n > 0 {
		last := req.Tools[n-1]
		if last.OfTool != nil {
			last.OfTool.CacheControl = cc
			req.Tools[n-1] = last
		}
	}

	// 3. Last content block in the final message.
	if n := len(req.Messages); n > 0 {
		blocks := req.Messages[n-1].Content
		if nb := len(blocks); nb > 0 {
			last := &blocks[nb-1]
			switch {
			case last.OfText != nil:
				last.OfText.CacheControl = cc
			case last.OfToolUse != nil:
				last.OfToolUse.CacheControl = cc
			case last.OfToolResult != nil:
				last.OfToolResult.CacheControl = cc
			}
		}
	}
}
