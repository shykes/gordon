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

func main() {
	app := cli.NewApp()
	app.Name = "pack"
	app.Usage = "A tool for high-throughput code review"
	app.Version = "0.0.1"
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
	}
	app.Run(os.Args)
}

func cmdDump(c *cli.Context) {
	db, err := libpack.Init(".git", "refs/gordon", "0.1")
	if err != nil {
		Fatalf("%v", err)
	}
	if err := db.Dump(os.Stdout); err != nil {
		Fatalf("%v", err)
	}
}

func cmdLog(c *cli.Context) {
	db, err := libpack.Init(".git", "refs/gordon", "0.1")
	if err != nil {
		Fatalf("%v", err)
	}
	defer db.Free()
	repo, err := git.InitRepository(".", false)
	if err != nil {
		Fatalf("%v", err)
	}
	defer repo.Free()
	head, err := repo.Head()
	if err != nil {
		Fatalf("%v", err)
	}
	obj, err := repo.Lookup(head.Target())
	if err != nil {
		Fatalf("%v", err)
	}
	commit, isCommit := obj.(*git.Commit)
	if !isCommit {
		Fatalf("not a commit: HEAD")
	}
	for c := commit; c != nil; c = c.Parent(0) {
		hash := c.Id().String()
		var signoff bool
		val, err := db.Get(path.Join("signoff", hash))
		if err != nil {
			// not signed off
		}
		if val == "1" {
			signoff = true
		}
		if signoff {
			fmt.Printf("%s OK\n", hash)
		} else {
			fmt.Printf("%s X\n", hash)
		}
	}
}

func cmdSet(c *cli.Context) {
	if !c.Args().Present() {
		Fatalf("usage: set HASH [OP...]")
	}
	db, err := libpack.Init(".git", "refs/gordon", "0.1")
	if err != nil {
		Fatalf("%v", err)
	}
	hash := c.Args()[0]
	for _, op := range c.Args()[1:] {
		var val int
		if op[0] == '-' {
			val = -1
			op = op[1:]
		} else {
			val = 1
		}
		if op == "" {
			continue
		}
		fmt.Printf("Setting %s to %d\n", path.Join(op, hash), val)
		if err := db.Set(path.Join(op, hash), fmt.Sprintf("%d", val)); err != nil {
			Fatalf("%v", err)
		}
	}
	// FIXME: check that the hash exists
	if err := db.Commit(strings.Join(c.Args(), " ")); err != nil {
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
