package main

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"runtime/debug"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/diegomarino/kravis/internal/domain"
	"github.com/diegomarino/kravis/internal/storage"
	"github.com/diegomarino/kravis/internal/workspace"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

// buildVersion is the injected version, else the module version of the build
// info (go install …@vX.Y.Z), else dev; without the leading v.
func buildVersion() string {
	v := version
	if v == "dev" || v == "" {
		v = "dev"
		if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
			v = info.Main.Version
		}
	}
	if v != "dev" {
		v = strings.TrimPrefix(v, "v")
	}
	return v
}

// deps is what the CLI runs on. Tests swap them to force conflicts,
// failures and a fixed clock in process.
type deps struct {
	// connect opens the storage of the workspace folder root.
	connect func(root string) storage.Connector
	// now is the clock of every write.
	now func() time.Time
	// isTTY says whether w is a terminal.
	isTTY func(w io.Writer) bool
}

func defaultDeps() deps {
	return deps{
		connect: func(root string) storage.Connector { return storage.NewLocal(root) },
		now:     time.Now,
		isTTY: func(w io.Writer) bool {
			f, ok := w.(*os.File)
			return ok && term.IsTerminal(int(f.Fd()))
		},
	}
}

// app holds the state of one run of the CLI.
type app struct {
	deps
	// Persistent flags.
	actorFlag string
	output    string
	verbose   bool

	stdout io.Writer
	log    *slog.Logger
	// started is set once a command's own code runs: an error before that is
	// the parser's.
	started bool
	// exit is the exit code of a successful run; check sets 6.
	exit int
}

func newApp(d deps) *app {
	return &app{deps: d, log: slog.New(slog.DiscardHandler)}
}

// run executes the CLI in process and returns its exit code. Every test
// calls it, or runWith; main only forwards the process arguments and streams.
func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	return runWith(defaultDeps(), args, stdin, stdout, stderr)
}

// runWith is run on the given dependencies.
func runWith(d deps, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	a := newApp(d)
	return a.execute(a.root(), args, stdin, stdout, stderr)
}

// execute runs root and turns every failure into one error object on
// stderr and an exit code: usage errors, domain errors and panics alike.
func (a *app) execute(root *cobra.Command, args []string, stdin io.Reader, stdout, stderr io.Writer) (code int) {
	root.SetArgs(args)
	root.SetIn(stdin)
	root.SetOut(stdout)
	root.SetErr(stderr)
	a.stdout = stdout
	defer func() {
		if r := recover(); r != nil {
			code = writeError(stderr, &domain.Error{Code: domain.CodeInternal, Message: fmt.Sprint("unexpected failure: ", r)})
		}
	}()

	if err := root.Execute(); err != nil {
		if !a.started {
			err = parserError(root, err)
		}
		return writeError(stderr, err)
	}
	return a.exit
}

// root builds the command tree; commands reach storage only through a.
func (a *app) root() *cobra.Command {
	root := &cobra.Command{
		Use:           "kravis",
		Short:         "A CRM for agents",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			a.started = true
			return a.configure(cmd)
		},
	}
	root.SetVersionTemplate("") // the version is printed as JSON below
	root.CompletionOptions.DisableDefaultCmd = true
	// Only --help prints text: there is no help command.
	root.SetHelpCommand(&cobra.Command{Use: "no-help", Hidden: true})
	flags := root.PersistentFlags()
	flags.StringVar(&a.actorFlag, "actor", "", "the user who performs a write (default: KRAVIS_ACTOR, then the local configuration)")
	flags.StringVar(&a.output, "output", "", "output format: json or table (default: table on a terminal, json otherwise)")
	flags.BoolVar(&a.verbose, "verbose", false, "log what Kravis does on stderr")
	root.Flags().Bool("version", false, "print the version")
	root.RunE = func(cmd *cobra.Command, _ []string) error {
		if v, _ := cmd.Flags().GetBool("version"); v {
			return a.emit(cmd, reply{Data: map[string]any{"version": buildVersion()}})
		}
		return domain.UsageError("a command is required", "run kravis --help")
	}
	root.AddCommand(a.initCommand(), a.whoamiCommand(), a.checkCommand())
	return root
}

// configure validates the global flags and turns logging on.
func (a *app) configure(cmd *cobra.Command) error {
	if v := os.Getenv("KRAVIS_OUTPUT"); a.output == "" && v != "" {
		a.output = v
	}
	if a.output != "" && a.output != "json" && a.output != "table" {
		return domain.InvalidFlag("output", []string{"json", "table"}, "%q is not an output format", a.output)
	}
	if a.verbose || os.Getenv("KRAVIS_LOG") == "debug" {
		a.log = slog.New(slog.NewTextHandler(cmd.ErrOrStderr(), &slog.HandlerOptions{Level: slog.LevelDebug}))
	}
	return nil
}

// find returns the folder of the workspace the command runs on.
func (a *app) find() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	root, source, ok := workspace.Find(cwd, os.Getenv("KRAVIS_WORKSPACE"))
	if !ok {
		return "", domain.NoWorkspace()
	}
	a.log.Debug("workspace folder", "root", root, "found by", source)
	return root, nil
}

// open finds and opens the workspace the command runs on.
func (a *app) open() (*domain.Workspace, error) {
	root, err := a.find()
	if err != nil {
		return nil, err
	}
	ws, err := domain.Open(a.connect(root), a.now)
	if err != nil {
		return nil, err
	}
	a.log.Debug("workspace opened", "workspace", ws.Marker.Workspace, "timezone", ws.Config.Timezone)
	return ws, nil
}

// Where an actor came from.
const (
	sourceFlag          = "flag"
	sourceEnvironment   = "environment"
	sourceConfiguration = "configuration"
)

// actor is the resolved actor of a command.
type actor struct {
	User   *domain.User
	Source string
}

// actor resolves the actor: --actor, then KRAVIS_ACTOR, then the local
// configuration of the workspace. A write passes required: with no actor it
// fails with MISSING_ACTOR. A read passes false and gets nil when there is
// none. The actor must exist and be live.
func (a *app) actor(ws *domain.Workspace, required bool) (*actor, error) {
	id, source := a.actorFlag, sourceFlag
	if id == "" {
		id, source = os.Getenv("KRAVIS_ACTOR"), sourceEnvironment
	}
	if id == "" {
		var err error
		if id, err = workspace.DefaultActor(ws.Marker.Workspace); err != nil {
			return nil, err
		}
		source = sourceConfiguration
	}
	if id == "" {
		if required {
			return nil, &domain.Error{
				Code:    domain.CodeMissingActor,
				Message: "this command writes, and no actor was given",
				Hint:    "pass --actor <user>, or set KRAVIS_ACTOR, or store a default with kravis whoami --set <user>",
			}
		}
		return nil, nil
	}
	user, err := ws.Actor(id)
	if err != nil {
		return nil, err
	}
	a.log.Debug("actor", "user", user.ID, "from", source, "holder", user.Holder)
	return &actor{User: user, Source: source}, nil
}

// parserError turns what cobra reports before a command runs into a usage
// error: an unknown flag is UNKNOWN_FIELD, everything else INVALID_VALUE.
func parserError(root *cobra.Command, err error) error {
	msg := err.Error()
	for _, prefix := range []string{"unknown flag: --", "unknown shorthand flag: '"} {
		if rest, ok := strings.CutPrefix(msg, prefix); ok {
			field, _, _ := strings.Cut(rest, "'")
			return &domain.Error{
				Code: domain.CodeUnknownField, Message: msg, Hint: "run kravis --help",
				Details: map[string]any{"field": field},
			}
		}
	}
	e := &domain.Error{Code: domain.CodeInvalidValue, Message: msg, Hint: "run kravis --help"}
	if strings.HasPrefix(msg, "unknown command") {
		var names []string
		for _, c := range root.Commands() {
			if !c.Hidden {
				names = append(names, c.Name())
			}
		}
		e.Details = map[string]any{"allowed": names}
	}
	return e
}
