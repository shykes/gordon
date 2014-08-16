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

func initDb() *libpack.DB {
	repoPath, err := git.Discover(".", false, nil)
	if err != nil {
		Fatalf("%v", err)
	}
	fmt.Printf("-> %s\n", repoPath)
	db, err := libpack.Open(repoPath, "refs/gordon", "0.0.2")
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
		signoff, err := get(db, hash, SignedOff)
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

func getUserInfo(repo *git.Repository) (name string, email string, err error) {
	cfg, err := repo.Config()
	if err != nil {
		return "", "", err
	}
	name, err = cfg.LookupString("user.name")
	if err != nil {
		return "", "", err
	}
	email, err = cfg.LookupString("user.email")
	if err != nil {
		return "", "", err
	}
	return
}

func opPath(hash, op, name, email string) string {
	return path.Join(hash, op, fmt.Sprintf("%s <%s>", name, email))
}

func get(db *libpack.DB, hash, op string) (bool, error) {
	var res bool
	name, email, err := getUserInfo(db.Repo())
	if err != nil {
		return false, err
	}
	if name == "" || email == "" {
		return false, fmt.Errorf("email or username not set in git config")
	}
	val, err := db.Get(opPath(hash, op, name, email))
	if err != nil {
		res = false
	}
	if val == "1" {
		res = true
	}
	return res, nil
}

func set(db *libpack.DB, hash string, ops ...string) error {
	name, email, err := getUserInfo(db.Repo())
	if err != nil {
		Fatalf("%v", err)
	}
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
		fmt.Printf("Setting %s to %d\n", path.Join(op, email, hash), val)
		if err := db.Set(opPath(hash, op, name, email), fmt.Sprintf("%d", val)); err != nil {
			return err
		}
	}
	return nil
}

func cmdInfo(c *cli.Context) {
	db := initDb()
	name, email, err := getUserInfo(db.Repo())
	if err != nil {
		Fatalf("%v", err)
	}
	fmt.Printf("repo = %s\nDB = %v\nUser name = %s\nUser email = %s\n",
		db.Repo().Path(),
		db.Latest(),
		name,
		email,
	)
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
		if err := set(db, c.Id().String(), SignedOff); err != nil {
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
