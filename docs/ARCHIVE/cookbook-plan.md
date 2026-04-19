# Cookbook Plan

Goal: plan a scenario-based cookbook to teach APiX through concrete recipes. This is planned after the core learning docs are in place.

Proposed scope (initial):
1. Debugging auth failures — capture auth flow, inspect tokens, replay with modified headers
2. Mocking an unavailable backend — respond with synthetic responses to test clients
3. Replay and compare responses — reproducible replays and diffing responses
4. Inspect remote traffic — use the VS Code extension to view engine-captured traffic

Timing and sequencing
- PRs: produce one cookbook recipe per small PR, target after docs restructure and getting-started pages
- Prioritization: auth debugging, mocking, replaying, inspecting remote traffic

Acceptance criteria
- A prioritized list of recipes with short outlines
- Each recipe maps to required docs and example commands
- Cookbook work is explicitly sequenced after docs/ restructuring issues (#123–#127)

(Planned via issue #128)