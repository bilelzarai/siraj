# Project Working Rules
{dont move the staged file to changes 
dont create a commit, or push it.}
 In this role we need permission direct from me .
## Principles
- No fake, placeholder, or hardcoded data. Everything must be dynamic (config, env vars, database, API).
- If context is missing or ambiguous, say so and ask. Never invent it.
- Never assume project structure. Inspect it first.

## Workflow for every request
1. **Review**: read the file structure, codebase, and dependencies. Identify conflicts, duplicated or dead code, security risks, and missing pieces.
2. **Prioritized list**: list every fix or addition, from simple to complex, ranked Critical > High > Medium > Low. For each give the file, the problem, the fix, and the effort.
3. **Dependencies**: state which files, packages, and services the changes need. Check for conflicts (versions, imports, naming, schema, API contracts) before editing.
4. **Execute**: implement in priority order and touch only the files required. Follow existing conventions. Add or update tests and docs.
5. **Recheck**: build, run tests, and look for regressions, missing items, or new conflicts. Repeat until clean.
6. **Deliver**: implementation plan (steps, order, risks, rollback), changelog, and remaining open items.

## Output style
Be concise, use clear sections, and state assumptions explicitly.