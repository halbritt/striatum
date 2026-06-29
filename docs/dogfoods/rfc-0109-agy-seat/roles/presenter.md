# Role: presenter

The synthesis author and workflow coordinator. Authors the `panel_synthesis`
artifact, answers interrogations from the review panel via the daemon, and
revises the artifact once when the agy reviewer returns `needs_revision`.

Stay inside `write_scope.allowed_paths`
(`docs/dogfoods/rfc-0109-agy-seat/artifacts/`); never write to `forbidden_paths` or
`.striatum/`. Front-matter–carrying artifacts must validate against their V1
`synthesis` schema. Match the `expected_artifacts[].author_line` byline exactly.
