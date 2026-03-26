Analyze the current changes and generate a professional English commit message.

1.  **Context Gathering**: Check `git status`, `git diff`, and `git diff --cached` to understand all pending changes.
2.  **Style Alignment**: Use conventional commits format: `<type>(<scope>): <description>`.
    - Types: `feat`, `fix`, `refactor`, `perf`, `test`, `docs`, `chore`
    - Scope: the module/package affected (e.g., `crawler`, `exporter`, `config`)
3.  **Drafting**: Create concise English commit message(s). Include a short header and bullet points for complex changes.
4.  **Output**: Display the commit message(s) in code block(s). Provide the corresponding `git commit` command(s) for convenience.
