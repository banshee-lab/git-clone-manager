package syncer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"gitsync/internal/gitx"
	"gitsync/internal/model"
)

// Request carries trigger metadata into the sanitized post-command environment.
type Request struct {
	Trigger     model.Trigger
	Source      model.Source
	TriggeredBy string
}

// Result describes the outcome of one repository sync.
type Result struct {
	Action         model.Action
	FailedStage    model.Stage
	Summary        string
	CommitBefore   string
	CommitAfter    string
	ResolvedBranch string
	Changed        bool
}

// Executor contains process settings shared by syncs.
type Executor struct {
	GitTimeout         time.Duration
	PostCommandTimeout time.Duration
	EnvPassthrough     []string
	Home               string
}

// NewExecutor creates Git's isolated home directory at startup.
func NewExecutor(dataDir string, gitTimeout, postTimeout time.Duration, passthrough []string) (*Executor, error) {
	home, err := gitx.MakeHome(dataDir)
	if err != nil {
		return nil, err
	}
	return &Executor{GitTimeout: gitTimeout, PostCommandTimeout: postTimeout, EnvPassthrough: append([]string(nil), passthrough...), Home: home}, nil
}

// Run clones or updates a repository and then optionally runs its post command.
func (e *Executor) Run(ctx context.Context, repo model.Repository, req Request, out io.Writer) (result Result, err error) {
	if e == nil {
		return result, errors.New("syncer: nil executor")
	}
	if out == nil {
		out = io.Discard
	}
	if e.GitTimeout <= 0 {
		return result, errors.New("syncer: git timeout must be positive")
	}
	if e.Home == "" {
		return result, errors.New("syncer: git home is required")
	}
	if err = validateURL(repo.URL); err != nil {
		return result, err
	}
	defer func() {
		status := "success"
		if err != nil {
			status = "failed"
		}
		_, _ = fmt.Fprintf(out, "[gitsync] result: %s (%s)\n", status, result.Summary)
	}()
	abs, pathErr := filepath.Abs(repo.Path)
	if pathErr != nil {
		return e.fail(out, &result, model.StageInternal, fmt.Errorf("invalid repository path: %w", pathErr))
	}
	repo.Path = abs
	info, statErr := os.Stat(repo.Path)
	clone := errors.Is(statErr, os.ErrNotExist)
	if statErr != nil && !clone {
		return e.fail(out, &result, model.StageInternal, fmt.Errorf("inspect repository path: %w", statErr))
	}
	if !clone {
		if !info.IsDir() {
			return e.fail(out, &result, model.StageClone, errors.New("clone path exists and is not a directory"))
		}
		empty, emptyErr := isEmpty(repo.Path)
		if emptyErr != nil {
			return e.fail(out, &result, model.StageInternal, fmt.Errorf("inspect repository directory: %w", emptyErr))
		}
		clone = empty
	}
	if clone {
		result.Action = model.ActionClone
		if err = os.MkdirAll(filepath.Dir(repo.Path), 0o755); err != nil {
			return e.fail(out, &result, model.StageClone, fmt.Errorf("create clone parent: %w", err))
		}
		if !clonePathAbsent(repo.Path) {
			if empty, _ := isEmpty(repo.Path); empty {
				_ = os.Remove(repo.Path)
			}
		}
		if _, err = fmt.Fprintln(out, "[gitsync] --- clone ---"); err != nil {
			return result, err
		}
		args := []string{"git", "clone"}
		if repo.Branch != "" {
			if err = validateBranch(ctx, repo, e, out, repo.Branch); err != nil {
				return e.fail(out, &result, model.StageClone, err)
			}
			args = append(args, "--branch", repo.Branch)
		}
		args = append(args, repo.URL, repo.Path)
		if err = gitx.Run(ctx, gitx.Spec{Dir: filepath.Dir(repo.Path), Args: args, Env: e.gitEnv(repo), Out: out, Timeout: e.gitTimeout(), Token: repo.AccessToken}); err != nil {
			return e.fail(out, &result, model.StageClone, stageError("clone", err))
		}
	} else {
		if _, repoErr := gitOutput(ctx, repo.Path, e.gitTimeout(), e.gitEnv(repo), "git", "rev-parse", "--is-inside-work-tree"); repoErr != nil {
			result.Action = model.ActionClone
			return e.fail(out, &result, model.StageClone, errors.New("clone path is not empty and is not a Git repository"))
		}
		result.Action = model.ActionPull
		origin, originErr := gitOutput(ctx, repo.Path, e.gitTimeout(), e.gitEnv(repo), "git", "config", "--get", "remote.origin.url")
		if originErr != nil {
			return e.fail(out, &result, model.StageInternal, fmt.Errorf("read origin URL: %w", originErr))
		}
		if strings.TrimSpace(origin) != repo.URL {
			return e.fail(out, &result, model.StageInternal, errors.New("origin URL differs from configured repository URL"))
		}
		before, beforeErr := gitOutput(ctx, repo.Path, e.gitTimeout(), e.gitEnv(repo), "git", "rev-parse", "--short", "HEAD")
		if beforeErr != nil {
			return e.fail(out, &result, model.StageInternal, fmt.Errorf("resolve current commit: %w", beforeErr))
		}
		result.CommitBefore = strings.TrimSpace(before)
		branch := repo.Branch
		if branch == "" {
			branch, err = e.defaultBranch(ctx, repo, out)
			if err != nil {
				return e.fail(out, &result, model.StageFetch, err)
			}
		}
		if err = validateBranch(ctx, repo, e, out, branch); err != nil {
			return e.fail(out, &result, model.StageCheckout, err)
		}
		result.ResolvedBranch = branch
		if _, err = fmt.Fprintln(out, "[gitsync] --- fetch ---"); err != nil {
			return result, err
		}
		if err = e.git(ctx, repo, out, "git", "fetch", "origin", "+refs/heads/"+branch+":refs/remotes/origin/"+branch); err != nil {
			return e.fail(out, &result, model.StageFetch, stageError("fetch", err))
		}
		current, _ := gitOutput(ctx, repo.Path, e.gitTimeout(), e.gitEnv(repo), "git", "symbolic-ref", "--short", "HEAD")
		if strings.TrimSpace(current) != branch {
			if _, err = fmt.Fprintln(out, "[gitsync] --- checkout ---"); err != nil {
				return result, err
			}
			if err = e.checkout(ctx, repo, out, branch); err != nil {
				return e.fail(out, &result, model.StageCheckout, stageError("checkout", err))
			}
		}
		if repo.HardReset {
			if _, err = fmt.Fprintln(out, "[gitsync] --- reset ---"); err != nil {
				return result, err
			}
			if err = e.git(ctx, repo, out, "git", "reset", "--hard", "origin/"+branch); err != nil {
				return e.fail(out, &result, model.StageReset, stageError("reset", err))
			}
		} else {
			if _, err = fmt.Fprintln(out, "[gitsync] --- merge ---"); err != nil {
				return result, err
			}
			if err = e.git(ctx, repo, out, "git", "merge", "--ff-only", "origin/"+branch); err != nil {
				return e.fail(out, &result, model.StageMerge, stageError("merge", err))
			}
		}
	}
	if result.Action == model.ActionClone {
		resolved, resolveErr := gitOutput(ctx, repo.Path, e.gitTimeout(), e.gitEnv(repo), "git", "symbolic-ref", "--short", "HEAD")
		if resolveErr != nil {
			return e.fail(out, &result, model.StageInternal, fmt.Errorf("resolve cloned branch: %w", resolveErr))
		}
		result.ResolvedBranch = strings.TrimSpace(resolved)
	}
	after, afterErr := gitOutput(ctx, repo.Path, e.gitTimeout(), e.gitEnv(repo), "git", "rev-parse", "--short", "HEAD")
	if afterErr != nil {
		return e.fail(out, &result, model.StageInternal, fmt.Errorf("resolve resulting commit: %w", afterErr))
	}
	result.CommitAfter = strings.TrimSpace(after)
	result.Changed = result.CommitBefore != result.CommitAfter
	result.Summary = successSummary(result)
	if repo.PostCommand != "" {
		if _, err = fmt.Fprintln(out, "[gitsync] --- post command ---"); err != nil {
			return result, err
		}
		timeout := time.Duration(repo.PostCommandTimeoutSeconds) * time.Second
		if timeout == 0 {
			timeout = e.PostCommandTimeout
		}
		if timeout <= 0 {
			return e.fail(out, &result, model.StagePostCommand, errors.New("post command timeout must be positive"))
		}
		env := e.postEnv(repo, req, result)
		cmdErr := gitx.Run(ctx, gitx.Spec{Dir: repo.Path, ShellCmd: repo.PostCommand, Env: env, Out: out, Timeout: timeout, Token: repo.AccessToken})
		if cmdErr != nil {
			result.FailedStage = model.StagePostCommand
			var timedOut *gitx.TimeoutError
			if errors.As(cmdErr, &timedOut) {
				result.Summary = "post command timed out after " + timeout.String()
			} else {
				var exitErr *exec.ExitError
				if errors.As(cmdErr, &exitErr) {
					result.Summary = fmt.Sprintf("post command exited with %d", exitErr.ExitCode())
				} else {
					result.Summary = "post command failed: " + cmdErr.Error()
				}
			}
			return result, fmt.Errorf("%s", result.Summary)
		}
	}
	return result, nil
}

func (e *Executor) fail(out io.Writer, r *Result, stage model.Stage, err error) (Result, error) {
	r.FailedStage = stage
	if r.Summary == "" {
		r.Summary = err.Error()
	}
	return *r, err
}

func (e *Executor) git(ctx context.Context, repo model.Repository, out io.Writer, args ...string) error {
	return gitx.Run(ctx, gitx.Spec{Dir: repo.Path, Args: args, Env: e.gitEnv(repo), Out: out, Timeout: e.gitTimeout(), Token: repo.AccessToken})
}

func (e *Executor) gitTimeout() time.Duration { return e.GitTimeout }

func (e *Executor) gitEnv(repo model.Repository) []string {
	env := make([]string, 0, len(os.Environ()))
	for _, item := range os.Environ() {
		key, _, ok := strings.Cut(item, "=")
		if ok && key != "GIT_CONFIG" && !strings.HasPrefix(key, "GIT_CONFIG_") {
			env = append(env, item)
		}
	}
	username := repo.AccessUsername
	if username == "" {
		username = "git"
	}
	pairs, _ := gitx.AuthEnv(repo.URL, username, repo.AccessToken, e.Home)
	return mergeEnv(env, pairs)
}

func (e *Executor) defaultBranch(ctx context.Context, repo model.Repository, out io.Writer) (string, error) {
	if _, err := fmt.Fprintln(out, "[gitsync] --- resolve branch ---"); err != nil {
		return "", err
	}
	var output strings.Builder
	args := []string{"git", "ls-remote", "--symref", "origin", "HEAD"}
	err := gitx.Run(ctx, gitx.Spec{Dir: repo.Path, Args: args, Env: e.gitEnv(repo), Out: &output, Timeout: e.gitTimeout(), Token: repo.AccessToken})
	if err != nil {
		return "", stageError("branch resolution", err)
	}
	for _, line := range strings.Split(output.String(), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 3 && fields[0] == "ref:" && strings.HasPrefix(fields[1], "refs/heads/") && fields[2] == "HEAD" {
			return strings.TrimPrefix(fields[1], "refs/heads/"), nil
		}
	}
	return "", errors.New("remote HEAD does not identify a default branch")
}

func (e *Executor) checkout(ctx context.Context, repo model.Repository, out io.Writer, branch string) error {
	args := []string{"git", "checkout"}
	if repo.HardReset {
		args = append(args, "-f")
	}
	if _, err := gitOutput(ctx, repo.Path, e.gitTimeout(), e.gitEnv(repo), "git", "show-ref", "--verify", "--quiet", "refs/heads/"+branch); err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
			return err
		}
		args = append(args, "-b", branch, "--track", "origin/"+branch)
	} else {
		args = append(args, branch)
	}
	return e.git(ctx, repo, out, args...)
}

func validateBranch(ctx context.Context, repo model.Repository, e *Executor, out io.Writer, branch string) error {
	if strings.TrimSpace(branch) != branch || branch == "" {
		return errors.New("branch name is empty or has surrounding whitespace")
	}
	dir := repo.Path
	if _, err := os.Stat(dir); err != nil {
		dir = e.Home
	}
	return gitx.Run(ctx, gitx.Spec{Dir: dir, Args: []string{"git", "check-ref-format", "--branch", branch}, Env: e.gitEnv(repo), Out: io.Discard, Timeout: e.gitTimeout(), Token: repo.AccessToken})
}

func gitOutput(ctx context.Context, dir string, timeout time.Duration, env []string, args ...string) (string, error) {
	var output strings.Builder
	if err := gitx.Run(ctx, gitx.Spec{Dir: dir, Args: args, Env: env, Out: &output, Timeout: timeout}); err != nil {
		return "", err
	}
	text := output.String()
	if i := strings.IndexByte(text, '\n'); i >= 0 {
		text = text[i+1:]
	}
	return text, nil
}

func (e *Executor) postEnv(repo model.Repository, req Request, result Result) []string {
	base := []string{"PATH=" + os.Getenv("PATH"), "HOME=" + e.Home}
	base = append(base,
		"GITSYNC_REPO_NAME="+repo.Name,
		"GITSYNC_REPO_PATH="+repo.Path,
		"GITSYNC_BRANCH="+result.ResolvedBranch,
		"GITSYNC_COMMIT_BEFORE="+result.CommitBefore,
		"GITSYNC_COMMIT_AFTER="+result.CommitAfter,
		fmt.Sprintf("GITSYNC_CHANGED=%t", result.Changed),
		"GITSYNC_TRIGGER="+string(req.Trigger),
		"GITSYNC_SOURCE="+string(req.Source),
	)
	allowed := map[string]bool{}
	for _, key := range e.EnvPassthrough {
		allowed[key] = true
	}
	for _, item := range os.Environ() {
		key, _, ok := strings.Cut(item, "=")
		if ok && allowed[key] && key != "PATH" && key != "HOME" && !strings.HasPrefix(key, "GITSYNC_OIDC_") && !strings.HasPrefix(key, "GITSYNC_SERVER_") && !strings.HasPrefix(key, "GITSYNC_WEBHOOKS_") && !strings.HasPrefix(key, "GITSYNC_NOTIFICATIONS_") {
			base = append(base, item)
		}
	}
	return base
}

func mergeEnv(env, values []string) []string {
	m := make(map[string]string, len(env)+len(values))
	for _, item := range env {
		key, value, ok := strings.Cut(item, "=")
		if ok {
			m[key] = value
		}
	}
	for _, item := range values {
		key, value, ok := strings.Cut(item, "=")
		if ok {
			m[key] = value
		}
	}
	out := make([]string, 0, len(m))
	for key, value := range m {
		out = append(out, key+"="+value)
	}
	return out
}

func validateURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil {
		return errors.New("repository URL must be an absolute http(s) URL without embedded credentials")
	}
	return nil
}

func isEmpty(dir string) (bool, error) {
	f, err := os.Open(dir)
	if err != nil {
		return false, err
	}
	defer f.Close()
	_, err = f.Readdirnames(1)
	if errors.Is(err, io.EOF) {
		return true, nil
	}
	return false, err
}

func clonePathAbsent(path string) bool {
	_, err := os.Stat(path)
	return errors.Is(err, os.ErrNotExist)
}

func stageError(stage string, err error) error {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return fmt.Errorf("%s failed: exit status %d", stage, exitErr.ExitCode())
	}
	var timeout *gitx.TimeoutError
	if errors.As(err, &timeout) {
		return fmt.Errorf("%s timed out after %s", stage, timeout.Timeout)
	}
	return fmt.Errorf("%s failed: %w", stage, err)
}

func successSummary(r Result) string {
	if r.Action == model.ActionClone {
		return "Cloned (" + r.CommitAfter + ")"
	}
	if !r.Changed {
		return "Already up to date (" + r.CommitAfter + ")"
	}
	return "Updated " + r.CommitBefore + " → " + r.CommitAfter
}
