# Changelog

## Unreleased

- `extraBody` is now additive-only for provider-specific parameters. It can no longer override CodeRenga-managed chat request fields: `model`, `messages`, `stream`, `tools`, `tool_choice`, or `parallel_tool_calls`.
- `llamacpp_tools` Phase 1 always forces non-stream requests and, when native tools are present, `parallel_tool_calls:false` even if `parallelToolCalls:true` is configured. The `parallelToolCalls` setting is reserved for future native-tools providers or a later llama.cpp tools phase.
- Native tool schemas are generated internally from the CodeRenga tool registry. JSON configuration cannot inject `nativeTools`, and `extraBody.tools` is ignored as a managed field.
- Native llama.cpp tool calls now reject safe-name collisions such as `foo.bar` and `foo__bar` both mapping to `foo__bar`.