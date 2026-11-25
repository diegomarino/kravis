package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/diegomarino/kravis/internal/domain"
	"github.com/diegomarino/kravis/internal/workspace"
)

func (a *app) initCommand() *cobra.Command {
	var in domain.InitInput
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Create a workspace in the current folder",
		Long: "Create a workspace in the current folder, or in KRAVIS_WORKSPACE: its configuration, the first own organization " +
			"and the first human user, who becomes the default actor on this machine. Rerunning it with the same data changes nothing.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var missing []string
			for _, name := range []string{"timezone", "organization", "user"} {
				if !cmd.Flags().Changed(name) {
					missing = append(missing, "--"+name)
				}
			}
			if len(missing) > 0 {
				return domain.InvalidFlag(strings.TrimPrefix(missing[0], "--"), nil, "missing required flag(s): %s", strings.Join(missing, ", "))
			}
			root, err := os.Getwd()
			if env := os.Getenv("KRAVIS_WORKSPACE"); env != "" {
				root = env
			} else if err != nil {
				return err
			}
			a.log.Debug("initializing", "root", root)
			res, err := domain.Init(a.connect(root), a.now, in)
			if err != nil {
				return err
			}
			// A matching rerun keeps an actor already stored; an interrupted
			// one finds none and gets it now.
			changed, err := workspace.SetDefaultActor(res.Workspace, res.UserID, res.Outcome == domain.OutcomeExisting)
			if err != nil {
				path, _ := workspace.ConfigPath()
				return &domain.Error{Code: domain.CodeStorageUnavailable, Cause: err,
					Message: fmt.Sprintf("the workspace exists at %s, but its default actor %s could not be saved: %v", root, res.UserID, err),
					Hint:    fmt.Sprintf("repair the local configuration file %s, then repeat init with the same arguments", path)}
			}
			a.log.Debug("default actor", "user", res.UserID, "stored", changed)
			return a.emit(cmd, reply{Data: res, Outcome: res.Outcome})
		},
	}
	f := cmd.Flags()
	f.StringVar(&in.Timezone, "timezone", "", "IANA timezone of the workspace, such as Europe/Madrid")
	f.StringVar(&in.Organization, "organization", "", "name of the first own organization")
	f.StringVar(&in.UserKey, "user", "", "key of the first human user (its ID is us_<key>)")
	f.StringVar(&in.UserName, "user-name", "", "name of the first user")
	return cmd
}

// whoamiData is the data of whoami.
type whoamiData struct {
	Actor  string `json:"actor"`
	Source string `json:"source"`
	Holder string `json:"holder"`
}

func (a *app) whoamiCommand() *cobra.Command {
	var set string
	cmd := &cobra.Command{
		Use:   "whoami",
		Short: "Show the actor, where it comes from and its holder",
		Long: "Show the resolved actor (--actor, then KRAVIS_ACTOR, then the local configuration), where it came from and its holder.\n" +
			"With --set <user>, store a live user as this workspace's default actor in the local configuration.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ws, err := a.open()
			if err != nil {
				return err
			}
			if cmd.Flags().Changed("set") {
				user, err := ws.Actor(set)
				if err != nil {
					return err
				}
				changed, err := workspace.SetDefaultActor(ws.Marker.Workspace, user.ID, false)
				if err != nil {
					return err
				}
				outcome := "unchanged"
				if changed {
					outcome = domain.OutcomeUpdated
				}
				return a.emit(cmd, reply{Data: whoamiData{user.ID, sourceConfiguration, user.Holder}, Outcome: outcome})
			}
			act, err := a.actor(ws, true)
			if err != nil {
				return err
			}
			return a.emit(cmd, reply{Data: whoamiData{act.User.ID, act.Source, act.User.Holder}})
		},
	}
	cmd.Flags().StringVar(&set, "set", "", "store this live user as the default actor")
	return cmd
}

func (a *app) checkCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "check",
		Short: "Report what is wrong in the workspace",
		Long:  "Report what is wrong in the workspace as findings (code, record, message). It exits with code 6 when there are any, and never writes.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, err := a.find()
			if err != nil {
				return err
			}
			findings, err := domain.Check(a.connect(root))
			if err != nil {
				return err
			}
			a.log.Debug("check", "root", root, "findings", len(findings))
			if len(findings) > 0 {
				a.exit = exitCheckFound
			}
			return a.emit(cmd, reply{Data: findings})
		},
	}
}
