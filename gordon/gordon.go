package main

import (
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/codegangsta/cli"
	"github.com/docker/libpack"
	git "github.com/libgit2/git2go"
)

const (
	SignedOff = "Signed-off-by"
	Reviewed  = "Reviewed-by"
	Acked     = "Acked-by"
	Tested    = "Tested-by"
	Reported  = "Reported-by"
	Suggested = "Suggested-by"
)

func main() {
	app := cli.NewApp()
	app.Name = "pack"
	app.Usage = "A tool for high-throughput code review"
	app.Version = "0.0.2"
	app.Flags = []cli.Flag{}
	app.Commands = []cli.Command{
		{
			Name:   "set",
			Usage:  "",
			Action: cmdSet,
		},
		{
			Name:   "log",
			Usage:  "",
			Action: cmdLog,
		},
		{
			Name:   "dump",
			Usage:  "",
			Action: cmdDump,
		},
		{
			Name:   "info",
			Usage:  "",
			Action: cmdInfo,
		},
		{
			Name:   "signoff",
			Usage:  "",
			Action: cmdSignoff,
		},
	}
	app.Run(os.Args)
}

type env struct {
	repo  *git.Repository
	db    libpack.DB
	name  string
	email string
}

func initEnv() *env {
	repoPath, err := git.Discover(".", false, nil)
	if err != nil {
		Fatalf("%v", err)
	}
	fmt.Printf("-> %s\n", repoPath)
	br, err := libpack.GitOpen(repoPath, "refs/gordon", "0.0.2")
	if err != nil {
		Fatalf("%v", err)
	}
	repo := br.Repo()
	cfg, err := repo.Config()
	if err != nil {
		Fatalf("%v", err)
	}
	name, err := cfg.LookupString("user.name")
	if err != nil {
		Fatalf("%v", err)
	}
	email, err := cfg.LookupString("user.email")
	if err != nil {
		Fatalf("%v", err)
	}
	if name == "" || email == "" {
		Fatalf("email or username not set in git config")
	}
	return &env{
		repo:  repo,
		db:    br,
		name:  name,
		email: email,
	}
}

func cmdDump(c *cli.Context) {
	e := initEnv()
	if err := libpack.Dump(e.db, os.Stdout); err != nil {
		Fatalf("%v", err)
	}
}

func cmdLog(c *cli.Context) {
	e := initEnv()
	head, err := e.repo.Head()
	if err != nil {
		Fatalf("%v", err)
	}
	obj, err := e.repo.Lookup(head.Target())
	if err != nil {
		Fatalf("%v", err)
	}
	commit, isCommit := obj.(*git.Commit)
	if !isCommit {
		Fatalf("not a commit: HEAD")
	}
	for c := commit; c != nil; c = c.Parent(0) {
		hash := c.Id().String()
		signoff, err := get(e, hash, SignedOff)
		if err != nil {
			Fatalf("%v", err)
		}
		if signoff {
			fmt.Printf("%s OK\n", hash)
		} else {
			fmt.Printf("%s X\n", hash)
		}
	}
}

func opPath(hash, op, name, email string) string {
	return path.Join(hash, op, fmt.Sprintf("%s <%s>", name, email))
}

func get(e *env, hash, op string) (bool, error) {
	var res bool
	val, err := e.db.Get(opPath(hash, op, e.name, e.email))
	if err != nil {
		res = false
	}
	if val == "1" {
		res = true
	}
	return res, nil
}

func set(e *env, hash string, ops ...string) error {
	// FIXME: check that the hash exists
	for _, op := range ops {
		var val int
		if op[0] == '-' {
			val = -1
			op = op[1:]
		} else if op[0] == '+' {
			val = 1
			op = op[1:]
		} else {
			val = 1
		}
		if op == "" {
			continue
		}
		if err := e.db.Set(opPath(hash, op, e.name, e.email), fmt.Sprintf("%d", val)); err != nil {
			return err
		}
	}
	return nil
}

func cmdInfo(c *cli.Context) {
	e := initEnv()
	fmt.Printf("repo = %s\nDB = %v\nUser name = %s\nUser email = %s\n",
		e.repo.Path(),
		e.db.(*libpack.GitBranch).Latest(),
		e.name,
		e.email,
	)
}

func cmdSet(c *cli.Context) {
	if !c.Args().Present() {
		Fatalf("usage: set HASH [OP...]")
	}
	e := initEnv()
	if err := set(e, c.Args()[0], c.Args()[1:]...); err != nil {
		Fatalf("%v", err)
	}
	if err := e.db.Commit(strings.Join(c.Args(), " ")); err != nil {
		Fatalf("%v", err)
	}
}

func cmdSignoff(c *cli.Context) {
	if !c.Args().Present() {
		Fatalf("usage: signoff <since>[...<until]")
	}
	e := initEnv()
	setCommit := func(c *git.Commit) bool {
		if err := set(e, c.Id().String(), SignedOff); err != nil {
			Fatalf("%v", err)
		}
		return true
	}
	for _, arg := range c.Args() {
		if id, err := git.NewOid(arg); err == nil {
			obj, err := e.repo.Lookup(id)
			if err != nil {
				Fatalf("%v", err)
			}
			commit, isCommit := obj.(*git.Commit)
			if !isCommit {
				Fatalf("not a commit: %v", id)
			}
			setCommit(commit)
			continue
		}
		if strings.Contains(arg, "..") {
			walker, err := e.repo.Walk()
			if err != nil {
				Fatalf("%v", err)
			}
			if err := walker.PushRange(arg); err != nil {
				Fatalf("%v", err)
			}
			if err := walker.Iterate(setCommit); err != nil {
				Fatalf("%v", err)
			}
			walker.Free()
		} else {
			Fatalf("invalid argument: %s", arg)
		}
	}
	if err := e.db.Commit("signoff " + strings.Join(c.Args(), " ")); err != nil {
		Fatalf("%v", err)
	}
}

func Fatalf(msg string, args ...interface{}) {
	if !strings.HasSuffix(msg, "\n") {
		msg = msg + "\n"
	}
	fmt.Fprintf(os.Stderr, msg, args...)
	os.Exit(1)
}
