# Autonomous development

The main agent owns product coherence, architecture, integration, and the
conversation with the user.

Use subagents for independent implementation, exploration, testing, security
review, and documentation when this saves time or improves quality.

Keep the main conversation focused on requirements, decisions, progress, and
final evidence. Subagents should return concise summaries and file references.

Only one agent may own a subsystem or overlapping group of files at a time.
Parallelize read-only review freely. Coordinate parallel writes through the
main agent.

Do not wait for user input when a reasonable, reversible default exists.
Record meaningful assumptions in docs/DECISIONS.md.

After each meaningful change:
- run the relevant tests;
- update the scorecard;
- inspect generated applications;
- retain the best passing state.

Stop when the current milestone's written acceptance criteria pass. Do not
expand into later roadmap items merely because more improvements are possible.