package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/blakewilliams/manifest/inspectors"
	"github.com/fatih/color"
	"github.com/urfave/cli/v2"
)

type CLI struct {
	app *cli.App
}

func New() *CLI {
	app := &cli.App{
		Name:  "manifest",
		Usage: "Runs rules against pull requests and diffs",
		Commands: []*cli.Command{
			{
				Name:  "inspect",
				Usage: "Runs the configured inspectors against the provided diff",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:    "config",
						Aliases: []string{"c"},
						Usage:   "Uses provided config `FILE`",
					},
					&cli.StringFlag{
						Name:    "diff",
						Aliases: []string{"d"},
						Usage:   "Uses the provided diff `FILE`",
					},
					&cli.BoolFlag{
						Name:  "json-only",
						Usage: "Outputs only the JSON and does not run the inspectors",
					},
					&cli.IntFlag{
						Name:  "concurrency",
						Usage: "Sets how many inspectors will run concurrently",
					},
					&cli.StringSliceFlag{
						Name:    "inspector",
						Aliases: []string{"i"},
						Usage:   "Runs the provided inspector `script`",
					},
					&cli.StringFlag{
						Name:  "formatter",
						Usage: "Sets the formatter to use",
					},
					&cli.BoolFlag{
						Name:  "strict",
						Usage: "fails if PR information or other optional data fails to be resolved",
					},
					&cli.BoolFlag{
						Name:  "no-github",
						Usage: "Don't use the GH CLI to fetch information like the auth token",
					},
				},
				Action: func(cctx *cli.Context) error {
					var in io.Reader

					fi, err := os.Stdin.Stat()
					if err != nil {
						panic(err)
					}
					if (fi.Mode() & os.ModeCharDevice) == 0 {
						in = os.Stdin
					} else if diff := cctx.String("diff"); diff != "" {
						f, err := os.Open(diff)
						if err != nil {
							return err
						}
						defer f.Close()
						in = f
					} else {
						if err := cli.ShowSubcommandHelp(cctx); err != nil {
							fmt.Println(err)
						}
						fmt.Printf("\n")
						return cli.Exit(color.New(color.FgRed).Sprint("No diff provided. Please provide a --diff or pass the diff via stdin."), 1)
					}

					inspectCmd := &InspectCmd{
						configPath:  cctx.String("config"),
						diffPath:    cctx.String("diff"),
						jsonOnly:    cctx.Bool("json-only"),
						concurrency: cctx.Int("concurrency"),
						formatter:   cctx.String("formatter"),
						strict:      cctx.Bool("strict"),
						noGH:        cctx.Bool("no-gh"),
						cCtx:        cctx,
					}

					return inspectCmd.Run(in)
				},
			},
			{
				Name:  "inspector",
				Usage: "runs the given built-in inspector",
				Subcommands: []*cli.Command{
					{
						Name:  "rails_job_perform",
						Usage: "Runs the Rails job inspector to ensure perform is modified safely for rolling deploys",
						Action: func(cctx *cli.Context) error {
							err := inspectors.Wrap("rails_job_perform", inspectors.RailsJobArguments)
							if err != nil {
								fmt.Fprintf(os.Stderr, "%s\n", err)
							}
							return nil
						},
					},

					{
						Name:  "pull-body",
						Usage: "Ensures that the pull request body is not empty",
						Action: func(cctx *cli.Context) error {
							err := inspectors.Wrap("pull-body", inspectors.PullBody)
							if err != nil {
								fmt.Fprintf(os.Stderr, "%s\n", err)
							}
							return nil
						},
					},
				},
			},
		},
	}

	return &CLI{app: app}
}

func (c *CLI) Run(args []string) error {
	return c.app.Run(args)
}
