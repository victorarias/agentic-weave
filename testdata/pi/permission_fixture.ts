import type { ExtensionAPI } from "@mariozechner/pi-coding-agent";

// permission_fixture.ts is a deterministic manual/integration-test helper.
// It provides a command that always triggers a real pi confirm dialog so the
// remote-control wrapper/relay permission lifecycle can be exercised without
// depending on model-specific tool selection behavior.
export default function permissionFixture(pi: ExtensionAPI) {
  pi.registerCommand("permtest", {
    description: "Trigger a deterministic confirm dialog for remote-control permission smoke tests",
    handler: async (_args, ctx) => {
      const allowed = await ctx.ui.confirm(
        "Permission test",
        "Allow this remote-control permission smoke test to proceed?",
      );
      if (allowed) {
        pi.sendUserMessage("Reply with exactly PERMISSION_ALLOWED.");
        return;
      }
      pi.sendUserMessage("Reply with exactly PERMISSION_DENIED.");
    },
  });
}
