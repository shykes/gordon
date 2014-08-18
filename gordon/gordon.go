package main

import (
	"fmt"
	"io"
	"os"
	"path"
	"sort"
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
		{
			Name:   "pull",
			Usage:  "",
			Action: cmdPull,
		},
	}
	app.Run(os.Args)
}

type env struct {
	repo  *git.Repository
	db    *libpack.DB
	peers map[string]*libpack.DB
	name  string
	email string
	auth  map[string][]string
	notes *libpack.DB
}

func initEnv() *env {
	repoPath, err := git.Discover(".", false, nil)
	if err != nil {
		Fatalf("%v", err)
	}
	fmt.Printf("-> %s\n", repoPath)
	db, err := libpack.Open(repoPath, "refs/gordon/me")
	if err != nil {
		Fatalf("%v", err)
	}
	db = db.Scope("0.0.2")
	repo := db.Repo()

	// Load peers
	peerRefs, err := repo.NewReferenceIteratorGlob("refs/gordon/peer/*")
	if err != nil {
		Fatalf("git_newreferenceiterator: %v", err)
	}
	defer peerRefs.Free()
	peers := make(map[string]*libpack.DB)
	for {
		ref, err := peerRefs.Next()
		if err == io.EOF || ref == nil {
			break
		}
		if err != nil {
			Fatalf("git_ref_next: %v", err)
		}
		refname := ref.Name()
		peer, err := libpack.Open(repoPath, refname)
		if err != nil {
			Fatalf("git_open %s: %v", refname, err)
		}
		_, base := path.Split(refname)
		peers[base] = peer.Scope("0.0.2")
	}
	notes, err := libpack.Open(repoPath, "refs/notes/commits")
	if err != nil {
		Fatalf("%v", err)
	}
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

	entries, err := cfg.NewIteratorGlob("gordon.peer.*")
	if err != nil {
		Fatalf("%v", err)
	}
	defer entries.Free()
	auth := make(map[string][]string)
	for {
		entry, err := entries.Next()
		if err == io.EOF {
			break
		}
		if entry == nil {
			break
		}
		if err != nil {
			Fatalf("%v", err)
		}
		name := strings.Split(entry.Name, ".")
		if len(name) != 4 {
			fmt.Fprintf(os.Stderr, "Skipping invalid config entry %s\n", entry.Name)
			continue
		}
		switch name[3] {
			case "allow": {
				remote := name[2]
				_, exists := auth[remote]
				if !exists {
					auth[remote] = []string{entry.Value}
				} else {
					auth[remote] = append(auth[remote], entry.Value)
				}
			}
			default: {
				fmt.Fprintf(os.Stderr, "Skipping invalid config entry %s\n", entry.Name)
				continue
			}
		}
	}

	return &env{
		repo:  repo,
		db:    db,
		peers: peers,
		name:  name,
		email: email,
		auth:  auth,
		notes: notes,
	}
}

func syncNotes(e *env) {
	hashes, err := e.db.List("/")
	if err != nil {
		Fatalf("%v", err)
	}
	for _, h := range hashes {
		signoffs, err := e.db.List(path.Join("/", h))
		if err != nil {
			Fatalf("%v", err)
		}
		var note []string
		for _, s := range signoffs {
			names, err := e.db.List(path.Join("/", h, s))
			if err != nil {
				Fatalf("%v", err)
			}
			for _, n := range names {
				note = append(note, fmt.Sprintf("%s: %s", s, n))
			}
		}
		sort.Strings(note)
		if err := e.notes.Set(h, strings.Join(note, "\n")+"\n"); err != nil {
			Fatalf("%v", err)
		}
	}
	if err := e.notes.Commit("sync"); err != nil {
		Fatalf("%v", err)
	}
}

func cmdDump(c *cli.Context) {
	e := initEnv()
	defer syncNotes(e)
	if err := e.db.Dump(os.Stdout); err != nil {
		Fatalf("%v", err)
	}
}

func cmdLog(c *cli.Context) {
	e := initEnv()
	defer syncNotes(e)
	head, err := e.repo.Head()
	if err != nil {
		Fatalf("%v", err)
	}
	obj, err := e.repo.Lookup(head.Target())
	if err != nil {
		Fatalf("git_ref_lookup: %v", err)
	}
	commit, isCommit := obj.(*git.Commit)
	if !isCommit {
		Fatalf("not a commit: HEAD")
	}
	for c := commit; c != nil; c = c.Parent(0) {
		hash := c.Id().String()
		signoffs, err := get(e, hash, SignedOff)
		if err != nil {
			Fatalf("get: %v", err)
		}
		fmt.Printf("%s\n", hash)
		for name, ok := range signoffs {
			fmt.Printf("\t%s: %v\n", name, ok)
		}
	}
}

func opPath(hash, op, name, email string) string {
	return path.Join(hash, op, fmt.Sprintf("%s <%s>", name, email))
}

func get(e *env, hash, op string) (map[string]bool, error) {
	p := libpack.NewPipeline(e.repo)
	e.db.Dump(os.Stdout)
	me := e.db.Scope(hash, op)
	fmt.Printf("me (scoped by %s/%s):\n", hash, op)
	me.Dump(os.Stdout)
	p = p.Add("/", me, true)
	for _, peerdb := range e.peers {
		// FIXME: apply filter
		p = p.Add("/", peerdb.Scope(hash, op), true)
	}
	tree, err := p.Run()
	if err != nil {
		return nil, fmt.Errorf("pipeline_run: %v", err)
	}
	defer tree.Free()
	names, err := libpack.TreeList(e.repo, tree, "/")
	if err != nil {
		return nil, fmt.Errorf("treelist: %v", err)
	}
	res := make(map[string]bool)
	for _, k := range names {
		val, err := libpack.TreeGet(e.repo, tree, k)
		if err != nil {
			return nil, fmt.Errorf("treeget %s: %v", k, err)
		}
		if val == "1" {
			res[k] = true
		} else {
			res[k] = false
		}
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
	defer syncNotes(e)
	fmt.Printf("repo = %s\nDB = %v\nUser name = %s\nUser email = %s\n",
		e.repo.Path(),
		e.db.Latest(),
		e.name,
		e.email,
	)
	for remote, rules := range e.auth {
		for _, r := range rules {
			fmt.Printf("auth: allow '%s' from '%s'\n", r, remote)
		}
	}
}

func cmdSet(c *cli.Context) {
	if !c.Args().Present() {
		Fatalf("usage: set HASH [OP...]")
	}
	e := initEnv()
	defer syncNotes(e)
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
	defer syncNotes(e)
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

func cmdPull(c *cli.Context) {
	if len(c.Args()) != 2 {
		Fatalf("usage: pull URL REF")
	}
	var (
		url = c.Args()[0]
		ref = c.Args()[1]
	)
	e := initEnv()
	defer syncNotes(e)
	peer, err := libpack.Open(e.repo.Path(), ref)
	if err != nil {
		Fatalf("%v", err)
	}
	if err := peer.Pull(url, ""); err != nil {
		Fatalf("%v", err)
	}
	if err := peer.Dump(os.Stdout); err != nil {
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
