package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/blakewilliams/manifest"
	"github.com/blakewilliams/manifest/formatters/githubformat"
	"github.com/blakewilliams/manifest/formatters/prettyformat"
	"github.com/blakewilliams/manifest/githelpers"
	"github.com/blakewilliams/manifest/github"
	"github.com/fatih/color"
	"github.com/urfave/cli/v2"
)

type InspectCmd struct {
	configPath  string
	diffPath    string
	jsonOnly    bool
	concurrency int
	formatter   string
	inspectors  []string
	sha         string
	strict      bool
	noGH        bool
	cCtx        *cli.Context

	_githubClient   github.Client
	_githubPRNumber int
}

func (c *InspectCmd) Run(in io.Reader) error {
	manifestConfig := &manifest.Configuration{
		Concurrency: 1,
		Formatter:   prettyformat.New(os.Stdout),
		Inspectors:  map[string]string{},
	}

	if err := applyConfig(c.configPath, manifestConfig); err != nil {
		return cli.Exit(err, 1)
	}
	if c.noGH {
		manifestConfig.NoGH = true
	}
	if err := c.resolveFormatter(manifestConfig); err != nil {
		return cli.Exit(err, 1)
	}
	c.resolveInspectors(manifestConfig)
	if c.concurrency > 0 {
		manifestConfig.Concurrency = c.concurrency
	}
	if c.strict {
		manifestConfig.Strict = true
	}

	inspection, err := manifest.NewInspection(manifestConfig, in)
	if err != nil {
		color.New(color.FgRed).Println(err.Error())
		return cli.ShowSubcommandHelp(c.cCtx)
	}

	if err := c.populateGitHubData(inspection); err != nil {
		// If we fail to resolve any GitHub data, we can still run the
		// inspection locally. If we're in strict mode, we should exit with an
		// error.
		if c.strict {
			return cli.Exit(err, 1)
		}

		fmt.Fprintf(os.Stderr, "warning: could not resolve GitHub PR information: %s\n", err)
	}

	// Run the relevant command
	if c.jsonOnly {
		out, err := inspection.ImportJSON()
		if err != nil {
			fmt.Printf("Could not return import JSON: %s\n", err)
		}

		fmt.Println(string(out))
		return nil
	}

	// Validate we have inspectors to run
	if len(manifestConfig.Inspectors) == 0 {
		if err := cli.ShowSubcommandHelp(c.cCtx); err != nil {
			fmt.Println(err)
		}
		fmt.Printf("\n")
		return cli.Exit(color.New(color.FgRed).Sprint("No inspectors were provided. Add one to manifest.config.yaml or passed via --inspector"), 1)
	}

	// Run the real inspection
	if err := inspection.Perform(); err != nil {
		return cli.Exit(color.New(color.FgRed).Sprintf("Manifest's inspection encountered an error: %s\n", err.Error()), 1)
	}

	color.New(color.FgGreen).Fprintf(os.Stderr, "manifest inspection passed!\n")
	return nil
}

func (c *InspectCmd) populateGitHubData(i *manifest.Inspection) error {
	client, err := c.GitHubClient()
	if err != nil {
		return err
	}

	sha, err := c.CurrentSha()
	if err != nil {
		return err
	}

	prNum, err := c.GitHubPRNumber()
	if err != nil {
		return err
	}

	return i.PopulatePullDetails(client, sha, prNum)
}

func (c *InspectCmd) resolveInspectors(config *manifest.Configuration) {
	if len(c.inspectors) > 0 {
		config.Inspectors = make(map[string]string, len(c.inspectors))

		for _, inspector := range c.inspectors {
			config.Inspectors[inspector] = inspector
		}
	}
}

func (c *InspectCmd) resolveFormatter(config *manifest.Configuration) error {
	if c.formatter == "" {
		config.Formatter = prettyformat.New(os.Stdout)
		return nil
	}

	switch c.formatter {
	case "pretty":
		config.Formatter = prettyformat.New(os.Stdout)
	case "github":
		gh, err := c.GitHubClient()
		if err != nil {
			return cli.Exit(fmt.Errorf("cannot use GitHub formatter: %w", err), 1)
		}

		config.Formatter = githubformat.New(gh)
	default:
		return fmt.Errorf("unknown formatter %s", c.formatter)
	}

	return nil
}

var errNoGitHubToken = errors.New("no GitHub token found in MANIFEST_GITHUB_TOKEN")

func (c *InspectCmd) GitHubClient() (github.Client, error) {
	if c._githubClient == nil {
		// Ensure we have a token to fetch with
		token := os.Getenv("MANIFEST_GITHUB_TOKEN")
		if token == "" && c.noGH {
			return nil, errNoGitHubToken
		}

		if token == "" {
			rawToken, err := exec.Command("gh", "auth", "token").Output()
			if err != nil {
				return nil, fmt.Errorf("could not use gh to get token: %w", err)
			}
			token = strings.TrimSpace(string(rawToken))
		}

		// Get the owner and repo details so we can fetch from the API
		owner, repo, err := githelpers.NwoFromOrigin()
		if err != nil {
			return nil, fmt.Errorf("could not get owner and repo from git origin: %w", err)
		}

		c._githubClient = github.NewClient(token, owner, repo)
	}

	return c._githubClient, nil
}

func (c *InspectCmd) GitHubPRNumber() (int, error) {
	if c._githubPRNumber != 0 {
		return c._githubPRNumber, nil
	}

	client, err := c.GitHubClient()
	if err != nil {
		return 0, err
	}

	branch, err := githelpers.CurrentBranch()
	if err != nil {
		return 0, err
	}

	numbers, err := client.PullRequestIDsForBranch(branch)
	if err != nil {
		return 0, err
	}

	if len(numbers) == 0 {
		return 0, github.ErrNoPR
	}

	c._githubPRNumber = numbers[0]

	return numbers[0], nil
}

func (c *InspectCmd) CurrentSha() (string, error) {
	sha := c.sha
	if sha == "" {
		var err error
		// Get the most recent pushed SHA so we can fetch the PR details from GitHub
		sha, err = githelpers.UpstreamSha()
		if err != nil && err != githelpers.ErrNoPushedBranch {
			return "", fmt.Errorf("could not find most recently pushed sha. did you push?")
		}

		c.sha = sha
	}

	return sha, nil
}

func applyConfig(configArg string, rootConfig *manifest.Configuration) error {
	if configArg != "" {
		f, err := os.Open(configArg)
		if err != nil {
			return cli.Exit(fmt.Sprintf("Could not open the provided config file: %s", err), 1)
		}
		defer f.Close()
		manifest.ParseConfig(f, rootConfig, map[string]manifest.Formatter{"pretty": prettyformat.New(os.Stdout)})

		return nil
	}

	cwd, err := os.Getwd()
	if err != nil {
		return cli.Exit("Could not get current working directory", 1)
	}
	rootDir, err := findGitDir(cwd)
	if err != nil && err != os.ErrNotExist {
		return cli.Exit(fmt.Sprintf("error when looking for root dir: %s", err), 1)
	}

	if err == os.ErrNotExist {
		return nil
	}

	configPath := filepath.Join(rootDir, "manifest.config.yaml")
	if _, err := os.Stat(configPath); err == nil {
		f, err := os.Open(configPath)
		if err != nil {
			return cli.Exit(fmt.Sprintf("Could not open the config file found in the root folder: %s", err), 1)
		}
		defer f.Close()

		manifest.ParseConfig(f, rootConfig, map[string]manifest.Formatter{"pretty": prettyformat.New(os.Stdout)})
	}

	return nil
}

func findGitDir(startDir string) (string, error) {
	dir := startDir
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", os.ErrNotExist
}
