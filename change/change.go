package main

import (
	"fmt"
	"github.com/codegangsta/cli"
	"github.com/dotcloud/gordon"
	"os"
	"strings"
)

var (
	m *gordon.MaintainerManager
)

func main() {
	app := cli.NewApp()
	app.Name = "change"
	app.Usage = "Collaborate on changes to a git repository"
	app.Version = "0.0.1"
	app.Flags = []cli.Flag{
	}
	/*
	app.Before = func(c *cli.Context) error {
		t, err := gordon.NewMaintainerManager(client, org, name)
		if err != nil {
			gordon.Fatalf("init: %s", err)
		}
		m = t
	}
	*/
	app.Commands = []cli.Command{
		{
			Name:	"pull",
			Action:	cmdPull,
			Flags: []cli.Flag{
			},
		},
		{
			Name:	"push",
			Action:	cmdPush,
			Flags: []cli.Flag{
			},
		},
		{
			Name:	"new",
			Action:	cmdNew,
			Flags: []cli.Flag{
			},
		},
		{
			Name:	"list",
			Action:	cmdList,
			Flags: []cli.Flag{
			},
		},
	}
	app.Run(os.Args)
}

func cmdPull(c *cli.Context) {
	args := c.Args()
	if len(args) != 2 {
		gordon.Fatalf("usage: %s pull REMOTE NAMESPACE", c.App.Name)
	}
	remote := c.Args()[0]
	ns := c.Args()[1]

	brPrefix := fmt.Sprintf("refs/changes/%s", ns)
	if err := gordon.Git("fetch", remote, fmt.Sprintf("+%s/*:%s/*", brPrefix, brPrefix)); err != nil {
		gordon.Fatalf("%v", err)
	}
}

func cmdPush(c *cli.Context) {
	args := c.Args()
	if len(args) != 2 {
		gordon.Fatalf("usage: %s push REMOTE NAMESPACE", c.App.Name)
	}
	remote := c.Args()[0]
	ns := c.Args()[1]

	brPrefix := fmt.Sprintf("refs/changes/%s", ns)
	if err := gordon.Git("push", remote, fmt.Sprintf("+%s/*:%s/*", brPrefix, brPrefix)); err != nil {
		gordon.Fatalf("push: %v", err)
	}
}

func br(ns, subtree string) string {
	return fmt.Sprintf("refs/changes/%s/%s", ns, subtree)
}

func cmdList(c *cli.Context) {
	args := c.Args()
	if len(args) != 1 {
		gordon.Fatalf("usage: %s list NAMESPACE", c.App.Name)
	}
	ns := c.Args()[0]
	txt, _ := gordon.GitInOut("", "cat-file", "-p", br(ns, "specs^{tree}"))
	for _, line := range strings.Split(strings.Trim(txt, "\n"), "\n") {
		parts := strings.SplitN(line, "\t", 2)
		id := strings.Join(parts[1:], "")
		info := strings.SplitN(parts[0], " ", 3)
		hash := info[2]
		comment, err := gordon.GitInOut("", "cat-file", "-p", hash)
		if err != nil {
			gordon.Fatalf("git cat-file: %v", err)
		}
		fmt.Printf("%s/%s\t%s\n", ns, id, firstLine(comment))
	}
}

func firstLine(txt string) string {
	return strings.Join(strings.SplitN(txt, "\n", 2)[:1], "")
}

func cmdNew(c *cli.Context) {
	args := c.Args()
	if len(args) != 3 {
		gordon.Fatalf("usage: %s new NAMESPACE ID COMMENT", c.App.Name)
	}
	ns := c.Args()[0]
	id := c.Args()[1]
	comment := c.Args()[2]
	commentHash, err := gordon.GitInOut(comment, "hash-object", "-w", "--stdin")
	if err != nil {
		gordon.Fatalf("hash-object: %v", err)
	}
	commentHash = strings.Trim(commentHash, " \t\n")
	brPrefix := fmt.Sprintf("refs/changes/%s", ns)
	specsBr := fmt.Sprintf("%s/specs", brPrefix)
	var specsTree string
	if _, err := gordon.GitInOut("", "show-ref", specsBr); err == nil {
		if tree, err := gordon.GitInOut("", "cat-file", "-p", specsBr + "^{tree}"); err != nil {
			gordon.Fatalf("%v", err)
		} else {
			specsTree = tree
		}
	}
	fmt.Printf("old specsTree: %s\n", specsTree)
	specsTree = fmt.Sprintf("%s100644 blob %s\t%s\n", specsTree, commentHash, id)
	specsTreeHash, err := gordon.GitInOut(specsTree, "mktree")
	if err != nil {
		gordon.Fatalf("mktree: %v", err)
	}
	specsTreeHash = strings.Trim(specsTreeHash, "\n")
	specsCommit, err := gordon.GitInOut("", "commit-tree", specsTreeHash, "-m",
		fmt.Sprintf("%s/%s: %s", ns, id, comment))
	if err != nil {
		gordon.Fatalf("commit-tree: %v", err)
	}
	specsCommit = strings.Trim(specsCommit, " \t\n")
	if err := gordon.Git("update-ref", specsBr, specsCommit); err != nil {
		gordon.Fatalf("update-ref: %v", err)
	}
}
