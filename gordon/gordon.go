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
		{
			Name:   "signoff",
			Usage:  "",
			Action: cmdSignoff,
		},
	}
	app.Run(os.Args)
}

func initDb() *libpack.DB {
	repoPath, err := git.Discover(".", false, nil)
	if err != nil {
		Fatalf("%v", err)
	}
	fmt.Printf("-> %s\n", repoPath)
	db, err := libpack.Open(repoPath, "refs/gordon", "0.1")
	if err != nil {
		Fatalf("%v", err)
	}
	return db
}

func cmdDump(c *cli.Context) {
	db := initDb()
	defer db.Free()
	if err := db.Dump(os.Stdout); err != nil {
		Fatalf("%v", err)
	}
}

func cmdLog(c *cli.Context) {
	db := initDb()
	defer db.Free()
	repo := db.Repo()
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

func set(db *libpack.DB, hash string, ops ...string) error {
	// FIXME: check that the hash exists
	for _, op := range ops {
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
			return err
		}
		db.Dump(os.Stdout)
		fmt.Printf("---\n")
	}
	return nil
}

func cmdSet(c *cli.Context) {
	if !c.Args().Present() {
		Fatalf("usage: set HASH [OP...]")
	}
	db := initDb()
	defer db.Free()
	if err := set(db, c.Args()[0], c.Args()[1:]...); err != nil {
		Fatalf("%v", err)
	}
	if err := db.Commit(strings.Join(c.Args(), " ")); err != nil {
		Fatalf("%v", err)
	}
}

func cmdSignoff(c *cli.Context) {
	if !c.Args().Present() {
		Fatalf("usage: signoff <since>[...<until]")
	}
	db := initDb()
	defer db.Free()
	walker, err := db.Repo().Walk()
	if err != nil {
		Fatalf("%v", err)
	}
	for _, arg := range c.Args() {
		if id, err := git.NewOid(arg); err == nil {
			if err := walker.Push(id); err != nil {
				Fatalf("%v", err)
			}
			continue
		}
		if strings.Contains(arg, "..") {
			if err := walker.PushRange(arg); err != nil {
				Fatalf("%v", err)
			}
		} else {
			Fatalf("invalid argument: %s", arg)
		}
	}
	if err := walker.Iterate(func(c *git.Commit) bool {
		fmt.Printf("Iterate: %v\n", c.Id())
		if err := set(db, c.Id().String(), "signoff"); err != nil {
			Fatalf("%v", err)
		}
		return true
	}); err != nil {
		Fatalf("%v", err)
	}
	if err := db.Commit("signoff " + strings.Join(c.Args(), " ")); err != nil {
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
