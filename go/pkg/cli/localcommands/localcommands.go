package localcommands

type Command struct {
	Command    string
	Subcommand string
	Rationale  string
}

var commands = []Command{
	{Command: "workflow", Subcommand: "validate", Rationale: "local workflow-authoring validation reads the workflow file before daemon state exists"},
	{Command: "workflow", Subcommand: "generate", Rationale: "local workflow generation renders a starter workflow from an embedded catalog shape; it previews (or writes repo files) and touches no daemon state"},
	{Command: "workflow", Subcommand: "templates", Rationale: "local catalog read lists/shows workflow template shapes and lane sets from embedded data; it touches no daemon state"},
	{Command: "skills", Subcommand: "install", Rationale: "skills install renders embedded skill templates into the user/project skills dir; it writes no daemon state"},
	{Command: "skills", Subcommand: "list", Rationale: "skills list reads the embedded optional-skill catalog and the on-disk manifests to report installed state; it writes no daemon state"},
	{Command: "plugin", Subcommand: "install", Rationale: "plugin install renders embedded plugin bundles into the user/project plugin dir; it writes no daemon state"},
	{Command: "plugin", Subcommand: "uninstall", Rationale: "plugin uninstall removes a previously rendered bundle by reading its on-disk manifest; it writes no daemon state"},
	{Command: "operator", Subcommand: "bootstrap", Rationale: "operator bootstrap is a custom CLI-local cold-start entrypoint (not a generated 1:1 route) that composes daemon reads plus local repository/documentation probes; under RFC 0167 P0 it also calls the operator.bootstrap RPC to mint + lease + present the session-bound operator token, so the live operator-session/handle state it creates is daemon-owned via that RPC, not written by the CLI"},
	{Command: "records", Subcommand: "migration", Rationale: "records migration is a custom nested command group: inventory is a local read-only filesystem dry-run, while import/verify/materialize are explicit daemon RPC clients for RFC 0171 generated-record migration proof"},
	{Command: "daemon", Subcommand: "install", Rationale: "daemon install is a bootstrap helper that renders the systemd user unit and scaffolds daemon.toml before any daemon exists; it writes no daemon RPC state"},
	{Command: "daemon", Subcommand: "uninstall", Rationale: "daemon uninstall disables and removes the systemd user unit; it touches no daemon RPC state and leaves config/data intact"},
	{Command: "daemon", Subcommand: "status", Rationale: "daemon status summarizes the local unit/runtime layout and folds in read-only doctor; it issues no state-changing daemon RPC"},
	{Command: "daemon", Subcommand: "migrate-db", Rationale: "daemon migrate-db applies pending PostgreSQL migrations via an owner/admin DSN before the daemon serves (RFC 0079 §5); it is a bootstrap helper, not a daemon RPC"},
	{Command: "daemon", Subcommand: "owner-ddl", Rationale: "daemon owner-ddl apply installs the versioned owner-DDL bundles (RFC 0110 §8.1) via the owner DSN out-of-band; the runtime role cannot perform owner DDL, so it is a bootstrap helper, not a daemon RPC"},
}

func All() []Command {
	return append([]Command(nil), commands...)
}

func Lookup(args []string) (Command, bool) {
	if len(args) < 2 {
		return Command{}, false
	}
	for _, command := range commands {
		if args[0] == command.Command && args[1] == command.Subcommand {
			return command, true
		}
	}
	return Command{}, false
}
