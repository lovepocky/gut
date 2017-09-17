package main

import (
	"errors"
	"fmt"
	"path"
	"strings"
)

func (ctx *SyncContext) GutRevParseHead() (commit string, err error) {
	stdout, err := ctx.GutOutput("rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(stdout), nil
}

// Start a git-daemon on the host, bound to port gutd_bind_port on the *localhost* network interface only.
// `autossh` will create a tunnel to expose this port as gutd_connect_port on the other host.
func (ctx *SyncContext) GutDaemon(repoName string, bindPort int) (err error) {
	basePath := ctx.AbsPath(GutDaemonPath)
	ctx.Mkdirp(basePath)
	symlinkPath := path.Join(basePath, repoName)
	exists, err := ctx.PathExists(symlinkPath)
	if err != nil {
		return err
	}

	if exists {
		err = ctx.DeleteLink(symlinkPath)
		if err != nil {
			return err
		}
	}
	err = ctx.Symlink(ctx.AbsSyncPath(), symlinkPath)
	if err != nil {
		return err
	}
	args := []string{
		"daemon",
		//add for debug
		"--verbose",
		"--export-all",
		"--base-path=" + basePath,
		"--reuseaddr",
		"--listen=localhost",
		"--enable=receive-pack",
		fmt.Sprintf("--port=%d", bindPort),
		basePath,
	}
	pid, _, err := ctx.QuoteDaemonCwd("daemon", "", ctx.GutArgs(args...)...)
	if err != nil {
		return err
	}
	return ctx.SaveDaemonPid("daemon", pid)
}

func (ctx *SyncContext) GutInit() (err error) {
	ctx.Mkdirp(ctx.AbsSyncPath())
	exists, err := ctx.PathExists(path.Join(ctx.AbsSyncPath(), ".gut"))
	if err != nil {
		return err
	}
	if exists {
		return errors.New("Gut repository already exists in " + ctx.AbsSyncPath())
	}
	_, err = ctx.GutQuote("init", "init")
	return err
}

func (ctx *SyncContext) GutSetupOrigin(repoName string, connectPort int) (err error) {
	originUrl := fmt.Sprintf("gut://localhost:%d/%s/", connectPort, repoName)
	out, err := ctx.GutOutput("remote")
	if err != nil {
		return err
	}
	if strings.Contains(out, "origin") {
		_, err = ctx.GutOutput("remote", "set-url", "origin", originUrl)
	} else {
		_, err = ctx.GutOutput("remote", "add", "origin", originUrl)
	}
	if err != nil {
		return err
	}
	_, err = ctx.GutOutput("config", "color.ui", "always")
	if err != nil {
		return err
	}
	hostname, err := ctx.Output("hostname")
	if err != nil {
		return err
	}
	_, err = ctx.GutOutput("config", "user.name", hostname)
	if err != nil {
		return err
	}
	_, err = ctx.GutOutput("config", "user.email", "gut-sync@"+hostname)
	return err
}

var NeedsCommitError = errors.New("Needs commit before pull.")

func (ctx *SyncContext) GutMerge(branch string) (err error) {
	status := ctx.NewLogger("merge")
	status.Printf("@(dim:Merging changes to) %s@(dim:...)\n", ctx.NameAnsi())
	mergeArgs := []string{
		"merge",
		branch,
		"--strategy=recursive",
		"--strategy-option=theirs", // or "ours"? not sure either is better?
		"--no-edit",
	}
	_, stderr, retCode, err := ctx.GutQuoteBuf("merge", mergeArgs...)
	if err != nil {
		return err
	}
	needCommit := retCode != 0 && strings.Contains(string(stderr), needsCommitStr)
	if needCommit {
		// status.Printf("@(error:Failed to merge due to uncommitted changes.)\n")
		return NeedsCommitError
	}
	return nil
}

func (ctx *SyncContext) GutCheckoutAsMaster(branch string) (err error) {
	status := ctx.NewLogger("checkout")
	status.Printf("@(dim:Checking out) %s @(dim:on) %s@(dim:...)\n", branch, ctx.NameAnsi())
	_, err = ctx.GutQuote("checkout", "checkout", "-b", "master", branch)
	return err
}

func (ctx *SyncContext) GutPush() (err error) {
	status := ctx.NewLogger("push")
	status.Printf("@(dim:Pushing changes from) %s@(dim:...)\n", ctx.NameAnsi())
	_, err = ctx.GutQuote("push", "push", "origin", "master:"+ctx.BranchName(), "--progress")
	return err
}

var needsCommitStr = "Your local changes to the following files would be overwritten"

func (ctx *SyncContext) GutFetch() (err error) {
	status := ctx.NewLogger("fetch")
	status.Printf("@(dim:Fetching changes to) %s@(dim:...)\n", ctx.NameAnsi())
	_, stderr, retCode, err := ctx.GutQuoteBuf("fetch", "fetch", "origin", "--progress")
	if strings.Contains(string(stderr), "Cannot lock ref") {
		status.Printf("RETCODE FOR LOCK FAILURE IS: %d\n", retCode)
	}
	return err
}

func (ctx *SyncContext) GutPull() (err error) {
	err = ctx.GutFetch()
	if err != nil {
		return err
	}
	//here maybe used by remote (not local)
	return ctx.GutMerge("origin/master")
}

func (ctx *SyncContext) GutCommit(prefix string, updateUntracked bool) (changed bool, err error) {
	status := ctx.NewLogger("commit")
	headBefore, err := ctx.GutRevParseHead()
	if err != nil {
		return false, err
	}
	if updateUntracked {
		lsFiles, err := ctx.GutOutput("ls-files", "-i", "--exclude-standard", "--", prefix)
		if err != nil {
			return false, err
		}
		lsFiles = strings.TrimSpace(lsFiles)
		if lsFiles != "" {
			for _, filename := range strings.Split(lsFiles, "\n") {
				status.Printf("@(dim:Untracking newly-.gutignored) %s\n", filename)
				rmArgs := []string{"rm", "--cached", "--ignore-unmatch", "--quiet", "--", filename}
				_, err = ctx.GutQuote("rm--cached", rmArgs...)
				if err != nil {
					return false, err
				}
			}
		}
	}
	status.Printf("@(dim:Checking) %s @(dim)for changes (scope=@(r)%s@(dim))...\n", ctx.NameAnsi(), prefix)
	_, err = ctx.GutQuote("add", "add", "--all", "--", prefix)
	if err != nil {
		return false, err
	}
	_, err = ctx.GutQuote("commit", "commit", "--message", "autocommit")
	if err != nil {
		return false, err
	}
	headAfter, err := ctx.GutRevParseHead()
	if err != nil {
		return false, err
	}
	// status.Printf("before: %s, after: %s", headBefore, headAfter)
	madeACommit := headBefore != headAfter
	if madeACommit {
		status.Printf("@(dim:Committed) @(commit:%s)@(dim:.)\n", TrimCommit(headAfter))
	} else {
		status.Printf("@(dim:No changes.)\n")
	}
	return madeACommit, nil
}

func (ctx *SyncContext) GutEnsureInitialCommit() (err error) {
	status := ctx.NewLogger("firstcommit")
	status.Printf("@(dim:Writing first commit on) %s@(dim:.)\n", ctx.SyncPathAnsi())
	head, err := ctx.GutRevParseHead()
	if err != nil {
		return err
	}
	if head == "HEAD" {
		gutignorePath := path.Join(ctx.AbsSyncPath(), ".gutignore")
		exists, err := ctx.PathExists(gutignorePath)
		if err != nil {
			return err
		}
		if !exists {
			err = ctx.WriteFile(gutignorePath, []byte(DefaultGutignore))
			if err != nil {
				return err
			}
		}
		_, err = ctx.GutQuote("add", "add", ".gutignore")
		if err != nil {
			return err
		}
		_, err = ctx.GutQuote("commit", "commit", "--message", "Inital commit")
	}
	return err
}
